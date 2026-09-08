"""What the auth routes actually do, with the database.

Every function here takes a session and leaves the transaction to the caller,
so a redemption — activate the code, move the seat, mint the session — commits
as one unit or not at all.
"""

from __future__ import annotations

import datetime as dt
import logging

import secrets

from sqlalchemy import delete, select, update
from sqlalchemy.dialects.postgresql import insert as pg_insert
from sqlalchemy.orm import Session

from api.auth.models import (
    ADMIN_USERNAME,
    ANON_USERNAME,
    AdminChallenge,
    AuthSession,
    FreeAccessCode,
    User,
)
from api.auth.passwords import hash_password, verify_password
from api.auth.tokens import Principal, hash_refresh_token, new_refresh_token

log = logging.getLogger(__name__)

#: Codes are the only secret standing in front of the metered features, so a
#: short one is a real hole. 16 urlsafe characters is ~95 bits.
MIN_CODE_LENGTH = 16
MAX_CODE_LENGTH = 128
#: One request may not create more than this many codes.
MAX_CODES_PER_REQUEST = 200

#: Length of an auto-generated admin password. token_urlsafe(24) is exactly 32
#: characters and carries 192 bits, so the "32 characters" is a real 32, not a
#: truncation of something longer.
GENERATED_PASSWORD_BYTES = 24
#: A supplied admin password shorter than this is refused. Not in the spec, but
#: this credential unlocks every metered feature and the admin session.
MIN_ADMIN_PASSWORD_LENGTH = 12


class AuthError(Exception):
    """A failure with an HTTP status already decided."""

    def __init__(self, status: int, message: str) -> None:
        super().__init__(message)
        self.status = status
        self.message = message


class Grant:
    """A freshly minted pair: the access token's principal and a refresh token."""

    def __init__(
        self, principal: Principal, refresh_token: str, expires_at: dt.datetime
    ) -> None:
        self.principal = principal
        self.refresh_token = refresh_token
        #: When the *session* dies. The access token expires much sooner.
        self.expires_at = expires_at


def utcnow() -> dt.datetime:
    return dt.datetime.now(dt.timezone.utc)


def normalise_code(raw: str) -> str:
    """Trim only. Codes are opaque strings chosen by the admin, and case may
    well be load-bearing in them."""
    return raw.strip()


def get_user(session: Session, username: str) -> User | None:
    return session.execute(
        select(User).where(User.username == username)
    ).scalar_one_or_none()


def _require_account(session: Session, username: str) -> User:
    user = get_user(session, username)
    if user is None:
        # The seed migration creates both accounts, so this means someone
        # deleted a row rather than that the app is misconfigured.
        raise AuthError(500, f"the {username!r} account is missing")
    if user.disabled_at is not None:
        raise AuthError(403, f"the {username!r} account is disabled")
    return user


def _principal(user: User, session_id: int | None) -> Principal:
    return Principal(
        user_id=user.id,
        username=user.username,
        is_admin=user.is_admin,
        session_id=session_id,
    )


def _open_session(
    session: Session,
    user: User,
    expires_at: dt.datetime,
    code: FreeAccessCode | None = None,
) -> Grant:
    token, digest = new_refresh_token()
    row = AuthSession(
        user_id=user.id,
        user_is_anonymous=user.is_anonymous,
        refresh_token_hash=digest,
        free_access_code_id=code.id if code else None,
        code_activated=True if code else None,
        expires_at=expires_at,
        last_used_at=utcnow(),
    )
    session.add(row)
    # The id is needed for the token's `sid` claim, and flushing here also
    # surfaces a constraint violation as an error we can still map to a status.
    session.flush()
    return Grant(_principal(user, row.id), token, expires_at)


# --- code redemption -------------------------------------------------------


def redeem_code(session: Session, raw_code: str, window_hours: int) -> Grant:
    """Trade an access code for a session on the `anonfree` account.

    `SELECT ... FOR UPDATE` on the code row serialises two people submitting
    the same code at once; the partial unique index on live sessions is the
    backstop if that lock is ever bypassed.
    """
    value = normalise_code(raw_code)
    if not value:
        raise AuthError(400, "Enter an access code.")

    code = session.execute(
        select(FreeAccessCode).where(FreeAccessCode.code == value).with_for_update()
    ).scalar_one_or_none()
    if code is None:
        raise AuthError(401, "That access code is not recognised.")

    now = utcnow()
    if code.activated:
        assert code.activation_date is not None  # ck_free_access_codes_activation_pair
        expires_at = code.activation_date + dt.timedelta(hours=window_hours)
        if expires_at <= now:
            raise AuthError(401, "That access code has expired.")
    else:
        # Stamped once and never again: restamping on every redemption would
        # put the 48-hour window permanently out of reach.
        code.activated = True
        code.activation_date = now
        expires_at = now + dt.timedelta(hours=window_hours)

    # One seat per code. Redeeming on a second device moves the seat rather
    # than minting a second token, which is what keeps live anonfree sessions
    # from ever outnumbering activated codes.
    session.execute(
        update(AuthSession)
        .where(
            AuthSession.free_access_code_id == code.id,
            AuthSession.revoked_at.is_(None),
        )
        .values(revoked_at=now)
    )

    user = _require_account(session, ANON_USERNAME)
    log.info("access code %s redeemed, session until %s", code.id, expires_at)
    return _open_session(session, user, expires_at, code=code)


# --- password login --------------------------------------------------------


def login(
    session: Session, username: str, password: str, refresh_ttl_seconds: int
) -> Grant:
    user = get_user(session, username.strip())
    # One message for both "no such user" and "wrong password": the difference
    # tells an attacker which half to keep working on.
    if user is None or user.disabled_at is not None:
        raise AuthError(401, "Incorrect username or password.")
    if not verify_password(password, user.password_hash):
        raise AuthError(401, "Incorrect username or password.")

    expires_at = utcnow() + dt.timedelta(seconds=refresh_ttl_seconds)
    log.info("%s signed in, session until %s", user.username, expires_at)
    return _open_session(session, user, expires_at)


# --- refresh and logout ----------------------------------------------------


def refresh(
    session: Session, raw_token: str, window_hours: int, refresh_ttl_seconds: int
) -> Grant:
    """Rotate a refresh token, re-checking everything that could have changed.

    Rotation is in place: the presented token stops working the moment this
    succeeds, so a captured token is good for exactly one use and a replay
    arrives as an unknown digest.
    """
    digest = hash_refresh_token(raw_token)
    row = session.execute(
        select(AuthSession)
        .where(AuthSession.refresh_token_hash == digest)
        .with_for_update()
    ).scalar_one_or_none()
    if row is None:
        raise AuthError(401, "Your session has ended. Enter your access code again.")

    def kill(message: str) -> AuthError:
        """Revoke the row and commit *before* raising.

        The caller's `session_scope` rolls back on an exception, so a plain
        assignment here would be thrown away and the dead session would keep
        holding its seat until the row expired on its own. Committing first is
        what actually frees the code for re-use.
        """
        row.revoked_at = utcnow()
        session.commit()
        return AuthError(401, message)

    now = utcnow()
    if row.revoked_at is not None or row.expires_at <= now:
        raise AuthError(401, "Your session has ended. Enter your access code again.")

    user = session.get(User, row.user_id)
    if user is None or user.disabled_at is not None:
        raise kill("Your session has ended. Enter your access code again.")

    if row.free_access_code_id is not None:
        code = session.get(FreeAccessCode, row.free_access_code_id)
        # The code is the authority on the window, not the session row: this is
        # what stops a code-backed session from outliving its 48 hours.
        if code is None or code.activation_date is None:
            raise kill("Your access code is no longer valid.")
        code_expires = code.activation_date + dt.timedelta(hours=window_hours)
        if code_expires <= now:
            raise kill("Your access code has expired.")
        row.expires_at = code_expires
    else:
        # Password sessions slide forward on use.
        row.expires_at = now + dt.timedelta(seconds=refresh_ttl_seconds)

    token, new_digest = new_refresh_token()
    row.refresh_token_hash = new_digest
    row.last_used_at = now
    return Grant(_principal(user, row.id), token, row.expires_at)


def revoke(session: Session, raw_token: str) -> None:
    """End a session. Unknown tokens are ignored — logout always succeeds."""
    session.execute(
        update(AuthSession)
        .where(
            AuthSession.refresh_token_hash == hash_refresh_token(raw_token),
            AuthSession.revoked_at.is_(None),
        )
        .values(revoked_at=utcnow())
    )


# --- admin -----------------------------------------------------------------


def generate_codes(session: Session, codes: list[str]) -> tuple[int, int]:
    """Insert codes as unactivated. Returns `(inserted, skipped)`.

    Idempotent: re-posting a batch that overlaps an earlier one inserts the new
    codes and leaves the existing ones exactly as they are, rather than failing
    the whole request or — much worse — resetting a live code's clock.
    """
    if not codes:
        raise AuthError(400, "No codes supplied.")
    if len(codes) > MAX_CODES_PER_REQUEST:
        raise AuthError(400, f"At most {MAX_CODES_PER_REQUEST} codes per request.")

    seen: list[str] = []
    for raw in codes:
        value = normalise_code(raw)
        if not (MIN_CODE_LENGTH <= len(value) <= MAX_CODE_LENGTH):
            raise AuthError(
                400,
                f"Codes must be {MIN_CODE_LENGTH}-{MAX_CODE_LENGTH} characters; "
                f"{value[:8]!r}... is {len(value)}.",
            )
        if value not in seen:
            seen.append(value)

    result = session.execute(
        pg_insert(FreeAccessCode)
        .values([{"code": value} for value in seen])
        .on_conflict_do_nothing(index_elements=[FreeAccessCode.code])
        .returning(FreeAccessCode.id)
    )
    inserted = len(result.scalars().all())
    log.info("generated %d access codes (%d already existed)", inserted, len(seen) - inserted)
    return inserted, len(seen) - inserted


def count_live_anonymous_sessions(session: Session) -> int:
    """Live anonfree sessions. Reported by /admin/codes as a sanity read on the
    invariant the schema enforces."""
    now = utcnow()
    return len(
        session.execute(
            select(AuthSession.id).where(
                AuthSession.user_is_anonymous.is_(True),
                AuthSession.revoked_at.is_(None),
                AuthSession.expires_at > now,
            )
        )
        .scalars()
        .all()
    )


def admin_account(session: Session) -> User:
    return _require_account(session, ADMIN_USERNAME)


# --- signature challenges --------------------------------------------------


def issue_challenge(session: Session, action: str, ttl_seconds: int) -> AdminChallenge:
    """Mint a single-use nonce for `action`.

    Expired rows are swept here rather than on a timer: the table only grows
    when an admin call is attempted, so the cheapest place to keep it small is
    the next attempt.
    """
    now = utcnow()
    session.execute(delete(AdminChallenge).where(AdminChallenge.expires_at <= now))
    challenge = AdminChallenge(
        nonce=secrets.token_urlsafe(32),
        action=action,
        expires_at=now + dt.timedelta(seconds=ttl_seconds),
    )
    session.add(challenge)
    session.flush()
    return challenge


def consume_challenge(session: Session, nonce: str, action: str) -> None:
    """Spend a nonce, or raise.

    `DELETE ... RETURNING` is what makes it single use: the delete and the
    check are one statement, so two requests racing the same nonce cannot both
    see it as unspent.
    """
    row = session.execute(
        delete(AdminChallenge)
        .where(
            AdminChallenge.nonce == nonce,
            AdminChallenge.action == action,
            AdminChallenge.expires_at > utcnow(),
        )
        .returning(AdminChallenge.id)
    ).first()
    if row is None:
        raise AuthError(401, "Challenge is unknown, expired, or already used.")


# --- the admin account -----------------------------------------------------


def create_or_rotate_admin(
    session: Session, password: str
) -> tuple[str, bool, bool, int]:
    """Create the admin account, or rotate its password.

    Returns `(password, generated, existed, revoked)`. The password comes back so the
    caller can show it once; it is never stored in a readable form and there is
    no way to ask for it again.

    An empty string means "choose one for me". Any authenticated call rotates,
    by design: there is no way to invoke this and leave the old password
    working, so a leaked password is fixed by calling it again.
    """
    generated = password == ""
    if generated:
        password = secrets.token_urlsafe(GENERATED_PASSWORD_BYTES)
    elif len(password) < MIN_ADMIN_PASSWORD_LENGTH:
        raise AuthError(
            400,
            f"An admin password must be at least {MIN_ADMIN_PASSWORD_LENGTH} "
            "characters, or \"\" to have one generated.",
        )

    user = get_user(session, ADMIN_USERNAME)
    existed = user is not None
    if user is None:
        # uq_users_single_admin makes a second admin row impossible, so this
        # cannot quietly create a rival account if the username ever changes.
        user = User(username=ADMIN_USERNAME, is_admin=True, is_anonymous=False)
        session.add(user)
    user.password_hash = hash_password(password)
    user.disabled_at = None
    session.flush()

    # Every session opened with the old password is now a key that outlived the
    # credential it was issued for.
    revoked = session.execute(
        update(AuthSession)
        .where(AuthSession.user_id == user.id, AuthSession.revoked_at.is_(None))
        .values(revoked_at=utcnow())
    ).rowcount
    log.info(
        "admin password %s (%s); %d session(s) revoked",
        "rotated" if existed else "set",
        "generated" if generated else "supplied",
        revoked,
    )
    return password, generated, existed, revoked

"""HTTP surface for authentication, mounted at /auth, plus /admin.

Sync (`def`, not `async def`) like the corpus and ingestion routes: these talk
to Postgres through SQLAlchemy's blocking API, so FastAPI runs them in its
threadpool instead of stalling the event loop. `login` additionally burns ~100
ms in scrypt, which must not happen on the loop either.
"""

from __future__ import annotations

import logging
from functools import lru_cache
from typing import Optional

from fastapi import APIRouter, Depends, HTTPException, Request, Response
from sqlalchemy import func, select

from api.auth.config import AuthSettings, get_auth_settings
from api.auth import service
from api.auth.dependencies import optional_principal
from api.auth.models import FreeAccessCode
from api.auth.schemas import (
    ChallengeRequest,
    ChallengeResponse,
    CreateAdminRequest,
    CreateAdminResponse,
    GenerateCodesRequest,
    GenerateCodesResponse,
    LoginRequest,
    RedeemRequest,
    SessionInfo,
    SignedRequest,
    TokenResponse,
    UserInfo,
)
from api.auth.service import AuthError, Grant
from api.auth import sshsig
from api.auth.throttle import SlidingWindow
from api.auth.tokens import Principal, issue_access_token
from api.db import session_scope

log = logging.getLogger(__name__)

router = APIRouter(tags=["auth"])
admin_router = APIRouter(prefix="/admin", tags=["admin"])

#: Scoped to /auth so the long-lived credential is sent to the refresh and
#: logout routes and nowhere else — not to /corpus, not to /import.
REFRESH_COOKIE = "sciterm_refresh"
REFRESH_COOKIE_PATH = "/auth"


@lru_cache(maxsize=1)
def _limits() -> tuple:
    """The two limiters, built once per process from settings.

    Module state rather than a dependency: the point is to cap this process's
    own work, so the counters must outlive any single request. See
    `api.auth.throttle` for why they are not shared across tasks.
    """
    settings = get_auth_settings()
    return (
        SlidingWindow(settings.login_attempts_per_window, settings.throttle_window_seconds),
        SlidingWindow(settings.redeem_attempts_per_window, settings.throttle_window_seconds),
    )


def _client_key(request: Request) -> str:
    """The caller's address, as uvicorn understands it.

    Behind a load balancer this is only the real client when uvicorn runs with
    `--proxy-headers`; without it every request looks like it came from the
    balancer and the address limit degrades into a global one. The username
    limit on /auth/login is what still bounds the CPU in that case.
    """
    client = request.client
    return client.host if client else "unknown"


def _throttle(window: SlidingWindow, *keys: str) -> None:
    for key in keys:
        wait = window.retry_after(key)
        if wait is None:
            continue
        raise HTTPException(
            status_code=429,
            detail="Too many attempts. Try again shortly.",
            headers={"Retry-After": str(int(wait) + 1)},
        )


def _fail(exc: AuthError) -> HTTPException:
    return HTTPException(status_code=exc.status, detail=exc.message)


def _set_refresh_cookie(response: Response, token: str, settings: AuthSettings) -> None:
    response.set_cookie(
        REFRESH_COOKIE,
        token,
        max_age=settings.refresh_token_ttl_seconds,
        httponly=True,
        # Lax, not Strict: the app is a single origin and Strict would drop the
        # cookie on a plain link into the site, logging people out on arrival.
        samesite="lax",
        secure=settings.refresh_cookie_secure,
        path=REFRESH_COOKIE_PATH,
    )


def _grant_response(
    grant: Grant, response: Response, settings: AuthSettings
) -> TokenResponse:
    access_token, expires = issue_access_token(
        grant.principal, settings.signing_secret, settings.access_token_ttl_seconds
    )
    _set_refresh_cookie(response, grant.refresh_token, settings)
    return TokenResponse(
        access_token=access_token,
        expires_in=settings.access_token_ttl_seconds,
        session_expires_at=grant.expires_at,
        user=UserInfo(
            username=grant.principal.username, is_admin=grant.principal.is_admin
        ),
    )


@router.get("/auth/session", response_model=SessionInfo, summary="Who is signed in")
def read_session(
    principal: Optional[Principal] = Depends(optional_principal),
    settings: AuthSettings = Depends(get_auth_settings),
) -> SessionInfo:
    """Always 200, never 401.

    The UI asks this on load to decide whether to show the "Enter Access Code"
    button; making it an error case would mean provoking a failure to render a
    normal screen.
    """
    if principal is None:
        return SessionInfo(auth_required=settings.auth_required, authenticated=False)
    return SessionInfo(
        auth_required=settings.auth_required,
        authenticated=True,
        user=UserInfo(username=principal.username, is_admin=principal.is_admin),
    )


@router.post(
    "/auth/redeem", response_model=TokenResponse, summary="Trade an access code for a token"
)
def redeem(
    body: RedeemRequest,
    request: Request,
    response: Response,
    settings: AuthSettings = Depends(get_auth_settings),
) -> TokenResponse:
    """Activate a free access code and open a session on `anonfree`.

    The code's clock starts on first redemption and is never restarted, so the
    window is 48 hours from first use however many times it is entered.
    """
    _throttle(_limits()[1], f"ip:{_client_key(request)}")
    try:
        with session_scope() as session:
            grant = service.redeem_code(
                session, body.code, settings.free_code_window_hours
            )
            return _grant_response(grant, response, settings)
    except AuthError as exc:
        raise _fail(exc) from exc


@router.post("/auth/login", response_model=TokenResponse, summary="Sign in with a password")
def login(
    body: LoginRequest,
    request: Request,
    response: Response,
    settings: AuthSettings = Depends(get_auth_settings),
) -> TokenResponse:
    """Password sign-in. In practice this is the admin account: `anonfree` is
    forbidden a password hash by a table constraint."""
    # Both keys, because they stop different attacks: the address limit stops
    # one source, the username limit caps PBKDF2 work however many sources it
    # is spread across.
    _throttle(
        _limits()[0],
        f"ip:{_client_key(request)}",
        f"user:{body.username.strip().lower()}",
    )
    try:
        with session_scope() as session:
            grant = service.login(
                session, body.username, body.password, settings.refresh_token_ttl_seconds
            )
            return _grant_response(grant, response, settings)
    except AuthError as exc:
        raise _fail(exc) from exc


@router.post("/auth/refresh", response_model=TokenResponse, summary="Rotate the session")
def refresh(
    request: Request,
    response: Response,
    settings: AuthSettings = Depends(get_auth_settings),
) -> TokenResponse:
    """Exchange the refresh cookie for a new access token, rotating the cookie.

    Re-reads the access code every time, so an expired code cannot be refreshed
    past its window no matter what the browser is holding.
    """
    token = request.cookies.get(REFRESH_COOKIE)
    if not token:
        raise HTTPException(status_code=401, detail="No session to refresh.")
    try:
        with session_scope() as session:
            grant = service.refresh(
                session,
                token,
                settings.free_code_window_hours,
                settings.refresh_token_ttl_seconds,
            )
            return _grant_response(grant, response, settings)
    except AuthError as exc:
        # A dead session must not leave a cookie behind that keeps provoking
        # the same 401 on every load.
        response.delete_cookie(REFRESH_COOKIE, path=REFRESH_COOKIE_PATH)
        raise _fail(exc) from exc


@router.post("/auth/logout", status_code=204, summary="End the session")
def logout(request: Request, response: Response) -> Response:
    token = request.cookies.get(REFRESH_COOKIE)
    if token:
        with session_scope() as session:
            service.revoke(session, token)
    response.delete_cookie(REFRESH_COOKIE, path=REFRESH_COOKIE_PATH)
    response.status_code = 204
    return response


# --- admin -----------------------------------------------------------------
#
# These endpoints are not authenticated by a token. They take an SSH signature
# over a single-use nonce, made by a key whose public half is committed in
# `api/authorized_keys/`. That is what lets a fresh deployment be administered
# with no secret provisioned into it at all -- and it is what breaks the
# chicken-and-egg of `create_admin_user`, which cannot require an admin
# password because its whole job is to set one.

#: Which endpoint a nonce was minted for, checked when it is spent.
ACTION_GENERATE_CODES = "generate_codes"
ACTION_CREATE_ADMIN = "create_admin_user"
_ACTIONS = {ACTION_GENERATE_CODES, ACTION_CREATE_ADMIN}


def _authenticate_signature(
    body: SignedRequest, action: str, settings: AuthSettings, session
) -> str:
    """Spend the nonce, check the signature over it, return the signer.

    Order matters: the nonce is consumed *first*, so a wrong signature still
    burns it. Otherwise an attacker could grind signature attempts against one
    long-lived nonce.
    """
    service.consume_challenge(session, body.nonce, action)
    try:
        allowed = sshsig.load_allowed_keys(settings.authorized_keys_dir)
        signer = sshsig.verify(
            body.nonce.encode("utf-8"),
            body.signature,
            allowed,
            settings.ssh_signature_namespace,
        )
    except sshsig.SignatureError as exc:
        # One generic message: a prober must not learn whether it got the
        # namespace wrong, the key wrong, or the bytes wrong.
        log.warning("admin signature rejected for %s: %s", action, exc)
        raise HTTPException(status_code=401, detail="Signature rejected.") from exc
    log.info("admin %s authorised by %s (%s)", action, signer.source, signer.comment)
    return signer.comment or signer.source


@admin_router.post(
    "/challenge", response_model=ChallengeResponse, summary="Get a nonce to sign"
)
def challenge(
    body: ChallengeRequest,
    request: Request,
    settings: AuthSettings = Depends(get_auth_settings),
) -> ChallengeResponse:
    """Step one of every admin call: ask for something to sign.

    Deliberately unauthenticated. A nonce is a random number that grants
    nothing on its own, and requiring credentials to obtain one would recreate
    the bootstrap problem this design exists to avoid.
    """
    _throttle(_limits()[1], f"challenge:{_client_key(request)}")
    if body.action not in _ACTIONS:
        raise HTTPException(status_code=400, detail=f"Unknown action {body.action!r}.")
    with session_scope() as session:
        issued = service.issue_challenge(
            session, body.action, settings.admin_challenge_ttl_seconds
        )
        return ChallengeResponse(
            nonce=issued.nonce,
            expires_at=issued.expires_at,
            namespace=settings.ssh_signature_namespace,
        )


@admin_router.post(
    "/generate_codes",
    response_model=GenerateCodesResponse,
    summary="Add free access codes",
)
def generate_codes(
    body: GenerateCodesRequest, settings: AuthSettings = Depends(get_auth_settings)
) -> GenerateCodesResponse:
    """Insert codes in the unactivated state, ready to be handed out.

    Idempotent on re-post: a code that already exists is counted in `skipped`
    and left exactly as it is, so re-running a batch cannot rearm a code that
    somebody is already using.
    """
    try:
        with session_scope() as session:
            _authenticate_signature(body, ACTION_GENERATE_CODES, settings, session)
            inserted, skipped = service.generate_codes(session, body.codes)
            total = session.execute(
                select(func.count()).select_from(FreeAccessCode)
            ).scalar_one()
            activated = session.execute(
                select(func.count())
                .select_from(FreeAccessCode)
                .where(FreeAccessCode.activated.is_(True))
            ).scalar_one()
            return GenerateCodesResponse(
                inserted=inserted,
                skipped=skipped,
                total_codes=total,
                activated_codes=activated,
                live_anonymous_sessions=service.count_live_anonymous_sessions(session),
            )
    except AuthError as exc:
        raise _fail(exc) from exc


@admin_router.post(
    "/create_admin_user",
    response_model=CreateAdminResponse,
    summary="Create the admin account, or rotate its password",
)
def create_admin_user(
    body: CreateAdminRequest, settings: AuthSettings = Depends(get_auth_settings)
) -> CreateAdminResponse:
    """Set the admin password, and always change it.

    There is no way to call this and leave the previous password working: an
    authenticated call is a rotation. That is the point -- the SSH key is the
    root of trust, and the password is a derived, disposable credential you can
    replace at any time from a laptop with no server access.

    The username is always `admin`, enforced here and by a partial unique index
    that permits one admin row in the table.
    """
    if body.username != "admin":
        raise HTTPException(
            status_code=400,
            detail="The admin username is always 'admin'; omit the field or send \"admin\".",
        )
    try:
        with session_scope() as session:
            _authenticate_signature(body, ACTION_CREATE_ADMIN, settings, session)

            password, generated, existed, revoked = service.create_or_rotate_admin(
                session, body.password
            )
            return CreateAdminResponse(
                username="admin",
                password=password,
                generated=generated,
                rotated=existed,
                revoked_sessions=revoked,
            )
    except AuthError as exc:
        raise _fail(exc) from exc

"""The two token kinds.

**Access token** — a short-lived JWT, verified from its signature alone. No
database round-trip per request, which is the whole point of using one; the
cost is that a revoked session keeps working until the token expires. That
window is `access_token_ttl_seconds` (15 minutes by default), and it is
deliberately much shorter than the 48-hour code window it sits inside.

**Refresh token** — opaque, 256 bits from `secrets`, stored only as a sha256
digest. Nothing about it is guessable and a database leak yields no usable
tokens. It rotates on every refresh, so a captured token is single-use.
"""

from __future__ import annotations

import datetime as dt
import hashlib
import secrets
from dataclasses import dataclass
from typing import Optional

import jwt

_ALGORITHM = "HS256"
_ISSUER = "sciterm"


@dataclass(frozen=True)
class Principal:
    """Who is making a request. Built from a verified token, never from input."""

    user_id: int
    username: str
    is_admin: bool
    #: The auth_sessions row this token belongs to. None for the local-dev
    #: principal, which has no session because it never authenticated.
    session_id: Optional[int]


def new_refresh_token() -> tuple[str, str]:
    """Return `(token, sha256_hex)`. Only the digest is ever persisted."""
    token = secrets.token_urlsafe(32)
    return token, hash_refresh_token(token)


def hash_refresh_token(token: str) -> str:
    return hashlib.sha256(token.encode("utf-8")).hexdigest()


def issue_access_token(
    principal: Principal, secret: str, ttl_seconds: int
) -> tuple[str, dt.datetime]:
    now = dt.datetime.now(dt.timezone.utc)
    expires = now + dt.timedelta(seconds=ttl_seconds)
    payload = {
        "iss": _ISSUER,
        "sub": str(principal.user_id),
        "name": principal.username,
        "adm": principal.is_admin,
        "sid": principal.session_id,
        "iat": int(now.timestamp()),
        "exp": int(expires.timestamp()),
    }
    return jwt.encode(payload, secret, algorithm=_ALGORITHM), expires


def decode_access_token(token: str, secret: str) -> Optional[Principal]:
    """The verified principal, or None for anything we would not trust.

    Signature, expiry and issuer are all checked by PyJWT. A token that fails
    any of them is indistinguishable from no token at all.
    """
    try:
        payload = jwt.decode(token, secret, algorithms=[_ALGORITHM], issuer=_ISSUER)
        return Principal(
            user_id=int(payload["sub"]),
            username=str(payload["name"]),
            is_admin=bool(payload.get("adm", False)),
            session_id=payload.get("sid"),
        )
    except (jwt.InvalidTokenError, KeyError, TypeError, ValueError):
        return None

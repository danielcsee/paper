"""FastAPI dependencies that turn a bearer token into a `Principal`.

Verification is signature-only — no database round-trip per request. The
trade-off is bounded by the access token's 15-minute life: a session revoked
now stops working within that window, while the 48-hour code deadline is
enforced properly at every refresh.

In `SCITERM_ENV=local` nothing is gated and every dependency yields a local
admin, so development is exactly as it was before auth existed.
"""

from __future__ import annotations

from typing import Optional

from fastapi import Depends, HTTPException, Request

from api.app.config import Settings, get_settings
from api.auth.models import ADMIN_USERNAME
from api.auth.tokens import Principal, decode_access_token

#: The principal used when auth is switched off. It has no session row, which
#: is why `session_id` is None — nothing about it can be revoked.
LOCAL_ADMIN = Principal(user_id=0, username=ADMIN_USERNAME, is_admin=True, session_id=None)


def bearer_token(request: Request) -> Optional[str]:
    header = request.headers.get("authorization")
    if not header:
        return None
    scheme, _, token = header.partition(" ")
    if scheme.lower() != "bearer" or not token.strip():
        return None
    return token.strip()


def optional_principal(
    request: Request, settings: Settings = Depends(get_settings)
) -> Optional[Principal]:
    """Whoever is calling, or None. Never raises — for routes that are open but
    behave differently for a signed-in caller."""
    if not settings.auth_required:
        return LOCAL_ADMIN
    token = bearer_token(request)
    if token is None:
        return None
    return decode_access_token(token, settings.signing_secret)


def require_user(
    principal: Optional[Principal] = Depends(optional_principal),
) -> Principal:
    """Gate a route on any valid token.

    Attached at router level to the routers that spend money or NCBI budget, so
    a route added to one of them tomorrow is protected the day it is written.
    """
    if principal is None:
        raise HTTPException(
            status_code=401,
            detail="Enter an access code to use this feature.",
            headers={"WWW-Authenticate": "Bearer"},
        )
    return principal


def require_admin(principal: Principal = Depends(require_user)) -> Principal:
    if not principal.is_admin:
        raise HTTPException(status_code=403, detail="Administrator access required.")
    return principal

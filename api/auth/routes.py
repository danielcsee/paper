"""HTTP surface for authentication, mounted at /auth, plus /admin.

Sync (`def`, not `async def`) like the corpus and ingestion routes: these talk
to Postgres through SQLAlchemy's blocking API, so FastAPI runs them in its
threadpool instead of stalling the event loop. `login` additionally burns ~100
ms in scrypt, which must not happen on the loop either.
"""

from __future__ import annotations

import logging
from typing import Optional

from fastapi import APIRouter, Depends, HTTPException, Request, Response
from sqlalchemy import func, select

from api.app.config import Settings, get_settings
from api.auth import service
from api.auth.dependencies import LOCAL_ADMIN, bearer_token, optional_principal
from api.auth.models import FreeAccessCode
from api.auth.schemas import (
    GenerateCodesRequest,
    GenerateCodesResponse,
    LoginRequest,
    RedeemRequest,
    SessionInfo,
    TokenResponse,
    UserInfo,
)
from api.auth.service import AuthError, Grant
from api.auth.tokens import Principal, decode_access_token, issue_access_token
from api.db import session_scope

log = logging.getLogger(__name__)

router = APIRouter(tags=["auth"])
admin_router = APIRouter(prefix="/admin", tags=["admin"])

#: Scoped to /auth so the long-lived credential is sent to the refresh and
#: logout routes and nowhere else — not to /corpus, not to /import.
REFRESH_COOKIE = "sciterm_refresh"
REFRESH_COOKIE_PATH = "/auth"


def _fail(exc: AuthError) -> HTTPException:
    return HTTPException(status_code=exc.status, detail=exc.message)


def _set_refresh_cookie(response: Response, token: str, settings: Settings) -> None:
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
    grant: Grant, response: Response, settings: Settings
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
    settings: Settings = Depends(get_settings),
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
    response: Response,
    settings: Settings = Depends(get_settings),
) -> TokenResponse:
    """Activate a free access code and open a session on `anonfree`.

    The code's clock starts on first redemption and is never restarted, so the
    window is 48 hours from first use however many times it is entered.
    """
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
    response: Response,
    settings: Settings = Depends(get_settings),
) -> TokenResponse:
    """Password sign-in. In practice this is the admin account: `anonfree` is
    forbidden a password hash by a table constraint."""
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
    settings: Settings = Depends(get_settings),
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


def _admin_principal(
    request: Request, body_token: Optional[str], settings: Settings
) -> Principal:
    """Authenticate an admin from the body's `token` or the bearer header.

    The token is an ordinary admin access token — there is no second shared
    secret to leak, and revoking the admin session closes this door too.
    """
    if not settings.auth_required:
        return LOCAL_ADMIN
    raw = body_token or bearer_token(request)
    if not raw:
        raise HTTPException(status_code=401, detail="Administrator token required.")
    principal = decode_access_token(raw, settings.signing_secret)
    if principal is None:
        raise HTTPException(status_code=401, detail="Invalid or expired token.")
    if not principal.is_admin:
        raise HTTPException(status_code=403, detail="Administrator access required.")
    return principal


@admin_router.post(
    "/generate_codes",
    response_model=GenerateCodesResponse,
    summary="Add free access codes",
)
def generate_codes(
    body: GenerateCodesRequest,
    request: Request,
    settings: Settings = Depends(get_settings),
) -> GenerateCodesResponse:
    """Insert codes in the unactivated state, ready to be handed out.

    Idempotent on re-post: a code that already exists is counted in `skipped`
    and left exactly as it is, so re-running a batch cannot rearm a code that
    somebody is already using.
    """
    _admin_principal(request, body.token, settings)
    try:
        with session_scope() as session:
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

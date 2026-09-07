"""Request and response shapes for the auth and admin routes."""

from __future__ import annotations

import datetime as dt
from typing import Optional

from pydantic import BaseModel, Field


class RedeemRequest(BaseModel):
    code: str = Field(..., min_length=1, max_length=128)


class LoginRequest(BaseModel):
    username: str = Field(..., min_length=1, max_length=64)
    password: str = Field(..., min_length=1, max_length=256)


class UserInfo(BaseModel):
    username: str
    is_admin: bool


class TokenResponse(BaseModel):
    """What a browser gets after redeeming or signing in.

    The refresh token is *not* here: it goes back as an HttpOnly cookie, out of
    reach of any script on the page. The access token is meant to be held in
    memory only.
    """

    access_token: str
    token_type: str = "bearer"
    #: Seconds until the access token expires, not the session.
    expires_in: int
    #: When the session behind it ends — for a code, its 48-hour deadline.
    session_expires_at: dt.datetime
    user: UserInfo


class SessionInfo(BaseModel):
    """Answered for everyone, signed in or not, so the UI can decide what to
    gate without first provoking a 401."""

    #: False in local development, where nothing is gated at all.
    auth_required: bool
    authenticated: bool
    user: Optional[UserInfo] = None
    session_expires_at: Optional[dt.datetime] = None


class GenerateCodesRequest(BaseModel):
    #: An admin access token. May be omitted when an `Authorization: Bearer`
    #: header carries one instead.
    token: Optional[str] = None
    codes: list[str] = Field(..., min_length=1)


class GenerateCodesResponse(BaseModel):
    inserted: int
    #: Codes that already existed. Left untouched, never re-armed.
    skipped: int
    total_codes: int
    activated_codes: int
    #: Must never exceed `activated_codes`; the schema makes that impossible.
    live_anonymous_sessions: int

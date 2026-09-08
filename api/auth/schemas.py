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


class ChallengeRequest(BaseModel):
    #: Which admin endpoint the nonce is for. A nonce minted for one action is
    #: refused by the other, so a captured signature cannot be redirected.
    action: str = Field(..., min_length=1, max_length=32)


class ChallengeResponse(BaseModel):
    nonce: str
    expires_at: dt.datetime
    #: The namespace the signature must be made under — pass it to
    #: `ssh-keygen -Y sign -n <namespace>`.
    namespace: str


class SignedRequest(BaseModel):
    """Common half of every admin call: the nonce and the signature over it."""

    nonce: str = Field(..., min_length=1, max_length=64)
    #: An armored SSHSIG blob, as written by `ssh-keygen -Y sign`.
    signature: str = Field(..., min_length=1, max_length=8192)


class GenerateCodesRequest(SignedRequest):
    codes: list[str] = Field(..., min_length=1)


class CreateAdminRequest(SignedRequest):
    #: Always "admin". Accepted so the call is explicit about what it creates,
    #: and rejected if it is anything else rather than silently ignored.
    username: str = "admin"
    #: An empty string asks the server to generate a 32-character password.
    password: str = Field(..., max_length=256)


class CreateAdminResponse(BaseModel):
    username: str
    #: Shown exactly once. Nothing stores it in a readable form, so a lost
    #: password is replaced by calling the endpoint again, not recovered.
    password: str
    #: True when the server chose the password.
    generated: bool
    #: False on first creation, true when this call rotated an existing one.
    rotated: bool
    #: Sessions ended by the rotation.
    revoked_sessions: int


class GenerateCodesResponse(BaseModel):
    inserted: int
    #: Codes that already existed. Left untouched, never re-armed.
    skipped: int
    total_codes: int
    activated_codes: int
    #: Must never exceed `activated_codes`; the schema makes that impossible.
    live_anonymous_sessions: int

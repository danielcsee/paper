"""Authentication: users, sessions, and time-limited free access codes.

    POST /auth/redeem     code  -> access token + refresh cookie
    POST /auth/login      admin password login
    POST /auth/refresh    rotate, re-checking the code's 48-hour window
    POST /auth/logout
    GET  /auth/session    who is signed in (200 even when nobody is)
    POST /admin/generate_codes

Public interface: the two routers, and `require_user` for gating a router.
"""

from api.auth.dependencies import optional_principal, require_admin, require_user
from api.auth.routes import admin_router, router
from api.auth.tokens import Principal

__all__ = [
    "Principal",
    "admin_router",
    "optional_principal",
    "require_admin",
    "require_user",
    "router",
]

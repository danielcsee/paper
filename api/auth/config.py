"""Settings that only the API process may hold.

Split out of `api.app.config` deliberately. The Celery worker runs the
ingestion pipeline, which parses untrusted third-party documents from PubTator
— it is the more exposed of the two processes, and it has no business being
able to mint admin tokens. Keeping the signing key in its own settings class
means the worker's task definition simply never carries `JWT_SECRET`, and the
fail-closed check below cannot stop the worker from booting.

`SCITERM_ENV` is read here as well as in `api.app.config`. That is not
duplication of state: both read the same environment variable, so they cannot
disagree, and the alternative — importing the API's settings to learn the
environment — is the coupling this module exists to remove.

Construction is lazy, via `get_auth_settings()`. The worker imports
`api.auth` transitively (the client packages export their routers), so merely
importing this module must never demand a secret; only calling it does.
"""

from __future__ import annotations

import secrets
from functools import lru_cache
from typing import Any, Literal, Optional

from pathlib import Path

from pydantic_settings import BaseSettings, SettingsConfigDict

from api.app.config import REPO_ROOT


class AuthSettings(BaseSettings):
    """Signing key, token lifetimes, and the cookie policy."""

    model_config = SettingsConfigDict(
        env_file=REPO_ROOT / ".env", env_file_encoding="utf-8", extra="ignore"
    )

    #: Mirrors `Settings.sciterm_env`; see the module docstring.
    sciterm_env: Literal["local", "prod"] = "local"

    #: Signs access tokens. Required in prod; in local an ephemeral one is
    #: generated per process, so tokens simply do not survive a restart.
    jwt_secret: Optional[str] = None
    #: Access tokens are verified from their signature alone, so this is also
    #: how long a revoked session can keep working. Short on purpose.
    access_token_ttl_seconds: int = 900
    #: How long a *password* session lives, refreshed on use. Code-backed
    #: sessions ignore this: their deadline comes from the code.
    refresh_token_ttl_seconds: int = 60 * 60 * 24 * 14
    #: The free-access window, measured from a code's first redemption.
    free_code_window_hours: int = 48
    # --- admin endpoints ---
    #: Public keys allowed to call /admin/*, one `*.pub` per file. Committed to
    #: the repository on purpose: a public key is not a secret, so this needs no
    #: secret provisioning and rotation is a commit.
    authorized_keys_dir: Path = REPO_ROOT / "api" / "authorized_keys"
    #: The namespace a signature must be made under. Bound so a signature this
    #: key made for anything else -- signing git commits -- cannot be replayed.
    ssh_signature_namespace: str = "sciterm-admin"
    #: How long a challenge nonce stays spendable. Long enough to type a key
    #: passphrase, short enough that a captured nonce is worthless.
    admin_challenge_ttl_seconds: int = 120

    #: Whether the refresh cookie is marked Secure. Defaults to "yes in prod",
    #: which is right for any real deployment. Set false only to demo a prod
    #: build over plain HTTP: a Secure cookie is never sent over http://, so
    #: sessions would silently fail to survive a reload.
    cookie_secure: Optional[bool] = None

    def model_post_init(self, __context: Any) -> None:
        """Fail closed on a misconfigured prod, and stay effortless in local.

        A prod deployment without a signing secret would mint tokens anyone
        could forge, so the API refuses to start rather than serving something
        that looks protected and is not. Because this check lives here, it
        constrains the API alone — the worker never constructs this class.
        """
        if self.sciterm_env == "prod" and not self.jwt_secret:
            raise ValueError(
                "SCITERM_ENV=prod requires JWT_SECRET on the API process. "
                "Generate one with "
                "`python3 -c 'import secrets; print(secrets.token_urlsafe(48))'`. "
                "The Celery worker does not need it and should not be given it."
            )
        if not self.jwt_secret:
            self.jwt_secret = secrets.token_urlsafe(48)

    @property
    def auth_required(self) -> bool:
        """Whether the metered routes are gated. False in local development."""
        return self.sciterm_env == "prod"

    @property
    def refresh_cookie_secure(self) -> bool:
        """`cookie_secure` if set, otherwise on exactly when auth is on."""
        if self.cookie_secure is None:
            return self.auth_required
        return self.cookie_secure

    @property
    def signing_secret(self) -> str:
        """`jwt_secret`, narrowed to `str` — `model_post_init` guarantees one."""
        assert self.jwt_secret is not None
        return self.jwt_secret


@lru_cache
def get_auth_settings() -> AuthSettings:
    return AuthSettings()

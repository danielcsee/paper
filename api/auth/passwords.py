"""Password hashing, on the standard library.

PBKDF2-HMAC-SHA256 rather than scrypt: `hashlib.scrypt` exists only when CPython
was linked against an OpenSSL that offers it, and it is absent from at least one
Python we run on (the 3.9 in `.venv`). A hash the app cannot verify on some
machines is worse than a slightly less memory-hard one, and this protects a
single admin password, not a user table.

The stored form is self-describing — `pbkdf2_sha256$iterations$salt$hash`, both
halves base64 — so the cost can be raised, or a different scheme introduced,
without invalidating existing hashes.
"""

from __future__ import annotations

import base64
import hashlib
import hmac
import secrets

#: OWASP's 2023 floor for PBKDF2-HMAC-SHA256. ~200 ms on a laptop, and it runs
#: once per admin login rather than once per request.
_ITERATIONS = 600_000
_SALT_BYTES = 16
_KEY_LEN = 32
_SCHEME = "pbkdf2_sha256"


def _b64(raw: bytes) -> str:
    return base64.b64encode(raw).decode("ascii")


def hash_password(password: str) -> str:
    salt = secrets.token_bytes(_SALT_BYTES)
    key = hashlib.pbkdf2_hmac(
        "sha256", password.encode("utf-8"), salt, _ITERATIONS, dklen=_KEY_LEN
    )
    return f"{_SCHEME}${_ITERATIONS}${_b64(salt)}${_b64(key)}"


def verify_password(password: str, stored: str | None) -> bool:
    """Constant-time check. A null or malformed hash simply fails.

    Never raises: a corrupt row must read as "wrong password", not as a 500
    that tells an attacker they have found a real account.
    """
    if not stored:
        return False
    try:
        scheme, iterations, salt_b64, key_b64 = stored.split("$")
        if scheme != _SCHEME:
            return False
        salt = base64.b64decode(salt_b64)
        expected = base64.b64decode(key_b64)
        candidate = hashlib.pbkdf2_hmac(
            "sha256",
            password.encode("utf-8"),
            salt,
            int(iterations),
            dklen=len(expected),
        )
    except (ValueError, TypeError):
        return False
    return hmac.compare_digest(candidate, expected)

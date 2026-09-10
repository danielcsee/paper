"""`api.auth.passwords` — PBKDF2-HMAC-SHA256 on the standard library.

The stored form is self-describing so the cost can be raised later without
invalidating existing hashes, and verification never raises: a corrupt row must
read as "wrong password", not as a 500 that confirms the account exists.
"""

from __future__ import annotations

from typing import Optional

import pytest

from api.auth.passwords import hash_password, verify_password

_PASSWORD = "correct horse battery staple"


@pytest.fixture(scope="module")
def stored() -> str:
    """One hash for the module: PBKDF2 at 600k iterations costs ~200ms a call."""
    return hash_password(_PASSWORD)


def test_hash_is_self_describing(stored: str) -> None:
    """`pbkdf2_sha256$iterations$salt$hash`, both halves base64."""
    scheme, iterations, salt_b64, key_b64 = stored.split("$")

    assert scheme == "pbkdf2_sha256"
    assert int(iterations) >= 600_000
    assert salt_b64 and key_b64


def test_hash_is_salted(stored: str) -> None:
    """The same password must not produce the same stored value twice."""
    assert hash_password(_PASSWORD) != stored


def test_verify_accepts_the_correct_password(stored: str) -> None:
    assert verify_password(_PASSWORD, stored) is True


def test_verify_rejects_the_wrong_password(stored: str) -> None:
    assert verify_password("wrong password", stored) is False
    assert verify_password("", stored) is False


@pytest.mark.parametrize(
    "corrupt",
    [
        None,
        "",
        "not-a-hash",
        "pbkdf2_sha256$600000$only-three-fields",
        "pbkdf2_sha256$notanumber$c2FsdA==$a2V5",
        "scrypt$600000$c2FsdA==$a2V5",
        "pbkdf2_sha256$600000$!!!not-base64!!!$a2V5",
        "a$b$c$d$e",
    ],
)
def test_verify_treats_corrupt_storage_as_a_failed_login(
    corrupt: Optional[str],
) -> None:
    """Never raises — a malformed or null hash simply fails."""
    assert verify_password(_PASSWORD, corrupt) is False


def test_verify_password_contract(stored: str) -> None:
    """Every documented verification branch, in one test.

    The granular tests above isolate failures; this one exercises the whole
    contract in a single coverage context, which is how Testledger attributes a
    test to a function (its threshold is per-test, not unioned across the file).
    """
    cases: list[tuple[str, Optional[str], bool]] = [
        (_PASSWORD, stored, True),
        ("not the password", stored, False),
        (_PASSWORD, None, False),
        (_PASSWORD, "", False),
        (_PASSWORD, "not-a-hash", False),
        (_PASSWORD, "scrypt$600000$c2FsdA==$a2V5", False),
        (_PASSWORD, "pbkdf2_sha256$notanumber$c2FsdA==$a2V5", False),
    ]

    for password, hashed, expected in cases:
        assert verify_password(password, hashed) is expected, (
            f"verify_password({password!r}, {hashed!r}) should be {expected}"
        )

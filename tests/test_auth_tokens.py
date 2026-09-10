"""`api.auth.tokens` — the two token kinds.

An access token is trusted from its signature alone, so the rejection paths
matter more than the happy path: anything that fails must be indistinguishable
from no token at all.
"""

from __future__ import annotations

import datetime as dt

import jwt
import pytest

from api.auth.tokens import (
    Principal,
    decode_access_token,
    hash_refresh_token,
    issue_access_token,
    new_refresh_token,
)

_SECRET = "test-signing-secret"


@pytest.fixture
def principal() -> Principal:
    return Principal(user_id=7, username="danielcsee", is_admin=True, session_id=42)


def test_new_refresh_token_is_random_and_returns_its_digest() -> None:
    """256 bits from `secrets`; only the digest is ever persisted."""
    token_a, digest_a = new_refresh_token()
    token_b, _ = new_refresh_token()

    assert token_a != token_b
    assert digest_a == hash_refresh_token(token_a)
    # A database leak must yield no usable token.
    assert token_a not in digest_a


def test_hash_refresh_token_is_stable_sha256_hex() -> None:
    digest = hash_refresh_token("a-token")

    assert digest == hash_refresh_token("a-token")
    assert len(digest) == 64
    assert digest != hash_refresh_token("a-token ")


def test_access_token_round_trips_principal(principal: Principal) -> None:
    """Every field the app authorises on must survive the round trip."""
    token, expires = issue_access_token(principal, _SECRET, ttl_seconds=900)

    decoded = decode_access_token(token, _SECRET)

    assert decoded == principal
    assert expires.tzinfo is not None
    payload = jwt.decode(token, _SECRET, algorithms=["HS256"], issuer="sciterm")
    assert payload["exp"] == int(expires.timestamp())


def test_access_token_carries_null_session_for_local_dev_principal() -> None:
    """The local-dev principal has no session because it never authenticated."""
    anonymous = Principal(
        user_id=1, username="dev", is_admin=False, session_id=None
    )

    decoded = decode_access_token(
        issue_access_token(anonymous, _SECRET, ttl_seconds=60)[0], _SECRET
    )

    assert decoded == anonymous
    assert decoded is not None and decoded.session_id is None


def test_decode_rejects_wrong_secret(principal: Principal) -> None:
    token, _ = issue_access_token(principal, _SECRET, ttl_seconds=900)

    assert decode_access_token(token, "a-different-secret") is None


def test_decode_rejects_expired_token(principal: Principal) -> None:
    """A revoked session keeps working only until expiry, so expiry must bite."""
    token, expires = issue_access_token(principal, _SECRET, ttl_seconds=-10)

    assert expires < dt.datetime.now(dt.timezone.utc)
    assert decode_access_token(token, _SECRET) is None


def test_decode_rejects_foreign_issuer(principal: Principal) -> None:
    """A validly signed token minted for another service is still not ours."""
    now = dt.datetime.now(dt.timezone.utc)
    foreign = jwt.encode(
        {
            "iss": "not-sciterm",
            "sub": str(principal.user_id),
            "name": principal.username,
            "adm": principal.is_admin,
            "sid": principal.session_id,
            "iat": int(now.timestamp()),
            "exp": int((now + dt.timedelta(minutes=15)).timestamp()),
        },
        _SECRET,
        algorithm="HS256",
    )

    assert decode_access_token(foreign, _SECRET) is None


def test_decode_rejects_token_missing_required_claims() -> None:
    """A KeyError on a missing claim must read as "no token", not as a 500."""
    now = dt.datetime.now(dt.timezone.utc)
    incomplete = jwt.encode(
        {
            "iss": "sciterm",
            "sub": "7",
            "iat": int(now.timestamp()),
            "exp": int((now + dt.timedelta(minutes=15)).timestamp()),
        },
        _SECRET,
        algorithm="HS256",
    )

    assert decode_access_token(incomplete, _SECRET) is None


@pytest.mark.parametrize("token", ["", "not-a-jwt", "a.b.c", "..", "null"])
def test_decode_rejects_malformed_input_without_raising(token: str) -> None:
    assert decode_access_token(token, _SECRET) is None

"""Verifying SSH signatures against the keys in `api/authorized_keys/`.

The admin endpoints are authenticated by proving possession of a private key
whose public half is committed to this repository. A public key is not a
secret — GitHub already publishes yours at `github.com/<user>.keys` — so this
puts *nothing* sensitive in the repo, needs no secret provisioning in prod, and
makes rotation a commit.

The format is SSHSIG (OpenSSH's `PROTOCOL.sshsig`), so the client side is the
stock tool:

    ssh-keygen -Y sign -f ~/.ssh/id_ed25519 -n sciterm-admin <file>

which works with ssh-agent and passphrase-protected keys, unlike anything that
would have to read the private key itself.

Three things here are load-bearing, and all three are easy to leave out:

1. **The namespace is checked.** You sign git commits with the same key, under
   namespace "git". Without this check one of your own signed commits could be
   replayed as an admin login.
2. **The embedded public key is checked against the allowlist, and is the key
   the signature is verified with.** SSHSIG carries the signer's public key
   inside the blob; trusting it unchecked lets anyone sign their own way in.
3. **The signed message is a single-use nonce** — enforced by the caller, in
   `admin_challenges`. A signature over anything static is a password.
"""

from __future__ import annotations

import base64
import binascii
import hashlib
import logging
from pathlib import Path
from typing import Iterator, List, Tuple

from cryptography.exceptions import InvalidSignature, UnsupportedAlgorithm
from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PublicKey
from cryptography.hazmat.primitives.serialization import (
    Encoding,
    PublicFormat,
    load_ssh_public_key,
)

log = logging.getLogger(__name__)

_MAGIC = b"SSHSIG"
_SIG_VERSION = 1
_ARMOR_BEGIN = "-----BEGIN SSH SIGNATURE-----"
_ARMOR_END = "-----END SSH SIGNATURE-----"

#: Hash algorithms OpenSSH will name in a signature. It defaults to sha512.
_HASHES = {b"sha256": hashlib.sha256, b"sha512": hashlib.sha512}


class SignatureError(Exception):
    """The signature is absent, malformed, or not from an allowed key."""


class AllowedKey:
    """One committed public key, with the filename it came from for logging."""

    def __init__(self, key: Ed25519PublicKey, source: str, comment: str) -> None:
        self.key = key
        self.source = source
        self.comment = comment

    @property
    def raw(self) -> bytes:
        return self.key.public_bytes(Encoding.Raw, PublicFormat.Raw)

    def __repr__(self) -> str:  # pragma: no cover - logging aid
        return f"<AllowedKey {self.source} {self.comment}>"


# --- ssh wire format -------------------------------------------------------


def _read_string(blob: bytes, offset: int) -> Tuple[bytes, int]:
    """Read one length-prefixed field. SSH strings are uint32 length + bytes."""
    if offset + 4 > len(blob):
        raise SignatureError("truncated signature")
    length = int.from_bytes(blob[offset : offset + 4], "big")
    start = offset + 4
    end = start + length
    if end > len(blob):
        raise SignatureError("truncated signature")
    return blob[start:end], end


def _string(value: bytes) -> bytes:
    return len(value).to_bytes(4, "big") + value


# --- the allowlist ---------------------------------------------------------


def _iter_key_files(directory: Path) -> Iterator[Path]:
    if not directory.is_dir():
        return
    yield from sorted(directory.glob("*.pub"))


def load_allowed_keys(directory: Path) -> List[AllowedKey]:
    """Every `*.pub` in `directory`, one key per file.

    A directory rather than a single authorized_keys file: adding a key is
    adding a file and revoking one is deleting it, which reads clearly in a
    diff and cannot corrupt the other entries.

    Ed25519 only. That is what OpenSSH generates by default and what verifying
    here stays simple for; anything else raises rather than being skipped
    quietly, so a key that will never work says so at startup.
    """
    keys: List[AllowedKey] = []
    for path in _iter_key_files(directory):
        text = path.read_text().strip()
        if not text or text.startswith("#"):
            continue
        try:
            key = load_ssh_public_key(text.encode("utf-8"))
        except (ValueError, UnsupportedAlgorithm) as exc:
            raise SignatureError(f"{path.name}: unreadable public key ({exc})") from exc
        if not isinstance(key, Ed25519PublicKey):
            raise SignatureError(
                f"{path.name}: only ssh-ed25519 keys are supported here"
            )
        parts = text.split()
        keys.append(AllowedKey(key, path.name, parts[2] if len(parts) > 2 else ""))
    return keys


# --- verification ----------------------------------------------------------


def _unarmor(armored: str) -> bytes:
    lines = [line.strip() for line in armored.strip().splitlines()]
    if not lines or lines[0] != _ARMOR_BEGIN or lines[-1] != _ARMOR_END:
        raise SignatureError("not an SSH signature (missing BEGIN/END armor)")
    try:
        return base64.b64decode("".join(lines[1:-1]), validate=True)
    except (binascii.Error, ValueError) as exc:
        raise SignatureError("signature is not valid base64") from exc


def verify(
    message: bytes, armored_signature: str, allowed: List[AllowedKey], namespace: str
) -> AllowedKey:
    """Return the allowed key that signed `message`, or raise `SignatureError`.

    Every failure raises the same exception type with a short reason; callers
    turn that into one generic 401 so a prober cannot tell a bad namespace from
    an unknown key.
    """
    if not allowed:
        raise SignatureError("no authorized keys are configured")

    blob = _unarmor(armored_signature)
    if not blob.startswith(_MAGIC):
        raise SignatureError("wrong magic preamble")

    offset = len(_MAGIC)
    if offset + 4 > len(blob):
        raise SignatureError("truncated signature")
    version = int.from_bytes(blob[offset : offset + 4], "big")
    offset += 4
    if version != _SIG_VERSION:
        raise SignatureError(f"unsupported SSHSIG version {version}")

    publickey, offset = _read_string(blob, offset)
    sig_namespace, offset = _read_string(blob, offset)
    reserved, offset = _read_string(blob, offset)
    hash_name, offset = _read_string(blob, offset)
    signature, offset = _read_string(blob, offset)

    # (1) Namespace binding. Without this, a signature this key made for any
    # other purpose -- signing git commits, most likely -- authenticates here.
    if sig_namespace != namespace.encode("utf-8"):
        raise SignatureError(
            f"signature namespace {sig_namespace!r} is not {namespace!r}"
        )

    hasher = _HASHES.get(hash_name)
    if hasher is None:
        raise SignatureError(f"unsupported hash {hash_name!r}")

    # (2) The key inside the blob must be one of ours, and is then the key the
    # signature is checked against. Both halves matter: checking a key you do
    # not verify with, or verifying with a key you did not check, is no check.
    key_type, key_offset = _read_string(publickey, 0)
    if key_type != b"ssh-ed25519":
        raise SignatureError("signing key is not ssh-ed25519")
    raw_key, _ = _read_string(publickey, key_offset)
    match = next((k for k in allowed if k.raw == raw_key), None)
    if match is None:
        raise SignatureError("signing key is not in api/authorized_keys")

    sig_algorithm, sig_offset = _read_string(signature, 0)
    raw_signature, _ = _read_string(signature, sig_offset)
    if sig_algorithm != b"ssh-ed25519":
        raise SignatureError("signature algorithm is not ssh-ed25519")

    signed = (
        _MAGIC
        + _string(sig_namespace)
        + _string(reserved)
        + _string(hash_name)
        + _string(hasher(message).digest())
    )
    try:
        match.key.verify(raw_signature, signed)
    except InvalidSignature as exc:
        raise SignatureError("signature does not verify") from exc
    return match

# api/authorized_keys

Public keys allowed to call the `/admin/*` endpoints. One OpenSSH public key
per `*.pub` file; the filename is arbitrary and appears in the server log when
a key authorises a call.

**These are public keys, and committing them is the point.** A public key is
not a secret — GitHub already publishes yours at `github.com/<user>.keys` — so
this repository carries nothing sensitive, a fresh deployment needs no secret
provisioned into it to be administrable, and authorising or revoking a machine
is a commit rather than a server-side operation.

## Adding and removing

```bash
cp ~/.ssh/id_ed25519.pub api/authorized_keys/laptop.pub   # authorise
rm api/authorized_keys/laptop.pub                          # revoke
```

Then redeploy. Keys are read from disk on each admin request, so a restart
picks up changes without a rebuild of anything but the image.

**Never put a private key here.** A private key is the file *without* the
`.pub` extension; only the `.pub` half belongs in version control.

## Constraints

Ed25519 only. That is OpenSSH's default and keeps verification to one code
path; any other key type raises at load rather than being skipped silently, so
a key that would never work says so immediately instead of failing a call
later.

## How they are used

`api/auth/sshsig.py` verifies an SSHSIG signature over a single-use nonce
against these keys, under the namespace `sciterm-admin`. The namespace check
matters: without it, a signature made by the same key for another purpose —
signing git commits, most likely — would authenticate here. See
[`api/auth`](../auth) for the flow and `scripts/admin.sh` for the client.

# tests

Unit tests for the `api` package. Pure-logic only: no Postgres, no Neo4j, no
Redis, and no network. Anything needing a live service belongs in an
integration suite instead, and is recorded in Testledger as a skip disposition
rather than left as a silent gap.

## Layout

One file per module under test, named for it:

| File | Covers |
| --- | --- |
| `test_graph_keys.py` | `api/graph/keys.py` — node identity keys and label validation |
| `test_chunking.py` | `api/ingestion/chunking.py` — passage-to-chunk conversion and offsets |
| `test_auth_tokens.py` | `api/auth/tokens.py` — JWT access tokens and opaque refresh tokens |
| `test_auth_passwords.py` | `api/auth/passwords.py` — PBKDF2 hashing and verification |
| `test_pb_models.py` | `api/pb_client/models.py` — PubTator identifier normalisation |
| `test_eu_naming.py` | `api/eu_client/naming.py` — the no-op and early-return paths |
| `test_persist.py` | `api/ingestion/persist.py` — the pure provenance and date rules |
| `test_ncbi_http.py` | `api/ncbi/http.py` — `RateLimiter`, the in-process 3/s budget |
| `test_auth_throttle.py` | `api/auth/throttle.py` — the pre-authentication sliding window |

`conftest.py` puts the repository root on `sys.path`; the `api` package is run
from the checkout rather than installed.

## Dependencies

`pytest` and `coverage` (see `testledger/python-requirements.txt`), installed
into `.venv`. Tests are discovered and run through Testledger:

```sh
testledger/bin/testledger --config testledger/testledger.toml test --json
```

Running `pytest` directly works but records nothing in the ledger.

# internal/store

The SQLite ledger. WAL mode, immutable run identifiers, and normalized summaries
in the database beside content-hashed artifacts on disk — console output and raw
reports stay files; the database stores their paths, hashes and compact
summaries.

| File | Responsibility |
| --- | --- |
| `store.go` | The schema, connection, and open/close |
| `inventory.go` | Inventory snapshots and the lookups that compare them |
| `coverage.go` | Per-test observations, line/branch detail, coverage queries |
| `dispositions.go` | Version-scoped and durable skips |
| `proposals.go` | Proposals, decisions, and intended test links |
| `runs.go` | Test runs, case results, and artifacts |
| `failures.go` | Failure context and append-only agent diagnoses |
| `jobs.go` | Asynchronous job records |

Two invariants matter more than the rest. **The store holds no coverage
policy** — it reports observed percentages and lets `internal/app` decide what
clears the bar. **A diagnosis never overwrites evidence**: an agent's
interpretation of a failure is appended beside the runner's result, never over
it.

An intended test link resolves in one of three ways: `verified` when coverage
proved it, `dispositioned` when a human recorded a decision instead, and
`unresolved` while neither has happened. Only the last blocks a proposal.

## Dependencies

`internal/model`, and `modernc.org/sqlite` — a pure-Go driver, so the binary
builds without cgo and cross-compiles cleanly.

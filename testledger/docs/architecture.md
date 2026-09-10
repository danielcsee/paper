# Architecture

## Boundaries

`internal/app` is the application API. Both the CLI and MCP server call those
methods directly rather than shelling out to one another.
The store contains no agent decisions, and the Python adapter contains no
coverage policy.

Files in `internal/app` and `internal/store` are grouped by what they do, one
concern per file, each paired with a `_test.go` of the same name:

| File | Concern |
| --- | --- |
| `app.go` | the `App` type, its lifecycle, and configuration lookups |
| `adapter.go` | the `Discoverer` interface and the language registry |
| `inventory.go` | source to symbol inventory, and inventory comparison |
| `coverage.go` | which symbols count as gaps, and symbol context |
| `attribution.go` | native coverage reports to per-symbol evidence |
| `dispositions.go` | recorded decisions to leave a symbol untested |
| `proposals.go` | the propose / decide / implement lifecycle |
| `workflow.go` | `Status` and `NextActions` |
| `runner.go` | executing a language's native test runner |
| `results.go` | normalising each runner's result format |
| `jobs.go` | asynchronous runs and reading their results |
| `selection.go` | affected-test selection |
| `mutation.go` | optional mutation signals |
| `support.go` | identifiers, hashing, subprocess environment, paging |

A language adapter satisfies `app.Discoverer`. The interface is declared in the
consuming package and implementations are registered in one map, so adding a
language is a registry entry rather than another arm of a type switch.

```text
CLI / MCP stdio
       |
       v
application service ---- Python / Go / TypeScript adapters
       |
       v
     SQLite ---------- immutable artifacts
```

The Python pytest plugin and TypeScript discovery program are embedded into the
Go binary. Go discovery uses the standard library AST; TypeScript discovery uses
the project's compiler API. Runner commands use fixed argument arrays and
artifact placeholders, never shell evaluation.

## Deterministic lifecycle

1. Discover every configured function and method.
2. Hash its normalized AST, signature, and body.
3. persist a complete immutable inventory snapshot.
4. Compare symbol keys and semantic hashes with the previous snapshot.
5. Apply current, version-scoped dispositions.
6. Reuse only passing coverage observations for the exact semantic hash.
7. Report changed gaps separately while retaining all unresolved gaps.
8. Run pytest under coverage.py with a dynamic context for each test.
9. Persist normalized test results before attempting coverage attribution.
10. Intersect each test context's executed lines with each function's own AST
    lines and persist the observation.
11. Select affected tests from prior-version coverage, falling back to the full
    suite when any changed symbol lacks a safe mapping.
12. Persist missing line/branch details and optional mutation signals as
    deterministic evidence; agent interpretations remain separate.

SQLite uses WAL mode and immutable run IDs. Console output and raw reports are
content-hashed artifacts; normalized summaries and bounded excerpts live in the
database.

## MCP and proposal layer

Long-running tests are asynchronous because MCP hosts commonly impose tool
timeouts. The MCP surface exposes:

- `scan_changes`
- `select_affected_tests`
- `list_coverage_gaps`
- `get_symbol_context`
- `record_disposition`
- `start_test_run`
- `get_test_run`
- `get_failure_context`
- `record_failure_diagnosis`
- `get_next_actions`
- proposal submission, decisions, and implementation recording
- `run_mutation` for explicitly configured mutation adapters

Agents submit test intent, humans approve it, agents edit source, and a later
coverage run validates the intended test-to-symbol relationship. Proposal
targets are immutable semantic hashes, so a source change makes an unfulfilled
proposal obsolete. Agents never receive raw SQL access.

Only one MCP server should own a project database at a time. On startup it marks
jobs left queued or running by a prior process as interrupted. Within one
server, the configured semaphore enforces `max_parallel_runs`.

The server intentionally exposes stdio only. Phase 3 does not include
Streamable HTTP.

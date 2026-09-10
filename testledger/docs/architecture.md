# Architecture

## Boundaries

`internal/app` is the application API. Both the CLI and MCP server call those
methods directly rather than shelling out to one another.
The store contains no agent decisions, and the Python adapter contains no
coverage policy.

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

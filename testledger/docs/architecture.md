# Architecture

## Boundaries

`internal/ops` is the operation registry: every capability declared once, with
its input struct and its handler. The CLI generates flags from that struct and
the MCP server generates JSON Schema from the same struct, so neither transport
describes an argument of its own and a capability cannot exist on one surface
and be missing from the other.

`internal/app` is the application service beneath it. Both transports reach it
through the registry rather than shelling out to one another.

```text
   CLI                    MCP stdio
    └──────────┬───────────────┘
          internal/ops              one declaration per operation
               │
    application service ──── Python / Go / TypeScript adapters
               │
            SQLite ────────── immutable artifacts
```

Three boundaries are load-bearing. The store contains no agent decisions. An
adapter contains no coverage policy. And configuration, which defines what may
be scanned and what may be run, is reachable only from the command line.

Files in `internal/app` and `internal/store` are grouped one concern per file,
each paired with a `_test.go` of the same name; each package's README lists
them. A language adapter satisfies `app.Discoverer`, declared in the consuming
package and registered in one map, so adding a language is a registry entry
rather than another arm of a type switch.

The Python discovery program and pytest plugin, and the TypeScript discovery
program, are embedded into the Go binary. Go discovery uses the standard library
AST. Runner commands are fixed argument arrays with artifact placeholders, never
shell evaluation.

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

The tool surface is whatever `internal/ops` declares; [api.md](api.md) is the
reference. Two behaviours are genuinely transport-specific:

- **Runs are asynchronous over MCP**, because hosts commonly impose tool
  timeouts. `run_tests` returns a job id and the agent polls `get_test_run`.
- **Decision-recording is gated.** While `allow_agent_decisions` is false,
  `record_proposal_decision` and `record_disposition` are not advertised over
  MCP at all, and the human runs them from the command line.

Agents submit test intent, humans approve it, agents edit source, and a later
coverage run validates the intended test-to-symbol relationship. Proposal
targets are immutable semantic hashes, so a source change makes an unfulfilled
proposal obsolete. An intended link resolves as `verified`, `dispositioned` or
`unresolved`; only the last blocks a proposal, which is what keeps a proposal
containing a deliberate skip from stalling forever.

Agents never receive raw SQL access.

Only one MCP server should own a project database at a time. On startup it marks
jobs left queued or running by a prior process as interrupted. Within one
server, the configured semaphore enforces `max_parallel_runs`.

The server exposes stdio only. There is no Streamable HTTP transport.

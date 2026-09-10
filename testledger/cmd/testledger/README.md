# cmd/testledger

The single binary. Parses global flags, opens the application, and dispatches to
one operation from `internal/ops` — or serves those same operations as MCP tools
over stdio when the subcommand is `mcp`.

This directory holds only what is genuinely command-line: process exit codes
(a failing run's native code reaches CI), human-readable rendering of a
`ops.Result`, the `--reset-config` path, and generated help. There is no
per-command argument parsing here, because arguments are declared once in
`internal/ops` and read by both transports.

Two things stay deliberately CLI-only:

- **`mcp`** — you cannot launch the transport from inside it.
- **Configuration management** (`--reset-config`). `testledger.toml` defines
  what the agent may look at; an agent able to rewrite it could point coverage
  at an empty source set and make every gap disappear.

`decide` and `skip` are also effectively CLI-only by default: they record a
human decision, and `allow_agent_decisions` is false unless a project opts in.

## Dependencies

`internal/app` (open the service), `internal/ops` (the operation registry),
`internal/mcpserver` (the stdio transport), `internal/config` (reset and
resolve). No third-party packages.

## Build

```sh
go build -o ./bin/testledger ./cmd/testledger
```

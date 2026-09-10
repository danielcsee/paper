# internal/ops

The operation registry — the reason the CLI and the MCP server cannot drift
apart.

Each operation is declared once with a canonical name (its MCP tool name), a CLI
verb, an input struct, and a handler returning an `ops.Result`. Both transports
are generated from that single declaration:

- `schema.go` turns an input struct into an MCP JSON Schema.
- `args.go` turns the same struct into CLI arguments.
- `fields.go` is the shared reflection layer both read, flattening embedded
  structs so `Page` and `Confirm` appear as ordinary arguments.

Adding a capability in `registry.go` adds it to both surfaces at once. A parity
test in `ops_test.go` states that guarantee explicitly.

`args.go` deliberately does not use the standard `flag` package, which stops
parsing at the first non-flag argument — `decide ID --json` silently behaved as
if `--json` were absent. Knowing each field's type up front makes a correct pass
straightforward, so flag order no longer matters.

Two flags are universal: `--json` prints the structured value on any command,
and `--input <file>` supplies a whole argument set as JSON, which is how the CLI
expresses arguments too nested to be flags.

`Result` carries both shapes at once: `Value` is structured (CLI `--json`, MCP
`structuredContent`), `Summary` is one line of prose both surfaces show, and
`Detail` is human-only — an agent paying by the token gets the structured value
instead.

## Dependencies

`internal/app`, `internal/model`. Standard library only otherwise.

# internal/config

The TOML configuration, its embedded default, and the rules for finding one.

On first use the embedded `default.toml` is written to the project root. From
then on the file on disk is authoritative: an upgrade never merges into it or
silently changes it. `Reset` restores the embedded default while keeping the
previous file as `<config>.bak`.

Configuration defines what Testledger may look at and what it may run —
`include`/`exclude` globs, the test command, the coverage threshold. That makes
it the tool's trust anchor, which is why configuration management is reachable
only from the command line.

`allow_agent_decisions` is false by default. While it is off, the two
decision-recording operations are not advertised over MCP at all, and a human
records them from the terminal.

Defaults are applied on load rather than being required in the file: the
per-language interpreter, the runner for a known language name, and a
`minimum_line_percent` of 80.

## Files

- `config.go` — the types, loading, defaulting, path resolution and reset.
- `default.toml` — embedded into the binary via `go:embed`.

## Dependencies

`github.com/BurntSushi/toml`. Nothing else in the project, so every package can
depend on it.

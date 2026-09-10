# internal

Everything behind the binary. The dependency direction is one-way: transports
depend on the registry, the registry depends on the application service, the
service depends on the store and the adapters, and nothing depends back.

```text
cmd/testledger ── internal/mcpserver
        └────────────┬───────────────┘
              internal/ops          the operation registry
                   │
              internal/app          the application service
              ┌────┴─────┐
        internal/store   language adapters
```

## Subdirectories

| Package | Responsibility |
| --- | --- |
| `ops` | Every operation declared once, for the CLI and MCP alike |
| `app` | The application service: inventory, coverage, proposals, runs |
| `store` | The SQLite ledger and its schema |
| `model` | Wire types shared across every layer |
| `config` | The TOML configuration and its embedded default |
| `mcpserver` | The stdio MCP transport |
| `pythonadapter` | Python discovery and pytest integration |
| `goadapter` | Go discovery through `go/ast` |
| `typescriptadapter` | TypeScript discovery through the compiler API |
| `pathmatch` | The path-glob subset the adapters share |

Two boundaries are load-bearing and worth preserving: the store holds no agent
decisions, and an adapter holds no coverage policy.

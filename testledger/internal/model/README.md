# internal/model

The wire types, shared by every layer. A `Symbol`, a `Gap`, a `TestRunResult`,
a `TestProposal` and an `IntendedTestLink` mean the same thing in the store, the
application service, the CLI and the MCP server, because there is one definition
of each.

These structs are also the API contract. Their `json` tags are what an MCP
client and a `--json` CLI caller both receive, so renaming a field changes both
surfaces at once — which is the intent.

`Page[T]` is the generic envelope every list-shaped result uses: `items`,
`total`, and a `next_cursor` that is meaningful only when more remain.

The link resolution constants (`LinkVerified`, `LinkDispositioned`,
`LinkUnresolved`) live here rather than in the store, because both the store and
the workflow state machine reason about them.

## Dependencies

Standard library only. This package intentionally imports nothing else in the
project, so every other package can depend on it without creating a cycle.

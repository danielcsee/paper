# internal/typescriptadapter

TypeScript and TSX discovery through the project's own installed `typescript`
compiler API, driven by an embedded `discover.js` run under the configured Node
binary.

Using the project's compiler rather than a bundled parser means the adapter
understands whatever syntax that project's TypeScript version understands, and
never disagrees with the build about what a symbol is. The trade is a real
dependency: `typescript` must be installed in the project (`node_modules`), and
`node` must be on the path or named in the configuration.

Emits the same `model.Symbol` shape as every other adapter, so nothing
downstream knows which language a symbol came from.

Results come from Jest or Vitest JSON reporters and coverage from Istanbul JSON.
Istanbul reports aggregate line and branch totals per file, so TypeScript
symbols get gap detail without the exact test-to-function attribution Python
coverage contexts provide.

Sandboxed Node needs `HOME` in the configuration's
`execution.environment_allowlist` to reach its cache.

## Files

- `adapter.go` — locates Node, runs the embedded program, decodes NDJSON.
- `discover.js` — walks the compiler's AST and emits one record per function.

## Dependencies

`internal/config`, `internal/model`, `internal/pathmatch`. Externally: Node and
the project's installed `typescript`.

# internal/goadapter

Go function and method discovery through `go/ast` and `go/parser` from the
standard library — no subprocess and no embedded helper, because the toolchain
is already linked in.

Emits the same `model.Symbol` shape every adapter produces: qualified name
(receiver included for methods), line span, executable lines, and semantic
hashes computed from the parsed tree so formatting and comment edits do not
register as changes.

Test results come from `go test -json` and coverage from a coverprofile, both
normalized in `internal/app`. A coverprofile carries aggregate line coverage
only — there is no per-test context — so Go symbols get gap detail without the
exact test-to-function attribution Python coverage contexts provide.

When Go tooling runs in a sandbox, include `HOME` and `GOCACHE` in the
configuration's `execution.environment_allowlist`, or the toolchain cannot reach
its build cache.

## Dependencies

`internal/config`, `internal/model`, `internal/pathmatch`, and the standard
library's `go/ast`, `go/parser` and `go/token`.

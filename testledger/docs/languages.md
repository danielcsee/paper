# Languages

Discovery runs each language's own toolchain rather than reimplementing its
grammar, so a symbol's boundaries mean what that language says they mean.

| Language | Discovery | Results | Coverage | Per-test attribution |
| --- | --- | --- | --- | --- |
| Python | standard-library AST | `pytest` | coverage.py contexts | **yes** |
| Go | `go/ast` | `go test -json` | coverprofile | no |
| TypeScript | project's compiler API | Jest / Vitest JSON | Istanbul JSON | no |

Only Python's coverage contexts record which test executed which line, so only
Python produces exact test-to-function assignments. Go and TypeScript give
aggregate line and branch gaps — enough to report a gap, not enough to link a
named test to a symbol.

## Go

```toml
[[languages]]
name = "go"
include = ["**/*.go"]
exclude = ["vendor/**", "**/*_test.go"]
[languages.test]
runner = "go-test-json"
command = ["go", "test", "-json", "-coverprofile={coverage_file}", "./..."]
test_roots = ["."]
[languages.coverage]
format = "go-coverprofile"
minimum_line_percent = 80
```

## TypeScript

```toml
[[languages]]
name = "typescript"
node = "node"
working_directory = "ui"
include = ["ui/src/**/*.ts", "ui/src/**/*.tsx"]
exclude = ["**/*.d.ts", "**/*.test.ts", "**/*.test.tsx"]
[languages.test]
runner = "vitest-json"
command = ["npm", "test", "--", "--run", "--reporter=json", "--outputFile={results_file}"]
test_roots = ["ui/src"]
[languages.coverage]
format = "istanbul-json"
file = "coverage/coverage-final.json"
minimum_line_percent = 80
```

Requires `typescript` installed in the project and `node` available.

## Mutation signals

Optional, and separate from the deterministic pass/fail record: a surviving
mutant suggests weak assertions, it does not prove a test is missing.

```toml
[languages.mutation]
command = ["your-mutation-tool", "--json", "{results_file}"]
format = "testledger-json"
```

The command writes `{"mutants": [...]}` with `symbol_key`, `operator`, `status`,
and optional `test_key` and `detail`.

## Adding one

Implement `app.Discoverer` and add an entry to the registry in
`internal/app/adapter.go`. Nothing downstream knows which language a symbol
came from.

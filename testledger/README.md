# Testledger

Testledger is a deterministic function-level test inventory and runner, implemented as a Go binary backed by a sqlite datastore containing normalized results. The Go code contains embedded native-language adapters, allowing function discovery from source and integration with native-language test tools.

See [docs/architecture.md](docs/architecture.md) for component boundaries and
the stdio MCP transport.

## Current scope

- Python function and method discovery through the standard-library AST.
- Stable semantic hashes that ignore formatting and comments.
- Inventory-to-inventory change detection.
- `pytest` execution with structured results.
- Per-test coverage contexts through `coverage.py`.
- Deterministic test-to-function attribution.
- Version-scoped or durable skip dispositions.
- SQLite history and JSON output suitable for agents and CI.
- Version-pinned test proposals with explicit human decisions.
- Intended test links that become verified only after passing coverage.
- Asynchronous test jobs with paginated results.
- Append-only agent diagnoses that cannot overwrite runner evidence.
- A local MCP server over standard input/output.
- Affected-test selection from prior-version dynamic coverage.
- Missing line and branch evidence on function-level gaps.
- Go AST and TypeScript compiler-API discovery adapters.
- Structured `go test -json`, Jest, and Vitest result normalization.
- Optional normalized mutation-testing signals.
- Embedded defaults with explicit configuration backup and reset.

`pytest` and `coverage.py` must be installed in the Python environment selected
by `testledger.toml`:

```sh
python3 -m pip install -r testledger/python-requirements.txt
```

## Commands

Build the single binary, then run it from the project root:

```sh
(cd testledger && go build -o ./bin/testledger ./cmd/testledger)
testledger/bin/testledger check --json
testledger/bin/testledger test --json
testledger/bin/testledger status --json
testledger/bin/testledger next --json
testledger/bin/testledger skip --reason 'generated adapter' \
  'python:api/example.py:Example.method'
```

Use `--config` and `--root` before the command to override their defaults:

```sh
testledger/bin/testledger --config testledger/testledger.toml check
```

On first use Testledger creates `<project-root>/testledger.toml` from the TOML
embedded in the binary. An existing file is authoritative and is never merged
or silently changed by an upgrade. Restore the embedded defaults while keeping
the current file as `<config>.bak` with:

```sh
testledger/bin/testledger --reset-config
```

The default database and artifacts are stored under `.testledger/` at the
project root. Test stdout, stderr, raw pytest results, and coverage JSON remain
artifacts; SQLite stores their paths and hashes plus compact normalized data.
The `test` command returns the native failing test exit code (or `1` for an
infrastructure failure) after recording and printing its result.

## Phase 2 workflow

Submit a proposal as JSON, record the human's decision, and record the exact
pytest node IDs after the approved tests are written:

```sh
testledger/bin/testledger propose --json testledger/examples/proposal.json
testledger/bin/testledger proposals --status proposed --json
testledger/bin/testledger decide --decision approved --by "$USER" \
  --reason "Approved as proposed" PROPOSAL_ID
testledger/bin/testledger implemented \
  --links testledger/examples/implemented-links.json PROPOSAL_ID
```

Proposal targets store the function's semantic hash. Approval and
implementation automatically rescan the project and make the proposal
`obsolete` if a target changed. An implemented link becomes verified only when
the named passing test reaches the target at the configured coverage threshold.

## MCP server

Add the built binary as a project-local stdio MCP server. Use absolute paths in
the real configuration:

```toml
[mcp_servers.testledger]
command = "/absolute/repository/testledger/bin/testledger"
args = ["--root", "/absolute/repository", "mcp"]
cwd = "/absolute/repository"
required = true
tool_timeout_sec = 30

[mcp_servers.testledger.tools.record_proposal_decision]
approval_mode = "prompt"

[mcp_servers.testledger.tools.record_disposition]
approval_mode = "prompt"
```

The server advertises focused tools, structured results, read/write safety
annotations, and workflow instructions. Test execution is asynchronous so a
tool call returns a job ID without waiting for the native runner. `get_next_actions` is the
preferred entry point for agents.
The explicit prompt policy is the host-level human approval boundary; the
required `human_confirmed`, actor, and reason fields preserve that decision in
the ledger.

## Coverage assignment

A passing test is assigned to a function version when that test's dynamic
coverage context executed at least one executable line in the function and met
the configured `minimum_line_percent`. A changed function must be observed
again before coverage of its previous version counts.

Coverage is execution evidence, not proof that assertions are meaningful.
Optional mutation signals strengthen this policy while remaining separate from
the deterministic pass/fail record.

## Phase 3 adapters and selection

```sh
testledger/bin/testledger affected --json
testledger/bin/testledger test --affected --json
testledger/bin/testledger test --language go --json
testledger/bin/testledger mutation --language python --json
```

Affected tests come only from passing coverage observations for a changed
function's immediately previous semantic hash. New, changed, or deleted symbols
without a historical mapping force a full-suite fallback. Gap JSON now includes
exact missing lines and branches when the native coverage format provides them.

Go and TypeScript can be added with language blocks such as:

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

TypeScript discovery uses the project's installed `typescript` compiler API.
Jest/Vitest and Go coverage provide aggregate gap details; Python coverage
contexts additionally provide exact test-to-function assignments. Mutation
commands use `format = "testledger-json"`, write to `{results_file}`, and emit
`{"mutants":[...]}` entries with `symbol_key`, `operator`, `status`, and
optional `test_key` and `detail` fields.

When Go or Node tools need their normal user cache, include `HOME` (and relevant
cache variables such as `GOCACHE`) in `execution.environment_allowlist`.

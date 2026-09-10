# Testledger

A function-level test inventory and runner. It discovers every function in a
project, records which tests actually cover which of them, and keeps the whole
history in SQLite — so an agent can ask "what is broken?" and "what is untested?"
without reading a single line of raw test output.

One Go binary, no cgo. Python, Go and TypeScript.

## Why

Test loops burn context: most of a run's output is passing tests nobody needs to
read. Testledger keeps raw output on disk as artifacts and compact queryable
records in the database — failures without the passes, one traceback at a time,
gaps a page at a time.

## Install and run

```sh
python3 -m pip install -r testledger/python-requirements.txt
(cd testledger && go build -o ./bin/testledger ./cmd/testledger)

testledger/bin/testledger check          # what needs tests?
testledger/bin/testledger test           # run them
testledger/bin/testledger next           # what should I do now?
```

First use writes `testledger.toml` from the embedded default, which is then
authoritative and never silently changed.

## One API, two surfaces

Every capability is one operation with two spellings — a CLI subcommand and an
MCP tool — generated from a single declaration, so they cannot drift apart. Add
the binary as a stdio MCP server and an agent gets the same operations a human
gets, minus the two that record a human's decision.

## Documentation

| | |
| --- | --- |
| [api.md](docs/api.md) | Every operation, both spellings, and the trust boundary |
| [agent-workflow.md](docs/agent-workflow.md) | The loop an agent should follow |
| [coverage.md](docs/coverage.md) | What counts as covered, and why per-test |
| [configuration.md](docs/configuration.md) | `testledger.toml` reference |
| [languages.md](docs/languages.md) | Python, Go, TypeScript setup |
| [architecture.md](docs/architecture.md) | Component boundaries and lifecycle |

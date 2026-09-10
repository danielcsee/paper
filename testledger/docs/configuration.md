# Configuration

On first use Testledger writes the embedded default to `testledger.toml` at the
project root. From then on that file is authoritative: an upgrade never merges
into it or silently changes it.

```sh
testledger --reset-config     # restore defaults, keeping the old file as .bak
testledger --config path/to/testledger.toml check
```

Configuration is reachable only from the command line. It defines which files
are scanned and which command runs, so an agent able to rewrite it could point
coverage at an empty source set and make every gap disappear.

## Top level

| Key | Meaning |
| --- | --- |
| `schema_version` | Must be `1`. |
| `database` | SQLite path. Default `.testledger/testledger.db`. |
| `artifact_directory` | Where raw runner output is kept. Default `.testledger/artifacts`. |
| `allow_agent_decisions` | Let MCP record human decisions. Default `false`; see [api.md](api.md#the-trust-boundary). |

## A language block

```toml
[[languages]]
name = "python"
python = ".venv/bin/python"
include = ["api/**/*.py"]
exclude = ["api/db/migrations/**", "**/__pycache__/**"]

[languages.test]
runner = "pytest"
command = [".venv/bin/python", "-m", "pytest"]
test_roots = ["tests"]

[languages.coverage]
source = ["api"]
branch = true
minimum_line_percent = 80.0
```

`python` must name an interpreter that can import the code under test. A bare
`python3` usually resolves to the system interpreter, which will not have the
project's dependencies installed; point it at the virtualenv.

`include` and `exclude` use one glob dialect across every language: `**` crosses
directory boundaries, `*` and `?` stay within a segment, separators are always
slashes.

`minimum_line_percent` is judged **per test**, not across the suite. See
[coverage.md](coverage.md).

## Execution

```toml
[execution]
timeout_seconds = 600
max_output_bytes = 1048576
max_parallel_runs = 1
environment_allowlist = ["DATABASE_URL", "REDIS_URL", "PATH", "VIRTUAL_ENV"]
```

Subprocesses receive only the allow-listed variables. Go and Node tooling needs
`HOME` — and Go also `GOCACHE` — to reach its build cache.

Runner commands are fixed argument arrays, never shell-evaluated. Placeholders
`{artifact_dir}`, `{results_file}` and `{coverage_file}` are substituted before
execution.

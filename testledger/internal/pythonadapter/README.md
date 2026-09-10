# internal/pythonadapter

Python discovery and pytest integration. The richest adapter: Python's coverage
contexts are the only format that yields exact test-to-function attribution
rather than aggregate totals.

Both helper programs are embedded into the binary with `go:embed` and run with
the interpreter the configuration names. Using the project's own toolchain
rather than reimplementing its grammar means a symbol's boundaries mean what
Python says they mean — decorators, `async def`, nested classes and properties
all included.

## scripts/

- `discover.py` — walks the standard-library AST and emits one NDJSON record
  per function and method: qualified name, line span, executable lines, and
  semantic hashes that ignore formatting and comments.
- `testledger_pytest_plugin.py` — sets a coverage.py dynamic context per test,
  so each executed line is attributed to the test that ran it.

`discover.py` excludes every line above a function's first body statement from
its executable set. Decorators, the `def` line, a signature split across lines
and default expressions all run at import time, in coverage.py's empty context,
so they can never be attributed to a test — counting them once capped a one-line
`@property` at 50%, permanently below any useful threshold.

## Dependencies

Go side: `internal/config`, `internal/model`, `internal/pathmatch`. Python side:
`pytest` and `coverage`, installed in the interpreter the configuration selects
(see `python-requirements.txt`).

# API reference

Every capability is one operation with two spellings: a CLI subcommand and an
MCP tool. Both are generated from a single declaration in `internal/ops`, so
they take the same arguments, return the same structured result, and cannot
drift apart.

Reading as a human, use the command. Reading as an agent, use the MCP tool.
They are the same operation, and the table below is the whole surface.

## Conventions

- `--json` works on every command and prints the structured result — the exact
  payload the MCP tool returns as `structuredContent`. Without it the CLI prints
  a one-line summary and short human detail.
- `--input <file>` supplies a whole argument set as JSON. Use it for arguments
  too nested to be flags, such as proposal cases and implementation links.
- Flags accept either spelling: `--failures-only` and `--failures_only`. Flag
  order never matters, and a flag may follow a positional argument.
- Arguments marked **required** must be present on both surfaces.
- Anything that lists takes `--limit` (default 20, maximum 100) and `--cursor`.
  Responses carry `total` and `next_cursor`.
- A command whose first argument is marked *(positional)* also accepts it as a
  bare argument: `testledger symbol python:api/x.py:f`.

## The surface

| Command | MCP tool | Does |
| --- | --- | --- |
| `next` | `get_next_actions` | Return the smallest actionable workflow state. Call this first, and again after every decision or run |
| `status` | `get_status` | Summarize the inventory, outstanding gaps, active skips, and the latest run |
| `check` | `scan_changes` | Take a deterministic inventory snapshot and report which function versions still need coverage |
| `gaps` | `list_coverage_gaps` | Read uncovered function versions a page at a time, with missing line and branch evidence |
| `symbol` | `get_symbol_context` | Read one symbol, its bounded source span, and the tests that previously covered it |
| `propose` | `propose_tests` | Record proposed test cases against exact current function versions. This does not approve them |
| `proposals` | `list_proposals` | Read proposals with their targets, decisions, and link resolutions |
| `decide` | `record_proposal_decision` ¹ | Record a human's explicit approval or rejection of a proposal |
| `implemented` | `record_proposal_implementation` | Record the exact native test IDs written for an approved proposal. Re-recording replaces the previous links |
| `affected` | `select_affected_tests` | Choose tests for changed functions from the previous inventory's coverage, falling back to the full suite when unsafe |
| `test` | `run_tests` | Run a configured language's tests and record normalized results and coverage |
| `run` | `get_test_run` | Read a recorded run's normalized cases a page at a time, optionally failures only |
| `failure` | `get_failure_context` | Read one normalized failure and its latest diagnosis, without loading the whole run |
| `diagnose` | `record_failure_diagnosis` | Append an interpretation of a failure. Never overwrites the deterministic result |
| `skip` | `record_disposition` ¹ | Record a human-approved decision to leave a function untested |
| `mutation` | `run_mutation` | Run the configured mutation command and record normalized killed/survived signals |
| `mcp` | — | Serve these tools over stdio |
| `help` | — | List commands, or one command's arguments |

¹ Not advertised over MCP by default — see [the trust boundary](#the-trust-boundary).

## Operations

### `next` — `get_next_actions`

Return the smallest actionable workflow state. Call this first, and again after every decision or run.

| Argument | Type | Required | Description |
| --- | --- | --- | --- |
| `--limit` | `<n>` | — | Maximum actions to return; default 10, maximum 50. |


### `status` — `get_status`

Summarize the inventory, outstanding gaps, active skips, and the latest run.


### `check` — `scan_changes`

Take a deterministic inventory snapshot and report which function versions still need coverage.

| Argument | Type | Required | Description |
| --- | --- | --- | --- |
| `--all` | `Report` | — | every outstanding gap, not only those changed since the last inventory. |
| `--limit` | `<n>` | — | Maximum gaps to return; default 20, maximum 100. |


### `gaps` — `list_coverage_gaps`

Read uncovered function versions a page at a time, with missing line and branch evidence.

| Argument | Type | Required | Description |
| --- | --- | --- | --- |
| `--changed-only` | `Only` | — | functions changed since the previous inventory. |
| `--limit` | `<n>` | — | Page size; default 20, maximum 100. |
| `--cursor` | `<n>` | — | Offset cursor from the previous page. |


### `symbol` — `get_symbol_context`

Read one symbol, its bounded source span, and the tests that previously covered it.

| Argument | Type | Required | Description |
| --- | --- | --- | --- |
| `--symbol-key` | `<value>` | yes | Stable symbol key, as reported by a coverage gap. |
| `--include-source` | `Include` | — | the function's source span. |


### `propose` — `propose_tests`

Record proposed test cases against exact current function versions. This does not approve them.

| Argument | Type | Required | Description |
| --- | --- | --- | --- |
| `--targets` | `<value>…` | yes | Current symbol keys this proposal covers. |
| `--rationale` | `<value>` | yes | Why these cases are the right ones. |
| `--created-by` | `<value>` | yes | Agent or actor identifier. |
| `--cases` | `<file>` | yes | Proposed test cases. On the CLI, a JSON file. |


### `proposals` — `list_proposals`

Read proposals with their targets, decisions, and link resolutions.

| Argument | Type | Required | Description |
| --- | --- | --- | --- |
| `--proposal-id` | `<value>` | — | Read one proposal in full, including its link resolutions. |
| `--status` | `<proposed|approved|rejected|implemented|verified|obsolete>` | — | Filter the listing by status. |
| `--limit` | `<n>` | — | Page size; default 20, maximum 100. |
| `--cursor` | `<n>` | — | Offset cursor from the previous page. |


### `decide` — `record_proposal_decision`

Record a human's explicit approval or rejection of a proposal.

> **CLI only by default.** This records a human decision, so it is not
> advertised over MCP unless `allow_agent_decisions` is enabled. See
> [the trust boundary](#the-trust-boundary).

| Argument | Type | Required | Description |
| --- | --- | --- | --- |
| `--proposal-id` | `<value>` | yes | Proposal identifier. |
| `--decision` | `<approved|rejected>` | yes | The human's decision. |
| `--decided-by` | `<value>` | yes | Human actor identifier. |
| `--reason` | `<value>` | yes | The human's stated rationale. |
| `--human-confirmed` | `Set` | — | only after the human stated this exact decision. On the CLI the human typing the command is the confirmation. |


### `implemented` — `record_proposal_implementation`

Record the exact native test IDs written for an approved proposal. Re-recording replaces the previous links.

| Argument | Type | Required | Description |
| --- | --- | --- | --- |
| `--proposal-id` | `<value>` | yes | Approved proposal identifier. |
| `--links` | `<file>` | yes | Intended symbol-to-test links. On the CLI, a JSON file. |


### `affected` — `select_affected_tests`

Choose tests for changed functions from the previous inventory's coverage, falling back to the full suite when unsafe.

| Argument | Type | Required | Description |
| --- | --- | --- | --- |
| `--language` | `<value>` | — | Configured language name; required when more than one is configured. |


### `test` — `run_tests`

Run a configured language's tests and record normalized results and coverage.

| Argument | Type | Required | Description |
| --- | --- | --- | --- |
| `--language` | `<value>` | — | Configured language name; required when more than one is configured. |
| `--affected` | `Select` | — | tests from prior coverage; falls back to the full suite when unsafe. |
| `--async` | `Queue` | — | the run and return a job id instead of waiting. Always true over MCP, where hosts impose tool timeouts. |
| `--proposal-id` | `<value>` | — | Optional implemented proposal whose links this run should verify. |
| `--test-keys` | `<value>…` | — | Exact native test IDs or configured test paths. Runner flags are rejected. |


### `run` — `get_test_run`

Read a recorded run's normalized cases a page at a time, optionally failures only.

| Argument | Type | Required | Description |
| --- | --- | --- | --- |
| `--run-id` | `<value>` | — | Test run identifier. |
| `--job-id` | `<value>` | — | Asynchronous job identifier; poll this instead of run_id after an async run. |
| `--failures-only` | `Return` | — | only failed and errored cases. |
| `--limit` | `<n>` | — | Page size; default 20, maximum 100. |
| `--cursor` | `<n>` | — | Offset cursor from the previous page. |


### `failure` — `get_failure_context`

Read one normalized failure and its latest diagnosis, without loading the whole run.

| Argument | Type | Required | Description |
| --- | --- | --- | --- |
| `--run-id` | `<value>` | yes | Test run identifier. |
| `--test-key` | `<value>` | yes | Exact native test ID, from a failures page. |


### `diagnose` — `record_failure_diagnosis`

Append an interpretation of a failure. Never overwrites the deterministic result.

| Argument | Type | Required | Description |
| --- | --- | --- | --- |
| `--run-id` | `<value>` | yes | Test run identifier. |
| `--test-key` | `<value>` | yes | Exact failing native test ID. |
| `--category` | `<product_bug|incorrect_test_expectation|test_setup_defect|environment_problem|flaky|unknown>` | yes | Root-cause category. |
| `--explanation` | `<value>` | yes | Evidence-based reasoning for the category. |
| `--created-by` | `<value>` | — | Agent or actor identifier. |


### `skip` — `record_disposition`

Record a human-approved decision to leave a function untested.

> **CLI only by default.** This records a human decision, so it is not
> advertised over MCP unless `allow_agent_decisions` is enabled. See
> [the trust boundary](#the-trust-boundary).

| Argument | Type | Required | Description |
| --- | --- | --- | --- |
| `--symbol-key` | `<value>` | yes | Current symbol key. |
| `--reason` | `<value>` | yes | The human's stated rationale. |
| `--approved-by` | `<value>` | — | Human actor identifier; defaults to "human". |
| `--durable` | `Apply` | — | to future versions of this symbol too. Version-scoped is safer and is the default. |
| `--expires-at` | `<value>` | — | Optional RFC3339 expiry. |
| `--human-confirmed` | `Set` | — | only after the human stated this exact decision. On the CLI the human typing the command is the confirmation. |


### `mutation` — `run_mutation`

Run the configured mutation command and record normalized killed/survived signals.

| Argument | Type | Required | Description |
| --- | --- | --- | --- |
| `--language` | `<value>` | — | Configured language name; required when more than one is configured. |


## The trust boundary

Three things are reachable only from the command line, and each for the same
reason: the agent must not be able to grant itself permission.

| Not available over MCP | Why |
| --- | --- |
| `mcp` | You cannot launch the transport from inside it. |
| `--reset-config`, and configuration generally | `testledger.toml` defines which files are scanned and which command runs. An agent that can rewrite it can point coverage at an empty source set and make every gap disappear. |
| `decide`, `skip` | These record a *human's* decision. A decision tool the agent can call is a gate on the honour system. |

`decide` and `skip` are gated rather than removed. Set
`allow_agent_decisions = true` in `testledger.toml` to advertise them over MCP,
where they additionally require `human_confirmed: true`. Leave it false — the
default — and the human runs them in a terminal, where the process boundary
enforces what a prompt cannot.

## Synchronous and asynchronous runs

`run_tests` is one operation with two invocation modes. The CLI blocks by
default, because a human is watching and CI wants the exit code — a failing run
exits with the native runner's status. MCP always queues and returns a job id,
because hosts impose tool timeouts; poll `get_test_run` with that `job_id`.

`testledger test --async` gives the CLI the same queued behaviour when a script
wants a job id instead of a wait.

## Reading results without drowning in them

The tool exists to keep whole test logs out of an agent's context. Raw stdout,
stderr and native reports stay on disk as artifacts; the database holds compact
normalized records. The reading path is deliberately two-tier:

1. `run --failures-only --limit 10` lists failing cases with a one-line message.
2. `failure --run-id R --test-key K` loads one full traceback, only when needed.

`diagnose` appends your interpretation of a failure beside the runner's
evidence. It never overwrites it.

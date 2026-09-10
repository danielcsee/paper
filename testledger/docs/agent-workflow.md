# Agent workflow

The MCP server supplies a condensed form of this during initialization. Tool
names are given below; the equivalent CLI command for each is in
[api.md](api.md#the-surface).

**Always start with `get_next_actions`.** It returns the smallest actionable
state, and after every decision or run it tells you what changed. Retrieve
detailed records only for the identifiers it names.

## The loop

1. `scan_changes` after editing source.
2. `select_affected_tests`. Honour its full-suite fallback when `fallback` is
   true — it means a changed symbol has no safe historical mapping.
3. `list_coverage_gaps` in small pages. Read the missing line and branch
   evidence before deciding what a test should assert.
4. `get_symbol_context` only for the targets you are seriously considering.
5. `propose_tests` with focused cases against exact current symbol versions.
6. **Present the proposal to the human. Do not record a decision implicitly.**
7. The human records their decision with `testledger decide`. That command is
   not available to you unless the project enabled `allow_agent_decisions`, in
   which case pass `human_confirmed=true`, their identity and their reason.
8. Write only approved tests.
9. `record_proposal_implementation` with exact native test IDs. Re-recording
   replaces the previous links, so a mislinked test can be corrected.
10. `run_tests` (queued over MCP), then poll `get_test_run` with the `job_id`.
11. Read failures with `get_test_run` and `failures_only=true`, a page at a
    time. Load one full traceback with `get_failure_context` only for the
    failure you are working on. Append your reasoning with
    `record_failure_diagnosis`.
12. Fix and rerun.
13. Finish when the run passes and every intended link is resolved.

## Reading the states

`get_next_actions` returns one state. Two are easy to misread:

- **`tests_to_verify`** — an implemented proposal has not been judged by a run
  yet. Run the tests.
- **`links_unverified`** — a run *has* judged it and some links did not
  resolve. Running again will produce the identical answer. Either the named
  test does not reach the symbol at the required percentage (fix or relink it),
  or the symbol should carry a disposition instead. The action names the
  blocking symbol.

A link resolves as `verified` (coverage proved it), `dispositioned` (a human
decided instead), or `unresolved`. Only the last blocks a proposal.

## Two things not to do

**Do not infer deterministic evidence.** Scans, affected-test selection, runs,
branch gaps and mutation signals are measured. Read them; do not guess them.

**Do not treat a surviving mutant as proof a test is missing.** It suggests
weaker assertions than you would like, which is a reason to look, not a verdict.

`record_disposition` carries the same human-confirmation boundary as
`record_proposal_decision`. Prefer a version-scoped disposition; a durable one
must be explicitly requested by the human.

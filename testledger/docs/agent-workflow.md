# Agent workflow

The MCP server supplies these instructions during initialization. The first
operation should always be `get_next_actions`; retrieve detailed records only
for the returned identifiers.

1. Call `scan_changes` after source edits.
2. Call `select_affected_tests`; honor its full-suite fallback when true.
3. Read coverage gaps in small pages, including missing line/branch evidence.
4. Use `get_symbol_context` only for targets being considered.
5. Submit focused cases with `propose_tests`.
6. Present the proposal to the human. Do not record a decision implicitly.
7. After an explicit decision, call `record_proposal_decision` with
   `human_confirmed=true`, the human actor, and their reason.
8. Write only approved tests.
9. Call `record_proposal_implementation` with exact native test IDs.
10. Call `start_test_run`, then poll `get_async_test_run` using its job ID.
11. Read failures in pages with `failures_only=true`. Load only the selected
    failure with `get_failure_context`, then append the reasoned interpretation
    with `record_failure_diagnosis`.
12. Fix failures and rerun.
13. Use mutation signals when configured; surviving mutants suggest stronger
    assertions but are not automatic proof that a proposal is required.
14. Finish only when the run passes and intended links are coverage-verified.

`record_disposition` has the same human-confirmation boundary. Prefer a
version-scoped disposition; durable dispositions must be explicitly requested.

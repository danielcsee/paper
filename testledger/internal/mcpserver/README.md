# internal/mcpserver

The stdio MCP transport. It speaks JSON-RPC over standard input and output and
exposes the operations in `internal/ops` as tools.

The server describes no arguments of its own. A tool's `inputSchema` is
generated from its operation's input struct, so the schema an agent reads and
the struct the server decodes into cannot disagree.

Three behaviours are genuinely MCP-specific and live here:

- **Runs are always asynchronous.** Hosts impose tool timeouts, so
  `run_tests` returns a job id and the agent polls `get_test_run`.
- **The decision gate.** While `allow_agent_decisions` is false,
  `record_proposal_decision` and `record_disposition` are not advertised at all,
  and calling one returns an error naming the CLI command the human should run.
  A tool an agent cannot successfully call is worse than one it cannot see.
- **Startup recovery.** Jobs left queued or running by a previous process are
  marked interrupted, because only one server should own a project database at
  a time.

Responses carry both shapes: `structuredContent` for the agent to act on and a
one-line text block for a human reading the transcript. Human-only detail is
dropped, since an agent paying by the token wants the structured value.

Transport is stdio only, and deliberately so — there is no HTTP listener.

## Dependencies

`internal/app`, `internal/ops`, `internal/model`. No third-party packages; the
JSON-RPC framing is a few dozen lines of standard library.

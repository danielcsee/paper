# .agents

Codex configuration, generated from the Claude Code configuration by
`scripts/claude-to-codex.py`. **Do not edit these files by hand** -- edit `CLAUDE.md` or
`.claude/` and re-run the script:

```bash
scripts/claude-to-codex.py
```

Codex discovers `.agents/skills/*/SKILL.md` automatically for any session
started inside this repo, the same way it reads `AGENTS.md`. Verify what it
actually sees with:

```bash
codex debug prompt-input | grep -i llm-graph-practice-claude
```

`.claude-sync.json` records every generated file so re-runs stay idempotent, files you
wrote yourself are never overwritten, and `--prune` can clean up skills whose
Claude counterpart was deleted.

The Claude configuration remains the source of truth; nothing here replaces it.

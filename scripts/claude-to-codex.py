#!/usr/bin/env python3
"""Generate Codex configuration from this repo's Claude Code configuration.

Reads  CLAUDE.md (root and nested) and .claude/
Writes AGENTS.md (root and nested) and .agents/

The Claude side is never modified, moved, or deleted. Everything this script
writes is tracked in a manifest so re-runs are idempotent and hand-written
files are never clobbered.

    scripts/claude-to-codex.py --dry-run     # show the plan
    scripts/claude-to-codex.py               # write it
    scripts/claude-to-codex.py --prune       # also delete stale generated files

Verify the result with:  codex debug prompt-input | grep -i skill
"""

from __future__ import annotations

import argparse
import hashlib
import json
import re
import shutil
import sys
from pathlib import Path
from typing import Any, Optional

TOOL = "scripts/claude-to-codex.py"
MANIFEST_NAME = ".claude-sync.json"
BEGIN = "<!-- BEGIN generated from {src} by " + TOOL + " -- edits inside this block are overwritten -->"
END = "<!-- END generated from {src} -->"
GEN_HEADER = "<!-- Generated from {src} by " + TOOL + ". Do not edit; edit the source and re-run. -->"

# Frontmatter keys Codex's skill loader understands. Anything else is dropped
# from the frontmatter and, where it carries meaning, restated in the body.
CODEX_SKILL_KEYS = ("name", "description", "license", "version", "metadata", "allowed-tools")


# --------------------------------------------------------------------------
# Minimal YAML frontmatter reader/writer.
#
# Deliberately dependency-free: the system python3 has no PyYAML and this
# script has to run before anyone sets up a venv. It handles the shapes that
# actually occur in skill/agent/command frontmatter -- scalars, quoted
# scalars, block scalars, flat lists, and one level of nested mapping.
# --------------------------------------------------------------------------

FRONTMATTER_RE = re.compile(r"\A---[ \t]*\r?\n(.*?)\r?\n---[ \t]*(?:\r?\n|\Z)", re.S)


def _scalar(raw: str) -> str:
    raw = raw.strip()
    if len(raw) >= 2 and raw[0] == raw[-1] and raw[0] in "\"'":
        inner = raw[1:-1]
        return inner.replace('\\"', '"') if raw[0] == '"' else inner
    return raw


def parse_frontmatter(text: str) -> tuple[dict[str, Any], str]:
    """Return (frontmatter, body). Missing frontmatter yields ({}, text)."""
    match = FRONTMATTER_RE.match(text)
    if not match:
        return {}, text
    body = text[match.end():]
    lines = match.group(1).splitlines()
    data: dict[str, Any] = {}
    i = 0
    while i < len(lines):
        line = lines[i]
        i += 1
        if not line.strip() or line.lstrip().startswith("#"):
            continue
        if line[:1] in (" ", "\t"):  # stray indent with no parent; ignore
            continue
        key, _, rest = line.partition(":")
        key = key.strip()
        rest = rest.strip()

        if rest in ("|", ">", "|-", ">-"):  # block scalar
            block: list[str] = []
            while i < len(lines) and (not lines[i].strip() or lines[i][:1] in (" ", "\t")):
                block.append(lines[i].strip())
                i += 1
            joiner = "\n" if rest.startswith("|") else " "
            data[key] = joiner.join(block).strip()
            continue

        if rest:
            data[key] = _scalar(rest)
            continue

        # Bare "key:" -- a nested mapping or a list follows.
        child_lines: list[str] = []
        while i < len(lines) and (not lines[i].strip() or lines[i][:1] in (" ", "\t")):
            if lines[i].strip():
                child_lines.append(lines[i].strip())
            i += 1
        if child_lines and all(c.startswith("- ") for c in child_lines):
            data[key] = [_scalar(c[2:]) for c in child_lines]
        elif child_lines:
            child: dict[str, Any] = {}
            for c in child_lines:
                ck, _, cv = c.partition(":")
                child[ck.strip()] = _scalar(cv)
            data[key] = child
        else:
            data[key] = ""
    return data, body


def _emit_scalar(value: str) -> str:
    value = str(value)
    if value == "" or value[0] in "\"'{[&*?|>%@`!#" or value[-1:] == " " or ": " in value or "\n" in value:
        return '"' + value.replace("\\", "\\\\").replace('"', '\\"').replace("\n", " ") + '"'
    return value


def dump_frontmatter(data: dict[str, Any]) -> str:
    out = ["---"]
    for key, value in data.items():
        if isinstance(value, dict):
            out.append(f"{key}:")
            out.extend(f"  {k}: {_emit_scalar(v)}" for k, v in value.items())
        elif isinstance(value, list):
            out.append(f"{key}:")
            out.extend(f"  - {_emit_scalar(v)}" for v in value)
        else:
            out.append(f"{key}: {_emit_scalar(value)}")
    out.append("---")
    return "\n".join(out) + "\n"


# --------------------------------------------------------------------------
# Writer: every write goes through here so nothing escapes the manifest.
# --------------------------------------------------------------------------


class Writer:
    def __init__(self, root: Path, dry_run: bool, force: bool) -> None:
        self.root = root
        self.dry_run = dry_run
        self.force = force
        self.manifest_path = root / ".agents" / MANIFEST_NAME
        self.previous: dict[str, str] = {}
        if self.manifest_path.exists():
            try:
                self.previous = json.loads(self.manifest_path.read_text()).get("files", {})
            except (ValueError, OSError):
                self.previous = {}
        self.current: dict[str, str] = {}
        self.written: list[str] = []
        self.skipped: list[str] = []
        self.notes: list[str] = []

    def rel(self, path: Path) -> str:
        return str(path.relative_to(self.root))

    def write(self, path: Path, content: str) -> bool:
        """Write a fully generated file. Refuses to clobber unmanaged files."""
        rel = self.rel(path)
        if path.exists() and rel not in self.previous and not self.force:
            self.skipped.append(f"{rel} (exists and was not generated by this script; use --force)")
            return False
        digest = hashlib.sha256(content.encode()).hexdigest()
        self.current[rel] = digest
        if path.exists() and hashlib.sha256(path.read_bytes()).hexdigest() == digest:
            return True  # unchanged; still manifested
        self.written.append(rel)
        if not self.dry_run:
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(content)
        return True

    def copy(self, src: Path, dest: Path) -> None:
        """Copy a skill support file (scripts/, references/, assets/) verbatim."""
        rel = self.rel(dest)
        if dest.exists() and rel not in self.previous and not self.force:
            self.skipped.append(f"{rel} (exists and was not generated by this script; use --force)")
            return
        content = src.read_bytes()
        digest = hashlib.sha256(content).hexdigest()
        self.current[rel] = digest
        if dest.exists() and hashlib.sha256(dest.read_bytes()).hexdigest() == digest:
            return
        self.written.append(rel)
        if not self.dry_run:
            dest.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(src, dest)
            shutil.copymode(src, dest)

    def write_block(self, path: Path, source_label: str, block_body: str) -> None:
        """Insert generated content into a managed block, preserving the rest."""
        begin = BEGIN.format(src=source_label)
        end = END.format(src=source_label)
        block = f"{begin}\n\n{block_body.strip()}\n\n{end}\n"
        existing = path.read_text() if path.exists() else ""
        pattern = re.compile(
            re.escape(begin) + r".*?" + re.escape(end) + r"\n?", re.S
        )
        if pattern.search(existing):
            content = pattern.sub(lambda _m: block, existing, count=1)
        elif existing.strip():
            content = existing.rstrip() + "\n\n" + block
            self.notes.append(
                f"{self.rel(path)} already existed; the converted content was appended "
                "below your own text rather than replacing it."
            )
        else:
            content = block

        rel = self.rel(path)
        digest = hashlib.sha256(content.encode()).hexdigest()
        self.current[rel] = digest
        if path.exists() and hashlib.sha256(path.read_bytes()).hexdigest() == digest:
            return
        self.written.append(rel)
        if not self.dry_run:
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(content)

    def prune(self) -> list[str]:
        """Delete files we generated on an earlier run that are no longer produced."""
        removed = []
        for rel, digest in sorted(self.previous.items()):
            if rel in self.current:
                continue
            path = self.root / rel
            if not path.exists():
                continue
            if hashlib.sha256(path.read_bytes()).hexdigest() != digest:
                self.skipped.append(f"{rel} (stale, but edited by hand; not deleted)")
                continue
            removed.append(rel)
            if not self.dry_run:
                path.unlink()
                for parent in path.parents:
                    if parent == self.root or any(parent.iterdir()):
                        break
                    parent.rmdir()
        return removed

    def save_manifest(self) -> None:
        payload = {
            "generated_by": TOOL,
            "note": "Tracks files generated from the Claude Code config. Do not edit.",
            "files": dict(sorted(self.current.items())),
        }
        rel = self.rel(self.manifest_path)
        self.current.pop(rel, None)
        if not self.dry_run:
            self.manifest_path.parent.mkdir(parents=True, exist_ok=True)
            self.manifest_path.write_text(json.dumps(payload, indent=2) + "\n")


# --------------------------------------------------------------------------
# Converters
# --------------------------------------------------------------------------


def convert_memory(root: Path, writer: Writer) -> int:
    """CLAUDE.md -> AGENTS.md, at the root and in every subdirectory.

    Both files have the same semantics: directory-scoped instructions, with
    the more deeply nested file winning. So the content carries over as-is.
    """
    count = 0
    for claude_md in sorted(root.rglob("CLAUDE.md")):
        if any(part in {".git", "node_modules", ".venv", ".agents"} for part in claude_md.parts):
            continue
        text = claude_md.read_text()
        label = str(claude_md.relative_to(root))

        imports = re.findall(r"^@([^\s]+)\s*$", text, re.M)
        if imports:
            writer.notes.append(
                f"{label} uses Claude's @import syntax ({', '.join(imports)}). "
                "Codex does not expand imports; the lines were rewritten as instructions to read those files."
            )
            text = re.sub(
                r"^@([^\s]+)\s*$",
                lambda m: f"Read `{m.group(1)}` and follow it.",
                text,
                flags=re.M,
            )

        writer.write_block(claude_md.parent / "AGENTS.md", label, text)
        count += 1
    return count


def _skill_frontmatter(source: dict[str, Any], name: str, description: str) -> dict[str, Any]:
    out: dict[str, Any] = {"name": name, "description": description}
    for key in CODEX_SKILL_KEYS:
        if key in ("name", "description"):
            continue
        if key in source:
            out[key] = source[key]
    return out


def _explicit_only(dest_dir: Path, writer: Writer, display: str, short: str) -> None:
    """Mark a skill as explicit-invocation-only via agents/openai.yaml."""
    yaml = (
        "interface:\n"
        f'  display_name: "{display}"\n'
        f'  short_description: "{short}"\n'
        "policy:\n"
        "  allow_implicit_invocation: false\n"
    )
    writer.write(dest_dir / "agents" / "openai.yaml", yaml)


def convert_skills(root: Path, skills_dir: Path, writer: Writer) -> int:
    """.claude/skills/<name>/ -> <skills_dir>/<name>/

    The two formats are nearly identical -- a directory with SKILL.md plus
    optional scripts/, references/ and assets/. Only the frontmatter needs
    normalising.
    """
    src_root = root / ".claude" / "skills"
    if not src_root.is_dir():
        return 0
    count = 0
    for skill_md in sorted(src_root.glob("*/SKILL.md")):
        src_dir = skill_md.parent
        name = src_dir.name
        fm, body = parse_frontmatter(skill_md.read_text())
        label = str(skill_md.relative_to(root))

        description = fm.get("description") or f"Project skill {name}, imported from Claude Code."
        out_fm = _skill_frontmatter(fm, fm.get("name") or name, description)
        dest_dir = skills_dir / name

        preface = ""
        if fm.get("model"):
            writer.notes.append(
                f"{label} pins model '{fm['model']}'. Codex selects the model per session, "
                "so the pin was dropped."
            )
        if "tools" in fm:
            preface += (
                f"\n> Under Claude Code this skill was restricted to these tools: {fm['tools']}. "
                "Codex does not enforce per-skill tool restrictions; treat it as guidance.\n"
            )

        content = (
            dump_frontmatter(out_fm)
            + "\n"
            + GEN_HEADER.format(src=label)
            + "\n"
            + preface
            + "\n"
            + body.lstrip("\n")
        )
        if not writer.write(dest_dir / "SKILL.md", content):
            continue

        if str(fm.get("disable-model-invocation", "")).lower() == "true":
            _explicit_only(dest_dir, writer, name, description[:100])

        for extra in sorted(src_dir.rglob("*")):
            if extra.is_dir() or extra.name == "SKILL.md":
                continue
            writer.copy(extra, dest_dir / extra.relative_to(src_dir))
        count += 1
    return count


def convert_agents(root: Path, skills_dir: Path, writer: Writer) -> int:
    """.claude/agents/<name>.md -> <skills_dir>/<name>/SKILL.md

    Codex has no per-repo subagent definition file. A subagent is a named,
    described body of instructions selected by description, which is exactly
    what a skill is -- so each one becomes a skill that tells Codex it may be
    run in a sub-agent.
    """
    src_root = root / ".claude" / "agents"
    if not src_root.is_dir():
        return 0
    count = 0
    for agent_md in sorted(src_root.rglob("*.md")):
        fm, body = parse_frontmatter(agent_md.read_text())
        rel = agent_md.relative_to(src_root)
        name = fm.get("name") or "-".join(rel.with_suffix("").parts)
        label = str(agent_md.relative_to(root))
        description = fm.get("description") or f"Subagent {name}, imported from Claude Code."

        preface = [
            "> Imported from the Claude Code subagent "
            f"`{name}`. It ran in its own context window with its own instructions; "
            "run it as a focused task, delegating to a sub-agent when the work is "
            "large enough to justify one.",
        ]
        if "tools" in fm:
            preface.append(
                f"> The subagent was limited to these tools: {fm['tools']}. "
                "Codex does not enforce per-agent tool restrictions; treat it as guidance."
            )
        if fm.get("model"):
            writer.notes.append(
                f"{label} pins model '{fm['model']}'. Codex selects the model per session, "
                "so the pin was dropped."
            )

        content = (
            dump_frontmatter(_skill_frontmatter(fm, name, description))
            + "\n"
            + GEN_HEADER.format(src=label)
            + "\n\n"
            + "\n".join(preface)
            + "\n\n"
            + body.lstrip("\n")
        )
        if writer.write(skills_dir / name / "SKILL.md", content):
            count += 1
    return count


def convert_commands(root: Path, skills_dir: Path, writer: Writer) -> int:
    """.claude/commands/**/*.md -> <skills_dir>/<name>/SKILL.md

    Codex has no `/custom-command` surface; a user asks for the command by
    name and Codex routes to the skill, so each command becomes an
    explicit-invocation skill. Claude's template syntax has no Codex
    equivalent and is rewritten as instructions.
    """
    src_root = root / ".claude" / "commands"
    if not src_root.is_dir():
        return 0
    count = 0
    for cmd_md in sorted(src_root.rglob("*.md")):
        fm, body = parse_frontmatter(cmd_md.read_text())
        rel = cmd_md.relative_to(src_root)
        name = "-".join(rel.with_suffix("").parts)
        label = str(cmd_md.relative_to(root))
        slash = "/" + ":".join(rel.with_suffix("").parts)

        first_line = next((ln.strip() for ln in body.splitlines() if ln.strip()), "")
        description = fm.get("description") or first_line[:200] or f"Run the {name} command"
        if description[-1:] not in ".!?":
            description += "."
        description = f"{description} Use when the user asks for the {slash} command or names it directly."

        preface = [f"> Imported from the Claude Code slash command `{slash}`."]

        if "$ARGUMENTS" in body or re.search(r"\$[1-9]", body):
            hint = fm.get("argument-hint")
            preface.append(
                "> `$ARGUMENTS` and `$1`..`$9` below stand for what the user typed after the command"
                + (f" (expected: {hint})" if hint else "")
                + ". Substitute their words; ask for them if they are missing."
            )
        shell_calls = re.findall(r"!`([^`]+)`", body)
        if shell_calls:
            preface.append(
                "> Claude Code pre-ran the ``!`cmd` `` markers below and pasted their output. "
                "Codex does not: run each one yourself first, then use its output where the marker appears."
            )
        file_refs = re.findall(r"(?<![\w`])@([\w./-]+\.[\w]+)", body)
        if file_refs:
            preface.append(
                "> `@path` references below mean: read that file before continuing."
            )
        if "allowed-tools" in fm:
            preface.append(f"> Claude restricted this command to: {fm['allowed-tools']}.")

        content = (
            dump_frontmatter(_skill_frontmatter(fm, name, description))
            + "\n"
            + GEN_HEADER.format(src=label)
            + "\n\n"
            + "\n".join(preface)
            + "\n\n"
            + body.lstrip("\n")
        )
        if not writer.write(skills_dir / name / "SKILL.md", content):
            continue
        _explicit_only(skills_dir / name, writer, name, f"Imported from {slash}")
        count += 1
    if count:
        writer.notes.append(
            f"{count} slash command(s) became explicit-invocation skills, so Codex will not "
            "reach for them on its own -- ask for one by name, or mention it as $<skill-name>."
        )
    return count


def convert_mcp(root: Path, writer: Writer) -> int:
    """.mcp.json -> a config.toml fragment.

    Codex reads MCP servers from ~/.codex/config.toml, not from a file in the
    repo, and this script does not write outside the project. So the servers
    are emitted as a fragment for the user to paste (or `codex mcp add`).
    """
    mcp_json = root / ".mcp.json"
    if not mcp_json.exists():
        return 0
    try:
        servers = json.loads(mcp_json.read_text()).get("mcpServers", {})
    except ValueError as exc:
        writer.notes.append(f".mcp.json could not be parsed ({exc}); MCP servers were not converted.")
        return 0
    if not servers:
        return 0

    def toml_value(value: Any) -> str:
        if isinstance(value, bool):
            return "true" if value else "false"
        if isinstance(value, (int, float)):
            return str(value)
        if isinstance(value, list):
            return "[" + ", ".join(toml_value(v) for v in value) + "]"
        return json.dumps(str(value))

    lines = [
        "# Generated from .mcp.json by " + TOOL + ".",
        "# Codex reads MCP servers from ~/.codex/config.toml, which is outside this",
        "# project, so this file is not loaded automatically. Append it yourself:",
        "#",
        "#     cat .agents/codex-mcp-servers.toml >> ~/.codex/config.toml",
        "#",
        "# Review the entries first -- they become available in every Codex session,",
        "# not just this project.",
        "",
    ]
    for name, cfg in sorted(servers.items()):
        lines.append(f"[mcp_servers.{name}]")
        env = cfg.pop("env", None) if isinstance(cfg, dict) else None
        for key, value in sorted((cfg or {}).items()):
            if key in ("command", "args", "url", "startup_timeout_sec"):
                lines.append(f"{key} = {toml_value(value)}")
        if env:
            lines.append(f"\n[mcp_servers.{name}.env]")
            lines.extend(f"{k} = {toml_value(v)}" for k, v in sorted(env.items()))
        lines.append("")
    writer.write(root / ".agents" / "codex-mcp-servers.toml", "\n".join(lines))
    writer.notes.append(
        f"{len(servers)} MCP server(s) from .mcp.json were written to "
        ".agents/codex-mcp-servers.toml. Codex will not load them until you append that "
        "file to ~/.codex/config.toml."
    )
    return len(servers)


def inspect_settings(root: Path, writer: Writer) -> None:
    """Report on .claude/settings*.json, which has no faithful Codex mapping."""
    for name in ("settings.json", "settings.local.json"):
        path = root / ".claude" / name
        if not path.exists():
            continue
        try:
            data = json.loads(path.read_text())
        except ValueError:
            writer.notes.append(f".claude/{name} is not valid JSON; skipped.")
            continue
        if data.get("permissions"):
            writer.notes.append(
                f".claude/{name} defines tool permissions. Codex uses its own sandbox and "
                "approval policy (`/permissions`, or sandbox settings in ~/.codex/config.toml) "
                "and the two models do not map one-to-one, so these were NOT converted."
            )
        if data.get("hooks"):
            writer.notes.append(
                f".claude/{name} defines hooks. Codex supports hooks under [hooks] in "
                "~/.codex/config.toml with similar events (PreToolUse, PostToolUse, SessionStart), "
                "but the payload differs. These were NOT converted -- port them by hand."
            )
        if data.get("env"):
            writer.notes.append(
                f".claude/{name} sets environment variables. Port them to "
                "shell_environment_policy in ~/.codex/config.toml if Codex needs them."
            )


README = """# .agents

Codex configuration, generated from the Claude Code configuration by
`{tool}`. **Do not edit these files by hand** -- edit `CLAUDE.md` or
`.claude/` and re-run the script:

```bash
{tool}
```

Codex discovers `.agents/skills/*/SKILL.md` automatically for any session
started inside this repo, the same way it reads `AGENTS.md`. Verify what it
actually sees with:

```bash
codex debug prompt-input | grep -i {name}
```

`{manifest}` records every generated file so re-runs stay idempotent, files you
wrote yourself are never overwritten, and `--prune` can clean up skills whose
Claude counterpart was deleted.

The Claude configuration remains the source of truth; nothing here replaces it.
"""


def main() -> int:
    parser = argparse.ArgumentParser(
        description="Generate Codex config (AGENTS.md, .agents/) from Claude config (CLAUDE.md, .claude/).",
    )
    parser.add_argument("--root", default=".", type=Path, help="project root (default: cwd)")
    parser.add_argument(
        "--skills-dir",
        default=".agents/skills",
        help="where to write skills; .codex/skills also works (default: .agents/skills)",
    )
    parser.add_argument("--dry-run", action="store_true", help="show what would change, write nothing")
    parser.add_argument("--force", action="store_true", help="overwrite files this script did not generate")
    parser.add_argument("--prune", action="store_true", help="delete generated files whose source is gone")
    args = parser.parse_args()

    root = args.root.resolve()
    if not (root / "CLAUDE.md").exists() and not (root / ".claude").is_dir():
        print(f"error: no CLAUDE.md or .claude/ under {root}", file=sys.stderr)
        return 1

    skills_dir = root / args.skills_dir
    writer = Writer(root, args.dry_run, args.force)

    counts = {
        "AGENTS.md files": convert_memory(root, writer),
        "skills": convert_skills(root, skills_dir, writer),
        "subagents": convert_agents(root, skills_dir, writer),
        "commands": convert_commands(root, skills_dir, writer),
        "MCP servers": convert_mcp(root, writer),
    }
    inspect_settings(root, writer)
    writer.write(
        root / ".agents" / "README.md",
        README.format(tool=TOOL, manifest=MANIFEST_NAME, name=root.name),
    )

    removed = writer.prune() if args.prune else []
    stale = [r for r in writer.previous if r not in writer.current and (root / r).exists()]

    print(f"{'Planned' if args.dry_run else 'Wrote'} Codex config under {root}\n")
    for label, count in counts.items():
        if count:
            print(f"  {count:>3} {label}")
    print()
    for rel in writer.written:
        print(f"  {'would write' if args.dry_run else 'wrote'}  {rel}")
    if not writer.written:
        print("  (everything already up to date)")
    for rel in removed:
        print(f"  {'would delete' if args.dry_run else 'deleted'}  {rel}")
    if stale and not args.prune:
        print(f"\n  {len(stale)} generated file(s) no longer have a Claude source; re-run with --prune to remove them.")
    if writer.skipped:
        print("\nSkipped:")
        for item in writer.skipped:
            print(f"  - {item}")
    if writer.notes:
        print("\nNotes (things Codex handles differently):")
        for note in writer.notes:
            print(f"  - {note}")

    writer.save_manifest()
    if not args.dry_run:
        print("\nVerify with:  codex debug prompt-input | grep -i skill")
    return 0


if __name__ == "__main__":
    sys.exit(main())

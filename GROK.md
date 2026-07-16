# LittleJohnnyMnemonic — Cognitive Memory System

This Obsidian vault implements a human-modeled memory system for LLM agents. **Grok Build** uses the same protocol as Claude Code; this file is the Grok entry point.

**Authoritative spec:** [`CLAUDE.md`](CLAUDE.md) — read and follow it exactly.

## Grok Integration

| Component | Location |
|---|---|
| Lifecycle hooks | `~/.grok/hooks/ljm.json` (install via `grok/install.sh` or `grok/install.ps1`) |
| Hook template | `grok/hooks/ljm.template.json` (shared; installers expand per platform) |
| Memory skills | `~/.grok/skills/memory-*` |
| Background agents | `~/.grok/agents/memory-*.md` |
| Hook runner | `grok/bin/run-hook` (Unix/Git Bash) / `grok/bin/run-hook.ps1` + `run-hook.cmd` (Windows) |
| Agent binary | `agent/jm` (Unix) / `agent/jm.exe` (Windows) |
| Global rules | `~/.grok/GROK.md` |

LJM hooks are **global only** — do not add a live `ljm.json` under `.grok/hooks/` (double injection + platform breakage).

### Install

```bash
# Linux / macOS
./grok/install.sh
# builds agent/jm if missing, installs hooks/skills/agents
```

```powershell
# Windows
.\grok\install.ps1
```

### Install health (hooks self-check)

SessionStart examines whether the live install matches this platform and vault. On problems it emits `<ljm-install-warning>` (wrong-platform runner, unsubstituted template, project-scoped `ljm.json`, missing binary, vault mismatch, etc.).

| Action | Command |
|---|---|
| Inspect | `jm install check` |
| Fix (after user permission) | `jm install fix` |
| Suppress codes | `jm install ignore <code>…` / `--all` / `--clear` |

**Protocol:** When `<ljm-install-warning>` appears, notify the user and **ask permission** before `jm install fix`. If they want to stop the nag, use `jm install ignore` (state in `~/.grok/ljm-install-ignore.json`). Never silently rewrite hooks.

### Hook Events

- **SessionStart** — fixed-blend orientation (profiles, episodic, projects, machines); install-health check; repo trust
- **UserPromptSubmit** — association retrieval; emits `<retrieval-session id="..."/>` for adaptive-edge citations; daydream nudge on substantive prompts
- **PreToolUse** — repo trust write-blocking for untrusted repos
- **Stop** — auto-harvests `Memory/` citations from assistant output; behavioral rule logging; backup/consolidation triggers

### Subagents

Use `spawn_subagent` with `subagent_type: memory-daydream` or `memory-associator`. Daydream volleys: `background: true`, `capability_mode: all`, `cwd` = vault root. Associator: `capability_mode: execute`. Verify types in `/config-agents`. See `.grok/agents/` and `grok/config.toml.example`.

### Daydream Reflex

On substantive turns (prompt ≥200 chars OR ≥5 memories retrieved via hook), launch a daydream volley:

- At least one seeded from the current topic
- At least one random walk from an unrelated graph corner

The `UserPromptSubmit` hook emits `<daydream-nudge>` when density threshold is crossed — treat it as a commitment signal.

### Citation Drill (adaptive-edge pilot)

Citation harvest records only when the assistant turn contains explicit `Memory/...` paths that were in the retrieval session's loaded set. Natural integration without paths produces zero `Metrics/citations.json` / `edge_usage.jsonl` signal.

**Grok timing:** Grok does not pass `transcriptPath` on Stop, and Stop fires before assistant text is flushed to `updates.jsonl`. Citation harvest and **volley-commitment fulfillment** therefore run at the **next** `UserPromptSubmit` (resolving `~/.grok/sessions/*/<session-id>/updates.jsonl` or `chat_history.jsonl` via `GROK_SESSION_ID`), using the prior turn's transcript (including tool_call events). Stop-hook harvest/fulfillment remains for Claude Code and as a fallback when Grok passes an explicit transcript path.

On substantive turns where hook-loaded memories informed the response:

1. Note `<retrieval-session id="..."/>` from the hook output.
2. Include **1–3** explicit source paths in the assistant reply — bare (`Memory/Project/job_move_2026`) or wiki (`[[Memory/Feedback/geo_constraint_no_california_no_relocation]]`). Paths must match keys from that session's loaded set.
3. Weave content naturally; a brief inline parenthetical or "Grounded in:" line is enough — not announcement theater.

Manual fallback: `jm associate --cite "<key>,<context>,true" --session <id>`.

**Pilot state (2026-06-22):** operational observe phase. 14 operator-vetted
`learned` pairs seeded via `jm learn-edges apply-bootstrap`; citation harvest
is the `edge_usage` write path. Weights are moving (8 non-default as of
post-bootstrap `jm status`). **Still gated:** scope widen beyond `learned`;
automated `learn-edges` (PMI/distinct-session filter); `jm benchmark` proof of
retrieval lift. Citation drill remains load-bearing — organic stop-harvested
paths across real sessions are the evidence base, not bootstrap alone.

## Post-Change Documentation

Documentation is part of the work, never skipped post-change — see
[`CLAUDE.md` § Post-Change Documentation](CLAUDE.md) for the authoritative
seven-item checklist. The only judgment call is *which* records a change
touches, never *whether* to update them. Specifically for Grok sessions:

- **Mirror protocol changes here.** `GROK.md` is item #2's Grok target. Any
  behavioral-rule or tooling change that a Grok session must see at a glance —
  or any Grok-specific integration detail (hook events, citation drill, pilot
  state) — is updated in this file as well as `CLAUDE.md`.
- **Update the memory layer, not just the docs.** Semantic/project memories
  that *theorize about a subsystem you changed* (checklist #6) and any memory
  naming a *future change, planned fix, known bug, or "not-yet-implemented"
  status* for what you changed (checklist #7) must be reconciled to the new
  reality. A confident stale memory reads as knowledge, not as an old note —
  that's the drift this rule exists to stop.

## Quick Reference

| Action | When |
|---|---|
| **Write to Buffer/** | During conversation, when you observe something worth remembering |
| **Read from Memory/** | At conversation start (hooks handle this), or when context requires recall |
| **Consolidate** | At compaction, when Buffer/ exceeds 20 entries, or when explicitly asked |
| **Document post-change** | Always — protocol docs (both files), affected memories, stale forward-pointers. See above. |
| **Never** | Write directly to Memory/ during conversation (always buffer first) |

See [`CLAUDE.md`](CLAUDE.md) for the complete protocol.
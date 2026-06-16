# LittleJohnnyMnemonic — Cognitive Memory System

This Obsidian vault implements a human-modeled memory system for LLM agents. **Grok Build** uses the same protocol as Claude Code; this file is the Grok entry point.

**Authoritative spec:** [`CLAUDE.md`](CLAUDE.md) — read and follow it exactly.

## Grok Integration

| Component | Location |
|---|---|
| Lifecycle hooks | `~/.grok/hooks/ljm.json` (install via `grok/install.ps1`) |
| Memory skills | `~/.grok/skills/memory-*` |
| Background agents | `~/.grok/agents/memory-*.md` |
| Hook runner | `grok/bin/run-hook.ps1` |
| Global rules | `~/.grok/GROK.md` |

### Install

```powershell
.\grok\install.ps1
```

### Hook Events

- **SessionStart** — fixed-blend orientation (profiles, episodic, projects, machines)
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

Manual bootstrap when needed: `jm associate --cite "<key>,<context>,true" --session <id>`.

PR3 (learned-edge PMI) stays gated until stop-harvested citations accumulate across real sessions.

## Quick Reference

| Action | When |
|---|---|
| **Write to Buffer/** | During conversation, when you observe something worth remembering |
| **Read from Memory/** | At conversation start (hooks handle this), or when context requires recall |
| **Consolidate** | At compaction, when Buffer/ exceeds 20 entries, or when explicitly asked |
| **Never** | Write directly to Memory/ during conversation (always buffer first) |

See [`CLAUDE.md`](CLAUDE.md) for the complete protocol.
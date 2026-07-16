# LittleJohnnyMnemonic — Default Memory System

This host uses **LittleJohnnyMnemonic (LJM)** as the default memory substrate for all Grok Build sessions. LJM is wired globally via hooks in `~/.grok/hooks/ljm.json` — it is **not** project-scoped; it operates in every working directory. Do not add a live project-level `ljm.json` under `<vault>/.grok/hooks/` (installers own the global file only).

## Vault Location

Set by install (`JM_VAULT_ROOT` in hooks). Override with `JM_VAULT_ROOT` in the environment.

- `Buffer/` — short-term memory; write new observations here during conversation
- `Memory/` — long-term memory (`User/`, `Feedback/`, `Project/`, `Reference/`, `Semantic/`, `Episodic/`, `Knowledge/`)
- `Archive/` — decayed memories (recoverable)
- `CLAUDE.md` at vault root — **authoritative protocol** for writing, retrieving, consolidating, and associating

## Operating Protocol

Follow the vault's `CLAUDE.md` from any working directory on this host. Grok-specific equivalents:

| Claude Code | Grok Build |
|---|---|
| `Task` subagent | `spawn_subagent` |
| `Bash` | `run_terminal_command` |
| `Write` / `Edit` | `search_replace` |
| `Read` | `read_file` |
| `Grep` | `grep` |
| `memory-daydream` agent type | `memory-daydream` in `~/.grok/agents/` or `.grok/agents/` — verify via `/config-agents` |

Hooks inject memory context automatically:

- **SessionStart** — profile, recent sessions, active projects, machine registry
- **UserPromptSubmit** — topical association retrieval + daydream nudge on dense prompts
- **PreToolUse** — repo trust blocking for untrusted instruction files
- **Stop** — behavioral rule measurement, backup/consolidation triggers

## User Context at Runtime

Context is injected by LJM hooks — do not rely on static copies of this file for current vault state. If a fact is worth remembering, buffer it per the vault protocol.

## Skills and Agents

Installed to `~/.grok/skills/` and `~/.grok/agents/` by `grok/install.sh` (Linux/macOS) or `grok/install.ps1` (Windows):

- `/memory-associate` — run `jm associate` against current context
- `/memory-consolidate` — run buffer consolidation
- `/memory-buffer` — inspect or write buffer entries
- `/memory-daydream` — launch daydream volley (`spawn_subagent`, `capability_mode: all`, `background: true`)
- `/memory-associator` — background association (`capability_mode: execute` for `jm`)

**Daydream volley spawn** (on `<daydream-nudge>` or `/memory-daydream`): at least one topic seed + one random walk, both via `subagent_type: memory-daydream`, `cwd` set to vault root.

**Binary:** `agent/jm` (Unix) or `agent/jm.exe` (Windows). Vault-root `jm` / `jm.exe` may symlink to the agent binary.

Optional: merge `~/.grok/ljm-config.snippet.toml` into `config.toml` after install for model routing.

Reinstall after vault moves: `./grok/install.sh --vault-root <path>` or `.\grok\install.ps1 -VaultRoot <path>`.

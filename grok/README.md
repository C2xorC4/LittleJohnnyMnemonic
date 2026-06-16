# Grok Build Integration

Native Grok scaffolding for LittleJohnnyMnemonic — hooks, skills, and agents that mirror the Claude Code integration.

## Install

From the vault root:

```powershell
.\grok\install.ps1
```

Options:

```powershell
.\grok\install.ps1 -VaultRoot "D:\Repos\LLM\LittleJohnnyMnemonic"
.\grok\install.ps1 -Uninstall
```

Installs to `~/.grok/`:

| Target | Source |
|---|---|
| `hooks/ljm.json` | `.grok/hooks/ljm.json` (vault path substituted) |
| `skills/memory-*` | `.grok/skills/` |
| `agents/memory-*.md` | `.grok/agents/` |
| `GROK.md` | `grok/global/GROK.md` (global rules for all projects) |

## Hook Events

| Event | Handler | Purpose |
|---|---|---|
| `SessionStart` | `jm hook session-start` | Profile, episodic, projects, machines, trust check |
| `UserPromptSubmit` | `jm hook user-prompt-submit` | Association retrieval, `<retrieval-session>` ID, daydream nudge |
| `PreToolUse` | `jm hook pre-tool-use` | Repo trust write-blocking |
| `Stop` | `jm hook stop` | Citation harvest, behavioral rules, backup, consolidation |

Hooks use `grok/bin/run-hook.ps1` which resolves `JM_VAULT_ROOT` automatically.

## Skills

| Skill | Slash command |
|---|---|
| `memory-associate` | `/memory-associate` |
| `memory-consolidate` | `/memory-consolidate` |
| `memory-buffer` | `/memory-buffer` |
| `memory-daydream` | `/memory-daydream` |
| `memory-associator` | `/memory-associator` |

## Agents

First-class subagent types — defined in `.grok/agents/*.md` (project) and installed to `~/.grok/agents/`:

| Type | Capability | Purpose |
|---|---|---|
| `memory-daydream` | `all` | Graph exploration + `jm.exe` + breadcrumb writes |
| `memory-associator` | `execute` | `jm associate` + read-only evaluation |

Spawn pattern (daydream volley):

```
spawn_subagent(
  subagent_type: "memory-daydream",
  description: "Daydream — topic seed",
  prompt: "Memory Daydream Agent — seed from: <context>",
  background: true,
  capability_mode: "all",
  cwd: "<vault root>"
)
```

Verify registration: `/config-agents`. Use `/memory-daydream` skill for the full volley protocol.

## Optional config

Merge `grok/config.toml.example` into `~/.grok/config.toml` for model routing and subagent toggles. Install copies a reference snippet to `~/.grok/ljm-config.snippet.toml`.

## Claude Compatibility

Grok also scans `~/.claude/settings.json` for hooks by default. The native `.grok/hooks/ljm.json` uses Grok tool matchers (`search_replace`, `run_terminal_command`) and is preferred when Claude compat is disabled.

## Verify

```powershell
# Hook smoke test (vault-root jm.exe symlinks to agent/jm.exe)
'{"sessionId":"test","workspaceRoot":"D:\repos\llm\littlejohnnymnemonic"}' |
  & "D:\repos\llm\littlejohnnymnemonic\jm.exe" hook session-start

# List installed skills (if grok CLI available)
grok inspect
```

Restart Grok sessions or press `r` in `/hooks` after install.
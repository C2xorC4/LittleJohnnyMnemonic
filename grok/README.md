# Grok Build Integration

Native Grok scaffolding for LittleJohnnyMnemonic — hooks, skills, and agents that mirror the Claude Code integration.

## Install

From the vault root:

**Linux / macOS:**

```bash
./grok/install.sh
./grok/install.sh --vault-root /path/to/LittleJohnnyMnemonic
./grok/install.sh --uninstall
```

**Windows:**

```powershell
.\grok\install.ps1
.\grok\install.ps1 -VaultRoot "D:\Repos\LLM\LittleJohnnyMnemonic"
.\grok\install.ps1 -Uninstall
```

Installs to `~/.grok/`:

| Target | Source |
|---|---|
| `hooks/ljm.json` | Expanded from `grok/hooks/ljm.template.json` with platform runner + vault path |
| `skills/memory-*` | `.grok/skills/` |
| `agents/memory-*.md` | `.grok/agents/` |
| `GROK.md` | `grok/global/GROK.md` (global rules for all projects) |

LJM is **global, not project-scoped**. `.grok/hooks/` deliberately has no live `ljm.json` so Grok does not dual-load project + global hooks.

## Agent binary

```bash
cd agent && go build -o jm .      # Linux/macOS → agent/jm
cd agent && go build -o jm.exe . # Windows     → agent/jm.exe
```

Vault-root `jm` / `jm.exe` may symlink to `agent/jm` / `agent/jm.exe` (no copy step). Both are gitignored rebuildables.

## Hook Events

| Event | Handler | Purpose |
|---|---|---|
| `SessionStart` | `jm hook session-start` | Profile, episodic, projects, machines, trust check |
| `UserPromptSubmit` | `jm hook user-prompt-submit` | Association retrieval, `<retrieval-session>` ID, daydream nudge |
| `PreToolUse` | `jm hook pre-tool-use` | Repo trust write-blocking |
| `Stop` | `jm hook stop` | Citation harvest, behavioral rules, backup, consolidation |

Hooks use platform-aware runners that resolve `JM_VAULT_ROOT` and the native `jm` / `jm.exe` binary:

| Platform | Entry | Implementation |
|---|---|---|
| Linux / macOS | `grok/bin/run-hook` | bash; prefers `jm` / `agent/jm` |
| Git Bash / MSYS | `grok/bin/run-hook` | bash; prefers `jm.exe` |
| Native Windows | `grok/bin/run-hook.cmd` → `run-hook.ps1` | PowerShell; prefers `jm.exe` |

`run-hook.sh` remains a thin back-compat wrapper that execs `run-hook`.

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
| `memory-daydream` | `all` | Graph exploration + `jm` + breadcrumb writes |
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

Grok also scans `~/.claude/settings.json` for hooks by default. The native `~/.grok/hooks/ljm.json` uses Grok tool matchers (`search_replace`, `run_terminal_command`) and is preferred when Claude compat is disabled.

## Install health

SessionStart runs `jm install check` logic and emits `<ljm-install-warning>` when the live setup is wrong for this platform (PowerShell runner on Linux, unsubstituted `__HOOK_RUNNER__` / `__JM_VAULT_ROOT__`, project-level `ljm.json`, missing native `jm`, vault path mismatch, etc.).

```bash
jm install check                 # report issues (exit 2 if any)
jm install fix                   # rewrite ~/.grok/hooks/ljm.json; remove project ljm.json
jm install ignore <code> […]     # suppress specific codes
jm install ignore --all          # suppress all install warnings
jm install ignore --clear        # re-enable warnings
```

Ignore state: `~/.grok/ljm-install-ignore.json` (machine-local).

**Agent protocol:** detect → consult user → `fix` or `ignore`. Never auto-remediate inside the hook.

`jm install fix` only rewrites Grok global hooks (+ optional project-hook removal). It does not edit `~/.claude/settings.json` (Claude config is user-owned; issues there are reported as warnings with manual fix hints).

## Verify

```bash
# Install health
./agent/jm install check

# Hook smoke test (vault-root jm may symlink to agent/jm)
echo '{"sessionId":"test","workspaceRoot":"/path/to/vault"}' |
  JM_VAULT_ROOT=/path/to/vault ./agent/jm hook session-start | head

# Or via platform runner
JM_VAULT_ROOT=/path/to/vault ./grok/bin/run-hook session-start <<<'{"sessionId":"test"}'
```

```powershell
# Windows
.\agent\jm.exe install check
'{"sessionId":"test","workspaceRoot":"D:\repos\llm\littlejohnnymnemonic"}' |
  & "D:\repos\llm\littlejohnnymnemonic\grok\bin\run-hook.ps1" session-start
```

Restart Grok sessions or press `r` in `/hooks` after install. Confirm **project/ljm** is absent and only **global/ljm** appears.

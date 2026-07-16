# Project hooks (intentionally empty of LJM lifecycle hooks)

LJM is **global**, not project-scoped. Lifecycle hooks install to
`~/.grok/hooks/ljm.json` via:

| Platform | Installer | Runner |
|---|---|---|
| Linux / macOS | `grok/install.sh` | `grok/bin/run-hook` |
| Windows | `grok/install.ps1` | `grok/bin/run-hook.ps1` (via `run-hook.cmd` or PowerShell) |

The shared install template is `grok/hooks/ljm.template.json` (placeholders
`__HOOK_RUNNER__` and `__JM_VAULT_ROOT__`). Installers expand it for the
current platform.

**Do not** put a live `ljm.json` here. Grok merges project + global hooks;
a second copy double-injects memory context, and a Windows-only template
fails on Linux (see `project/ljm:session_start[0].hooks[0]`).

SessionStart install-health detects a project `ljm.json` and emits
`<ljm-install-warning>`. With user permission: `jm install fix` removes it
and rewrites global hooks for the current platform.

# Machine & Tooling Registry

`System/machines.json` is the canonical, always-loaded source of truth for:
- Which machines exist and how to connect to them
- What elevation (sudo/UAC) is available per machine
- Where specific tools are installed
- SSH connection method, binary path, key path, and known quirks

`System/machines.example.json` is the committed template. `System/machines.json` is
gitignored — it contains real paths, IPs, and key locations that must not be public.

## Bootstrap

`jm machines` checks for `machines.json`. If absent, it copies `machines.example.json`
to `machines.json` and emits a notice. After bootstrap, edit the real file to replace
placeholder values. Entries with `"status": "unconfigured"` are silently skipped in
routing decisions — Claude will not attempt SSH or installs on them.

## Schema

### Top-level

| Field | Type | Description |
|---|---|---|
| `schema_version` | int | Always `1` for current schema |
| `machines` | object | Map of machine ID → MachineEntry |
| `tooling` | object | Map of tool ID → ToolEntry |

### MachineEntry

| Field | Required | Values | Description |
|---|---|---|---|
| `display_name` | yes | string | Human-readable label |
| `platform` | yes | `windows` `linux` `linux-wsl` `macos` | OS type |
| `current` | no | bool | Set to `true` on the machine this file lives on |
| `hostname` | no | string | FQDN or short hostname |
| `ip` | no | string | Static IP (use when hostname is unreliable) |
| `user` | no | string | Login username for Claude sessions |
| `elevation` | yes | see below | Permission level |
| `elevation_note` | no | string | Human context for elevation value |
| `ssh` | no | SSHConfig | Present only when SSH is needed to reach this machine |
| `status` | no | `""` or `"unconfigured"` | Unconfigured entries are skipped |
| `notes` | no | string | Freeform context |

### Elevation values

| Value | Meaning | Claude behavior |
|---|---|---|
| `none` | User-only; no elevation path | Never attempt sudo/UAC. Raise an explicit request to the user with the specific command they need to run. |
| `user` | User-local installs only | Can run `pip install --user`, `go install`, `npm --prefix $HOME`, etc. No system-wide installs. |
| `prompt` | UAC or sudo prompt available | Claude cannot trigger it. Raise a request; the user approves and runs the elevated command manually. |
| `full` | Root/admin available in-session | Proceed with installs and elevated operations. |

### SSHConfig

| Field | Required | Description |
|---|---|---|
| `method` | yes | `windows-native-openssh` or `standard` |
| `binary` | yes | Full absolute path to the ssh binary |
| `key` | yes | Full absolute path to the private key file |
| `flags` | no | Extra flags (e.g., `["-p", "2222", "-o", "StrictHostKeyChecking=no"]`) |
| `notes` | no | Critical quirks — these are surfaced at every session start |

**`windows-native-openssh`:** Use `C:\Windows\System32\OpenSSH\ssh.exe`. Do NOT use
Git Bash's ssh binary — it has known libcrypto incompatibilities with certain key types
(confirmed with mcp-binja key and strx). When using the Bash tool, provide the full
Windows path explicitly.

### ToolEntry

| Field | Required | Values | Description |
|---|---|---|---|
| `description` | yes | string | What the tool does |
| `install` | yes | `auto` `manual` `n/a` | Whether Claude can auto-install |
| `type` | yes | `executable` `mcp` `service` `library` | Tool category |
| `machines` | yes | object | Map of machine ID → ToolMachineEntry |

### ToolMachineEntry

| Field | Required | Description |
|---|---|---|
| `path` | yes | Absolute install path, or `builtin` / `builtin-remote` for MCP |
| `notes` | no | Quirks, aliases, access method |

## Behavioral rules

These rules are also documented in `CLAUDE.md#Machine--Tooling-Registry`.

1. **Before any SSH connection:** Read the target machine's `ssh` entry. Use
   `ssh.binary`, `ssh.key`, and `ssh.flags` exactly as specified. Never default to
   Git Bash SSH or assume a method.

2. **Before any installation:** Check `elevation`. If `none`, do not attempt sudo
   or UAC. Raise a request with the specific install command for the user to run.

3. **Before concluding a tool is missing:** Check all `tooling` entries. If the tool
   exists on another machine, surface that and ask whether to use it remotely.

4. **When a tool has no registry entry:** Ask the user for location and install
   preference before attempting discovery, download, or installation.

5. **Unconfigured entries** (`status: "unconfigured"`): Skip in routing. Do not
   attempt SSH or installs. Inform the user if they ask to use that machine.

## Project-scoped tooling overlays

For project-specific tooling (e.g., Argus needs IDA Pro on argus-lab only), keep a
`tooling.json` at the project root:

```json
{
  "project": "argus",
  "inherits": "LJM:System/machines",
  "tooling": {
    "ida_pro": {
      "description": "IDA Pro — licensed copy",
      "install": "manual",
      "type": "executable",
      "machines": {
        "argus-lab": {
          "path": "/opt/ida",
          "notes": "Licensed copy, argus-lab only"
        }
      }
    }
  }
}
```

Project overlays are gitignored in the project repo (they may contain paths or
machine-specific details). The master `machines.json` fills the gaps — project
overlays only need to declare the delta.

## Updating this registry

When adding a machine:
1. Add the entry to `System/machines.json` (never to `machines.example.json`)
2. Add a sanitized placeholder entry to `machines.example.json` if the machine
   type is novel
3. Buffer a project memory entry about the new machine for LTM

When adding a tool:
1. Add the entry to the `tooling` block in `machines.json`
2. If it's Argus-specific or project-specific, put it in that project's `tooling.json`
   instead — only add to master if the tool is used outside that project

When a machine changes IP or hostname: update `machines.json` and buffer an entry
noting the change (IPs especially drift with DHCP).

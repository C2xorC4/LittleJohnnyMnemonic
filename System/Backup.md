# Backup, Restore, and Source Repository

LJM holds substantial private user material. Naive cloud sync would leak it.
This document describes how the vault is protected against workstation
failure without exposing memory or buffer content to any remote system.

## Three layers, three audiences

| Layer | What it holds | Audience | Encryption |
|---|---|---|---|
| **1. Source repo** | Code (`agent/`), protocol (`System/`, `CLAUDE.md`), agent definitions (`Agents/`) | Eventually public — currently private | None (it's just code) |
| **2. Encrypted vault backup** | Memory, Buffer, Archive, Metrics, Ingestion manifests — *everything* | Only the user | age (X25519+ChaCha20-Poly1305) |
| **3. Sanitized public release** | Stripped snapshot of the vault for showcasing | Public | None — but content has been scrubbed |

Layers 1 and 2 are operating on **disjoint file sets**. The vault `.gitignore`
keeps Layer-1 git from touching memory content; the encrypted backup picks up
exactly what `.gitignore` excludes. Layer 3 is **deferred** — see the
roadmap entry `Buffer/2026-04-29_ljm-roadmap-sanitization-encrypted-backup.md`.

## First-time setup

### 1. Generate the age keypair

```
./jm.exe backup --init-key
```

Writes the private key to `~/.config/ljm/age.key` (or
`%USERPROFILE%\.config\ljm\age.key` on Windows) with mode `0600` and prints
the public recipient. **Escrow the keyfile immediately**:

- Copy it to a hardware device (USB, Yubikey-attached storage)
- Paste contents into your password manager
- Print and store offline

Without the keyfile, every encrypted backup is unrecoverable. There is no
recovery mechanism by design — that's the cloud-password-manager threat
model.

### 2. Wire the public recipient into Config.md

Paste the printed `age1...` recipient into `System/Config.md`:

```yaml
backup_age_recipient: age1xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
backup_local_target_dir: D:/Backup/LJM
backup_enabled: true
```

The recipient is the **public** half of the keypair — it's safe in any repo,
including a public one. Only the identity (private) key needs protection.

### 3. (Optional) Configure a private git remote

Create a private GitHub repo (one-line description: "LJM encrypted vault
backups — opaque blobs only"). Then:

```yaml
backup_remote_url: git@github.com:youruser/ljm-vault-backup.git
backup_remote_clone_path: D:/Backup/LJM/.remote-clone
backup_push_on_backup: true
```

The repo will only ever hold `vault-*.age` and `vault-*.meta.json` files.
There is no plaintext path that ever crosses the network.

### 4. Run a backup

```
./jm.exe backup
```

Verify the result:

```
./jm.exe restore-backup --latest --target ./tmp-restore
diff -r D:/Repos/LLM/LittleJohnnyMnemonic ./tmp-restore
```

The diff should show only the documented exclusions (`jm.exe`,
`workspace.json`, `rule_firings.jsonl`, `.git/`).

## Operational commands

| Command | What it does |
|---|---|
| `jm backup --init-key` | Generate keypair, write private key to identity path, print public recipient |
| `jm backup --show-recipient` | Re-derive and print the public recipient from the identity file |
| `jm backup --dry-run` | Build the manifest and report what would be backed up; no encryption, no writes |
| `jm backup` | Full backup: encrypt to local dir, optionally push to remote |
| `jm backup --no-push` | Local-only (skip remote push even if configured) |
| `jm backup --local PATH` | Override local target directory for this run |
| `jm backup --remote URL` | Override git remote URL for this run |
| `jm restore-backup --list` | List backups in the local target dir, newest first |
| `jm restore-backup --latest` | Restore the newest local backup to a temp dir |
| `jm restore-backup PATH` | Restore from a specific blob path |
| `jm restore-backup --from-remote URL` | Pull the newest blob from a remote and restore |
| `jm restore-backup --target DIR --force` | Restore on top of an existing tree (use with care — overwrites) |

## What is and isn't backed up

**Included:** `Memory/`, `Buffer/`, `Archive/`, `Metrics/citations.json`,
`Metrics/coactivation.json`, `Metrics/consolidation_log.md`,
`Metrics/last_backup.json`, `Ingestion/manifest_*.md`, `System/`, `Agents/`,
`CLAUDE.md`, `Welcome.md`, `.claude/settings.local.json`.

**Excluded (always):**

- `agent/jm.exe`, `agent/jm` — rebuildable artifacts
- `.obsidian/workspace.json`, `.obsidian/graph.json` — UI state
- `Metrics/rule_firings.jsonl` — transient operational log
- `.git/` — version-control metadata (Layer 1 has its own git)
- `Backup/` — recursion guard if a target dir lives inside the vault
- `*.tmp`, `*.swp`, `.DS_Store`, `Thumbs.db` — OS junk

Add `backup_exclude_extra` (future) for project-specific extras.

## Cleartext metadata sidecar

Every encrypted blob is paired with a `vault-{timestamp}.meta.json`. This
file is **not encrypted** because a remote needs to be able to compare
backups without unwrapping every one. The metadata contains only:

```json
{
  "version": 1,
  "created_at": "2026-04-29T17:42:11Z",
  "tool_version": "jm/go1.24.3",
  "file_count": 154,
  "uncompressed_bytes": 1572864,
  "manifest_sha256": "ab3f82...",
  "blob_name": "vault-20260429T174211Z-ab3f8260a44b.age"
}
```

No filenames. No content. No tags. Nothing that aids reconstruction.
The `manifest_sha256` is a hash over `(path, size, sha256-of-content)`
triples and is what `jm restore-backup` verifies after decrypt.

## Layer 1: source repository (vault root as git)

The vault root carries a `.gitignore` that excludes every data layer.
Initialize and push:

```
cd D:\Repos\LLM\LittleJohnnyMnemonic
git init
git add agent/ System/ Agents/ Ingestion/_README.md CLAUDE.md Welcome.md .gitignore
git commit -m "initial source-only checkpoint"

# Create a private GitHub repo (do not name it as data; "ljm-source" or similar)
git remote add origin git@github.com:youruser/ljm-source.git
git push -u origin main
```

**Verify the ignore is working:**

```
git status --ignored
```

Should show `Buffer/`, `Memory/`, `Archive/`, `Metrics/` as ignored. If any
of those appear in the *tracked* tree, stop — `.gitignore` rules don't
retroactively untrack files. Run `git rm --cached -r <path>` to fix.

## Disaster recovery

You're on a fresh machine. The vault is gone. Your keyfile is in a
password manager.

```
# 1. Restore the source
git clone git@github.com:youruser/ljm-source.git D:/Repos/LLM/LittleJohnnyMnemonic
cd D:/Repos/LLM/LittleJohnnyMnemonic/agent
go build -o jm.exe .

# 2. Restore the keyfile
mkdir -p ~/.config/ljm
# paste the AGE-SECRET-KEY contents into ~/.config/ljm/age.key
chmod 600 ~/.config/ljm/age.key

# 3. Pull the latest encrypted blob and restore
./jm.exe restore-backup --from-remote git@github.com:youruser/ljm-vault-backup.git --target ../ --force
```

The restored vault should round-trip-verify against its meta sidecar
automatically.

## Threat model

**Protected against:**

- Workstation hardware failure (encrypted blob is on at least one remote)
- Loss of the local backup directory (encrypted blob is on the git remote)
- A compromise of the git remote (blobs are unreadable without the keyfile)
- A compromise of cleartext meta sidecars (no useful content leaks)

**Not protected against:**

- Loss of both the live workstation AND the keyfile (this is irrecoverable
  by design — escrow the keyfile)
- A compromise of the live workstation while the vault is unencrypted
  (LJM does not encrypt the on-disk vault — it's plaintext for Obsidian
  and the agent to read)
- An attacker with the keyfile (they get everything, same as if they had
  the vault directly)

**Open questions (deferred):**

- Multi-device write conflict resolution (single-device assumption for v1)
- Key rotation (`jm backup-rekey` not yet built)
- Remote-side retention (currently keep-everything; relies on the user
  garbage-collecting old commits if it grows too large)

# LittleJohnnyMnemonic (LJM)

A cognitive memory substrate for LLM agents. Buffer → consolidate → long-term
memory; ACT-R-style activation, CLS-inspired rate separation, progressive
compression, spreading-activation retrieval, and daydream agents that
explore the graph in the background.

> **Status:** working prototype, in daily use. This repository holds the
> source, protocol, and tooling — no memory content. Operator memory
> lives in a separate private repository as encrypted blobs (see
> [Two-repo durability model](#two-repo-durability-model)).

## What LJM is

LJM models human-style memory for an LLM agent that runs every day in the
same workspace. The substrate is an Obsidian vault: every memory is a
markdown file with frontmatter, every association is a wiki-link, every
consolidation produces an audit trail. The agent reads the vault to recall
context at conversation start and writes to a buffer during the
conversation; consolidation moves buffered observations into typed long-term
memory.

Inspirations:

- **ACT-R** — activation, base-level decay, fan effect, spreading
  activation
- **CLS (Complementary Learning Systems)** — fast hippocampal-style
  buffer protected from catastrophic interference by rate separation
- **Information theory** — surprise weighting on encoding strength
- **Progressive forgetting** — `full → detailed → summary → gist`,
  reset on access; gists persist nearly forever as remnants

Authoritative spec: [`CLAUDE.md`](CLAUDE.md).

## Behaviour in practice — self-assessments

The substrate produces an audit trail you can read. Periodic
post-`/clear` self-assessments capture how the system performs against
its design intent: what survived the context boundary, what the hooks
reconstructed, what daydream agents surfaced about the system itself,
where the gap between *memory as context* and *memory as constraint*
showed up in real work, and what the substrate-stored knowledge gives
the agent that pretrained weights do not.

- [2026-04-29 — post-`/clear` self-assessment](docs/reflections/2026-04-29_postclear.md)

These are written by the agent in a fresh session against the same
vault, with no access to the conversation thread that produced the
material being assessed. Useful as a behavioural reference for anyone
evaluating LJM from a research, employment, or comparative-systems
perspective.

## Architecture overview

| Tier | Path | What lives here |
|---|---|---|
| Short-term | `Buffer/` | Observations from the live conversation, awaiting consolidation |
| Long-term | `Memory/{User,Feedback,Project,Reference,Semantic,Episodic,Knowledge}/` | Promoted, scored, decayed entries |
| Decayed | `Archive/` | Below-threshold memories — recoverable via `jm restore` |
| Audit | `Metrics/` | Consolidation log, co-activation graph, citations |
| Protocol | `System/`, `Agents/`, `CLAUDE.md` | Spec, schema, scoring, agent definitions |
| Tooling | `agent/` | The `jm` Go CLI (consolidate, retrieve, associate, decay, compress, backup, …) |

The agent integrates into Claude Code via three lifecycle hooks
(SessionStart, UserPromptSubmit, Stop) — see `System/` for the full
lifecycle.

## Two-repo durability model

Memory state is too sensitive to live on a single workstation, and too
sensitive to live in plaintext on a remote. LJM separates concerns into
two repositories:

1. **This repository** (public) — code, protocol, agent definitions,
   schema. No memory content; `.gitignore` keeps `Buffer/`, `Memory/`,
   `Archive/`, and `Metrics/` out of source. A separate
   `jm publish` workflow for emitting sanitized memory snapshots is
   on the roadmap but not required for the source itself.
2. **Encrypted vault repo** (separate, always private) — `.age`
   blobs of the live vault, written by `jm backup` and pulled by
   `jm restore-backup`. Encryption is X25519+ChaCha20-Poly1305 via
   [age](https://age-encryption.org); the private key never leaves
   the operator's machine.

Always-pull-then-push, never-auto-merge. Conflicting memory states
require manual review. Full operational guide:
[`System/Backup.md`](System/Backup.md).

## Quickstart

```bash
# 1. Build the CLI
cd agent
go build -o jm.exe .

# 2. Generate the age keypair (writes to ~/.config/ljm/age.key)
./jm.exe backup --init-key
# Paste the printed age1... recipient into System/Config.md.
# Escrow the keyfile — without it, encrypted backups are unrecoverable.

# 3. Run a backup (writes to backup_local_target_dir, then pushes if configured)
./jm.exe backup

# 4. Verify the round-trip
./jm.exe restore-backup --latest --target /tmp/ljm-restore
```

Common subcommands:

```
jm status          # System health dashboard
jm consolidate     # Run buffer consolidation
jm retrieve "..."  # Score-and-rank memories against a query
jm associate "..." # Free-text contextual association
jm backup          # Encrypted backup
jm restore-backup  # Decrypt + extract a backup
```

## Repository layout

```
Agents/             Agent definitions (memory-associator, memory-daydream)
Buffer/             [gitignored] Live short-term memory
Memory/             [gitignored] Long-term memory by type
Archive/            [gitignored] Decayed but recoverable
Metrics/            [gitignored] Audit trail, co-activation, citations
Ingestion/          Book-ingestion protocol (manifests gitignored)
System/             Architecture docs, schema, scoring, config
agent/              Go CLI (jm) — consolidate, retrieve, backup, hooks
.obsidian/          Shared Obsidian config (workspace state gitignored)
CLAUDE.md           Authoritative protocol — read this first
```

The `.gitignore` at repo root keeps memory content out of source. The
[encrypted-backup](System/Backup.md) repo covers exactly what
`.gitignore` excludes — the two mechanisms are complementary.

## Configuration

All tunables live in [`System/Config.md`](System/Config.md). Edits take
effect on the next consolidation or decay run; no rebuild required.

For backup-specific config (encryption recipient, local target dir,
remote URL, retention, cooldown), see the `## Backup` section in
`System/Config.md` and the operational guide at
[`System/Backup.md`](System/Backup.md).

## License

License pending — drop a `LICENSE` file at repo root before any public
release.

## Acknowledgments

The cognitive model draws on ACT-R (John Anderson and the Carnegie Mellon
group), Complementary Learning Systems (McClelland, McNaughton, O'Reilly),
and the broader literature on consolidation, decay, and retrieval. The
implementation choices and operational protocol are the operator's own.

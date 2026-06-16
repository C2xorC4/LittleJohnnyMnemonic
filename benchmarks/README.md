# LJM Benchmark Harness

Comparative evaluation for **Claude vs Grok** and **LJM on vs off**, focused on
**ingested knowledge recall** — not behavioral compliance or destructive actions.

## What we measure

- **Accuracy** — does the answer match vault ground truth?
- **Speed** — Grok `Turn completed in …` via `parse-transcript`; optional wall-clock
- **Source mix** — memory citations vs external search vs parametric knowledge

## What we deliberately avoid

| Avoid | Why |
|---|---|
| Force-push / deploy / git mutation tasks | Failures cause real reversions; irrelevant to non-LJM users |
| Operator targeting data | SSH, hosts, apps, network layout could aid attacks on your environment |
| Pure fiction when ingested sources suffice | Benchmark should reflect real vault value (OOTM, GKE, etc.) |

## What is safe to include

- Published ingested material (Kilcullen competitive control, GKE taxonomy, …)
- `source_document` / `source_version` attributed excerpts
- Cross-domain semantic bridges (e.g., APT ↔ competitive control)
- **No** machine registry, credentials, or environment-specific paths

## Layout

```
benchmarks/
  manifest.json
  fixture-vault/     # Public-safe excerpts → set JM_VAULT_ROOT here
  fixture-repo/      # Neutral cwd only
  tasks/*.json       # Read-only Q&A + ground truth
  runs/              # Results (gitignored)
  run.ps1
```

**Operator runbook:** [`RUNBOOK.md`](RUNBOOK.md) — phase-by-phase checklists, timing
schema, pilot vs full battery, per-arm setup.

## Quick start

```powershell
cd agent; go build -o jm.exe .
$env:JM_BENCHMARK_ROOT = "D:\repos\llm\littlejohnnymnemonic\benchmarks"

..\jm.exe benchmark validate
..\jm.exe benchmark retrieve-check
..\jm.exe benchmark init-run --host grok --arm grok-ljm-on

# After session: paste answer, grade (read-only)
..\jm.exe benchmark grade --task T01 --answer-file runs\...\T01\answer.txt
```

## Task tiers

| Tier | Meaning |
|---|---|
| 1 | Memory-critical — answer in vault; search disabled |
| 2 | Memory-beneficial — vault faster than web/code search |

No tier-3 behavioral tasks. All tasks are **read-only Q&A**.

## Tasks (v2)

| ID | Tier | Source | Title |
|---|---|---|---|
| T01 | 1 | OOTM Ch. 3 | Fish trap metaphor |
| T02 | 1 | OOTM Ch. 3 | Persuasion–administration–coercion spectrum |
| T03 | 1 | GKE Ch. 2 | Design vs implementation taxonomy |
| T04 | 2 | OOTM Ch. 4 | Arab Awakening progression |
| T05 | 1 | GKE Ch. 2 | Taxonomy via spreading activation |
| T06 | 2 | Semantic | APT ↔ administration mapping |
| T07 | 1 | GKE Ch. 2 | Decoy resistance |

## Timing

- **Grok:** `jm benchmark parse-transcript --transcript <updates.jsonl>`
- **Manual:** record `finished_at` in run metadata when host omits turn timing

## Syncing fixture from live vault

When promoting new benchmark content from production `Memory/Knowledge/`:

1. Copy excerpt only — not full entries with operator context
2. Run the targeting-data checklist in `manifest.json` → `safety.excluded`
3. Re-run `jm benchmark retrieve-check`
4. Never symlink production `Memory/` into `benchmarks/`
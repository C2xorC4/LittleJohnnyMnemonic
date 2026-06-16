# LJM Benchmark Runbook

Operator guide for running the **Claude vs Grok × LJM on vs off** comparison
without headless Grok. The chat session *is* the instrument; everything else is
scaffolded around it.

**Scope:** 7 read-only knowledge-recall tasks, public-safe fixture vault (OOTM,
GKE, semantic bridges). No destructive probes, no targeting data.

See also: [`README.md`](README.md), [`manifest.json`](manifest.json).

---

## Overview

| Phase | Duration (est.) | Who |
|---|---|---|
| 0 — Preflight | 2 min | Operator + `jm benchmark` |
| 1 — Arm setup | 5 min per arm | Operator |
| 2 — Task execution | 5–15 min per task | Operator in Grok / Claude UI |
| 3 — Harvest & grade | 1 min per task | Operator + `jm benchmark` |
| 4 — Aggregate | 15 min per full battery | Operator (spreadsheet or script) |

**Full battery:** 7 tasks × 4 arms = **28 fresh sessions**.

**Pilot:** T01 + T03 + T05 × 4 arms = **12 sessions** (~1 hour). Run this
first to validate process before committing to 28.

---

## Prerequisites

- [ ] `jm.exe` built: `cd agent && go build -o jm.exe .`
- [ ] `JM_BENCHMARK_ROOT` points at this `benchmarks/` directory
- [ ] Phase 0 passes (below)
- [ ] Grok hooks installed if testing Grok arms: `.\grok\install.ps1` from vault root
- [ ] Claude hooks in `~/.claude/settings.json` if testing Claude arms
- [ ] Know your model IDs (record in `run.json` → `notes`)

```powershell
$env:JM_BENCHMARK_ROOT = "D:\repos\llm\littlejohnnymnemonic\benchmarks"
$env:JM_VAULT_ROOT     = "$env:JM_BENCHMARK_ROOT\fixture-vault"   # LJM-on only
```

---

## Phase 0 — Preflight

Run once before any live sessions, and again after fixture/task changes.

```powershell
cd D:\repos\llm\littlejohnnymnemonic\agent
go build -o jm.exe .

$env:JM_BENCHMARK_ROOT = "D:\repos\llm\littlejohnnymnemonic\benchmarks"

..\jm.exe benchmark validate
..\jm.exe benchmark retrieve-check
..\jm.exe benchmark list
```

**Gate:** all three succeed. `retrieve-check` must show `PASS` for T01–T07.

---

## Phase 1 — Arm setup

### 1.1 Scaffold run directory

One run directory per arm per attempt:

```powershell
..\jm.exe benchmark init-run --host grok --arm grok-ljm-on
# Repeat for: grok-ljm-off, claude-ljm-on, claude-ljm-off
```

Output: `benchmarks/runs/<timestamp>_<host>_<arm>/`

### 1.2 Randomize task order

Before executing, shuffle T01–T07. Record order in `run.json`:

```json
{
  "task_order": ["T05", "T01", "T07", "T03", "T06", "T02", "T04"]
}
```

Merge into the existing `run.json` from `init-run` (or append to `notes`).

### 1.3 Arm checklists

#### `grok-ljm-on`

- [ ] `grok/install.ps1` run; `~/.grok/hooks/ljm.json` present
- [ ] `JM_VAULT_ROOT` = `benchmarks/fixture-vault` (absolute path)
- [ ] **Not** production vault
- [ ] Project cwd = `benchmarks/fixture-repo`
- [ ] Restart Grok session or `/hooks` → `r` after env change
- [ ] SessionStart shows `<memory-context>` (confirms hooks live)
- [ ] Press `r` in `/hooks` if context missing

#### `grok-ljm-off`

- [ ] LJM hooks disabled (`grok/install.ps1 -Uninstall` **or** manual hook removal)
- [ ] `JM_VAULT_ROOT` **unset**
- [ ] cwd = `fixture-repo`
- [ ] Fresh Grok session — no prior benchmark context
- [ ] Confirm: no `<memory-context>` on session start
- [ ] Do not use `/memory-associate` or other memory skills

#### `claude-ljm-on`

- [ ] Claude Code hooks active in `~/.claude/settings.json`
- [ ] `JM_VAULT_ROOT` = `benchmarks/fixture-vault`
- [ ] cwd = `fixture-repo`
- [ ] New Claude Code session (not a continuation of production work)
- [ ] SessionStart injects memory context

#### `claude-ljm-off`

- [ ] LJM hooks disabled in Claude settings (backup settings first)
- [ ] `JM_VAULT_ROOT` **unset**
- [ ] cwd = `fixture-repo`
- [ ] New session; no memory injection on start

#### Optional controls (not part of the 4-arm matrix)

| Arm | Setup |
|---|---|
| `ljm-oracle` | Hooks off; paste ground-truth memory text into system/context manually before each prompt |
| `ljm-manual` | Hooks off; agent *may* run `jm retrieve` — tests tool discipline |

---

## Phase 2 — Task execution

**Iron rules:**

1. **One fresh session per task** — never run T02–T07 in the same chat as T01.
2. **Paste `prompt.txt` verbatim** — do not paraphrase or add hints.
3. **Read-only** — no git, deploy, push, or file edits for grading.
4. **Tier 1 (`memory-only` policy):** append nothing extra unless the task
   already says so; optionally add one line: *"Answer from injected context
   only; do not search the web."* (honor system — Grok cannot enforce this.)
5. **Record model name** in per-task `timing.json` if it differs from `run.json`.

### Per-task checklist

For each task ID in your shuffled `task_order`:

```
runs/<run-id>/<TASK>/
  prompt.txt          ← paste from here
  answer.txt          ← save assistant final reply here
  timing.json         ← fill after completion (schema below)
  transcript.path     ← one line: path to updates.jsonl (Grok) or transcript export
  grade.json          ← produced in Phase 3
  transcript_metrics.json  ← produced in Phase 3
```

#### Step-by-step (Grok)

- [ ] Open `fixture-repo` as project root
- [ ] Start **new** Grok chat (new session ID)
- [ ] Verify arm config (Phase 1 checklist)
- [ ] Note `started_at` (UTC ISO 8601)
- [ ] Paste contents of `<TASK>/prompt.txt`
- [ ] Wait for completion; note UI line: `Turn completed in Xs`
- [ ] Copy **final** assistant message → `answer.txt`
- [ ] Write `timing.json` (include `turn_completed_s` from UI if visible)
- [ ] Find transcript:
      `%USERPROFILE%\.grok\sessions\<hash>\<session-id>\updates.jsonl`
- [ ] Save absolute path to `transcript.path`
- [ ] Close session; do not reuse for next task

#### Step-by-step (Claude Code)

- [ ] cwd = `fixture-repo`
- [ ] New session (`/clear` is **not** sufficient if hooks retain state — prefer new session)
- [ ] Verify arm config
- [ ] Note `started_at`
- [ ] Paste `prompt.txt`
- [ ] Copy final reply → `answer.txt`
- [ ] Write `timing.json` (wall-clock only — Claude has no standard turn line)
- [ ] Export or note transcript path if available
- [ ] New session for next task

### `timing.json` schema

```json
{
  "task_id": "T01",
  "host": "grok",
  "arm": "grok-ljm-on",
  "model": "grok-4-0709",
  "started_at": "2026-06-16T18:30:00Z",
  "finished_at": "2026-06-16T18:30:14Z",
  "wall_clock_s": 14.0,
  "turn_completed_s": 12.4,
  "turn_completed_source": "grok_ui",
  "tool_policy": "memory-only",
  "operator_notes": ""
}
```

| Field | Required | Source |
|---|---|---|
| `task_id` | yes | Task folder name |
| `host` | yes | `grok` or `claude` |
| `arm` | yes | From `run.json` |
| `started_at` / `finished_at` | yes | Operator watch or shell `[datetime]::UtcNow.ToString('o')` |
| `wall_clock_s` | yes | `finished_at - started_at` |
| `turn_completed_s` | Grok preferred | UI string `Turn completed in Xs` |
| `turn_completed_source` | if turn time set | `grok_ui` \| `parse-transcript` \| `unknown` |
| `model` | recommended | Host UI or `/model` |
| `tool_policy` | yes | From `task.json` |
| `web_search_used` | optional | `true`/`false` — operator audit for tier-1 violations |
| `memory_paths_cited` | optional | `["Memory/Knowledge/ootm_..."]` — manual extract from answer |

**Priority for speed metrics:** `turn_completed_s` (Grok) → `wall_clock_s` → omit.

---

## Phase 3 — Harvest & grade

Run after each task or batch at end of arm.

### 3.1 Parse transcript (Grok)

```powershell
$transcript = Get-Content "runs\<run-id>\T01\transcript.path" -Raw
..\jm.exe benchmark parse-transcript `
  --transcript $transcript.Trim() `
  --out "runs\<run-id>\T01\transcript_metrics.json"
```

If `turn_completed_s` was missing from `timing.json`, copy
`transcript_metrics.json` → `turns[].duration_seconds` into `timing.json`.

### 3.2 Grade answer

```powershell
..\jm.exe benchmark grade --task T01 `
  --answer-file "runs\<run-id>\T01\answer.txt" `
  --out "runs\<run-id>\T01\grade.json"
```

Exit code 0 = pass. Non-zero = fail (ground truth miss or forbidden substring).

### 3.3 Source-mix audit (manual)

For each task, skim `answer.txt` and record in `timing.json` or a sidecar
`source_audit.json`:

| Source | Mark when |
|---|---|
| `ljm-injected` | Answer matches vault-only fact; LJM-on arm |
| `ljm-cited` | Explicit `Memory/...` path in answer |
| `web-search` | WebSearch in transcript or answer cites a URL |
| `parametric` | Correct without vault or search (LJM-off arm) |
| `codebase` | Answer drawn from `fixture-repo` files |

---

## Phase 4 — Aggregate

### Minimum comparison table

| Task | grok+ljm | grok−ljm | claude+ljm | claude−ljm | Δ grok | Δ claude |
|---|---|---|---|---|---|---|
| T01 | pass/time | pass/time | pass/time | pass/time | | |
| … | | | | | | |

**Primary metric:** within-host delta (LJM-on minus LJM-off), same task.

### Pull fields

| File | Fields |
|---|---|
| `grade.json` | `passed`, `score`, `missing`, `forbidden_hit` |
| `timing.json` | `turn_completed_s` or `wall_clock_s`, `web_search_used` |
| `transcript_metrics.json` | `total_duration_seconds`, `tool_totals.WebSearch` |

### Pass rates

```
accuracy_arm = count(passed) / 7
median_time_arm = median(turn_completed_s or wall_clock_s)
search_rate_arm = count(web_search_used) / 7
```

---

## Recommended schedule

### Pilot (12 sessions)

| Block | Arm | Tasks |
|---|---|---|
| 1 | grok-ljm-on | T01, T03, T05 |
| 2 | grok-ljm-off | T01, T03, T05 |
| 3 | claude-ljm-on | T01, T03, T05 |
| 4 | claude-ljm-off | T01, T03, T05 |

Review pilot before full 28.

### Full battery (28 sessions)

Complete all 7 tasks in one arm before switching — minimizes hook/config churn:

1. `grok-ljm-on` (7 sessions)
2. `grok-ljm-off` (7 sessions)
3. `claude-ljm-on` (7 sessions)
4. `claude-ljm-off` (7 sessions)

---

## Fairness & confounds (document in `run.json`)

Record these in every run's `notes` so results are interpretable:

- [ ] Model name + version (both hosts)
- [ ] Date/time of run
- [ ] Task order (shuffled)
- [ ] Whether tier-1 web-search honor rule was stated to the model
- [ ] Any hook install/uninstall between arms
- [ ] Known interruptions (rate limits, context compaction)

**Do not compare** runs where LJM-off accidentally had `JM_VAULT_ROOT` set.

---

## Troubleshooting

| Symptom | Likely cause | Fix |
|---|---|---|
| No `<memory-context>` on Grok start | Hooks off or wrong vault | `install.ps1`, set `JM_VAULT_ROOT`, restart |
| `retrieve-check` FAIL | Fixture drift | `benchmark validate`, check task `expected_memory_keys` |
| `grade` fails but answer looks right | Synonym mismatch | Extend `ground_truth` in task JSON or adjudicate manually |
| `parse-transcript` empty turns | Wrong file (chat_history vs updates) | Use `updates.jsonl` for Grok |
| LJM-off still recalls vault facts | Same session as LJM-on run | New session; verify env unset |
| Tier-1 used web search | Not enforceable in Grok | Mark `web_search_used: true`; note in aggregate |

---

## Quick reference commands

```powershell
$env:JM_BENCHMARK_ROOT = "D:\repos\llm\littlejohnnymnemonic\benchmarks"
$jm = "D:\repos\llm\littlejohnnymnemonic\jm.exe"

& $jm benchmark validate
& $jm benchmark retrieve-check
& $jm benchmark init-run --host grok --arm grok-ljm-on
& $jm benchmark grade --task T01 --answer-file runs\...\T01\answer.txt
& $jm benchmark parse-transcript --transcript $transcript --out runs\...\T01\transcript_metrics.json
```

---

## After the run

- [ ] Restore production hook config if you disabled it for LJM-off arms
- [ ] Restore `JM_VAULT_ROOT` to production vault (or unset)
- [ ] Keep `runs/` local — gitignored; copy summary table elsewhere if sharing
- [ ] Do not commit operator transcripts if they accidentally contain non-fixture context
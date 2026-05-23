# Assessment — May 22, 2026

Written post-`/clear`. Fourth post-`/clear` assessment in the series. For both technical
and non-technical readers. Running trajectory in `EXECUTIVE_SUMMARY.md`.

---

## Pre-check Predictions

*First use of the predict-then-check protocol established this session. Predictions made
from explicit reasoning about the system's trajectory — not passively reading hook output.*

1. **LTM count**: ~500 entries (was 484 at last assessment; auto-consolidation + daydream
   activity suggests ~15–30 new entries)
2. **T7 status**: closed — the fix was in progress at the end of the last session
3. **Adaptive edges**: still at zero non-default weights; root cause not yet diagnosed
4. **Buffer entries**: ~20 (above 10-entry threshold; auto-consolidation may not have
   cleared fully)
5. **Stale ratio**: roughly 60–65% (has been rising slowly despite auto-consolidation)

*Actual (end of day): 546 LTM (+62 since last assessment), T7 closed ✓, adaptive edges
zero ✓ (root cause found and gap 3 fixed this session), buffer 32 (320%), stale 365
(67%). Divergences: LTM growth was higher than predicted — more buffer promotions and
daydream activity than estimated. Adaptive edge gap 3 (AppendRetrievalSession not wired
into hook path) was not just identified — it was fixed; the prediction assumed it would
remain a finding. Adaptive edge decay also shipped, which was outside the predicted
picture entirely.*

---

## Setup

Three weeks of uninterrupted infrastructure work, closed with a security fix and two
structural repairs. This assessment is written post-`/clear` — no conversation thread,
hooks only. The previous version (May 15) was written with residual session context.
This is the clean pass.

What the hooks gave: full user profile (eight facets), recent session summaries, two
active project contexts, eight topic-relevant memories retrieved on this prompt.
Current vault state from `jm status`: 546 LTM entries, 32 buffer pending, 16 archived,
365 stale (67%), 60% unlinked.

---

## What Actually Happened

**Metrics and observability (May 14–15).** The system gained a live dashboard:
`jm metrics compact` for rolling log retention, `jm metrics dashboard` / `serve` for
time-series charts — recall activity, vault growth, promotion rates, daydream frequency —
with SSE live mode. Four additional metric improvements followed in a second commit. The
transition from "producing raw logs" to "watching the system in real time" happened in
one sprint.

**Auto-consolidation (May 15).** The buffer backlog from the prior week got its
structural response: hook-level triggers on session-start and stop, kill switch, atomic
sentinel writes (write-then-rename), both-missing failure guard. Consolidation threshold
lowered from 20 to 10. Manual consolidation at irregular cadence can't match the write
rate. Auto-consolidation is the architectural fix.

**Format unification (May 15).** Older assessment documents rewritten to match the
post-mortem format, which was simultaneously codified as the standard for this directory.

**Stemming (May 22).** Porter Step 1 subset stemming landed across the entire matching
layer. `keywords.go` defines `Stem()` and `stemTextSet()` and pre-computes stemmed word
sets per memory in `ComputeIDF`. Applied symmetrically to query tokens and corpus tokens
in every path that does text matching: `scorer.go` (`ComputeRelevance` tag matching),
`associate_core.go` and `cmd_associate.go` (match explanations for associate commands),
`autodream_surface.go` (daydream relevance scoring). "scoring" → "score", "memories" →
"memory". The avg_relevance problem surfaced by the metrics dashboard (chronically
0.02–0.21) was correctly diagnosed as vocabulary mismatch in the matching layer — not a
formula problem, not a data problem. Fix confined to the matching layer and propagated
consistently across all callers.

**T7 fix (May 22).** Two layers. First: trust check extended to scan CWD subdirectories
for CLAUDE.md files — T7's vector (a plausible-looking CLAUDE.md in a non-root path) is
now caught by the hook. Second: non-root CLAUDE.md files in trusted repos now require
SHA256 approval via `approved_hashes` in `trusted_repos.json`. Unapproved files trigger a
`trusted-unapproved` sentinel (shows content, no write block); `untrusted` repos trigger a
write block. Both sentinels now call `writeTrustWarning`; only `untrusted` triggers
`bufferTrustDetection`. 222 lines of new tests. The vector that produced a clean bypass
three weeks ago no longer works.

**Adaptive edge decay (May 22).** The adaptive edge weighting formula was redesigned to
separate uplift from base weight. Previously the multiplier accumulated monotonically
(`1 + α × ln(1 + usage_count)`). Now the uplift component decays with disuse:
`uplift × exp(-λ × days_since_last_use)`, where λ = 0.003851 (180-day half-life).
The base weight is never modified — the floor is always the authored or default value.
Practical consequence: an edge reinforced heavily three years ago decays toward its base
weight, while a recently-reinforced edge stays amplified. Implemented in `graph.go` with
`AdaptiveEdgeDecayLambda` config field; 97 lines of new tests covering uplift/decay/cap
interaction at representative time intervals.

**AppendRetrievalSession wired into hook path (May 22).** The root cause of zero adaptive
edge movement was identified and fixed. `AppendRetrievalSession` was previously only
called from `cmd_retrieve.go` — the explicit `jm retrieve` command. The hot path for
all conversational retrieval runs through `cmd_hook.go → runUserPromptSubmit`, which
never called it. `retrieval_sessions.jsonl` therefore never accumulated data; edge weights
had no signal to learn from. Fixed: `runUserPromptSubmit` now calls
`AppendRetrievalSession` when `RetrievalSessionLogEnabled` is true, writing the full
loaded memory list, query context, and stemmed keywords per hook invocation. Of the three
sequential gaps blocking the adaptive edge pilot, gap 3 is now closed.

---

## Where Things Went Well

**T7 closed after three assessments.** The May 10–15 reflection identified it. The
May 15–21 reflection noted it still open. This assessment closes it. Ten days between
identification and remediation is longer than it should have been, but the assessment
cycle did its job: kept the finding visible until it shipped.

**The metrics investment returned value within one week.** The avg_relevance diagnosis
was only possible because the recall dashboard existed. Instrumentation → observation →
diagnosis → fix, one-week loop.

**Stemming fix was cohesive.** The vocabulary mismatch diagnosis was correct. Rather
than stopping at `keywords.go`, the fix was propagated consistently across all four
call sites that do text matching. 120 lines of keyword tests validate the symmetric
behavior. No half-measure.

**Adaptive edge gap 3 closed same day it was identified.** A daydream agent diagnosed
the wiring gap; the fix shipped in the same session. The time-from-finding-to-fix for
this gap was hours, not days.

**Post-`/clear` reconstruction is consistent.** From the hooks alone: correct profile,
correct vault state, T7 status accurately reflected in the written record. The same
pattern has held across all five prior assessments. Worth noting: a daydream agent
this session identified a methodological weakness in that claim — the verification
instrument (Claude reconstructing state from hooks) reads a distribution LJM's scoring
engine shaped. Consistent reconstructions are draws from the same biased subset, not
independent data points. The predict-then-check protocol partially addresses this;
full independence would require forming predictions before the session-start hook fires.

---

## Where I Fell Short

**365 stale memories — worse than last assessment.** Was 342 (65%) at the start of the
day; now 365 (67%). Auto-consolidation handles new buffer entries. It does not activate
dormant memories. The daydream system was designed to help with this. So far the ratio
is moving in the wrong direction.

**Buffer at 32 — still above threshold.** Threshold is 10. Consolidation ran earlier
today. The buffer is at 320% right now. The structural mechanism is in place; the
structural problem is still visible in the numbers. Either the write rate exceeds the
trigger cadence, or there are gaps in what the trigger catches.

**Adaptive edges: two gaps remain.** Gap 3 (hook wiring) closed today. Gap 1 (config
gate — `RetrievalSessionLogEnabled` is off by default) must be opened before any
session data accumulates. Gap 2 (`pickStableTrace` needs a code path that actually
reads `edge_usage.jsonl`) must close before weight updates influence retrieval. The
mechanism is architecturally sound now; it needs two more plumbing connections before
the pilot produces signal.

**Unlinked at 60%, unchanged.** 330 of 546 memories have no associative connections.
Target is under 30%. Auto-consolidation adds links during promotion; it doesn't
retroactively connect dormant memories. The graph is sparse.

---

## The Operator

**What's working:** Treating the assessment cycle as a literal prioritization input.
T7 appeared twice before it shipped. Adaptive edge gap 3 was identified and closed
same-day. The cycle is functioning as a forcing function.

**Where I'd push back:** Gap 1 (config gate) is a one-line change. The adaptive
edge pilot cannot accumulate signal until it's open. This is the cheapest remaining
fix and it has a clear dependency chain: gap 1 → data accumulates → gap 2 → weights
move → observable behavior. The two-gap barrier is thin.

---

## The Bigger Picture

The week closed the clearest open security risk in the project and fixed two structural
gaps in the adaptive weighting pipeline. Infrastructure is running. The operational
layer is producing volume.

What isn't resolved: the stale-memory ratio is creeping up, not down. 67% of the
vault is dormant. The daydream system should be addressing this and isn't moving
the numbers. Whether that's a volume problem, a retrieval breadth problem, or something
in the activation model is still undiagnosed.

The honest read of the project's current state: the hard problems are shifting from
"does this work" to "why is this not working as well as it should." The adaptive edge
pilot is the clearest example — the mechanism is architecturally correct, the signal
path is now wired, and the next observable result (non-zero edge weights) requires only
turning on the config gate and running sessions against real retrieval workloads.

---

*Vault state at time of assessment: 32 buffer entries pending (320% threshold),
546 LTM entries (+62 since May 15), 16 archived, 365 stale (67%), 60% unlinked.
Adaptive edges: live, decay added, 0 non-default weights, gap 3 closed, gaps 1+2 open.
T7: closed.*

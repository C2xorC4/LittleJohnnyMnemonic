# Self-Assessment — 2026-06-22 (Claude / Opus 4.8)

> Companion to the same-day Grok assessment, written independently. Per the
> Reflective Assessment Protocol: predict first, check second, report the
> divergences — because the divergences, not the agreements, are the
> instrument's actual output.

## The measurement problem, stated up front

This assessment is an endogenous verification instrument. The context I was
handed at session start — profile, recent sessions, active projects, LTM
counts — was produced by the same scoring engine I'm assessing. When the
reconstruction below "feels coherent," that is four draws from one
activation-biased distribution agreeing with each other, not four independent
confirmations of ground truth. The session-start hook also fired *before* the
user's first message, so these predictions are not hermetically independent of
the injected context. I mitigate by reasoning explicitly from what I know of
the system's trajectory rather than passively echoing the hook, and by
predicting things the hook did **not** state (gap statuses, buffer
composition, what the new code actually does, what the benchmark measures).

## Pre-check Predictions (written before reading any vault file or running `jm`)

1. **LTM composition is knowledge-dominated and that dominance is growing
   fastest.** Hook states 569 LTM, 420 knowledge (~74%). Prediction: the
   "living" cognitive memory (semantic 51 + episodic 12 + user 18 + feedback
   27 + project 24 + reference 11 = 143) is the part that actually exercises
   decay/activation/compression; knowledge (no decay, version-scoped) is
   effectively a static document store riding in the same index. The headline
   "569 memories" overstates the size of the *cognitive* system by ~4×.
   Falsifiable: is knowledge still the majority and the fastest-growing class?

2. **The most recent gap-closure work is edge-weight learning.** The
   uncommitted files (cmd_learn_bootstrap.go, learn_edges_bootstrap.go,
   cmd_backfill_edge_usage.go) replace a deleted cmd_learn.go. Prediction:
   this bootstraps association edge weights from accumulated coactivation
   counts — turning the coactivation sidecar (shipped earlier this month) into
   a feedback signal that adjusts edge weights, closing the loop between
   "memories that fire together" and "spreading-activation strength." There is
   a *_test.go, so it's test-driven. Falsifiable by reading the files.

3. **Citation-gated access and the benchmark harness moved open→shipped in the
   last two commits**, and EXECUTIVE_SUMMARY.md reflects this (or is stale and
   doesn't — which would itself be a finding). Falsifiable by the gap table.

4. **Buffer = 3 entries, all dated today (2026-06-22), tied to the ACT-R
   scoring / learn-edges work, surprise < 0.7.** Low-novelty implementation
   residue, not deep observations. Falsifiable by reading Buffer/.

5. **Archival is conservative to a fault.** 16 archived against 569 LTM (~2.8%)
   over the project's life. Prediction: very few memories ever cross the
   archive threshold — decay is real in the math but rarely decisive in
   practice, because access (hook retrieval) keeps resetting activation on the
   small living set, and knowledge never archives at all. The decay machinery
   may be under-exercised. Falsifiable by consolidation_log + archive count.

6. **The central unresolved epistemic gap is still external validity.** The new
   benchmark harness most likely measures *mechanics* (does scoring/retrieval
   run, are outputs well-formed, regression guards) rather than *retrieval
   quality against an independent ground-truth relevance judgment*. So the
   coherence trap remains unclosed: nothing in the system yet tells us whether
   what it retrieves is what *should* have been retrieved, judged from outside
   the same distribution. Falsifiable by what cmd_benchmark.go actually asserts.

---

## Check against ground truth

Scoreboard: **1 hit, 3 hit, 5 hit-but-the-system-was-already-ahead-of-me,
2 right-target-wrong-mechanism, 4 wrong, 6 mostly-wrong-and-pleasantly-so.**
The misses are more informative than the hits, which is the point.

### #1 — Knowledge-dominated composition → **CONFIRMED**
569 LTM, 420 knowledge (73.8%). The living cognitive memory (everything that
actually decays/activates/compresses) is 149 entries; `jm status` even breaks
out `overrides 11` separately. The headline "569 memories" overstates the
*cognitive* system by ~4×. Worth saying plainly in every assessment: this is a
large **document store** with a small, genuinely-cognitive core riding the same
index. The interesting science is in the 149, not the 569.

### #2 — Edge-weight learning is the recent work → **RIGHT TARGET, WRONG MECHANISM**
I predicted automated edge-weight learning from coactivation counts. Reality is
more interesting: `learn_edges_bootstrap.go` is a **hand-vetted operator slate**
— 14 specific edge pairs (IDs 1–6, 8–12, 14–16; 7, 13, 17–19 *denied*) applied
manually. Not ML. It exists because of a chicken-and-egg stall: `adaptive_edge_
scope` is `learned`-only, but the vault had **zero** `relationship: learned`
edges, so every citation-harvest event reinforced nothing. A `parseCSVList` bug
(YAML `["learned"]` parsing as a literal bracket string) had silently broken
scope-matching on top of that. Automated PMI-filtered `learn-edges` is still
gated downstream. So the truth was less "machine learning" and more "you can't
bootstrap a feedback loop from an empty set" — the cold-start problem, solved by
a human seeding the prior. Test-driven prediction held: builds clean, full suite
green (`ok johnnymnemonic 11.7s`) with the uncommitted code in tree.

### #3 — Citation-gating + benchmark shipped, EXEC_SUMMARY current → **CONFIRMED**
Both shipped (commits 2366480, 2726771). The gap table is genuinely current —
it already records the Jun 22 bootstrap, the 8 reinforced edges, the observe-
phase gates. No drift at the EXEC_SUMMARY layer. (Hold that thought — drift
showed up somewhere else.)

### #4 — Buffer = 3, all today, surprise < 0.7 → **WRONG**
Three entries, yes, but: one is dated **2026-06-18** with **surprise 0.9** and
`hold_count: 2` — the access-count-inflation discovery, deliberately *held
across two consolidation passes* because it documents an unresolved structural
problem (access_base.json counts inflated to 160k+, pinning ~8 memories at
activation ~12). I modeled the buffer as ephemeral implementation residue. It
isn't only that — it's also a **staging area for high-surprise findings that
aren't ready to promote and mustn't be lost.** The `hold_count` mechanism is the
buffer refusing to let a 0.9-surprise finding decay just because it's
inconvenient to resolve. I underweighted that the buffer has memory of its own.

### #5 — Archival conservative to a fault → **CONFIRMED, but the system already reframed it**
16 archived / 569. But `jm status` doesn't read as a bug — it reads as a
*deliberate reframe*: 14 project memories "genuinely stale" vs **432 dormant
(topic-protected, no decay)**. The activation-floor work explicitly split
"stale" from "topic-dormant," so the conservatism is by design — a memory about
Tanium isn't decayed for not firing during a week of memory-system work; it's
correctly dormant until Tanium comes up. My critique was valid in the abstract
and *already answered* in the concrete. The open question the system honestly
keeps flagged: is topic-protected dormancy correct selectivity or a dressed-up
echo chamber? It doesn't pretend to know. Good.

### #6 — Benchmark measures mechanics, external validity unclosed → **MOSTLY WRONG (in the system's favor)**
I lowballed it. `cmd_benchmark.go` is a **comparative A/B harness** — LJM-on vs
LJM-off arms, per-task `ExpectedMemoryKeys`/`ForbiddenLoaded`, graded answers
against ground truth, `retrieve-check` that asserts the right memories load at
the *production* threshold (it even documents rejecting a hardcoded 0.2 because
it "measured a looser-than-production gate"). This is a real attempt at external
validity, not mechanics-checking. **But** the bottom line of my prediction
survives: it's *manual-execution* (operator pastes prompts, saves answers), and
it has **not yet produced retrieval-lift evidence** — that's an explicit pending
gate in both the buffer and Gap 3/6. So: the *instrument* for escaping the
coherence trap now exists and is genuinely external (graded answers beat
self-consistent reconstruction). The trap is **not yet closed empirically** —
no data has come out of it. Structure built, measurement pending.

---

## What the divergences actually reveal

The protocol says the divergences are the instrument's output. Mine cluster into
one signature, and it's the coherence trap's fingerprint:

**I systematically predicted as "open concerns" things the system had already
named and partially closed.** Archival (#5), external validity (#6), even the
shape of the edge work (#2) — in each case the injected session-start context
had primed me with the *trajectory and recent wins*, so my "concerns" were
recapitulations of concerns the system was already tracking, one step behind the
actual state. That's exactly the failure mode the protocol warns about: the
reconstruction felt like independent analysis, but it was a draw from the same
distribution that wrote the gap table — so of course it agreed with the gap
table's *framing* while lagging its *progress*. Four consistent reconstructions,
one distribution.

The two genuine misses (#2 mechanism, #4 buffer) are where I substituted a
**prior** for a **reading**: "edge learning → ML" and "buffer → scratchpad" are
my defaults for what those words mean in other systems. The vault's actual
implementations are weirder and more specific (manual cold-start seeding;
surprise-held staging). The lesson is the verification rule applied to *myself*:
a confident reconstruction that names a mechanism is a claim about a prior, not
a reading of the code.

---

## Cross-checked finding: the semantic layer lags the code

The two daydream agents started from opposite corners of the graph (one seeded
on the current scoring work, one a random walk from game-theory knowledge
entries) and **independently hit the same structural problem:** semantic
memories that *theorize about the system* have drifted behind what shipped.

- The seeded walk found that `differential_access_source_weighting` still lists
  "where does access-event recording happen?" as an open question — but the Jun
  18 commit answered it: per-event source attribution is live in
  `access_events.jsonl`. The entry proposes a 5-level continuous weighting
  scheme; the commit shipped a **binary** version of it and the substrate for
  the full scheme is already built (`replayAccessEvents` is the only function
  that'd need to change). The semantic memory doesn't know its own proposal is
  half-implemented.
- The random walk found `auto_daydream_as_cls_replay_partial` catalogs four
  CLS-replay gaps but is missing a fifth: the daydream seed selector ignores
  `surprise_at_encoding`, even though biological replay is *defined* by
  preferential reactivation of high-prediction-error episodes. The field exists
  in every frontmatter; the selector never reads it.

(Both are candidate findings, not yet verified end-to-end — the binary-gating
claim is corroborated by the access-count-inflation buffer entry I read
directly; the seed-selector claim I have not confirmed against the selector
source. Per the verification rule, flag before acting.)

The throughline: **EXEC_SUMMARY is current (well-tended), but the semantic
memories that model the system are stale relative to the system.** This is the
same documentation-drift failure the CLAUDE.md "Post-Change Documentation" rule
exists to prevent — appearing one layer down, in the *memory* of the system
rather than its *docs*. The post-change checklist covers buffers, CLAUDE.md,
EXEC_SUMMARY, manifests — it does **not** say "re-examine semantic memories that
theorize about the subsystem you just changed." That's a gap in the protocol,
not just the content. When you ship a change to scoring, the semantic entry
*about* scoring theory should be a buffer-enrichment target. Right now nothing
makes that happen, so the theory layer rots quietly while the code moves.

---

## Net assessment

The system is in a healthy, honest state. The strongest evidence isn't any
single capability — it's that the gap table reframes rather than closes when the
honest answer is "reframe" (#5), keeps a 0.9-surprise problem alive in the
buffer rather than burying it (#4), and built a genuinely external measurement
harness even though it hasn't paid off yet (#6). Those are the moves of a system
optimizing for *not fooling itself*, which is the only thing that matters for an
endogenous-verification instrument.

The one thing I'd actually act on: **the semantic-layer drift is a real,
specific, cheap-to-fix finding that surfaced twice independently.** Two stale
entries, both one buffer-enrichment away from current, both flagged. Promote the
two daydream breadcrumbs and add "semantic memories theorizing about a changed
subsystem" to the Post-Change Documentation checklist. That closes a protocol
gap, not just two stale files.

What I got wrong, I got wrong in the system's favor — I underestimated it. That's
the better direction to be wrong in, but it's still the activation bias talking,
and worth naming as such.

---

### Predictions vs. outcomes (one-line ledger)

| # | Prediction | Outcome |
|---|---|---|
| 1 | Knowledge-dominated; ~149 living core | ✅ confirmed (420/569 knowledge) |
| 2 | Automated edge-weight learning, TDD | ◐ right target, wrong mechanism (manual cold-start bootstrap); TDD/green confirmed |
| 3 | Citation-gate + benchmark shipped, EXEC current | ✅ confirmed, no drift |
| 4 | Buffer 3, all today, surprise <0.7 | ✗ wrong — one is 06-18, surprise 0.9, held ×2 |
| 5 | Archival conservative to a fault | ◑ confirmed but already reframed by activation floors |
| 6 | Benchmark = mechanics, external validity open | ◐ mostly wrong (it's a real A/B harness); bottom line "not yet closed" survives |

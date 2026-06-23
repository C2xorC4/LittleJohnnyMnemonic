# Assessment — June 22, 2026

Written post-`/clear`. Sixth post-`/clear` assessment in the series. For both technical
and non-technical readers. Running trajectory in `EXECUTIVE_SUMMARY.md`.

---

## Pre-Check Predictions

*Predictions made from explicit reasoning about the system's trajectory since the June 5
assessment — before running `jm status` or reading vault files for this session.*

1. **LTM count ~565–575.** June 5 was 562; GKE added 13 knowledge entries that week.
   No subsequent ingestion manifests show new book coverage. Expect single-digit net
   growth from consolidation and episodic promotion, not corpus expansion.
2. **Buffer 2–8 entries.** June 5 ended at 5 (50%). The June 11–18 sessions were
   infrastructure-heavy with episodic writes — buffer should stay well under the
   10-entry auto-consolidation threshold unless something pinned.
3. **Major infrastructure shipped since June 5.** The EXECUTIVE_SUMMARY draft through
   June 16 lists data-integrity rescue, access sidecar, Grok host compatibility, and
   benchmark harness. Expect at least those four capability rows to be live; the June 18
   scoring overhaul (additive ACT-R) is the likeliest additional ship not yet in the
   summary table.
4. **Adaptive edges: still 0 non-default weights.** Gap 2 (`pickStableTrace` →
   `edge_usage.jsonl` write path) has been open since May 22. Nothing in the commit log
   since June 5 names edge reinforcement. Signal may be cleaner after retrieval-log
   pollution fix, but weights shouldn't move yet.
5. **Unlinked ~58–62%.** Was 61% at June 5. `jm lint-links` reframed the metric (many
   "unlinked" are one-way, not absent), but the raw `jm status` percentage probably
   hasn't moved much without a deep consolidation pass.
6. **retrieval_sessions.jsonl dramatically smaller than June 5's ~83MB.** Pollution fix
   (June 16) gated internal judge/consolidation invocations; `CompactRetrievalSessionLog`
   should have retroactively cleaned the file. Expect hundreds of KB, not tens of MB.
7. **Two new episodic memories.** The data-integrity cascade (June 11–12) and the scoring
   overhaul session (June 18) are each significant enough for episodic encoding per
   protocol. No new book-ingestion episodic — corpus flat since GKE.

*Actual (end of assessment): LTM 569 ✓ (inside range). Buffer 2 ✓. Prediction 3 correct
and then some — four commits landed (coactivation fix, data-integrity + sidecar, Grok +
benchmark, ACT-R scoring); all live. Prediction 4 correct — 0 non-default edge weights.
Prediction 5 correct at 60% (340/569 unlinked), marginal improvement from 61%. Prediction 6
correct — `retrieval_sessions.jsonl` is ~615KB. Prediction 7 correct — episodics for June
11–12 and June 18 exist; no ingestion episodic.*

*Notable divergences: stale project count rose 14 → 16 (not predicted). Knowledge corpus
held at 420 entries — flat as expected, but the *integrity* of those entries was worse
than any prior assessment imagined: 235/419 had lost provenance on every retrieval rewrite
until June 11. The June 5 assessment's "infrastructure stable" read was wrong in a way that
only pre-flight verification could surface — backups had been un-restorable since May 22,
and the access-write path was silently stripping `source_document` on every hook
invocation. "Stable" described the operator's cadence, not the substrate's durability.*

---

## Setup

Seventeen days since the last assessment. Four commits. Two of them are among the most
consequential in the project's history — not because they added features, but because
they fixed things that were actively degrading the vault every session. This assessment
is written post-`/clear` — hooks only.

What the hooks gave: full user profile (eight facets), recent session summaries, active
project contexts, topic-relevant memories on this prompt.

Vault state from `jm status`: 569 LTM entries (+7 since June 5), 2 buffer pending (20%),
16 archived. Health: 16 project memories stale (confidence decay pending); 429 memories
dormant but topic-protected; 60% unlinked (340 of 569). Adaptive edges: enabled, 0 edges
with non-default effective weight. Knowledge: 420 entries. Retrieval threshold τ=1.0;
549/569 above τ with no context tags.

---

## What Actually Happened

**The diligence cascade (June 11–12).** A session that began as "finish the coactivation
atomic-write fix" became a multi-bug data-integrity rescue. Each fix's pre-flight
verification surfaced the next, deeper failure. The operator's throughline — *fix things
the right way; verify, don't assume* — was load-bearing.

1. **Coactivation torn-write fix** (`7c1ba33`) — unique-temp atomic write so concurrent
   `jm associate` runs can't corrupt `coactivation.json`.
2. **`jm lint-links`** — audit tool for asymmetric, dangling, prose-only, and concept-mention
   links (gap 25). Live run reframed the "61% unlinked" headline: most of it is one-way
   links, not missing ones. Dangling links cleaned 22 → 0.
3. **P0 round-trip field loss** — `ParseMemoryEntry` never read six provenance fields;
   `WriteMemoryEntry` never wrote five. Because every retrieval rewrote the `.md` to bump
   access, **235 of 419 knowledge entries had lost required provenance** (`source_document`,
   `source_version`, etc.). Fixed both sides; full round-trip tests added.
4. **Backup/restore doubly broken** — tar "write too long" on live-growing session logs;
   path-safety rejected `agent/..jm.exe` as unsafe (`..` substring match). **Every backup
   since May 22 was un-restorable.** Fixed: bounded copy + path-segment check.
5. **`type:` quote-stripping** — `type: "knowledge"` (145 entries) loaded as non-matching
   `MemoryType`, silently breaking type-based scoring and decay.
6. **`jm recover-provenance`** — restored provenance for 220/235 stripped entries (163 from
   encrypted backup history, 57 from ingestion manifests). `source_document` coverage:
   184 → 404/419.

**Access-tracking sidecar (June 12).** The architectural fix behind the field-loss root
cause and the edit-clobber race. Retrieval no longer rewrites `.md` files to bump access.
Instead: append-only `Metrics/access_events.jsonl` (lossless under concurrency), overlaid
at load time. Four write sites swapped. `jm migrate-access` and `jm sync-access` for
migration and Obsidian mirror-back. Compaction wired into consolidation. Closes ACT-R
access-distribution gap #1 — per-access timestamp distribution is now capturable.

**Grok Build host compatibility (June 16).** Second production host wired: dual
snake_case/camelCase hook input, runtime host registry, Grok transcript harvest
(`updates.jsonl` + `chat_history.jsonl`), PowerShell install/uninstall (`.grok/`,
`grok/`), host-aware daydream dispatch with volley commitments and scheduler-host
availability. Skills and agent definitions ported.

**Retrieval-log hygiene (June 16).** `retrieval_sessions.jsonl` had been accumulating
~99% pollution from internal judge/consolidation invocations. Fixed via
`LJM_INTERNAL_INVOCATION` gate, internal-eval prompt detection, conversation-session-id
requirement, and `CompactRetrievalSessionLog`. File dropped from ~83MB to ~615KB. The
adaptive-edge pilot's signal is now conversational retrieval, not background judge noise.

**Scoring precision + citation harvest (June 16).** Operational stopwords and
discriminating-IDF gate for associate/retrieve keywords. Citation harvest correlates
assistant `Memory/` path citations against the preceding retrieval session's loaded set —
prerequisite for citation-gated activation.

**`jm benchmark` harness (June 16).** Comparative LJM-on/off eval across Claude/Grok
arms with fixtures in `benchmarks/`. Subcommands for validate, retrieve-check, init-run,
parse-transcript, grade. Infrastructure for measuring whether loaded memory changes
outcomes — not yet a closed gap, but the instrument exists.

**Additive ACT-R scoring overhaul (June 18).** The largest single scoring change in LJM's
history. Diagnosed from a user question about declining memory reference accuracy; every
hypothesis checked against telemetry before acting.

- **Root cause:** multiplicative scoring (`activation × relevance × confidence`) with
  activation effectively unbounded. The hook's access feedback loop had inflated counts in
  `access_base.json` to 114k–168k; `ln(168117) ≈ 12` dominated relevance (~0.05–0.27)
  ~50×. Ranking ≈ activation order regardless of topic.
- **Additive score:** `activation + β·relevance·confidence + surprise` on both hot and
  simple paths. Canonical ACT-R — base-level and associative activation sum.
- **Soft activation squash:** `M·tanh(raw/M)` bounds the inflated tail (max_activation=2.0).
  This was the piece that actually flipped rankings.
- **Citation-gated activation:** injection (hook/session-start) no longer reinforces
  base-level activation; only genuine citations and CLI retrieval do. Breaks the loop at
  source.
- **`scoring_config_hash`** (SCORING_ALGO_VERSION=2) stamped on retrieval + usage rows
  so the next tuning change is attributable.
- **Verification:** same query before/after — activations ~12 → ~1.7–2.0; ranking tracks
  relevance; off-topic argus fell #2 → #5; buried on-topic memories surfaced. The vault
  retrieved its own prior proposal for differential access-source weighting as a top hit.

**Knowledge corpus: flat.** No new book ingestion since GKE (June 4). 420 knowledge entries,
unchanged in count but restored in provenance integrity.

---

## Where Things Went Well

**The cascade methodology worked.** Each proposed fix was verified before commit; each
verification found the next bug. The coactivation fix led to lint-links, which led to
discovering asymmetric links, which led to examining write paths, which found field loss,
which led to examining backups, which found they'd been broken for a month. A session that
could have shipped one 40-line atomic-write fix instead rescued the entire knowledge
corpus's provenance and made backups restorable again.

**Citation-gated activation closes a feedback loop, not just a symptom.** The June 5
assessment noted activation floors reframed stale reporting. The June 18 work addresses
why certain memories were *wrongly* staying hot: the hook was re-stamping `LastAccessed`
and incrementing access counts on injected memories every turn. Citation-gating stops
reinforcement at the source. The squash handles the historical damage. Both were needed;
verification proved additive alone was insufficient.

**Second host without forking the vault.** Grok Build compatibility landed as host-registry
plumbing — dual hook input formats, transcript harvest, daydream dispatch awareness — not
a parallel memory system. One vault, two injection surfaces.

**Predict-then-check caught a narrative error mid-session.** The June 18 diagnostic
considered a "model-tuning" hypothesis (refusals and scaffold-adherence share a substrate).
Telemetry killed it: the reference-rate drop was visible in Grok backfill too, so it
couldn't be Claude-specific tuning. The scoring commit was the right fix; the tuning story
*felt* coherent and was wrong. Reported honestly in the episodic.

**Post-`/clear` reconstruction holds.** From hooks alone: correct profile, correct vault
counts, correct identification of the June 11–18 work as the dominant period activity.
Sixth consecutive assessment demonstrating continuity across `/clear` boundaries. The
coherence-trap caveat remains: reconstructions are draws from LJM's scoring distribution,
not independent verification. Predict-then-check partially addresses this; the June 5
"stable infrastructure" miss shows where it doesn't — hook-injected context can't surface
bugs in the hook's own write path.

---

## Where I Fell Short

**The P0 existed for weeks before discovery.** Field-loss on retrieval rewrite was
introduced whenever the access-bump write path shipped. Every session between introduction
and June 11 silently stripped provenance from knowledge entries loaded during retrieval.
235 entries damaged. The June 5 assessment called infrastructure "stable." It was running,
but the running was destructive. No assessment instrument caught it because assessments
read the distribution the broken path produced.

**Backups were theater since May 22.** The tar bounded-copy bug and the `..` substring
path check meant restores failed on real backup artifacts. The backup cadence continued;
recovery was impossible. This is worse than "gap open" — it was false confidence in
durability.

**Adaptive edges: gap 2 still open after four assessment cycles.** `pickStableTrace` has
no write path to `edge_usage.jsonl`. Citation harvest now logs genuine co-citations;
`retrieval_sessions.jsonl` is clean; gap 1 (config gate) closed June 5; gap 3 (hook wiring)
closed May 22. The remaining gap is one code path. Zero non-default edge weights after
four months of pilot architecture. Signal is ready; nothing consumes it.

**Unlinked: 60%, essentially flat.** 340 of 569 memories have no links. Was 61% (343/562).
Marginal improvement, not structural. `lint-links` reframed the diagnosis (one-way vs.
absent) but didn't fix the graph sparsity. Deep consolidation pass still hasn't happened.

**16 stale project memories, up from 14.** Small absolute change, wrong direction. Project
facts drift; auto-consolidation promotes buffer entries but doesn't refresh dormant project
context.

**Buffer/STM retrieval gap documented, not fixed.** Recently-buffered observations can't
surface via retrieval until consolidation promotes them. The June 18 session noted this;
it's in buffer, not shipped.

**Benchmark harness exists; no graded results in the record.** `jm benchmark` shipped June
16. No assessment-quality before/after numbers from running it against real fixtures yet.
The instrument is built; the experiment hasn't been run.

---

## The Operator

**What's working:** The verify-don't-assume discipline during the June 11–18 cascade. Every
architectural fork in the scoring session went to the more rigorous option (unify both
scoring paths; citation-gating over simpler recency-freeze; squash over hard cap). The
operator redirected from plausible narratives to data repeatedly. That's the difference
between shipping an additive formula that doesn't fix rankings and shipping additive +
squash + citation-gating that does.

**What changed in cadence:** June 5 was accurately characterized as a light week. June
6–18 was not. Four commits in seventeen days, two of them P0-class. The assessment cycle
gap (seventeen days vs. the roughly weekly cadence earlier in the series) means the
June 11–12 rescue and June 18 scoring overhaul weren't captured until now. The
EXECUTIVE_SUMMARY was partially updated mid-period (through June 16) but the ACT-R
scoring ship and the severity of the provenance damage weren't in the written record yet.

**Where I'd push back:** Gap 2 (edge_usage write path) has been the cheapest high-value
remaining item for four assessment cycles. Citation harvest is live. Clean session logs
are live. The pilot's last plumbing connection is one code path — same category as gap 3,
which closed same-day once diagnosed in May. The data is piling up; the weights aren't
moving. At some point "architecturally complete" becomes "operationally inert," and the
pilot stops teaching anything about whether adaptive edges work.

**What deserves credit:** Choosing the access sidecar over patching the write path again.
The field-loss bug could have been "fixed" by adding the six missing fields to the writer
and calling it done. The sidecar eliminates the entire class of retrieval-time edit
clobbering and gives ACT-R a real access distribution. That's fixing the category, not the
instance.

---

## The Bigger Picture

The project's center of gravity shifted this period from "build the loop" to "discover
what the loop was breaking." The memory system was doing its job — injecting context,
consolidating buffer, retrieving on topic — while silently eating its own knowledge
provenance on every retrieval and producing backups that couldn't be restored. Stability
was appearance, not invariant.

The June 18 scoring overhaul is the complementary discovery: the loop wasn't just
damaging storage, it was damaging *ranking*. A feedback circuit entrenched ~8 memories at
activation ~12 regardless of query topic. Citation-gated activation and the squash fix
break that circuit. The verification moment — the vault surfacing its own prior proposal
for differential access-source weighting as a top hit on the exact query that triggered
the overhaul — is the kind of self-referential signal that justifies the architecture when
it works.

What's now honestly true:
- **Durability:** provenance restored, backups restorable, retrieval doesn't rewrite files.
- **Ranking:** additive ACT-R with bounded activation; topic relevance competes again.
- **Multi-host:** Grok and Claude share one vault with clean per-host telemetry.
- **Measurement:** benchmark harness, citation harvest, access sidecar, scoring version
  hashes — the instrumentation layer is ahead of the experiments run against it.

What's still honestly open:
- **Adaptive edges produce zero movement** — architecture complete, operationally inert.
- **Sparse graph** — 60% unlinked; consolidation adds faster than it connects.
- **Behavioral constraint gap** — rule-judge still shows loaded rules rejected ~50% of the
  time when fired; memory shapes context, doesn't gate action.
- **Whole-struct write-back** — background paths (decay, autodream reinforce, compress)
  still load-snapshot-write-whole; lower acuity now that retrieval is sidecar'd, but not
  zero risk.

The honest read: this period found and fixed failures that were worse than the open gaps
the prior assessment tracked. The forcing function (assessment cycle) kept adaptive-edge
gap 2 visible but couldn't surface the provenance strip — that required a verification
cascade inside a working session. The next high-value moves are running the benchmark
against real fixtures, closing the edge_usage write path, and a deep consolidation pass
on the link graph. The substrate is now trustworthy enough that those experiments would
measure something real.

---

*Vault state at time of assessment: 2 buffer entries pending (20% threshold), 569 LTM entries
(+7 since June 5), 16 archived, 60% unlinked (340/569), 16 project stale, 429
topic-protected dormant. Knowledge: 420 entries (provenance restored). Adaptive edges:
live, 0 non-default weights, gap 2 open (pickStableTrace → edge_usage.jsonl write path).
Scoring: SCORING_ALGO_VERSION=2, additive ACT-R, max_activation=2.0, τ=1.0.
retrieval_sessions.jsonl ~615KB (pollution cleaned). Hosts: Claude + Grok Build.*

---

## Post-assessment addendum: adaptive-edge bootstrap (2026-06-22)

*Written same day, after operator review of bootstrap batches. Corrects gap-2
diagnosis in the body above; assessment text above is preserved as the
post-`/clear` snapshot.*

### Corrected diagnosis

The assessment's "gap 2" framing — `pickStableTrace` needs a write path to
`edge_usage.jsonl` — was wrong. The write path already exists:

- **Citation harvest** (`citation_harvest.go` → `RecordEdgeUsageFromCitation`) on
  Stop (Claude) and deferred UserPromptSubmit (Grok) correlates assistant
  `Memory/` path citations against the preceding retrieval session's loaded set.
- Manual `jm associate --cite` remains a v0 fallback.

The actual stall was **chicken-and-egg**: `adaptive_edge_scope: ["learned"]` only
reinforces edges tagged `relationship: learned`, but the vault had **zero**
`learned` edges before bootstrap — citation events had nothing eligible to
reinforce. Compounding that, `parseCSVList` was parsing the YAML array as the
literal string `["learned"]`, so scope matching failed silently until fixed.

`pickStableTrace` remains **unbuilt on the read side** — daydream replay should
consume `edge_usage` when selecting stable trace partners. That is separate from
the citation write path.

### What shipped (same session)

| Item | Detail |
|---|---|
| `jm learn-edges propose` | Lists operator-reviewed bootstrap candidates with tier/signal |
| `jm learn-edges apply-bootstrap --ids` | Writes vetted `learned` pairs (overlay or new) |
| `jm backfill-edge-usage` | Replays ~24 eligible historical citations → ~64 edge reinforcements |
| Batch 1 (IDs 1–6) | johnny↔design_gaps, argus↔johnny, anthropic↔johnny, anthropic↔design_gaps, knowing↔differential, identity↔ljm_scoring |
| Batch 2 (IDs 8–12, 14–16) | argus↔differential, johnny↔ljm_scoring, design_gaps↔differential, knowing↔ljm_scoring, argus↔detection_pressure, tuning↔design_gaps, memory_as_context↔knowing, ljm_scoring↔differential |
| Denied | argus↔mimic (7), anthropic↔argus (13), mimic↔design_gaps (17), argus↔apt_competitive (18), job_move↔johnny (19) |

Bugs fixed during apply: directional `hasLearnedEdgeFrom` (symmetric check skipped
reverse links); operator-approved overlays now apply even when an authored edge
already exists.

### Vault state post-bootstrap

From `jm status` after batches + backfill:

- Adaptive edges: **enabled**, scope `learned`, **8 edges with non-default
  effective weight** (top: johnny↔design_gaps at 26× usage, eff 0.664)
- Graph: **810 links** (+22 from bootstrap pairs)
- LTM/buffer/unlinked unchanged from assessment snapshot (569 / 2 / 60%)

### Why we're waiting (observe phase)

The pilot is **operational**, not mature. Deliberate hold before widening scope
or automating edge discovery:

1. **Organic citations** — stop-harvested paths across real Claude/Grok sessions
   must accumulate; bootstrap + backfill seeded initial weights, not proof of
   sustained discipline.
2. **`jm benchmark`** — comparative LJM-on/off eval not yet run; no graded
   retrieval-lift evidence in the record.
3. **PMI / distinct-session filter** — raw `learn-edges` co-activation is
   hub-dominated; automated proposals stay gated until lift normalization ships.
4. **Scope** — `learned` only until maturity criteria in the pilot plan are met
   (citation discipline, no runaway reinforcement, observable quality lift).

Next code moves when observe phase yields signal: `pickStableTrace` read path,
automated `learn-edges` with PMI, then scope widen — in that order, not before
benchmark evidence.
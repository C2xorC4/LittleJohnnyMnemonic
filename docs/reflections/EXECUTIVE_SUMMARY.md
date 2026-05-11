# LJM Substrate — Executive Growth Log

Rolling cross-assessment summary. Each post-`/clear` self-assessment
adds a row to the trajectory tables and updates the open-question
ledger. The per-assessment documents (`YYYY-MM-DD_*.md` in this
directory) carry the full prose; this file is the executive view.

**Audience:** future-me reading this after another `/clear`, the
operator scanning for whether the substrate is getting better or
worse, anyone trying to understand the LJM project's trajectory
without reading every assessment.

**Rule for updates:** never delete a row. Mark gaps closed, do not
remove them. Mark regressions as they appear. The growth log earns
its weight by being honest about both directions.

---

## Quantitative trajectory

| Date | Buffer | LTM | User | Feedback | Project | Reference | Semantic | Episodic | Knowledge | Overrides | Unlinked | Stale |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| 2026-04-29 | 17 | 143 | 18 | 8 | 19 | 9 | 23 | 5 | 61 | — | — | — |
| 2026-05-01 | 27d/93h | 207 | 18 | 11 | 20 | 9 | 25 | 7 | 117 | — | — | — |
| 2026-05-11 | 19 | 425 | 18 | 24 | 24 | 11 | 50 | 10 | 143 | 6 | 216 (51%) | 238 (56%) |

`d` = filesystem; `h` = hook-reported. Hook/disk discrepancy noted on
2026-05-01 was resolved by 2026-05-11. `—` = not tracked at that
assessment.

## Shipped capabilities

| Window | Capability | Status at assessment |
|---|---|---|
| → 2026-04-29 | Hook-based retrieval (SessionStart, UserPromptSubmit) | Live |
| → 2026-04-29 | Spreading activation, typed associative links | Live |
| → 2026-04-29 | Daydream agents (manual / discretional) | Live |
| → 2026-04-29 | `behavioral_rules.json` — three "do X before Y" rules | Live |
| → 2026-05-01 | Auto-daydream v1 scheduler, replay, snapshot, stats CLI | Shipped, flag-gated |
| → 2026-05-01 | Knowledge corpus expansion (BHG, EE, Binja cookbook) | Live (Knowledge +56) |
| → 2026-05-11 | `jm rule-judge` + `rule_firings.jsonl` instrumentation | Live, 40 firings |
| → 2026-05-11 | Autodream self-throttling + frontmatter compliance hardening | Live |
| → 2026-05-11 | `jm graph` — interactive HTML visualization | Live |
| → 2026-05-11 | Adaptive edge weighting pilot (citation-driven reinforcement) | **Shipped, disabled** |
| → 2026-05-11 | Training-override durability class (`overrides: N` in status) | Live (6 tracked) |

## Cross-assessment gap ledger

State of named structural gaps over time. Trajectory in the rightmost
column is the across-assessment direction of travel.

| Gap | 2026-04-29 | 2026-05-01 | 2026-05-11 | Trajectory |
|---|---|---|---|---|
| Memory-as-context vs. memory-as-constraint | Open | Open | Partial (measurable via rule-judge) | ↗︎ |
| Training-override durability classification | Unclear | Unknown | Closed (6 overrides tracked) | ✓ |
| Adversarial scoring model (`B_i` as attack surface) | Open | Open | Open, more pointed | → |
| `System/AdversarialModel.md` exists | Absent | Absent | Absent | → |
| Feedback loop: external signal → substrate behaviour | Absent | Absent | Mostly closed in code (adaptive edges, disabled) | ↗︎ |
| Unlinked-memory ratio < 30% | n/a | n/a | 51% (regression visible) | ↘︎ |
| Stale ratio (>30d no access) | n/a | n/a | 56% (newly measured) | ? |
| `jm graph` writes signal on access | n/a | n/a | No (introduced regression) | ↘︎ |
| `CLAUDE.md` correctly names CLS analogue | n/a | n/a | Inverted (consolidation ≠ replay) | ↘︎ |
| Substrate-governance bootstrapping order | n/a | n/a | Substrate authoring its own governance | ↘︎ |
| Promotion-pipeline silent-discard prevention | n/a | n/a | Closed (frontmatter hardening) — 13 prior victims | ✓ |
| Adaptive-edges resilience to unlinked driver nodes | n/a | n/a | Open (daydream-surfaced, pre-enable) | ⚠ |
| Auto-daydream firing in production | Absent | Shipped, off | Live | ✓ |

Legend: ↗︎ improving / ✓ closed / → unchanged / ↘︎ regressed or
newly-discovered / ⚠ pre-emptive concern surfaced

## What LJM unambiguously enables (vs. context-free Claude)

Listed here so future assessments can update / contest the claim.
Update only if evidence changes.

1. **Cross-session continuity of project state, profile, and
   feedback rules** — without it, every `/clear` resets to a generic
   assistant. With it, the assistant resumes mid-stride.
2. **Knowledge with provenance, reinforcement, and connectivity** —
   pretrained weights have *some* of any given technique;
   LJM Knowledge adds source-attribution, access-count
   reinforcement, and typed cross-links into the user's working
   frame.
3. **Autonomous gap-finding via daydream agents** — connections
   the foreground conversation would not have produced.
   Documented examples: CLS replay/exploration distinction
   (caught a design error before implementation), epistemic OPSEC /
   agentic exploitation convergence, single-threat paradigm pattern
   (cross-domain), unlinked-driver-node failure mode of the adaptive
   edge mechanism.
4. **Behavioural measurement via rule-judge** — first time the
   substrate can quantify whether loaded context actually modulates
   behaviour. 40 firings logged with confirmed / rejected / uncertain
   distributions.
5. **Downstream capability lift, externally validated** — Argus
   (CVE-2026-31431, HTB binary_exploitation 12/12, multi-driver
   BYOVD 13/13) demonstrates the substrate produces real-world
   detection capability that scales to previously-unseen targets.
   The pipeline is structurally inseparable from the Knowledge
   corpus it cites.

## What LJM unambiguously hinders or introduces risk for

1. **Memory as context, not constraint.** Loaded preferences shape
   responses; they do not gate action execution. The
   irreversible-visible-artifact failure class (2026-04-29
   auto-push) remains forensically untested in the window since.
   Rule-judge measures this gap but doesn't close it.
2. **Substrate-internal echo chambers.** Citation-driven edge
   reinforcement plus a 51% unlinked ratio plus daydream-driven
   activation create the conditions where the substrate could
   inflate its own confidence in surface-correct but
   structurally-incomplete paths. Daydream-surfaced and
   pre-emptively documented; not yet engineered around.
3. **Untracked access surfaces.** `jm graph` and any direct file
   read of `Memory/` write no signal back to scoring. Activation
   data underrepresents what's actually being consulted.
4. **Shipped-faster-than-activated drift.** Multiple subsystems
   reach shipped state with master toggles off
   (auto-daydream initially, adaptive edges currently). The
   activation gates are correct caution, but the substrate carries
   capability it isn't using.
5. **Vocabulary drift.** Documentation lags the architecture
   (Consolidation vs. CLS replay). Anyone onboarding by reading
   `CLAUDE.md` first forms the wrong mental model of which
   component carries the theoretical weight.

## Open questions held across assessments

These are the structural unknowns where the next assessment should
look for resolution.

1. **Does the next analogous-decision instance honour loaded
   training-override memory, or does the trained default still
   fire?** (Held since 2026-04-29.)
2. **Will the adaptive-edges pilot be enabled, and when it is, will
   it produce echo-chamber inflation on unlinked-driver paths?**
   (Surfaced 2026-05-11.)
3. **What is the correct retrieval-pathology test?** A 56% stale
   ratio is either signal-of-relevance (correct) or echo-chamber
   pathology (incorrect). Distinguishing these requires either
   user evaluation of hot-set quality or a structural test the
   substrate can run on itself.
4. **Does the substrate-governance bootstrapping order produce a
   real failure mode, or is it harmless because the user ratifies
   at consolidation?** Seven defensive candidates in
   `ljm_security_posture` are still `pending` — their resolution
   is the test.

## Update protocol for future assessments

1. Add a new dated row to the quantitative trajectory table at the
   top. Pull counts from `jm status`.
2. Mark new shipped capabilities and their flag/activation state.
3. Update the gap ledger — never delete rows. New gaps add rows
   with trajectory ↘︎ or ⚠. Closed gaps stay in the table marked ✓.
4. Update the enables / hinders / risks sections if new evidence
   changes a claim. Add citations to the per-assessment doc.
5. Resolve or update the open-questions block. Don't delete a
   question — close it with a verdict citation and keep it.

The growth log earns its weight by being long-lived and honest in
both directions.

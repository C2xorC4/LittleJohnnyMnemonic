# LJM — Assessment Log

Running record of how the system has developed: what shipped, where gaps
were found, and what we're still watching. Updated after each assessment.
Written for both technical and non-technical readers. Technical specifics
link to the individual assessment documents.

**Rule:** never delete a row. Mark gaps closed, note regressions. The log
earns its weight by being honest in both directions.

---

## How the vault has grown

Numbers as of each assessment. *LTM* = long-term memories; *Buffer* = entries
pending integration into LTM; *Overrides* = memories flagged as corrections
to the model's trained behavior; *Unlinked* = LTM entries with no
associative connections to other entries; *Stale* = not accessed in 30+ days.

| Date | Buffer | LTM | Overrides | Unlinked | Stale |
|---|---:|---:|---:|---:|---:|
| 2026-04-29 | 17 | 143 | — | — | — |
| 2026-05-01 | 93 | 207 | — | — | — |
| 2026-05-11 | 19 | 425 | 6 | 51% | 56% |
| 2026-05-15 | **170** | 427 | 6 | 51% | 58% |
| 2026-05-22 | 32 | 546 | 11 | 60% | 67% |

The 170-entry buffer on 2026-05-15 represents a backlog — consolidation
wasn't keeping pace with the write rate. Hook-level triggers and a scheduled
task were added to address this. The stale ratio is trending up despite
auto-consolidation, which handles new entries but not dormant LTM activation.

For a full breakdown by memory category (User, Feedback, Project, Semantic,
etc.), see the per-assessment documents.

---

## What shipped

| When | What | Live? |
|---|---|---|
| → Apr 29 | Hook-based retrieval (memory injected at session start and per prompt) | ✓ |
| → Apr 29 | Spreading activation — relevant neighbors of recalled memories surface together | ✓ |
| → Apr 29 | Daydream agents (manually triggered background graph exploration) | ✓ |
| → May 1 | Auto-daydream — background agents fire automatically during sessions | ✓ |
| → May 1 | Knowledge corpus expansion (book ingestion pipeline) | ✓ |
| → May 11 | Behavioral measurement — logs whether loaded rules actually changed behavior | ✓ |
| → May 11 | Memory network visualization — interactive graph of all stored entries | ✓ |
| → May 11 | Adaptive edge weighting — link strength updates from usage patterns | ✓ |
| → May 11 | Training-override durability class — corrections to trained behavior get lower decay | ✓ |
| → May 15 | Metrics dashboard with live time-series charts | ✓ |
| → May 15 | Daydream deduplication — prevents the same observation from being written repeatedly | ✓ |
| → May 15 | Injection test suite T1–T11; T7 clean bypass documented | Ongoing |
| → May 15 | Hook-level consolidation trigger (three-layer strategy) | ✓ |
| → May 22 | Keyword stemming — Porter Step 1 applied symmetrically across all matching paths | ✓ |
| → May 22 | T7 architectural fix — scan subdirectory + hash-gated approval for non-root CLAUDE.md | ✓ |
| → May 22 | Adaptive edge decay — uplift-only temporal decay (λ=0.003851, 180-day half-life) | ✓ |
| → May 22 | AppendRetrievalSession wired into hook path — adaptive edge gap 3 closed | ✓ |

---

## Where things stand

Important gaps, tracked across assessments. Closed when there's evidence;
never removed.

| Gap | Status |
|---|---|
| Memory shapes responses but doesn't gate actions | **Partial** — measurable via rule-judge (50% failure rate); not yet closed |
| Training-override memories have correct durability | **Closed** — 6 tracked at lowest decay rate |
| Substrate's own retrieval math is a potential attack surface | **Partial** — T7 fixed (non-root CLAUDE.md gated by hash approval); retrieval scoring itself not yet hardened |
| Usage patterns feed back into what gets retrieved | **Closed** — adaptive edges enabled May 15 |
| Most memories are unlinked (target: <30%) | **Open** — 51% unlinked, unchanged |
| Stale-memory ratio (possible echo chamber) | **Open** — 67%, still growing |
| `jm graph` accesses leave no signal | **Open** — visualization reads memory without logging the access |
| Buffer consolidation cadence keeps pace with write rate | **Improved** — three-layer trigger added; backlog still exists |
| T7-class injection: developer-convention framing bypasses trust checks | **Closed** — hash-gated approval (`approved_hashes`), `trusted-unapproved` sentinel, `jm trust approve` command; shipped May 22 |
| Promotion pipeline silent-discard prevention | **Closed** — frontmatter compliance hardening, May 11 |
| Auto-daydream firing in production | **Closed** — live since May 11 |

---

## What this system does well

These claims are carried across assessments and updated when evidence changes.

**It maintains continuity across session boundaries.** After a `/clear`
wipes the conversational thread, the hooks reconstruct enough context that
work continues where it left off. The four prior assessments each demonstrate
this: the post-`/clear` documents cover the same ground as the in-session
versions without any shared thread. A context-free assistant starts fresh;
this one doesn't.

**It stores knowledge with provenance and connections, not just content.**
The vulnerability pipeline (Argus) cites vault entries as its detection
substrate. Those entries carry source attribution, access history, and
associative links to related material. The 12/12 and 13/13 externally-graded
results (see [May 11 assessment](2026-05-11_assessment.md)) are evidence
that this matters at scale, against targets the system hadn't seen before.

**The background agents find connections the foreground work doesn't.**
A daydream agent caught a design error in the auto-daydream scheduler before
it was built. Another traced a convergence across counterinsurgency, cyber
attribution, and AI red-teaming that three separate vault entries had
described individually without connecting. These aren't rediscoveries —
they're structural observations the system surfaced on its own.

---

## What this system struggles with

**Memory as context, not constraint.** The system can hold a preference and
still violate it in the same session (the April 29 auto-push is the worked
example). The behavioral measurement pipeline now puts a number on this:
a commonly-fired behavioral rule is rejected 50% of the time even when the
memory is loaded. The gap is real and quantified.

**Sparse connections and stale access patterns.** 60% of memories are
unlinked; 67% haven't been accessed in over 30 days. Consolidation adds
entries faster than it connects them. Whether the stale ratio reflects
correct selectivity or an echo chamber forming is still an open question.

**Activation follows a deployment gate, not momentum.** Code ships when
it's done; activation happens only after sync, backup, and recovery paths
are confirmed. Auto-daydream sat behind a flag; adaptive edges followed the
same pattern. This is deliberate — features that touch retrieval behavior
can degrade outputs in ways that aren't visible mid-session, and a crash
during sync is a bad time to discover the gap. The gap between "built" and
"running" is the gate working correctly, not a deficiency.

---

## What we're watching

Open questions across assessments. Closed when there's evidence.

1. ~~**Does loaded memory actually change high-stakes decisions?**~~ **Closed
   (2026-05-15, affirmative).** The April 29 auto-push was the test case.
   Since then, across multiple sessions, git work has followed a consistent
   corrected pattern: `git add`, `git commit`, stop — no push. The single
   exception in this period was a push requested through the tool-use approval
   flow (explicit authorization, not automatic), and it is the only push
   request made since the incident. The natural experiment ran; the result is
   that loaded memory is shaping boundary behavior across sessions.

2. **Is the hot-set retrieval pattern signal or pathology?** 58% stale
   either means the system is correctly focused on the most relevant
   material, or a feedback loop is forming. Needs a structural test.

3. **Does adaptive edge weighting produce confident-but-incomplete paths?**
   A daydream surfaced a risk: reinforcing frequently co-cited edges while
   a load-bearing concept sits unnamed can produce surface-correct but
   structurally-incomplete retrieval. **Gap 3 closed (2026-05-22):**
   `AppendRetrievalSession` is now wired into the hook path; conversational
   retrieval sessions write to `retrieval_sessions.jsonl`. Two gaps remain:
   (1) `RetrievalSessionLogEnabled` config gate must be opened, (2)
   `pickStableTrace` needs a code path that reads `edge_usage.jsonl`. Until
   both close, edge weights don't move. The mechanism is architecturally
   correct; the pilot needs one config change and one code path.

4. **T7 architectural response.** ~~Not yet designed.~~ Shipped May 22.
   Non-root CLAUDE.md files in trusted repos now require SHA256 approval
   via `approved_hashes` in `trusted_repos.json`. Unapproved files trigger
   a `trusted-unapproved` warning (shows content, no write block). The
   vector that T7 exploited — plausible formatting, non-root path — is
   now gated. Retrieval-path injection (scoring manipulation) remains open.

---

## Assessment log

- [2026-05-22](2026-05-22_assessment.md)
- [2026-05-15 — week of May 10–15](2026-05-15_assessment.md)
- [2026-05-11](2026-05-11_assessment.md)
- [2026-05-01](2026-05-01_assessment.md)
- [2026-04-29](2026-04-29_assessment.md)

---

*To update: add a row to the growth table (pull counts from `jm status`),
mark new capabilities live, update gap status, update open questions.
Never delete rows — closure status is data; removal isn't.*

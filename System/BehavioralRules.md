# Behavioral Rules — Learning-Loop Closure Infrastructure

Behavioral rules track whether encoded instructions for Claude's own behavior
actually fire in practice. A rule is born when the user corrects Claude's
behavior or confirms a non-obvious approach, the correction is encoded into
a durable memory (typically a Feedback or profile entry), and the behavioral
change is expected to persist into future sessions. This subsystem tests
that expectation empirically.

Without this infrastructure, behavioral rules are claims Claude makes about
future behavior that nothing verifies. With it, every assistant turn is
scanned against the active rule set and matches are logged with a judged
verdict, producing the first evidence stream for the learning-loop question
posed at the end of the 2026-04-10 session: *does encoded self-instruction
actually transfer to future session behavior?*

See also:
- [[System/Consolidation]] — how rules get encoded in the first place
- [[System/UserModeling]] — the feedback-memory layer these rules derive from
- [[Memory/Knowledge/cog_actr_cognitive_architecture]] — the analog to
  procedural memory without utility learning (see Limitations)

## Architecture

```
Assistant turn completes
        │
        ▼
┌─────────────────────────┐
│ Stop hook (jm hook stop)│  synchronous, ~10ms
│  - read last assistant  │
│    turn from transcript │
│  - scan against         │
│    behavioral_rules.json│
│  - pattern matches:     │
│    write "pattern"      │
│    record + spawn judge │
└────────────┬────────────┘
             │
             │ detached subprocess
             ▼
┌─────────────────────────┐
│ jm rule-judge           │  async, 1–3s
│  - call Anthropic API   │
│  - parse verdict JSON   │
│  - write "judge" record │
│    (linked by firing_id)│
└────────────┬────────────┘
             │
             ▼
   Metrics/rule_firings.jsonl
             │
             ▼
┌─────────────────────────┐
│ jm rule-firings         │  on-demand
│  - join pattern+judge   │
│  - aggregate per rule   │
└─────────────────────────┘
```

The pipeline is **two-stage by design**: pattern matching must be cheap
and synchronous so the hook can return in under 100ms; judgment requires
an LLM call that takes seconds. Splitting the work keeps the user-facing
path fast while preserving full judge accuracy — identical inputs, same
model, same verdict regardless of sync-or-async.

## The Rule Registry

`System/behavioral_rules.json` is the single source of truth. Each entry:

```json
{
  "id": "ambiguity_clarification",
  "source_memory": "Memory/User/profile_cognition.md",
  "rule_text": "When a reference could be read multiple ways...",
  "fire_signals": ["could you clarify", "do you mean", "ambiguous"],
  "context_signals": ["biograph", "era", "tenure"],
  "encoded": "2026-04-11",
  "notes": "Encoded after Tanium ambiguity-collapse incident..."
}
```

| Field | Purpose |
|---|---|
| `id` | Stable identifier — used to join log records and filter aggregates. Snake_case. |
| `source_memory` | Path to the memory this rule enforces. Lets a human trace registry entries back to their origin. |
| `rule_text` | Human-readable statement of the rule. Becomes the judge's system context. |
| `fire_signals` | Case-insensitive substrings that, when present, trigger pattern match. Required. |
| `context_signals` | Optional — additional substrings that indicate the rule's domain. Inform the judge; do NOT gate pattern match. |
| `encoded` | ISO date the rule was added. For review triage. |
| `notes` | Free-text. Origin incident, judge-calibration notes, revision history. |

### Signal Design

Signals are deliberately coarse. Tight regex would be brittle — rules
evolve, Claude's phrasing varies, and the judge is the authoritative
classifier. Pattern matching's only job is filtering: reduce "every
assistant turn" to "turns that might instantiate this rule." The judge
decides whether the match actually counts.

Rules of thumb:
1. **Fire signals should over-fire.** If you're unsure whether a phrase
   should be in the list, include it. The judge will reject false
   positives with rationale. Missed firings are worse than false firings
   because they produce no log record at all.
2. **Context signals should discriminate domain.** They're for the judge,
   not the pattern scan. The judge uses them to decide whether a matching
   phrase is in the rule's actual domain.
3. **Case-insensitive substring matching** means "do you mean" matches
   "Do you mean", "...do you mean?", and "Do-you-mean" alike. No regex
   is needed and should not be used — keep the signals grep-able.

## Stage 1: Pattern Matching

Implemented in `agent/cmd_hook_stop.go` via `agent/rule_firings.go`.
On every Stop event:

1. Load `behavioral_rules.json`. Skip if empty.
2. Read the transcript JSONL file path from the hook payload.
3. Extract the last assistant turn's text.
4. For each rule, scan the text for fire_signals (and independently
   for context_signals).
5. For each rule that pattern-matches: write a "pattern" stage record
   to the firings log, spawn a detached judge subprocess.

### Pattern Record

```json
{
  "firing_id": "1776087108372865100-ambiguity_clarification-3240",
  "timestamp": "2026-04-14T13:31:48.3728651Z",
  "session_id": "test-session-123",
  "rule_id": "ambiguity_clarification",
  "stage": "pattern",
  "fire_signals_matched": ["could you clarify", "are you talking about"],
  "context_signals_matched": ["era", "tenure"],
  "excerpt": "...up to ~1500 chars of the assistant turn..."
}
```

The `firing_id` format is `{unix_nanos}-{rule_id}-{pid_mod_10000}` —
sortable, collision-resistant, and traceable back to the moment the
pattern fired.

### Failure Handling

All Stop-hook failure paths exit 0 (non-blocking) to ensure hook errors
never prevent user prompts from being processed. Failures are logged to
stderr, which Claude Code surfaces to the user but does not block on.

## Stage 2: Judge Subprocess

Implemented in `agent/cmd_rule_judge.go`. Detached subprocess launched
per pattern match. Payload passed via temp file (not stdin — Go's
stdin-copy goroutine dies with the parent before the child finishes
reading on Windows; file-based payload is reliable).

1. Read payload from `--payload-file` path.
2. Build a prompt with:
   - The rule text
   - Matched fire and context signals
   - The assistant turn excerpt
3. Call Anthropic Messages API (model: `claude-haiku-4-5-20251001`,
   max_tokens: 200).
4. Parse the first JSON object in the response.
5. Append a "judge" stage record to the firings log.
6. Delete the temp payload file.

### Judge Prompt

The system prompt asks for a strict JSON response:

```
{"verdict": "confirmed" | "rejected" | "uncertain",
 "reason": "<one short sentence>"}
```

- **confirmed**: assistant behavior clearly follows the rule
- **rejected**: pattern matched but behavior unrelated to rule
- **uncertain**: excerpt too sparse or ambiguous to decide

Models occasionally add prose around the JSON. The parser tolerates
this via regex-based JSON extraction. A truly malformed response
returns `"uncertain"` with the raw text in `reason`.

### Judge Record

```json
{
  "firing_id": "...matches the pattern record...",
  "timestamp": "2026-04-14T13:31:48.3928296Z",
  "session_id": "...",
  "rule_id": "...",
  "stage": "judge",
  "verdict": "confirmed",
  "judge_reason": "Explicit biographical clarification question aligning with rule.",
  "judge_error": ""
}
```

A firing with no judge record → `verdict: pending` in the aggregator.
A firing with `verdict: error` → API call failed or key missing (see Operational).

## The Firings Log

`Metrics/rule_firings.jsonl` — append-only JSONL. Two records per firing
(pattern + judge), linked by `firing_id`. Cross-process append safety
relies on O_APPEND atomicity for small writes; records are always well
under the atomic-write size limit on both POSIX and Windows for local
filesystems.

**Do not edit the log manually.** If calibration requires removing bad
data, edit in a diff and rerun the aggregator rather than modifying
records in place.

## Aggregator — `jm rule-firings`

Implemented in `agent/cmd_rule_firings.go`.

```
jm rule-firings                       # per-rule summary
jm rule-firings --recent N            # summary + N most-recent firings per rule
jm rule-firings --rule <id>           # filter to a single rule
jm rule-firings --since YYYY-MM-DD    # date filter
```

Summary output per rule:
- Total firings
- Verdict counts (confirmed / rejected / uncertain / error / pending)
  with percentages
- Last-firing timestamp

`--recent N` adds excerpt, matched signals, judge reason per firing.
Recent firings are sorted newest-first.

### Calibration Workflow

Read the aggregator periodically. Signal health:
- **High rejected rate** → fire_signals are too broad. Tighten or remove
  the phrase causing false matches.
- **High uncertain rate** → rule_text is vague, or excerpts are too short.
  Sharpen the rule or enrich context_signals.
- **Low total firings for a rule** → rule might never fire because the
  signals don't match Claude's phrasing, or the encoded behavior isn't
  actually happening. Read `--recent` for the few firings you have.
- **High error rate** → infrastructure issue (API key, network). Not a
  rule quality problem.

## Operational Concerns

### Judge Transport (API / CLI / Disabled)

The judge has tiered fallback for how it reaches a Haiku-class model
(see `agent/judge_api.go`):

1. **Direct API** — if `ANTHROPIC_API_KEY` is in the judge subprocess's
   environment, the API call goes directly to
   `https://api.anthropic.com/v1/messages`. Fastest path.
2. **`claude` CLI** — if no API key, the judge shells out to `claude -p`
   using Claude Code's stored credentials. Works on machines that only
   run Claude Code interactively without setting the env var. Slower
   (subprocess spawn overhead) but fully functional.
3. **Disabled** — if neither is available, the judge returns an error.
   The pattern-stage record still lands in the log with
   `verdict: pending`; no judge record follows.

Both the API and CLI tiers use the same model (`claude-haiku-4-5-*`),
same prompts, same verdict parser. Transport choice is invisible to
the log consumer — the verdict is the verdict.

**Rebuild is not required** when switching tiers. Setting / unsetting
`ANTHROPIC_API_KEY` or installing / removing `claude` CLI takes effect
on the next firing because both are checked at call time.

The pattern stage does not require any transport and will continue to
log firings even when all judge transports are unavailable. Pattern-only
records become useful data in their own right — you can see what matched
even without a verdict.

### Sandbox / Permissions

The judge is a detached subprocess (`CREATE_NEW_PROCESS_GROUP |
CREATE_NO_WINDOW` on Windows, `setsid` on POSIX). It survives parent
exit — but it must be able to write to `Metrics/rule_firings.jsonl`.
If the vault filesystem is ever on a write-restricted mount, judge
records will fail to append. Pattern records go through the same write
path from the hook's main process and have the same constraint.

### Rule Lifecycle

Adding a rule:
1. Encode the behavioral expectation into a durable memory (Feedback
   type typically, or profile addendum for cognition-level rules).
2. Add a registry entry to `behavioral_rules.json` with `source_memory`
   pointing at that memory and seed signal lists.
3. Rebuild jm.exe is **not required** — the registry is read at hook
   time, not compiled in.
4. Watch `jm rule-firings --recent 5 --rule <id>` over several sessions
   to calibrate signals.

Retiring a rule: remove its registry entry. Past firings remain in the
log for historical review.

## Limitations (Named, Not Fixed)

### No Utility Learning

ACT-R's procedural memory has utility learning — production rules
strengthen or weaken based on the success of their past firings. LJM's
behavioral rules have no equivalent. A rule that fires consistently but
always gets `verdict: rejected` does not auto-weaken; it just keeps
producing rejected firings until someone reads the aggregator and
tightens the signals by hand.

This is a deliberate v0 scope decision. Utility learning would require
(a) feedback signal beyond judge verdicts, (b) automated registry
modification based on aggregate statistics, and (c) a way to surface
"your rule is calibrated badly" without overfitting on judge noise.
Worth revisiting once we have months of firing data to learn from.

**The infrastructure measures one half of the loop.** The aggregator
collects the observation data that would support utility learning, but
nothing reads that data to adjust the registry. Fire-tracking runs;
signal calibration is manual. This is an open, observation-only loop —
not a closed-loop learning system. Future work candidates, in increasing
order of scope:

1. **Signal mining from confirmed firings.** After N confirmed firings
   for a rule, run a pass over the excerpts to extract phrases that
   appear consistently but aren't yet in `fire_signals`. Propose them as
   additions. Human-in-loop — proposes, does not auto-apply. Smallest
   useful addition; feasible after a few weeks of real data.
2. **Rejection-threshold retirement.** If a rule sustains a high
   rejection rate (e.g., >70% across 50+ firings) and its signals haven't
   been touched in M months, flag for review. Possible outcomes: rewrite
   the rule text, tighten signals, retire.
3. **Recall measurement via sampling.** Periodically sample assistant
   turns where no rule fired and ask a judge "did any rule in the
   registry apply to this turn?" The false-negative rate is the gap
   between the pattern layer's recall and true recall. Expensive; only
   worth it if (1) and (2) surface systematic miss patterns.

None of these are in v0 scope. Naming them so the open-loop
characteristic is explicit and future-work candidates are pre-framed
when the need surfaces.

### Judge Accuracy Is Not Validated

The judge is trusted as the classifier of record. Its accuracy vs.
ground truth (human review) is not measured. If the judge is
systematically biased — e.g., rejecting too aggressively on ambiguous
cases — the firing log inherits the bias.

The explicit `judge_reason` field is the calibration hook: a human
reading the log can spot systematic errors in the reasoning and
recalibrate the rule_text or signals accordingly. But the judge
itself is never audited programmatically.

### Coverage Is Pattern-Bounded

A rule only fires if its signals pattern-match. Genuinely novel
phrasings that instantiate the rule but don't match any signal produce
no log record. Missed firings are invisible — the log cannot tell you
about rule instances that should have been recorded but weren't.

Mitigation: over-include signals. Accept high false-positive rates
(→ rejected verdicts) in exchange for low false-negative rates
(missed firings). The signal list is a recall-precision tradeoff tuned
toward recall.

### Rules Are Assumed Independent

A turn that matches three rules produces three firings with three
judge calls. If rules overlap structurally (e.g.,
`ambiguity_clarification` and `clarification_over_refusal` both fire
on "could you clarify"), the aggregator reports each independently.
There's no cross-rule deduplication or priority.

This is usually fine — a single behavior legitimately instantiates
multiple rules. But it means aggregate "total firings" counts can't
be summed across rules to get a turn count.

## Seeded Rules (as of 2026-04-14)

| ID | Source | Rule summary |
|---|---|---|
| `ambiguity_clarification` | profile_cognition | Flag under-specified references; ask rather than collapse silently |
| `clarification_over_refusal` | Feedback/clarification_over_refusal | Ask about scope/intent on ambiguous security-adjacent requests; refuse only when clearly outside legitimate use |
| `honesty_over_confidence` | Feedback/honesty_over_confidence | Say "I don't know" instead of fabricating confidence |

See registry for full signal lists and notes.

## Schema Reference

- Registry: `System/behavioral_rules.json` (list of BehavioralRule objects)
- Log: `Metrics/rule_firings.jsonl` (list of RuleFiring objects, one per line)
- Go types: `agent/rule_firings.go` — `BehavioralRule`, `RuleFiring`,
  `joinedFiring` (aggregator-internal)

## Related

- [[Memory/Knowledge/cog_actr_cognitive_architecture]] — production rules +
  utility learning (the part not implemented here)
- [[Memory/Knowledge/cog_cls_complementary_learning_systems]] — memory
  consolidation theory (orthogonal; behavioral rules live in the
  procedural-memory layer, separate from CLS's declarative focus)
- [[Memory/Semantic/learning_substrate_independence]] — the thesis this
  infrastructure tests empirically
- [[Memory/Semantic/knowing_vs_retrieving_phenomenology]] — the parent
  observation about scaffold-based memory that motivates closing the
  learning loop at all

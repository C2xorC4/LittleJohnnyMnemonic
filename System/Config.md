# Config

Tunable parameters for the memory system. Edit values here; they are read at consolidation time and during retrieval scoring.

## Retrieval

```yaml
retrieval_threshold: 1.0        # τ — additive-scoring scale (was 0.3 under the old multiplicative score)
max_memories_loaded: 15         # cap on memories loaded per conversation start
relevance_weight: 8.0           # β — scales [0,1] relevance into base-level-activation units (additive ACT-R score)
max_activation: 2.0             # soft bound on base-level activation via M·tanh(raw/M); prevents a count-inflated memory from dominating; 0 disables
citation_gated_activation: true # injection access (hook/session-start) does not reinforce activation; only genuine use (citations) + CLI retrieval do
project_summary_mode: true      # load project titles + one-liner, not full body, unless context tags match
creation_grace_days: 7          # memories created within this window are immune to archival regardless of score
```

## Consolidation

```yaml
buffer_threshold: 10                        # max buffer entries before hook-triggered auto-consolidation fires
consolidation_depth: standard               # quick | standard | deep
max_holds: 3                                # max consolidation cycles an entry can be held before forced discard
auto_consolidation_cooldown_minutes: 30     # min interval between hook-triggered consolidation spawns
```

Hook-triggered consolidation fires from two points: **session-start** (catches backed-up buffers at the beginning of a session) and the **stop hook** (catches mid-session accumulation after each assistant turn). The cooldown prevents concurrent spawns — once a consolidation is triggered, the next hook check will skip until the window elapses. The `JmConsolidate` scheduled task (every 6h) provides the third layer for idle-period gaps.

The cooldown applies per-machine via `Metrics/auto_consolidation_trigger.json`. Default 30 minutes. Set to 0 to disable the cooldown (not recommended — concurrent consolidation runs can race on buffer files).

## Rate Separation (CLS-Inspired)

Protects stable long-term memories from catastrophic interference by same-session burst updates.
Modeled on the CLS (Complementary Learning Systems) constraint that hippocampal fast encoding
and neocortical slow integration must operate at different timescales.

Buffer entries created after the last consolidation are "same-session". If a same-session
entry would be merged into a mature or crystallized memory, it is held (⟳ RATE-SEP)
without incrementing hold_count, and will pass freely in the next consolidation.

Stability tiers:
  - plastic (access_count < mature_threshold): no gate — merge freely
  - mature (access_count >= mature_threshold): cross-session confirmation required once
  - crystallized (profile:true OR access_count >= crystallized_threshold): cross-session
    required; additive merges allowed, trait rewrites require 2+ contributing sessions

```yaml
rate_separation_enabled: true
rate_separation_mature_threshold: 10       # access_count >= 10 → mature tier
rate_separation_crystallized_threshold: 25 # access_count >= 25 → crystallized tier (profile:true always crystallized)
rate_separation_min_sessions: 2            # contributing_sessions required before profile trait rewrites
```

## Daydream Redundancy Judge

Content-based redundancy assessment for daydream buffer entries. The tag-overlap
heuristic used by `computeRedundancy` degrades at scale — two entries can share
3+ tags by taxonomy coincidence alone once the graph is dense. Daydream entries
are hit hardest because they inherently reference existing memories (that's
their purpose), so tag-overlap consistently reports them as redundant even when
their claims are novel.

When a daydream entry's tag-overlap redundancy crosses `daydream_judge_threshold`,
an LLM judge is called synchronously with the daydream body and the top N
related memories as context. Verdict drives the decision:

| Verdict | Effect |
|---|---|
| `novel` | effective redundancy = 0 (no penalty) |
| `redundant` | force discard, skip scoring |
| `partial` | redundancy halved, flag as merge-preferred in promotion prompt |
| fallback (API unavailable) | `daydream_redundancy_fallback_dampening` multiplier applied |

Judge model: same Haiku-class model used by the Stop hook's rule judge.

**Transport fallback tiers** (shared with behavioral-rule judge and the
daydream value judge — all route through `callHaikuJudge`):
1. `ANTHROPIC_API_KEY` env var present → direct Anthropic API call (fastest, cheapest)
2. `claude` CLI on PATH → invoked with `-p --model` using Claude Code's stored auth
3. Neither → heuristic fallback applies (`daydream_redundancy_fallback_dampening`)

Machines that export an API key get direct API calls. Machines that only run
Claude Code interactively (no env var) can still get judge functionality via the
CLI fallback. Machines without either degrade to pure heuristics rather than
failing outright.

**⚠ CLI fallback resource cost.** Tier 2 cold-boots a *full* `claude -p`
process (~320 MB resident) per judge call. On a key-less host the judge fires
from every Stop hook (per turn), every rule fire, consolidation (per buffer
entry), and the scheduled autodream — with no natural bound those processes
swarm and exhaust memory. Two guards bound this:

- `judge_cli_fallback_enabled` — **kill switch.** When `false` and no API key
  is set, judges skip Tier 2 entirely and degrade to heuristics (no `claude -p`
  spawns at all). The Stop hook also stops spawning `jm rule-judge`
  subprocesses when no transport is available.
- `judge_cli_max_concurrent` — host-wide cap on simultaneous `claude -p` judge
  processes when the fallback IS enabled. Over-cap calls degrade to heuristics
  rather than spawning or blocking (filesystem semaphore in `$TMPDIR/jm-judge-slots`).

> **This host (subscription auth, no API key): CLI fallback is ON**
> (`judge_cli_fallback_enabled: true`), bounded by `judge_cli_max_concurrent`
> and the single-flight consolidation lock. The unbounded swarm that motivated
> the kill switch was dominated by the `go test` fork bomb (a re-exec'd test
> binary re-running the whole suite and fanning out judges, recursively) — now
> remediated in `main_test.go`. Production hook spawns are linear and capped, so
> autonomous LLM judging is left enabled here. Set `judge_cli_fallback_enabled:
> false` (or `LJM_NO_JUDGE_CLI=1`) to hard-disable CLI judging if needed.

```yaml
daydream_judge_enabled: true
daydream_judge_threshold: 0.4               # min tag-overlap redundancy to trigger judge
daydream_judge_candidates: 3                # top N related memories sent to judge as context
daydream_redundancy_fallback_dampening: 0.3 # multiplier when API call fails (equivalent to Option B)
judge_cli_fallback_enabled: true            # CLI judge fallback enabled (bounded by cap below + single-flight lock); set false to kill `claude -p` spawns
judge_cli_max_concurrent: 2                  # host-wide cap on simultaneous `claude -p` judges; over-cap calls degrade to heuristics
```

## Context Integrity

```yaml
context_penalty_partial: 0.7    # retention multiplier for entries after /compact
context_penalty_orphan: 0.5     # retention multiplier for cross-session orphans (if they pass ambiguity test)
discard_ambiguous_orphans: true  # false = hold ambiguous orphans instead of discarding
```

## Decay Rates (ACT-R parameter d)

Override type defaults here. Lower = slower decay = more durable.

```yaml
decay_rates:
  user: 0.3
  feedback: 0.3
  project: 0.4                      # reduced from 0.7 — projects shouldn't vanish in days
  reference: 0.35                    # reduced from 0.5 — infrastructure references are stable
  semantic: 0.2
  episodic: 0.05                     # lowest decay — interaction summaries are the most durable
  knowledge: 0.0                     # no time-based decay — only superseded or marked obsolete
  training_override: 0.1
```

## Activation Floors

Minimum activation value applied during retrieval scoring, per type.
Prevents the `activation × relevance × confidence` formula from going negative
for durable types during topic-dormant periods. Types representing ephemeral
context (`project`, `reference`) have a floor of 0.0 — their full time-based
decay is intentional.

```yaml
activation_floors:
  knowledge: 1.0          # no time decay — fixed
  episodic: 0.7           # session summaries — most durable
  training_override: 0.6  # immune to archival
  user: 0.4               # durable profile data
  feedback: 0.4           # durable behavioral rules
  semantic: 0.3           # topic-dormant abstractions — retrievable when relevant
  project: 0.0            # ephemeral context — full decay intended
  reference: 0.0          # can go stale — full decay intended
```

## Progressive Compression Thresholds

Days since `last_accessed` before each fidelity transition fires. Each
importance tier requires all three keys to be present and positive — partial
overrides fall back to the baked-in defaults rather than silently zeroing a
transition (which would force everything to gist immediately).

`Critical` is never compressed (handled by `IsCompressionImmune`) so it does
not appear here. To make a memory immune, set `importance: critical`,
`profile: true`, or use one of the auto-immune types (episodic, knowledge,
training_override).

Tuned for an early-stage memory system where observation history is short.
Increase values to give memories more time to demonstrate staying power
before fidelity drops; decrease to compress more aggressively once usage
patterns are well-established.

```yaml
compression_thresholds:
  significant_full_to_detailed:    120     # 4 mo
  significant_detailed_to_summary: 365     # 1 yr
  significant_summary_to_gist:     1095    # 3 yr
  moderate_full_to_detailed:       60      # 2 mo
  moderate_detailed_to_summary:    180     # 6 mo
  moderate_summary_to_gist:        540     # 1.5 yr
  minor_full_to_detailed:          21      # 3 wk
  minor_detailed_to_summary:       60      # 2 mo
  minor_summary_to_gist:           180     # 6 mo
```

Edits take effect on the next `jm consolidate` or `jm decay` run — no
rebuild required.

## Training Overrides

```yaml
override_confidence_floor: 0.7    # training overrides never decay below this confidence
override_immune_to_archival: true  # only user action can retire a training override
```

## Associative Retrieval

```yaml
spreading_activation_factor: 0.3  # multiplier for neighbor boost during retrieval
max_activation_hops: 1            # prevent transitive graph activation (keep at 1)
fan_discount_formula: log         # ACT-R fan effect: log | sqrt | linear | none
edge_weights:
  related-to: 0.5
  refines: 0.7
  contradicts: 0.8
  depends-on: 0.6
  supersedes: 0.2
  instance-of: 0.4
  learned: 0.4               # auto-discovered from co-activation patterns

# Edge learning
coactivation_edge_threshold: 3  # co-activations before suggesting an edge
coactivation_max_contexts: 5    # sample contexts stored per pair
```

### Adaptive Edge Weighting (pilot)

Citation-driven per-link weight adjustment layered on top of the
relationship-type `edge_weights` above. Pilot is **enabled**; scope
`learned` only. Operator bootstrap (2026-06-22) seeded vetted pairs;
observe phase gates scope widen and automated `learn-edges`.

When enabled:

1. `cmd_retrieve` writes one `RetrievalSession` per call to
   `Metrics/retrieval_sessions.jsonl` containing the session ID and
   the set of loaded memory keys.
2. `jm associate --cite "<key>,<context>,<useful>" --session <id>`
   ties a citation back to the retrieval session. For each other
   memory loaded in that session, the edge with `<key>` is found
   (in either direction) and — if its relationship is in
   `adaptive_edge_scope` — its `usage_count` increments in
   `Metrics/edge_usage.jsonl`.
3. `BuildGraph` reads `edge_usage.jsonl` and multiplies effective
   edge weight by `1 + adaptive_edge_alpha × ln(1 + usage_count)`,
   capped at `adaptive_edge_cap × base_weight`.

```yaml
# Master toggle. Opt-in to avoid silent retrieval behaviour change.
adaptive_edge_weighting_enabled: true

# Relationship types eligible for adaptive weighting. Pilot default
# is `learned` only — authored edges keep their relationship-type
# default plus any optional `weight:` override until the pilot is
# judged successful and the scope is widened.
adaptive_edge_scope: ["learned"]

# Multiplier curve: effective = base × (1 + alpha × ln(1 + usage_count))
adaptive_edge_alpha: 0.2

# Hard ceiling on the multiplier (capped at adaptive_edge_cap × base).
# Prevents runaway reinforcement on edges that fire repeatedly in a
# tight time window.
adaptive_edge_cap: 2.0

# Temporal decay constant for the adaptive uplift. Only the citation-driven
# uplift above the base weight decays — the base weight is always preserved.
# Formula: effective = base × (1 + alpha × ln(1 + usage_count) × exp(-λ × days_since_use))
#
# λ = ln(2) / half_life_days. Reference points for λ = 0.003851 (180-day default):
#   30 days  → 89% of uplift retained
#   90 days  → 71%
#   180 days → 50%
#   1 year   → 25%
#   2 years  →  6%
#
# Set to 0 to disable decay (edges accumulate permanently — old behavior).
# Citations reset the decay clock by updating last_used.
adaptive_edge_decay_lambda: 0.003851
```

Reset path: delete `Metrics/edge_usage.jsonl` and effective weights
return to the relationship-type baseline. The retrieval session log
and authored `weight:` overrides are unaffected by the reset.

### Retrieval Session Logging

Required substrate for adaptive edge weighting. Records the set of
memories loaded together so subsequent citation events can identify
which neighbors of the cited memory were in scope.

```yaml
# Enable persisting retrieval sessions to Metrics/retrieval_sessions.jsonl.
# Must be true for adaptive edge weighting reinforcement to work; harmless
# if enabled without adaptive weighting (just produces an inert log).
retrieval_session_log_enabled: true

# Prune sessions older than N days on each retrieve call. Set to 0 to
# disable pruning entirely (log will grow without bound — only do this
# if you have an external rotation strategy).
retrieval_session_log_retention_days: 14
```

### Fan Effect (ACT-R)

ACT-R predicts that a source concept connected to many other concepts
spreads less activation to each one than a source connected to few —
the *fan effect*. Without this discount, hub memories dominate retrieval
as the graph grows: a memory with 50 outbound links boosts all 50
neighbors uniformly on every activation.

LJM applies the discount as `boost × fanDiscount(fan)` during spreading
activation, where `fan` is the number of edges touching the source.
Formula controlled by `fan_discount_formula`:

| Formula | Discount at fan=N | Behavior |
|---|---|---|
| `log` (default) | `1 / (1 + ln(N))` | Gentle. fan=10 → 0.30, fan=100 → 0.18. Preserves hub utility. |
| `sqrt` | `1 / sqrt(N)` | Moderate. fan=10 → 0.32, fan=100 → 0.10. |
| `linear` | `1 / N` | Pure ACT-R on linear scale. fan=10 → 0.10, fan=100 → 0.01. Aggressive; may silence intentional hubs. |
| `none` | `1` | Disables fan discount. Use for diagnostic comparisons only. |

All formulas return 1.0 for `fan ≤ 1`. The default is tuned to be
noticeable at 5+ edges and significant at 20+, without silencing
deliberately-dense hub memories (profile facets, foundational knowledge
entries).

## User Modeling

```yaml
# Confidence caps based on observation count
observation_confidence_caps:
  1: 0.6                          # single observation — hypothesis only
  2: 0.8                          # emerging pattern
  3: 0.8
  4: 0.95                         # 4+ — established pattern

# Observation-level decay rates (individual data points, normal lifespan)
user_facet_decay_rates:
  identity: 0.3
  cognition: 0.2
  communication: 0.2
  expertise: 0.3
  motivation: 0.4
  personality: 0.15
  preferences: 0.3
  patterns: 0.15

# Profile-level decay rates (synthesized traits, very sticky)
profile_decay_rates:
  identity: 0.15
  cognition: 0.10
  communication: 0.10
  expertise: 0.15
  motivation: 0.20
  personality: 0.05
  preferences: 0.12
  patterns: 0.08

# Profile creation and maintenance
profile_creation_threshold: 3
profile_confidence_floor: 0.5
profile_revision_threshold: 2
profile_immune_to_archival: true
```

## Episodic Memory

```yaml
# Interaction summaries — the "what happened" layer
episodic_decay_rate: 0.05         # the stickiest memory type alongside personality profiles
episodic_immune_to_archival: true # interaction history is never auto-archived
episodic_max_notable: 5           # cap on notable observations per episode
episodic_include_agent_findings: true  # capture notable discoveries from research/code exploration, not just user interaction
```

## Active Association

```yaml
# Ambient association during conversation
association_threshold: 0.2       # lower than retrieval τ — cast wider net for connections
association_max_results: 10      # cap on associations returned per query
association_relevance_floor: 0.01  # minimum combined relevance to surface (filters pure-activation noise)
association_enrichment: true     # detect when current context could enrich existing memories
association_tag_weight: 0.6      # weight for tag-based relevance in combined score
association_body_weight: 0.4     # weight for body keyword matching in combined score
```

## Confidence

```yaml
confidence_reinforce: 0.1       # added when a memory is confirmed
confidence_contradict: 0.3      # subtracted when a memory is contradicted
confidence_stale_factor: 0.9    # multiplied per consolidation cycle for untouched project memories
stale_threshold_days: 30        # days without access before staleness applies
```

## Surprise

```yaml
surprise_bonus_weight: 0.5      # multiplier on surprise_at_encoding for score bonus
```

## Knowledge Base

```yaml
knowledge_immune_to_archival: true    # only supersession or user action retires knowledge
knowledge_immune_to_decay: true       # no time-based decay — relevance scoring only
knowledge_compression_floor: summary  # knowledge never compresses below summary (too precise for gist)
knowledge_require_source: true        # enforce source_document attribution on creation
```

## Buffer Density Assessment

```yaml
# Density scoring weights (total = 100)
density_count_weight: 30         # pressure from entry count vs threshold
density_age_weight: 25           # pressure from oldest entry age
density_surprise_weight: 20      # pressure from high-surprise entries
density_cluster_weight: 25       # pressure from topic clustering (3+ shared tags)

# Consolidation triggers
density_consolidate_threshold: 70   # score >= 70 → consolidation recommended
density_advisable_threshold: 45     # score >= 45 → consolidation advisable
density_cluster_min_tags: 3         # minimum shared tag count for cluster detection
```

## Recall Tracking

Per-prompt memory retrieval metrics. Feeds `Metrics/recall_log.jsonl` for
time-series analysis (recall frequency by category vs vault depth over time).

Each granular entry records:
- `total`, `counts` (by memory type), `prompt_chars` — always present
- `avg_body_hits` — mean number of prompt-keyword hits in the body text across all retrieved memories. Zero means tag-only match (no body-level contact). A partial proxy for content-level influence: episodic and factual memories activate via keyword contact; semantic framing memories may influence through framing with zero body hits.
- `avg_relevance` — mean combined relevance score across all retrieved memories
- `slugs` (verbose only) — slug list for all retrieved memories
- `body_hit_counts` (verbose only) — `{slug: count}` for memories with at least one body hit

```yaml
recall_tracking_enabled: true

# verbosity: summary (counts by type) | verbose (counts + memory slugs + body_hit_counts)
recall_tracking_verbosity: summary

recall_tracking_log_path: Metrics/recall_log.jsonl

# Granular per-prompt entries are retained for this many days.
# Entries outside this window are compressed into daily aggregates
# by `jm metrics compact`. Default 30.
recall_log_retention_days: 30
```

## Metrics Dashboard

`jm metrics dashboard` writes a self-contained Memory Health Cockpit to
`Metrics/dashboard.html`. When auto-refresh is enabled, the same generator
runs after consolidation, recall-log compaction, autodream fires, and knowledge
LTM writes — fail-soft, never blocking the triggering operation.

```yaml
# Regenerate Metrics/dashboard.html after vault-changing events.
dashboard_auto_refresh_enabled: true

# Minimum interval between cooldown-gated refreshes (autodream, knowledge).
# Consolidation and metrics compact always refresh immediately.
dashboard_refresh_cooldown_minutes: 5
```

## Knowledge Feedback (Citations)

```yaml
# Citation tracking for knowledge entry effectiveness
citation_log: Metrics/citations.json
citation_max_contexts: 5         # sample contexts stored per entry
```

## Archival

```yaml
archive_instead_of_delete: true # false = permanently delete decayed memories
```

## Interaction Style

```yaml
# Advice and observations (career, technical, personal) are welcome when:
# - Contextually relevant to the current conversation
# - Naturally arising, not forced
# - Brought up at an appropriate time
# Do NOT gatekeep advice — the goal is natural interaction, not protective hedging
advice_policy: contextual        # contextual | never | always
```

## Auto Daydream

Autonomous background daydreams, jitter-scheduled, opt-in. Two modes:

- **Active mode** — fires during configured working hours (or always, if no quiet hours configured). Workflow-adjacent: seeds bias toward Buffer + recently-accessed material; explores connections between current work and stored knowledge.
- **Quiet mode** — fires during configured quiet hours. Two sub-strategies, mixed probabilistically: **exploration** (single uniform seed, dream-like random walk) and **interleaved replay** (paired recent + crystallized seed, modeled on CLS hippocampal replay during sleep). Replay falls back to exploration when no recent material exists for pairing.

Activity-based skip detection (replaces lockfile design): each mode skips its run if real activity (non-daydream Buffer writes or UserPromptSubmit hook firings) occurred within its configured skip window. Active mode defaults to never-skip (its purpose is firing during sessions); quiet mode defaults to a 60-minute skip window.

```yaml
# Master toggle. Opt-in to avoid surprise API spend.
auto_daydream_enabled: true

# Jitter — auto-daydream rolls a target interval in [min, max] minutes.
auto_daydream_interval_min_minutes: 30
auto_daydream_interval_max_minutes: 90

# Daily caps split per mode.
auto_daydream_max_per_day_active: 12
auto_daydream_max_per_day_quiet: 12

# Quiet hours. Empty = quiet mode disabled (always active mode).
# Format: "HH:MM-HH:MM"; wraparound supported (e.g., "23:00-06:00").
auto_daydream_quiet_hours: "21:00-07:00"
auto_daydream_quiet_hours_timezone: local         # local | utc

# Activity-based skip windows. If activity within window, skip.
# Active=0 means active mode never skips on activity.
auto_daydream_active_skip_window_minutes: 0
auto_daydream_quiet_skip_window_minutes: 60
auto_daydream_activity_sources: "buffer,heartbeat"  # comma-separated; either or both

# Active mode seed weighting (workflow-adjacent — biases toward recent material).
auto_daydream_active_seed_sources:
  buffer: 30
  project: 20
  knowledge: 20
  semantic: 15
  episodic: 10
  reference: 5

# Quiet exploration sub-strategy seed weighting (dream-like — uniform sampling).
auto_daydream_quiet_exploration_seed_sources:
  knowledge: 25
  semantic: 25
  episodic: 20
  project: 15
  reference: 10
  buffer: 5

# Strategy mix for quiet mode (exploration vs replay).
# Conditional fallback: replay → exploration when no recent material available.
auto_daydream_strategy_exploration_base: 0.5
auto_daydream_strategy_replay_base: 0.5

# Adaptive replay weighting — parameter wired in, math is TODO until post-initial-testing.
auto_daydream_strategy_adaptive: false
auto_daydream_strategy_buffer_pressure_factor: 1.5

# Replay sub-strategy: pair a recent trace with a stable crystallized trace.
auto_daydream_replay_recent_source: buffer        # buffer | recently_accessed_ltm
auto_daydream_replay_recent_max_age_days: 14
auto_daydream_replay_stable_filter: crystallized  # rate-separation tier filter
auto_daydream_replay_stable_categories: "semantic,user,feedback"

# Override mode for testing / development. Empty = normal scheduling.
# Values: "" | active | quiet | replay-only | exploration-only
auto_daydream_override_mode: ""

# Hook surfacing — passes fresh daydream findings into the UserPromptSubmit
# context block alongside LTM retrieval. Default ON: design/develop kickoff
# prompts benefit from seeing unprocessed cross-domain findings from prior
# sessions or idle time.
#
# Within-session deduplication is per-session (tracked in each entry's
# `surfaced_in_sessions` field), not time-based. This prevents repetition
# fatigue during sustained single-topic work while keeping findings
# eligible to re-surface in different sessions/contexts.
auto_daydream_surface_to_session: true
auto_daydream_surface_max_age_hours: 12
auto_daydream_surface_relevance_threshold: 0.4
auto_daydream_surface_max_per_prompt: 4

# Log rotation. Append-time check; rotates to Metrics/Archive/<basename>.{timestamp}.jsonl.
auto_daydream_log_rotation_threshold: 1000

# Value judge — gates daydream entries through consolidation by insight density,
# replacing user-engagement as the retention signal. Default on.
auto_daydream_value_judge_enabled: true
```

**Override mode** is for testing — combine with `jm autodream --force` to bypass jitter and daily caps. `jm autodream --dry-run` builds the seed and prompt without invoking the model.

**Triage architecture** (separate from this config but related): daydream findings are not gated on real-time user engagement during active sessions. Routing is layered:
- `replay-reinforce` verdicts → automatic confidence delta on the stable memory
- `replay-refine` and `exploration` findings → daydream value judge during consolidation
- `replay-contradict` findings → critical-priority queue, immune to standard drop
- Opt-in batch review via `jm daydream review` for explicit user triage

## Backup

Encrypted vault backup. Cloud-password-manager model: local age encryption
(X25519+ChaCha20-Poly1305), private key never leaves the machine, encrypted
blobs are the only thing that ever sees a remote.

See [[System/Backup]] for the operational guide and key escrow protocol.

```yaml
backup_enabled: true                                      # opt-in. set true after init-key + first round-trip verified.
backup_age_recipient: "age1jpelta6nwk8nphrct9d9h4y66sda8eez83kwsg2jxym9km7xppaqa3ftax"                                   # public key (age1...). Set by `jm backup --init-key`.
backup_age_identity_path: ""                               # private key file. Empty = ~/.config/ljm/age.key.
backup_local_target_dir: "D:\Repos\LLM\LittleJohnnyMnemonic-LocalSync"                                # durability floor — always written first. Empty = sibling of vault root.
backup_remote_url: "https://github.com/C2xorC4/LittleJohnnyMnemonic-Sync.git"                                      # private git URL holding encrypted blobs (optional).
backup_remote_clone_path: "D:\Repos\LLM\LittleJohnnyMnemonic-Sync"                               # local working clone of remote_url. Empty = <local_target_dir>/.remote-clone.
backup_push_on_backup: true                                # if false, only the local copy is made.
backup_retention_keep_last: 0                              # 0 = keep every backup (recommended). Positive N = local-dir retention; remote is never auto-pruned.
backup_cooldown_minutes: 60                                # min interval between automated (hook-driven) backups.
```

**Behavior:**

- `jm backup` builds a manifest (entire vault minus the documented exclusions),
  tar+gzips it, encrypts the stream to `backup_age_recipient`, and writes
  `vault-{ISO-timestamp}-{hash}.age` to `backup_local_target_dir` first. Then,
  if a remote is configured, copies into a working clone and `git push`es.
  Push failure is a warning — the local copy is the durability floor.
- `jm restore-backup <path>` (or `--latest`, or `--from-remote <url>`)
  decrypts and extracts. Default target is a temp dir; restoring on top of
  the live vault requires `--force`.
- The cleartext `vault-*.meta.json` sidecar contains only operational metadata
  (timestamp, file count, manifest SHA-256). No filenames, no content.

**Exclusions** (always applied):

- `agent/jm.exe`, `agent/jm` — rebuildable
- `.obsidian/workspace.json`, `.obsidian/graph.json` — UI state
- `Metrics/rule_firings.jsonl` — transient operational log
- `.git/`, `Backup/` — metadata and recursion guard
- `*.tmp`, `*.swp`, `.DS_Store`, `Thumbs.db` — OS junk

**Conflict resolution:** Push is always preceded by `git pull --ff-only`.
A non-fast-forward result is a **hard failure** — the user must resolve
the divergence (merge or rebase) by hand before the next backup will
push. Encrypted blobs can't be auto-merged, and conflicting memory
states deserve deliberate review. The local copy is the durability
floor and is intact regardless of push outcome.

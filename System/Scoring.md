# Scoring

## Retrieval Model

When a conversation begins or context is needed, memories are scored and ranked. Only memories above the retrieval threshold τ are loaded. This prevents context window pollution with low-value memories.

## Score Computation

```
score(m, q) = activation(m) + β · relevance(m, q) · confidence(m) + surprise_bonus(m)
```

Where:
- `m` = a memory entry
- `q` = the current query/context
- `β` = `relevance_weight` (default 8.0) — scales the [0,1] relevance term into
  base-level-activation units so relevance can steer ranking.

**This is additive, not multiplicative** (changed 2026-06-18, `SCORING_ALGO_VERSION = 2`).
The old form `activation × relevance × confidence` let base-level activation — effectively
unbounded — dominate relevance ~50× in the product, so ranking collapsed to activation
order and recently/frequently accessed memories crowded out topically relevant ones. In the
additive form base-level activation and the relevance term **add** (canonical ACT-R, where
base-level and spreading/associative activation sum), so a strongly on-topic dormant memory
can out-rank an off-topic active one. Confidence **weights the relevance term** rather than
multiplying the whole score, so a high-activation low-confidence memory can't be dragged to
the top by raw activation alone.

### Component 1: Activation (ACT-R Base Level)

```
activation(m) = ln(Σ t_j^(-d))
```

- `t_j` = hours since the jth access (from `last_accessed` and `access_count`)
- `d` = `decay_rate` from the memory's frontmatter
- Simplified approximation when full access history isn't available:

```
activation(m) ≈ ln(access_count × recency^(-d))
```

Where `recency` = hours since `last_accessed`.

**Practical ranges:**
| Activation | Interpretation |
|---|---|
| > 2.0 | Very active — accessed frequently and recently |
| 1.0–2.0 | Active — regular use |
| 0.0–1.0 | Moderate — occasional relevance |
| -1.0–0.0 | Fading — hasn't been accessed recently |
| < -1.0 | Candidate for archival |

**Soft bound (squash).** Raw base-level activation is passed through a smooth, monotonic
squash before use: `activation' = M · tanh(activation / M)` where `M = max_activation`
(default 2.0; 0 disables). Without it, an access count inflated by the historical retrieval
feedback loop (observed 2026-06-18: counts up to ~168,000 → `ln` ≈ 12) dominates the additive
score regardless of relevance. The squash asymptotes toward `M` without the flattening/ties
of a hard cap, keeping activation in the band where `β · relevance` can compete while
preserving relative order.

**Citation-gated reinforcement.** Access events are tagged by source. With
`citation_gated_activation: true` (default), system *injection* (the session-start and
user-prompt-submit hooks surfacing a memory) does **not** advance the count/recency that feed
base-level activation — only genuine use (the assistant actually citing a loaded memory,
harvested by `citation_harvest`) and explicit CLI retrieval do. This breaks the feedback loop
where injected memories re-pinned their own recency every turn — the mechanism that inflated
counts in the first place.

### Component 2: Relevance

Relevance measures semantic similarity between the memory and the current context. In the absence of embedding infrastructure, this is estimated heuristically:

**Tag match:** Each shared tag between the memory and the inferred context adds +0.2 (max 1.0)

**Link proximity:** If the memory links to another memory that's already been retrieved, add +0.3 (spreading activation analog)

**Type match:** If the conversation is asking for guidance → feedback memories get +0.2; if asking about the user → user memories get +0.2; etc.

When embedding infrastructure is available, replace with cosine similarity between memory embedding and query embedding.

### Component 3: Confidence

Weights the relevance term (not the whole score): the relevance contribution is `β · relevance · confidence`. A memory with confidence 0.3 contributes only 30% of its potential *relevance* term — it's uncertain and shouldn't be pulled to the top on a topical match alone — while its base-level activation is unaffected.

### Component 4: Surprise Bonus

```
surprise_bonus(m) = surprise_at_encoding × 0.5
```

Memories that were surprising when encoded get a persistent retrieval advantage. This models the human tendency to remember unexpected events more readily than expected ones (the Von Restorff / isolation effect).

---

## Retrieval Threshold τ

```
τ = 0.3  (default)
```

Memories scoring below τ are not loaded into context. They remain in `Memory/` but are invisible to the active conversation.

During consolidation, memories whose *maximum possible score* (assuming perfect relevance) falls below τ are candidates for archival.

### Threshold Tuning

| τ Value | Effect |
|---|---|
| 0.1 | Permissive — more memories loaded, higher context cost |
| 0.3 | Balanced (default) |
| 0.5 | Selective — only high-activation, high-confidence memories |
| 0.7 | Aggressive — minimal context usage, risk of missing relevant memories |

The user can adjust τ in [[Config]].

---

## Access Update

When a memory is retrieved and used in conversation:

```yaml
last_accessed: <now>
access_count: <previous + 1>
```

This reinforces the memory's activation for future retrievals — the spaced repetition effect.

---

## Worked Example

Memory: "User prefers Go for offensive tooling"
```yaml
created: 2026-03-15
last_accessed: 2026-04-02   # 96 hours ago
access_count: 7
decay_rate: 0.3
confidence: 0.95
surprise_at_encoding: 0.2
tags: [go, tooling, preference]
```

Query context tags: [go, development, tooling]

```
activation  = ln(7 × 96^(-0.3)) = ln(1.73) = 0.549
              squashed: 2 · tanh(0.549 / 2) = 0.536
relevance   = tag_match(2 shared tags × 0.2) = 0.4
confidence  = 0.95
surprise    = 0.2 × 0.5 = 0.1

score = 0.536 + 8 · 0.4 · 0.95 + 0.1 = 3.68

3.68 > τ(1.0) → RETRIEVED
```

If the same memory hadn't been accessed in 30 days (720 hours):
```
activation  = ln(7 × 720^(-0.3)) = -0.087 → squashed -0.087 → floored to 0.4 (user floor)
score       = 0.4 + 8 · 0.4 · 0.95 + 0.1 = 3.54

3.54 > τ(1.0) → STILL RETRIEVED — under the additive form a topically relevant memory
surfaces even when dormant, because the relevance term carries it. (Under the old
multiplicative form this scored 0.067 and was dropped — relevance could not rescue a low
activation.)
```

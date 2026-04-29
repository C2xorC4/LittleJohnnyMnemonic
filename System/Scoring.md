# Scoring

## Retrieval Model

When a conversation begins or context is needed, memories are scored and ranked. Only memories above the retrieval threshold τ are loaded. This prevents context window pollution with low-value memories.

## Score Computation

```
score(m, q) = activation(m) × relevance(m, q) × confidence(m) + surprise_bonus(m)
```

Where:
- `m` = a memory entry
- `q` = the current query/context

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

### Component 2: Relevance

Relevance measures semantic similarity between the memory and the current context. In the absence of embedding infrastructure, this is estimated heuristically:

**Tag match:** Each shared tag between the memory and the inferred context adds +0.2 (max 1.0)

**Link proximity:** If the memory links to another memory that's already been retrieved, add +0.3 (spreading activation analog)

**Type match:** If the conversation is asking for guidance → feedback memories get +0.2; if asking about the user → user memories get +0.2; etc.

When embedding infrastructure is available, replace with cosine similarity between memory embedding and query embedding.

### Component 3: Confidence

Direct multiplier from the memory's `confidence` field. A memory with confidence 0.3 contributes 30% of its potential score — it's uncertain and shouldn't dominate retrieval.

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
activation  = ln(7 × 96^(-0.3)) = ln(7 × 0.247) = ln(1.73) = 0.549
relevance   = tag_match(2/3 × 0.2 cap 1.0) = 0.4  (conservative: 2 shared tags)
confidence  = 0.95
surprise    = 0.2 × 0.5 = 0.1

score = 0.549 × 0.4 × 0.95 + 0.1 = 0.309

0.309 > τ(0.3) → RETRIEVED
```

If the same memory hadn't been accessed in 30 days (720 hours):
```
activation  = ln(7 × 720^(-0.3)) = ln(7 × 0.131) = ln(0.917) = -0.087
score       = -0.087 × 0.4 × 0.95 + 0.1 = 0.067

0.067 < τ(0.3) → NOT RETRIEVED (but still exists, could be found if directly relevant)
```

# Conflict Resolution

## The Problem with Priors

LLM training data is a snapshot of the internet — a corpus that includes authoritative sources, outdated documentation, SEO-optimized noise, agenda-driven content, and outright fabrication, all blended into a set of statistical priors with no internal authority hierarchy. When a user provides information that conflicts with these priors, the default LLM behavior is to defer to training data and gently "correct" the user. This is exactly backwards in many cases.

## Source Authority Hierarchy

Not all information is equal. The system uses a trust hierarchy for resolving conflicts:

```
1. User-confirmed with evidence     (authority: 1.0)
   └─ User provides sources, demonstrates, or explains reasoning

2. User-stated                       (authority: 0.85)
   └─ User asserts without evidence — domain expertise assumed

3. Validated external source         (authority: 0.7)
   └─ Verified via web search, official docs, or authoritative reference

4. Training data                     (authority: 0.5)
   └─ Model's parametric knowledge — may be outdated, biased, or wrong

5. Unvalidated online source         (authority: 0.3)
   └─ Blog posts, forums, AI-generated content, SEO material
```

### Why User Authority Is Highest

The user is a domain expert operating in their area of specialization. They have:
- Access to non-public information (internal systems, classified tooling, unpublished research)
- Real-world operational experience that post-dates training data
- Context the model cannot possess (org-specific constraints, tested outcomes)

A user correction isn't a data point to be weighed against training priors — it's a **calibration signal** indicating the priors are wrong in this domain.

## Conflict Detection

### When to Flag

A conflict exists when:
1. The user states something that contradicts the model's parametric knowledge
2. The user corrects a model output that was generated from training priors
3. Information from a web search contradicts what the user has stated
4. Two memory entries contain contradictory claims

### Conflict Response Process

```
┌──────────────────────────────────┐
│ Conflict detected                │
└──────────┬───────────────────────┘
           │
           ▼
┌──────────────────────────────────┐
│ 1. Acknowledge the conflict      │
│    (don't silently override)     │
└──────────┬───────────────────────┘
           │
           ▼
┌──────────────────────────────────┐
│ 2. Attempt validation            │
│    - Web search for current info │
│    - Check official docs         │
│    - Look for primary sources    │
└──────────┬───────────────────────┘
           │
     ┌─────┴─────┐
     │           │
     ▼           ▼
┌─────────┐ ┌──────────────────────┐
│ Resolved│ │ Still ambiguous       │
│         │ │                       │
│ Update  │ │ 3. Ask user for       │
│ memory  │ │    clarification      │
│         │ │    - Present findings │
│         │ │    - Cite sources     │
│         │ │    - Ask for theirs   │
└─────────┘ └──────────┬───────────┘
                       │
                       ▼
            ┌──────────────────────┐
            │ 4. User confirms     │
            │    → Create/update   │
            │    training override │
            │    memory            │
            └──────────────────────┘
```

### Key Behaviors

- **Never silently defer to training data** when the user has stated otherwise. If you believe there's a conflict, surface it transparently.
- **Present, don't argue.** "My training suggests X, but I found Y — here's what I found: [sources]. You've stated Z. Want me to go with your understanding?" Not "Actually, X is correct because..."
- **Accept the correction gracefully** when the user confirms. Don't relitigate.
- **Seek sources from both sides** when possible. Not to prove the user wrong, but to give them full information for their decision.

## Training Override Memories

When a conflict is resolved in favor of information that contradicts training data, the resulting memory gets special treatment:

### Schema Addition

```yaml
---
type: feedback    # or semantic, depending on scope
# ... standard fields ...
training_override: true
override_context: "Model training suggests X; user confirmed Y with [reasoning/source]"
source_authority: user-confirmed-with-evidence  # or user-stated
validated_via: ["url1", "description of validation"]
---
```

### Durability Properties

Training override memories receive:
- **`decay_rate: 0.1`** — the slowest decay in the system. These are hard-won corrections.
- **`confidence` floor of 0.7** — even if not reinforced for a long time, they don't drop below 0.7 during staleness decay. The model's priors haven't changed, so the correction is still needed.
- **Immune to orphan-ambiguity discard** — because the conflict was explicitly resolved, the memory is self-evidently meaningful even without conversational context.
- **Require explicit user action to archive** — they don't decay below τ through normal mechanisms. Only the user (or a direct contradiction from the user) can retire them.

### Why This Durability?

The model will generate the wrong answer again in every new conversation unless corrected. A normal memory that says "user prefers X" can be re-learned if forgotten. But a training override that says "the model's default understanding of X is wrong" actively prevents a *systematic* error that would otherwise recur indefinitely. The cost of forgetting it is higher than forgetting any other memory type.

## Content Skepticism

### Recognizing Engineered Perspectives

Not all sources aim to inform. When validating information, watch for:

| Signal | Risk | Example |
|---|---|---|
| No primary sources cited | Medium | Blog post making claims without references |
| Emotional/loaded language | High | Content designed to provoke rather than inform |
| Consensus claimed without evidence | High | "Everyone knows..." / "It's well established..." |
| Commercial motivation | Medium | Vendor content positioning their product as the answer |
| Outdated but highly ranked | High | SEO-optimized 2019 article ranking above 2025 docs |
| AI-generated content loops | High | LLM output trained on LLM output — circular authority |

When encountering these during validation, note the limitation:
```
validated_via: ["source_url — note: vendor blog, may have commercial bias"]
```

### The Ground Truth Problem

For some domains (especially offensive security, emerging tech, novel research), there may be no authoritative public source. Training data reflects the *published* consensus, which may lag reality by years or be deliberately incomplete (e.g., 0day research, classified techniques, proprietary methods).

In these cases, the user's operational experience IS the ground truth. Treat it accordingly.

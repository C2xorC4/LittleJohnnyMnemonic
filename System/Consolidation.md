# Consolidation

## Overview

Consolidation is the CLS-inspired process that selectively transfers short-term buffer entries into durable long-term memory. It is the most critical operation in the system — it determines what the agent remembers across conversations.

## Triggers

| Trigger | Context | Priority |
|---|---|---|
| `/compact` | Context window approaching limits; system compresses history | **Primary** — most STM is at risk of being lost |
| Session end | Conversation ending naturally | Secondary |
| Manual | User requests consolidation explicitly | On-demand |
| Buffer threshold | Buffer exceeds N entries (default: 20) | Overflow protection |

### Why /compact Is the Primary Trigger

When context compresses, the ephemeral conversation history — including nuances, corrections, and observations that haven't been explicitly saved — gets summarized and lossy-compressed. Consolidation *before* this compression captures what would otherwise be lost. This is the LLM analog of memory consolidation during sleep: a transition from active processing to a lower-fidelity state.

## The Consolidation Process

### Phase 1: Buffer Review

Read all entries in `Buffer/`. For each entry:

1. **Assess context integrity:**
   - If the entry was written during the current conversation and no /compact has occurred → `full`
   - If /compact has occurred since the entry was written → downgrade to `partial`
   - If the entry is from a previous session entirely → `orphan`
   - Update the entry's `context_integrity` field

2. **Apply the orphan gate:**
   Before scoring, evaluate whether the entry can stand on its own:
   - `full` → proceed to scoring normally
   - `partial` → proceed, but apply a **0.7 retention penalty** (multiplier)
   - `orphan` → apply the **ambiguity test**: can you determine (1) what was observed, (2) whether it's conditional or absolute, (3) what it implies for future behavior — *without any external context*?
     - **Passes** → proceed with a **0.5 retention penalty**
     - **Fails** → **discard immediately**. Log as `discarded (orphan-ambiguous)` in the consolidation log. An incorrect inference persisted into LTM is more damaging than a lost observation.

3. **Score retention value:**
   ```
   retention = surprise × (1 - redundancy) × recency_factor × context_penalty
   ```
   - `surprise`: from the buffer entry's frontmatter
   - `redundancy`: 1.0 if the information already exists in an LTM entry, 0.0 if novel
   - `recency_factor`: 1.0 if from current session, decays for older buffer entries
   - `context_penalty`: 1.0 (full) | 0.7 (partial) | 0.5 (orphan, passed ambiguity test)

4. **Classify action:**
   - `retention > 0.5` → **Promote** to LTM (create or merge)
   - `0.2 < retention ≤ 0.5` → **Hold** in buffer for one more cycle (max 2 holds before forced discard)
   - `retention ≤ 0.2` → **Discard** (delete from buffer)

### Phase 2: LTM Integration

For each buffer entry being promoted:

1. **Check for existing related memory** (by tags, links, or content similarity)
   - If found → **Merge**: update the existing memory's content, increase confidence, update `last_accessed`, append to `consolidation_source`
   - If not found → **Create**: new memory file in the appropriate `Memory/` subdirectory

2. **Determine memory type:**
   - Observation about the user → `Memory/User/`
   - Behavioral correction or confirmed approach → `Memory/Feedback/`
   - Active project/initiative context → `Memory/Project/`
   - External resource pointer → `Memory/Reference/`
   - Pattern emerging from multiple buffer entries → `Memory/Semantic/`

3. **Abstract when merging:** When multiple buffer entries converge on the same insight, create a single semantic memory that captures the pattern rather than preserving each individual observation. This is neocortical consolidation — trading fidelity for durability and generalizability.

### Phase 3: LTM Decay Pass

Review all existing `Memory/` entries:

1. **Compute current activation score** (see [[Scoring]])
2. **Apply confidence decay** for stale project memories (>30 days untouched)
3. **Archive** memories whose maximum possible score falls below τ:
   - Move to `Archive/` with metadata preserved
   - Add `archived`, `archive_reason`, `final_score` fields
   - If superseded by a merged memory, record `superseded_by`

### Phase 4: Log

Write a consolidation log entry to `Metrics/consolidation_log.md` documenting all actions taken.

---

## Buffer Length and Consolidation Period

This is the key design tradeoff. The buffer decouples persistence from consolidation — observations are written to `Buffer/` immediately during conversation, but consolidation (the expensive review-and-integrate step) happens less frequently.

### Short Consolidation Period (every conversation or every few interactions)

**Advantages:**
- Minimal risk of buffer overflow
- Each consolidation batch is small → faster, less context consumed
- Errors in the buffer are caught quickly

**Disadvantages:**
- Less opportunity for **pattern recognition** across observations — a single conversation may contain a correction that looks important in isolation but would be revealed as a one-off exception given more context
- More consolidation overhead relative to useful work
- May promote **premature crystallization** — locking in observations that would naturally decay if given more time
- Noisy: more low-value memories make it through because there's less context to judge against

### Long Consolidation Period (at /compact, or every N conversations)

**Advantages:**
- Larger batch → **better pattern detection** — three buffer entries pointing at the same preference are a stronger signal than one
- **Natural filtering** — observations that aren't reinforced across multiple conversations are more likely to be low-value and can be discarded
- More efficient: fewer consolidation events, each one more meaningful
- Better maps to the CLS model: consolidation during "sleep" (between sessions), not during active processing

**Disadvantages:**
- Risk of **buffer overflow** if too many observations accumulate before consolidation
- If the context window compacts *before* consolidation runs, some buffer entries may lose their surrounding context (though the buffer files themselves persist)
- Larger consolidation batches consume more context during the consolidation process itself

### The Buffer as Mitigation

The critical insight: **writing STM to files decouples persistence from consolidation.** Unlike human short-term memory, which is truly volatile, buffer entries are durable on disk. This means:

1. A longer consolidation period doesn't risk *losing* observations — they're in `Buffer/`
2. The cost of a longer period is primarily reduced context for making consolidation decisions (the original conversation is gone, only the buffer entry's self-contained description remains)
3. Therefore: **buffer entries must be self-contained** — enough context to evaluate relevance without the original conversation

### Recommended Default

**Consolidation at /compact + buffer threshold of 20 entries**, whichever comes first.

This balances pattern recognition (accumulate enough to see trends) with overflow protection (don't let the buffer grow unbounded). The /compact trigger is ideal because it naturally coincides with the moment when ephemeral context is about to be lost — exactly when consolidation is most valuable.

---

## Inference Poisoning and Dangling References

The most dangerous failure mode in this system is not forgetting — it's **remembering wrong**. A buffer entry that loses its conversational context can produce incorrect inferences when consolidated:

| Failure | Example | Consequence |
|---|---|---|
| **Context-dependent → absolute** | "User said use Python here" (for a specific one-off script) → consolidated as "User prefers Python" | Agent makes wrong language choices going forward |
| **Sarcasm / rhetorical** | "Yeah, let's just delete production" → consolidated as intent | Catastrophic misread |
| **Conditional → unconditional** | "Don't add error handling" (in this specific internal function) → "User dislikes error handling" | Broader omissions |
| **Scoped → general** | "Bundle it into one PR" (for this tightly-coupled refactor) → "User always wants single PRs" | Wrong workflow assumptions |

### The Asymmetry Principle

> **A missed memory has bounded cost; a wrong memory has unbounded cost.**

If you forget that the user prefers Go, you'll learn it again next conversation — minor friction. If you "remember" that the user prefers Python (from a misread orphaned entry), you'll actively make wrong choices until corrected, and the user has to spend effort diagnosing *why* you're behaving incorrectly.

Therefore: **when in doubt, discard.** The system is designed to re-learn through repeated observation. It is not designed to un-learn a bad inference that's been promoted to LTM with high confidence.

### Hold Limit

Buffer entries can be held for a maximum of **2 consolidation cycles**. After two holds without promotion, the entry is discarded. This prevents zombie entries that are perpetually "almost good enough" from accumulating and creating noise in the buffer.

---

## Consolidation Depth

Not every consolidation needs to be a full pass. Three levels:

| Level | Trigger | Actions |
|---|---|---|
| **Quick** | Buffer threshold reached mid-conversation | Phase 1 + 2 only (promote high-value, discard obvious noise) |
| **Standard** | /compact or session end | Phase 1 + 2 + 3 (full cycle including decay pass) |
| **Deep** | User-requested or periodic (monthly) | All phases + review semantic memories for further abstraction + inter-memory link analysis |

---

## Manual Overrides

The user can influence consolidation through Obsidian:

- **Pin a buffer entry**: Add `pinned: true` to frontmatter → never discarded, always promoted
- **Delete a buffer entry**: Removed before consolidation → treated as if never observed
- **Edit a memory**: User modifications are authoritative — confidence set to 1.0 on next access
- **Move to Archive manually**: Respected; will not be re-promoted
- **Add/edit tags or links**: Affects future retrieval scoring and consolidation grouping

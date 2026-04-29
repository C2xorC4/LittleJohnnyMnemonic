# Architecture

## Cognitive Model

LittleJohnnyMnemonic models LLM memory on three well-studied frameworks from cognitive science, adapted for the constraints and affordances of a stateless language model operating across discrete conversation sessions.

### Theoretical Foundations

**ACT-R (Adaptive Control of Thought — Rational)**
John Anderson's architecture provides the activation and retrieval mechanics. Each memory chunk has a base-level activation determined by recency and frequency of access:

```
B_i = ln(Σ t_j^(-d))
```

- `t_j` = time elapsed since the jth retrieval (in hours)
- `d` = decay rate (default 0.5; tunable per memory type)
- Memories below a retrieval threshold τ are not surfaced

Spreading activation adds contextual relevance: memories semantically connected to the current query receive an associative boost.

**Complementary Learning Systems (CLS)**
McClelland & O'Reilly's two-system model maps directly to the STM/LTM split:

| Property | Hippocampal (STM) | Neocortical (LTM) |
|---|---|---|
| Encoding speed | Fast (single exposure) | Slow (consolidation required) |
| Fidelity | High (verbatim) | Abstracted (gist) |
| Decay | Rapid without reinforcement | Durable |
| Interference | Prone | Resistant |
| Analog | `Buffer/` directory | `Memory/` directory |

The critical process is **consolidation** — periodic review that selectively transfers, merges, or discards STM entries into LTM.

**Information-Theoretic Weighting**
Not all observations carry equal information. Surprise (self-information) at encoding determines initial weight:

```
I(x) = -log₂ P(x|context)
```

- A user correction ("no, don't do X") has high surprise — it contradicts the model's prior
- A routine confirmation ("yes") has low surprise — expected outcome
- A novel fact about the user's workflow has moderate surprise — new information, no contradiction

This creates natural prioritization: corrections > novel facts > confirmations > derivable information.

---

## System Architecture

```
┌─────────────────────────────────────────────┐
│              Active Conversation            │
│         (ephemeral context window)          │
└──────────┬───────────────┬──────────────────┘
           │ write         │ read (scored)
           ▼               │
┌──────────────────┐       │
│   Buffer/ (STM)  │       │
│  timestamped     │       │
│  high-fidelity   │       │
│  rapid decay     │       │
└────────┬─────────┘       │
         │ consolidation   │
         ▼                 │
┌──────────────────┐       │
│  Memory/ (LTM)   │◄──────┘
│  scored          │
│  categorized     │
│  durable         │
└────────┬─────────┘
         │ decay below τ
         ▼
┌──────────────────┐
│  Archive/        │
│  tombstoned      │
│  recoverable     │
└──────────────────┘
```

### Directory Roles

| Directory | Purpose | Retention |
|---|---|---|
| `Buffer/` | Raw STM entries from active conversations | Until next consolidation |
| `Memory/User/` | Identity, role, preferences, knowledge profile | High durability |
| `Memory/Feedback/` | Behavioral corrections and confirmed approaches | High durability |
| `Memory/Project/` | Active work context, goals, deadlines | Medium durability (decays with project lifecycle) |
| `Memory/Reference/` | Pointers to external resources | Medium durability |
| `Memory/Semantic/` | Consolidated abstractions (merged from multiple observations) | Highest durability |
| `Archive/` | Decayed memories below retrieval threshold | Indefinite (human-recoverable) |
| `Metrics/` | Consolidation logs, system health | Append-only |

### File Format

All memory files use Obsidian-compatible YAML frontmatter. See [[Schema]] for the complete specification.

### Consolidation

See [[Consolidation]] for the process, triggers, and buffer-length tradeoffs.

### Scoring

See [[Scoring]] for the retrieval algorithm and parameter tuning.

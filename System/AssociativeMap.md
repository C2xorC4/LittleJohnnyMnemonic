# Associative Map

## Purpose

Human memory doesn't retrieve in isolation — recalling one concept primes related concepts. Remembering "process injection" naturally activates "thread hijacking," "shellcode," "EDR evasion," and the specific tools and techniques connected to them. This is **spreading activation** in ACT-R terms, and it's what makes human recall contextually rich rather than keyword-flat.

The associative map provides this for LLM memory: a graph of conceptual relationships between memories that guides retrieval toward relevant neighbors without pulling in the entire memory store.

## Relationship Types

Memories can be connected through typed edges. Each type carries a different retrieval implication:

| Relationship | Notation | Retrieval Effect | Example |
|---|---|---|---|
| `related-to` | `--` | Moderate boost to neighbor activation | "Go tooling" ↔ "cross-compilation" |
| `refines` | `->` | Child inherits parent's context | "process injection" → "thread hijacking" |
| `contradicts` | `><` | Both surfaced when either is relevant (for comparison) | "training says X" >< "user confirmed Y" |
| `depends-on` | `=>` | Dependency loaded first | "EOD toolkit" => "Go development preferences" |
| `supersedes` | `>>` | New version preferred; old archived | "Q2 ops plan" >> "Q1 ops plan" |
| `instance-of` | `~>` | Specific example of a general pattern | "blsim project" ~> "ransomware simulation techniques" |
| `learned` | `~~` | Auto-discovered from co-activation patterns | "Phrea mentorship" ~~ "build-and-lose pattern" |

## Implementation in Obsidian

Relationships are encoded in two ways, both Obsidian-native:

### 1. Frontmatter Links (machine-readable)

```yaml
links:
  - target: "[[Memory/User/go_expertise]]"
    relationship: depends-on
  - target: "[[Memory/Feedback/table_driven_tests]]"
    relationship: related-to
  - target: "[[Memory/Semantic/process_injection_patterns]]"
    relationship: refines
```

### 2. Inline Wiki-Links (human-readable, graph-view compatible)

Within the memory body, reference related memories naturally:

```markdown
User's offensive tooling work (see [[go_expertise]]) relies heavily on
process injection techniques documented in [[process_injection_patterns]].
```

Both forms contribute to the Obsidian graph view, giving the user visual topology of the memory space.

## Concept Clusters

Related memories naturally form clusters — groups of tightly connected nodes that represent a domain of knowledge. During retrieval, activating any node in a cluster provides a small boost to all other nodes in the same cluster.

### Cluster Detection

During **deep consolidation**, review the link graph and identify clusters:
- Nodes with 3+ connections to each other form a natural cluster
- Name the cluster and note it in a `Semantic/` memory if one doesn't already exist
- The semantic memory becomes the cluster's "index node" — retrieving it provides an overview without loading every individual memory

### Example Cluster

```
┌─────────────────────────────────────────────┐
│  Cluster: Offensive Go Development          │
│                                             │
│  [[go_expertise]]                           │
│       ├── related-to ── [[cross_compile]]   │
│       ├── depends-on ◄── [[eod_toolkit]]    │
│       │                    ├── refines ──    │
│       │                    │  [[proc_inj]]   │
│       │                    └── refines ──    │
│       │                       [[hellshell]]  │
│       └── related-to ── [[windows_apis]]    │
│                                             │
│  Index: [[Semantic/offensive_go_dev]]       │
└─────────────────────────────────────────────┘
```

## Retrieval with Spreading Activation

When a memory is retrieved (scores above τ), its neighbors receive an activation boost:

```
neighbor_boost(n, m) = activation(m) × edge_weight(relationship) × 0.3
```

### Edge Weights by Relationship Type

| Relationship | Edge Weight | Rationale |
|---|---|---|
| `related-to` | 0.5 | General association — moderate boost |
| `refines` | 0.7 | Specificity is usually relevant when the parent is |
| `contradicts` | 0.8 | High — if one side of a conflict is relevant, the other must be too |
| `depends-on` | 0.6 | Dependencies provide context |
| `supersedes` | 0.2 | Low — the old version is rarely needed, but keep it findable |
| `instance-of` | 0.4 | Examples help but aren't always needed |
| `learned` | 0.4 | Emergent — discovered through repeated co-activation, not explicit curation |

### Activation Cap

Spreading activation propagates **one hop only** — no transitive spreading. This prevents the entire memory graph from lighting up when any node is accessed. The retrieval algorithm:

1. Score all memories against the current context (standard scoring)
2. Select memories above τ
3. For each selected memory, boost its direct neighbors by `neighbor_boost`
4. Re-rank with boosted scores
5. Select final set (capped at `max_memories_loaded`)

## Negative Associations

Some memories should *suppress* unrelated retrieval rather than boost it. If a conversation is clearly about "Python scripting for quick prototyping," the strong "Go preference" memory shouldn't dominate just because it has high activation.

This is handled through the **relevance** component of scoring — tag mismatch and type mismatch naturally reduce the score. But if a memory has a `contradicts` edge to a highly relevant memory, it gets boosted (because the contradiction itself is relevant context).

## Building the Map Over Time

The associative map isn't built all at once — it grows organically through three mechanisms:

### Explicit Links (curated)
1. **At buffer write time:** Note `related` entries in frontmatter if obvious connections exist
2. **At consolidation (standard):** When creating/merging LTM entries, add links to existing related memories
3. **At consolidation (deep):** Review the full graph, identify clusters, create semantic index nodes, prune dead links
4. **User edits:** The user can add/remove/retype links in Obsidian at any time — these are authoritative

### Learned Links (emergent)
Memories that repeatedly appear together in association results develop `learned` edges — Hebbian in principle ("fire together, wire together"):

1. **Recording:** Every `jm associate` run logs which memories co-activated in `Metrics/coactivation.json`
2. **Detection:** `jm learn-edges` identifies pairs exceeding the co-activation threshold (default 3) that lack explicit edges
3. **Application:** Learned edges are `related-to` in character but tagged as `learned` to distinguish them from curated links. They carry a 0.4 edge weight.
4. **Promotion:** During deep consolidation, high-count learned edges can be reviewed and optionally retyped to a more specific relationship.

### Ambient Association (contextual, no edge)
Not all associations need a stored edge. Keyword and body-text overlap during `jm associate` can surface memories that share concepts without any graph connection. These are the "this reminds me of..." associations — the LLM synthesizes the connection in context rather than the graph encoding it.

The three tiers work together: ambient catches broad thematic resonance, learned edges crystallize repeated patterns, and explicit links encode known structural relationships.

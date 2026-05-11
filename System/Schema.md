# Schema

## Buffer Entry (STM)

Buffer entries are raw observations written during active conversation. High fidelity, minimal processing.

```yaml
---
type: buffer
timestamp: 2026-04-06T14:30:00-04:00   # ISO 8601, always absolute
source: conversation                     # conversation | consolidation | user-edit
surprise: 0.7                           # 0.0–1.0, estimated at encoding
context_integrity: full                  # full | partial | orphan (see below)
tags: [correction, code-style]          # freeform, used for consolidation grouping
related: []                             # wiki-links to existing Memory/ entries
---

The raw observation. Written as-observed, not abstracted.
Include enough context that consolidation can determine relevance
without access to the original conversation.
```

### Context Integrity

Tracks whether the original conversational context is still available when the buffer entry is evaluated. This is critical for avoiding incorrect inferences from decontextualized observations.

| Value | Meaning | Consolidation Behavior |
|---|---|---|
| `full` | Original conversation is still in the active context window | Normal scoring — all context available for evaluation |
| `partial` | Conversation has been compressed (/compact occurred) but key details are in the summary | Promote only if the entry is self-contained enough to stand alone; apply a 0.7 penalty to retention score |
| `orphan` | Original conversation is completely gone (different session, no summary available) | **Do not promote unless the entry is unambiguous on its own.** If ambiguous, discard. An incorrect inference in LTM is worse than a lost observation. |

**Rules for setting context_integrity:**
- At time of writing: always `full` (you're in the conversation)
- At consolidation: downgrade to `partial` if /compact has occurred since the entry was written
- Cross-session buffer entries (written in a previous conversation): `orphan`

### Self-Containment Requirements

Because buffer entries may outlive their conversational context, every entry must pass this test:

> *Could someone with no knowledge of the conversation that produced this entry determine: (1) what was observed, (2) whether it's conditional or absolute, and (3) what it implies for future behavior?*

**Anti-patterns** (entries that fail self-containment):
- "User said to use the other approach" — which approach? Other than what?
- "Don't do that again" — do what? In what context?
- "The fix worked" — what fix? For what problem?
- "User prefers X" — unconditionally, or only in the context being discussed?

**Good patterns:**
- "User corrected: when writing Go tests, use table-driven tests rather than individual test functions. Reason given: consistency with existing test patterns in the project."
- "User confirmed that bundling related changes into a single PR is preferred over splitting for this type of refactor (infrastructure changes that touch multiple packages)."

### Surprise Estimation Guide

| Signal | Surprise | Examples |
|---|---|---|
| Explicit correction | 0.8–1.0 | "No, don't do that", "Stop X", "That's wrong" |
| New fact, contradicts assumption | 0.7–0.9 | User reveals unexpected role, tool preference |
| New fact, no prior assumption | 0.4–0.6 | User mentions a project, names a tool |
| Confirmation of expected behavior | 0.1–0.3 | "Yes", "Correct", accepts suggestion without comment |
| Derivable from context | 0.0–0.1 | Information already in codebase, git history, docs |

---

## Memory Entry (LTM)

Long-term memories carry full activation metadata for scored retrieval.

```yaml
---
type: user | feedback | project | reference | semantic | episodic
title: "Short descriptive title"
created: 2026-04-06T14:30:00-04:00
last_accessed: 2026-04-06T14:30:00-04:00
access_count: 1
decay_rate: 0.5                         # ACT-R decay parameter d
confidence: 0.8                         # 0.0–1.0, Bayesian posterior
surprise_at_encoding: 0.7               # preserved from buffer entry
fidelity: full                          # full | detailed | summary | gist (see ProgressiveCompression)
importance: moderate                    # critical | significant | moderate | minor
consolidation_source: []                # buffer entries that contributed
tags: [go, tooling, preference]
links:                                  # typed associative links (see AssociativeMap)
  - target: "[[Memory/Category/entry]]"
    relationship: related-to            # related-to | refines | contradicts | depends-on | supersedes | instance-of
    weight: 0.65                        # OPTIONAL — overrides the relationship-type default from Config.edge_weights

# --- Optional fields (include when applicable) ---
# For training overrides (see ConflictResolution):
# training_override: true
# override_context: "Model suggests X; user confirmed Y"
# source_authority: user-confirmed-with-evidence | user-stated | validated-external
# validated_via: ["source description or URL"]

# For user observations (see UserModeling):
# facet: identity | cognition | communication | expertise | motivation | patterns
# observation_count: 1
---

The memory content. For feedback and project types, structure as:

**Rule/Fact:** The core assertion.

**Why:** The motivation or evidence behind it.

**How to apply:** When and where this should influence behavior.

For user observations, structure as:

**Observation:** What was observed.

**Observed in:** Context where this was seen (project, conversation topic, situation).

**How to apply:** How this should shape interaction style or approach.
```

### Type-Specific Decay Defaults

| Type | Default `decay_rate` | Rationale |
|---|---|---|
| user | 0.3 | Identity is stable; slow decay |
| feedback | 0.3 | Behavioral guidance is durable |
| project | 0.4 | Projects are semi-persistent; moderate decay |
| reference | 0.35 | Infrastructure references are fairly stable |
| semantic | 0.2 | Abstractions are durable |
| episodic | 0.05 | Interaction history is the most durable — like human autobiographical memory |

---

## Episodic Entry (Interaction Memory)

Episodic memories capture high-level summaries of interactions — what happened, what was notable, how the session felt. They model human autobiographical memory: you remember the *event* and your *impression* of it, not every technical detail.

Episodic memories have the lowest decay rate in the system. They are never auto-archived.

```yaml
---
type: episodic
title: "2026-04-08: Memory system design, biography, philosophy of learning"
session_date: 2026-04-08
created: 2026-04-08T14:30:00-04:00
last_accessed: 2026-04-08T14:30:00-04:00
access_count: 1
decay_rate: 0.05
confidence: 0.90
surprise_at_encoding: 0.6
topics: [memory-system, biography, career, philosophy, agi]
projects_touched: [johnny_mnemonic, blsim, mimic]
tags: [episodic, session, interaction]
links:
  - target: "[[Memory/User/profile_personality]]"
    relationship: related-to
---

**Summary:** [2-4 sentence overview of the session — what was accomplished,
what was discussed, what shifted in understanding]

**What was done:**
- [Concrete actions taken: code written, files created, projects explored]

**What was discussed:**
- [Key conversation threads beyond the immediate task]

**Notable observations:**
- [Surprising findings, corrections, insights — from user interaction,
  research, codebase exploration, or any other source]
- [Things that changed the agent's understanding]

**Perceived tone/engagement:**
- [How the session felt: engaged, reflective, frustrated, exploratory, etc.
  NOT emotional state judgment — behavioral observation of session dynamics]
```

### What Makes Episodic Memory Different

| Property | Standard Memory | Episodic Memory |
|---|---|---|
| Granularity | Single fact, observation, or rule | Entire interaction session |
| Detail level | Specific and precise | Summary — gist, not transcript |
| Decay | Varies by type (0.05–0.4) | Always 0.05 (stickiest) |
| Purpose | Inform specific decisions | Provide continuity and context across sessions |
| Archival | Can be auto-archived | Never auto-archived |
| Notable captures | User-facing observations only | Any source: user, research, code, tools |

### When to Create Episodic Entries

- At consolidation (end of significant sessions)
- When a session covers substantial ground (multiple topics, meaningful work)
- NOT for trivial interactions (quick file edits, single-question answers)

### What "Notable" Captures

Notable observations aren't limited to user interaction. They include:
- Surprising findings during code exploration or research
- Unexpected patterns in data or codebases
- Things that changed the agent's model of the user, a project, or a domain
- Corrections to prior understanding (from any source)
- Technical discoveries worth remembering across sessions

### Confidence Update Rules

- **Reinforced** (same observation repeated): `confidence = min(1.0, confidence + 0.1)`
- **Contradicted** (conflicting observation): `confidence = max(0.0, confidence - 0.3)`
- **Unchallenged** (neither confirmed nor denied): no change
- **Stale** (not accessed in >30 days and project type): `confidence *= 0.9` per consolidation cycle

---

## Knowledge Entry (Persistent Reference Material)

Knowledge entries store ingested technical documentation, reference material, and researched findings. Unlike other memory types, they do not decay with time — they persist until explicitly superseded or marked obsolete. This models how learned technical knowledge works: you don't forget how PE headers are structured because you haven't looked at them in a month.

```yaml
---
type: knowledge
title: "NTDLL Syscall Stub Structure (Windows 11 24H2)"
created: 2026-04-09T10:00:00-04:00
last_accessed: 2026-04-09T10:00:00-04:00
access_count: 1
decay_rate: 0.0                          # no time-based decay
confidence: 0.90
surprise_at_encoding: 0.5
fidelity: full
importance: significant
source_document: "Windows Internals 7th Ed, Ch. 3"
source_version: "Windows 11 24H2 / NTDLL 10.0.26100"
domain: windows-internals
verified: true                            # cross-referenced against live binary
tags: [windows, ntdll, syscalls, internals]
links:
  - target: "[[Memory/Knowledge/pe_header_structure]]"
    relationship: related-to
---

Technical content here. Structure as appropriate for the material:
- Factual reference (data structures, APIs, protocols)
- Technique documentation (procedures, methodologies)
- Analysis findings (reversing results, behavioral observations)

**Source:** [Attribution and version info]

**Verified:** [How/when this was validated — binary analysis, source comparison, live testing]

**Applicability:** [Version scope — what OS/tool/version this applies to]
```

### Knowledge vs. Other Memory Types

| Property | Knowledge | Reference | Semantic |
|---|---|---|---|
| Purpose | Ingested technical documentation | Pointers to external resources | Consolidated abstractions |
| Decay | None (0.0) | 0.35 | 0.2 |
| Archival | Only by supersession or obsolescence | Auto-archivable | Auto-archivable |
| Scoring | Relevance × confidence (no activation decay) | Standard scoring | Standard scoring |
| Source | Attributed to specific document/version | Location pointers | Synthesized from observations |
| Compression | Full → detailed → summary (no gist — too precise) | Standard | Standard |
| Verification | Can be validated against live systems | N/A | N/A |

### When to Create Knowledge Entries

- Ingesting chapters or sections from technical books
- Documenting findings from reverse engineering sessions
- Recording API behaviors, data structures, or protocols from primary sources
- Capturing tool-specific knowledge (Binary Ninja scripting patterns, nmap internals)
- Any technical reference that should persist independent of recency

### Version Scoping

Knowledge entries are tied to a source and version. When the source material is updated or findings are invalidated by a newer OS/tool version:

1. Create a new knowledge entry for the updated version
2. Add a `supersedes` link from new to old
3. Old entry moves to Archive with `archive_reason: superseded`

Do NOT silently update knowledge entries in place — version history matters for understanding what changed and when.

---

## Archive Entry

Archived memories retain their original frontmatter plus:

```yaml
---
# ... original frontmatter preserved ...
archived: 2026-05-01T10:00:00-04:00
archive_reason: decay | superseded | contradiction | user-request
final_score: 0.12                       # score at time of archival
superseded_by: "[[Memory/Feedback/new_entry]]"  # if applicable
---
```

Archived memories are never loaded into context automatically. They exist for human review and potential recovery.

---

## Consolidation Log Entry

```yaml
---
type: consolidation_log
timestamp: 2026-04-06T15:00:00-04:00
trigger: compact | scheduled | manual
buffer_entries_processed: 5
memories_created: 2
memories_updated: 3
memories_archived: 1
memories_unchanged: 8
duration_notes: "brief"
---

### Actions Taken
- Created [[Memory/Feedback/example]] from Buffer/2026-04-06_correction.md
- Updated [[Memory/User/role]] — increased confidence (reinforced)
- Archived [[Memory/Project/old_initiative]] — below threshold, project completed
```

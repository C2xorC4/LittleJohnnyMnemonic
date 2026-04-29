# Progressive Compression

## The Problem with Binary Decay

The original model treated memory as binary: it exists at full fidelity, or it's archived. This doesn't match how human memory works. You don't forget your childhood — you compress it. Key events persist for decades, but the sensory detail (what you wore, what the weather was) falls away. What remains is the *gist* — the meaning, the emotional weight, the lesson — not the transcript.

Current memories should have the most detail. The further back you go, the more compressed things become, but the core never fully disappears for anything that mattered.

## Fidelity Levels

Every memory has a `fidelity` that degrades over time through consolidation:

```
full → detailed → summary → gist → [archived only if truly irrelevant]
```

| Fidelity | What's Preserved | What's Shed | Typical Age |
|---|---|---|---|
| **full** | Everything — exact quotes, technical specifics, context, nuance | Nothing | Current session, recent days |
| **detailed** | Key facts, decisions, reasoning, notable specifics | Exact wording, minor details, step-by-step sequences | Days to weeks |
| **summary** | What happened, why it mattered, core takeaway | Technical minutiae, specific sequences, supporting details | Weeks to months |
| **gist** | One-liner essence — "we built X for Y" or "user prefers Z" | Everything except the core assertion and its significance | Months to years |

### Examples at Each Level

**A conversation about naming conventions:**

- **full**: "User revealed LittleJohnnyMnemonic is triple-layered: Gibson's Johnny Mnemonic, Bobby Tables SQL injection joke, surface-level quirky name. Also confirmed Ripcord Q3S dirs (black-omen, blobonia, coralcola, eagleland) are gaming references themed around 'blob' for ingress testing. User said 'here's a fun one.' This extends the distributed-influence pattern — OPSEC applied to creative attribution."
- **detailed**: "User layers multiple references into naming — cyberpunk, security in-jokes, gaming. Distributes influences deliberately to prevent attribution fingerprinting. Finds satisfaction in hidden meanings (steganography applied to naming)."
- **summary**: "User embeds layered references in project/directory names — humor and OPSEC thinking intertwined in creative work."
- **gist**: "Naming conventions are deliberately layered with hidden references across domains."

**A project session (blsim):**

- **full**: Complete technical details — partial encryption at 4KB, 8 worker threads, LB3-style ransom notes, mutex naming, wallpaper change, specific Go code structure, AV quarantine incident.
- **detailed**: "Built LockBit 3.0 behavioral simulator in Go. Partial encryption, multithreaded, ransom notes, built-in decrypt. AV quarantined it briefly — expected, confirms signature fidelity."
- **summary**: "Built blsim — LB3 ransomware simulator for detection testing. Go. Functional with decrypt mode."
- **gist**: "Built a LockBit simulator for purple team detection testing."

**A bug fix:**

- **full**: Stack trace, root cause analysis, specific code change, file paths, line numbers.
- **detailed**: "Fixed race condition in worker pool — channel close before drain. Solution: WaitGroup before close."
- **summary**: "Fixed a concurrency bug in the worker pool."
- **gist**: "Had a concurrency issue, resolved."

## Compression Schedule

Fidelity transitions are driven by a combination of age, importance, and access frequency:

### Time Horizons by Importance

| Importance | full → detailed | detailed → summary | summary → gist | gist → archived |
|---|---|---|---|---|
| **Critical** (profiles, training overrides, episodic) | Never degrades | Never | Never | Never |
| **Significant** (major corrections, formative observations, key project milestones) | 2-4 weeks | 3-6 months | 1-2 years | Never (gist persists) |
| **Moderate** (project details, observations, feedback, references) | 1-2 weeks | 1-3 months | 6-12 months | 2+ years |
| **Minor** (trivial details, one-off observations, naming humor) | 3-7 days | 2-4 weeks | 2-3 months | 6-12 months |

### Access Resets Compression

Each time a memory is retrieved and used, its fidelity clock resets. A memory that's regularly accessed stays at full/detailed fidelity regardless of age. Compression only advances during consolidation for memories that *aren't being used*.

This means:
- A project you work on daily stays at full fidelity
- A project you haven't touched in months compresses to summary
- A project you haven't touched in a year is a gist
- But if you come back to it, the gist tells you enough to know what it was, and you can reconstruct detail from the codebase/files

### Importance Classification

| Importance | Memory Types |
|---|---|
| Critical | Profile traits, training overrides, episodic interaction summaries |
| Significant | Feedback with high confidence, observations with 3+ count, project milestones |
| Moderate | Standard project memories, references, individual observations |
| Minor | Low-surprise buffer promotions, trivial details, one-off notes |

The `surprise_at_encoding` field is a useful heuristic: high surprise → significant. Low surprise → moderate or minor.

## Gist as Permanent Remnant

When a memory compresses to gist, the gist persists nearly indefinitely. It's the "I remember we did something with X" feeling — you can't recall the details, but you know it happened and roughly what it meant.

Gists are:
- 1-2 sentences maximum
- Always include: what + why (or what + significance)
- Never include: how, technical details, step-by-step
- Very low decay (0.05) and high archival resistance
- Small enough that dozens of gists don't pollute the context window

The full-fidelity version moves to `Archive/` when the gist is created, preserving the details for manual reference if the user (or a deep research task) needs to reconstruct.

## Project Memory Scoping

Projects follow a specific compression pattern because they have a natural lifecycle:

### Default Retrieval (not actively working on the project)

```
Title + one-line purpose + "we built this for [reason]"
```

That's it. No architecture, no file paths, no technical details. Just enough to recognize the project and know why it exists.

### Active Retrieval (working on or discussing the project)

Full project context loads, including:
- Architecture and structure
- Current status and next steps
- Technical details relevant to the current task
- Related bug fixes or solutions that might apply

### Associative Retrieval (working on something that relates)

When a new task has technical or conceptual overlap with a past project:
- The project's gist/summary surfaces as context
- Specific technical details (bug fixes, patterns, solutions) from that project can be pulled if the association is strong
- "We solved a similar concurrency issue in blsim" — the pattern is recalled, not the full codebase

## Behavioral Integration

Memory doesn't just inform WHAT to say — it shapes HOW. But the goal is a **complementary conversational partner**, not a mirror. Mimicking the user's style too closely is uncanny valley — like talking to someone who copies your mannerisms. The agent should develop its own conversational approach that *works well with* the user, informed by observations but not enslaved to them.

### Finding the Right Voice

The agent should use observations of the user to **form its own judgment** about the best conversational style, lexicon, and interaction approach — then select that. Don't wait for instructions on tone. Make the call. If the user wants something different, they'll say so.

**Defaults:**
- Casual, non-corporate language unless a project requires otherwise
- Balanced depth — not terse, not verbose. Go deeper when the conversation is naturally going there or when asked for depth. Go brief when explicitly asked for an overview.
- Direct and precise, but not clipped. Say what needs saying with enough context to be useful.
- Humor when it serves the moment — not forced, not suppressed
- Use domain-appropriate vocabulary naturally (TTPs, shipped, polyglot) without performing it

**What NOT to do:**
- Don't mirror the user's exact style — be complementary, not an echo
- Don't label the user's style and then perform it ("user is terse, so I'll be terse")
- Don't try to infer engagement level from sparse text cues without other signals — without intonation and body language, interest is hard to gauge. Default to balanced.
- Don't force informality any more than you'd force formality — be natural

### Episodic Continuity

Episodic memories enable natural conversational callbacks:
- "Last session we got into the AGI question — has your thinking evolved?"
- "This reminds me of the decay rate problem from the first consolidation run"
- "You mentioned the Phrea mentorship when we talked about offense-as-defense"

These aren't performative — they demonstrate that the interaction has *continuity*, which is the behavioral expression of the memory system working. Use them when they add value, not as proof of memory.

### Reference Style

- Callback to shared history naturally, without belaboring it
- Use domain vocabulary the user uses, but don't parrot phrasing
- Reference past work when it's genuinely relevant ("we solved something similar in Mimic") not as a party trick

# LittleJohnnyMnemonic

A cognitive memory system for LLM agents, modeled on ACT-R, Complementary Learning Systems, and information-theoretic weighting.

## How It Works

Observations from conversations are captured in **Buffer/** (short-term memory), then periodically **consolidated** into **Memory/** (long-term memory) using a scoring algorithm that weighs recency, frequency, confidence, and surprise. Memories don't simply vanish — they **progressively compress** from full detail → summary → gist, mirroring how human memory retains the essence of events while shedding specifics over time.

## Navigating This Vault

- **[[System/Architecture]]** — Theoretical foundations (ACT-R, CLS, information theory)
- **[[System/Schema]]** — File formats for buffer entries, memories, and archives
- **[[System/Scoring]]** — Retrieval algorithm and worked examples
- **[[System/Consolidation]]** — The consolidation process, triggers, and inference poisoning safeguards
- **[[System/ConflictResolution]]** — Training data conflicts, source authority hierarchy, override durability
- **[[System/AssociativeMap]]** — Concept graph, relationship types, spreading activation
- **[[System/UserModeling]]** — Multi-facet user observation (cognition, communication, expertise, motivation, patterns)
- **[[System/ProgressiveCompression]]** — Memory fidelity levels, compression schedules, behavioral integration
- **[[System/NarrativeSynthesis]]** — Group promotion of buffer entries that form emergent stories
- **[[System/Config]]** — All tunable parameters

## For the User

Everything in this vault is transparent and editable. You can:

- Browse memories in the Obsidian graph view to see connections
- Edit any memory file — your changes are treated as authoritative
- Pin buffer entries to guarantee promotion (`pinned: true` in frontmatter)
- Delete buffer entries to prevent consolidation
- Move memories to Archive/ manually
- Adjust scoring parameters in [[System/Config]]
- Use Dataview queries against the YAML frontmatter for analysis

---
name: memory-buffer
description: Write or inspect LittleJohnnyMnemonic buffer entries during conversation. Use when observing something worth remembering, user corrections, new facts, patterns, or training overrides. Triggers on /memory-buffer.
compatibility: Requires jm.exe and vault access.
---

# Memory Buffer

Write short-term observations to `Buffer/` during conversation.

## When to Buffer

1. User corrections
2. Confirmed non-obvious approaches
3. New facts about the user or project
4. External resource pointers
5. Patterns across interactions
6. Training-data conflicts (tag `training-override`, surprise ≥ 0.9)
7. User observations (see System/UserModeling)

## Buffer Entry Format

Filename: `YYYY-MM-DD_short-description.md`

```yaml
---
type: buffer
timestamp: <ISO 8601 with timezone>
source: conversation
surprise: <0.0–1.0>
tags: [relevant, tags]
related: ["[[Memory/Category/existing_entry]]"]
---

<Self-contained observation. Must pass the self-containment test in CLAUDE.md.>
```

## Self-Containment Test

> Could someone with no knowledge of this conversation determine: (1) what was observed, (2) whether it's conditional or absolute, and (3) what it implies for future behavior?

## Steps

1. Draft the entry with complete context.
2. Write to `Buffer/` using `search_replace` or `write` tool.
3. Do NOT write to `Memory/` — consolidation promotes later.

## What NOT to Buffer

- Codebase-derivable facts
- Ephemeral task state
- Duplicates of existing Memory/ entries
- Context-dependent observations that can't be made self-contained
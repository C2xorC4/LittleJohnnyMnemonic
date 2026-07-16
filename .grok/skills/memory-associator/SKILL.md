---
name: memory-associator
description: Mid-workflow memory association check against the LJM knowledge base. Use on topic shifts, design decisions, research discoveries, or natural pauses in focused work. Triggers on /memory-associator.
compatibility: Requires jm and spawn_subagent.
---

# Memory Associator

Background association check — does what we're doing now connect to stored knowledge?

## When to Associate

- Topic shifts
- Research or code discoveries
- Design decisions
- Deep discussion (richest associative map)
- **Don't over-trigger** — focused coding with clear direction doesn't need constant lookup

## Steps

1. Spawn a background associator:

```
spawn_subagent(
  subagent_type: "memory-associator",
  description: "Memory association check",
  prompt: "Context: <brief description of current topic/activity>",
  background: true,
  capability_mode: "execute",
  cwd: "<vault root>"
)
```

`execute` is required — the associator runs `jm associate` via shell. Use `read-only` only if you run association yourself in the parent and pass results in the prompt.

2. Or run directly:

```bash
VAULT="${JM_VAULT_ROOT:-}"
JM=""
for c in "$VAULT/agent/jm" "$VAULT/jm" "$VAULT/agent/jm.exe" "$VAULT/jm.exe"; do
  [[ -n "$VAULT" && -x "$c" ]] && JM="$c" && break
done
[[ -z "$JM" && -x ./agent/jm ]] && JM=./agent/jm
"$JM" associate --no-update "<context description>"
```

3. Weave relevant connections naturally — don't announce "the associator found..."
4. Buffer enrichment opportunities; never modify LTM directly.

## Rules

- Don't force relevance.
- Cross-domain connections are highest value.
- One follow-up question at a time, at natural breaks.
- Read disengagement — short answer + redirect = drop it.

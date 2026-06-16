---
name: memory-daydream
description: Launch LittleJohnnyMnemonic daydream exploration — autonomous knowledge graph wandering. Use on substantive turns (long prompts or dense retrieval), at reflective breaks, or when the UserPromptSubmit hook emits a daydream-nudge. Triggers on /memory-daydream.
compatibility: Requires jm.exe, spawn_subagent, and memory-daydream agent in ~/.grok/agents/ or .grok/agents/.
---

# Memory Daydream

Launch background daydream subagents (`subagent_type: memory-daydream`) to explore the knowledge graph without a specific goal.

## When to Fire

- User prompt ≥200 characters OR ≥5 memories retrieved (hook emits `<daydream-nudge>`)
- Reflective break, milestone, or design decision
- User explicitly asks for tangential exploration

**Volley composition** (when firing more than one):

- At least one seeded from the current topic
- At least one random walk from an unrelated graph corner

## Steps

1. Resolve vault root (use for `cwd` on every spawn):

```powershell
$vault = if ($env:JM_VAULT_ROOT) { $env:JM_VAULT_ROOT } else { "D:\Repos\LLM\LittleJohnnyMnemonic" }
```

2. Spawn daydream subagents **in parallel** with `background: true` and `capability_mode: "all"` (needs shell for `jm.exe`, writes for breadcrumbs, read for exploration):

```
spawn_subagent(
  subagent_type: "memory-daydream",
  description: "Daydream — topic seed",
  prompt: "Memory Daydream Agent — seed from current topic: <brief context>. Follow the memory-daydream agent protocol.",
  background: true,
  capability_mode: "all",
  cwd: "<vault root>"
)

spawn_subagent(
  subagent_type: "memory-daydream",
  description: "Daydream — random walk",
  prompt: "Memory Daydream Agent — random walk: pick an unrelated starting point in Memory/Knowledge/. Follow the memory-daydream agent protocol.",
  background: true,
  capability_mode: "all",
  cwd: "<vault root>"
)
```

3. When results return, surface genuinely relevant findings naturally. Hold null results — don't report them.

4. If a daydream returns an **inline-fallback** breadcrumb, persist it to `Buffer/Daydream/` with `search_replace`.

## Verify agent is registered

Open `/config-agents` — `memory-daydream` should appear. If missing, run `.\grok\install.ps1` from the vault root and restart the session (or press `r` in `/hooks`).

## Rules

- Daydreams explore for their own sake — value may not be apparent until later.
- Don't fabricate connections.
- One thread per agent.
- Null results are fine — report honestly, don't manufacture insights.
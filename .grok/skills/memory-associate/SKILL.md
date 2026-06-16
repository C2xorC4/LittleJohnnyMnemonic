---
name: memory-associate
description: Run LittleJohnnyMnemonic contextual association against the current topic. Use when checking what the vault knows about a subject, mid-workflow memory lookup, or when the user asks to associate, recall, or search memory. Triggers on /memory-associate.
compatibility: Requires jm.exe built in the LJM vault (agent/jm.exe).
---

# Memory Associate

Run free-text association against the LJM vault and surface relevant memories.

## Steps

1. Identify the current topic or activity as a concise phrase (1–2 sentences max).
2. Run association:

```powershell
$jm = if ($env:JM_VAULT_ROOT) { Join-Path $env:JM_VAULT_ROOT "agent\jm.exe" } else { "D:\Repos\LLM\LittleJohnnyMnemonic\agent\jm.exe" }
& $jm associate --no-update "<topic description>"
```

3. Evaluate results:
   - Genuinely relevant vs keyword overlap?
   - Would any memory change or improve current work?
   - Cross-domain connections (highest value)?
   - Enrichment opportunities — concepts in context not yet in vault?

4. Weave relevant findings naturally into the conversation. Do not announce "association found."

5. If enrichment is warranted, buffer an update — never write directly to `Memory/`.

## Adaptive edge citations

When the `user-prompt-submit` hook retrieves memories, it emits:

```xml
<retrieval-session id="uuid-here"/>
```

Use that ID when a loaded memory materially shaped your response:

```powershell
& $jm associate --cite "Memory/Knowledge/entry_name,how it was used,true" --session <retrieval-session-id>
```

The stop hook also auto-harvests `Memory/` path citations from assistant output against the same retrieval session — manual `--cite` is optional reinforcement.

## Rules

- Quality over quantity — one genuine insight beats five marginal matches.
- Cross-domain connections are gold.
- Say "no notable associations" honestly when nothing useful surfaces.
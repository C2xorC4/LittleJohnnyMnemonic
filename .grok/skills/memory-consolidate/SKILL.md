---
name: memory-consolidate
description: Run LittleJohnnyMnemonic buffer consolidation — promote buffer entries to long-term memory. Use when the user asks to consolidate, compact memory, or when Buffer/ exceeds threshold. Triggers on /memory-consolidate.
compatibility: Requires jm.exe built in the LJM vault.
---

# Memory Consolidate

Run the four-phase consolidation pipeline on `Buffer/`.

## When to Run

- User explicitly requests consolidation
- `/compact` or conversation compaction event
- Buffer exceeds 20 entries (hooks may auto-trigger in background)

## Steps

1. Check buffer status:

```powershell
$jm = if ($env:JM_VAULT_ROOT) { Join-Path $env:JM_VAULT_ROOT "agent\jm.exe" } else { "D:\Repos\LLM\LittleJohnnyMnemonic\agent\jm.exe" }
& $jm buffer count
& $jm status
```

2. Run consolidation (foreground — user-initiated):

```powershell
& $jm consolidate
```

3. Review output: promoted, merged, held, archived counts.
4. If protocol or capabilities changed, update `CLAUDE.md` and `docs/reflections/EXECUTIVE_SUMMARY.md` per post-change documentation rules.

## Rules

- Never write directly to `Memory/` during conversation — consolidation handles promotion.
- Merge over create when an existing memory covers the same topic.
- Preserve user-edited Memory/ files as authoritative (confidence = 1.0).
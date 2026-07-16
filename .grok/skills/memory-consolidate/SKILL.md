---
name: memory-consolidate
description: Run LittleJohnnyMnemonic buffer consolidation — promote buffer entries to long-term memory. Use when the user asks to consolidate, compact memory, or when Buffer/ exceeds threshold. Triggers on /memory-consolidate.
compatibility: Requires jm built in the LJM vault (agent/jm on Unix, agent/jm.exe on Windows).
---

# Memory Consolidate

Run the four-phase consolidation pipeline on `Buffer/`.

## When to Run

- User explicitly requests consolidation
- `/compact` or conversation compaction event
- Buffer exceeds 20 entries (hooks may auto-trigger in background)

## Resolve vault + jm

```bash
VAULT="${JM_VAULT_ROOT:-}"
JM=""
for c in "$VAULT/agent/jm" "$VAULT/jm" "$VAULT/agent/jm.exe" "$VAULT/jm.exe"; do
  [[ -n "$VAULT" && -x "$c" ]] && JM="$c" && break
done
[[ -z "$JM" && -x ./agent/jm ]] && JM=./agent/jm
[[ -z "$JM" && -x ./agent/jm.exe ]] && JM=./agent/jm.exe
```

## Steps

1. Check buffer status:

```bash
"$JM" buffer count
"$JM" status
```

2. Run consolidation (foreground — user-initiated):

```bash
"$JM" consolidate
```

3. Review output: promoted, merged, held, archived counts.
4. If protocol or capabilities changed, update `CLAUDE.md` and `docs/reflections/EXECUTIVE_SUMMARY.md` per post-change documentation rules.

## Rules

- Never write directly to `Memory/` during conversation — consolidation handles promotion.
- Merge over create when an existing memory covers the same topic.
- Preserve user-edited Memory/ files as authoritative (confidence = 1.0).

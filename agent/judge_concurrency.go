package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Host-wide concurrency cap for `claude -p` judge subprocesses.
//
// The CLI judge fallback (judge_api.go, callHaikuViaCLI) cold-boots a full
// ~320MB Claude Code process per call. Judge calls originate from multiple
// independent OS processes — the in-process consolidation loop, every detached
// `jm rule-judge` spawned by the Stop hook, and the scheduled autodream's
// downstream consolidation — so an in-process semaphore can't bound the total.
// This is a filesystem-backed counting semaphore shared across all jm processes
// on the host: at most JudgeCLIMaxConcurrent slot files exist at once.
//
// Design choices:
//   - Slots live in the OS temp dir, not the vault — the cap is about host
//     memory pressure, which is shared across vaults/invocations.
//   - Non-blocking: if every slot is taken, acquire returns ok=false and the
//     caller degrades to its heuristic fallback. We never queue/block, because
//     a blocked judge holds a process open — exactly what we're avoiding.
//   - Stale reclamation: a slot older than judgeSlotTTL is assumed orphaned
//     (its owner crashed without releasing) and is reclaimed. The TTL is well
//     above the CLI's own 60s context timeout so a live-but-slow call is never
//     stolen from.

const (
	judgeSlotDirName = "jm-judge-slots"
	judgeSlotTTL     = 3 * time.Minute
)

// acquireJudgeCLISlot attempts to claim one of maxConcurrent host-wide slots.
// Returns a release func and ok=true on success; release is safe to call once.
// On ok=false the caller must NOT spawn a CLI and should fall back to heuristics.
// maxConcurrent <= 0 disables the cap (always grants, release is a no-op).
func acquireJudgeCLISlot(maxConcurrent int) (release func(), ok bool) {
	if maxConcurrent <= 0 {
		return func() {}, true
	}

	dir := filepath.Join(os.TempDir(), judgeSlotDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		// Can't manage slots — fail closed (no CLI spawn) so a broken temp dir
		// can't disable the cap and let the swarm back in.
		return nil, false
	}

	for i := 0; i < maxConcurrent; i++ {
		slot := filepath.Join(dir, fmt.Sprintf("slot-%d.lock", i))
		if tryClaimSlot(slot) {
			var released bool
			return func() {
				if released {
					return
				}
				released = true
				_ = os.Remove(slot)
			}, true
		}
	}
	return nil, false
}

// tryClaimSlot creates slot exclusively. If it already exists but is stale
// (older than judgeSlotTTL), it reclaims it. Returns true if now owned.
func tryClaimSlot(slot string) bool {
	f, err := os.OpenFile(slot, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err == nil {
		fmt.Fprintf(f, "%d\n%s\n", os.Getpid(), time.Now().Format(time.RFC3339))
		_ = f.Close()
		return true
	}
	if !os.IsExist(err) {
		return false
	}
	// Occupied — reclaim if stale.
	info, statErr := os.Stat(slot)
	if statErr != nil {
		return false
	}
	if time.Since(info.ModTime()) <= judgeSlotTTL {
		return false
	}
	// Stale: remove and retry once. A racing reclaimer means at most one wins
	// the subsequent O_EXCL create.
	if err := os.Remove(slot); err != nil {
		return false
	}
	f, err = os.OpenFile(slot, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return false
	}
	fmt.Fprintf(f, "%d\n%s\n", os.Getpid(), time.Now().Format(time.RFC3339))
	_ = f.Close()
	return true
}

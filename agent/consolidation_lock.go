package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Single-flight lock for consolidation runs.
//
// Consolidation is fired from several independent triggers — the Stop hook
// (after every assistant turn), the SessionStart hook, and the scheduled
// autodream's downstream daydream session (which itself fires a SessionStart
// hook). With rapid turns or overlapping triggers, multiple detached
// `jm consolidate` processes can run at once, each re-scanning the same buffer
// and (when the CLI judge path is live) stacking judge subprocesses. This lock
// ensures at most one consolidation runs per vault at a time.
//
// Filesystem-backed (O_CREATE|O_EXCL) so it holds across separate OS processes.
// Stale locks (older than consolidationLockTTL — a crashed run that never
// released) are reclaimed so a single crash can't wedge consolidation forever.

const consolidationLockTTL = 15 * time.Minute

func consolidationLockPath(vaultRoot string) string {
	return filepath.Join(vaultRoot, "Metrics", "consolidation.lock")
}

// acquireConsolidationLock claims the per-vault consolidation lock. Returns a
// release func and ok=true on success. On ok=false another run holds a fresh
// lock and the caller should return without consolidating. release is
// idempotent and safe to defer.
func acquireConsolidationLock(vaultRoot string) (release func(), ok bool) {
	path := consolidationLockPath(vaultRoot)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		// Can't manage the lock — proceed unlocked rather than wedging
		// consolidation entirely (consolidation itself is idempotent enough
		// that a rare double-run is less bad than never running).
		return func() {}, true
	}

	if claimConsolidationLock(path) {
		return releaserFor(path), true
	}

	// Held — reclaim only if stale.
	info, err := os.Stat(path)
	if err != nil {
		return nil, false
	}
	if time.Since(info.ModTime()) <= consolidationLockTTL {
		return nil, false
	}
	if err := os.Remove(path); err != nil {
		return nil, false
	}
	if claimConsolidationLock(path) {
		return releaserFor(path), true
	}
	return nil, false
}

// consolidationLockHeld reports whether a fresh consolidation lock exists.
// Used by the hook spawner as a cheap pre-check to avoid spawning a process
// that would immediately exit on lock contention.
func consolidationLockHeld(vaultRoot string) bool {
	info, err := os.Stat(consolidationLockPath(vaultRoot))
	if err != nil {
		return false
	}
	return time.Since(info.ModTime()) <= consolidationLockTTL
}

func claimConsolidationLock(path string) bool {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return false
	}
	fmt.Fprintf(f, "%d\n%s\n", os.Getpid(), time.Now().Format(time.RFC3339))
	_ = f.Close()
	return true
}

func releaserFor(path string) func() {
	var released bool
	return func() {
		if released {
			return
		}
		released = true
		_ = os.Remove(path)
	}
}

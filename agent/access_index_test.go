package main

import (
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestAccessIndex_LoadAggregatesBaseAndEvents(t *testing.T) {
	vault := t.TempDir()
	t0 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	// Seed base via events then fold, then add more events.
	if err := recordAccessBatch(vault, []string{"memory/a", "memory/a", "memory/b"}, t0, ""); err != nil {
		t.Fatal(err)
	}
	if err := foldAccessLog(vault); err != nil {
		t.Fatal(err)
	}
	t1 := t0.Add(48 * time.Hour)
	if err := recordAccess(vault, "memory/a", t1, ""); err != nil {
		t.Fatal(err)
	}

	idx := loadAccessIndex(vault, DefaultConfig())
	if idx["memory/a"].Count != 3 {
		t.Errorf("a count = %d, want 3", idx["memory/a"].Count)
	}
	if !idx["memory/a"].LastAccessed.Equal(t1) {
		t.Errorf("a last_accessed = %v, want %v", idx["memory/a"].LastAccessed, t1)
	}
	if idx["memory/b"].Count != 1 {
		t.Errorf("b count = %d, want 1", idx["memory/b"].Count)
	}
}

// TestAccessIndex_ConcurrentAppendsLossless is the core guarantee: many
// concurrent writers (modeling hooks + autodream + retrieve) must not lose any
// access event — the reason we use an append log instead of a rewritten file.
func TestAccessIndex_ConcurrentAppendsLossless(t *testing.T) {
	vault := t.TempDir()
	ts := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	const writers = 20
	const perWriter = 50
	var wg sync.WaitGroup
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perWriter; i++ {
				_ = recordAccess(vault, "memory/hot", ts, "")
			}
		}()
	}
	wg.Wait()

	idx := loadAccessIndex(vault, DefaultConfig())
	if got := idx["memory/hot"].Count; got != writers*perWriter {
		t.Fatalf("lost events under concurrency: got %d, want %d", got, writers*perWriter)
	}
}

func TestAccessIndex_FoldThenAppendNotLost(t *testing.T) {
	vault := t.TempDir()
	ts := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		_ = recordAccess(vault, "memory/x", ts, "")
	}
	if err := foldAccessLog(vault); err != nil {
		t.Fatal(err)
	}
	// post-fold appends must accumulate on top of the folded base
	_ = recordAccess(vault, "memory/x", ts, "")
	if got := loadAccessIndex(vault, DefaultConfig())["memory/x"].Count; got != 6 {
		t.Fatalf("count after fold+append = %d, want 6", got)
	}
}

func TestAccessIndex_SeedAndMerge(t *testing.T) {
	vault := t.TempDir()
	seedTime := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	m := &MemoryEntry{
		Type: TypeKnowledge, Title: "K", AccessCount: 42, LastAccessed: seedTime,
		FilePath: filepath.ToSlash(filepath.Join("Memory", "Knowledge", "k.md")), FileName: "k.md",
	}
	if err := seedAccessIndex(vault, []*MemoryEntry{m}); err != nil {
		t.Fatal(err)
	}
	// A fresh load of the same entry (simulating frontmatter that may be stale)
	// must be overlaid with the sidecar value, plus any new events.
	_ = recordAccess(vault, "memory/knowledge/k", seedTime.Add(time.Hour), "")
	fresh := &MemoryEntry{Type: TypeKnowledge, AccessCount: 1, FilePath: m.FilePath, FileName: m.FileName}
	mergeAccessIndex(vault, []*MemoryEntry{fresh})
	if fresh.AccessCount != 43 { // 42 seeded + 1 event
		t.Fatalf("merged count = %d, want 43", fresh.AccessCount)
	}
}

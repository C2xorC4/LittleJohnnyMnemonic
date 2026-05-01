package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeMemFile constructs Memory/<subdir>/<name>.md with the given frontmatter
// body and returns the path. Tests use this to populate vaults with controlled
// LTM contents without going through the full WriteMemoryEntry path.
func writeMemFile(t *testing.T, vault, subdir, name, frontmatter, body string) string {
	t.Helper()
	dir := filepath.Join(vault, "Memory", subdir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	content := "---\n" + frontmatter + "\n---\n" + body
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// writeBufFile is the buffer analogue of writeMemFile.
func writeBufFile(t *testing.T, vault, subdir, name, frontmatter, body string) string {
	t.Helper()
	dir := filepath.Join(vault, "Buffer", subdir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	content := "---\n" + frontmatter + "\n---\n" + body
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestPickSeed_EmptySourcesReturnsError(t *testing.T) {
	_, err := PickSeed(t.TempDir(), nil, false, seededRand(1, 2))
	if !errors.Is(err, ErrNoSeedAvailable) {
		t.Errorf("err = %v, want ErrNoSeedAvailable", err)
	}
}

func TestPickSeed_AllZeroWeightsReturnsError(t *testing.T) {
	_, err := PickSeed(t.TempDir(), map[string]float64{"buffer": 0, "knowledge": 0}, false, seededRand(1, 2))
	if !errors.Is(err, ErrNoSeedAvailable) {
		t.Errorf("err = %v, want ErrNoSeedAvailable", err)
	}
}

func TestPickSeed_NoEntriesReturnsError(t *testing.T) {
	vault := t.TempDir()
	_, err := PickSeed(vault, map[string]float64{"buffer": 100}, false, seededRand(1, 2))
	if !errors.Is(err, ErrNoSeedAvailable) {
		t.Errorf("err = %v, want ErrNoSeedAvailable", err)
	}
}

func TestPickSeed_FallsThroughEmptyCategoryToNext(t *testing.T) {
	vault := t.TempDir()
	// Knowledge has entries; episodic does not. Heavy weight on episodic
	// should still produce a knowledge seed once episodic is found empty.
	writeMemFile(t, vault, "Knowledge", "k1.md",
		"type: knowledge\ntitle: K1\nlast_accessed: 2026-04-30T10:00:00Z\naccess_count: 5", "body")

	seed, err := PickSeed(vault, map[string]float64{"episodic": 95, "knowledge": 5}, false, seededRand(1, 2))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if seed.Source != "knowledge" {
		t.Errorf("Source = %q, want knowledge (episodic was empty, should have fallen through)", seed.Source)
	}
}

func TestPickSeed_RecencyBiasNarrowsToTopHalf(t *testing.T) {
	vault := t.TempDir()

	// 6 buffer files with mtimes spaced 1 hour apart (newest first by index).
	now := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 6; i++ {
		path := writeBufFile(t, vault, "", "buf-"+string(rune('a'+i))+".md",
			"type: buffer\ntimestamp: 2026-04-29T00:00:00Z\nsource: conversation\nsurprise: 0.5", "body")
		mtime := now.Add(-time.Duration(i) * time.Hour)
		if err := os.Chtimes(path, mtime, mtime); err != nil {
			t.Fatal(err)
		}
	}

	// With recencyBias=true, the pool is the top 3 (rounded up from 6/2).
	// Run many iterations and verify only those 3 ever get picked.
	seenNames := map[string]bool{}
	for i := 0; i < 200; i++ {
		seed, err := PickSeed(vault, map[string]float64{"buffer": 1}, true, seededRand(uint64(i), 2))
		if err != nil {
			t.Fatalf("iter %d: %v", i, err)
		}
		seenNames[seed.Title] = true
	}
	if len(seenNames) > 3 {
		t.Errorf("recency bias should narrow to top 3, got %d distinct: %v", len(seenNames), seenNames)
	}
	for _, want := range []string{"buf-a", "buf-b", "buf-c"} {
		if !seenNames[want] {
			t.Errorf("expected to see %q in top-half samples, did not", want)
		}
	}
}

func TestPickSeed_NoRecencyBiasUniform(t *testing.T) {
	vault := t.TempDir()

	now := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 4; i++ {
		path := writeBufFile(t, vault, "", "buf-"+string(rune('a'+i))+".md",
			"type: buffer\ntimestamp: 2026-04-29T00:00:00Z\nsource: conversation\nsurprise: 0.5", "body")
		mtime := now.Add(-time.Duration(i) * time.Hour)
		if err := os.Chtimes(path, mtime, mtime); err != nil {
			t.Fatal(err)
		}
	}

	seenNames := map[string]bool{}
	for i := 0; i < 200; i++ {
		seed, err := PickSeed(vault, map[string]float64{"buffer": 1}, false, seededRand(uint64(i), 2))
		if err != nil {
			t.Fatalf("iter %d: %v", i, err)
		}
		seenNames[seed.Title] = true
	}
	// Without bias, all 4 should appear in 200 trials.
	if len(seenNames) != 4 {
		t.Errorf("uniform should reach all 4 entries, got %d distinct: %v", len(seenNames), seenNames)
	}
}

func TestPickSeed_BufferExcludesDaydreamBySource(t *testing.T) {
	vault := t.TempDir()
	writeBufFile(t, vault, "", "real.md",
		"type: buffer\ntimestamp: 2026-04-30T10:00:00Z\nsource: conversation\nsurprise: 0.5", "body")
	writeBufFile(t, vault, "Daydream", "wandered.md",
		"type: buffer\ntimestamp: 2026-04-30T11:00:00Z\nsource: daydream\nsurprise: 0.5", "body")

	for i := 0; i < 50; i++ {
		seed, err := PickSeed(vault, map[string]float64{"buffer": 1}, false, seededRand(uint64(i), 2))
		if err != nil {
			t.Fatalf("iter %d: %v", i, err)
		}
		// Use the immediate parent dir to identify Daydream-subdir entries —
		// matching "Daydream" anywhere in the path would false-positive on
		// temp-dir names that happen to contain the word (the test name
		// being one such case).
		parent := filepath.Base(filepath.Dir(seed.FilePath))
		if parent == "Daydream" {
			t.Errorf("daydream entry was picked: %s", seed.FilePath)
		}
		if seed.Title != "real" {
			t.Errorf("Title = %q, want real", seed.Title)
		}
	}
}

func TestPickSeed_DistributionAcrossCategories(t *testing.T) {
	vault := t.TempDir()
	writeMemFile(t, vault, "Knowledge", "k1.md",
		"type: knowledge\ntitle: K\nlast_accessed: 2026-04-30T10:00:00Z\naccess_count: 5", "")
	writeMemFile(t, vault, "Semantic", "s1.md",
		"type: semantic\ntitle: S\nlast_accessed: 2026-04-30T10:00:00Z\naccess_count: 5", "")

	// 80% knowledge, 20% semantic.
	const trials = 500
	r := seededRand(99, 100)
	knowledgeCount := 0
	for i := 0; i < trials; i++ {
		seed, err := PickSeed(vault, map[string]float64{"knowledge": 80, "semantic": 20}, false, r)
		if err != nil {
			t.Fatalf("iter %d: %v", i, err)
		}
		if seed.Source == "knowledge" {
			knowledgeCount++
		}
	}
	// Want roughly 400/500. Generous tolerance.
	if knowledgeCount < 350 || knowledgeCount > 450 {
		t.Errorf("80%% knowledge produced %d/500 (expected ~400 ±50)", knowledgeCount)
	}
}

func TestBuildReplayPair_HappyPath(t *testing.T) {
	vault := t.TempDir()
	now := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)

	// Recent: a buffer entry from 2 days ago
	bufPath := writeBufFile(t, vault, "", "recent.md",
		"type: buffer\ntimestamp: 2026-04-28T12:00:00Z\nsource: conversation\nsurprise: 0.5", "body")
	twoDaysAgo := now.Add(-48 * time.Hour)
	if err := os.Chtimes(bufPath, twoDaysAgo, twoDaysAgo); err != nil {
		t.Fatal(err)
	}

	// Stable: a crystallized semantic entry
	writeMemFile(t, vault, "Semantic", "stable.md",
		"type: semantic\ntitle: Crystallized\nlast_accessed: 2026-04-20T00:00:00Z\naccess_count: 30", "body")

	cfg := DefaultConfig()
	pair, err := BuildReplayPair(vault, cfg, now, seededRand(1, 2))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(pair.Recent.FilePath, "Buffer") {
		t.Errorf("Recent.FilePath = %s, want a Buffer/ entry", pair.Recent.FilePath)
	}
	if !strings.Contains(pair.Stable.FilePath, "Semantic") {
		t.Errorf("Stable.FilePath = %s, want a Memory/Semantic/ entry", pair.Stable.FilePath)
	}
}

func TestBuildReplayPair_NoRecentReturnsErr(t *testing.T) {
	vault := t.TempDir()
	now := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)

	// Only a stale buffer entry (older than max_age_days)
	bufPath := writeBufFile(t, vault, "", "old.md",
		"type: buffer\ntimestamp: 2026-01-01T00:00:00Z\nsource: conversation\nsurprise: 0.5", "body")
	tooOld := now.Add(-30 * 24 * time.Hour)
	if err := os.Chtimes(bufPath, tooOld, tooOld); err != nil {
		t.Fatal(err)
	}

	// And a crystallized stable
	writeMemFile(t, vault, "Semantic", "stable.md",
		"type: semantic\ntitle: S\nlast_accessed: 2026-04-20T00:00:00Z\naccess_count: 30", "")

	cfg := DefaultConfig() // max_age_days = 14
	_, err := BuildReplayPair(vault, cfg, now, seededRand(1, 2))
	if !errors.Is(err, ErrNoPairAvailable) {
		t.Errorf("err = %v, want ErrNoPairAvailable", err)
	}
}

func TestBuildReplayPair_NoStableReturnsErr(t *testing.T) {
	vault := t.TempDir()
	now := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)

	// Recent buffer is fine
	bufPath := writeBufFile(t, vault, "", "recent.md",
		"type: buffer\ntimestamp: 2026-04-30T00:00:00Z\nsource: conversation\nsurprise: 0.5", "body")
	if err := os.Chtimes(bufPath, now, now); err != nil {
		t.Fatal(err)
	}

	// Stable candidate but plastic (low access_count) — fails crystallized filter
	writeMemFile(t, vault, "Semantic", "plastic.md",
		"type: semantic\ntitle: P\nlast_accessed: 2026-04-20T00:00:00Z\naccess_count: 1", "")

	cfg := DefaultConfig()
	_, err := BuildReplayPair(vault, cfg, now, seededRand(1, 2))
	if !errors.Is(err, ErrNoPairAvailable) {
		t.Errorf("err = %v, want ErrNoPairAvailable (no crystallized stable)", err)
	}
}

func TestBuildReplayPair_RecentlyAccessedLTMSource(t *testing.T) {
	vault := t.TempDir()
	now := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)

	// Recently-accessed semantic (the recent partner)
	writeMemFile(t, vault, "Semantic", "recent.md",
		"type: semantic\ntitle: Recent\nlast_accessed: 2026-04-29T12:00:00Z\naccess_count: 2", "")
	// Crystallized semantic in a different category as stable
	writeMemFile(t, vault, "User", "stable.md",
		"type: user\ntitle: Stable\nlast_accessed: 2026-04-15T00:00:00Z\nprofile: true\naccess_count: 0", "")

	cfg := DefaultConfig()
	cfg.AutoDaydreamReplayRecentSource = "recently_accessed_ltm"
	cfg.AutoDaydreamReplayStableCategories = []string{"user"}

	pair, err := BuildReplayPair(vault, cfg, now, seededRand(1, 2))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if pair.Recent.Title != "Recent" {
		t.Errorf("Recent.Title = %q, want Recent", pair.Recent.Title)
	}
	if pair.Stable.Title != "Stable" {
		t.Errorf("Stable.Title = %q, want Stable (profile=true)", pair.Stable.Title)
	}
}

func TestBuildReplayPair_UnknownRecentSourceErrors(t *testing.T) {
	cfg := DefaultConfig()
	cfg.AutoDaydreamReplayRecentSource = "garbage"
	now := time.Now()

	_, err := BuildReplayPair(t.TempDir(), cfg, now, seededRand(1, 2))
	if err == nil {
		t.Fatal("expected error for unknown source")
	}
	// Should NOT be ErrNoPairAvailable — that would mask a config bug.
	if errors.Is(err, ErrNoPairAvailable) {
		t.Errorf("unknown source should produce a parse error, not ErrNoPairAvailable: %v", err)
	}
}

func TestMeetsStabilityFilter_Crystallized(t *testing.T) {
	cfg := DefaultConfig() // crystallized threshold = 25

	cases := []struct {
		name        string
		profile     bool
		accessCount int
		want        bool
	}{
		{"profile-true-low-count", true, 0, true},
		{"profile-true-high-count", true, 100, true},
		{"plastic", false, 5, false},
		{"mature-but-not-crystallized", false, 15, false},
		{"crystallized-by-count", false, 25, true},
		{"crystallized-by-count-above", false, 50, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := &MemoryEntry{Profile: c.profile, AccessCount: c.accessCount}
			got := meetsStabilityFilter(m, cfg)
			if got != c.want {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
}

func TestMeetsStabilityFilter_Mature(t *testing.T) {
	cfg := DefaultConfig() // mature threshold = 10
	cfg.AutoDaydreamReplayStableFilter = "mature"

	cases := []struct {
		accessCount int
		want        bool
	}{
		{0, false},
		{9, false},
		{10, true},
		{50, true},
	}
	for _, c := range cases {
		m := &MemoryEntry{AccessCount: c.accessCount}
		got := meetsStabilityFilter(m, cfg)
		if got != c.want {
			t.Errorf("access_count=%d: got %v, want %v", c.accessCount, got, c.want)
		}
	}
}

func TestCategoryToType_KnownCategories(t *testing.T) {
	cases := map[string]string{
		"user":      string(TypeUser),
		"feedback":  string(TypeFeedback),
		"project":   string(TypeProject),
		"reference": string(TypeReference),
		"semantic":  string(TypeSemantic),
		"episodic":  string(TypeEpisodic),
		"knowledge": string(TypeKnowledge),
		"USER":      string(TypeUser), // case insensitive
		"unknown":   "",
		"":          "",
	}
	for in, want := range cases {
		got := categoryToType(in)
		if got != want {
			t.Errorf("categoryToType(%q) = %q, want %q", in, got, want)
		}
	}
}

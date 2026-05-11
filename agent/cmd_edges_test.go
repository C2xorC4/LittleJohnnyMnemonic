package main

import (
	"testing"
	"time"
)

func TestAdaptiveEdgeStats_Disabled(t *testing.T) {
	vault := t.TempDir()
	cfg := DefaultConfig() // toggle off
	nonDefault, top := adaptiveEdgeStats(cfg, vault, 5)
	if nonDefault != 0 || len(top) != 0 {
		t.Errorf("disabled: expected (0, []), got (%d, %d entries)", nonDefault, len(top))
	}
}

func TestAdaptiveEdgeStats_EnabledNoUsage(t *testing.T) {
	vault := t.TempDir()
	cfg := adaptiveConfig()
	nonDefault, top := adaptiveEdgeStats(cfg, vault, 5)
	if nonDefault != 0 || len(top) != 0 {
		t.Errorf("enabled but no usage: expected (0, []), got (%d, %d entries)", nonDefault, len(top))
	}
}

func TestAdaptiveEdgeStats_RanksByUsageCount(t *testing.T) {
	vault := t.TempDir()
	cfg := adaptiveConfig()

	usage := map[string]EdgeUsage{
		edgeUsageKey("memory/x/a", "memory/x/b", "learned"): {
			Source: "memory/x/a", Target: "memory/x/b", Relationship: "learned",
			UsageCount: 1, LastUsed: time.Now(),
		},
		edgeUsageKey("memory/x/c", "memory/x/d", "learned"): {
			Source: "memory/x/c", Target: "memory/x/d", Relationship: "learned",
			UsageCount: 7, LastUsed: time.Now(),
		},
		edgeUsageKey("memory/x/e", "memory/x/f", "learned"): {
			Source: "memory/x/e", Target: "memory/x/f", Relationship: "learned",
			UsageCount: 3, LastUsed: time.Now(),
		},
		// out-of-scope — should not appear in top
		edgeUsageKey("memory/x/g", "memory/x/h", "related-to"): {
			Source: "memory/x/g", Target: "memory/x/h", Relationship: "related-to",
			UsageCount: 99, LastUsed: time.Now(),
		},
	}
	if err := SaveEdgeUsage(vault, usage); err != nil {
		t.Fatalf("save: %v", err)
	}

	nonDefault, top := adaptiveEdgeStats(cfg, vault, 5)
	if nonDefault != 3 {
		t.Errorf("nonDefault = %d, expected 3 (three in-scope edges with usage>0)", nonDefault)
	}
	if len(top) != 3 {
		t.Fatalf("top len = %d, expected 3 (out-of-scope edge excluded)", len(top))
	}
	if top[0].UsageCount != 7 {
		t.Errorf("top[0].UsageCount = %d, expected 7 (highest)", top[0].UsageCount)
	}
	if top[1].UsageCount != 3 {
		t.Errorf("top[1].UsageCount = %d, expected 3", top[1].UsageCount)
	}
	if top[2].UsageCount != 1 {
		t.Errorf("top[2].UsageCount = %d, expected 1", top[2].UsageCount)
	}
}

func TestAdaptiveEdgeStats_TopNCap(t *testing.T) {
	vault := t.TempDir()
	cfg := adaptiveConfig()

	usage := make(map[string]EdgeUsage)
	for i := 0; i < 10; i++ {
		src := "memory/x/" + string(rune('a'+i))
		tgt := "memory/x/z"
		usage[edgeUsageKey(src, tgt, "learned")] = EdgeUsage{
			Source: src, Target: tgt, Relationship: "learned",
			UsageCount: i + 1, LastUsed: time.Now(),
		}
	}
	if err := SaveEdgeUsage(vault, usage); err != nil {
		t.Fatalf("save: %v", err)
	}

	_, top := adaptiveEdgeStats(cfg, vault, 3)
	if len(top) != 3 {
		t.Errorf("top with topN=3 returned %d entries, expected 3", len(top))
	}
	// Highest counts first
	if top[0].UsageCount < top[1].UsageCount || top[1].UsageCount < top[2].UsageCount {
		t.Errorf("top not sorted descending: %+v", top)
	}
}

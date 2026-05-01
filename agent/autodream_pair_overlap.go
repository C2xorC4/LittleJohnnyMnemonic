package main

import (
	"strings"
)

// ComputePairTagOverlap returns the symmetric Jaccard overlap between the
// tag sets of two seed files. 1.0 = identical tag sets; 0.0 = no shared tags.
//
// Returns 0 (no error) when either file fails to parse or has no tags —
// missing tags is a real signal (early ingestion, no taxonomy yet) and we
// don't want a parse failure to look like genuine distance.
//
// Used at pair construction time so the autodream log can distinguish:
//   - "unrelated because genuinely far" (low overlap, expected unrelated verdict)
//   - "unrelated because adjacent but no integration path" (high overlap,
//     surprising unrelated verdict — much more informative)
func ComputePairTagOverlap(recentPath, stablePath string) float64 {
	recentTags := tagsForPath(recentPath)
	stableTags := tagsForPath(stablePath)
	return jaccardTags(recentTags, stableTags)
}

// tagsForPath returns the lowercased tag set for either a buffer entry or a
// memory entry, choosing the parser by trying buffer first (cheaper signal:
// if buffer parsing fails, try memory).
func tagsForPath(path string) []string {
	if path == "" {
		return nil
	}
	if entry, err := ParseBufferEntry(path); err == nil && entry != nil && len(entry.Tags) > 0 {
		return entry.Tags
	}
	if entry, err := ParseMemoryEntry(path); err == nil && entry != nil && len(entry.Tags) > 0 {
		return entry.Tags
	}
	return nil
}

func jaccardTags(a, b []string) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	setA := make(map[string]struct{}, len(a))
	for _, t := range a {
		setA[strings.ToLower(strings.TrimSpace(t))] = struct{}{}
	}
	setB := make(map[string]struct{}, len(b))
	for _, t := range b {
		setB[strings.ToLower(strings.TrimSpace(t))] = struct{}{}
	}

	intersection := 0
	for t := range setA {
		if _, ok := setB[t]; ok {
			intersection++
		}
	}
	union := len(setA) + len(setB) - intersection
	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}

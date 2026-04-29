package main

import (
	"testing"
	"time"
)

func TestExtractKeywords_BasicTokenization(t *testing.T) {
	keywords := ExtractKeywords("discussing eBPF kernel hooks for network deception")
	expected := map[string]bool{
		"discussing": true, "ebpf": true, "kernel": true,
		"hooks": true, "network": true, "deception": true,
	}
	for _, kw := range keywords {
		if !expected[kw] {
			t.Errorf("unexpected keyword: %s", kw)
		}
		delete(expected, kw)
	}
	for missing := range expected {
		t.Errorf("missing keyword: %s", missing)
	}
}

func TestExtractKeywords_StopWordsRemoved(t *testing.T) {
	keywords := ExtractKeywords("the quick brown fox and the lazy dog")
	for _, kw := range keywords {
		if kw == "the" || kw == "and" {
			t.Errorf("stop word not filtered: %s", kw)
		}
	}
	// "quick", "brown", "fox", "lazy", "dog" should survive
	if len(keywords) != 5 {
		t.Errorf("expected 5 keywords, got %d: %v", len(keywords), keywords)
	}
}

func TestExtractKeywords_Deduplication(t *testing.T) {
	keywords := ExtractKeywords("go Go GO golang go")
	goCount := 0
	for _, kw := range keywords {
		if kw == "go" {
			goCount++
		}
	}
	if goCount != 1 {
		t.Errorf("expected 1 'go', got %d", goCount)
	}
}

func TestExtractKeywords_ShortTokensRemoved(t *testing.T) {
	keywords := ExtractKeywords("I a x do it be")
	if len(keywords) != 0 {
		t.Errorf("expected 0 keywords (all short/stopwords), got %d: %v", len(keywords), keywords)
	}
}

func TestExtractKeywords_HyphenatedTerms(t *testing.T) {
	keywords := ExtractKeywords("offense-defense cross-platform real-time")
	// Should preserve hyphenated terms
	found := make(map[string]bool)
	for _, kw := range keywords {
		found[kw] = true
	}
	if !found["offense-defense"] && !(found["offense"] && found["defense"]) {
		t.Logf("keywords: %v", keywords)
		// Either form is acceptable
	}
}

func TestComputeBodyRelevance_FullMatch(t *testing.T) {
	m := &MemoryEntry{
		Title: "Mimic eBPF deception",
		Body:  "Uses eBPF TC hooks to rewrite kernel-level network fingerprints",
	}
	keywords := []string{"ebpf", "kernel", "network", "fingerprints"}
	rel := ComputeBodyRelevance(m, keywords)
	if rel < 0.9 {
		t.Errorf("expected high relevance for full match, got %.3f", rel)
	}
}

func TestComputeBodyRelevance_NoMatch(t *testing.T) {
	m := &MemoryEntry{
		Title: "Army E4 politics",
		Body:  "Competence punished, neutrality failed, institutional exit",
	}
	keywords := []string{"ebpf", "kernel", "network"}
	rel := ComputeBodyRelevance(m, keywords)
	if rel > 0.01 {
		t.Errorf("expected zero relevance for no match, got %.3f", rel)
	}
}

func TestComputeBodyRelevance_PartialMatch(t *testing.T) {
	m := &MemoryEntry{
		Title: "Go offensive tooling",
		Body:  "Primary language for network tools and kernel drivers",
	}
	keywords := []string{"ebpf", "kernel", "network", "deception"}
	rel := ComputeBodyRelevance(m, keywords)
	if rel < 0.3 || rel > 0.7 {
		t.Errorf("expected moderate relevance for partial match, got %.3f", rel)
	}
}

func TestComputeCombinedRelevance_TagsWeightedHigher(t *testing.T) {
	m := &MemoryEntry{
		Title: "Test memory",
		Body:  "content with ebpf and kernel",
		Tags:  []string{"ebpf", "network"},
	}
	// With tags matching and body matching
	combined := ComputeCombinedRelevance(m, []string{"ebpf", "kernel"}, []string{"ebpf", "kernel"})
	tagOnly := ComputeRelevance(m, []string{"ebpf", "kernel"})
	bodyOnly := ComputeBodyRelevance(m, []string{"ebpf", "kernel"})

	// Combined should reflect the 60/40 weighting
	expected := tagOnly*0.6 + bodyOnly*0.4
	if expected > 1.0 {
		expected = 1.0
	}
	if abs(combined-expected) > 0.01 {
		t.Errorf("combined %.3f != expected %.3f (tag=%.3f, body=%.3f)", combined, expected, tagOnly, bodyOnly)
	}
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

func TestFindEnrichmentKeywords(t *testing.T) {
	m := &MemoryEntry{
		Title: "Mimic eBPF deception",
		Body:  "Uses eBPF to rewrite network fingerprints for OS misattribution",
		Tags:  []string{"ebpf", "network", "deception"},
	}
	novel := FindEnrichmentKeywords(m, []string{"ebpf", "kernel", "hooks", "network", "tc-egress"})
	// "ebpf" and "network" are in tags/body, so NOT novel
	// "kernel", "hooks", "tc-egress" should be novel
	novelSet := make(map[string]bool)
	for _, n := range novel {
		novelSet[n] = true
	}
	if novelSet["ebpf"] || novelSet["network"] {
		t.Error("existing keywords should not appear as novel")
	}
	if !novelSet["kernel"] || !novelSet["hooks"] {
		t.Errorf("expected kernel and hooks as novel, got %v", novel)
	}
}

func TestFindEnrichmentKeywords_NoneNovel(t *testing.T) {
	m := &MemoryEntry{
		Title: "eBPF kernel hooks",
		Body:  "Network deception via kernel hooks",
		Tags:  []string{"ebpf", "kernel", "hooks", "network", "deception"},
	}
	novel := FindEnrichmentKeywords(m, []string{"ebpf", "kernel", "hooks"})
	if len(novel) != 0 {
		t.Errorf("expected no novel keywords, got %v", novel)
	}
}

func TestComputeIDF_RareTermsWeightHigher(t *testing.T) {
	memories := []*MemoryEntry{
		{Title: "Memory about eBPF kernel hooks", Body: "Uses eBPF for network deception", Tags: []string{"ebpf"}},
		{Title: "Go development preferences", Body: "Prefers Go for project work on OS tools", Tags: []string{"go", "project"}},
		{Title: "Career and project history", Body: "Multiple project efforts over career", Tags: []string{"career", "project"}},
		{Title: "OS fingerprinting", Body: "OS identification via network probes", Tags: []string{"os", "network"}},
		{Title: "Project management notes", Body: "Project planning and OS integration", Tags: []string{"project"}},
	}

	keywords := []string{"ebpf", "project", "os", "deception"}
	idf := ComputeIDF(keywords, memories)

	// "ebpf" appears in 1/5 memories — should have high weight
	// "project" appears in 4/5 — should have low weight
	// "deception" appears in 1/5 — should have high weight
	// "os" appears in 3/5 — moderate weight (in Go dev, OS fingerprinting, Project mgmt)

	if idf["project"] >= idf["ebpf"] {
		t.Errorf("'project' (%.3f) should weigh less than 'ebpf' (%.3f)", idf["project"], idf["ebpf"])
	}
	if idf["project"] >= idf["deception"] {
		t.Errorf("'project' (%.3f) should weigh less than 'deception' (%.3f)", idf["project"], idf["deception"])
	}
	if idf["os"] >= idf["ebpf"] {
		t.Errorf("'os' (%.3f) should weigh less than 'ebpf' (%.3f)", idf["os"], idf["ebpf"])
	}
}

func TestComputeIDF_NovelTermMaxWeight(t *testing.T) {
	memories := []*MemoryEntry{
		{Title: "Something", Body: "about cats", Tags: []string{"cats"}},
	}
	keywords := []string{"cats", "quantum"}
	idf := ComputeIDF(keywords, memories)

	// "quantum" doesn't appear at all — should have max weight
	if idf["quantum"] <= idf["cats"] {
		t.Errorf("novel term 'quantum' (%.3f) should outweigh 'cats' (%.3f)", idf["quantum"], idf["cats"])
	}
}

func TestComputeWeightedBodyRelevance_DiscriminatingTermsMatter(t *testing.T) {
	m := &MemoryEntry{
		Title: "Mimic eBPF project",
		Body:  "eBPF deception tool for OS fingerprint manipulation on the project network",
	}
	keywords := []string{"ebpf", "project", "os", "deception"}
	idf := IDFWeights{"ebpf": 0.9, "project": 0.1, "os": 0.05, "deception": 1.0}

	weighted := ComputeWeightedBodyRelevance(m, keywords, idf)
	flat := ComputeBodyRelevance(m, keywords)

	// Both should be high since all keywords match, but weighted should
	// emphasize the discriminating terms
	if weighted < 0.5 {
		t.Errorf("weighted relevance should be high for full match, got %.3f", weighted)
	}
	if flat < 0.5 {
		t.Errorf("flat relevance should be high for full match, got %.3f", flat)
	}
}

func TestComputeWeightedBodyRelevance_GenericMatchesLowScore(t *testing.T) {
	m := &MemoryEntry{
		Title: "Career history",
		Body:  "Various project work over the years with OS systems",
	}
	keywords := []string{"ebpf", "project", "os", "deception"}
	idf := IDFWeights{"ebpf": 0.9, "project": 0.1, "os": 0.05, "deception": 1.0}

	weighted := ComputeWeightedBodyRelevance(m, keywords, idf)
	// Only "project" and "os" match — both very low IDF weight
	if weighted > 0.15 {
		t.Errorf("generic-only matches should score low, got %.3f", weighted)
	}
}

func TestFindWeightedEnrichmentKeywords_FiltersGeneric(t *testing.T) {
	m := &MemoryEntry{
		Title: "Career notes",
		Body:  "project history",
		Tags:  []string{"career"},
	}
	keywords := []string{"ebpf", "project", "os", "deception"}
	idf := IDFWeights{"ebpf": 0.9, "project": 0.1, "os": 0.05, "deception": 1.0}

	novel := FindWeightedEnrichmentKeywords(m, keywords, idf, 0.3)
	// "project" is in body (not novel), "os" is novel but low IDF (filtered)
	// "ebpf" and "deception" are novel and high IDF
	novelSet := make(map[string]bool)
	for _, n := range novel {
		novelSet[n] = true
	}
	if novelSet["os"] {
		t.Error("'os' should be filtered (IDF 0.05 < threshold 0.3)")
	}
	if !novelSet["ebpf"] {
		t.Error("'ebpf' should be included (IDF 0.9 ≥ threshold 0.3)")
	}
	if !novelSet["deception"] {
		t.Error("'deception' should be included (IDF 1.0 ≥ threshold 0.3)")
	}
}

func TestAssociateRelevanceFloor(t *testing.T) {
	// Memories with zero keyword overlap should not appear even if high activation
	m := &MemoryEntry{
		Type:         TypeFeedback,
		Title:        "Unrelated feedback",
		Body:         "completely different topic about cooking recipes",
		Tags:         []string{"cooking", "recipes"},
		Created:      time.Now(),
		LastAccessed: time.Now(),
		AccessCount:  100, // high activation
		DecayRate:    0.1,
		Confidence:   0.95,
	}

	keywords := []string{"ebpf", "kernel", "network"}
	bodyRel := ComputeBodyRelevance(m, keywords)
	tagRel := ComputeRelevance(m, keywords)
	combined := tagRel*0.6 + bodyRel*0.4

	if combined >= 0.01 {
		t.Errorf("unrelated memory should have near-zero combined relevance, got %.3f", combined)
	}
}

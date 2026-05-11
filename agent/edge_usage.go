package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// EdgeUsage records the citation-driven usage counter for a directed
// edge between two memories. Persisted as one JSONL line per
// (source, target, relationship) triple in Metrics/edge_usage.jsonl.
//
// usage_count is incremented when a citation event arrives with a
// retrieval session_id whose loaded set contains both endpoints. The
// adaptive-edge-weighting pilot uses the count to derive a multiplier
// on the relationship-type base weight at graph-build time, modulating
// spreading activation for edges in AdaptiveEdgeScope.
//
// See System/AssociativeMap.md for the design rationale (citation as
// external reward signal; endogeneity avoidance; tradeoff on
// averages-collapse-context).
type EdgeUsage struct {
	Source       string    `json:"source"`
	Target       string    `json:"target"`
	Relationship string    `json:"relationship"`
	UsageCount   int       `json:"usage_count"`
	LastUsed     time.Time `json:"last_used"`
}

const edgeUsagePath = "Metrics/edge_usage.jsonl"

// edgeUsageKey produces the canonical map key for an edge usage entry.
// Directed: (source, target, relationship). The edge is direction-aware
// because the source/target asymmetry matters for spreading activation
// (boost flows from source to target).
//
// All key components are lower-cased so the resulting key matches the
// (lower-cased) normalised form used by graph.normalizeKey() — this is
// how BuildGraph's adaptive-weighting layer locates a usage entry for
// a given graph edge.
func edgeUsageKey(source, target, relationship string) string {
	return strings.ToLower(source) + "|" + strings.ToLower(target) + "|" + strings.ToLower(relationship)
}

// LoadEdgeUsage reads every edge usage record from disk into a map
// keyed by (source, target, relationship). Returns an empty map if the
// file does not exist — adaptive weighting silently no-ops when no
// reinforcement data has accumulated.
func LoadEdgeUsage(vaultRoot string) (map[string]EdgeUsage, error) {
	path := filepath.Join(vaultRoot, edgeUsagePath)
	usage := make(map[string]EdgeUsage)

	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return usage, nil
		}
		return nil, fmt.Errorf("open edge usage log: %w", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1<<16), 1<<20)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var e EdgeUsage
		if err := json.Unmarshal(line, &e); err != nil {
			// Skip malformed lines (matches resilience of sibling JSONL readers).
			continue
		}
		// Later entries for the same triple override earlier ones —
		// the file is append-only and the most recent record wins.
		usage[edgeUsageKey(e.Source, e.Target, e.Relationship)] = e
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan edge usage log: %w", err)
	}
	return usage, nil
}

// SaveEdgeUsage rewrites the entire edge usage log atomically. Used
// after a batch update so the file contains one row per triple
// rather than growing without bound under append-only semantics.
func SaveEdgeUsage(vaultRoot string, usage map[string]EdgeUsage) error {
	path := filepath.Join(vaultRoot, edgeUsagePath)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create metrics dir: %w", err)
	}

	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("create tmp edge usage log: %w", err)
	}
	w := bufio.NewWriter(f)
	for _, e := range usage {
		enc, err := json.Marshal(e)
		if err != nil {
			f.Close()
			os.Remove(tmp)
			return fmt.Errorf("marshal edge usage: %w", err)
		}
		if _, err := w.Write(append(enc, '\n')); err != nil {
			f.Close()
			os.Remove(tmp)
			return fmt.Errorf("write edge usage: %w", err)
		}
	}
	if err := w.Flush(); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("flush edge usage log: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("close tmp edge usage log: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("rename tmp edge usage log: %w", err)
	}
	return nil
}

// RecordEdgeUsageFromCitation increments edge usage counters for every
// edge between `citedMemory` and any other memory in the retrieval
// session, where the relationship type is in `scope`. Returns the
// list of edges that were reinforced (for caller-side logging / tests).
//
// Edges are looked up bidirectionally — a citation on memory Y in a
// session that also loaded memory X reinforces edge(X, Y) if it
// exists AND edge(Y, X) if it exists (they're independent records
// because spreading activation is direction-aware).
//
// If the session is not found, returns nil without error: the citation
// may have been recorded long after the session log was pruned, which
// is expected behaviour rather than a failure.
//
// If `scope` is empty, no reinforcement happens (matches the v0
// pilot policy of opting in per-relationship).
func RecordEdgeUsageFromCitation(vaultRoot, sessionID, citedMemory string, scope []string) ([]EdgeUsage, error) {
	if sessionID == "" || citedMemory == "" || len(scope) == 0 {
		return nil, nil
	}

	session, err := FindRetrievalSession(vaultRoot, sessionID)
	if err != nil {
		return nil, fmt.Errorf("find session: %w", err)
	}
	if session == nil {
		return nil, nil
	}

	// Load all memories once — needed to find the actual Link objects
	// between citedMemory and each other loaded memory.
	memories, err := LoadAllMemories(vaultRoot)
	if err != nil {
		return nil, fmt.Errorf("load memories: %w", err)
	}

	// Build a quick lookup from memory key → *MemoryEntry for the
	// memories we care about (cited + loaded set). All keys lower-cased
	// so MemoryKey()-canonical and session-recorded forms (which may
	// pre-date the MemoryKey() lowercasing) match consistently.
	relevantKeys := make(map[string]bool, len(session.Loaded)+1)
	relevantKeys[strings.ToLower(citedMemory)] = true
	for _, k := range session.Loaded {
		relevantKeys[strings.ToLower(k)] = true
	}
	byKey := make(map[string]*MemoryEntry)
	for i := range memories {
		k := MemoryKey(memories[i]) // already lower-cased
		if relevantKeys[k] {
			byKey[k] = memories[i]
		}
	}

	scopeSet := make(map[string]bool, len(scope))
	for _, r := range scope {
		scopeSet[r] = true
	}

	// Load current usage state, will rewrite atomically at end.
	usage, err := LoadEdgeUsage(vaultRoot)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	var reinforced []EdgeUsage

	citedLower := strings.ToLower(citedMemory)

	// For each loaded memory in the session (other than the cited one),
	// find edges in both directions and reinforce when relationship is in scope.
	for _, otherKey := range session.Loaded {
		otherLower := strings.ToLower(otherKey)
		if otherLower == citedLower {
			continue
		}

		// Forward direction: cited → other
		if cited, ok := byKey[citedLower]; ok {
			for _, link := range cited.Links {
				if !scopeSet[link.Relationship] {
					continue
				}
				if !linkTargetMatches(link.Target, otherLower) {
					continue
				}
				e := bumpUsage(usage, citedLower, otherLower, link.Relationship, now)
				reinforced = append(reinforced, e)
			}
		}

		// Reverse direction: other → cited
		if other, ok := byKey[otherLower]; ok {
			for _, link := range other.Links {
				if !scopeSet[link.Relationship] {
					continue
				}
				if !linkTargetMatches(link.Target, citedLower) {
					continue
				}
				e := bumpUsage(usage, otherLower, citedLower, link.Relationship, now)
				reinforced = append(reinforced, e)
			}
		}
	}

	if len(reinforced) == 0 {
		return nil, nil
	}

	if err := SaveEdgeUsage(vaultRoot, usage); err != nil {
		return reinforced, err
	}
	return reinforced, nil
}

// bumpUsage increments the usage_count for an edge, creating the entry
// if it doesn't exist. Mutates the map and returns the updated entry.
// Source/target are lower-cased on storage so the on-disk file matches
// graph.normalizeKey() form (see edgeUsageKey for the rationale).
func bumpUsage(usage map[string]EdgeUsage, source, target, relationship string, now time.Time) EdgeUsage {
	s := strings.ToLower(source)
	t := strings.ToLower(target)
	r := strings.ToLower(relationship)
	key := edgeUsageKey(s, t, r)
	e, ok := usage[key]
	if !ok {
		e = EdgeUsage{
			Source:       s,
			Target:       t,
			Relationship: r,
		}
	}
	e.UsageCount++
	e.LastUsed = now
	usage[key] = e
	return e
}

// linkTargetMatches normalises a Link.Target against a session-recorded
// memory key. Targets in frontmatter often appear as "Memory/Type/name"
// or "[[Memory/Type/name]]" — both forms should match the canonical
// "memory/type/name" key produced by MemoryKey(). Comparison is
// case-insensitive to tolerate vault-side inconsistencies (e.g., some
// older entries used `[[Memory/...]]` while session-logged keys come
// from MemoryKey() which lower-cases).
func linkTargetMatches(linkTarget, sessionKey string) bool {
	t := strings.TrimSpace(linkTarget)
	t = strings.TrimPrefix(t, "[[")
	t = strings.TrimSuffix(t, "]]")
	t = strings.TrimSuffix(t, ".md")
	sk := strings.TrimSuffix(sessionKey, ".md")
	return strings.EqualFold(t, sk)
}

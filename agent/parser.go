package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var frontmatterRe = regexp.MustCompile(`(?s)\A---\n(.+?)\n---\n(.*)`)

// splitFrontmatter separates YAML frontmatter from body content.
func splitFrontmatter(data []byte) (yaml string, body string, err error) {
	m := frontmatterRe.FindSubmatch(data)
	if m == nil {
		return "", string(data), fmt.Errorf("no frontmatter found")
	}
	return string(m[1]), string(m[2]), nil
}

// parseYAMLValue parses a single YAML value (string, number, bool, list).
// This is intentionally simple — handles the subset we use in memory files.
func parseYAMLMap(yaml string) map[string]string {
	result := make(map[string]string)
	lines := strings.Split(yaml, "\n")
	var currentKey string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		// Check if it's a key: value pair (not indented list item)
		if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") && strings.Contains(trimmed, ":") {
			idx := strings.Index(trimmed, ":")
			key := strings.TrimSpace(trimmed[:idx])
			val := strings.TrimSpace(trimmed[idx+1:])
			currentKey = key
			if val != "" {
				result[key] = val
			}
		} else if currentKey != "" && (strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "target:") || strings.HasPrefix(trimmed, "relationship:")) {
			// Append to existing key value for lists
			existing := result[currentKey]
			if existing != "" {
				result[currentKey] = existing + "\n" + trimmed
			} else {
				result[currentKey] = trimmed
			}
		}
	}
	return result
}

func parseStringList(val string) []string {
	// Handle inline [a, b, c] format
	val = strings.TrimSpace(val)
	if strings.HasPrefix(val, "[") && strings.HasSuffix(val, "]") {
		inner := val[1 : len(val)-1]
		if inner == "" {
			return nil
		}
		parts := strings.Split(inner, ",")
		var result []string
		for _, p := range parts {
			p = strings.TrimSpace(p)
			p = strings.Trim(p, "\"'")
			if p != "" {
				result = append(result, p)
			}
		}
		return result
	}

	// Handle multi-line - item format
	var result []string
	for _, line := range strings.Split(val, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "- ") {
			item := strings.TrimPrefix(line, "- ")
			item = strings.Trim(item, "\"'")
			result = append(result, item)
		}
	}
	return result
}

func parseLinks(yaml string) []Link {
	var links []Link
	lines := strings.Split(yaml, "\n")
	inLinks := false
	var current Link

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "links:") {
			inLinks = true
			continue
		}

		if inLinks {
			// End of links section if we hit a non-indented key.
			// Critical: clear current after saving, otherwise the post-loop
			// append below double-saves the final link every time the links
			// section is followed by another frontmatter field.
			if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") && strings.Contains(trimmed, ":") && !strings.HasPrefix(trimmed, "- ") && !strings.HasPrefix(trimmed, "target:") && !strings.HasPrefix(trimmed, "relationship:") && !strings.HasPrefix(trimmed, "weight:") {
				if current.Target != "" {
					links = append(links, current)
					current = Link{}
				}
				break
			}

			if strings.HasPrefix(trimmed, "- target:") {
				if current.Target != "" {
					links = append(links, current)
				}
				current = Link{}
				current.Target = extractQuotedValue(strings.TrimPrefix(trimmed, "- target:"))
			} else if strings.HasPrefix(trimmed, "target:") {
				current.Target = extractQuotedValue(strings.TrimPrefix(trimmed, "target:"))
			} else if strings.HasPrefix(trimmed, "relationship:") {
				current.Relationship = extractQuotedValue(strings.TrimPrefix(trimmed, "relationship:"))
			} else if strings.HasPrefix(trimmed, "weight:") {
				raw := strings.TrimSpace(strings.TrimPrefix(trimmed, "weight:"))
				raw = strings.Trim(raw, "\"'")
				if raw != "" {
					if w, err := strconv.ParseFloat(raw, 64); err == nil {
						current.Weight = &w
					}
				}
			}
		}
	}
	if current.Target != "" {
		links = append(links, current)
	}

	// Deduplicate by (target, relationship) pair. Duplicate links are always
	// artifacts of a prior parser bug or consolidation glitch — the semantic
	// model has no use for two edges with identical target and relationship.
	// This pass also auto-heals files that were corrupted by the pre-fix
	// parser: on next load-and-write, the duplicate is collapsed.
	if len(links) > 1 {
		seen := make(map[string]bool)
		deduped := make([]Link, 0, len(links))
		for _, l := range links {
			key := l.Target + "|" + l.Relationship
			if seen[key] {
				continue
			}
			seen[key] = true
			deduped = append(deduped, l)
		}
		links = deduped
	}

	return links
}

func extractQuotedValue(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, "\"'")
	// Extract wiki-link target if present
	if strings.HasPrefix(s, "[[") && strings.HasSuffix(s, "]]") {
		s = s[2 : len(s)-2]
	}
	return s
}

func parseTime(s string) time.Time {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, "\"'")

	formats := []string{
		time.RFC3339,
		"2006-01-02T15:04:05-07:00",
		"2006-01-02T15:04:05Z",
		"2006-01-02",
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

func parseFloat(s string) float64 {
	s = strings.TrimSpace(s)
	f, _ := strconv.ParseFloat(s, 64)
	return f
}

func parseInt(s string) int {
	s = strings.TrimSpace(s)
	i, _ := strconv.Atoi(s)
	return i
}

func parseBool(s string) bool {
	s = strings.TrimSpace(s)
	return s == "true" || s == "yes"
}

// ParseBufferEntry reads and parses a buffer .md file.
func ParseBufferEntry(path string) (*BufferEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	yamlStr, body, err := splitFrontmatter(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}

	m := parseYAMLMap(yamlStr)

	entry := &BufferEntry{
		Type:             MemoryType(strings.TrimSpace(m["type"])),
		Timestamp:        parseTime(m["timestamp"]),
		Source:           strings.TrimSpace(m["source"]),
		Surprise:         parseFloat(m["surprise"]),
		ContextIntegrity: ContextIntegrity(strings.TrimSpace(m["context_integrity"])),
		Tags:             parseStringList(m["tags"]),
		Related:          parseStringList(m["related"]),
		Pinned:           parseBool(m["pinned"]),
		HoldCount:        parseInt(m["hold_count"]),
		DaydreamKind:       strings.TrimSpace(m["daydream_kind"]),
		DaydreamMode:       strings.TrimSpace(m["daydream_mode"]),
		Priority:           strings.TrimSpace(m["priority"]),
		Relationship:       strings.TrimSpace(m["relationship"]),
		SurfacedInSessions: parseStringList(m["surfaced_in_sessions"]),
		Body:             strings.TrimSpace(body),
		FilePath:         path,
		FileName:         filepath.Base(path),
	}

	if entry.ContextIntegrity == "" {
		entry.ContextIntegrity = ContextFull
	}

	// Buffer entries must have type=buffer. Daydream-agent compliance
	// failures occasionally produce malformed frontmatter (type:semantic,
	// type:knowledge, etc.) — see the 2026-05-04 audit. Auto-correct in
	// memory and warn so the malformed file is normalized on next write
	// and the operator can investigate the producing agent run.
	if entry.Type != TypeBuffer {
		fmt.Fprintf(os.Stderr,
			"[parser] warning: buffer entry %s has type=%q, normalizing to %q\n",
			path, entry.Type, TypeBuffer)
		entry.Type = TypeBuffer
	}

	return entry, nil
}

// ParseMemoryEntry reads and parses a memory .md file.
func ParseMemoryEntry(path string) (*MemoryEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	yamlStr, body, err := splitFrontmatter(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}

	m := parseYAMLMap(yamlStr)

	entry := &MemoryEntry{
		Type:               MemoryType(strings.TrimSpace(m["type"])),
		Title:              extractQuotedValue(m["title"]),
		Created:            parseTime(m["created"]),
		LastAccessed:       parseTime(m["last_accessed"]),
		AccessCount:        parseInt(m["access_count"]),
		Confidence:         parseFloat(m["confidence"]),
		SurpriseAtEncoding: parseFloat(m["surprise_at_encoding"]),
		Tags:               parseStringList(m["tags"]),
		Links:              parseLinks(yamlStr),

		TrainingOverride: parseBool(m["training_override"]),
		OverrideContext:  extractQuotedValue(m["override_context"]),
		SourceAuthority:  strings.TrimSpace(m["source_authority"]),
		ValidatedVia:     parseStringList(m["validated_via"]),

		Fidelity:       strings.TrimSpace(m["fidelity"]),
		TargetFidelity: strings.TrimSpace(m["target_fidelity"]),
		ArchiveRef:     extractQuotedValue(m["archive_ref"]),
		Importance:     strings.TrimSpace(m["importance"]),

		Facet:            strings.TrimSpace(m["facet"]),
		ObservationCount: parseInt(m["observation_count"]),
		Profile:          parseBool(m["profile"]),
		Evidence:         parseStringList(m["evidence"]),

		ArchiveReason: strings.TrimSpace(m["archive_reason"]),
		FinalScore:    parseFloat(m["final_score"]),
		SupersededBy:  extractQuotedValue(m["superseded_by"]),

		Body:     strings.TrimSpace(body),
		FilePath: path,
		FileName: filepath.Base(path),
	}

	// Decay rate: honor an explicit value in the YAML (including 0.0 for
	// Knowledge entries) via key-presence check. Only apply a default if
	// the field is absent from the frontmatter. Go's zero-value semantics
	// cannot distinguish "0.0" from "unset" via parseFloat alone — the
	// key-presence check is the only correct signal.
	if _, hasDecay := m["decay_rate"]; hasDecay {
		entry.DecayRate = parseFloat(m["decay_rate"])
	} else {
		// Field absent — apply type-based default from config.
		// Knowledge entries default to 0.0 (no time-based decay).
		defaults := DefaultConfig().DecayRates
		if rate, ok := defaults[string(entry.Type)]; ok {
			entry.DecayRate = rate
		} else {
			entry.DecayRate = 0.5 // fallback for unknown types
		}
	}

	if entry.AccessCount == 0 {
		entry.AccessCount = 1
	}

	// Parse archived time if present
	if archStr, ok := m["archived"]; ok && archStr != "" {
		t := parseTime(archStr)
		if !t.IsZero() {
			entry.Archived = &t
		}
	}

	return entry, nil
}

// LoadAllBufferEntries reads all .md files from the Buffer/ directory,
// including the Buffer/Daydream/ subdirectory where daydream agents drop
// breadcrumbs. loadDir does not recurse, so subdirs are listed explicitly.
func LoadAllBufferEntries(vaultRoot string) ([]*BufferEntry, error) {
	bufferDir := filepath.Join(vaultRoot, "Buffer")
	subdirs := []string{"", "Daydream"}

	var all []*BufferEntry
	for _, sub := range subdirs {
		dir := filepath.Join(bufferDir, sub)
		entries, err := loadDir(dir, func(path string) (*BufferEntry, error) {
			return ParseBufferEntry(path)
		})
		if err != nil {
			continue // directory may not exist
		}
		all = append(all, entries...)
	}
	return all, nil
}

// LoadAllMemories reads all .md files from Memory/ subdirectories.
func LoadAllMemories(vaultRoot string) ([]*MemoryEntry, error) {
	memoryDir := filepath.Join(vaultRoot, "Memory")
	subdirs := []string{"User", "Feedback", "Project", "Reference", "Semantic", "Episodic", "Knowledge"}

	var all []*MemoryEntry
	for _, sub := range subdirs {
		dir := filepath.Join(memoryDir, sub)
		entries, err := loadDir(dir, func(path string) (*MemoryEntry, error) {
			return ParseMemoryEntry(path)
		})
		if err != nil {
			continue // directory may not exist or be empty
		}
		all = append(all, entries...)
	}
	return all, nil
}

// LoadArchived reads all .md files from Archive/, including files in
// per-type subdirectories (Archive/Project/, Archive/Semantic/, etc.).
// Supports both the legacy flat layout and the new mirrored structure.
func LoadArchived(vaultRoot string) ([]*MemoryEntry, error) {
	archiveDir := filepath.Join(vaultRoot, "Archive")
	var all []*MemoryEntry

	// Flat top-level files (legacy)
	entries, err := os.ReadDir(archiveDir)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		m, err := ParseMemoryEntry(filepath.Join(archiveDir, e.Name()))
		if err != nil {
			fmt.Fprintf(os.Stderr, "[!] Parse error: %v\n", err)
			continue
		}
		all = append(all, m)
	}

	// Per-type subdirectories (new layout)
	subdirs := []string{"User", "Feedback", "Project", "Reference", "Semantic", "Episodic", "Knowledge"}
	for _, sub := range subdirs {
		dir := filepath.Join(archiveDir, sub)
		subEntries, err := loadDir(dir, func(path string) (*MemoryEntry, error) {
			return ParseMemoryEntry(path)
		})
		if err != nil {
			continue // subdir may not exist, that's fine
		}
		all = append(all, subEntries...)
	}

	return all, nil
}

func loadDir[T any](dir string, parser func(string) (T, error)) ([]T, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var result []T
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		item, err := parser(filepath.Join(dir, e.Name()))
		if err != nil {
			fmt.Fprintf(os.Stderr, "[!] Parse error: %v\n", err)
			continue
		}
		result = append(result, item)
	}
	return result, nil
}

// UnarchiveOnAccess clears the archive fields on a memory entry if it was
// soft-archived. Called from retrieval-path writes (associate, retrieve,
// hook user-prompt-submit) to implement B' semantics: access to an
// archived memory during query-driven retrieval resurrects it.
//
// Maintenance-path loads (heal, decay, status, consolidate) must NOT call
// this — decay is the pass that legitimately sets the archive flag, and
// heal is expected to preserve state.
//
// Returns true if archive fields were actually cleared (caller may log).
func UnarchiveOnAccess(entry *MemoryEntry) bool {
	if entry.Archived == nil {
		return false
	}
	entry.Archived = nil
	entry.ArchiveReason = ""
	entry.FinalScore = 0
	entry.SupersededBy = ""
	return true
}

// WriteMemoryEntry serializes a memory entry back to disk.
func WriteMemoryEntry(entry *MemoryEntry) error {
	var buf bytes.Buffer
	buf.WriteString("---\n")

	buf.WriteString(fmt.Sprintf("type: %s\n", entry.Type))
	buf.WriteString(fmt.Sprintf("title: \"%s\"\n", entry.Title))
	buf.WriteString(fmt.Sprintf("created: %s\n", entry.Created.Format(time.RFC3339)))
	buf.WriteString(fmt.Sprintf("last_accessed: %s\n", entry.LastAccessed.Format(time.RFC3339)))
	buf.WriteString(fmt.Sprintf("access_count: %d\n", entry.AccessCount))
	buf.WriteString(fmt.Sprintf("decay_rate: %.2f\n", entry.DecayRate))
	buf.WriteString(fmt.Sprintf("confidence: %.2f\n", entry.Confidence))
	buf.WriteString(fmt.Sprintf("surprise_at_encoding: %.2f\n", entry.SurpriseAtEncoding))

	if len(entry.ConsolidationSource) > 0 {
		buf.WriteString("consolidation_source:\n")
		for _, s := range entry.ConsolidationSource {
			buf.WriteString(fmt.Sprintf("  - \"%s\"\n", s))
		}
	}

	writeStringList(&buf, "tags", entry.Tags)

	if len(entry.Links) > 0 {
		buf.WriteString("links:\n")
		for _, l := range entry.Links {
			buf.WriteString(fmt.Sprintf("  - target: \"[[%s]]\"\n", l.Target))
			buf.WriteString(fmt.Sprintf("    relationship: %s\n", l.Relationship))
			if l.Weight != nil {
				buf.WriteString(fmt.Sprintf("    weight: %.2f\n", *l.Weight))
			}
		}
	}

	if entry.TrainingOverride {
		buf.WriteString("training_override: true\n")
		if entry.OverrideContext != "" {
			buf.WriteString(fmt.Sprintf("override_context: \"%s\"\n", entry.OverrideContext))
		}
		if entry.SourceAuthority != "" {
			buf.WriteString(fmt.Sprintf("source_authority: %s\n", entry.SourceAuthority))
		}
		if len(entry.ValidatedVia) > 0 {
			writeStringList(&buf, "validated_via", entry.ValidatedVia)
		}
	}

	if entry.Fidelity != "" {
		buf.WriteString(fmt.Sprintf("fidelity: %s\n", entry.Fidelity))
	}
	if entry.TargetFidelity != "" {
		buf.WriteString(fmt.Sprintf("target_fidelity: %s\n", entry.TargetFidelity))
	}
	if entry.ArchiveRef != "" {
		buf.WriteString(fmt.Sprintf("archive_ref: \"%s\"\n", entry.ArchiveRef))
	}
	if entry.Importance != "" {
		buf.WriteString(fmt.Sprintf("importance: %s\n", entry.Importance))
	}
	if entry.Profile {
		buf.WriteString("profile: true\n")
	}
	if entry.Facet != "" {
		buf.WriteString(fmt.Sprintf("facet: %s\n", entry.Facet))
	}
	if entry.ObservationCount > 0 {
		buf.WriteString(fmt.Sprintf("observation_count: %d\n", entry.ObservationCount))
	}
	if len(entry.Evidence) > 0 {
		buf.WriteString("evidence:\n")
		for _, e := range entry.Evidence {
			buf.WriteString(fmt.Sprintf("  - \"%s\"\n", e))
		}
	}

	if entry.Archived != nil {
		buf.WriteString(fmt.Sprintf("archived: %s\n", entry.Archived.Format(time.RFC3339)))
		buf.WriteString(fmt.Sprintf("archive_reason: %s\n", entry.ArchiveReason))
		buf.WriteString(fmt.Sprintf("final_score: %.4f\n", entry.FinalScore))
		if entry.SupersededBy != "" {
			buf.WriteString(fmt.Sprintf("superseded_by: \"[[%s]]\"\n", entry.SupersededBy))
		}
	}

	buf.WriteString("---\n\n")
	buf.WriteString(entry.Body)
	buf.WriteString("\n")

	return os.WriteFile(entry.FilePath, buf.Bytes(), 0644)
}

// WriteBufferEntry serializes a buffer entry back to disk (for hold_count updates).
//
// Belt-and-suspenders: forces entry.Type = TypeBuffer regardless of input.
// A buffer entry persisted to disk MUST have type=buffer; the parser also
// auto-corrects on read, but the canonical write happens here. Any caller
// constructing a BufferEntry with a wrong Type (whether from a daydream
// agent compliance failure or a future code regression) gets the schema-
// correct serialization without needing to know about the rule.
func WriteBufferEntry(entry *BufferEntry) error {
	entry.Type = TypeBuffer

	var buf bytes.Buffer
	buf.WriteString("---\n")

	buf.WriteString(fmt.Sprintf("type: %s\n", entry.Type))
	buf.WriteString(fmt.Sprintf("timestamp: %s\n", entry.Timestamp.Format(time.RFC3339)))
	buf.WriteString(fmt.Sprintf("source: %s\n", entry.Source))
	buf.WriteString(fmt.Sprintf("surprise: %.1f\n", entry.Surprise))
	buf.WriteString(fmt.Sprintf("context_integrity: %s\n", entry.ContextIntegrity))
	writeStringList(&buf, "tags", entry.Tags)
	writeStringList(&buf, "related", entry.Related)

	if entry.Pinned {
		buf.WriteString("pinned: true\n")
	}
	if entry.HoldCount > 0 {
		buf.WriteString(fmt.Sprintf("hold_count: %d\n", entry.HoldCount))
	}
	if entry.HeldForCrossSession {
		buf.WriteString("held_for_cross_session: true\n")
	}
	if entry.DaydreamKind != "" {
		buf.WriteString(fmt.Sprintf("daydream_kind: %s\n", entry.DaydreamKind))
	}
	if entry.DaydreamMode != "" {
		buf.WriteString(fmt.Sprintf("daydream_mode: %s\n", entry.DaydreamMode))
	}
	if entry.Priority != "" {
		buf.WriteString(fmt.Sprintf("priority: %s\n", entry.Priority))
	}
	if entry.Relationship != "" {
		buf.WriteString(fmt.Sprintf("relationship: %s\n", entry.Relationship))
	}
	if len(entry.SurfacedInSessions) > 0 {
		writeStringList(&buf, "surfaced_in_sessions", entry.SurfacedInSessions)
	}

	buf.WriteString("---\n\n")
	buf.WriteString(entry.Body)
	buf.WriteString("\n")

	return os.WriteFile(entry.FilePath, buf.Bytes(), 0644)
}

func writeStringList(buf *bytes.Buffer, key string, items []string) {
	if len(items) == 0 {
		buf.WriteString(fmt.Sprintf("%s: []\n", key))
		return
	}
	buf.WriteString(fmt.Sprintf("%s: [%s]\n", key, strings.Join(items, ", ")))
}

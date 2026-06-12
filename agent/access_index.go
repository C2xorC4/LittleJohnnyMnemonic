package main

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// Access tracking is stored OUTSIDE memory files so that retrieval never has to
// rewrite a .md (the root cause of the 2026-06-11 field-loss P0 and the
// concurrent-edit clobbering). The store has two parts:
//
//   Metrics/access_events.jsonl — append-only event log, one {key, ts} per
//     access. Appends never conflict, so it is lossless under the multiple
//     concurrent writers LJM runs (hooks, autodream, retrieve, associate).
//     It also preserves the per-access TIMESTAMP DISTRIBUTION, which the
//     access_count scalar discards (design-gap #1: full ACT-R base-level
//     activation needs ln(Σ tₖ⁻ᵈ)).
//
//   Metrics/access_base.json — compacted snapshot {key: {count, last_accessed}}.
//     Folding the event log into the base keeps the log bounded.
//
// Effective access = base snapshot with the event log replayed on top.
const (
	accessEventsPath = "Metrics/access_events.jsonl"
	accessBasePath   = "Metrics/access_base.json"
)

type accessRecord struct {
	Count        int       `json:"count"`
	LastAccessed time.Time `json:"last_accessed"`
}

type accessEvent struct {
	Key string    `json:"key"`
	Ts  time.Time `json:"ts"`
}

func accessEventsFile(vaultRoot string) string {
	return filepath.Join(vaultRoot, filepath.FromSlash(accessEventsPath))
}
func accessBaseFile(vaultRoot string) string {
	return filepath.Join(vaultRoot, filepath.FromSlash(accessBasePath))
}

// recordAccess appends a single access event. Append-only + one Write call per
// line → atomic and lossless under concurrent writers.
func recordAccess(vaultRoot, key string, ts time.Time) error {
	return recordAccessBatch(vaultRoot, []string{key}, ts)
}

// recordAccessBatch appends one event per key at ts in a single open/close.
// Used by retrieval, which loads a whole set at once.
func recordAccessBatch(vaultRoot string, keys []string, ts time.Time) error {
	if len(keys) == 0 {
		return nil
	}
	path := accessEventsFile(vaultRoot)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	var buf []byte
	for _, k := range keys {
		if k == "" {
			continue
		}
		line, err := json.Marshal(accessEvent{Key: k, Ts: ts})
		if err != nil {
			continue
		}
		buf = append(buf, line...)
		buf = append(buf, '\n')
	}
	_, err = f.Write(buf) // single write of the whole batch
	return err
}

// loadAccessIndex returns the effective per-key access record: the base
// snapshot with every logged event replayed on top (count summed, last_accessed
// = max). Missing files are treated as empty.
func loadAccessIndex(vaultRoot string) map[string]accessRecord {
	idx := make(map[string]accessRecord)
	if data, err := os.ReadFile(accessBaseFile(vaultRoot)); err == nil {
		_ = json.Unmarshal(data, &idx)
	}
	replayAccessEvents(accessEventsFile(vaultRoot), idx)
	return idx
}

// replayAccessEvents folds the events in path into idx.
func replayAccessEvents(path string, idx map[string]accessRecord) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for sc.Scan() {
		var e accessEvent
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil || e.Key == "" {
			continue
		}
		r := idx[e.Key]
		r.Count++
		if e.Ts.After(r.LastAccessed) {
			r.LastAccessed = e.Ts
		}
		idx[e.Key] = r
	}
}

// foldAccessLog compacts the event log into the base snapshot and truncates the
// log. It rotates the log first so concurrent appends (which go to a freshly
// created log) are not lost.
func foldAccessLog(vaultRoot string) error {
	eventsPath := accessEventsFile(vaultRoot)
	rotated := eventsPath + ".folding"
	if err := os.Rename(eventsPath, rotated); err != nil {
		if os.IsNotExist(err) {
			return nil // nothing logged since last fold
		}
		return err // e.g. transient sharing violation; caller retries next cycle
	}
	idx := make(map[string]accessRecord)
	if data, err := os.ReadFile(accessBaseFile(vaultRoot)); err == nil {
		_ = json.Unmarshal(data, &idx)
	}
	replayAccessEvents(rotated, idx)
	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(accessBaseFile(vaultRoot)), 0o755); err != nil {
		return err
	}
	writeAtomic(accessBaseFile(vaultRoot), data, 0o644)
	return os.Remove(rotated)
}

// seedAccessIndex initializes the base snapshot from the current frontmatter
// access values (one-time migration). Idempotent: re-running with the merge
// active just rewrites the same values.
func seedAccessIndex(vaultRoot string, memories []*MemoryEntry) error {
	idx := make(map[string]accessRecord, len(memories))
	for _, m := range memories {
		idx[normalizeKey(m)] = accessRecord{Count: m.AccessCount, LastAccessed: m.LastAccessed}
	}
	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(accessBaseFile(vaultRoot)), 0o755); err != nil {
		return err
	}
	writeAtomic(accessBaseFile(vaultRoot), data, 0o644)
	return nil
}

// mergeAccessIndex overlays the sidecar access values onto loaded memories so
// every downstream consumer (scoring, status, graph) sees current access
// without reading it from frontmatter. No-op before migration (empty index).
func mergeAccessIndex(vaultRoot string, memories []*MemoryEntry) {
	idx := loadAccessIndex(vaultRoot)
	if len(idx) == 0 {
		return
	}
	for _, m := range memories {
		if r, ok := idx[normalizeKey(m)]; ok {
			if r.Count > 0 {
				m.AccessCount = r.Count
			}
			if !r.LastAccessed.IsZero() {
				m.LastAccessed = r.LastAccessed
			}
		}
	}
}

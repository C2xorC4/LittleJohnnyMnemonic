package main

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// rotateJSONLIfNeeded checks whether `path` has accumulated `threshold` or
// more entries and, if so, atomically moves it to
// Metrics/Archive/<basename>.{timestamp}.jsonl. The next append creates a
// fresh empty file. No-op for missing files, empty files, threshold <= 0,
// or counts below the threshold.
//
// Designed to be called BEFORE each append. Two cheap prefilters keep this
// from being expensive on the hot path:
//
//  1. Size check: if the file is smaller than threshold * conservativeBytesPerLine,
//     it cannot possibly be over the count threshold — return without
//     reading the file.
//  2. Line count: if size check is inconclusive, scan to count actual lines.
//
// At default threshold=1000 with ~120 byte heartbeat lines, the size
// prefilter avoids the line scan until the file approaches ~80KB.
func rotateJSONLIfNeeded(path string, threshold int, now time.Time) error {
	if threshold <= 0 {
		return nil
	}
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat %s: %w", path, err)
	}
	if info.Size() == 0 {
		return nil
	}

	// Conservative lower bound: 80 bytes per line. The smallest record we
	// emit (heartbeat) is ~120 bytes; using 80 means we scan-to-count a
	// little earlier than strictly necessary, which is fine.
	const conservativeBytesPerLine = 80
	if info.Size() < int64(threshold)*conservativeBytesPerLine {
		return nil
	}

	count, err := countJSONLLines(path)
	if err != nil {
		return fmt.Errorf("count %s: %w", path, err)
	}
	if count < threshold {
		return nil
	}

	archive := filepath.Join(filepath.Dir(path), "Archive",
		fmt.Sprintf("%s.%s.jsonl",
			strings.TrimSuffix(filepath.Base(path), ".jsonl"),
			now.Format("20060102-150405")))

	if err := os.MkdirAll(filepath.Dir(archive), 0o755); err != nil {
		return fmt.Errorf("mkdir archive: %w", err)
	}
	if err := os.Rename(path, archive); err != nil {
		return fmt.Errorf("rotate %s: %w", path, err)
	}
	return nil
}

// collectAutodreamLogPaths returns all autodream_log JSONL paths for a given
// metricsDir: archived files (sorted oldest-first by name) followed by the
// live log. Callers that iterate in order get chronological data without
// additional sorting. Missing files are silently omitted.
func collectAutodreamLogPaths(metricsDir string) []string {
	archived, _ := filepath.Glob(filepath.Join(metricsDir, "Archive", "autodream_log.*.jsonl"))
	sort.Strings(archived)
	live := filepath.Join(metricsDir, "autodream_log.jsonl")
	return append(archived, live)
}

// countJSONLLines returns the number of non-blank lines in a JSONL file.
// Blank lines are skipped (defensive against accidental whitespace).
func countJSONLLines(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	count := 0
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		if len(bytes.TrimSpace(scanner.Bytes())) > 0 {
			count++
		}
	}
	return count, scanner.Err()
}

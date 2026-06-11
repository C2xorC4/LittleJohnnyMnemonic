package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// TestSaveCoactivation_RoundTrip is a basic sanity check that a saved log
// reads back as valid JSON with the same pairs.
func TestSaveCoactivation_RoundTrip(t *testing.T) {
	vault := t.TempDir()
	log := &CoactivationLog{}
	RecordCoactivation(log, []string{"a", "b", "c"}, "ctx", 5)
	if err := SaveCoactivation(vault, log); err != nil {
		t.Fatalf("SaveCoactivation: %v", err)
	}
	got, err := LoadCoactivation(vault)
	if err != nil {
		t.Fatalf("LoadCoactivation: %v", err)
	}
	if len(got.Pairs) != 3 { // ab, ac, bc
		t.Fatalf("pairs=%d, want 3", len(got.Pairs))
	}
}

// buildSmallLog / buildLargeLog return freshly-allocated logs of very
// different serialized sizes. Each concurrent writer builds its OWN log so the
// test faithfully models production — where every `jm associate` invocation is
// a separate OS process with its own in-memory log — rather than introducing a
// shared-memory race that does not exist in the real system.
func buildSmallLog() *CoactivationLog {
	l := &CoactivationLog{}
	RecordCoactivation(l, []string{"a", "b"}, "small", 1)
	return l
}

func buildLargeLog() *CoactivationLog {
	l := &CoactivationLog{}
	keys := make([]string, 40)
	for i := range keys {
		keys[i] = string(rune('A'+i%26)) + string(rune('a'+i%26)) + "_long_memory_key_padding"
	}
	RecordCoactivation(l, keys, "a much longer context string to inflate the payload", 5)
	return l
}

// TestSaveCoactivation_ConcurrentWritersNeverCorrupt reproduces the 2026-06-11
// corruption: concurrent writers of different payload sizes must never leave a
// torn file with trailing garbage after the top-level JSON value. With a plain
// os.WriteFile (O_TRUNC) the shorter writer can leave the longer writer's tail
// behind; the atomic temp-file+rename write must make every observed state a
// complete, parseable log.
func TestSaveCoactivation_ConcurrentWritersNeverCorrupt(t *testing.T) {
	vault := t.TempDir()
	path := filepath.Join(vault, "Metrics", "coactivation.json")

	smallPairs := len(buildSmallLog().Pairs)
	largePairs := len(buildLargeLog().Pairs)

	// Seed the file so there is always something on disk to read back.
	if err := SaveCoactivation(vault, buildLargeLog()); err != nil {
		t.Fatalf("seed save: %v", err)
	}

	var wg sync.WaitGroup
	const rounds = 200
	// Writers hammer the same target with alternating sizes. Each goroutine
	// builds its own log — no shared mutable state between writers.
	for i := 0; i < rounds; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); _ = SaveCoactivation(vault, buildSmallLog()) }()
		go func() { defer wg.Done(); _ = SaveCoactivation(vault, buildLargeLog()) }()
	}

	// Concurrent reader: every successful read must parse cleanly with no
	// trailing bytes after the top-level value.
	stop := make(chan struct{})
	var readWG sync.WaitGroup
	readWG.Add(1)
	go func() {
		defer readWG.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			data, err := os.ReadFile(path)
			if err != nil {
				continue // mid-rename window; the file briefly may not exist
			}
			var log CoactivationLog
			dec := json.NewDecoder(bytes.NewReader(data))
			if err := dec.Decode(&log); err != nil {
				t.Errorf("decode failed (torn write): %v", err)
				return
			}
			// Reject trailing content after the top-level value — the exact
			// corruption signature from 2026-06-11.
			if dec.More() {
				t.Errorf("trailing bytes after top-level JSON value (torn write)")
				return
			}
		}
	}()

	wg.Wait()
	close(stop)
	readWG.Wait()

	// Final state must be one of the two complete logs.
	final, err := LoadCoactivation(vault)
	if err != nil {
		t.Fatalf("final LoadCoactivation: %v", err)
	}
	if n := len(final.Pairs); n != smallPairs && n != largePairs {
		t.Fatalf("final pairs=%d, want %d or %d", n, smallPairs, largePairs)
	}
}

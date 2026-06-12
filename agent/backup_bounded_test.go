package main

import (
	"archive/tar"
	"bytes"
	"io"
	"strings"
	"testing"
)

// tarOneEntry writes a single tar entry declaring headerSize bytes, with body
// supplied by src, using writeTarEntryBounded — then reads the archive back and
// returns the actual entry body. Mirrors how TarGzManifest writes a file whose
// on-disk size may differ from the header size fixed at stat time.
func tarOneEntry(t *testing.T, headerSize int64, src io.Reader) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	if err := tw.WriteHeader(&tar.Header{Name: "f", Mode: 0o644, Size: headerSize, Typeflag: tar.TypeReg}); err != nil {
		t.Fatalf("WriteHeader: %v", err)
	}
	if err := writeTarEntryBounded(tw, src, headerSize); err != nil {
		t.Fatalf("writeTarEntryBounded: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar Close (entry body size mismatch?): %v", err)
	}

	tr := tar.NewReader(&buf)
	if _, err := tr.Next(); err != nil {
		t.Fatalf("tar Next: %v", err)
	}
	body, err := io.ReadAll(tr)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return body
}

// TestWriteTarEntryBounded_FileGrew is the regression guard for the 2026-06-11
// backup failure: a live-appended file (retrieval_sessions.jsonl) grew past the
// stat-fixed header size and io.Copy tripped tar's "write too long". The bounded
// writer must cap at headerSize and still produce a valid archive.
func TestWriteTarEntryBounded_FileGrew(t *testing.T) {
	headerSize := int64(10)
	src := strings.NewReader("0123456789ABCDEFGHIJ") // 20 bytes — grew past 10
	body := tarOneEntry(t, headerSize, src)
	if len(body) != 10 || string(body) != "0123456789" {
		t.Fatalf("want exactly first 10 bytes, got %d bytes %q", len(body), body)
	}
}

func TestWriteTarEntryBounded_FileShrank(t *testing.T) {
	headerSize := int64(10)
	src := strings.NewReader("ABCDE") // 5 bytes — shrank below 10
	body := tarOneEntry(t, headerSize, src)
	if len(body) != 10 {
		t.Fatalf("want padded to 10 bytes, got %d", len(body))
	}
	if string(body[:5]) != "ABCDE" {
		t.Fatalf("prefix corrupted: %q", body[:5])
	}
	for i := 5; i < 10; i++ {
		if body[i] != 0 {
			t.Fatalf("expected zero padding at %d, got %d", i, body[i])
		}
	}
}

func TestWriteTarEntryBounded_Exact(t *testing.T) {
	body := tarOneEntry(t, 10, strings.NewReader("0123456789"))
	if string(body) != "0123456789" {
		t.Fatalf("exact-size copy corrupted: %q", body)
	}
}

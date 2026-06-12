package main

import "testing"

// TestIsSafeTarPath guards the 2026-06-11 restore regression: a benign filename
// containing ".." as a substring (the stray "agent/..jm.exe" artifact) was
// rejected as traversal, making every backup containing it un-restorable. Only a
// ".." path SEGMENT is a real traversal risk.
func TestIsSafeTarPath(t *testing.T) {
	safe := []string{
		"agent/..jm.exe",          // the artifact that broke restore — must be allowed
		"Memory/Knowledge/x.md",
		"a/..b/c",                 // ".." as a substring of a segment
		"file..ext",
		"jm.exe",
	}
	for _, p := range safe {
		if !isSafeTarPath(p) {
			t.Errorf("expected SAFE, got rejected: %q", p)
		}
	}

	unsafe := []string{
		"",
		"/etc/passwd",      // absolute (unix)
		"../escape",        // leading traversal
		"a/../../etc",      // ".." segments
		"foo/..",           // trailing ".." segment
		"..",               // bare traversal
		`C:\Windows\x`,     // absolute (windows)
	}
	for _, p := range unsafe {
		if isSafeTarPath(p) {
			t.Errorf("expected UNSAFE, got allowed: %q", p)
		}
	}
}

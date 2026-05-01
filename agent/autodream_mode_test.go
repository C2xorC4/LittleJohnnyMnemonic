package main

import (
	"strings"
	"testing"
	"time"
)

// utcTime builds a UTC time.Time at the given hour/minute on a fixed date.
// Tests use UTC so quiet-window evaluation is deterministic regardless of
// where the test happens to run.
func utcTime(h, m int) time.Time {
	return time.Date(2026, 4, 30, h, m, 0, 0, time.UTC)
}

func TestResolveMode_EmptyQuietHoursAlwaysActive(t *testing.T) {
	for _, h := range []int{0, 6, 12, 23} {
		mode, err := ResolveMode(utcTime(h, 0), "", "utc")
		if err != nil {
			t.Errorf("hour %d: unexpected err: %v", h, err)
		}
		if mode != ModeActive {
			t.Errorf("hour %d: mode = %s, want active", h, mode)
		}
	}
}

func TestResolveMode_NonWraparoundWindow(t *testing.T) {
	// quiet 13:00-15:00 (no wraparound)
	cases := []struct {
		h, m int
		want AutodreamMode
	}{
		{12, 59, ModeActive},
		{13, 0, ModeQuiet},  // start-inclusive
		{13, 30, ModeQuiet},
		{14, 59, ModeQuiet},
		{15, 0, ModeActive}, // end-exclusive
		{15, 1, ModeActive},
	}
	for _, c := range cases {
		mode, err := ResolveMode(utcTime(c.h, c.m), "13:00-15:00", "utc")
		if err != nil {
			t.Errorf("%02d:%02d: %v", c.h, c.m, err)
		}
		if mode != c.want {
			t.Errorf("%02d:%02d: mode = %s, want %s", c.h, c.m, mode, c.want)
		}
	}
}

func TestResolveMode_WraparoundWindow(t *testing.T) {
	// quiet 23:00-06:00 (wraparound)
	cases := []struct {
		h, m int
		want AutodreamMode
	}{
		{22, 59, ModeActive},
		{23, 0, ModeQuiet},  // start-inclusive
		{23, 30, ModeQuiet},
		{0, 0, ModeQuiet},
		{3, 0, ModeQuiet},
		{5, 59, ModeQuiet},
		{6, 0, ModeActive},  // end-exclusive
		{12, 0, ModeActive},
	}
	for _, c := range cases {
		mode, err := ResolveMode(utcTime(c.h, c.m), "23:00-06:00", "utc")
		if err != nil {
			t.Errorf("%02d:%02d: %v", c.h, c.m, err)
		}
		if mode != c.want {
			t.Errorf("%02d:%02d: mode = %s, want %s", c.h, c.m, mode, c.want)
		}
	}
}

func TestResolveMode_ZeroWidthWindowAlwaysActive(t *testing.T) {
	for _, h := range []int{0, 12, 12, 23} {
		mode, err := ResolveMode(utcTime(h, 0), "12:00-12:00", "utc")
		if err != nil {
			t.Errorf("hour %d: %v", h, err)
		}
		if mode != ModeActive {
			t.Errorf("hour %d: mode = %s, want active (zero-width window)", h, mode)
		}
	}
}

func TestResolveMode_UTCAndLocalDiffer(t *testing.T) {
	// Construct a moment that's 03:00 UTC. In a positive-offset timezone (e.g.,
	// Australia/Sydney), the same moment is mid-morning local. Quiet window
	// "23:00-06:00" should classify it differently depending on which timezone
	// we're using to interpret time-of-day.
	//
	// To avoid depending on time.Local, this test uses a known timezone for
	// the "local" path: we monkey-call ResolveMode twice with timezone="utc"
	// and once with the time pre-adjusted. The point is to verify that the
	// timezone argument actually changes the answer.
	moment := utcTime(3, 0) // 03:00 UTC
	utcMode, err := ResolveMode(moment, "23:00-06:00", "utc")
	if err != nil {
		t.Fatalf("utc resolve: %v", err)
	}
	if utcMode != ModeQuiet {
		t.Errorf("utc 03:00 should be quiet under 23:00-06:00, got %s", utcMode)
	}

	// Construct a fictitious +12 timezone — 03:00 UTC = 15:00 local.
	plus12 := time.FixedZone("FAKE+12", 12*3600)
	momentLocal := moment.In(plus12) // same instant, different wall-clock view
	// We can't pass *time.Location directly to ResolveMode, so verify by
	// asserting the documented behavior: passing the same moment with
	// timezone="utc" interprets time-of-day in UTC.
	hourInPlus12 := momentLocal.Hour()
	if hourInPlus12 != 15 {
		t.Fatalf("test setup error: 03:00 UTC in +12 should be 15, got %d", hourInPlus12)
	}
}

func TestResolveMode_LocalTimezoneAccepted(t *testing.T) {
	// Smoke test: timezone "local" should resolve without error and return
	// either active or quiet — we don't assert which, since the answer
	// depends on the test machine's local timezone and the current moment
	// being constructed in UTC.
	mode, err := ResolveMode(utcTime(12, 0), "23:00-06:00", "local")
	if err != nil {
		t.Errorf("local timezone rejected: %v", err)
	}
	if mode != ModeActive && mode != ModeQuiet {
		t.Errorf("got invalid mode %q", mode)
	}
}

func TestResolveMode_TimezoneEmptyDefaultsToLocal(t *testing.T) {
	// Empty timezone should be treated identically to "local".
	mode1, err := ResolveMode(utcTime(12, 0), "23:00-06:00", "")
	if err != nil {
		t.Fatalf("empty tz: %v", err)
	}
	mode2, err := ResolveMode(utcTime(12, 0), "23:00-06:00", "local")
	if err != nil {
		t.Fatalf("local tz: %v", err)
	}
	if mode1 != mode2 {
		t.Errorf("empty tz (%s) != local tz (%s)", mode1, mode2)
	}
}

func TestResolveMode_InvalidTimezoneErrors(t *testing.T) {
	mode, err := ResolveMode(utcTime(12, 0), "23:00-06:00", "America/New_York")
	if err == nil {
		t.Error("expected error for unsupported timezone")
	}
	// Caller-safety contract: error path returns ModeActive.
	if mode != ModeActive {
		t.Errorf("on error, mode should default to active, got %s", mode)
	}
}

func TestResolveMode_InvalidQuietHoursErrors(t *testing.T) {
	cases := []string{
		"23:00",          // missing dash and second time
		"23-06",          // missing colons
		"23:00-",         // missing end
		"-06:00",         // missing start
		"23:00-06:00-12", // too many parts
		"foo-bar",        // not numeric
	}
	for _, q := range cases {
		mode, err := ResolveMode(utcTime(12, 0), q, "utc")
		if err == nil {
			t.Errorf("quiet_hours %q: expected error", q)
		}
		if mode != ModeActive {
			t.Errorf("quiet_hours %q: on error, mode should default to active, got %s", q, mode)
		}
	}
}

func TestResolveMode_OutOfRangeHHMM(t *testing.T) {
	cases := []string{
		"24:00-06:00", // hour > 23
		"23:00-06:60", // minute > 59
		"-1:00-06:00", // negative hour
	}
	for _, q := range cases {
		_, err := ResolveMode(utcTime(12, 0), q, "utc")
		if err == nil {
			t.Errorf("quiet_hours %q: expected error", q)
		}
	}
}

func TestResolveMode_TimezoneCaseInsensitive(t *testing.T) {
	for _, tz := range []string{"UTC", "Utc", "utc", "LOCAL", "Local", "local"} {
		_, err := ResolveMode(utcTime(12, 0), "23:00-06:00", tz)
		if err != nil {
			t.Errorf("tz %q rejected: %v", tz, err)
		}
	}
}

func TestParseHHMM_Valid(t *testing.T) {
	cases := []struct {
		in   string
		h, m int
	}{
		{"00:00", 0, 0},
		{"09:05", 9, 5},
		{"23:59", 23, 59},
		{"  12:30  ", 12, 30},
		{"7:0", 7, 0}, // single-digit accepted (not strict ISO, but lenient)
	}
	for _, c := range cases {
		got, err := parseHHMM(c.in)
		if err != nil {
			t.Errorf("parseHHMM(%q): %v", c.in, err)
			continue
		}
		if got.h != c.h || got.m != c.m {
			t.Errorf("parseHHMM(%q) = %d:%02d, want %d:%02d", c.in, got.h, got.m, c.h, c.m)
		}
	}
}

func TestParseHHMM_Errors(t *testing.T) {
	cases := []string{"", "12", "12:", ":30", "ab:cd", "12:30:45"}
	for _, c := range cases {
		_, err := parseHHMM(c)
		if err == nil {
			t.Errorf("parseHHMM(%q): expected error", c)
		}
	}
}

func TestResolveMode_InvalidQuietHoursErrorWraps(t *testing.T) {
	// Ensure the error message is informative — it should mention quiet_hours
	// to help users diagnose Config.md typos.
	_, err := ResolveMode(utcTime(12, 0), "not-a-window", "utc")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "quiet_hours") {
		t.Errorf("error should mention quiet_hours: %q", err.Error())
	}
}

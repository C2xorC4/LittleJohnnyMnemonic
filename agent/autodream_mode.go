package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// AutodreamMode is the operating mode the auto-daydream scheduler should run
// in for a given moment in time. Active = workflow-adjacent (single seed,
// recency-biased); Quiet = dream-like (mixes random-walk exploration and
// CLS-style interleaved replay).
type AutodreamMode string

const (
	ModeActive AutodreamMode = "active"
	ModeQuiet  AutodreamMode = "quiet"
)

// ResolveMode returns the autodream mode for `now` given the configured
// quiet-hours window. Empty quietHours always resolves to active. Wraparound
// windows like "23:00-06:00" (quiet from 23:00 through 06:00 the next day)
// are supported.
//
// Boundary semantics: start-inclusive, end-exclusive. For "13:00-15:00":
// 13:00:00 is quiet, 14:59:59 is quiet, 15:00:00 is active.
//
// A zero-width window (start == end, e.g., "12:00-12:00") resolves to always
// active rather than always quiet — the convention treats it as "no window
// configured" rather than "all day quiet."
//
// Errors: malformed quietHours (anything other than HH:MM-HH:MM with valid
// numeric ranges) or unrecognized timezone (anything other than local|utc).
// On error, callers should default to active mode for safety — never silently
// promote a misconfigured system into quiet behavior.
func ResolveMode(now time.Time, quietHours, timezone string) (AutodreamMode, error) {
	if strings.TrimSpace(quietHours) == "" {
		return ModeActive, nil
	}

	loc, err := ResolveTimezone(timezone)
	if err != nil {
		return ModeActive, err
	}
	localNow := now.In(loc)

	start, end, err := parseQuietWindow(quietHours)
	if err != nil {
		return ModeActive, err
	}

	startMinutes := start.h*60 + start.m
	endMinutes := end.h*60 + end.m
	if startMinutes == endMinutes {
		return ModeActive, nil
	}

	nowMinutes := localNow.Hour()*60 + localNow.Minute()

	var inQuiet bool
	if startMinutes < endMinutes {
		inQuiet = nowMinutes >= startMinutes && nowMinutes < endMinutes
	} else {
		inQuiet = nowMinutes >= startMinutes || nowMinutes < endMinutes
	}

	if inQuiet {
		return ModeQuiet, nil
	}
	return ModeActive, nil
}

// ResolveTimezone maps the config string (case-insensitive) to a *time.Location.
// Only "local" and "utc" are accepted. Empty string is treated as "local" since
// that's the documented default in System/Config.md.
func ResolveTimezone(tz string) (*time.Location, error) {
	switch strings.ToLower(strings.TrimSpace(tz)) {
	case "", "local":
		return time.Local, nil
	case "utc":
		return time.UTC, nil
	default:
		return nil, fmt.Errorf("unsupported timezone %q (expected local|utc)", tz)
	}
}

type hhmm struct{ h, m int }

// parseQuietWindow splits "HH:MM-HH:MM" into start and end. Whitespace around
// either side is tolerated. The dash separator is mandatory; everything else
// is a parse error.
func parseQuietWindow(s string) (hhmm, hhmm, error) {
	parts := strings.SplitN(strings.TrimSpace(s), "-", 2)
	if len(parts) != 2 {
		return hhmm{}, hhmm{}, fmt.Errorf("invalid quiet_hours %q: expected HH:MM-HH:MM", s)
	}
	start, err := parseHHMM(parts[0])
	if err != nil {
		return hhmm{}, hhmm{}, fmt.Errorf("quiet_hours start: %w", err)
	}
	end, err := parseHHMM(parts[1])
	if err != nil {
		return hhmm{}, hhmm{}, fmt.Errorf("quiet_hours end: %w", err)
	}
	return start, end, nil
}

func parseHHMM(s string) (hhmm, error) {
	s = strings.TrimSpace(s)
	parts := strings.Split(s, ":")
	if len(parts) != 2 {
		return hhmm{}, fmt.Errorf("expected HH:MM, got %q", s)
	}
	h, err := strconv.Atoi(parts[0])
	if err != nil {
		return hhmm{}, fmt.Errorf("expected HH:MM, got %q", s)
	}
	m, err := strconv.Atoi(parts[1])
	if err != nil {
		return hhmm{}, fmt.Errorf("expected HH:MM, got %q", s)
	}
	if h < 0 || h > 23 {
		return hhmm{}, fmt.Errorf("hour out of range: %d", h)
	}
	if m < 0 || m > 59 {
		return hhmm{}, fmt.Errorf("minute out of range: %d", m)
	}
	return hhmm{h: h, m: m}, nil
}

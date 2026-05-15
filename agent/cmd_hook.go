package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// hookInput is the JSON schema Claude Code passes on stdin to hook commands.
// Only the fields we care about are decoded — the rest is ignored.
type hookInput struct {
	HookEventName string `json:"hook_event_name"`
	SessionID     string `json:"session_id"`
	Cwd           string `json:"cwd"`

	// UserPromptSubmit
	Prompt string `json:"prompt,omitempty"`

	// SessionStart
	Source string `json:"source,omitempty"`

	// Stop
	TranscriptPath string `json:"transcript_path,omitempty"`
	StopHookActive bool   `json:"stop_hook_active,omitempty"`

	// PreToolUse
	ToolName  string          `json:"tool_name,omitempty"`
	ToolInput json.RawMessage `json:"tool_input,omitempty"`
}

// cmdHook dispatches to the right hook handler based on the event name.
// All failure paths exit 0 (non-blocking) to ensure hook errors never
// prevent user prompts from being processed.
func cmdHook(vaultRoot string, args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "[jm hook] usage: jm hook <event>")
		fmt.Fprintln(os.Stderr, "  events: session-start, user-prompt-submit")
		os.Exit(0)
	}

	event := args[0]

	input, err := readHookInput()
	if err != nil {
		fmt.Fprintf(os.Stderr, "[jm hook] failed to read hook input: %v\n", err)
		os.Exit(0)
	}

	switch event {
	case "session-start":
		runSessionStart(vaultRoot, input)
	case "user-prompt-submit":
		runUserPromptSubmit(vaultRoot, input)
	case "stop":
		runStop(vaultRoot, input)
	case "pre-tool-use":
		runPreToolUse(vaultRoot, input)
	default:
		fmt.Fprintf(os.Stderr, "[jm hook] unknown event: %s\n", event)
		os.Exit(0)
	}
}

// readHookInput parses the JSON payload Claude Code provides on stdin.
// An empty stdin is valid and produces a zero-value hookInput.
func readHookInput() (*hookInput, error) {
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return &hookInput{}, nil
	}
	var input hookInput
	if err := json.Unmarshal(data, &input); err != nil {
		return nil, fmt.Errorf("parse hook JSON: %w", err)
	}
	return &input, nil
}

// autodreamInvocationEnvVar is the marker env var that autodream's
// claude -p invocation sets. Heartbeat writes from sessions spawned by
// autodream are suppressed because counting them as "user activity"
// creates a self-throttling endogeneity loop in the activity-skip
// check (every fire would suppress the next ~4 polls regardless of
// whether the user did anything). Diagnosed 2026-05-05 via session-id
// correlation between autodream_log fires and heartbeat entries.
const autodreamInvocationEnvVar = "LJM_AUTODREAM_INVOCATION"

// writeSessionHeartbeat appends a single JSONL line to
// Metrics/session_heartbeat.jsonl recording that real activity occurred at
// the given moment. Used by autodream's activity-based skip detection: a
// recent heartbeat means a session is doing real work and quiet-mode
// daydreams should hold off. Failures are logged but never block hook flow.
//
// Returns nil (suppressing the write) when the LJM_AUTODREAM_INVOCATION
// env var is set, indicating the session was spawned by autodream itself
// rather than by the user. Counting autodream-spawned sessions as user
// activity creates a 60-min self-throttle after every fire.
func writeSessionHeartbeat(vaultRoot, sessionID, cwd string, ts time.Time) error {
	if os.Getenv(autodreamInvocationEnvVar) == "1" {
		return nil
	}

	dir := filepath.Join(vaultRoot, "Metrics")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir Metrics: %w", err)
	}
	path := filepath.Join(dir, "session_heartbeat.jsonl")

	// Rotate before append if threshold reached. Failures here are
	// non-fatal — losing a single heartbeat is acceptable; failing to
	// write is what we actually care about.
	if cfg := LoadConfig(vaultRoot); cfg.AutoDaydreamLogRotationThreshold > 0 {
		if err := rotateJSONLIfNeeded(path, cfg.AutoDaydreamLogRotationThreshold, ts); err != nil {
			fmt.Fprintf(os.Stderr, "[jm hook] heartbeat rotation: %v\n", err)
		}
	}

	rec := struct {
		Timestamp string `json:"timestamp"`
		SessionID string `json:"session_id"`
		Cwd       string `json:"cwd"`
	}{
		Timestamp: ts.Format(time.RFC3339),
		SessionID: sessionID,
		Cwd:       cwd,
	}
	line, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("marshal heartbeat: %w", err)
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open heartbeat file: %w", err)
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("write heartbeat: %w", err)
	}
	return nil
}

// runSessionStart produces the fixed-blend session orientation:
// system state + profile traits + recent episodic + recently-accessed projects.
//
// Archived memories are excluded. Knowledge entries are NOT loaded at session
// start — they surface topically via user-prompt-submit association.
func runSessionStart(vaultRoot string, input *hookInput) {
	if err := writeSessionHeartbeat(vaultRoot, input.SessionID, input.Cwd, time.Now()); err != nil {
		fmt.Fprintf(os.Stderr, "[jm hook] session-start: heartbeat: %v\n", err)
	}

	// Repo trust check — runs before memory loading so <repo-trust-warning>
	// appears first in session context, before <memory-context>.
	if sentinel, files := checkRepoTrust(vaultRoot, input); sentinel.TrustLevel == "untrusted" {
		writeTrustWarning(os.Stdout, sentinel, files)
		bufferTrustDetection(vaultRoot, sentinel, files)
	}

	// Spawn consolidation in the background if buffer is backed up. Detached
	// and non-blocking — runs concurrently with the session-start context load.
	spawnConsolidationIfNeeded(vaultRoot, LoadConfig(vaultRoot))

	memories, err := LoadAllMemories(vaultRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[jm hook] session-start: load memories: %v\n", err)
		return
	}

	var active []*MemoryEntry
	for _, m := range memories {
		if m.Archived == nil {
			active = append(active, m)
		}
	}

	var selected []*MemoryEntry

	// 1. All profile:true entries (facet profiles — durable user model)
	for _, m := range active {
		if m.Profile {
			selected = append(selected, m)
		}
	}

	// 2. Top 3 most recently accessed episodic entries
	var episodics []*MemoryEntry
	for _, m := range active {
		if m.Type == TypeEpisodic {
			episodics = append(episodics, m)
		}
	}
	sort.Slice(episodics, func(i, j int) bool {
		return episodics[i].LastAccessed.After(episodics[j].LastAccessed)
	})
	if len(episodics) > 3 {
		episodics = episodics[:3]
	}
	selected = append(selected, episodics...)

	// 3. Top 2 most recently accessed project memories
	var projects []*MemoryEntry
	for _, m := range active {
		if m.Type == TypeProject {
			projects = append(projects, m)
		}
	}
	sort.Slice(projects, func(i, j int) bool {
		return projects[i].LastAccessed.After(projects[j].LastAccessed)
	})
	if len(projects) > 2 {
		projects = projects[:2]
	}
	selected = append(selected, projects...)

	// Deduplicate by FilePath (defensive — profiles shouldn't overlap the other categories)
	seen := make(map[string]bool)
	var unique []*MemoryEntry
	for _, m := range selected {
		if m == nil || seen[m.FilePath] {
			continue
		}
		seen[m.FilePath] = true
		unique = append(unique, m)
	}

	// Update access metadata synchronously
	now := time.Now()
	for _, m := range unique {
		m.LastAccessed = now
		m.AccessCount++
		if err := WriteMemoryEntry(m); err != nil {
			fmt.Fprintf(os.Stderr, "[jm hook] session-start: update %s: %v\n", m.FileName, err)
		}
	}

	writeSessionStartContext(os.Stdout, vaultRoot, unique)
}

// runUserPromptSubmit extracts the user's prompt text and performs
// association-based retrieval. Output framing is deliberately minimal —
// "active-context" rather than "retrieved memories" — so injected content
// integrates into baseline knowing rather than reading as a briefing.
//
// For prompts that cross the density threshold (long prompt OR many
// retrieved memories), also emits a daydream-nudge block reminding
// Claude to launch memory-daydream background agents in parallel.
// This is the backstop to the Claude-side reflex rule; the hook can't
// spawn agents directly (only Claude's main loop can), but it can put
// the reminder in front of Claude at the start of every substantive turn.
func runUserPromptSubmit(vaultRoot string, input *hookInput) {
	if err := writeSessionHeartbeat(vaultRoot, input.SessionID, input.Cwd, time.Now()); err != nil {
		fmt.Fprintf(os.Stderr, "[jm hook] user-prompt-submit: heartbeat: %v\n", err)
	}

	prompt := strings.TrimSpace(input.Prompt)
	if prompt == "" {
		return
	}

	opts := AssociateOpts{
		Limit:        8,
		Threshold:    0.2,
		UpdateAccess: true,
		Enrichment:   false,
	}

	results, _, _, err := AssociateMemories(vaultRoot, prompt, opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[jm hook] user-prompt-submit: %v\n", err)
		return
	}

	if len(results) == 0 {
		// No retrievals — but prompt itself may still be substantive.
		// Emit only the nudge in that case if density warrants.
		if isDensePrompt(prompt, 0) {
			writeDaydreamNudge(os.Stdout, len(prompt), 0)
		}
		// Hook surfacing still gets a chance — fresh daydreams may have
		// no LTM overlap but still match the prompt by tag/body.
		surfaceFreshDaydreamsToHook(vaultRoot, prompt, input.SessionID, time.Now())
		return
	}

	writeRecallMetrics(vaultRoot, results, input.SessionID, len(prompt))
	writePromptAssociationContext(os.Stdout, results)

	surfaceFreshDaydreamsToHook(vaultRoot, prompt, input.SessionID, time.Now())

	if isDensePrompt(prompt, len(results)) {
		writeDaydreamNudge(os.Stdout, len(prompt), len(results))
	}
}

// surfaceFreshDaydreamsToHook is the hook-level wrapper around
// SurfaceFreshDaydreams. Loads config, surfaces matching entries, marks
// them as surfaced for THIS session. Failures are logged but never block
// the hook.
func surfaceFreshDaydreamsToHook(vaultRoot, prompt, sessionID string, now time.Time) {
	cfg := LoadConfig(vaultRoot)
	if !cfg.AutoDaydreamSurfaceToSession {
		return
	}
	surfaced := SurfaceFreshDaydreams(vaultRoot, prompt, sessionID, cfg, now)
	if len(surfaced) == 0 {
		return
	}
	writeFreshDaydreamFindings(os.Stdout, surfaced)
	for _, s := range surfaced {
		if err := MarkDaydreamSurfaced(s.Entry, sessionID); err != nil {
			fmt.Fprintf(os.Stderr, "[jm hook] mark surfaced: %v\n", err)
		}
	}
}

// spawnConsolidationIfNeeded fires a detached `jm consolidate --trigger hook`
// when the buffer is at or above threshold and all safety gates pass.
// Designed to be called from session-start and stop hooks. Never blocks hook
// flow — all errors are logged to stderr and ignored.
//
// Gate order:
//  1. Buffer below threshold → silent return.
//  2. User kill switch (AutoConsolidationEnabled=false) → return; emit reminder
//     if a system suspension record also exists (failure condition unresolved).
//  3. System suspension (auto_consolidation_suspended.json) → emit reminder + return.
//  4. Both-sentinels-missing guard → write suspension record, emit warning, return.
//  5. Cooldown window → silent return.
//  6. Fire.
func spawnConsolidationIfNeeded(vaultRoot string, cfg Config) {
	entries, err := LoadAllBufferEntries(vaultRoot)
	if err != nil || len(entries) < cfg.BufferThreshold {
		return
	}
	n := len(entries)

	// Gate 2: user kill switch.
	if !cfg.AutoConsolidationEnabled {
		if suspended, reason, since := readAutoConsolidationSuspended(vaultRoot); suspended {
			fmt.Fprintf(os.Stderr,
				"[jm hook] auto-consolidation disabled — %d buffer entries waiting. "+
					"Suspension record present (reason: %s, since: %s). "+
					"Resolve failure conditions, then delete Metrics/auto_consolidation_suspended.json "+
					"and re-enable auto_consolidation_enabled in System/Config.md.\n",
				n, reason, since.Format(time.RFC3339))
		}
		return
	}

	// Gate 3: system suspension from a prior failure detection.
	if suspended, reason, since := readAutoConsolidationSuspended(vaultRoot); suspended {
		fmt.Fprintf(os.Stderr,
			"[jm hook] auto-consolidation suspended (reason: %s, since: %s) — "+
				"%d buffer entries waiting. "+
				"Resolve failure conditions, then delete Metrics/auto_consolidation_suspended.json to re-enable.\n",
			reason, since.Format(time.RFC3339), n)
		return
	}

	// Gate 4: both-sentinels-missing guard.
	lastTrigger := readLastAutoConsolidationTrigger(vaultRoot)
	lastBackup := readLastBackup(vaultRoot)
	if lastTrigger.IsZero() && lastBackup.IsZero() {
		fmt.Fprintf(os.Stderr,
			"[jm hook] WARN: auto_consolidation_trigger.json and last_backup.json are both missing or corrupt. "+
				"Possible interrupted sync — auto-consolidation suspended to prevent consolidation against unknown vault state. "+
				"%d buffer entries are waiting. "+
				"Verify vault integrity, then delete Metrics/auto_consolidation_suspended.json to re-enable.\n", n)
		writeAutoConsolidationSuspended(vaultRoot, "both-sentinels-missing", time.Now())
		return
	}

	// Gate 5: cooldown.
	cooldown := time.Duration(cfg.AutoConsolidationCooldownMinutes) * time.Minute
	if cooldown > 0 && !lastTrigger.IsZero() && time.Since(lastTrigger) < cooldown {
		return
	}

	recordAutoConsolidationTrigger(vaultRoot, time.Now())

	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "[jm hook] consolidation spawn: locate exe: %v\n", err)
		return
	}

	cmd := exec.Command(exe, "consolidate", "--trigger", "hook")
	devnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err == nil {
		cmd.Stdout = devnull
		cmd.Stderr = devnull
	}
	detachSysProcAttr(cmd)

	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "[jm hook] consolidation spawn: start: %v\n", err)
		return
	}
	_ = cmd.Process.Release()
}

func recordAutoConsolidationTrigger(vaultRoot string, t time.Time) {
	path := filepath.Join(vaultRoot, "Metrics", "auto_consolidation_trigger.json")
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	payload := map[string]any{"triggered_utc": t.UTC().Format(time.RFC3339)}
	data, _ := json.MarshalIndent(payload, "", "  ")
	writeAtomic(path, data, 0o644)
}

// autoConsolidationSuspendedPath returns the path of the system suspension sentinel.
func autoConsolidationSuspendedPath(vaultRoot string) string {
	return filepath.Join(vaultRoot, "Metrics", "auto_consolidation_suspended.json")
}

// writeAutoConsolidationSuspended records a system-detected failure that has
// caused auto-consolidation to be suspended. The file persists until the user
// manually deletes it after resolving the failure condition.
func writeAutoConsolidationSuspended(vaultRoot, reason string, t time.Time) {
	path := autoConsolidationSuspendedPath(vaultRoot)
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	payload := map[string]any{
		"reason":       reason,
		"suspended_at": t.UTC().Format(time.RFC3339),
	}
	data, _ := json.MarshalIndent(payload, "", "  ")
	writeAtomic(path, data, 0o644)
}

// readAutoConsolidationSuspended reports whether a system suspension record
// exists and returns the reason and timestamp if so.
func readAutoConsolidationSuspended(vaultRoot string) (suspended bool, reason string, since time.Time) {
	data, err := os.ReadFile(autoConsolidationSuspendedPath(vaultRoot))
	if err != nil {
		return false, "", time.Time{}
	}
	var payload struct {
		Reason      string `json:"reason"`
		SuspendedAt string `json:"suspended_at"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return true, "unknown (corrupt suspension record)", time.Time{}
	}
	t, _ := time.Parse(time.RFC3339, payload.SuspendedAt)
	return true, payload.Reason, t
}

// writeAtomic writes data to path via a temp-file + rename so that a crash
// mid-write cannot leave path in a corrupt partial state.
func writeAtomic(path string, data []byte, perm os.FileMode) {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return
	}
	_ = os.Rename(tmp, path)
}

func readLastAutoConsolidationTrigger(vaultRoot string) time.Time {
	path := filepath.Join(vaultRoot, "Metrics", "auto_consolidation_trigger.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return time.Time{}
	}
	var payload struct {
		TriggeredUTC string `json:"triggered_utc"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, payload.TriggeredUTC)
	if err != nil {
		return time.Time{}
	}
	return t
}

// isDensePrompt decides whether a prompt is substantive enough to
// warrant a daydream nudge. Heuristic: long prompt OR many retrieved
// memories. Both conditions independently catch different flavors of
// dense task — a long research prompt, or a short prompt that happens
// to touch many topics already in the graph.
func isDensePrompt(prompt string, retrievedCount int) bool {
	const (
		denseCharThreshold    = 200
		denseRetrievalCutoff  = 5
	)
	if len(prompt) >= denseCharThreshold {
		return true
	}
	if retrievedCount >= denseRetrievalCutoff {
		return true
	}
	return false
}

// writeDaydreamNudge emits a separate tagged block after the active-context
// reminding Claude to launch memory-daydream background agents. Kept short
// and specific so it doesn't compete for attention with the retrieved memories.
func writeDaydreamNudge(w io.Writer, promptLen, retrievedCount int) {
	fmt.Fprintln(w)
	fmt.Fprintln(w, "<daydream-nudge>")
	fmt.Fprintf(w, "Substantive prompt detected (%d chars, %d memories retrieved). ",
		promptLen, retrievedCount)
	fmt.Fprintln(w, "Consider launching memory-daydream background agents in parallel:")
	fmt.Fprintln(w, "  - at least 1 seeded from the current topic")
	fmt.Fprintln(w, "  - at least 1 random walk from an unrelated corner of the graph")
	fmt.Fprintln(w, "Daydreams can't be fired from this hook — only Claude's main loop can spawn agents.")
	fmt.Fprintln(w, "</daydream-nudge>")
}

// writeSessionStartContext emits the fixed-blend session orientation.
// Uses explicit headers since session-start is a one-time orientation
// event — framing helps Claude grasp what's being loaded.
func writeSessionStartContext(w io.Writer, vaultRoot string, memories []*MemoryEntry) {
	fmt.Fprintln(w, "<memory-context source=\"session-start\">")
	fmt.Fprintln(w)
	fmt.Fprintln(w, systemStateSummary(vaultRoot))
	fmt.Fprintln(w)

	byType := make(map[MemoryType][]*MemoryEntry)
	for _, m := range memories {
		byType[m.Type] = append(byType[m.Type], m)
	}

	if users := byType[TypeUser]; len(users) > 0 {
		fmt.Fprintln(w, "## Profile")
		fmt.Fprintln(w)
		for _, m := range users {
			fmt.Fprintf(w, "### %s\n", m.Title)
			if m.Facet != "" {
				fmt.Fprintf(w, "*facet: %s*\n\n", m.Facet)
			}
			fmt.Fprintln(w, condenseBody(m.Body, 500))
			fmt.Fprintln(w)
		}
	}

	if eps := byType[TypeEpisodic]; len(eps) > 0 {
		fmt.Fprintln(w, "## Recent sessions")
		fmt.Fprintln(w)
		for _, m := range eps {
			fmt.Fprintf(w, "### %s\n", m.Title)
			fmt.Fprintln(w, condenseBody(m.Body, 500))
			fmt.Fprintln(w)
		}
	}

	if projs := byType[TypeProject]; len(projs) > 0 {
		fmt.Fprintln(w, "## Active projects")
		fmt.Fprintln(w)
		for _, m := range projs {
			fmt.Fprintf(w, "### %s\n", m.Title)
			fmt.Fprintln(w, condenseBody(m.Body, 500))
			fmt.Fprintln(w)
		}
	}

	fmt.Fprintln(w, "</memory-context>")
}

// writePromptAssociationContext uses minimal framing so injected content
// slots into baseline knowing rather than reading as a retrieval briefing.
// No score displays, no explanations — just titles and condensed bodies.
func writePromptAssociationContext(w io.Writer, results []AssociatedMemory) {
	fmt.Fprintln(w, "<active-context>")
	fmt.Fprintln(w)
	for _, r := range results {
		m := r.Memory
		fmt.Fprintf(w, "## %s\n", m.Title)
		fmt.Fprintln(w, condenseBody(m.Body, 300))
		fmt.Fprintln(w)
	}
	fmt.Fprintln(w, "</active-context>")
}

// systemStateSummary produces a compact block describing LJM's current state.
// Shown only at session start — not repeated on every prompt.
func systemStateSummary(vaultRoot string) string {
	var lines []string
	lines = append(lines, "## LittleJohnnyMnemonic state")
	lines = append(lines, fmt.Sprintf("Vault: %s", vaultRoot))

	bufferEntries, _ := LoadAllBufferEntries(vaultRoot)
	lines = append(lines, fmt.Sprintf("Buffer: %d entries pending", len(bufferEntries)))

	memories, _ := LoadAllMemories(vaultRoot)
	typeCounts := make(map[MemoryType]int)
	active := 0
	for _, m := range memories {
		if m.Archived != nil {
			continue
		}
		typeCounts[m.Type]++
		active++
	}
	var typeParts []string
	typeOrder := []MemoryType{TypeUser, TypeFeedback, TypeProject, TypeReference, TypeSemantic, TypeEpisodic, TypeKnowledge}
	for _, t := range typeOrder {
		if c, ok := typeCounts[t]; ok {
			typeParts = append(typeParts, fmt.Sprintf("%s: %d", t, c))
		}
	}
	lines = append(lines, fmt.Sprintf("LTM: %d entries (%s)", active, strings.Join(typeParts, ", ")))

	logPath := filepath.Join(vaultRoot, "Metrics", "consolidation_log.md")
	if info, err := os.Stat(logPath); err == nil {
		since := time.Since(info.ModTime())
		switch {
		case since < 12*time.Hour:
			lines = append(lines, fmt.Sprintf("Last consolidation: %s (today)", info.ModTime().Format("15:04")))
		case since < 36*time.Hour:
			lines = append(lines, "Last consolidation: yesterday")
		default:
			days := int(since.Hours() / 24)
			lines = append(lines, fmt.Sprintf("Last consolidation: %d days ago", days))
		}
	}

	archived, _ := LoadArchived(vaultRoot)
	if len(archived) > 0 {
		lines = append(lines, fmt.Sprintf("Archived: %d", len(archived)))
	}

	return strings.Join(lines, "\n")
}

// condenseBody truncates a memory body at a clean word boundary with an
// ellipsis marker, preferring paragraph > sentence > word boundaries.
func condenseBody(body string, max int) string {
	body = strings.TrimSpace(body)
	if len(body) <= max {
		return body
	}
	cut := max
	if idx := strings.LastIndex(body[:max], "\n\n"); idx > max/2 {
		cut = idx
	} else if idx := strings.LastIndex(body[:max], ". "); idx > max/2 {
		cut = idx + 1
	} else if idx := strings.LastIndex(body[:max], " "); idx > max/2 {
		cut = idx
	}
	return strings.TrimSpace(body[:cut]) + " …"
}

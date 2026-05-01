package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
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

// writeSessionHeartbeat appends a single JSONL line to
// Metrics/session_heartbeat.jsonl recording that real activity occurred at
// the given moment. Used by autodream's activity-based skip detection: a
// recent heartbeat means a session is doing real work and quiet-mode
// daydreams should hold off. Failures are logged but never block hook flow.
func writeSessionHeartbeat(vaultRoot, sessionID, cwd string, ts time.Time) error {
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

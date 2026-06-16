package main

import (
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"time"
)

// maxCitationsPerStop caps automatic citation harvest per assistant turn
// to limit transcript-noise inflation.
const maxCitationsPerStop = 5

// memoryPathRE matches wiki-linked or bare Memory/ paths in assistant output.
var memoryPathRE = regexp.MustCompile(`(?i)(?:\[\[(Memory/[A-Za-z0-9_./-]+)\]\]|(Memory/[A-Za-z0-9_./-]+))`)

// normalizeCitedMemoryKey canonicalizes a cited path to the lowercase
// memory/type/name form used in retrieval_sessions.jsonl and edge_usage.
func normalizeCitedMemoryKey(raw string) string {
	s := strings.TrimSpace(raw)
	s = strings.TrimPrefix(s, "[[")
	s = strings.TrimSuffix(s, "]]")
	s = strings.TrimSuffix(s, ".md")
	s = strings.ReplaceAll(s, "\\", "/")
	return strings.ToLower(s)
}

// extractCitedMemoryKeys returns deduplicated memory keys referenced in text.
func extractCitedMemoryKeys(text string) []string {
	if text == "" {
		return nil
	}
	seen := make(map[string]bool)
	var keys []string
	for _, m := range memoryPathRE.FindAllStringSubmatch(text, -1) {
		raw := m[1]
		if raw == "" {
			raw = m[2]
		}
		key := normalizeCitedMemoryKey(raw)
		if key == "" || !strings.HasPrefix(key, "memory/") {
			continue
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		keys = append(keys, key)
	}
	return keys
}

// writeRetrievalSessionID emits a machine-readable retrieval session marker
// for adaptive-edge citation reinforcement (jm associate --cite --session).
func writeRetrievalSessionID(w io.Writer, sessionID string) {
	if sessionID == "" {
		return
	}
	fmt.Fprintln(w)
	fmt.Fprintf(w, "<retrieval-session id=\"%s\"/>\n", sessionID)
}

// harvestCitationsFromPreviousTurn runs at UserPromptSubmit before a new
// retrieval session is appended. The prior turn's assistant text is fully
// persisted in Grok chat_history by then, avoiding Stop-hook timing races.
func harvestCitationsFromPreviousTurn(vaultRoot string, input *hookInput) {
	if input == nil || input.SessionID == "" {
		return
	}
	transcriptPath := resolveTranscriptPath(input)
	if transcriptPath == "" {
		return
	}
	harvestCitationsForConversation(vaultRoot, input.SessionID, transcriptPath, "user-prompt-submit harvest")
}

// harvestCitationsFromStop parses the last assistant turn for Memory/
// references that were in the immediately preceding hook retrieval session,
// records citations, and reinforces learned edges when the pilot is on.
func harvestCitationsFromStop(vaultRoot string, input *hookInput) {
	if input == nil || input.SessionID == "" {
		return
	}
	transcriptPath := resolveTranscriptPath(input)
	if transcriptPath == "" {
		return
	}
	harvestCitationsForConversation(vaultRoot, input.SessionID, transcriptPath, "stop-hook harvest")
}

func harvestCitationsForConversation(vaultRoot, conversationSessionID, transcriptPath, ctxPrefix string) {
	cfg := LoadConfig(vaultRoot)
	if !cfg.RetrievalSessionLogEnabled {
		return
	}

	rs, err := FindLatestRetrievalSessionForConversation(vaultRoot, conversationSessionID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[jm hook] citation harvest: find retrieval session: %v\n", err)
		return
	}
	if rs == nil {
		return
	}
	if alreadyHarvestedRetrievalSession(vaultRoot, rs.SessionID) {
		return
	}

	turn, err := lastAssistantTurn(transcriptPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[jm hook] citation harvest: read transcript: %v\n", err)
		return
	}
	if turn == "" {
		return
	}

	loaded := make(map[string]bool, len(rs.Loaded))
	for _, k := range rs.Loaded {
		loaded[strings.ToLower(k)] = true
	}

	var toCite []string
	for _, key := range extractCitedMemoryKeys(turn) {
		if !loaded[key] {
			continue
		}
		toCite = append(toCite, key)
		if len(toCite) >= maxCitationsPerStop {
			break
		}
	}
	if len(toCite) == 0 {
		return
	}

	cLog, err := LoadCitations(vaultRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[jm hook] citation harvest: load citations: %v\n", err)
		return
	}

	ctx := fmt.Sprintf("%s (%s)", ctxPrefix, time.Now().UTC().Format(time.RFC3339))
	for _, key := range toCite {
		RecordCitationWithSession(cLog, key, ctx, true, rs.SessionID)
	}

	if err := SaveCitations(vaultRoot, cLog); err != nil {
		fmt.Fprintf(os.Stderr, "[jm hook] citation harvest: save citations: %v\n", err)
		return
	}
	if err := markRetrievalSessionHarvested(vaultRoot, rs.SessionID); err != nil {
		fmt.Fprintf(os.Stderr, "[jm hook] citation harvest: mark harvested: %v\n", err)
	}

	if cfg.AdaptiveEdgeWeightingEnabled {
		var totalReinforced int
		for _, key := range toCite {
			reinforced, err := RecordEdgeUsageFromCitation(vaultRoot, rs.SessionID, key, cfg.AdaptiveEdgeScope)
			if err != nil {
				fmt.Fprintf(os.Stderr, "[jm hook] citation harvest: edge reinforcement for %s: %v\n", key, err)
				continue
			}
			totalReinforced += len(reinforced)
		}
		if totalReinforced > 0 {
			fmt.Fprintf(os.Stderr, "[jm hook] citation harvest: harvested %d citation(s), reinforced %d edge(s)\n",
				len(toCite), totalReinforced)
		}
	}
}
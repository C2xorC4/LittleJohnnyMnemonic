package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Redundancy judge: for buffer entries whose tag overlap with existing
// memories crosses a threshold, call an LLM to decide whether the entry's
// claim is genuinely novel, redundant, or partially covered. This replaces
// tag-overlap-as-redundancy-proxy with content-based judgment that remains
// valid as the vault scales past the point where tag overlap is dominated
// by taxonomy coincidence.
//
// Originally scoped to daydream-sourced entries — hence the "daydream"
// naming retained on config fields and verdict metadata — but now fires on
// any source, since conversation-sourced cross-cutting insights exhibit the
// same tag-overlap-penalty failure mode.
//
// Synchronous — consolidation is not latency-sensitive and runs in-process.
// Falls back to a dampening factor on API failure so consolidation never
// blocks on an unavailable API.

const daydreamJudgeSystemPrompt = `You evaluate whether a buffer entry for a memory system adds novel information beyond what existing related memories already capture.

You will be given:
- The buffer entry (an excerpt from a short note written during a conversation or by an autonomous exploration agent)
- Up to 3 related existing memories (title + body excerpt each), selected by tag overlap with the entry

Your job: decide whether the entry's core claim is already stated in the related memories, or whether it adds something new.

Respond with VALID JSON only, no prose around it:
{"verdict": "novel" | "redundant" | "partial", "reason": "<one short sentence>"}

- novel: the entry's claim is not stated in any of the related memories; it is a new observation, connection, or gap worth preserving
- redundant: the entry's claim is fully captured in one or more of the related memories; creating a new memory from it would duplicate existing content
- partial: the entry's claim is partially captured; the better action is to merge into an existing memory rather than create a new one

Prefer "novel" for findings that articulate a cross-entry connection, identify an architectural gap, or frame existing observations at a new level of abstraction — those are novel even if the constituent facts appear elsewhere.`

// judgeDaydreamRedundancy classifies a buffer entry's relationship to its
// top candidate targets via LLM judgment. Uses the tiered transport
// (API → CLI → error) from judge_api.go.
// Returns verdict (novel|redundant|partial), reason, and any error.
// On error, the caller falls back to the dampening heuristic.
// Name retained from when the judge was daydream-only; now fires on any source.
func judgeDaydreamRedundancy(entry *BufferEntry, candidates []*MemoryEntry, cliFallback bool, cliMaxConcurrent int) (verdict, reason string, err error) {
	userContent := buildDaydreamJudgeMessage(entry, candidates)
	rawText, _, err := callHaikuJudge(daydreamJudgeSystemPrompt, userContent, 250, cliFallback, cliMaxConcurrent)
	if err != nil {
		return "", "", err
	}
	verdict, reason = parseDaydreamVerdict(rawText)
	if verdict == "" {
		return "", "", fmt.Errorf("unparseable verdict: %s", truncateJudge(rawText, 200))
	}
	return verdict, reason, nil
}

func buildDaydreamJudgeMessage(entry *BufferEntry, candidates []*MemoryEntry) string {
	var b strings.Builder
	sourceLabel := entry.Source
	if sourceLabel == "" {
		sourceLabel = "unknown"
	}
	fmt.Fprintf(&b, "Buffer entry (source=%s, file=%s):\n---\n", sourceLabel, entry.FileName)
	b.WriteString(excerptText(entry.Body, 1500))
	b.WriteString("\n---\n\n")

	if len(candidates) == 0 {
		b.WriteString("No candidate related memories found by tag overlap.\n")
	} else {
		b.WriteString("Related existing memories:\n\n")
		for i, m := range candidates {
			fmt.Fprintf(&b, "%d. [%s] %s\n", i+1, m.Type, m.Title)
			body := excerptText(m.Body, 800)
			for _, line := range strings.Split(body, "\n") {
				b.WriteString("   ")
				b.WriteString(line)
				b.WriteString("\n")
			}
			b.WriteString("\n")
		}
	}

	b.WriteString("Does the buffer entry add novel information, or is it already captured?")
	return b.String()
}

// parseDaydreamVerdict extracts verdict+reason JSON from the model's reply.
// Accepts the three valid verdicts; returns empty on unparseable.
func parseDaydreamVerdict(text string) (verdict, reason string) {
	candidates := jsonObjectRegex.FindAllString(text, -1)
	for _, c := range candidates {
		var v judgeVerdict
		if err := json.Unmarshal([]byte(c), &v); err != nil {
			continue
		}
		switch v.Verdict {
		case "novel", "redundant", "partial":
			return v.Verdict, v.Reason
		}
	}
	return "", ""
}

// findTopRelatedMemories returns up to N memories with highest tag overlap
// to the buffer entry. Used to pick candidates for the daydream judge.
// Ignores archived memories.
func findTopRelatedMemories(entry *BufferEntry, memories []*MemoryEntry, n int) []*MemoryEntry {
	type scored struct {
		memory  *MemoryEntry
		overlap float64
	}
	var ranked []scored
	for _, m := range memories {
		if m.Archived != nil {
			continue
		}
		overlap := tagOverlap(entry.Tags, m.Tags)
		if overlap > 0 {
			ranked = append(ranked, scored{m, overlap})
		}
	}
	// Simple insertion sort since n is small (~3)
	for i := 1; i < len(ranked); i++ {
		for j := i; j > 0 && ranked[j].overlap > ranked[j-1].overlap; j-- {
			ranked[j], ranked[j-1] = ranked[j-1], ranked[j]
		}
	}
	if len(ranked) > n {
		ranked = ranked[:n]
	}
	out := make([]*MemoryEntry, 0, len(ranked))
	for _, r := range ranked {
		out = append(out, r.memory)
	}
	return out
}

// truncateJudge trims a string to max length with a trailing ellipsis.
// Separate name from cmd_retrieve's truncate to avoid any linker surprise.
func truncateJudge(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

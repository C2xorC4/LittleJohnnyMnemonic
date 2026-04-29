package main

import (
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func cmdConsolidate(vaultRoot string, args []string) {
	fs := flag.NewFlagSet("consolidate", flag.ExitOnError)
	trigger := fs.String("trigger", "manual", "Trigger type: compact, scheduled, manual")
	depth := fs.String("depth", "", "Override consolidation depth: quick, standard, deep")
	dryRun := fs.Bool("dry-run", false, "Show what would happen without making changes")
	fs.Parse(args)

	cfg := LoadConfig(vaultRoot)
	ApplyConfigCompressionThresholds(cfg)
	now := time.Now()

	consolidationDepth := cfg.ConsolidationDepth
	if *depth != "" {
		consolidationDepth = *depth
	}

	fmt.Printf("=== Consolidation [%s] depth=%s trigger=%s ===\n\n", now.Format("2006-01-02 15:04"), consolidationDepth, *trigger)

	// Determine last consolidation time for rate-separation gate.
	lastConsolidation := lastConsolidationTime(vaultRoot)
	if cfg.RateSeparationEnabled {
		if lastConsolidation.IsZero() {
			fmt.Println("  [rate-sep] No prior consolidation found — treating all entries as same-session.")
		} else {
			fmt.Printf("  [rate-sep] Last consolidation: %s\n", lastConsolidation.Format("2006-01-02 15:04 UTC"))
		}
	}

	// Load buffer entries
	bufferEntries, err := LoadAllBufferEntries(vaultRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[!] Failed to load buffer: %v\n", err)
	}

	// Load existing memories for redundancy checking
	memories, _ := LoadAllMemories(vaultRoot)

	report := ConsolidationReport{
		Timestamp: now,
		Trigger:   *trigger,
		Depth:     consolidationDepth,
	}

	// ─── Phase 1: Buffer Review ───
	fmt.Printf("Phase 1: Buffer Review (%d entries)\n", len(bufferEntries))
	fmt.Println(strings.Repeat("─", 60))

	sessionID := consolidationSessionID(now)

	for _, entry := range bufferEntries {
		assessment := assessBufferEntry(entry, memories, cfg, now, lastConsolidation)
		report.BufferAssessments = append(report.BufferAssessments, assessment)

		actionSymbol := map[ConsolidationAction]string{
			ActionPromote:     "↑ PROMOTE",
			ActionHold:        "↔ HOLD",
			ActionHoldRateSep: "⟳ RATE-SEP",
			ActionDiscard:     "✕ DISCARD",
		}

		judgeNote := ""
		if assessment.DaydreamVerdict != "" {
			judgeNote = fmt.Sprintf(" [judge=%s]", assessment.DaydreamVerdict)
		}
		fmt.Printf("  %s  %-40s  retention=%.3f  ctx=%s%s  %s\n",
			actionSymbol[assessment.Action],
			truncate(entry.FileName, 40),
			assessment.RetentionScore,
			assessment.ContextIntegrity,
			judgeNote,
			assessment.Reason)

		switch assessment.Action {
		case ActionPromote:
			report.Promoted++
			if !*dryRun {
				prompt := generatePromotionPrompt(entry, memories, sessionID)
				report.LLMPrompts = append(report.LLMPrompts, prompt)
			}
		case ActionHold:
			report.Held++
			if !*dryRun {
				entry.HoldCount++
				if err := WriteBufferEntry(entry); err != nil {
					fmt.Fprintf(os.Stderr, "  [!] Failed to update hold count: %v\n", err)
				}
			}
		case ActionHoldRateSep:
			// Rate-separation holds do NOT increment HoldCount — the entry is
			// valid and will pass the gate in the next consolidation session.
			report.RateSepHeld++
			report.Held++
			if !*dryRun {
				entry.HeldForCrossSession = true
				if err := WriteBufferEntry(entry); err != nil {
					fmt.Fprintf(os.Stderr, "  [!] Failed to mark rate-sep hold: %v\n", err)
				}
			}
		case ActionDiscard:
			report.Discarded++
			if !*dryRun {
				if err := os.Remove(entry.FilePath); err != nil {
					fmt.Fprintf(os.Stderr, "  [!] Failed to remove: %v\n", err)
				}
			}
		}
	}

	rateSepNote := ""
	if report.RateSepHeld > 0 {
		rateSepNote = fmt.Sprintf(", %d rate-sep", report.RateSepHeld)
	}
	fmt.Printf("\n  Summary: %d promoted, %d held%s, %d discarded\n\n",
		report.Promoted, report.Held, rateSepNote, report.Discarded)

	// ─── Phase 2: LTM Integration (LLM judgment) ───
	if report.Promoted > 0 {
		fmt.Println("Phase 2: LTM Integration (requires LLM judgment)")
		fmt.Println(strings.Repeat("─", 60))
		fmt.Println("  The following buffer entries are ready for promotion.")
		fmt.Println("  Each requires a judgment call: create new memory or merge with existing?")
		fmt.Println()

		for i, prompt := range report.LLMPrompts {
			fmt.Printf("  --- Promotion %d ---\n%s\n", i+1, prompt)
		}
	}

	// ─── Phase 3: LTM Decay Pass (standard + deep only) ───
	//
	// Under the progressive-compression model (phases 1-5 of the decay
	// redesign), this pass no longer hard-archives memories. Instead it:
	//   1. Applies confidence adjustments (staleness, overrides, profile floors)
	//   2. Classifies each non-immune memory's target fidelity based on age
	//      and importance
	//   3. Marks pending compressions via the target_fidelity frontmatter
	//      field (actual content transformation is a separate Claude-driven
	//      pass via `jm compress`)
	if consolidationDepth == "standard" || consolidationDepth == "deep" {
		fmt.Println("\nPhase 3: LTM Decay Pass")
		fmt.Println(strings.Repeat("─", 60))

		for _, m := range memories {
			daysSinceAccess := now.Sub(m.LastAccessed).Hours() / 24
			isStale := daysSinceAccess > float64(cfg.StaleThresholdDays) && m.Type == TypeProject
			changed := false

			// Staleness confidence decay (project memories only)
			if isStale {
				oldConf := m.Confidence
				m.Confidence *= cfg.ConfidenceStaleFactor
				fmt.Printf("  ↓ STALE   %-40s  conf: %.2f → %.2f (%.0f days)\n",
					m.Title, oldConf, m.Confidence, daysSinceAccess)
				changed = true
			}

			// Training override confidence floor
			if m.TrainingOverride && m.Confidence < cfg.OverrideConfidenceFloor {
				m.Confidence = cfg.OverrideConfidenceFloor
				fmt.Printf("  ◆ FLOOR   %-40s  conf restored to %.2f (training override)\n",
					m.Title, m.Confidence)
				changed = true
			}

			// Profile trait confidence floor
			if m.Profile && m.Confidence < cfg.ProfileConfidenceFloor {
				m.Confidence = cfg.ProfileConfidenceFloor
				fmt.Printf("  ◆ FLOOR   %-40s  conf restored to %.2f (profile trait)\n",
					m.Title, m.Confidence)
				changed = true
			}

			// Progressive fidelity classification (replaces old archival logic)
			if !IsCompressionImmune(m) {
				targetFid := ClassifyTargetFidelity(m, now)
				currentFid := currentFidelity(m)
				if FidelityIsHigherThan(targetFid, currentFid) && m.TargetFidelity != targetFid {
					m.TargetFidelity = targetFid
					report.MemoriesQueued = append(report.MemoriesQueued, m.FilePath)
					fmt.Printf("  ⬇ QUEUE   %-40s  %s → %s (%.0fd, %s)\n",
						m.Title, currentFid, targetFid, daysSinceAccess, m.Importance)
					changed = true
				}
			}

			// Persist any changes (confidence, queue mark)
			if changed && !*dryRun {
				if err := WriteMemoryEntry(m); err != nil {
					fmt.Fprintf(os.Stderr, "  [!] Failed to update %s: %v\n", m.FileName, err)
				}
			}
		}

		if len(report.MemoriesQueued) == 0 {
			fmt.Println("  No memories queued for compression.")
		} else {
			fmt.Printf("\n  %d memories queued. Run `jm compress` to apply transitions.\n",
				len(report.MemoriesQueued))
		}
	}

	// ─── Phase 3.5: Deep consolidation extras ───
	if consolidationDepth == "deep" {
		fmt.Println("\nPhase 3.5: Deep Analysis")
		fmt.Println(strings.Repeat("─", 60))

		graph := BuildGraph(memories, cfg)
		clusters := graph.FindClusters()
		if len(clusters) > 0 {
			fmt.Printf("  Found %d concept clusters:\n", len(clusters))
			for i, cluster := range clusters {
				fmt.Printf("    Cluster %d: %s\n", i+1, strings.Join(cluster, ", "))
			}
			fmt.Println("  Consider creating Semantic/ index memories for unnamed clusters.")
		} else {
			fmt.Println("  No clusters detected (need more interconnected memories).")
		}

		// Check for orphan memories (no links)
		orphanCount := 0
		for _, m := range memories {
			if len(m.Links) == 0 {
				orphanCount++
			}
		}
		if orphanCount > 0 {
			fmt.Printf("  %d memories have no associative links — consider connecting them.\n", orphanCount)
		}

		// Profile synthesis analysis
		fmt.Println("\n  Profile Synthesis:")
		facetObservations := make(map[string]int)
		facetProfiles := make(map[string]int)
		for _, m := range memories {
			if m.Type == TypeUser && m.Facet != "" {
				if m.Profile {
					facetProfiles[m.Facet]++
				} else {
					facetObservations[m.Facet] += m.ObservationCount
					if m.ObservationCount == 0 {
						facetObservations[m.Facet]++
					}
				}
			}
		}

		allFacets := []string{"identity", "cognition", "communication", "expertise",
			"motivation", "personality", "preferences", "patterns"}
		for _, f := range allFacets {
			obs := facetObservations[f]
			prof := facetProfiles[f]
			status := "○ no data"
			if obs > 0 && obs < cfg.ProfileCreationThreshold {
				status = fmt.Sprintf("◐ %d observations (need %d for profile)", obs, cfg.ProfileCreationThreshold)
			} else if obs >= cfg.ProfileCreationThreshold && prof == 0 {
				status = fmt.Sprintf("● %d observations — READY FOR PROFILE SYNTHESIS", obs)
			} else if prof > 0 {
				status = fmt.Sprintf("◆ %d profile traits, %d supporting observations", prof, obs)
			}
			fmt.Printf("    %-16s %s\n", f, status)
		}

		// Prompt for profile synthesis where ready
		for _, f := range allFacets {
			obs := facetObservations[f]
			prof := facetProfiles[f]
			if obs >= cfg.ProfileCreationThreshold && prof == 0 {
				var relevantObs []string
				for _, m := range memories {
					if m.Type == TypeUser && m.Facet == f && !m.Profile {
						relevantObs = append(relevantObs, fmt.Sprintf("    - %s (conf=%.2f, obs=%d)", m.Title, m.Confidence, m.ObservationCount))
					}
				}
				prompt := fmt.Sprintf("\n  --- Profile Synthesis: %s ---\n", f)
				prompt += fmt.Sprintf("  %d observations are ready to be synthesized into a profile trait:\n", obs)
				prompt += strings.Join(relevantObs, "\n") + "\n"
				prompt += "  Decision needed: What profile trait should be created from these observations?\n"
				prompt += "  The trait should capture the stable pattern, not individual instances.\n"
				report.LLMPrompts = append(report.LLMPrompts, prompt)
				fmt.Print(prompt)
			}
		}
	}

	// ─── Phase 4: Log ───
	fmt.Println("\nPhase 4: Logging")
	fmt.Println(strings.Repeat("─", 60))

	if !*dryRun {
		if err := writeConsolidationLog(vaultRoot, &report); err != nil {
			fmt.Fprintf(os.Stderr, "[!] Failed to write consolidation log: %v\n", err)
		} else {
			fmt.Println("  Consolidation log appended to Metrics/consolidation_log.md")
		}
	} else {
		fmt.Println("  [DRY RUN] No log written.")
	}

	fmt.Println("\n=== Consolidation complete ===")
}

// assessBufferEntry evaluates a single buffer entry for consolidation.
func assessBufferEntry(entry *BufferEntry, memories []*MemoryEntry, cfg Config, now time.Time, lastConsolidation time.Time) BufferAssessment {
	assessment := BufferAssessment{
		Entry: entry,
	}

	// Step 1: Context integrity assessment
	assessment.ContextIntegrity = entry.ContextIntegrity
	switch entry.ContextIntegrity {
	case ContextFull:
		assessment.ContextPenalty = 1.0
	case ContextPartial:
		assessment.ContextPenalty = cfg.ContextPenaltyPartial
	case ContextOrphan:
		// Ambiguity test: check if the entry is self-contained
		if isAmbiguous(entry) && cfg.DiscardAmbiguousOrphans {
			assessment.Action = ActionDiscard
			assessment.Reason = "orphan-ambiguous: fails self-containment test"
			return assessment
		}
		assessment.ContextPenalty = cfg.ContextPenaltyOrphan
	}

	// Pinned entries always promote (bypass all gates)
	if entry.Pinned {
		assessment.Action = ActionPromote
		assessment.RetentionScore = 1.0
		assessment.Reason = "pinned by user"
		return assessment
	}

	// Check hold limit (rate-sep holds don't count toward MaxHolds)
	if entry.HoldCount >= cfg.MaxHolds {
		assessment.Action = ActionDiscard
		assessment.Reason = fmt.Sprintf("exceeded max holds (%d/%d)", entry.HoldCount, cfg.MaxHolds)
		return assessment
	}

	// CLS rate-separation gate: hold same-session entries that would merge into
	// a mature or crystallized memory. HoldCount is NOT incremented for these —
	// the entry is valid and will pass in the next consolidation session.
	if gated, target, tier := checkRateSeparationGate(entry, memories, cfg, lastConsolidation); gated {
		assessment.Action = ActionHoldRateSep
		assessment.RetentionScore = 1.0 // not scored out — held for timing, not value
		assessment.Reason = fmt.Sprintf("rate-sep gate: same-session; target %q is %s (access_count=%d)",
			target.Title, tier, target.AccessCount)
		return assessment
	}

	// Step 2: Redundancy check against existing LTM
	assessment.Redundancy = computeRedundancy(entry, memories)

	// Step 2a: Redundancy judge (C4)
	// Tag-overlap redundancy is a proxy that degrades at scale — two entries can
	// share 3 tags by taxonomy coincidence alone once the graph is dense. When
	// tag overlap crosses the threshold, we replace the proxy with a content-based
	// LLM judgment. Originally scoped to daydream-sourced entries (which always
	// show high tag overlap by construction), the gate has been broadened to any
	// source because the failure mode — cross-cutting insights penalized by the
	// heuristic — applies to conversation-sourced entries too.
	// Verdict semantics:
	//   novel     → effective redundancy = 0 (don't penalize novel cross-entry findings)
	//   redundant → force discard with judge's reason
	//   partial   → half the penalty, flag as merge-preferred in promotion prompt
	//   fallback  → API unavailable; apply DaydreamRedundancyFallbackDampening
	if cfg.DaydreamJudgeEnabled && assessment.Redundancy >= cfg.DaydreamJudgeThreshold {
		candidates := findTopRelatedMemories(entry, memories, cfg.DaydreamJudgeCandidates)
		verdict, reason, jerr := judgeDaydreamRedundancy(entry, candidates)
		if jerr != nil {
			// Fallback path: apply dampening so scoring isn't pathological when API is down.
			assessment.DaydreamVerdict = "fallback"
			assessment.DaydreamVerdictReason = jerr.Error()
			assessment.Redundancy *= cfg.DaydreamRedundancyFallbackDampening
		} else {
			assessment.DaydreamVerdict = verdict
			assessment.DaydreamVerdictReason = reason
			switch verdict {
			case "novel":
				assessment.Redundancy = 0
			case "redundant":
				assessment.Action = ActionDiscard
				assessment.Reason = fmt.Sprintf("daydream-judge redundant: %s", reason)
				return assessment
			case "partial":
				assessment.Redundancy *= 0.5
			}
		}
	}

	// Step 3: Recency factor
	hoursSinceEntry := now.Sub(entry.Timestamp).Hours()
	assessment.RecencyFactor = math.Max(0.1, 1.0-hoursSinceEntry/(24*7)) // decays over a week

	// Step 4: Retention score
	assessment.RetentionScore = entry.Surprise *
		(1 - assessment.Redundancy) *
		assessment.RecencyFactor *
		assessment.ContextPenalty

	// Step 5: Classify
	if assessment.RetentionScore > 0.5 {
		assessment.Action = ActionPromote
		assessment.Reason = fmt.Sprintf("retention=%.3f > 0.5", assessment.RetentionScore)
	} else if assessment.RetentionScore > 0.2 {
		assessment.Action = ActionHold
		assessment.Reason = fmt.Sprintf("retention=%.3f — hold for next cycle", assessment.RetentionScore)
	} else {
		assessment.Action = ActionDiscard
		assessment.Reason = fmt.Sprintf("retention=%.3f ≤ 0.2", assessment.RetentionScore)
	}

	return assessment
}

// isAmbiguous checks if a buffer entry fails the self-containment test.
// Looks for signals of context dependency.
func isAmbiguous(entry *BufferEntry) bool {
	body := strings.ToLower(entry.Body)

	// Check for dangling references
	ambiguousSignals := []string{
		"the other",
		"that approach",
		"do that again",
		"as discussed",
		"mentioned earlier",
		"the above",
		"this one",
		"like before",
		"same as",
		"the fix",
		"it worked",
	}

	for _, signal := range ambiguousSignals {
		if strings.Contains(body, signal) {
			return true
		}
	}

	// Very short entries are suspect
	if len(entry.Body) < 30 {
		return true
	}

	return false
}

// computeRedundancy checks how much of the buffer entry's information
// already exists in LTM.
func computeRedundancy(entry *BufferEntry, memories []*MemoryEntry) float64 {
	if len(memories) == 0 {
		return 0.0
	}

	// Tag overlap heuristic
	maxOverlap := 0.0
	for _, m := range memories {
		overlap := tagOverlap(entry.Tags, m.Tags)
		if overlap > maxOverlap {
			maxOverlap = overlap
		}
	}

	return maxOverlap
}

func tagOverlap(a, b []string) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0.0
	}

	setB := make(map[string]bool)
	for _, t := range b {
		setB[strings.ToLower(t)] = true
	}

	matches := 0
	for _, t := range a {
		if setB[strings.ToLower(t)] {
			matches++
		}
	}

	return float64(matches) / float64(len(a))
}

// generatePromotionPrompt creates a structured prompt for the LLM
// to make the judgment call on how to integrate a buffer entry into LTM.
func generatePromotionPrompt(entry *BufferEntry, memories []*MemoryEntry, sessionID string) string {
	var b strings.Builder

	b.WriteString(fmt.Sprintf("  Buffer entry: %s\n", entry.FileName))
	b.WriteString(fmt.Sprintf("  Timestamp: %s\n", entry.Timestamp.Format(time.RFC3339)))
	b.WriteString(fmt.Sprintf("  Surprise: %.1f\n", entry.Surprise))
	b.WriteString(fmt.Sprintf("  Tags: %s\n", strings.Join(entry.Tags, ", ")))
	b.WriteString(fmt.Sprintf("  Content:\n    %s\n\n", strings.ReplaceAll(entry.Body, "\n", "\n    ")))

	// Find potentially related existing memories, noting stability tier
	cfg := DefaultConfig()
	var related []string
	var profileTargets []*MemoryEntry
	for _, m := range memories {
		if tagOverlap(entry.Tags, m.Tags) > 0.3 {
			tier := stabilityTier(m, cfg)
			tierNote := ""
			if tier == "crystallized" {
				tierNote = " [CRYSTALLIZED]"
				if m.Profile {
					profileTargets = append(profileTargets, m)
				}
			} else if tier == "mature" {
				tierNote = " [MATURE]"
			}
			sessions := ""
			if len(m.ContributingSessions) > 0 {
				sessions = fmt.Sprintf(", contributing_sessions=%d", len(m.ContributingSessions))
			}
			related = append(related, fmt.Sprintf("    - %s (%s, conf=%.2f, access=%d%s)%s",
				m.Title, m.Type, m.Confidence, m.AccessCount, sessions, tierNote))
		}
	}

	if len(related) > 0 {
		b.WriteString("  Potentially related existing memories:\n")
		for _, r := range related {
			b.WriteString(r + "\n")
		}
	} else {
		b.WriteString("  No closely related existing memories found.\n")
	}

	b.WriteString("\n  Decision needed: CREATE new memory or MERGE with existing?\n")
	b.WriteString("  If creating: which type? (user/feedback/project/reference/semantic)\n")
	b.WriteString("  If merging: which memory, and how should confidence be updated?\n")

	// Rate-separation guidance for profile/crystallized targets
	if len(profileTargets) > 0 {
		b.WriteString("\n  *** RATE-SEPARATION NOTE (profile targets) ***\n")
		b.WriteString("  The following targets are profile facets (crystallized tier):\n")
		for _, m := range profileTargets {
			b.WriteString(fmt.Sprintf("    - %s (contributing_sessions: %v)\n",
				m.Title, m.ContributingSessions))
		}
		b.WriteString(fmt.Sprintf("  Current session ID: %s\n", sessionID))
		b.WriteString("  When merging into a profile entry:\n")
		b.WriteString("  1. APPEND the session ID to contributing_sessions in the target's frontmatter.\n")
		b.WriteString("  2. For ADDITIVE merges (new facts appended): proceed normally.\n")
		b.WriteString("  3. For REWRITE merges (existing trait descriptions changed): only proceed if\n")
		b.WriteString("     contributing_sessions will have 2+ distinct entries after this merge.\n")
		b.WriteString("     If this would be the first contributing session, make it additive only.\n")
	}

	return b.String()
}

// archiveMemory moves a memory from Memory/ to Archive/.
func archiveMemory(vaultRoot string, m *MemoryEntry, reason string, finalScore float64, now time.Time) {
	m.Archived = &now
	m.ArchiveReason = reason
	m.FinalScore = finalScore

	archivePath := filepath.Join(vaultRoot, "Archive", m.FileName)
	m.FilePath = archivePath

	if err := WriteMemoryEntry(m); err != nil {
		fmt.Fprintf(os.Stderr, "  [!] Failed to write archive: %v\n", err)
		return
	}

	// Remove from original location
	origPath := filepath.Join(vaultRoot, "Memory")
	subdirs := []string{"User", "Feedback", "Project", "Reference", "Semantic"}
	for _, sub := range subdirs {
		candidate := filepath.Join(origPath, sub, m.FileName)
		if _, err := os.Stat(candidate); err == nil {
			os.Remove(candidate)
			break
		}
	}
}

// writeConsolidationLog appends to the consolidation log.
func writeConsolidationLog(vaultRoot string, report *ConsolidationReport) error {
	logPath := filepath.Join(vaultRoot, "Metrics", "consolidation_log.md")

	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	fmt.Fprintf(f, "\n## %s — %s (%s)\n\n",
		report.Timestamp.Format("2006-01-02 15:04"),
		report.Trigger,
		report.Depth)

	fmt.Fprintf(f, "- Buffer entries processed: %d\n", len(report.BufferAssessments))
	fmt.Fprintf(f, "- Promoted: %d\n", report.Promoted)
	if report.RateSepHeld > 0 {
		fmt.Fprintf(f, "- Held: %d (%d rate-separation gate)\n", report.Held, report.RateSepHeld)
	} else {
		fmt.Fprintf(f, "- Held: %d\n", report.Held)
	}
	fmt.Fprintf(f, "- Discarded: %d\n", report.Discarded)
	fmt.Fprintf(f, "- Queued for compression: %d\n", len(report.MemoriesQueued))
	if len(report.MemoriesArchived) > 0 {
		fmt.Fprintf(f, "- Archived (legacy path): %d\n", len(report.MemoriesArchived))
	}

	if len(report.BufferAssessments) > 0 {
		fmt.Fprintln(f, "\n### Actions")
		for _, a := range report.BufferAssessments {
			fmt.Fprintf(f, "- **%s** `%s` — %s\n", a.Action, a.Entry.FileName, a.Reason)
		}
	}

	if len(report.MemoriesQueued) > 0 {
		fmt.Fprintln(f, "\n### Queued for compression")
		for _, path := range report.MemoriesQueued {
			fmt.Fprintf(f, "- `%s`\n", filepath.Base(path))
		}
	}

	if len(report.MemoriesArchived) > 0 {
		fmt.Fprintln(f, "\n### Archived (legacy)")
		for _, path := range report.MemoriesArchived {
			fmt.Fprintf(f, "- `%s`\n", filepath.Base(path))
		}
	}

	fmt.Fprintln(f)
	return nil
}

package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"
)

func cmdScore(vaultRoot string, args []string) {
	fs := flag.NewFlagSet("score", flag.ExitOnError)
	tags := fs.String("tags", "", "Comma-separated context tags for relevance scoring")
	intent := fs.String("intent", "", "Query intent: guidance, who, what, where")
	verbose := fs.Bool("v", false, "Show score breakdown per memory")
	fs.Parse(args)

	cfg := DefaultConfig()
	now := time.Now()

	memories, err := LoadAllMemories(vaultRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[!] Failed to load memories: %v\n", err)
		os.Exit(1)
	}

	if len(memories) == 0 {
		fmt.Println("No memories found in Memory/")
		return
	}

	var contextTags []string
	if *tags != "" {
		for _, t := range strings.Split(*tags, ",") {
			contextTags = append(contextTags, strings.TrimSpace(t))
		}
	}

	scored := ScoreAllMemories(memories, contextTags, *intent, cfg, now)

	// Build graph for spreading activation
	graph := BuildGraph(memories, cfg)
	scored = ApplySpreadingActivation(scored, graph, cfg)

	fmt.Printf("%-50s %8s %8s %8s %8s %8s %8s\n",
		"MEMORY", "ACTIV", "RELEV", "CONF", "SURP", "BOOST", "TOTAL")
	fmt.Println(strings.Repeat("─", 110))

	for _, s := range scored {
		marker := " "
		if s.Total >= cfg.RetrievalThreshold {
			marker = "●"
		}
		if s.Memory.TrainingOverride {
			marker = "◆"
		}

		title := s.Memory.Title
		if len(title) > 48 {
			title = title[:45] + "..."
		}

		fmt.Printf("%s %-48s %8.3f %8.3f %8.3f %8.3f %8.3f %8.3f\n",
			marker, title,
			s.Activation, s.Relevance, s.Confidence, s.Surprise, s.Boost, s.Total)

		if *verbose {
			relPath := s.Memory.FilePath
			fmt.Printf("  File: %s\n", relPath)
			fmt.Printf("  Type: %s | Decay: %.2f | Access: %d | Last: %s\n",
				s.Memory.Type, s.Memory.DecayRate, s.Memory.AccessCount,
				s.Memory.LastAccessed.Format("2006-01-02"))
			if len(s.Memory.Tags) > 0 {
				fmt.Printf("  Tags: %s\n", strings.Join(s.Memory.Tags, ", "))
			}
			if len(s.Memory.Links) > 0 {
				for _, l := range s.Memory.Links {
					fmt.Printf("  Link: --%s--> %s\n", l.Relationship, l.Target)
				}
			}
			fmt.Println()
		}
	}

	fmt.Println(strings.Repeat("─", 110))
	aboveThreshold := 0
	for _, s := range scored {
		if s.Total >= cfg.RetrievalThreshold {
			aboveThreshold++
		}
	}
	fmt.Printf("● = above τ (%.1f) | ◆ = training override | %d/%d retrievable\n",
		cfg.RetrievalThreshold, aboveThreshold, len(scored))
}

package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

// cmdReclassify is a one-off maintenance command that backfills the
// importance field on existing memories. It never overwrites an explicit
// importance value — only fills in the blanks.
//
// Rules (from InferImportance in decay_model.go):
//   - profile, training_override, episodic, knowledge, buffer → critical
//     (they're all compression-immune, importance is tracked for consistency)
//   - feedback, project, semantic → significant
//   - user (non-profile), reference → moderate
//   - surprise >= 0.7 bumps one level up
//   - 0 < surprise < 0.3 bumps one level down
func cmdReclassify(vaultRoot string, args []string) {
	fs := flag.NewFlagSet("reclassify", flag.ExitOnError)
	dryRun := fs.Bool("dry-run", false, "Show what would be set without writing")
	force := fs.Bool("force", false, "Overwrite existing importance values (normally preserved)")
	fs.Parse(args)

	memories, err := LoadAllMemories(vaultRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[!] Failed to load memories: %v\n", err)
		os.Exit(1)
	}

	if *dryRun {
		fmt.Printf("=== Importance Reclassification (DRY RUN) ===\n\n")
	} else {
		fmt.Printf("=== Importance Reclassification ===\n\n")
	}

	type change struct {
		path    string
		old     string
		new     string
		reason  string
		skipped bool
	}
	var changes []change

	for _, m := range memories {
		relPath, _ := filepath.Rel(vaultRoot, m.FilePath)
		inferred := string(InferImportance(m))

		if m.Importance != "" && !*force {
			// Already has a value and --force not set — preserve.
			if m.Importance != inferred {
				changes = append(changes, change{
					path:    relPath,
					old:     m.Importance,
					new:     inferred,
					reason:  "already set (use --force to override)",
					skipped: true,
				})
			}
			continue
		}

		if m.Importance == inferred {
			continue
		}

		changes = append(changes, change{
			path: relPath,
			old:  m.Importance,
			new:  inferred,
		})

		if !*dryRun {
			m.Importance = inferred
			if err := WriteMemoryEntry(m); err != nil {
				fmt.Fprintf(os.Stderr, "[!] %s: %v\n", relPath, err)
			}
		}
	}

	applied := 0
	skipped := 0
	for _, c := range changes {
		marker := " "
		if c.skipped {
			marker = "·"
			skipped++
		} else {
			marker = "✓"
			applied++
		}
		old := c.old
		if old == "" {
			old = "(unset)"
		}
		fmt.Printf("  %s %-55s %-12s → %-12s", marker, c.path, old, c.new)
		if c.reason != "" {
			fmt.Printf("  [%s]", c.reason)
		}
		fmt.Println()
	}

	fmt.Println()
	if *dryRun {
		fmt.Printf("[DRY RUN] Would update: %d | Skipped: %d\n", applied, skipped)
	} else {
		fmt.Printf("Updated: %d | Skipped: %d\n", applied, skipped)
	}

	if skipped > 0 && !*force {
		fmt.Println("\nNote: Memories with existing importance values were preserved.")
		fmt.Println("      Use --force to overwrite them.")
	}
}

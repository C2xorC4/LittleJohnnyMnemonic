package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// cmdInstall dispatches install-health subcommands:
//
//	jm install check              — examine hooks / binary / project scope
//	jm install fix [--keep-project-hooks]  — rewrite per-platform global hooks
//	jm install ignore <codes...> | --all | --clear
func cmdInstall(vaultRoot string, args []string) {
	if len(args) < 1 {
		printInstallUsage()
		os.Exit(1)
	}
	switch args[0] {
	case "check":
		cmdInstallCheck(vaultRoot, args[1:])
	case "fix":
		cmdInstallFix(vaultRoot, args[1:])
	case "ignore":
		cmdInstallIgnore(vaultRoot, args[1:])
	case "-h", "--help", "help":
		printInstallUsage()
	default:
		fmt.Fprintf(os.Stderr, "unknown install subcommand: %s\n", args[0])
		printInstallUsage()
		os.Exit(1)
	}
}

func printInstallUsage() {
	fmt.Fprintln(os.Stderr, `usage: jm install <subcommand>

  check                         Report install health (hooks, platform runner, binary)
  fix [--keep-project-hooks]    Rewrite ~/.grok/hooks/ljm.json for this platform+vault;
                                remove project .grok/hooks/ljm.json unless --keep-project-hooks
  ignore <code> [<code>...]     Suppress specific issue codes for future hook warnings
  ignore --all                  Suppress all install-health warnings
  ignore --clear                Clear ignore state (re-enable warnings)

Issue codes: grok_hooks_missing, grok_hooks_unsubstituted, grok_hooks_wrong_platform,
  grok_hooks_vault_mismatch, grok_hooks_runner_missing, project_ljm_hooks,
  jm_binary_missing, claude_hooks_wrong_platform, claude_hooks_unsubstituted

Protocol: session-start emits <ljm-install-warning> when issues remain after ignore
filtering. Agents must consult the user before running fix or ignore.`)
}

func cmdInstallCheck(vaultRoot string, args []string) {
	jsonOut := false
	for _, a := range args {
		if a == "--json" {
			jsonOut = true
		}
	}
	rep := CheckInstallHealth(vaultRoot)
	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(rep)
		if !rep.Healthy {
			os.Exit(2)
		}
		return
	}

	fmt.Printf("LJM install health — %s\n", map[bool]string{true: "OK", false: "ISSUES"}[rep.Healthy])
	fmt.Printf("  Platform:   %s\n", rep.Platform)
	fmt.Printf("  Vault:      %s\n", rep.VaultRoot)
	fmt.Printf("  Grok hooks: %s\n", rep.HookFile)
	if len(rep.Ignored) > 0 {
		fmt.Printf("  Ignored:    %s\n", strings.Join(rep.Ignored, ", "))
	}
	fmt.Println()
	if rep.Healthy {
		fmt.Println("No active issues.")
		return
	}
	for i, iss := range rep.Issues {
		fmt.Printf("%d. [%s] %s\n", i+1, iss.Severity, iss.Code)
		fmt.Printf("   %s\n", iss.Summary)
		if iss.Detail != "" {
			fmt.Printf("   detail: %s\n", iss.Detail)
		}
		if iss.Path != "" {
			fmt.Printf("   path:   %s\n", iss.Path)
		}
		if iss.FixHint != "" {
			fmt.Printf("   fix:    %s\n", iss.FixHint)
		}
	}
	fmt.Println()
	fmt.Println("Remediate (after user permission): jm install fix")
	fmt.Println("Suppress:                         jm install ignore <code> | --all")
	os.Exit(2)
}

func cmdInstallFix(vaultRoot string, args []string) {
	opts := FixInstallOptions{RemoveProjectHooks: true}
	for _, a := range args {
		switch a {
		case "--keep-project-hooks":
			opts.RemoveProjectHooks = false
		case "-h", "--help":
			fmt.Fprintln(os.Stderr, "usage: jm install fix [--keep-project-hooks]")
			return
		default:
			fmt.Fprintf(os.Stderr, "unknown flag: %s\n", a)
			os.Exit(1)
		}
	}

	if err := FixInstallHealth(vaultRoot, opts); err != nil {
		fmt.Fprintf(os.Stderr, "jm install fix: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Install fixed for this platform:")
	fmt.Printf("  Platform: %s\n", installPlatform())
	fmt.Printf("  Vault:    %s\n", vaultRoot)
	fmt.Printf("  Hooks:    %s\n", grokHooksDest())
	fmt.Printf("  Runner:   %s\n", expectedHookRunner(vaultRoot))
	if opts.RemoveProjectHooks {
		fmt.Println("  Project:  .grok/hooks/ljm.json removed if it was present")
	}
	fmt.Println()
	fmt.Println("Press r in /hooks or restart Grok sessions to reload.")

	// Re-check and report remaining issues (e.g. missing binary).
	rep := CheckInstallHealth(vaultRoot)
	if !rep.Healthy {
		fmt.Println()
		fmt.Println("Remaining issues after fix:")
		for _, iss := range rep.Issues {
			fmt.Printf("  - [%s] %s: %s\n", iss.Severity, iss.Code, iss.Summary)
			if iss.FixHint != "" {
				fmt.Printf("    → %s\n", iss.FixHint)
			}
		}
		os.Exit(2)
	}
	fmt.Println("Re-check: healthy.")
}

func grokHooksDest() string {
	gh := grokHomeDir()
	if gh == "" {
		return "~/.grok/hooks/ljm.json"
	}
	return filepath.Join(gh, "hooks", "ljm.json")
}

func cmdInstallIgnore(vaultRoot string, args []string) {
	_ = vaultRoot
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: jm install ignore <code> [<code>...] | --all | --clear")
		os.Exit(1)
	}

	st := loadInstallIgnore()

	switch args[0] {
	case "--clear":
		st = installIgnoreState{}
		if err := saveInstallIgnore(st); err != nil {
			fmt.Fprintf(os.Stderr, "jm install ignore: %v\n", err)
			os.Exit(1)
		}
		// Remove file entirely for cleanliness.
		if p := installIgnorePath(); p != "" {
			_ = os.Remove(p)
		}
		fmt.Println("Install-health ignore cleared. Warnings will surface again on session-start.")
		return
	case "--all":
		st.IgnoreAll = true
		st.Codes = nil
		st.Note = "user requested ignore all via jm install ignore --all"
		if err := saveInstallIgnore(st); err != nil {
			fmt.Fprintf(os.Stderr, "jm install ignore: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Ignoring all install-health warnings (%s)\n", installIgnorePath())
		return
	}

	// Collect codes; allow repeated ignore merges.
	codeSet := map[string]bool{}
	for _, c := range st.Codes {
		codeSet[c] = true
	}
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			fmt.Fprintf(os.Stderr, "unknown flag in ignore list: %s (use --all or --clear alone)\n", a)
			os.Exit(1)
		}
		codeSet[a] = true
	}
	st.IgnoreAll = false
	st.Codes = st.Codes[:0]
	for c := range codeSet {
		st.Codes = append(st.Codes, c)
	}
	sort.Strings(st.Codes)
	st.Note = "user suppressed specific codes via jm install ignore"
	if err := saveInstallIgnore(st); err != nil {
		fmt.Fprintf(os.Stderr, "jm install ignore: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Ignoring install codes: %s\n", strings.Join(st.Codes, ", "))
	fmt.Printf("State: %s\n", installIgnorePath())
}

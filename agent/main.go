package main

import (
	"fmt"
	"os"
	"path/filepath"
)

const usage = `johnnymnemonic — Cognitive Memory Agent

Usage:
  jm <command> [options]

Commands:
  score        Compute and display activation scores for all memories
  retrieve     Scored retrieval with spreading activation (LLM-consumable output)
  associate    Contextual association — free-text to memory matching with enrichment detection
  hook         Agent hook dispatcher — Claude Code and Grok Build (session-start, user-prompt-submit, stop, pre-tool-use)
  rule-judge   Async behavioral-rule judge subprocess (spawned by stop hook)
  rule-firings Aggregate and display behavioral-rule firing log (see rule_firings.jsonl)
  ingestion    Book ingestion lifecycle: list / scan / sync (see Ingestion/_README.md)
  learn-edges  Detect and create graph edges from co-activation patterns
  consolidate  Run consolidation phases on the buffer
  decay        Apply decay pass to all LTM entries
  heal         Rewrite all memories through the parser (maintenance/repair)
  unarchive    Explicit un-archive (complements B' auto-resurrection)
  reclassify   Backfill importance field on memories (one-off maintenance)
  compress     Apply progressive fidelity compression (Claude-driven, stdin body)
  restore      Restore an archived full version back into Memory/
  migrate-archive  Reorganize legacy flat Archive/ into per-type subdirectories
  buffer       Buffer operations (list, count, threshold check)
  status       System health dashboard
  config       Display current configuration
  backup       Encrypted vault backup (age-encrypted tar.gz, local + optional git remote)
  restore-backup  Decrypt and extract a backup (distinct from "restore" — see -h)
  autodream    Auto-daydream scheduler (jitter + mode + replay; see Config.md)
  daydream     Daydream finding triage — review or list pending entries
  graph        Export interactive HTML visualization of the memory graph
  edges        Inspect outgoing edges of a memory (base / authored / effective weights, usage)
  lint-links   Audit link health: asymmetric, dangling, prose-only, and concept-mention links
  recover-provenance  Recover frontmatter fields stripped by the round-trip bug, from backup history
  trust        Manage non-root instruction file approvals (trust approve <rel-path>)
  machines     List registered machines and tooling (see System/machines.json)
  benchmark    Comparative evaluation harness (validate, retrieve-check, grade, …)

Use "jm <command> -h" for command-specific help.
`

func main() {
	vaultRoot := findVaultRoot()

	if len(os.Args) < 2 {
		fmt.Print(usage)
		os.Exit(0)
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	switch cmd {
	case "score":
		cmdScore(vaultRoot, args)
	case "retrieve":
		cmdRetrieve(vaultRoot, args)
	case "associate":
		cmdAssociate(vaultRoot, args)
	case "hook":
		cmdHook(vaultRoot, args)
	case "rule-judge":
		cmdRuleJudge(vaultRoot, args)
	case "rule-firings":
		cmdRuleFirings(vaultRoot, args)
	case "ingestion":
		cmdIngestion(vaultRoot, args)
	case "learn-edges":
		cmdLearnEdges(vaultRoot, args)
	case "consolidate":
		cmdConsolidate(vaultRoot, args)
	case "decay":
		cmdDecay(vaultRoot, args)
	case "heal":
		cmdHeal(vaultRoot, args)
	case "unarchive":
		cmdUnarchive(vaultRoot, args)
	case "reclassify":
		cmdReclassify(vaultRoot, args)
	case "compress":
		cmdCompress(vaultRoot, args)
	case "restore":
		cmdRestore(vaultRoot, args)
	case "migrate-archive":
		cmdMigrateArchive(vaultRoot, args)
	case "buffer":
		cmdBuffer(vaultRoot, args)
	case "status":
		cmdStatus(vaultRoot, args)
	case "config":
		cmdConfig(vaultRoot, args)
	case "backup":
		cmdBackup(vaultRoot, args)
	case "restore-backup":
		cmdRestoreBackup(vaultRoot, args)
	case "autodream":
		cmdAutodream(vaultRoot, args)
	case "daydream":
		cmdDaydream(vaultRoot, args)
	case "graph":
		cmdGraph(vaultRoot, args)
	case "edges":
		cmdEdges(vaultRoot, args)
	case "lint-links":
		cmdLintLinks(vaultRoot, args)
	case "recover-provenance":
		cmdRecoverProvenance(vaultRoot, args)
	case "migrate-access":
		cmdMigrateAccess(vaultRoot, args)
	case "sync-access":
		cmdSyncAccess(vaultRoot, args)
	case "trust":
		cmdTrust(vaultRoot, args)
	case "machines":
		cmdMachines(vaultRoot, args)
	case "metrics":
		cmdMetrics(vaultRoot, args)
	case "benchmark":
		cmdBenchmark(vaultRoot, args)
	case "-h", "--help", "help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", cmd)
		fmt.Print(usage)
		os.Exit(1)
	}
}

// findVaultRoot walks up from the executable (or cwd) to find the vault root
// by looking for the System/ directory and CLAUDE.md.
func findVaultRoot() string {
	// First check if VAULT_ROOT env is set
	if root := os.Getenv("JM_VAULT_ROOT"); root != "" {
		return root
	}

	// Try relative to executable location (agent/ is inside the vault)
	exe, err := os.Executable()
	if err == nil {
		candidate := filepath.Dir(filepath.Dir(exe)) // up from agent/
		if isVaultRoot(candidate) {
			return candidate
		}
	}

	// Try walking up from cwd
	cwd, err := os.Getwd()
	if err == nil {
		dir := cwd
		for {
			if isVaultRoot(dir) {
				return dir
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}

	// Default: assume cwd parent is vault root (we're in agent/)
	if cwd != "" {
		return filepath.Dir(cwd)
	}

	fmt.Fprintln(os.Stderr, "[!] Could not find vault root. Set JM_VAULT_ROOT or run from within the vault.")
	os.Exit(1)
	return ""
}

func isVaultRoot(dir string) bool {
	claude := filepath.Join(dir, "CLAUDE.md")
	system := filepath.Join(dir, "System")
	_, err1 := os.Stat(claude)
	_, err2 := os.Stat(system)
	return err1 == nil && err2 == nil
}

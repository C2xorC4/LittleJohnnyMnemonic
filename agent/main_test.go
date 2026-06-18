package main

import (
	"os"
	"testing"
)

// TestMain hardens the whole test binary against two failure modes triggered
// by the consolidation/rule-judge spawn paths, which re-exec os.Executable()
// — under `go test` that is THIS test binary.
//
//  1. Fork bomb. A re-exec'd test binary normally re-runs the ENTIRE suite
//     (m.Run), which re-hits the spawn tests, which re-exec again… Each
//     generation is detached, so the chain outlives the run. We intercept
//     known re-exec subcommands and dispatch them to the real command router
//     (main) so the child behaves like the jm binary and exits, instead of
//     recursing into the suite.
//
//  2. Live LLM calls. On a host with a real `claude` CLI on PATH and the judge
//     CLI fallback enabled by default config, the re-exec'd consolidate/judge
//     would cold-boot a ~320MB `claude -p` per judge call and swarm the host.
//     Setting LJM_NO_JUDGE_CLI hard-disables the Tier-2 CLI fallback; clearing
//     ANTHROPIC_API_KEY removes the Tier-1 path. Both are set before any spawn
//     and are inherited by the detached children, so the whole process tree
//     degrades to heuristics — no network, no CLI spawns.
func TestMain(m *testing.M) {
	os.Setenv(judgeCLIDisableEnvVar, "1")
	os.Unsetenv("ANTHROPIC_API_KEY")

	if len(os.Args) > 1 && isReexecSubcommand(os.Args[1]) {
		main()
		return
	}

	os.Exit(m.Run())
}

// isReexecSubcommand reports whether os.Args[1] is a subcommand that the spawn
// paths re-exec the binary with (spawnConsolidationIfNeeded → "consolidate",
// spawnJudge → "rule-judge"). Keep in sync with those spawn sites.
func isReexecSubcommand(s string) bool {
	switch s {
	case "consolidate", "rule-judge":
		return true
	}
	return false
}

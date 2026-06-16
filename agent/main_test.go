package main

import (
	"os"
	"testing"
)

// TestMain hardens the whole test binary against making real LLM judge calls.
//
// Several tests exercise the consolidation/rule-judge spawn paths, which
// re-exec os.Executable() — during `go test` that is THIS test binary — in
// `consolidate` / `rule-judge` mode, detached. On a host with a real `claude`
// CLI on PATH (and the judge CLI fallback enabled by default config), those
// detached children run live judges and cold-boot a ~320MB `claude -p` per
// call, swarming the host and outliving the test run.
//
// Setting LJM_NO_JUDGE_CLI hard-disables the Tier-2 CLI fallback at the judge
// chokepoint; clearing ANTHROPIC_API_KEY removes the Tier-1 path. Both env
// changes are inherited by the detached re-exec'd subprocesses (they don't
// override cmd.Env), so the entire test process tree degrades to heuristics and
// never touches the network or spawns a CLI.
func TestMain(m *testing.M) {
	os.Setenv(judgeCLIDisableEnvVar, "1")
	os.Unsetenv("ANTHROPIC_API_KEY")
	os.Exit(m.Run())
}

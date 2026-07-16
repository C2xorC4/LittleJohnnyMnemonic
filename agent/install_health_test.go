package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCheckInstallHealth_HealthyLayout(t *testing.T) {
	vault, grokHome := setupInstallFixture(t, installFixtureOpts{
		withTemplate: true,
		withBinary:   true,
		writeGlobal:  true,
		platformOK:   true,
	})
	t.Setenv("GROK_HOME", grokHome)
	t.Setenv("CLAUDE_SETTINGS_PATH", filepath.Join(t.TempDir(), "no-claude-settings.json"))

	rep := CheckInstallHealth(vault)
	if !rep.Healthy {
		t.Fatalf("expected healthy, got issues: %+v", rep.Issues)
	}
}

func TestCheckInstallHealth_UnsubstitutedTemplate(t *testing.T) {
	vault, grokHome := setupInstallFixture(t, installFixtureOpts{
		withTemplate: true,
		withBinary:   true,
		writeGlobal:  true,
		rawGlobal: `{
  "hooks": {
    "SessionStart": [{
      "hooks": [{
        "type": "command",
        "command": "__HOOK_RUNNER__ session-start",
        "env": { "JM_VAULT_ROOT": "__JM_VAULT_ROOT__" }
      }]
    }]
  }
}`,
	})
	t.Setenv("GROK_HOME", grokHome)
	t.Setenv("CLAUDE_SETTINGS_PATH", filepath.Join(t.TempDir(), "missing.json"))

	rep := CheckInstallHealth(vault)
	if rep.Healthy {
		t.Fatal("expected unhealthy for unsubstituted placeholders")
	}
	if !hasIssueCode(rep, IssueGrokHooksUnsubstituted) {
		t.Fatalf("missing %s in %+v", IssueGrokHooksUnsubstituted, codesOf(rep))
	}
}

func TestCheckInstallHealth_WrongPlatformPowerShellOnUnix(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("wrong-platform PowerShell check targets Unix hosts")
	}
	vault, grokHome := setupInstallFixture(t, installFixtureOpts{
		withTemplate: true,
		withBinary:   true,
		writeGlobal:  true,
		rawGlobal: `{
  "hooks": {
    "SessionStart": [{
      "hooks": [{
        "type": "command",
        "command": "powershell -NoProfile -File \"/tmp/x/grok/bin/run-hook.ps1\" session-start",
        "env": { "JM_VAULT_ROOT": "VAULT" }
      }]
    }]
  }
}`,
	})
	// Patch vault path in raw after we know it
	hookPath := filepath.Join(grokHome, "hooks", "ljm.json")
	raw, _ := os.ReadFile(hookPath)
	fixed := strings.ReplaceAll(string(raw), "VAULT", filepath.ToSlash(vault))
	// Point runner at a non-existent ps1 so runner_missing may also fire — we care about wrong_platform
	_ = os.WriteFile(hookPath, []byte(fixed), 0o644)

	t.Setenv("GROK_HOME", grokHome)
	t.Setenv("CLAUDE_SETTINGS_PATH", filepath.Join(t.TempDir(), "missing.json"))

	rep := CheckInstallHealth(vault)
	if !hasIssueCode(rep, IssueGrokHooksWrongPlatform) {
		t.Fatalf("expected %s, got %+v", IssueGrokHooksWrongPlatform, codesOf(rep))
	}
}

func TestCheckInstallHealth_ProjectHooks(t *testing.T) {
	vault, grokHome := setupInstallFixture(t, installFixtureOpts{
		withTemplate:  true,
		withBinary:    true,
		writeGlobal:   true,
		platformOK:    true,
		projectHooks:  true,
	})
	t.Setenv("GROK_HOME", grokHome)
	t.Setenv("CLAUDE_SETTINGS_PATH", filepath.Join(t.TempDir(), "missing.json"))

	rep := CheckInstallHealth(vault)
	if !hasIssueCode(rep, IssueProjectLJMHooks) {
		t.Fatalf("expected %s, got %+v", IssueProjectLJMHooks, codesOf(rep))
	}
}

func TestCheckInstallHealth_IgnoreCodes(t *testing.T) {
	vault, grokHome := setupInstallFixture(t, installFixtureOpts{
		withTemplate: true,
		withBinary:   true,
		writeGlobal:  true,
		platformOK:   true,
		projectHooks: true,
	})
	t.Setenv("GROK_HOME", grokHome)
	t.Setenv("CLAUDE_SETTINGS_PATH", filepath.Join(t.TempDir(), "missing.json"))

	// Ignore project hooks issue
	if err := saveInstallIgnore(installIgnoreState{Codes: []string{IssueProjectLJMHooks}}); err != nil {
		t.Fatal(err)
	}

	rep := CheckInstallHealth(vault)
	if hasIssueCode(rep, IssueProjectLJMHooks) {
		t.Fatal("project_ljm_hooks should be ignored")
	}
	if len(rep.Ignored) == 0 {
		t.Fatal("expected ignored codes recorded")
	}
	if !rep.Healthy {
		t.Fatalf("expected healthy after ignore, got %+v", rep.Issues)
	}
}

func TestFixInstallHealth_RendersPlatformHooksAndRemovesProject(t *testing.T) {
	vault, grokHome := setupInstallFixture(t, installFixtureOpts{
		withTemplate: true,
		withBinary:   true,
		writeGlobal:  true,
		rawGlobal:    `{"hooks":{}}`, // garbage prior install
		projectHooks: true,
	})
	t.Setenv("GROK_HOME", grokHome)
	t.Setenv("CLAUDE_SETTINGS_PATH", filepath.Join(t.TempDir(), "missing.json"))

	if err := FixInstallHealth(vault, FixInstallOptions{RemoveProjectHooks: true}); err != nil {
		t.Fatal(err)
	}

	dest := filepath.Join(grokHome, "hooks", "ljm.json")
	raw, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	content := string(raw)
	if strings.Contains(content, "__HOOK_RUNNER__") || strings.Contains(content, "__JM_VAULT_ROOT__") {
		t.Fatal("placeholders remain after fix")
	}
	if !strings.Contains(content, filepath.ToSlash(vault)) {
		t.Fatalf("vault path not embedded: %s", content)
	}
	if runtime.GOOS != "windows" {
		if strings.Contains(strings.ToLower(content), "powershell") {
			t.Fatal("unix fix should not emit powershell")
		}
		if !strings.Contains(content, "run-hook") {
			t.Fatal("unix fix should reference run-hook")
		}
	} else {
		if !strings.Contains(strings.ToLower(content), "powershell") {
			t.Fatal("windows fix should emit powershell")
		}
	}

	projectHook := filepath.Join(vault, ".grok", "hooks", "ljm.json")
	if _, err := os.Stat(projectHook); !os.IsNotExist(err) {
		t.Fatal("project ljm.json should be removed")
	}

	rep := CheckInstallHealth(vault)
	if !rep.Healthy {
		t.Fatalf("expected healthy after fix, got %+v", rep.Issues)
	}
}

func TestWriteInstallHealthWarning_ContainsProtocol(t *testing.T) {
	var buf bytes.Buffer
	writeInstallHealthWarning(&buf, InstallHealthReport{
		Platform:  "linux",
		VaultRoot: "/vault",
		HookFile:  "/home/u/.grok/hooks/ljm.json",
		Issues: []InstallIssue{{
			Code:     IssueGrokHooksWrongPlatform,
			Severity: "error",
			Summary:  "wrong runner",
			FixHint:  "jm install fix",
		}},
	})
	out := buf.String()
	for _, want := range []string{
		"<ljm-install-warning>",
		"</ljm-install-warning>",
		IssueGrokHooksWrongPlatform,
		"Ask permission",
		"jm install fix",
		"jm install ignore",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("warning missing %q\n%s", want, out)
		}
	}
}

// --- helpers ---

type installFixtureOpts struct {
	withTemplate bool
	withBinary   bool
	writeGlobal  bool
	platformOK   bool
	rawGlobal    string
	projectHooks bool
}

func setupInstallFixture(t *testing.T, opts installFixtureOpts) (vault, grokHome string) {
	t.Helper()
	vault = t.TempDir()
	grokHome = t.TempDir()

	// Minimal vault markers
	if err := os.MkdirAll(filepath.Join(vault, "System"), 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(vault, "CLAUDE.md"), []byte("# test\n"), 0o644)

	if opts.withTemplate {
		dir := filepath.Join(vault, "grok", "hooks")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		tmpl := `{
  "hooks": {
    "SessionStart": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "__HOOK_RUNNER__ session-start",
            "timeout": 30,
            "env": {
              "JM_VAULT_ROOT": "__JM_VAULT_ROOT__"
            }
          }
        ]
      }
    ]
  }
}
`
		if err := os.WriteFile(filepath.Join(dir, "ljm.template.json"), []byte(tmpl), 0o644); err != nil {
			t.Fatal(err)
		}
		// runners
		bin := filepath.Join(vault, "grok", "bin")
		if err := os.MkdirAll(bin, 0o755); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"run-hook", "run-hook.sh", "run-hook.ps1", "run-hook.cmd"} {
			_ = os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"), 0o755)
		}
	}

	if opts.withBinary {
		agentDir := filepath.Join(vault, "agent")
		if err := os.MkdirAll(agentDir, 0o755); err != nil {
			t.Fatal(err)
		}
		name := "jm"
		if runtime.GOOS == "windows" {
			name = "jm.exe"
		}
		_ = os.WriteFile(filepath.Join(agentDir, name), []byte("fake"), 0o755)
	}

	if opts.writeGlobal {
		hooksDir := filepath.Join(grokHome, "hooks")
		if err := os.MkdirAll(hooksDir, 0o755); err != nil {
			t.Fatal(err)
		}
		var content string
		if opts.rawGlobal != "" {
			content = opts.rawGlobal
		} else if opts.platformOK {
			runner := expectedHookRunner(vault)
			// expectedHookRunner uses runtime GOOS — good for fixture
			content = `{
  "hooks": {
    "SessionStart": [{
      "hooks": [{
        "type": "command",
        "command": "` + escapeJSON(runner) + ` session-start",
        "env": { "JM_VAULT_ROOT": "` + escapeJSON(filepath.ToSlash(vault)) + `" }
      }]
    }]
  }
}`
		} else {
			content = `{"hooks":{}}`
		}
		if err := os.WriteFile(filepath.Join(hooksDir, "ljm.json"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if opts.projectHooks {
		dir := filepath.Join(vault, ".grok", "hooks")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		_ = os.WriteFile(filepath.Join(dir, "ljm.json"), []byte(`{"hooks":{}}`), 0o644)
	}

	return vault, grokHome
}

func hasIssueCode(rep InstallHealthReport, code string) bool {
	for _, iss := range rep.Issues {
		if iss.Code == code {
			return true
		}
	}
	return false
}

func codesOf(rep InstallHealthReport) []string {
	var out []string
	for _, iss := range rep.Issues {
		out = append(out, iss.Code)
	}
	return out
}

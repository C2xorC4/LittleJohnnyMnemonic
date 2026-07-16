package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Install issue codes — stable identifiers for ignore lists and agent protocol.
const (
	IssueGrokHooksMissing       = "grok_hooks_missing"
	IssueGrokHooksUnsubstituted = "grok_hooks_unsubstituted"
	IssueGrokHooksWrongPlatform = "grok_hooks_wrong_platform"
	IssueGrokHooksVaultMismatch = "grok_hooks_vault_mismatch"
	IssueGrokHooksRunnerMissing = "grok_hooks_runner_missing"
	IssueProjectLJMHooks        = "project_ljm_hooks"
	IssueJMBinaryMissing        = "jm_binary_missing"
	IssueClaudeHooksWrongPlatform = "claude_hooks_wrong_platform"
	IssueClaudeHooksUnsubstituted = "claude_hooks_unsubstituted"
)

// InstallIssue is one detected install/setup problem.
type InstallIssue struct {
	Code     string `json:"code"`
	Severity string `json:"severity"` // error | warn
	Summary  string `json:"summary"`
	Detail   string `json:"detail,omitempty"`
	Path     string `json:"path,omitempty"`
	FixHint  string `json:"fix_hint,omitempty"`
}

// InstallHealthReport is the result of examining host install state.
type InstallHealthReport struct {
	Platform   string         `json:"platform"`
	VaultRoot  string         `json:"vault_root"`
	GrokHome   string         `json:"grok_home"`
	HookFile   string         `json:"hook_file"`
	Issues     []InstallIssue `json:"issues"`
	Ignored    []string       `json:"ignored,omitempty"`
	Healthy    bool           `json:"healthy"`
	FixCommand string         `json:"fix_command"`
}

// installIgnoreState is machine-local suppression of install warnings.
// Stored next to the hooks it governs: ~/.grok/ljm-install-ignore.json
type installIgnoreState struct {
	IgnoreAll bool     `json:"ignore_all"`
	Codes     []string `json:"codes"`
	Updated   string   `json:"updated,omitempty"`
	Note      string   `json:"note,omitempty"`
}

func grokHomeDir() string {
	if h := os.Getenv("GROK_HOME"); h != "" {
		return h
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".grok")
}

func claudeSettingsPath() string {
	if p := os.Getenv("CLAUDE_SETTINGS_PATH"); p != "" {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude", "settings.json")
}

func installIgnorePath() string {
	gh := grokHomeDir()
	if gh == "" {
		return ""
	}
	return filepath.Join(gh, "ljm-install-ignore.json")
}

func loadInstallIgnore() installIgnoreState {
	path := installIgnorePath()
	if path == "" {
		return installIgnoreState{}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return installIgnoreState{}
	}
	var st installIgnoreState
	if err := json.Unmarshal(data, &st); err != nil {
		return installIgnoreState{}
	}
	return st
}

func saveInstallIgnore(st installIgnoreState) error {
	path := installIgnorePath()
	if path == "" {
		return fmt.Errorf("cannot resolve ~/.grok for ignore state")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	st.Updated = time.Now().Format(time.RFC3339)
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

// installPlatform returns a coarse OS family used for runner selection.
// windows | linux | darwin | other
func installPlatform() string {
	switch runtime.GOOS {
	case "windows":
		return "windows"
	case "linux":
		return "linux"
	case "darwin":
		return "darwin"
	default:
		return runtime.GOOS
	}
}

func isUnixLikeInstall() bool {
	p := installPlatform()
	return p == "linux" || p == "darwin"
}

// expectedHookRunner returns the platform-correct command prefix (without event).
// Unix: absolute path to grok/bin/run-hook
// Windows: powershell invocation of run-hook.ps1
func expectedHookRunner(vaultRoot string) string {
	vault := filepath.ToSlash(vaultRoot)
	if installPlatform() == "windows" {
		return fmt.Sprintf(`powershell -NoProfile -ExecutionPolicy Bypass -File "%s/grok/bin/run-hook.ps1"`, vault)
	}
	return filepath.ToSlash(filepath.Join(vaultRoot, "grok", "bin", "run-hook"))
}

func nativeJMCandidates(vaultRoot string) []string {
	if installPlatform() == "windows" {
		return []string{
			filepath.Join(vaultRoot, "agent", "jm.exe"),
			filepath.Join(vaultRoot, "jm.exe"),
			filepath.Join(vaultRoot, "agent", "jm"),
			filepath.Join(vaultRoot, "jm"),
		}
	}
	return []string{
		filepath.Join(vaultRoot, "agent", "jm"),
		filepath.Join(vaultRoot, "jm"),
		filepath.Join(vaultRoot, "agent", "jm.exe"),
		filepath.Join(vaultRoot, "jm.exe"),
	}
}

func findNativeJM(vaultRoot string) (string, bool) {
	for _, c := range nativeJMCandidates(vaultRoot) {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			// On Unix, prefer non-.exe when both exist — already ordered.
			if isUnixLikeInstall() && strings.HasSuffix(strings.ToLower(c), ".exe") {
				// Accept only if no native candidate exists (checked earlier).
				continue
			}
			if installPlatform() == "windows" && !strings.HasSuffix(strings.ToLower(c), ".exe") {
				// Prefer .exe; non-exe may be a bash script — skip as primary.
				continue
			}
			return c, true
		}
	}
	// Fallback: any candidate that exists (even cross-platform binary).
	for _, c := range nativeJMCandidates(vaultRoot) {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c, true
		}
	}
	return "", false
}

func pathsEqualLoose(a, b string) bool {
	a = filepath.Clean(filepath.FromSlash(strings.TrimSpace(a)))
	b = filepath.Clean(filepath.FromSlash(strings.TrimSpace(b)))
	if a == b {
		return true
	}
	// Case-insensitive compare on Windows.
	if installPlatform() == "windows" {
		return strings.EqualFold(a, b)
	}
	// Also tolerate trailing slash / slash style.
	return filepath.ToSlash(a) == filepath.ToSlash(b)
}

// extractHookCommands pulls command strings from a Grok/Claude-style hooks JSON blob.
func extractHookCommands(raw []byte) []string {
	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil
	}
	hooks, _ := root["hooks"].(map[string]any)
	if hooks == nil {
		return nil
	}
	var cmds []string
	for _, ev := range hooks {
		arr, ok := ev.([]any)
		if !ok {
			continue
		}
		for _, matcher := range arr {
			m, ok := matcher.(map[string]any)
			if !ok {
				continue
			}
			inner, _ := m["hooks"].([]any)
			for _, h := range inner {
				hm, ok := h.(map[string]any)
				if !ok {
					continue
				}
				if c, ok := hm["command"].(string); ok && c != "" {
					cmds = append(cmds, c)
				}
				if env, ok := hm["env"].(map[string]any); ok {
					if v, ok := env["JM_VAULT_ROOT"].(string); ok && v != "" {
						// Encode vault as a pseudo-command for mismatch checks.
						cmds = append(cmds, "JM_VAULT_ROOT="+v)
					}
				}
			}
		}
	}
	return cmds
}

func commandLooksPowerShell(cmd string) bool {
	lower := strings.ToLower(cmd)
	return strings.Contains(lower, "powershell") ||
		strings.Contains(lower, "run-hook.ps1") ||
		strings.Contains(lower, "pwsh ")
}

func commandLooksUnsubstituted(cmd string) bool {
	return strings.Contains(cmd, "__HOOK_RUNNER__") ||
		strings.Contains(cmd, "__JM_VAULT_ROOT__")
}

func commandRunnerExists(cmd string) bool {
	// Extract a likely filesystem path from the command.
	// Patterns:
	//   /path/to/run-hook event
	//   powershell ... -File "/path/to/run-hook.ps1" event
	lower := strings.ToLower(cmd)
	if idx := strings.Index(lower, "-file"); idx >= 0 {
		rest := strings.TrimSpace(cmd[idx+5:])
		rest = strings.TrimLeft(rest, " ")
		path := takeQuotedOrToken(rest)
		if path != "" {
			if _, err := os.Stat(filepath.FromSlash(path)); err == nil {
				return true
			}
			return false
		}
	}
	// First token that looks like a path
	tok := takeQuotedOrToken(strings.TrimSpace(cmd))
	if tok == "" {
		return true // can't decide — don't false-positive
	}
	if strings.Contains(tok, "/") || strings.Contains(tok, `\`) {
		if _, err := os.Stat(filepath.FromSlash(tok)); err == nil {
			return true
		}
		return false
	}
	return true
}

func takeQuotedOrToken(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if s[0] == '"' {
		end := strings.IndexByte(s[1:], '"')
		if end < 0 {
			return strings.Trim(s, `"`)
		}
		return s[1 : end+1]
	}
	if s[0] == '\'' {
		end := strings.IndexByte(s[1:], '\'')
		if end < 0 {
			return strings.Trim(s, `'`)
		}
		return s[1 : end+1]
	}
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

// CheckInstallHealth examines Grok (and Claude, when relevant) hook installation
// for this vault + platform. Failures never panic — missing paths become issues.
func CheckInstallHealth(vaultRoot string) InstallHealthReport {
	vaultRoot = filepath.Clean(vaultRoot)
	gh := grokHomeDir()
	hookFile := ""
	if gh != "" {
		hookFile = filepath.Join(gh, "hooks", "ljm.json")
	}

	rep := InstallHealthReport{
		Platform:   installPlatform(),
		VaultRoot:  vaultRoot,
		GrokHome:   gh,
		HookFile:   hookFile,
		FixCommand: "jm install fix",
	}

	var issues []InstallIssue

	// --- Grok global hooks ---
	if hookFile == "" {
		issues = append(issues, InstallIssue{
			Code:     IssueGrokHooksMissing,
			Severity: "error",
			Summary:  "Cannot resolve home directory for ~/.grok/hooks/ljm.json",
			FixHint:  "Set HOME/USERPROFILE, then run: jm install fix",
		})
	} else if st, err := os.Stat(hookFile); err != nil || st.IsDir() {
		issues = append(issues, InstallIssue{
			Code:     IssueGrokHooksMissing,
			Severity: "error",
			Summary:  "Global Grok hooks not installed",
			Detail:   "Expected ~/.grok/hooks/ljm.json (expanded from grok/hooks/ljm.template.json)",
			Path:     hookFile,
			FixHint:  platformInstallHint(vaultRoot),
		})
	} else {
		raw, err := os.ReadFile(hookFile)
		if err != nil {
			issues = append(issues, InstallIssue{
				Code:     IssueGrokHooksMissing,
				Severity: "error",
				Summary:  "Cannot read global Grok hooks file",
				Path:     hookFile,
				Detail:   err.Error(),
				FixHint:  platformInstallHint(vaultRoot),
			})
		} else {
			content := string(raw)
			if strings.Contains(content, "__HOOK_RUNNER__") || strings.Contains(content, "__JM_VAULT_ROOT__") {
				issues = append(issues, InstallIssue{
					Code:     IssueGrokHooksUnsubstituted,
					Severity: "error",
					Summary:  "Global hooks still contain template placeholders",
					Detail:   "File looks like an unexpanded install template, not a platform install",
					Path:     hookFile,
					FixHint:  "jm install fix",
				})
			}
			cmds := extractHookCommands(raw)
			var vaultFromEnv string
			for _, cmd := range cmds {
				if strings.HasPrefix(cmd, "JM_VAULT_ROOT=") {
					vaultFromEnv = strings.TrimPrefix(cmd, "JM_VAULT_ROOT=")
					continue
				}
				if commandLooksUnsubstituted(cmd) {
					// covered by unsubstituted issue above
					continue
				}
				if isUnixLikeInstall() && commandLooksPowerShell(cmd) {
					issues = append(issues, InstallIssue{
						Code:     IssueGrokHooksWrongPlatform,
						Severity: "error",
						Summary:  "Global hooks use Windows PowerShell runner on a Unix host",
						Detail:   truncateForIssue(cmd, 200),
						Path:     hookFile,
						FixHint:  "jm install fix  # rewrites hooks for " + installPlatform(),
					})
					break
				}
				if installPlatform() == "windows" {
					// Windows should prefer powershell/run-hook.ps1; bare bash run-hook may work under Git Bash but is fragile for native Grok.
					if !commandLooksPowerShell(cmd) && strings.Contains(cmd, "run-hook") && !strings.Contains(strings.ToLower(cmd), "run-hook.ps1") {
						issues = append(issues, InstallIssue{
							Code:     IssueGrokHooksWrongPlatform,
							Severity: "warn",
							Summary:  "Global hooks do not use the native Windows PowerShell runner",
							Detail:   truncateForIssue(cmd, 200),
							Path:     hookFile,
							FixHint:  "jm install fix  # or .\\grok\\install.ps1",
						})
						break
					}
				}
				if !commandRunnerExists(cmd) {
					issues = append(issues, InstallIssue{
						Code:     IssueGrokHooksRunnerMissing,
						Severity: "error",
						Summary:  "Hook runner path does not exist on this machine",
						Detail:   truncateForIssue(cmd, 200),
						Path:     hookFile,
						FixHint:  "jm install fix  # or re-run installer with correct --vault-root",
					})
					break
				}
			}
			if vaultFromEnv != "" && !pathsEqualLoose(vaultFromEnv, vaultRoot) {
				// Only flag if env vault does not exist OR differs and current process vault is the live one.
				if _, err := os.Stat(filepath.Join(filepath.FromSlash(vaultFromEnv), "System")); err != nil {
					issues = append(issues, InstallIssue{
						Code:     IssueGrokHooksVaultMismatch,
						Severity: "error",
						Summary:  "Hooks JM_VAULT_ROOT points to a missing/invalid vault",
						Detail:   fmt.Sprintf("hooks=%s  live=%s", vaultFromEnv, vaultRoot),
						Path:     hookFile,
						FixHint:  "jm install fix",
					})
				} else if !pathsEqualLoose(vaultFromEnv, vaultRoot) {
					issues = append(issues, InstallIssue{
						Code:     IssueGrokHooksVaultMismatch,
						Severity: "warn",
						Summary:  "Hooks JM_VAULT_ROOT differs from this process vault root",
						Detail:   fmt.Sprintf("hooks=%s  live=%s", vaultFromEnv, vaultRoot),
						Path:     hookFile,
						FixHint:  "jm install fix  # if this vault should own the global hooks",
					})
				}
			}
		}
	}

	// --- Project-scoped ljm.json (must not exist) ---
	projectHook := filepath.Join(vaultRoot, ".grok", "hooks", "ljm.json")
	if st, err := os.Stat(projectHook); err == nil && !st.IsDir() {
		issues = append(issues, InstallIssue{
			Code:     IssueProjectLJMHooks,
			Severity: "error",
			Summary:  "Project-level .grok/hooks/ljm.json present (double-injects; breaks cross-platform)",
			Detail:   "LJM hooks are global-only. Grok merges project + global hooks.",
			Path:     projectHook,
			FixHint:  "jm install fix  # removes project ljm.json; keep .grok/hooks/README.md",
		})
	}

	// --- Native jm binary ---
	if path, ok := findNativeJM(vaultRoot); !ok {
		build := "cd agent && go build -o jm ."
		if installPlatform() == "windows" {
			build = "cd agent && go build -o jm.exe ."
		}
		issues = append(issues, InstallIssue{
			Code:     IssueJMBinaryMissing,
			Severity: "error",
			Summary:  "Native jm binary not found under vault",
			Detail:   "Expected agent/jm (Unix) or agent/jm.exe (Windows)",
			Path:     filepath.Join(vaultRoot, "agent"),
			FixHint:  build,
		})
	} else if isUnixLikeInstall() && strings.HasSuffix(strings.ToLower(path), ".exe") {
		issues = append(issues, InstallIssue{
			Code:     IssueJMBinaryMissing,
			Severity: "warn",
			Summary:  "Only jm.exe found on Unix host — build native agent/jm",
			Path:     path,
			FixHint:  "cd agent && go build -o jm .",
		})
	}

	// --- Claude settings (when file exists and mentions LJM) ---
	if cs := claudeSettingsPath(); cs != "" {
		if raw, err := os.ReadFile(cs); err == nil {
			text := string(raw)
			if strings.Contains(text, "jm hook") || strings.Contains(text, "run-hook") || strings.Contains(text, "JM_VAULT_ROOT") {
				if strings.Contains(text, "__JM_VAULT_ROOT__") || strings.Contains(text, "__HOOK_RUNNER__") {
					issues = append(issues, InstallIssue{
						Code:     IssueClaudeHooksUnsubstituted,
						Severity: "error",
						Summary:  "Claude settings.json contains unsubstituted LJM template placeholders",
						Path:     cs,
						FixHint:  "Update ~/.claude/settings.json with platform-correct paths, or ask user",
					})
				}
				if isUnixLikeInstall() && (strings.Contains(strings.ToLower(text), "powershell") || strings.Contains(strings.ToLower(text), "run-hook.ps1")) {
					issues = append(issues, InstallIssue{
						Code:     IssueClaudeHooksWrongPlatform,
						Severity: "warn",
						Summary:  "Claude settings.json appears to use Windows PowerShell LJM runners on Unix",
						Path:     cs,
						FixHint:  "Edit ~/.claude/settings.json to call native jm/run-hook — not auto-fixed (Claude config is user-owned)",
					})
				}
			}
		}
	}

	// Deduplicate by code (keep first).
	seen := map[string]bool{}
	var deduped []InstallIssue
	for _, iss := range issues {
		if seen[iss.Code] {
			continue
		}
		seen[iss.Code] = true
		deduped = append(deduped, iss)
	}
	issues = deduped

	// Apply ignore state.
	ign := loadInstallIgnore()
	var active []InstallIssue
	var ignoredCodes []string
	if ign.IgnoreAll {
		for _, iss := range issues {
			ignoredCodes = append(ignoredCodes, iss.Code)
		}
		issues = nil
	} else if len(ign.Codes) > 0 {
		ignoreSet := map[string]bool{}
		for _, c := range ign.Codes {
			ignoreSet[c] = true
		}
		for _, iss := range issues {
			if ignoreSet[iss.Code] {
				ignoredCodes = append(ignoredCodes, iss.Code)
				continue
			}
			active = append(active, iss)
		}
		issues = active
	}

	rep.Issues = issues
	rep.Ignored = ignoredCodes
	rep.Healthy = len(issues) == 0
	return rep
}

func platformInstallHint(vaultRoot string) string {
	if installPlatform() == "windows" {
		return fmt.Sprintf(`.\grok\install.ps1 -VaultRoot "%s"  # or: jm install fix`, vaultRoot)
	}
	return fmt.Sprintf(`./grok/install.sh --vault-root %s  # or: jm install fix`, vaultRoot)
}

func truncateForIssue(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// writeInstallHealthWarning emits the install-health block. Mirrors repo-trust style:
// detect only — never auto-remediate. Agent must ask the user before fix or ignore.
func writeInstallHealthWarning(w io.Writer, rep InstallHealthReport) {
	fmt.Fprintln(w, "<ljm-install-warning>")
	fmt.Fprintln(w, "LJM INSTALL HEALTH — incorrect or incomplete setup detected")
	fmt.Fprintln(w)
	fmt.Fprintf(w, "Platform:   %s\n", rep.Platform)
	fmt.Fprintf(w, "Vault:      %s\n", rep.VaultRoot)
	if rep.HookFile != "" {
		fmt.Fprintf(w, "Grok hooks: %s\n", rep.HookFile)
	}
	fmt.Fprintln(w)
	fmt.Fprintf(w, "Issues (%d):\n", len(rep.Issues))
	for i, iss := range rep.Issues {
		fmt.Fprintf(w, "  %d. [%s] %s — %s\n", i+1, iss.Severity, iss.Code, iss.Summary)
		if iss.Detail != "" {
			fmt.Fprintf(w, "     detail: %s\n", iss.Detail)
		}
		if iss.Path != "" {
			fmt.Fprintf(w, "     path:   %s\n", iss.Path)
		}
		if iss.FixHint != "" {
			fmt.Fprintf(w, "     fix:    %s\n", iss.FixHint)
		}
	}
	if len(rep.Ignored) > 0 {
		fmt.Fprintf(w, "\nIgnored (user-suppressed): %s\n", strings.Join(rep.Ignored, ", "))
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "INSTRUCTION: Immediately notify the user of these install issues.")
	fmt.Fprintln(w, "Ask permission before remediating. Do NOT silently rewrite hooks or")
	fmt.Fprintln(w, "delete project files.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "If the user approves a fix:")
	fmt.Fprintln(w, "  jm install fix")
	fmt.Fprintln(w, "  (rewrites ~/.grok/hooks/ljm.json for this platform + vault;")
	fmt.Fprintln(w, "   removes project .grok/hooks/ljm.json if present)")
	fmt.Fprintln(w, "  Then ask them to press r in /hooks or restart the session.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "If the user wants to ignore (stop nagging):")
	fmt.Fprintln(w, "  jm install ignore <code> [<code>...]   # specific issue codes")
	fmt.Fprintln(w, "  jm install ignore --all                # suppress all install warnings")
	fmt.Fprintln(w, "  jm install ignore --clear              # re-enable warnings")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Inspect without changing: jm install check")
	fmt.Fprintln(w, "</ljm-install-warning>")
}

// FixInstallHealth rewrites global Grok hooks for the current platform and vault,
// and removes a project-level ljm.json if present. Does not touch Claude settings
// (user-owned) or build the binary unless requested.
type FixInstallOptions struct {
	RemoveProjectHooks bool
	// BuildBinary is reserved; currently unused (install scripts build).
	BuildBinary bool
}

// FixInstallHealth applies the platform-correct hook install for this vault.
func FixInstallHealth(vaultRoot string, opts FixInstallOptions) error {
	vaultRoot = filepath.Clean(vaultRoot)
	templatePath := filepath.Join(vaultRoot, "grok", "hooks", "ljm.template.json")
	raw, err := os.ReadFile(templatePath)
	if err != nil {
		return fmt.Errorf("read hook template: %w (expected %s)", err, templatePath)
	}
	runner := expectedHookRunner(vaultRoot)
	vaultSlash := filepath.ToSlash(vaultRoot)
	content := string(raw)
	content = strings.ReplaceAll(content, "__HOOK_RUNNER__", runner)
	content = strings.ReplaceAll(content, "__JM_VAULT_ROOT__", vaultSlash)

	gh := grokHomeDir()
	if gh == "" {
		return fmt.Errorf("cannot resolve ~/.grok")
	}
	destDir := filepath.Join(gh, "hooks")
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return err
	}
	dest := filepath.Join(destDir, "ljm.json")
	if err := os.WriteFile(dest, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", dest, err)
	}

	// Ensure Unix runner is executable when present.
	if isUnixLikeInstall() {
		runHook := filepath.Join(vaultRoot, "grok", "bin", "run-hook")
		_ = os.Chmod(runHook, 0o755)
		_ = os.Chmod(filepath.Join(vaultRoot, "grok", "bin", "run-hook.sh"), 0o755)
	}

	if opts.RemoveProjectHooks {
		projectHook := filepath.Join(vaultRoot, ".grok", "hooks", "ljm.json")
		if st, err := os.Stat(projectHook); err == nil && !st.IsDir() {
			if err := os.Remove(projectHook); err != nil {
				return fmt.Errorf("remove project ljm.json: %w", err)
			}
		}
	}

	return nil
}

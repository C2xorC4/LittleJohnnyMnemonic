package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Dashboard refresh reasons. Consolidation and compact bypass the cooldown
// because they are infrequent and always change dashboard-visible state.
const (
	dashReasonConsolidation = "consolidation"
	dashReasonCompact       = "compact"
	dashReasonAutodream     = "autodream"
	dashReasonKnowledge     = "knowledge"
)

type bufferSnapshot struct {
	Count     int `json:"count"`
	Threshold int `json:"threshold"`
}

type dashboardRefreshState struct {
	LastRefresh time.Time `json:"last_refresh"`
	Reason      string    `json:"reason"`
}

func dashboardRefreshStatePath(vaultRoot string) string {
	return filepath.Join(vaultRoot, "Metrics", "dashboard_refresh_state.json")
}

func dashboardBypassesCooldown(reason string) bool {
	return reason == dashReasonConsolidation || reason == dashReasonCompact
}

// WriteMetricsDashboard builds the payload and writes Metrics/dashboard.html.
func WriteMetricsDashboard(vaultRoot, outputPath string) error {
	payload, err := buildDashboardPayload(vaultRoot)
	if err != nil {
		return err
	}
	payload.Meta["vault_root"] = vaultRoot
	payload.Meta["generated_at"] = time.Now().UTC().Format(time.RFC3339)

	if outputPath == "" {
		outputPath = filepath.Join(vaultRoot, "Metrics", "dashboard.html")
	}

	title := "LJM Memory Health — " + filepath.Base(vaultRoot)
	rendered, err := renderDashboardHTML(payload, title, filepath.Base(vaultRoot), "static")
	if err != nil {
		return fmt.Errorf("render: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	if err := writeFileAtomic(outputPath, rendered, 0644); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	return nil
}

// writeFileAtomic writes path via a same-directory temp file and renames into
// place so readers (browsers with the file open) never see a truncated file.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".dashboard-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	cleanup = false
	return nil
}

// MaybeRefreshDashboard regenerates Metrics/dashboard.html when enabled.
// Fail-soft: errors are logged to stderr and never returned to callers.
func MaybeRefreshDashboard(vaultRoot, reason string) {
	cfg := LoadConfig(vaultRoot)
	if !cfg.DashboardAutoRefreshEnabled {
		return
	}
	if vaultRoot == "" {
		return
	}
	if !dashboardBypassesCooldown(reason) && dashboardRefreshOnCooldown(vaultRoot, cfg.DashboardRefreshCooldownMinutes) {
		return
	}
	if err := WriteMetricsDashboard(vaultRoot, ""); err != nil {
		fmt.Fprintf(os.Stderr, "[jm] dashboard refresh (%s): %v\n", reason, err)
		return
	}
	recordDashboardRefresh(vaultRoot, reason)
}

func dashboardRefreshOnCooldown(vaultRoot string, cooldownMinutes int) bool {
	if cooldownMinutes <= 0 {
		return false
	}
	data, err := os.ReadFile(dashboardRefreshStatePath(vaultRoot))
	if err != nil {
		return false
	}
	var state dashboardRefreshState
	if json.Unmarshal(data, &state) != nil || state.LastRefresh.IsZero() {
		return false
	}
	return time.Since(state.LastRefresh) < time.Duration(cooldownMinutes)*time.Minute
}

func recordDashboardRefresh(vaultRoot, reason string) {
	state := dashboardRefreshState{
		LastRefresh: time.Now().UTC(),
		Reason:      reason,
	}
	data, err := json.Marshal(state)
	if err != nil {
		return
	}
	path := dashboardRefreshStatePath(vaultRoot)
	_ = os.MkdirAll(filepath.Dir(path), 0755)
	_ = os.WriteFile(path, data, 0644)
}

// deriveVaultRootFromPath extracts the vault root from an absolute memory/buffer path.
func deriveVaultRootFromPath(filePath string) string {
	p := filepath.ToSlash(filePath)
	for _, marker := range []string{"/Memory/", "/Buffer/", "/Metrics/", "/Archive/"} {
		if i := strings.Index(p, marker); i > 0 {
			return filepath.FromSlash(p[:i])
		}
	}
	return ""
}

// maybeRefreshDashboardForKnowledge triggers a cooldown-gated refresh after
// knowledge LTM writes (ingestion, promotion, provenance recovery).
func autodreamCountsForDashboard(decision string) bool {
	return !strings.Contains(decision, "dry-run") && !strings.Contains(decision, "skip")
}

func maybeRefreshDashboardForKnowledge(entry *MemoryEntry) {
	if entry == nil || entry.Type != TypeKnowledge {
		return
	}
	vaultRoot := deriveVaultRootFromPath(entry.FilePath)
	if vaultRoot == "" {
		return
	}
	MaybeRefreshDashboard(vaultRoot, dashReasonKnowledge)
}
package main

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

//go:embed templates/dashboard.html
var dashboardHTMLTemplate string

//go:embed templates/dashboard.css
var dashboardCSS string

//go:embed templates/dashboard.js
var dashboardJS string

// dashboardPayload is the JSON blob injected into the dashboard HTML.
type dashboardPayload struct {
	Meta          map[string]any    `json:"meta"`
	RecallByDay   []dayRecallPoint  `json:"recall_by_day"`
	PromotesByDay []dayCountPoint   `json:"promotes_by_day"`
	VaultDepth    []vaultDepthPoint `json:"vault_depth"`
	DaydreamByDay []dayCountPoint   `json:"daydream_by_day"`
	GraphRelPath  string            `json:"graph_rel_path"`
}

type dayRecallPoint struct {
	Date         string         `json:"date"`
	Total        int            `json:"total"`
	Prompts      int            `json:"prompts"`
	AvgRecall    float64        `json:"avg_recall"`
	AvgBodyHits  float64        `json:"avg_body_hits"`
	AvgRelevance float64        `json:"avg_relevance"`
	Counts       map[string]int `json:"counts"`
}

type dayCountPoint struct {
	Date  string `json:"date"`
	Count int    `json:"count"`
}

type vaultDepthPoint struct {
	Date   string         `json:"date"`
	Total  int            `json:"total"`
	ByType map[string]int `json:"by_type"`
}

func cmdMetricsDashboard(vaultRoot string, args []string) {
	fs := flag.NewFlagSet("metrics dashboard", flag.ExitOnError)
	output := fs.String("output", "", "output path (default: <vault>/Metrics/dashboard.html)")
	openFlag := fs.Bool("open", false, "open the HTML in the default browser after writing")
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "Usage: jm metrics dashboard [flags]")
		fmt.Fprintln(fs.Output(), "")
		fmt.Fprintln(fs.Output(), "Generate an interactive metrics dashboard from the vault's JSONL log files.")
		fmt.Fprintln(fs.Output(), "Output is a single self-contained HTML file. Re-run to refresh.")
		fmt.Fprintln(fs.Output(), "")
		fmt.Fprintln(fs.Output(), "Flags:")
		fs.PrintDefaults()
	}
	_ = fs.Parse(args)

	metricsDir := filepath.Join(vaultRoot, "Metrics")

	payload, err := buildDashboardPayload(metricsDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "metrics dashboard: %v\n", err)
		os.Exit(1)
	}
	payload.Meta["vault_root"] = vaultRoot
	payload.Meta["generated_at"] = time.Now().UTC().Format(time.RFC3339)

	outputPath := *output
	if outputPath == "" {
		outputPath = filepath.Join(metricsDir, "dashboard.html")
	}

	title := "LJM Metrics — " + filepath.Base(vaultRoot)
	rendered, err := renderDashboardHTML(payload, title, "static")
	if err != nil {
		fmt.Fprintf(os.Stderr, "metrics dashboard: render: %v\n", err)
		os.Exit(1)
	}

	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		fmt.Fprintf(os.Stderr, "metrics dashboard: mkdir: %v\n", err)
		os.Exit(1)
	}
	if err := os.WriteFile(outputPath, rendered, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "metrics dashboard: write: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Wrote %s  (%d recall-days, %d depth-snapshots, %d bytes)\n",
		outputPath, len(payload.RecallByDay), len(payload.VaultDepth), len(rendered))

	if *openFlag {
		if err := openInBrowser(outputPath); err != nil {
			fmt.Fprintf(os.Stderr, "[!] Failed to open browser: %v\n", err)
		}
	}
}

func buildDashboardPayload(metricsDir string) (dashboardPayload, error) {
	p := dashboardPayload{
		Meta:         map[string]any{},
		GraphRelPath: "graph.html",
	}

	var err error

	p.RecallByDay, err = loadRecallByDay(filepath.Join(metricsDir, "recall_log.jsonl"))
	if err != nil {
		return p, fmt.Errorf("recall_log: %w", err)
	}

	p.PromotesByDay, err = loadPromotesByDay(filepath.Join(metricsDir, "consolidation_outcomes.jsonl"))
	if err != nil {
		return p, fmt.Errorf("consolidation_outcomes: %w", err)
	}

	p.VaultDepth, err = loadVaultDepth(filepath.Join(metricsDir, "autodream_activation_snapshots.jsonl"))
	if err != nil {
		return p, fmt.Errorf("autodream_activation_snapshots: %w", err)
	}

	p.DaydreamByDay, err = loadDaydreamByDay(metricsDir)
	if err != nil {
		return p, fmt.Errorf("autodream_log: %w", err)
	}

	return p, nil
}

// loadRecallByDay aggregates both granular and daily-aggregate entries from
// recall_log.jsonl into one dayRecallPoint per calendar day.
func loadRecallByDay(logPath string) ([]dayRecallPoint, error) {
	data, err := os.ReadFile(logPath)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	type accum struct {
		total            int
		prompts          int
		counts           map[string]int
		weightedBodyHits float64 // sum of (AvgBodyHits * Total) for weighted mean
		weightedRel      float64 // sum of (AvgRelevance * Total) for weighted mean
	}
	byDay := map[string]*accum{}

	getDay := func(date string) *accum {
		if byDay[date] == nil {
			byDay[date] = &accum{counts: map[string]int{}}
		}
		return byDay[date]
	}

	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var disc struct {
			Granularity string `json:"granularity"`
		}
		_ = json.Unmarshal([]byte(line), &disc)

		if disc.Granularity == "day" {
			var d recallDayEntry
			if json.Unmarshal([]byte(line), &d) != nil || d.Date == "" {
				continue
			}
			acc := getDay(d.Date)
			acc.total += d.TotalRecalls
			acc.prompts += d.Prompts
			acc.weightedBodyHits += d.AvgBodyHits * float64(d.TotalRecalls)
			acc.weightedRel += d.AvgRelevance * float64(d.TotalRecalls)
			for k, v := range d.Counts {
				acc.counts[k] += v
			}
		} else {
			var g recallLogEntry
			if json.Unmarshal([]byte(line), &g) != nil || g.Timestamp == "" {
				continue
			}
			t, err := time.Parse(time.RFC3339, g.Timestamp)
			if err != nil {
				continue
			}
			acc := getDay(t.UTC().Format("2006-01-02"))
			acc.total += g.Total
			acc.prompts++
			acc.weightedBodyHits += g.AvgBodyHits * float64(g.Total)
			acc.weightedRel += g.AvgRelevance * float64(g.Total)
			for k, v := range g.Counts {
				acc.counts[k] += v
			}
		}
	}

	dates := sortedStringKeys(byDay)
	result := make([]dayRecallPoint, len(dates))
	for i, d := range dates {
		acc := byDay[d]
		avgRecall, avgBodyHits, avgRelevance := 0.0, 0.0, 0.0
		if acc.prompts > 0 {
			avgRecall = float64(acc.total) / float64(acc.prompts)
		}
		if acc.total > 0 {
			avgBodyHits = acc.weightedBodyHits / float64(acc.total)
			avgRelevance = acc.weightedRel / float64(acc.total)
		}
		result[i] = dayRecallPoint{
			Date:         d,
			Total:        acc.total,
			Prompts:      acc.prompts,
			AvgRecall:    avgRecall,
			AvgBodyHits:  avgBodyHits,
			AvgRelevance: avgRelevance,
			Counts:       acc.counts,
		}
	}
	return result, nil
}

// loadPromotesByDay counts action=="promote" entries per calendar day from
// consolidation_outcomes.jsonl.
func loadPromotesByDay(logPath string) ([]dayCountPoint, error) {
	data, err := os.ReadFile(logPath)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	byDay := map[string]int{}

	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var entry struct {
			Timestamp string `json:"timestamp"`
			Action    string `json:"action"`
		}
		if json.Unmarshal([]byte(line), &entry) != nil || entry.Timestamp == "" {
			continue
		}
		if entry.Action != "promote" {
			continue
		}
		t, err := time.Parse(time.RFC3339, entry.Timestamp)
		if err != nil {
			// Try the longer format with sub-second precision.
			t, err = time.Parse("2006-01-02T15:04:05.9999999-07:00", entry.Timestamp)
			if err != nil {
				continue
			}
		}
		byDay[t.UTC().Format("2006-01-02")]++
	}

	return dayCountSlice(byDay), nil
}

// loadVaultDepth reads autodream_activation_snapshots.jsonl and returns the
// latest snapshot per calendar day, sorted ascending.
func loadVaultDepth(logPath string) ([]vaultDepthPoint, error) {
	data, err := os.ReadFile(logPath)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	type snapshot struct {
		Timestamp string         `json:"timestamp"`
		Total     int            `json:"total_memories"`
		ByType    map[string]int `json:"by_type"`
	}

	// Keep last snapshot seen per day (latest wins since file is append-only).
	latestPerDay := map[string]snapshot{}

	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var s snapshot
		if json.Unmarshal([]byte(line), &s) != nil || s.Timestamp == "" {
			continue
		}
		t, err := time.Parse(time.RFC3339, s.Timestamp)
		if err != nil {
			t, err = time.Parse("2006-01-02T15:04:05.9999999-07:00", s.Timestamp)
			if err != nil {
				continue
			}
		}
		date := t.UTC().Format("2006-01-02")
		latestPerDay[date] = s
	}

	dates := sortedStringKeys(latestPerDay)
	result := make([]vaultDepthPoint, len(dates))
	for i, d := range dates {
		s := latestPerDay[d]
		result[i] = vaultDepthPoint{
			Date:   d,
			Total:  s.Total,
			ByType: s.ByType,
		}
	}
	return result, nil
}

// loadDaydreamByDay counts daydream firing events per day from all
// autodream_log JSONL files in metricsDir (live + Archive/*.jsonl).
// Dry-run and skip entries are excluded; only real fires count.
func loadDaydreamByDay(metricsDir string) ([]dayCountPoint, error) {
	byDay := map[string]int{}

	for _, path := range collectAutodreamLogPaths(metricsDir) {
		data, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
			if line == "" {
				continue
			}
			var entry struct {
				Timestamp string `json:"timestamp"`
				Decision  string `json:"decision"`
			}
			if json.Unmarshal([]byte(line), &entry) != nil || entry.Timestamp == "" {
				continue
			}
			if strings.Contains(entry.Decision, "dry-run") || strings.Contains(entry.Decision, "skip") {
				continue
			}
			t, err := time.Parse(time.RFC3339, entry.Timestamp)
			if err != nil {
				t, err = time.Parse("2006-01-02T15:04:05.9999999-07:00", entry.Timestamp)
				if err != nil {
					continue
				}
			}
			byDay[t.UTC().Format("2006-01-02")]++
		}
	}

	return dayCountSlice(byDay), nil
}

// renderDashboardHTML renders the dashboard HTML template.
// mode is "static" (embedded data only) or "live" (embedded initial data + SSE subscription).
func renderDashboardHTML(payload dashboardPayload, title, mode string) ([]byte, error) {
	jsonBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal payload: %w", err)
	}

	tmpl, err := template.New("dashboard").Parse(dashboardHTMLTemplate)
	if err != nil {
		return nil, fmt.Errorf("parse template: %w", err)
	}

	type tplData struct {
		Title string
		Mode  string
		CSS   template.CSS
		App   template.JS
		Data  template.JS
	}

	var buf bytes.Buffer
	err = tmpl.Execute(&buf, tplData{
		Title: title,
		Mode:  mode,
		CSS:   template.CSS(dashboardCSS),
		App:   template.JS(dashboardJS),
		Data:  template.JS(jsonBytes),
	})
	if err != nil {
		return nil, fmt.Errorf("execute template: %w", err)
	}
	return buf.Bytes(), nil
}

// sortedStringKeys returns the sorted keys of any map[string]T.
func sortedStringKeys[T any](m map[string]T) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// dayCountSlice converts map[date]count to a sorted []dayCountPoint.
func dayCountSlice(m map[string]int) []dayCountPoint {
	dates := sortedStringKeys(m)
	result := make([]dayCountPoint, len(dates))
	for i, d := range dates {
		result[i] = dayCountPoint{Date: d, Count: m[d]}
	}
	return result
}

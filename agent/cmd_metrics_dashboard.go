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
	Meta           map[string]any      `json:"meta"`
	RecallByDay    []dayRecallPoint    `json:"recall_by_day"`
	UsageByDay     []dayUsagePoint          `json:"usage_by_day"`
	UsageByModel   []modelUsagePoint        `json:"usage_by_model"`
	UsageSummary   usageTelemetrySummary    `json:"usage_summary"`
	PromotesByDay  []dayCountPoint     `json:"promotes_by_day"`
	VaultDepth     []vaultDepthPoint   `json:"vault_depth"`
	DaydreamByDay  []dayCountPoint     `json:"daydream_by_day"`
	Buffer         bufferSnapshot      `json:"buffer"`
	GraphRelPath   string              `json:"graph_rel_path"`
}

type dayRecallPoint struct {
	Date              string         `json:"date"`
	Total             int            `json:"total"`
	Prompts           int            `json:"prompts"`
	ZeroRecallPrompts int            `json:"zero_recall_prompts"`
	AvgRecall         float64        `json:"avg_recall"`
	AvgBodyHits       float64        `json:"avg_body_hits"`
	AvgRelevance      float64        `json:"avg_relevance"`
	Counts            map[string]int `json:"counts"`
}

type dayUsagePoint struct {
	Date              string  `json:"date"`
	Turns             int     `json:"turns"`
	LiveTurns         int     `json:"live_turns"`
	BackfillTurns     int     `json:"backfill_turns"`
	Injected          int     `json:"injected"`
	Referenced        int     `json:"referenced"`
	LiveInjected      int     `json:"live_injected"`
	LiveReferenced    int     `json:"live_referenced"`
	UsageRate         float64 `json:"usage_rate"`
	ZeroUsageTurns    int     `json:"zero_usage_turns"`
	PartialUsageTurns int     `json:"partial_usage_turns"`
}

type modelUsagePoint struct {
	Model              string  `json:"model"`
	RuntimeHost        string  `json:"runtime_host"`
	Turns              int     `json:"turns"`
	LiveTurns          int     `json:"live_turns"`
	BackfillTurns      int     `json:"backfill_turns"`
	Injected           int     `json:"injected"`
	Referenced         int     `json:"referenced"`
	LiveInjected       int     `json:"live_injected"`
	LiveReferenced     int     `json:"live_referenced"`
	UsageRate          float64 `json:"usage_rate"`
	LiveUsageRate      float64 `json:"live_usage_rate"`
	ZeroUsageTurns     int     `json:"zero_usage_turns"`
	LiveZeroUsageTurns int     `json:"live_zero_usage_turns"`
}

// usageTelemetrySummary describes coverage and provenance of memory_usage_log rows.
type usageTelemetrySummary struct {
	TotalTurns            int     `json:"total_turns"`
	LiveTurns             int     `json:"live_turns"`
	BackfillTurns         int     `json:"backfill_turns"`
	UsageCoverageFrom     string  `json:"usage_coverage_from,omitempty"`
	UsageCoverageTo       string  `json:"usage_coverage_to,omitempty"`
	InjectionOnlyBefore   string  `json:"injection_only_before,omitempty"`
	LiveUsageRate         float64 `json:"live_usage_rate"`
	BackfillUsageRate     float64 `json:"backfill_usage_rate"`
	OverallUsageRate      float64 `json:"overall_usage_rate"`
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
	openFlag := fs.Bool("open", false, "open the dashboard in the default browser after writing")
	serveFlag := fs.Bool("serve", false, "serve the dashboard over HTTP after writing (blocks; Ctrl+C to stop)")
	port := fs.Int("port", 8080, "HTTP port when --serve is set")
	pollInterval := fs.Duration("poll", 2*time.Second, "metrics file poll interval when --serve is set")
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "Usage: jm metrics dashboard [flags]")
		fmt.Fprintln(fs.Output(), "")
		fmt.Fprintln(fs.Output(), "Generate an interactive metrics dashboard from the vault's JSONL log files.")
		fmt.Fprintln(fs.Output(), "Writes Metrics/dashboard.html (atomic replace). Use --serve for a live HTTP view.")
		fmt.Fprintln(fs.Output(), "")
		fmt.Fprintln(fs.Output(), "Flags:")
		fs.PrintDefaults()
	}
	_ = fs.Parse(args)

	outputPath := *output
	if outputPath == "" {
		outputPath = filepath.Join(vaultRoot, "Metrics", "dashboard.html")
	}

	if err := WriteMetricsDashboard(vaultRoot, outputPath); err != nil {
		fmt.Fprintf(os.Stderr, "metrics dashboard: %v\n", err)
		os.Exit(1)
	}

	payload, _ := buildDashboardPayload(vaultRoot)
	info, _ := os.Stat(outputPath)
	size := int64(0)
	if info != nil {
		size = info.Size()
	}
	fmt.Printf("Wrote %s  (%d recall-days, %d depth-snapshots, %d bytes)\n",
		outputPath, len(payload.RecallByDay), len(payload.VaultDepth), size)

	if *openFlag {
		if *serveFlag {
			url := fmt.Sprintf("http://localhost:%d", *port)
			if err := openURL(url); err != nil {
				fmt.Fprintf(os.Stderr, "[!] Failed to open browser: %v\n", err)
			}
		} else if err := openInBrowser(outputPath); err != nil {
			fmt.Fprintf(os.Stderr, "[!] Failed to open browser: %v\n", err)
		}
	}

	if *serveFlag {
		if err := runMetricsServer(vaultRoot, *port, *pollInterval); err != nil {
			fmt.Fprintf(os.Stderr, "metrics dashboard --serve: %v\n", err)
			os.Exit(1)
		}
	} else if *openFlag {
		fmt.Println("Tip: use --serve to keep a live dashboard at http://localhost:8080")
	}
}

func buildDashboardPayload(vaultRoot string) (dashboardPayload, error) {
	metricsDir := filepath.Join(vaultRoot, "Metrics")
	cfg := LoadConfig(vaultRoot)

	p := dashboardPayload{
		Meta:         map[string]any{},
		GraphRelPath: "graph.html",
	}

	bufferEntries, _ := LoadAllBufferEntries(vaultRoot)
	p.Buffer = bufferSnapshot{
		Count:     len(bufferEntries),
		Threshold: cfg.BufferThreshold,
	}

	var err error

	p.RecallByDay, err = loadRecallByDay(filepath.Join(metricsDir, "recall_log.jsonl"))
	if err != nil {
		return p, fmt.Errorf("recall_log: %w", err)
	}

	p.UsageByDay, p.UsageByModel, p.UsageSummary, err = loadMemoryUsage(filepath.Join(metricsDir, "memory_usage_log.jsonl"))
	if err != nil {
		return p, fmt.Errorf("memory_usage_log: %w", err)
	}
	if len(p.RecallByDay) > 0 && p.UsageSummary.UsageCoverageFrom != "" {
		firstRecall := p.RecallByDay[0].Date
		if firstRecall < p.UsageSummary.UsageCoverageFrom {
			p.UsageSummary.InjectionOnlyBefore = p.UsageSummary.UsageCoverageFrom
		}
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
		zeroRecall       int
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
			acc.prompts++
			if g.ZeroRecall {
				acc.zeroRecall++
			} else {
				acc.total += g.Total
				acc.weightedBodyHits += g.AvgBodyHits * float64(g.Total)
				acc.weightedRel += g.AvgRelevance * float64(g.Total)
				for k, v := range g.Counts {
					acc.counts[k] += v
				}
			}
		}
	}

	dates := sortedStringKeys(byDay)
	result := make([]dayRecallPoint, len(dates))
	for i, d := range dates {
		acc := byDay[d]
		avgRecall, avgBodyHits, avgRelevance := 0.0, 0.0, 0.0
		recallPrompts := acc.prompts - acc.zeroRecall
		if recallPrompts > 0 {
			avgRecall = float64(acc.total) / float64(recallPrompts)
		}
		if acc.total > 0 {
			avgBodyHits = acc.weightedBodyHits / float64(acc.total)
			avgRelevance = acc.weightedRel / float64(acc.total)
		}
		result[i] = dayRecallPoint{
			Date:              d,
			Total:             acc.total,
			Prompts:           acc.prompts,
			ZeroRecallPrompts: acc.zeroRecall,
			AvgRecall:         avgRecall,
			AvgBodyHits:       avgBodyHits,
			AvgRelevance:      avgRelevance,
			Counts:            acc.counts,
		}
	}
	return result, nil
}

// loadMemoryUsage aggregates memory_usage_log.jsonl into daily and per-model series.
func loadMemoryUsage(logPath string) ([]dayUsagePoint, []modelUsagePoint, usageTelemetrySummary, error) {
	data, err := os.ReadFile(logPath)
	if os.IsNotExist(err) {
		return nil, nil, usageTelemetrySummary{}, nil
	}
	if err != nil {
		return nil, nil, usageTelemetrySummary{}, err
	}

	type dayAccum struct {
		turns, liveTurns, backfillTurns int
		injected, referenced, zeroUsage, partialUsage int
		liveInjected, liveReferenced, liveZeroUsage int
	}
	byDay := map[string]*dayAccum{}
	type modelKey struct {
		model, host string
	}
	byModel := map[modelKey]*dayAccum{}

	var summary usageTelemetrySummary
	var liveInjected, liveReferenced, backfillInjected, backfillReferenced int

	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var e memoryUsageLogEntry
		if json.Unmarshal([]byte(line), &e) != nil || e.Timestamp == "" {
			continue
		}
		if e.Outcome == "no_injection" {
			continue
		}
		t, err := time.Parse(time.RFC3339, e.Timestamp)
		if err != nil {
			t, err = time.Parse("2006-01-02T15:04:05Z", e.Timestamp)
			if err != nil {
				continue
			}
		}
		date := t.UTC().Format("2006-01-02")

		accDay := byDay[date]
		if accDay == nil {
			accDay = &dayAccum{}
			byDay[date] = accDay
		}
		accDay.turns++
		if e.Backfill {
			accDay.backfillTurns++
			summary.BackfillTurns++
			backfillInjected += e.MemoriesInjected
			backfillReferenced += e.MemoriesReferenced
		} else {
			accDay.liveTurns++
			summary.LiveTurns++
			liveInjected += e.MemoriesInjected
			liveReferenced += e.MemoriesReferenced
			accDay.liveInjected += e.MemoriesInjected
			accDay.liveReferenced += e.MemoriesReferenced
			if e.Outcome == "none" {
				accDay.liveZeroUsage++
			}
		}
		summary.TotalTurns++
		accDay.injected += e.MemoriesInjected
		accDay.referenced += e.MemoriesReferenced
		switch e.Outcome {
		case "none":
			accDay.zeroUsage++
		case "partial":
			accDay.partialUsage++
		}

		mk := modelKey{model: e.Model, host: e.RuntimeHost}
		accModel := byModel[mk]
		if accModel == nil {
			accModel = &dayAccum{}
			byModel[mk] = accModel
		}
		accModel.turns++
		if e.Backfill {
			accModel.backfillTurns++
		} else {
			accModel.liveTurns++
			accModel.liveInjected += e.MemoriesInjected
			accModel.liveReferenced += e.MemoriesReferenced
			if e.Outcome == "none" {
				accModel.liveZeroUsage++
			}
		}
		accModel.injected += e.MemoriesInjected
		accModel.referenced += e.MemoriesReferenced
		if e.Outcome == "none" {
			accModel.zeroUsage++
		}
	}

	dates := sortedStringKeys(byDay)
	dayResult := make([]dayUsagePoint, len(dates))
	for i, d := range dates {
		acc := byDay[d]
		rate := 0.0
		if acc.injected > 0 {
			rate = float64(acc.referenced) / float64(acc.injected)
		}
		dayResult[i] = dayUsagePoint{
			Date:              d,
			Turns:             acc.turns,
			LiveTurns:         acc.liveTurns,
			BackfillTurns:     acc.backfillTurns,
			Injected:          acc.injected,
			Referenced:        acc.referenced,
			LiveInjected:      acc.liveInjected,
			LiveReferenced:    acc.liveReferenced,
			UsageRate:         rate,
			ZeroUsageTurns:    acc.zeroUsage,
			PartialUsageTurns: acc.partialUsage,
		}
	}

	if len(dates) > 0 {
		summary.UsageCoverageFrom = dates[0]
		summary.UsageCoverageTo = dates[len(dates)-1]
	}
	if liveInjected > 0 {
		summary.LiveUsageRate = float64(liveReferenced) / float64(liveInjected)
	}
	if backfillInjected > 0 {
		summary.BackfillUsageRate = float64(backfillReferenced) / float64(backfillInjected)
	}
	if liveInjected+backfillInjected > 0 {
		summary.OverallUsageRate = float64(liveReferenced+backfillReferenced) / float64(liveInjected+backfillInjected)
	}

	type modelSort struct {
		key modelKey
		acc *dayAccum
	}
	var modelRows []modelSort
	for k, acc := range byModel {
		modelRows = append(modelRows, modelSort{key: k, acc: acc})
	}
	sort.Slice(modelRows, func(i, j int) bool {
		if modelRows[i].acc.turns != modelRows[j].acc.turns {
			return modelRows[i].acc.turns > modelRows[j].acc.turns
		}
		return modelRows[i].key.model < modelRows[j].key.model
	})

	modelResult := make([]modelUsagePoint, len(modelRows))
	for i, row := range modelRows {
		acc := row.acc
		rate := 0.0
		if acc.injected > 0 {
			rate = float64(acc.referenced) / float64(acc.injected)
		}
		liveRate := 0.0
		if acc.liveInjected > 0 {
			liveRate = float64(acc.liveReferenced) / float64(acc.liveInjected)
		}
		modelResult[i] = modelUsagePoint{
			Model:              row.key.model,
			RuntimeHost:        row.key.host,
			Turns:              acc.turns,
			LiveTurns:          acc.liveTurns,
			BackfillTurns:      acc.backfillTurns,
			Injected:           acc.injected,
			Referenced:         acc.referenced,
			LiveInjected:       acc.liveInjected,
			LiveReferenced:     acc.liveReferenced,
			UsageRate:          rate,
			LiveUsageRate:      liveRate,
			ZeroUsageTurns:     acc.zeroUsage,
			LiveZeroUsageTurns: acc.liveZeroUsage,
		}
	}

	return dayResult, modelResult, summary, nil
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
func renderDashboardHTML(payload dashboardPayload, title, vaultName, mode string) ([]byte, error) {
	jsonBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal payload: %w", err)
	}

	tmpl, err := template.New("dashboard").Parse(dashboardHTMLTemplate)
	if err != nil {
		return nil, fmt.Errorf("parse template: %w", err)
	}

	type tplData struct {
		Title     string
		VaultName string
		Mode      string
		CSS       template.CSS
		App       template.JS
		Data      template.JS
	}

	var buf bytes.Buffer
	err = tmpl.Execute(&buf, tplData{
		Title:     title,
		VaultName: vaultName,
		Mode:      mode,
		CSS:       template.CSS(dashboardCSS),
		App:       template.JS(dashboardJS),
		Data:      template.JS(jsonBytes),
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

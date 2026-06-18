package main

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

//go:embed templates/graph.html
var graphHTMLTemplate string

//go:embed templates/graph.css
var graphCSS string

//go:embed templates/graph.js
var graphJS string

//go:embed templates/vendor/vis-network.min.js
var graphVendor string

// graphNode is the JSON shape consumed by the front-end visualization.
type graphNode struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Path        string   `json:"path"`
	Type        string   `json:"type"`
	Tags        []string `json:"tags"`
	Activation  float64  `json:"activation"`
	Confidence  float64  `json:"confidence"`
	AccessCount int      `json:"access_count"`
	Degree      int      `json:"degree"`
	Importance  string   `json:"importance,omitempty"`
}

// graphEdge represents one connection in the rendered graph. Explicit links
// from frontmatter use Kind="explicit"; pairs surfaced from coactivation
// data use Kind="coactivation".
type graphEdge struct {
	Source       string  `json:"source"`
	Target       string  `json:"target"`
	Relationship string  `json:"relationship"`
	Weight       float64 `json:"weight"`
	Kind         string  `json:"kind"`
	Directed     bool    `json:"directed"`
}

type graphPayload struct {
	Meta  map[string]any `json:"meta"`
	Nodes []graphNode    `json:"nodes"`
	Edges []graphEdge    `json:"edges"`
}

type graphOpts struct {
	IncludeTypes          []string
	IncludeCoactivation   bool
	CoactivationThreshold int
	MinActivation         float64
}

func cmdGraph(vaultRoot string, args []string) {
	fs := flag.NewFlagSet("graph", flag.ExitOnError)
	output := fs.String("output", "", "output path (default: <vault>/Metrics/graph.html)")
	includeTypes := fs.String("include-types", "", "comma-separated allowlist of memory types (empty = all)")
	noCoactivation := fs.Bool("no-coactivation", false, "disable the coactivation edge layer (default: enabled)")
	coactThreshold := fs.Int("coactivation-threshold", 3, "minimum coactivation count to draw an overlay edge")
	minActivation := fs.Float64("min-activation", -10.0, "skip nodes below this activation")
	title := fs.String("title", "", "page title (default: derived from vault directory name)")
	format := fs.String("format", "html", "output format: html | json")
	openFlag := fs.Bool("open", false, "open the HTML in the default browser after writing")
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "Usage: jm graph [flags]")
		fmt.Fprintln(fs.Output(), "")
		fmt.Fprintln(fs.Output(), "Export an interactive HTML visualization of the memory graph.")
		fmt.Fprintln(fs.Output(), "Output is a single self-contained file. No network access required.")
		fmt.Fprintln(fs.Output(), "")
		fmt.Fprintln(fs.Output(), "Flags:")
		fs.PrintDefaults()
	}
	fs.Parse(args)

	cfg := DefaultConfig()
	now := time.Now()

	memories, err := LoadAllMemories(vaultRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[!] Failed to load memories: %v\n", err)
		os.Exit(1)
	}

	opts := graphOpts{
		IncludeCoactivation:   !*noCoactivation,
		CoactivationThreshold: *coactThreshold,
		MinActivation:         *minActivation,
	}
	if *includeTypes != "" {
		for _, t := range strings.Split(*includeTypes, ",") {
			t = strings.TrimSpace(t)
			if t != "" {
				opts.IncludeTypes = append(opts.IncludeTypes, t)
			}
		}
	}

	var coactLog *CoactivationLog
	if opts.IncludeCoactivation {
		coactLog, err = LoadCoactivation(vaultRoot)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[!] Failed to load coactivation data: %v\n", err)
			coactLog = &CoactivationLog{}
		}
	}

	payload := buildGraphPayload(memories, coactLog, cfg, now, opts)
	payload.Meta["vault_root"] = vaultRoot

	outputPath := *output
	if outputPath == "" {
		ext := ".html"
		if *format == "json" {
			ext = ".json"
		}
		outputPath = filepath.Join(vaultRoot, "Metrics", "graph"+ext)
	}

	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		fmt.Fprintf(os.Stderr, "[!] Failed to create output directory: %v\n", err)
		os.Exit(1)
	}

	pageTitle := *title
	if pageTitle == "" {
		pageTitle = "LJM Memory Graph — " + filepath.Base(vaultRoot)
	}

	var rendered []byte
	switch *format {
	case "json":
		rendered, err = json.MarshalIndent(payload, "", "  ")
	case "html":
		rendered, err = renderGraphHTML(payload, pageTitle)
	default:
		fmt.Fprintf(os.Stderr, "[!] Unknown format %q (expected html or json)\n", *format)
		os.Exit(1)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "[!] Failed to render output: %v\n", err)
		os.Exit(1)
	}

	if err := os.WriteFile(outputPath, rendered, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "[!] Failed to write %s: %v\n", outputPath, err)
		os.Exit(1)
	}

	fmt.Printf("Wrote %s  (%d nodes, %d edges, %d bytes)\n",
		outputPath, len(payload.Nodes), len(payload.Edges), len(rendered))

	if *openFlag {
		if err := openInBrowser(outputPath); err != nil {
			fmt.Fprintf(os.Stderr, "[!] Failed to open: %v\n", err)
		}
	}
}

// buildGraphPayload assembles the JSON payload from loaded memories, the
// coactivation log, config, and options. Pure function — easy to test.
func buildGraphPayload(memories []*MemoryEntry, coactLog *CoactivationLog, cfg Config, now time.Time, opts graphOpts) graphPayload {
	typeAllowed := func(t MemoryType) bool {
		if len(opts.IncludeTypes) == 0 {
			return true
		}
		for _, allow := range opts.IncludeTypes {
			if string(t) == allow {
				return true
			}
		}
		return false
	}

	var filtered []*MemoryEntry
	for _, m := range memories {
		if !typeAllowed(m.Type) {
			continue
		}
		filtered = append(filtered, m)
	}

	graph := BuildGraph(filtered, cfg)

	// --- Build deduplicated edge list ---
	seen := make(map[string]bool)
	var edges []graphEdge

	// Stable iteration: collect source keys, sort
	srcKeys := make([]string, 0, len(graph.Edges))
	for k := range graph.Edges {
		srcKeys = append(srcKeys, k)
	}
	sort.Strings(srcKeys)

	for _, src := range srcKeys {
		for _, e := range graph.Edges[src] {
			directed := e.Relationship == "supersedes"
			var key string
			var sourceID, targetID string
			if directed {
				sourceID, targetID = src, e.Target
				key = sourceID + "|" + targetID + "|" + e.Relationship + "|d"
			} else {
				a, b := src, e.Target
				if a > b {
					a, b = b, a
				}
				sourceID, targetID = a, b
				key = a + "|" + b + "|" + e.Relationship
			}
			if seen[key] {
				continue
			}
			seen[key] = true
			edges = append(edges, graphEdge{
				Source:       sourceID,
				Target:       targetID,
				Relationship: e.Relationship,
				Weight:       e.Weight,
				Kind:         "explicit",
				Directed:     directed,
			})
		}
	}

	// --- Coactivation overlay (skip pairs already covered) ---
	if opts.IncludeCoactivation && coactLog != nil {
		for _, p := range coactLog.Pairs {
			if p.Count < opts.CoactivationThreshold {
				continue
			}
			if _, ok := graph.Index[p.MemoryA]; !ok {
				continue
			}
			if _, ok := graph.Index[p.MemoryB]; !ok {
				continue
			}
			if hasEdge(graph, p.MemoryA, p.MemoryB) {
				continue
			}
			a, b := p.MemoryA, p.MemoryB
			if a > b {
				a, b = b, a
			}
			w := float64(p.Count) / 20.0
			if w > 1.0 {
				w = 1.0
			}
			edges = append(edges, graphEdge{
				Source:       a,
				Target:       b,
				Relationship: "coactivation",
				Weight:       w,
				Kind:         "coactivation",
				Directed:     false,
			})
		}
	}

	// --- Build nodes (apply min-activation filter) ---
	type nodeWithMem struct {
		Node graphNode
		Mem  *MemoryEntry
	}
	nodeMap := make(map[string]nodeWithMem)

	for key, m := range graph.Index {
		var activation float64
		if m.Type == TypeKnowledge {
			activation = 1.0
		} else {
			activation = ComputeActivation(m, now)
		}
		if activation < opts.MinActivation {
			continue
		}
		nodeMap[key] = nodeWithMem{
			Node: graphNode{
				ID:          key,
				Title:       firstNonEmpty(m.Title, key),
				Path:        relativePath(m.FilePath),
				Type:        string(m.Type),
				Tags:        normalizeTags(m.Tags),
				Activation:  activation,
				Confidence:  m.Confidence,
				AccessCount: m.AccessCount,
				Importance:  m.Importance,
			},
			Mem: m,
		}
	}

	// --- Drop edges with vanished endpoints, compute degree ---
	var keptEdges []graphEdge
	degree := make(map[string]int)
	for _, e := range edges {
		_, srcOK := nodeMap[e.Source]
		_, tgtOK := nodeMap[e.Target]
		if !srcOK || !tgtOK {
			continue
		}
		keptEdges = append(keptEdges, e)
		degree[e.Source]++
		degree[e.Target]++
	}

	// --- Assemble node list, set degree, sort for stability ---
	nodes := make([]graphNode, 0, len(nodeMap))
	for k, nm := range nodeMap {
		nm.Node.Degree = degree[k]
		nodes = append(nodes, nm.Node)
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })
	sort.Slice(keptEdges, func(i, j int) bool {
		if keptEdges[i].Source != keptEdges[j].Source {
			return keptEdges[i].Source < keptEdges[j].Source
		}
		if keptEdges[i].Target != keptEdges[j].Target {
			return keptEdges[i].Target < keptEdges[j].Target
		}
		return keptEdges[i].Relationship < keptEdges[j].Relationship
	})

	return graphPayload{
		Meta: map[string]any{
			"generated_at":           now.Format(time.RFC3339),
			"n_nodes":                len(nodes),
			"n_edges":                len(keptEdges),
			"edge_weights":           cfg.EdgeWeights,
			"coactivation_threshold": opts.CoactivationThreshold,
			"include_coactivation":   opts.IncludeCoactivation,
			"min_activation":         opts.MinActivation,
		},
		Nodes: nodes,
		Edges: keptEdges,
	}
}

func renderGraphHTML(payload graphPayload, title string) ([]byte, error) {
	jsonBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal payload: %w", err)
	}

	tmpl, err := template.New("graph").Parse(graphHTMLTemplate)
	if err != nil {
		return nil, fmt.Errorf("parse template: %w", err)
	}

	type tplData struct {
		Title  string
		CSS    template.CSS
		Vendor template.JS
		App    template.JS
		Data   template.JS
	}

	var buf bytes.Buffer
	err = tmpl.Execute(&buf, tplData{
		Title:  title,
		CSS:    template.CSS(graphCSS),
		Vendor: template.JS(graphVendor),
		App:    template.JS(graphJS),
		Data:   template.JS(jsonBytes),
	})
	if err != nil {
		return nil, fmt.Errorf("execute template: %w", err)
	}
	return buf.Bytes(), nil
}

func openInBrowser(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	return openURL(abs)
}

func openURL(url string) error {
	switch runtime.GOOS {
	case "windows":
		return exec.Command("cmd", "/c", "start", "", url).Start()
	case "darwin":
		return exec.Command("open", url).Start()
	default:
		return exec.Command("xdg-open", url).Start()
	}
}

// relativePath converts an absolute file path to a vault-relative one when it
// contains "/Memory/" or "/Archive/"; otherwise returns the basename.
func relativePath(p string) string {
	slash := filepath.ToSlash(p)
	for _, marker := range []string{"/Memory/", "/Archive/"} {
		if idx := strings.Index(slash, marker); idx >= 0 {
			return slash[idx+1:]
		}
	}
	return filepath.Base(p)
}

func firstNonEmpty(s ...string) string {
	for _, v := range s {
		if v != "" {
			return v
		}
	}
	return ""
}

func normalizeTags(tags []string) []string {
	if tags == nil {
		return []string{}
	}
	return tags
}

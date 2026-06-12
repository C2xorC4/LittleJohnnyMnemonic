package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// lint-links audits the associative graph for link-health problems that the
// runtime graph and `jm graph` cannot surface:
//
//   - ASYMMETRIC: A links to B in frontmatter but B has no link back to A.
//     The runtime graph (graph.go BuildGraph) synthesizes a reverse edge for
//     spreading activation, so retrieval already propagates both ways — but the
//     asymmetry is invisible in `jm graph` (cmd_graph.go undirects all
//     non-supersedes edges before dedup) and in Obsidian's graph view, and the
//     reciprocal is absent from B's frontmatter. For SYMMETRIC relationship
//     types a reciprocal with the same relationship is valid, so --fix can add
//     it; DIRECTIONAL types are reported for manual handling (an inverse asserts
//     a different claim).
//
//   - DANGLING: a frontmatter link names a Memory/ target that does not resolve
//     to any loaded LTM entry — a broken reference (the "inference poisoning"
//     hazard). Archive/Buffer targets are not flagged (not LTM).
//
//   - PROSE-ONLY: the body contains a [[...]] wiki-link to an existing memory
//     that is NOT present in the frontmatter links: block. parseLinks reads only
//     the YAML block, so these never become edges. Reported as candidates, never
//     auto-promoted (bodies contain negative-example and hypothetical mentions).
//
//   - CONCEPT (--concepts): the body mentions another entry's title concept
//     (stemmed token overlap) without any link. Uses the same Stem/stemTextSet
//     matching as retrieval so inflectional variants (tense, plural) still match
//     — the "don't miss a link because matching was too literal" guard. Noisier;
//     report-only.
//
// Only ASYMMETRIC-symmetric is ever written, and only under --fix.

// symmetricRelationships are relationship types where a reciprocal link with the
// SAME relationship is semantically valid, so --fix may auto-add it.
var symmetricRelationships = map[string]bool{
	"related-to":  true,
	"contradicts": true,
	"complements": true,
	"shares":      true,
	"learned":     true,
}

// directional relationships are intentionally one-way (supersedes/superseded-by)
// and excluded from asymmetry reporting entirely — a missing reverse is correct.
var oneWayByDesign = map[string]bool{
	"supersedes":    true,
	"superseded-by": true,
}

var lintWikiLinkRe = regexp.MustCompile(`\[\[([^\]\[]+)\]\]`)

var conceptStopwords = map[string]bool{
	"the": true, "and": true, "for": true, "with": true, "from": true,
	"that": true, "this": true, "into": true, "over": true, "under": true,
	"using": true, "your": true, "via": true, "per": true, "vs": true,
}

type lintAsymmetric struct {
	From         string `json:"from"`
	To           string `json:"to"`
	Relationship string `json:"relationship"`
	AutoFixable  bool   `json:"auto_fixable"`
}

type lintDangling struct {
	From         string `json:"from"`
	Target       string `json:"target"`
	Relationship string `json:"relationship"`
	Resolution   string `json:"resolution"`          // repoint | archived | ambiguous | missing
	RepointTo    string `json:"repoint_to,omitempty"` // live key to repoint to (Resolution==repoint)
}

type lintProseOnly struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type lintConcept struct {
	From    string   `json:"from"`
	To      string   `json:"to"`
	Overlap []string `json:"overlap"`
}

type lintReport struct {
	Asymmetric []lintAsymmetric `json:"asymmetric"`
	Dangling   []lintDangling   `json:"dangling"`
	ProseOnly  []lintProseOnly  `json:"prose_only"`
	Concepts   []lintConcept    `json:"concepts,omitempty"`
}

func cmdLintLinks(vaultRoot string, args []string) {
	fs := flag.NewFlagSet("lint-links", flag.ExitOnError)
	fix := fs.Bool("fix", false, "Add reciprocal links for asymmetric SYMMETRIC-type edges (writes memory files). Prose/concept candidates are never auto-applied.")
	fixDangling := fs.Bool("fix-dangling", false, "Repoint dangling links whose target exists at a different path (writes). Archived/ambiguous/missing targets are reported, never auto-changed.")
	repoint := fs.String("repoint", "", "Manually repoint links: comma-separated old=new key mappings, e.g. user/obs_x=user/profile_x (writes)")
	removeDangling := fs.String("remove-dangling", "", "Remove all links pointing to these comma-separated target keys (writes)")
	format := fs.String("format", "text", "Output format: text | json")
	concepts := fs.Bool("concepts", false, "Also report stemmed title-concept mentions in body text without a link (noisier, report-only)")
	minOverlap := fs.Int("min-overlap", 2, "For --concepts: minimum stemmed-token overlap with a target title to flag")
	fs.Parse(args)

	memories, err := LoadAllMemories(vaultRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[!] Failed to load memories: %v\n", err)
		os.Exit(1)
	}

	report := computeLinkLint(memories, *minOverlap, *concepts)
	classifyDangling(report.Dangling, memories, vaultRoot)

	if *fixDangling {
		written, repointed := applyDanglingRepoints(memories, report.Dangling)
		fmt.Printf("Repointed %d dangling link(s) across %d file(s).\n\n", repointed, written)
		memories, _ = LoadAllMemories(vaultRoot)
		report = computeLinkLint(memories, *minOverlap, *concepts)
		classifyDangling(report.Dangling, memories, vaultRoot)
	}

	if *repoint != "" {
		written, n := applyRepointMap(memories, parseKeyMap(*repoint))
		fmt.Printf("Repointed %d link(s) across %d file(s).\n\n", n, written)
		memories, _ = LoadAllMemories(vaultRoot)
		report = computeLinkLint(memories, *minOverlap, *concepts)
		classifyDangling(report.Dangling, memories, vaultRoot)
	}

	if *removeDangling != "" {
		written, n := applyRemoveLinks(memories, parseKeySet(*removeDangling))
		fmt.Printf("Removed %d dead link(s) across %d file(s).\n\n", n, written)
		memories, _ = LoadAllMemories(vaultRoot)
		report = computeLinkLint(memories, *minOverlap, *concepts)
		classifyDangling(report.Dangling, memories, vaultRoot)
	}

	if *fix {
		written, fixed := applyReciprocalFixes(memories, report)
		fmt.Printf("Applied %d reciprocal link(s) across %d file(s).\n\n", fixed, written)
		// Recompute so the printed report reflects post-fix state.
		memories, _ = LoadAllMemories(vaultRoot)
		report = computeLinkLint(memories, *minOverlap, *concepts)
		classifyDangling(report.Dangling, memories, vaultRoot)
	}

	switch *format {
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(report)
	default:
		printLintReport(report, *concepts)
	}
}

// computeLinkLint runs all detections and returns a structured report. Split out
// from printing/fixing so it is directly unit-testable.
func computeLinkLint(memories []*MemoryEntry, minOverlap int, withConcepts bool) lintReport {
	keyToMem := make(map[string]*MemoryEntry, len(memories))
	for _, m := range memories {
		keyToMem[normalizeKey(m)] = m
	}

	// linked[a|b] = true if a has any frontmatter link to b.
	linked := make(map[string]bool)
	for _, m := range memories {
		ka := normalizeKey(m)
		for _, l := range m.Links {
			linked[ka+"|"+normalizeLinkTarget(l.Target)] = true
		}
	}

	var report lintReport
	seenAsym := make(map[string]bool)
	seenDangling := make(map[string]bool)

	for _, m := range memories {
		ka := normalizeKey(m)
		for _, l := range m.Links {
			rel := l.Relationship
			if oneWayByDesign[rel] {
				continue
			}
			kb := normalizeLinkTarget(l.Target)
			if kb == ka {
				continue // self-link; ignore
			}
			if _, ok := keyToMem[kb]; !ok {
				// Only Memory/ targets are LTM claims; archive/buffer/other are
				// not flagged as dangling.
				if strings.HasPrefix(kb, "memory/") {
					dk := ka + "|" + kb + "|" + rel
					if !seenDangling[dk] {
						seenDangling[dk] = true
						report.Dangling = append(report.Dangling, lintDangling{From: ka, Target: kb, Relationship: rel})
					}
				}
				continue
			}
			// Asymmetric if B has no link back to A (any relationship).
			if !linked[kb+"|"+ka] {
				ak := ka + "|" + kb + "|" + rel
				if !seenAsym[ak] {
					seenAsym[ak] = true
					report.Asymmetric = append(report.Asymmetric, lintAsymmetric{
						From: ka, To: kb, Relationship: rel, AutoFixable: symmetricRelationships[rel],
					})
				}
			}
		}
	}

	// Prose-only: body [[...]] to an existing memory not in frontmatter.
	seenProse := make(map[string]bool)
	for _, m := range memories {
		ka := normalizeKey(m)
		for _, mt := range lintWikiLinkRe.FindAllStringSubmatch(m.Body, -1) {
			kb := normalizeLinkTarget(mt[1])
			if kb == ka {
				continue
			}
			if _, ok := keyToMem[kb]; !ok {
				continue // prose mention of a non-LTM or non-existent target
			}
			if linked[ka+"|"+kb] {
				continue // already an explicit edge
			}
			pk := ka + "|" + kb
			if !seenProse[pk] {
				seenProse[pk] = true
				report.ProseOnly = append(report.ProseOnly, lintProseOnly{From: ka, To: kb})
			}
		}
	}

	if withConcepts {
		report.Concepts = computeConceptMentions(memories, keyToMem, linked, minOverlap)
	}

	sortLintReport(&report)
	return report
}

// computeConceptMentions flags entries whose body mentions another entry's title
// concept (stemmed-token overlap >= minOverlap) without any link between them.
func computeConceptMentions(memories []*MemoryEntry, keyToMem map[string]*MemoryEntry, linked map[string]bool, minOverlap int) []lintConcept {
	// Precompute significant stemmed title tokens per memory.
	titleTokens := make(map[string][]string, len(memories))
	for _, m := range memories {
		k := normalizeKey(m)
		var toks []string
		seen := make(map[string]bool)
		for t := range stemTextSet(m.Title) {
			if len(t) >= 4 && !conceptStopwords[t] && !seen[t] {
				seen[t] = true
				toks = append(toks, t)
			}
		}
		titleTokens[k] = toks
	}

	var out []lintConcept
	for _, m := range memories {
		ka := normalizeKey(m)
		bodySet := stemTextSet(m.Title + " " + m.Body)
		for _, b := range memories {
			kb := normalizeKey(b)
			if kb == ka || linked[ka+"|"+kb] || linked[kb+"|"+ka] {
				continue
			}
			var overlap []string
			for _, t := range titleTokens[kb] {
				if bodySet[t] {
					overlap = append(overlap, t)
				}
			}
			if len(overlap) >= minOverlap {
				sort.Strings(overlap)
				out = append(out, lintConcept{From: ka, To: kb, Overlap: overlap})
			}
		}
	}
	return out
}

// applyReciprocalFixes adds a reciprocal link to the target of each auto-fixable
// asymmetric edge, then writes each modified file once. Returns (filesWritten,
// linksAdded).
func applyReciprocalFixes(memories []*MemoryEntry, report lintReport) (int, int) {
	keyToMem := make(map[string]*MemoryEntry, len(memories))
	for _, m := range memories {
		keyToMem[normalizeKey(m)] = m
	}

	modified := make(map[*MemoryEntry]bool)
	added := 0
	for _, a := range report.Asymmetric {
		if !a.AutoFixable {
			continue
		}
		target := keyToMem[a.To] // B — gains a back-link to A
		source := keyToMem[a.From]
		if target == nil || source == nil {
			continue
		}
		// Re-check the reciprocal is genuinely absent (defensive against dupes
		// within this batch).
		already := false
		for _, l := range target.Links {
			if normalizeLinkTarget(l.Target) == a.From {
				already = true
				break
			}
		}
		if already {
			continue
		}
		target.Links = append(target.Links, Link{
			Target:       relMemoryPath(source),
			Relationship: a.Relationship,
		})
		modified[target] = true
		added++
	}

	written := 0
	for m := range modified {
		if err := WriteMemoryEntry(m); err != nil {
			fmt.Fprintf(os.Stderr, "[!] Failed to write %s: %v\n", m.FileName, err)
			continue
		}
		written++
	}
	return written, added
}

// classifyDangling annotates each dangling link with a resolution: repoint (a
// unique live entry exists with the same basename at a different path — usually
// a reclassified entry whose inbound links weren't updated), archived (target
// decayed to Archive/), ambiguous (multiple basename matches), or missing (no
// trace anywhere). Mutates the slice in place.
func classifyDangling(dangling []lintDangling, memories []*MemoryEntry, vaultRoot string) {
	byBase := make(map[string][]string)
	for _, m := range memories {
		k := normalizeKey(m)
		byBase[baseName(k)] = append(byBase[baseName(k)], k)
	}
	arch := archiveBasenames(vaultRoot)
	for i := range dangling {
		base := baseName(dangling[i].Target)
		switch matches := byBase[base]; {
		case len(matches) == 1:
			dangling[i].Resolution = "repoint"
			dangling[i].RepointTo = matches[0]
		case len(matches) > 1:
			dangling[i].Resolution = "ambiguous"
		case arch[base]:
			dangling[i].Resolution = "archived"
		default:
			dangling[i].Resolution = "missing"
		}
	}
}

func baseName(key string) string {
	if i := strings.LastIndex(key, "/"); i >= 0 {
		return key[i+1:]
	}
	return key
}

// archiveBasenames returns the lowercased basenames of all .md files under Archive/.
func archiveBasenames(vaultRoot string) map[string]bool {
	set := make(map[string]bool)
	_ = filepath.WalkDir(filepath.Join(vaultRoot, "Archive"), func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}
		set[strings.ToLower(strings.TrimSuffix(d.Name(), ".md"))] = true
		return nil
	})
	return set
}

// applyDanglingRepoints rewrites each Resolution=="repoint" dangling link to its
// actual live target path and writes affected files once. Returns (filesWritten,
// linksRepointed). Archived/ambiguous/missing links are never auto-changed.
func applyDanglingRepoints(memories []*MemoryEntry, dangling []lintDangling) (int, int) {
	keyToMem := make(map[string]*MemoryEntry, len(memories))
	for _, m := range memories {
		keyToMem[normalizeKey(m)] = m
	}
	modified := make(map[*MemoryEntry]bool)
	repointed := 0
	for _, d := range dangling {
		if d.Resolution != "repoint" {
			continue
		}
		src, dst := keyToMem[d.From], keyToMem[d.RepointTo]
		if src == nil || dst == nil {
			continue
		}
		newTarget := relMemoryPath(dst)
		for i := range src.Links {
			if normalizeLinkTarget(src.Links[i].Target) == d.Target {
				src.Links[i].Target = newTarget
				modified[src] = true
				repointed++
			}
		}
	}
	written := 0
	for m := range modified {
		if err := WriteMemoryEntry(m); err != nil {
			fmt.Fprintf(os.Stderr, "[!] write %s: %v\n", m.FileName, err)
			continue
		}
		written++
	}
	return written, repointed
}

// normInputKey normalizes a user-supplied memory key (as shown in lint output,
// e.g. "user/obs_x" or "Memory/User/obs_x") to the canonical lowercased form
// with a memory/ (or archive/) prefix, matching normalizeKey/normalizeLinkTarget.
func normInputKey(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.TrimPrefix(s, "[[")
	s = strings.TrimSuffix(s, "]]")
	s = strings.TrimSuffix(s, ".md")
	if s != "" && !strings.HasPrefix(s, "memory/") && !strings.HasPrefix(s, "archive/") {
		s = "memory/" + s
	}
	return s
}

// parseKeyMap parses "old=new,old2=new2" into a normalized old→new key map.
func parseKeyMap(s string) map[string]string {
	m := make(map[string]string)
	for _, pair := range strings.Split(s, ",") {
		kv := strings.SplitN(strings.TrimSpace(pair), "=", 2)
		if len(kv) == 2 {
			if k := normInputKey(kv[0]); k != "" {
				m[k] = normInputKey(kv[1])
			}
		}
	}
	return m
}

// parseKeySet parses "k1,k2,k3" into a normalized key set.
func parseKeySet(s string) map[string]bool {
	set := make(map[string]bool)
	for _, k := range strings.Split(s, ",") {
		if nk := normInputKey(k); nk != "" {
			set[nk] = true
		}
	}
	return set
}

// applyRepointMap rewrites every link whose normalized target is a key in the
// mapping to the mapped target's actual live path. Returns (filesWritten,
// linksRepointed).
func applyRepointMap(memories []*MemoryEntry, mapping map[string]string) (int, int) {
	keyToMem := make(map[string]*MemoryEntry, len(memories))
	for _, m := range memories {
		keyToMem[normalizeKey(m)] = m
	}
	modified := make(map[*MemoryEntry]bool)
	n := 0
	for _, m := range memories {
		for i := range m.Links {
			if newKey, ok := mapping[normalizeLinkTarget(m.Links[i].Target)]; ok {
				dst := keyToMem[newKey]
				if dst == nil {
					fmt.Fprintf(os.Stderr, "[!] repoint target not found, skipping: %s\n", newKey)
					continue
				}
				m.Links[i].Target = relMemoryPath(dst)
				modified[m] = true
				n++
			}
		}
	}
	return writeModifiedMemories(modified), n
}

// applyRemoveLinks deletes every link whose normalized target is in the set.
// Returns (filesWritten, linksRemoved).
func applyRemoveLinks(memories []*MemoryEntry, targets map[string]bool) (int, int) {
	modified := make(map[*MemoryEntry]bool)
	removed := 0
	for _, m := range memories {
		kept := m.Links[:0]
		changed := false
		for _, l := range m.Links {
			if targets[normalizeLinkTarget(l.Target)] {
				removed++
				changed = true
				continue
			}
			kept = append(kept, l)
		}
		if changed {
			m.Links = kept
			modified[m] = true
		}
	}
	return writeModifiedMemories(modified), removed
}

func writeModifiedMemories(modified map[*MemoryEntry]bool) int {
	w := 0
	for m := range modified {
		if err := WriteMemoryEntry(m); err != nil {
			fmt.Fprintf(os.Stderr, "[!] write %s: %v\n", m.FileName, err)
			continue
		}
		w++
	}
	return w
}

// relMemoryPath returns the case-preserved vault-relative link target for a
// memory ("Memory/Semantic/foo"), suitable for storage in a Link.Target (which
// WriteMemoryEntry wraps in [[ ]]).
func relMemoryPath(m *MemoryEntry) string {
	parts := strings.Split(filepath.ToSlash(m.FilePath), "/")
	for i, p := range parts {
		if strings.EqualFold(p, "Memory") || strings.EqualFold(p, "Archive") {
			rel := strings.Join(parts[i:], "/")
			return strings.TrimSuffix(rel, ".md")
		}
	}
	return strings.TrimSuffix(filepath.Base(m.FilePath), ".md")
}

func sortLintReport(r *lintReport) {
	sort.Slice(r.Asymmetric, func(i, j int) bool {
		if r.Asymmetric[i].From != r.Asymmetric[j].From {
			return r.Asymmetric[i].From < r.Asymmetric[j].From
		}
		return r.Asymmetric[i].To < r.Asymmetric[j].To
	})
	sort.Slice(r.Dangling, func(i, j int) bool {
		if r.Dangling[i].From != r.Dangling[j].From {
			return r.Dangling[i].From < r.Dangling[j].From
		}
		return r.Dangling[i].Target < r.Dangling[j].Target
	})
	sort.Slice(r.ProseOnly, func(i, j int) bool {
		if r.ProseOnly[i].From != r.ProseOnly[j].From {
			return r.ProseOnly[i].From < r.ProseOnly[j].From
		}
		return r.ProseOnly[i].To < r.ProseOnly[j].To
	})
	sort.Slice(r.Concepts, func(i, j int) bool {
		if len(r.Concepts[i].Overlap) != len(r.Concepts[j].Overlap) {
			return len(r.Concepts[i].Overlap) > len(r.Concepts[j].Overlap) // strongest first
		}
		return r.Concepts[i].From < r.Concepts[j].From
	})
}

func printLintReport(r lintReport, withConcepts bool) {
	autoFixable := 0
	for _, a := range r.Asymmetric {
		if a.AutoFixable {
			autoFixable++
		}
	}

	fmt.Println("# Link Lint Report")
	fmt.Println()
	fmt.Printf("Asymmetric: %d (%d auto-fixable)   Dangling: %d   Prose-only: %d",
		len(r.Asymmetric), autoFixable, len(r.Dangling), len(r.ProseOnly))
	if withConcepts {
		fmt.Printf("   Concept candidates: %d", len(r.Concepts))
	}
	fmt.Println()

	if len(r.Dangling) > 0 {
		repointable := 0
		for _, d := range r.Dangling {
			if d.Resolution == "repoint" {
				repointable++
			}
		}
		fmt.Printf("\n## Dangling links (broken references) — %d of %d mechanically repointable\n", repointable, len(r.Dangling))
		for _, d := range r.Dangling {
			switch d.Resolution {
			case "repoint":
				fmt.Printf("  %s  →(%s)  %s   [REPOINT → %s]\n", shortKey(d.From), d.Relationship, shortKey(d.Target), shortKey(d.RepointTo))
			case "archived":
				fmt.Printf("  %s  →(%s)  %s   [ARCHIVED — remove link or unarchive target]\n", shortKey(d.From), d.Relationship, shortKey(d.Target))
			case "ambiguous":
				fmt.Printf("  %s  →(%s)  %s   [AMBIGUOUS — multiple basename matches]\n", shortKey(d.From), d.Relationship, shortKey(d.Target))
			default:
				fmt.Printf("  %s  →(%s)  %s   [MISSING — no trace, manual]\n", shortKey(d.From), d.Relationship, shortKey(d.Target))
			}
		}
		if repointable > 0 {
			fmt.Printf("\n  Run `jm lint-links --fix-dangling` to repoint the %d mechanical case(s).\n", repointable)
		}
	}

	if len(r.Asymmetric) > 0 {
		fmt.Println("\n## Asymmetric links (no reciprocal in target frontmatter)")
		for _, a := range r.Asymmetric {
			tag := "manual (directional)"
			if a.AutoFixable {
				tag = "auto-fixable"
			}
			fmt.Printf("  %s  →(%s)  %s   [%s]\n", shortKey(a.From), a.Relationship, shortKey(a.To), tag)
		}
		if autoFixable > 0 {
			fmt.Printf("\n  Run `jm lint-links --fix` to add %d reciprocal link(s).\n", autoFixable)
		}
	}

	if len(r.ProseOnly) > 0 {
		fmt.Println("\n## Prose-only links (body [[...]] not in frontmatter — review candidates)")
		for _, p := range r.ProseOnly {
			fmt.Printf("  %s  ⟶  %s\n", shortKey(p.From), shortKey(p.To))
		}
	}

	if withConcepts && len(r.Concepts) > 0 {
		fmt.Println("\n## Concept mentions (body mentions target title, no link — review candidates)")
		for _, c := range r.Concepts {
			fmt.Printf("  %s  ~  %s   (%s)\n", shortKey(c.From), shortKey(c.To), strings.Join(c.Overlap, ", "))
		}
	}

	if len(r.Asymmetric) == 0 && len(r.Dangling) == 0 && len(r.ProseOnly) == 0 && (!withConcepts || len(r.Concepts) == 0) {
		fmt.Println("\n✓ No link-health issues found.")
	}
}

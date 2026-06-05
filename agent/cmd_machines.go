package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	machinesFile        = "System/machines.json"
	machinesExampleFile = "System/machines.example.json"
)

// LoadMachineRegistry loads System/machines.json. If absent, attempts to
// bootstrap from machines.example.json (copying it to machines.json).
// Returns (nil, "") if neither file exists; the second return value is a
// notice message when bootstrapping occurred.
func LoadMachineRegistry(vaultRoot string) (*MachineRegistry, string) {
	realPath := filepath.Join(vaultRoot, machinesFile)
	data, err := os.ReadFile(realPath)
	if err == nil {
		var reg MachineRegistry
		if jsonErr := json.Unmarshal(data, &reg); jsonErr != nil {
			return nil, fmt.Sprintf("machines.json parse error: %v", jsonErr)
		}
		return &reg, ""
	}

	// Try bootstrapping from the committed example template.
	exPath := filepath.Join(vaultRoot, machinesExampleFile)
	exData, err := os.ReadFile(exPath)
	if err != nil {
		return nil, ""
	}

	if writeErr := os.WriteFile(realPath, exData, 0o644); writeErr != nil {
		return nil, fmt.Sprintf("bootstrap: could not write machines.json: %v", writeErr)
	}

	var reg MachineRegistry
	if jsonErr := json.Unmarshal(exData, &reg); jsonErr != nil {
		return nil, fmt.Sprintf("machines.example.json parse error: %v", jsonErr)
	}
	return &reg, "machines.json bootstrapped from machines.example.json — " +
		"edit System/machines.json with real values (gitignored, never committed)"
}

func cmdMachines(vaultRoot string, args []string) {
	fs := flag.NewFlagSet("machines", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "output raw JSON")
	fs.Parse(args) //nolint:errcheck

	reg, notice := LoadMachineRegistry(vaultRoot)
	if notice != "" {
		fmt.Fprintln(os.Stderr, "[jm machines]", notice)
	}
	if reg == nil {
		fmt.Fprintln(os.Stderr, "[jm machines] no machines.json or machines.example.json found in System/")
		os.Exit(1)
	}

	if *jsonOut {
		data, _ := json.MarshalIndent(reg, "", "  ")
		fmt.Println(string(data))
		return
	}

	writeMachinesTable(os.Stdout, reg)
}

func writeMachinesTable(w io.Writer, reg *MachineRegistry) {
	ids := sortedMachineKeys(reg.Machines)

	fmt.Fprintln(w, "MACHINES")
	for _, id := range ids {
		m := reg.Machines[id]

		tags := ""
		if m.Current {
			tags += " [current]"
		}
		if m.Status == "unconfigured" {
			tags += " [UNCONFIGURED]"
		}

		fmt.Fprintf(w, "  %-16s  %-14s  %-14s  elevation: %s%s\n",
			id, m.Platform, m.User, m.Elevation, tags)

		if m.SSH != nil {
			addr := machineSSHAddr(m)
			flags := ""
			if len(m.SSH.Flags) > 0 {
				flags = " " + strings.Join(m.SSH.Flags, " ")
			}
			fmt.Fprintf(w, "  %-16s  ssh: %s%s -i %s %s\n",
				"", m.SSH.Binary, flags, m.SSH.Key, addr)
			if m.SSH.Notes != "" {
				fmt.Fprintf(w, "  %-16s  ⚠  %s\n", "", m.SSH.Notes)
			}
		}
	}

	if len(reg.Tooling) == 0 {
		return
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, "TOOLING")
	for _, id := range sortedToolEntryKeys(reg.Tooling) {
		t := reg.Tooling[id]
		mids := sortedToolMachineKeys(t.Machines)
		fmt.Fprintf(w, "  %-26s  %-12s  on: %s\n", id, t.Type, strings.Join(mids, ", "))
		for _, mid := range mids {
			tm := t.Machines[mid]
			line := tm.Path
			if tm.Notes != "" {
				line += " — " + tm.Notes
			}
			fmt.Fprintf(w, "  %-26s  %-12s  %s\n", "", mid, line)
		}
	}
}

// machinesSummaryBlock produces the compact machine/tooling block injected at
// session start. Returns empty string when the registry is unavailable.
func machinesSummaryBlock(vaultRoot string) string {
	reg, _ := LoadMachineRegistry(vaultRoot)
	if reg == nil {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## Machines & Tooling\n")

	// Current machine header
	ids := sortedMachineKeys(reg.Machines)
	for _, id := range ids {
		m := reg.Machines[id]
		if m.Current {
			fmt.Fprintf(&sb, "Current: %s (%s, %s, elevation: %s)\n",
				id, m.Platform, m.User, m.Elevation)
			break
		}
	}

	// SSH targets (configured only)
	hasSSHHeader := false
	for _, id := range ids {
		m := reg.Machines[id]
		if m.SSH == nil || m.Status == "unconfigured" {
			continue
		}
		if !hasSSHHeader {
			sb.WriteString("SSH targets:\n")
			hasSSHHeader = true
		}
		addr := machineSSHAddr(m)
		flags := ""
		if len(m.SSH.Flags) > 0 {
			flags = " " + strings.Join(m.SSH.Flags, " ")
		}
		fmt.Fprintf(&sb, "  %s — %s%s -i %s %s\n",
			id, m.SSH.Binary, flags, m.SSH.Key, addr)
		if m.SSH.Notes != "" {
			fmt.Fprintf(&sb, "    ⚠ %s\n", m.SSH.Notes)
		}
		switch m.Elevation {
		case "none":
			sb.WriteString("    ⚠ elevation: none — never attempt sudo/UAC; raise install requests to user\n")
		case "full":
			sb.WriteString("    ✓ elevation: full — root/sudo available\n")
		}
	}

	// Unconfigured entries
	var unconfigured []string
	for _, id := range ids {
		if reg.Machines[id].Status == "unconfigured" {
			unconfigured = append(unconfigured, id)
		}
	}
	if len(unconfigured) > 0 {
		fmt.Fprintf(&sb, "Unconfigured (skip in routing): %s\n", strings.Join(unconfigured, ", "))
	}

	return strings.TrimRight(sb.String(), "\n")
}

func machineSSHAddr(m MachineEntry) string {
	host := m.IP
	if host == "" {
		host = m.Hostname
	}
	if host == "" || m.User == "" {
		return ""
	}
	return m.User + "@" + host
}

func sortedMachineKeys(m map[string]MachineEntry) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedToolEntryKeys(m map[string]ToolEntry) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedToolMachineKeys(m map[string]ToolMachineEntry) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

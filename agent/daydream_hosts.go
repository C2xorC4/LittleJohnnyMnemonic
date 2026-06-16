package main

import (
	"os"
	"os/exec"
)

const defaultVolleySpawnHint = "Claude: spawn memory-daydream background agents from the main loop."

// RuntimeHostSpec describes one daydream dispatch target. New hosts register
// here — detection, scheduler availability, and volley nudge hints stay in
// one table instead of scattered switch statements.
type RuntimeHostSpec struct {
	ID                string
	DetectEnvKeys     []string // any non-empty env var => this host
	DefaultFallback   bool     // used when no host env signals match
	SchedulerBinary   string   // empty => no headless invoker (hard skip)
	VolleySpawnHint   string   // appended to <daydream-nudge> block
}

var runtimeHostRegistry = []RuntimeHostSpec{
	{
		ID:              HostGrokBuild,
		DetectEnvKeys:   []string{"GROK_HOOK_EVENT", "GROK_SESSION_ID"},
		SchedulerBinary: "", // headless Grok invoker not wired yet
		VolleySpawnHint: "Grok: spawn_subagent(subagent_type: memory-daydream, background: true, capability_mode: all, cwd: vault) — see .grok/skills/memory-daydream/.",
	},
	{
		ID:              HostClaudeCode,
		DetectEnvKeys:   []string{"CLAUDE_CODE", "CLAUDE_SESSION_ID"},
		DefaultFallback: true,
		SchedulerBinary: "claude",
		VolleySpawnHint: defaultVolleySpawnHint,
	},
}

// LookupRuntimeHost returns the spec for a host ID, or false if unknown.
func LookupRuntimeHost(id string) (RuntimeHostSpec, bool) {
	id = normalizeSchedulerHost(id)
	for _, spec := range runtimeHostRegistry {
		if spec.ID == id {
			return spec, true
		}
	}
	return RuntimeHostSpec{}, false
}

// DetectRuntimeHost derives the active hook runtime from environment signals.
func DetectRuntimeHost() string {
	for _, spec := range runtimeHostRegistry {
		for _, key := range spec.DetectEnvKeys {
			if os.Getenv(key) != "" {
				return spec.ID
			}
		}
	}
	for _, spec := range runtimeHostRegistry {
		if spec.DefaultFallback {
			return spec.ID
		}
	}
	return HostClaudeCode
}

// SchedulerHostAvailable reports whether autodream can invoke the preferred host headlessly.
func SchedulerHostAvailable(host string) bool {
	spec, ok := LookupRuntimeHost(normalizeSchedulerHost(host))
	if !ok || spec.SchedulerBinary == "" {
		return false
	}
	_, err := exec.LookPath(spec.SchedulerBinary)
	return err == nil
}

// VolleySpawnHintForHost returns host-specific spawn instructions for the nudge block.
func VolleySpawnHintForHost(host string) string {
	spec, ok := LookupRuntimeHost(host)
	if ok && spec.VolleySpawnHint != "" {
		return spec.VolleySpawnHint
	}
	return defaultVolleySpawnHint
}
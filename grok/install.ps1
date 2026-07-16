# Install LJM Grok hooks, skills, agents, and global rules (Windows).
# Usage: .\grok\install.ps1 [-VaultRoot <path>] [-Uninstall]
# Linux/macOS: use grok/install.sh (same template, platform-specific runner).

param(
    [string]$VaultRoot = "",
    [switch]$Uninstall
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

if (-not $VaultRoot) {
    $VaultRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
}
$VaultRoot = $VaultRoot -replace '\\', '/'

$grokHome = Join-Path $env:USERPROFILE ".grok"
$hookDest = Join-Path $grokHome "hooks\ljm.json"
$skillsDest = Join-Path $grokHome "skills"
$agentsDest = Join-Path $grokHome "agents"
$globalGrok = Join-Path $grokHome "GROK.md"
$configSnippet = Join-Path $grokHome "ljm-config.snippet.toml"
$hookTemplate = Join-Path $VaultRoot "grok\hooks\ljm.template.json"
# Native Windows runner — PowerShell invokes run-hook.ps1 (run-hook.cmd is the PATHEXT entry).
$hookRunner = "powershell -NoProfile -ExecutionPolicy Bypass -File `"$VaultRoot/grok/bin/run-hook.ps1`""

function Ensure-Dir([string]$Path) {
    if (-not (Test-Path -LiteralPath $Path)) {
        New-Item -ItemType Directory -Path $Path -Force | Out-Null
    }
}

function Install-Tree([string]$Source, [string]$Dest) {
    Ensure-Dir $Dest
    Get-ChildItem -LiteralPath $Source -Directory | ForEach-Object {
        $target = Join-Path $Dest $_.Name
        if (Test-Path -LiteralPath $target) {
            Remove-Item -LiteralPath $target -Recurse -Force
        }
        Copy-Item -LiteralPath $_.FullName -Destination $target -Recurse -Force
    }
}

function Render-Hooks([string]$Template, [string]$Dest, [string]$Runner, [string]$Vault) {
    if (-not (Test-Path -LiteralPath $Template)) {
        throw "Hook template missing: $Template"
    }
    $content = Get-Content -LiteralPath $Template -Raw
    $content = $content.Replace('__HOOK_RUNNER__', $Runner)
    $content = $content.Replace('__JM_VAULT_ROOT__', $Vault)
    Set-Content -LiteralPath $Dest -Value $content -Encoding UTF8
}

if ($Uninstall) {
    @($hookDest, $globalGrok) | ForEach-Object {
        if (Test-Path -LiteralPath $_) { Remove-Item -LiteralPath $_ -Force }
    }
    @("memory-associator", "memory-daydream", "memory-associate", "memory-buffer", "memory-consolidate") | ForEach-Object {
        $p = Join-Path $skillsDest $_
        if (Test-Path -LiteralPath $p) { Remove-Item -LiteralPath $p -Recurse -Force }
    }
    @("memory-associator", "memory-daydream") | ForEach-Object {
        $p = Join-Path $agentsDest "$_.md"
        if (Test-Path -LiteralPath $p) { Remove-Item -LiteralPath $p -Force }
    }
    Write-Host "LJM Grok integration removed from $grokHome"
    exit 0
}

# Build agent/jm.exe if missing. Vault-root jm.exe should symlink here (no copy).
$jm = Join-Path $VaultRoot "agent/jm.exe"
if (-not (Test-Path -LiteralPath $jm)) {
    Write-Host "Building agent/jm.exe..."
    Push-Location (Join-Path $VaultRoot "agent")
    go build -o jm.exe .
    Pop-Location
}

# Hooks — expand shared template with this platform's runner (global only).
Ensure-Dir (Join-Path $grokHome "hooks")
Render-Hooks -Template $hookTemplate -Dest $hookDest -Runner $hookRunner -Vault $VaultRoot

# Skills and agents
Install-Tree (Join-Path $VaultRoot ".grok/skills") $skillsDest
Ensure-Dir $agentsDest
Get-ChildItem -LiteralPath (Join-Path $VaultRoot ".grok/agents") -Filter "*.md" | ForEach-Object {
    Copy-Item -LiteralPath $_.FullName -Destination (Join-Path $agentsDest $_.Name) -Force
}

# Global memory protocol rule (applies to all projects)
$globalTemplate = Join-Path $VaultRoot "grok/global/GROK.md"
Copy-Item -LiteralPath $globalTemplate -Destination $globalGrok -Force

# Optional subagent defaults — merge into ~/.grok/config.toml (never overwrite)
$configTemplate = Join-Path $VaultRoot "grok/config.toml.example"
if (Test-Path -LiteralPath $configTemplate) {
    Copy-Item -LiteralPath $configTemplate -Destination $configSnippet -Force
}

Write-Host "Installed LJM Grok integration:"
Write-Host "  Vault:   $VaultRoot"
Write-Host "  Hooks:   $hookDest"
Write-Host "  Runner:  $hookRunner"
Write-Host "  Skills:  $skillsDest\memory-*"
Write-Host "  Agents:  $agentsDest\memory-*.md"
Write-Host "  Rules:   $globalGrok"
if (Test-Path -LiteralPath $configSnippet) {
    Write-Host "  Config:  $configSnippet (merge into ~/.grok/config.toml)"
}
Write-Host ""
Write-Host "Verify agents: /config-agents - memory-daydream and memory-associator should appear."
Write-Host "Restart Grok sessions (or press r in /hooks) to load hooks."

# Install LJM Grok hooks, skills, agents, and global rules.
# Usage: .\grok\install.ps1 [-VaultRoot <path>] [-Uninstall]

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

# Hooks — substitute vault root into template
Ensure-Dir (Join-Path $grokHome "hooks")
$hookTemplate = Join-Path $VaultRoot ".grok/hooks/ljm.json"
$hookContent = Get-Content -LiteralPath $hookTemplate -Raw
$hookContent = $hookContent -replace '__JM_VAULT_ROOT__', $VaultRoot
Set-Content -LiteralPath $hookDest -Value $hookContent -Encoding UTF8

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
Write-Host "  Skills:  $skillsDest\memory-*"
Write-Host "  Agents:  $agentsDest\memory-*.md"
Write-Host "  Rules:   $globalGrok"
if (Test-Path -LiteralPath $configSnippet) {
    Write-Host "  Config:  $configSnippet (merge into ~/.grok/config.toml)"
}
Write-Host ""
Write-Host "Verify agents: /config-agents - memory-daydream and memory-associator should appear."
Write-Host "Restart Grok sessions (or press r in /hooks) to load hooks."
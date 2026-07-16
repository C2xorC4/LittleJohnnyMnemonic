# Platform-aware LJM Grok hook runner (native Windows PowerShell).
# Unix / Git Bash: use grok/bin/run-hook instead.
# Usage: run-hook.ps1 <session-start|user-prompt-submit|pre-tool-use|stop>
param(
    [Parameter(Mandatory = $true, Position = 0)]
    [ValidateSet("session-start", "user-prompt-submit", "pre-tool-use", "stop")]
    [string]$Event
)

$ErrorActionPreference = "Stop"

$vaultRoot = $env:JM_VAULT_ROOT
if (-not $vaultRoot) {
    $vaultRoot = (Resolve-Path (Join-Path $PSScriptRoot "..\..")).Path
}

# Prefer native Windows binary; fall back to Unix names (WSL/odd layouts).
$candidates = @(
    (Join-Path $vaultRoot "jm.exe"),
    (Join-Path $vaultRoot "agent\jm.exe"),
    (Join-Path $vaultRoot "jm"),
    (Join-Path $vaultRoot "agent\jm")
)

$jm = $null
foreach ($candidate in $candidates) {
    if (Test-Path -LiteralPath $candidate) {
        $jm = $candidate
        break
    }
}

if (-not $jm) {
    Write-Error "jm.exe not found under $vaultRoot. Build with: cd agent; go build -o jm.exe ."
}

$env:JM_VAULT_ROOT = $vaultRoot
& $jm hook $Event
exit $LASTEXITCODE

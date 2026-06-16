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

# Vault-root jm.exe symlinks to agent/jm.exe; fall back to agent path if symlink absent.
$jm = Join-Path $vaultRoot "jm.exe"
if (-not (Test-Path -LiteralPath $jm)) {
    $jm = Join-Path $vaultRoot "agent\jm.exe"
}
if (-not (Test-Path -LiteralPath $jm)) {
    Write-Error "jm.exe not found. Build with: cd agent; go build -o jm.exe . (symlink vault-root jm.exe -> agent/jm.exe optional)"
}

& $jm hook $Event
exit $LASTEXITCODE
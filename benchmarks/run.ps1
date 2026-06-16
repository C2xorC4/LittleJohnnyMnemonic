# LJM benchmark runner helper — validates fixture and scaffolds a run.
param(
    [ValidateSet("validate", "retrieve-check", "init-run", "list")]
    [string]$Action = "validate",
    [string]$BenchmarkRoot = (Join-Path $PSScriptRoot ""),
    [string]$Host = "grok",
    [string]$Arm = "grok-ljm-on",
    [string]$Task = ""
)

$ErrorActionPreference = "Stop"
$env:JM_BENCHMARK_ROOT = $BenchmarkRoot

$jm = Join-Path (Split-Path $PSScriptRoot -Parent) "jm.exe"
if (-not (Test-Path $jm)) {
    $jm = Join-Path (Join-Path $PSScriptRoot "..\agent") "jm.exe"
}
if (-not (Test-Path $jm)) {
    Write-Error "jm.exe not found. Build with: cd agent; go build -o jm.exe ."
}

switch ($Action) {
    "validate" {
        & $jm benchmark validate --root $BenchmarkRoot
    }
    "retrieve-check" {
        $args = @("benchmark", "retrieve-check", "--root", $BenchmarkRoot)
        if ($Task) { $args += @("--task", $Task) }
        & $jm @args
    }
    "list" {
        & $jm benchmark list --root $BenchmarkRoot
    }
    "init-run" {
        & $jm benchmark init-run --root $BenchmarkRoot --host $Host --arm $Arm
    }
}
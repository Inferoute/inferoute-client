# Requires -Version 5.1
# Native Windows build. Mirrors scripts/build.sh.
#
#   powershell -NoProfile -ExecutionPolicy Bypass -File scripts\build.ps1
#   scripts\build.bat

$ErrorActionPreference = "Stop"

function Write-Info([string]$Message) { Write-Host $Message -ForegroundColor Cyan }
function Write-Ok([string]$Message) { Write-Host $Message -ForegroundColor Green }
function Write-Err([string]$Message) { Write-Host $Message -ForegroundColor Red }

function GitValue {
    param(
        [string[]]$GitArgs,
        [string]$Fallback
    )
    if (-not (Get-Command git -ErrorAction SilentlyContinue)) {
        return $Fallback
    }
    $prev = $ErrorActionPreference
    $ErrorActionPreference = "Continue"
    try {
        $out = & git @GitArgs 2>$null
        if ($LASTEXITCODE -ne 0 -or [string]::IsNullOrWhiteSpace("$out")) {
            return $Fallback
        }
        return ("$out").Trim()
    } finally {
        $ErrorActionPreference = $prev
    }
}

$RepoRoot = Resolve-Path (Join-Path $PSScriptRoot "..")
Set-Location $RepoRoot

if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    Write-Err "go is not on PATH. Install Go 1.22+ from https://go.dev/dl/"
    exit 1
}

$version = GitValue -GitArgs @("describe", "--tags", "--always", "--dirty") -Fallback "dev"
$commit = GitValue -GitArgs @("rev-parse", "--short", "HEAD") -Fallback "none"
$date = [DateTime]::UtcNow.ToString("yyyy-MM-ddTHH:mm:ssZ")
$out = Join-Path $RepoRoot "inferoute-client.exe"
$ldflags = "-X main.version=$version -X main.commit=$commit -X main.date=$date"

Write-Info "Building inferoute-client.exe"
Write-Host "Version: $version"
Write-Host "Commit:  $commit"
Write-Host "Date:    $date"
Write-Host "Go:      $(go version)"

& go build -ldflags $ldflags -o $out ./cmd
if ($LASTEXITCODE -ne 0) {
    Write-Err "go build failed"
    exit $LASTEXITCODE
}
if (-not (Test-Path -LiteralPath $out)) {
    Write-Err "go build succeeded but $out is missing"
    exit 1
}

Write-Ok "Build complete: $out"
Write-Host "Run: .\inferoute-client.exe"
Write-Host "Config: %USERPROFILE%\.config\inferoute\config.yaml"

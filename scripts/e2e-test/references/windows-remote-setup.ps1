# Requires -Version 5.1
# Run ON the Windows GCE instance (copied + invoked by run-e2e-windows.sh).
# Pulls inferoute-client, rebuilds the exe, writes config, starts Ollama + client.
#
# Usage:
#   powershell.exe -ExecutionPolicy Bypass -File windows-remote-setup.ps1 -ParamsFile .\inferoute-e2e-params.ps1
param(
    [Parameter(Mandatory = $true)]
    [string]$ParamsFile
)

$ErrorActionPreference = "Stop"
$ProgressPreference = "SilentlyContinue"

if (-not (Test-Path -LiteralPath $ParamsFile)) {
    throw "params file not found: $ParamsFile"
}
. $ParamsFile
if (-not $E2E) {
    throw "params file did not define `$E2E"
}

function Write-Info([string]$Message) { Write-Host "[win-e2e] $Message" -ForegroundColor Cyan }
function Write-Ok([string]$Message) { Write-Host "[win-e2e] $Message" -ForegroundColor Green }
function Write-Warn([string]$Message) { Write-Host "[win-e2e] $Message" -ForegroundColor Yellow }

function Add-PathIfExists([string]$Dir) {
    if ([string]::IsNullOrWhiteSpace($Dir)) { return }
    if (-not (Test-Path -LiteralPath $Dir)) { return }
    if ($env:Path -notlike "*$Dir*") {
        $env:Path = "$Dir;$env:Path"
    }
}

function Write-Utf8File([string]$Path, [string]$Content) {
    $utf8 = New-Object System.Text.UTF8Encoding $false
    [System.IO.File]::WriteAllText($Path, $Content, $utf8)
}

function Test-HttpOk([string]$Url) {
    try {
        $code = & curl.exe -sf -o NUL -w "%{http_code}" --max-time 8 $Url 2>$null
        return ($code -eq "200")
    } catch {
        return $false
    }
}

function Wait-HttpOk([string]$Name, [string]$Url, [int]$TimeoutSec, [int]$PollSec = 5) {
    $deadline = (Get-Date).AddSeconds($TimeoutSec)
    while ((Get-Date) -lt $deadline) {
        if (Test-HttpOk $Url) {
            Write-Ok "$Name ready"
            return
        }
        Write-Info "waiting for $Name..."
        Start-Sleep -Seconds $PollSec
    }
    throw "$Name not ready after ${TimeoutSec}s ($Url)"
}

function Stop-NamedProcess([string]$Name) {
    $procs = Get-Process -Name $Name -ErrorAction SilentlyContinue
    if ($procs) {
        Write-Info "stopping $Name (pids $($procs.Id -join ','))"
        $procs | Stop-Process -Force -ErrorAction SilentlyContinue
        Start-Sleep -Seconds 2
    }
}

# OpenSSH on Windows puts the session in a job with KILL_ON_JOB_CLOSE.
# Start-Process children stay in that job and die when this SSH session
# ends — which is exactly after this script returns "ready". CreateProcess
# with CREATE_BREAKAWAY_FROM_JOB is what the client's tray spawn uses.
if (-not ([System.Management.Automation.PSTypeName]"E2E.Breakaway").Type) {
    Add-Type -TypeDefinition @"
using System;
using System.Runtime.InteropServices;
using System.Text;
namespace E2E {
  [StructLayout(LayoutKind.Sequential, CharSet = CharSet.Unicode)]
  public struct STARTUPINFO {
    public int cb;
    public string lpReserved;
    public string lpDesktop;
    public string lpTitle;
    public int dwX, dwY, dwXSize, dwYSize, dwXCountChars, dwYCountChars, dwFillAttribute, dwFlags;
    public short wShowWindow, cbReserved2;
    public IntPtr lpReserved2, hStdInput, hStdOutput, hStdError;
  }
  [StructLayout(LayoutKind.Sequential)]
  public struct PROCESS_INFORMATION {
    public IntPtr hProcess, hThread;
    public int dwProcessId, dwThreadId;
  }
  public static class Breakaway {
    public const uint CREATE_NEW_PROCESS_GROUP = 0x00000200;
    public const uint CREATE_NO_WINDOW = 0x08000000;
    public const uint CREATE_BREAKAWAY_FROM_JOB = 0x01000000;
    public const uint DETACHED_PROCESS = 0x00000008;
    [DllImport("kernel32.dll", CharSet = CharSet.Unicode, SetLastError = true)]
    public static extern bool CreateProcess(
      string app, StringBuilder cmd, IntPtr pa, IntPtr ta, bool inherit,
      uint flags, IntPtr env, string cwd, ref STARTUPINFO si, out PROCESS_INFORMATION pi);
    [DllImport("kernel32.dll", SetLastError = true)] public static extern bool CloseHandle(IntPtr h);
  }
}
"@
}

function Start-BreakawayProcess {
    param(
        [Parameter(Mandatory = $true)][string]$FilePath,
        [string]$ArgumentList = "",
        [string]$WorkingDirectory = "",
        [string]$RedirectStandardOutput = "",
        [string]$RedirectStandardError = ""
    )
    if ([string]::IsNullOrWhiteSpace($WorkingDirectory)) {
        $WorkingDirectory = Split-Path -Parent $FilePath
    }
    $app = $FilePath
    $cmdLine = "`"$FilePath`""
    if (-not [string]::IsNullOrWhiteSpace($ArgumentList)) {
        $cmdLine = "$cmdLine $ArgumentList"
    }
    if ($RedirectStandardOutput -or $RedirectStandardError) {
        $launcherDir = Split-Path -Parent $RedirectStandardOutput
        if ([string]::IsNullOrWhiteSpace($launcherDir)) { $launcherDir = $WorkingDirectory }
        New-Item -ItemType Directory -Force -Path $launcherDir | Out-Null
        $launcher = Join-Path $launcherDir ("e2e-launch-{0}.cmd" -f [IO.Path]::GetFileNameWithoutExtension($FilePath))
        $out = if ($RedirectStandardOutput) { "> `"$RedirectStandardOutput`"" } else { "" }
        $err = if ($RedirectStandardError) { "2> `"$RedirectStandardError`"" } else { "" }
        $batch = @(
            "@echo off"
            "cd /d `"$WorkingDirectory`""
            "`"$FilePath`" $ArgumentList $out $err"
        )
        $utf8 = New-Object System.Text.UTF8Encoding $false
        [System.IO.File]::WriteAllLines($launcher, $batch, $utf8)
        $app = Join-Path $env:SystemRoot "System32\cmd.exe"
        $cmdLine = "`"$app`" /c `"$launcher`""
    }
    $flagSets = @(
        ([E2E.Breakaway]::CREATE_NEW_PROCESS_GROUP -bor [E2E.Breakaway]::DETACHED_PROCESS -bor [E2E.Breakaway]::CREATE_NO_WINDOW -bor [E2E.Breakaway]::CREATE_BREAKAWAY_FROM_JOB),
        ([E2E.Breakaway]::CREATE_NEW_PROCESS_GROUP -bor [E2E.Breakaway]::CREATE_NO_WINDOW -bor [E2E.Breakaway]::CREATE_BREAKAWAY_FROM_JOB),
        ([E2E.Breakaway]::CREATE_NEW_PROCESS_GROUP -bor [E2E.Breakaway]::CREATE_NO_WINDOW)
    )
    $lastErr = 0
    foreach ($flags in $flagSets) {
        $si = New-Object E2E.STARTUPINFO
        $si.cb = [Runtime.InteropServices.Marshal]::SizeOf([type][E2E.STARTUPINFO])
        $pi = New-Object E2E.PROCESS_INFORMATION
        $sb = New-Object System.Text.StringBuilder $cmdLine
        $ok = [E2E.Breakaway]::CreateProcess($app, $sb, [IntPtr]::Zero, [IntPtr]::Zero, $false, $flags, [IntPtr]::Zero, $WorkingDirectory, [ref]$si, [ref]$pi)
        if ($ok) {
            [E2E.Breakaway]::CloseHandle($pi.hProcess) | Out-Null
            [E2E.Breakaway]::CloseHandle($pi.hThread) | Out-Null
            return
        }
        $lastErr = [Runtime.InteropServices.Marshal]::GetLastWin32Error()
    }
    throw "CreateProcess failed ($FilePath): Win32 $lastErr"
}

# Non-interactive SSH sessions don't load the user PATH. Prepend the usual
# install locations so git/go/ollama/cloudflared/nvidia-smi resolve.
Add-PathIfExists $E2E.GoBinDir
Add-PathIfExists "C:\Program Files\Go\bin"
Add-PathIfExists "C:\Program Files\Git\cmd"
Add-PathIfExists "C:\Program Files\Git\bin"
Add-PathIfExists (Join-Path $env:LOCALAPPDATA "Programs\Ollama")
Add-PathIfExists (Join-Path $env:LOCALAPPDATA "inferoute\bin")
Add-PathIfExists "C:\Program Files\NVIDIA Corporation\NVSMI"
Add-PathIfExists "C:\Windows\System32"

$clientDir = $E2E.ClientDir
$gitBranch = $E2E.GitBranch
$gitRepo = $E2E.GitRepo
$gitPull = $E2E.GitPull
$providerApiKey = $E2E.ProviderApiKey
$platformUrl = $E2E.PlatformUrl
$ollamaModel = $E2E.OllamaModel
$ollamaUrl = $E2E.OllamaUrl
$configFile = $E2E.ConfigFile
$logDir = $E2E.LogDir

if ([string]::IsNullOrWhiteSpace($clientDir)) { throw "ClientDir is required" }
if ([string]::IsNullOrWhiteSpace($providerApiKey)) { throw "ProviderApiKey is required" }
if ([string]::IsNullOrWhiteSpace($platformUrl)) { throw "PlatformUrl is required" }
if ([string]::IsNullOrWhiteSpace($gitBranch)) { $gitBranch = "main" }
if ([string]::IsNullOrWhiteSpace($gitRepo)) { $gitRepo = "https://github.com/Inferoute/inferoute-client.git" }
if ([string]::IsNullOrWhiteSpace($gitPull)) { $gitPull = "1" }
if ([string]::IsNullOrWhiteSpace($ollamaModel)) { $ollamaModel = "qwen3:0.6b" }
if ([string]::IsNullOrWhiteSpace($ollamaUrl)) { $ollamaUrl = "http://localhost:11434" }
if ([string]::IsNullOrWhiteSpace($configFile)) { $configFile = "config.yaml" }
if ([string]::IsNullOrWhiteSpace($logDir)) { $logDir = Join-Path $clientDir "logs" }

# Drop the params file immediately - it contains PROVIDER_API_KEY.
Remove-Item -LiteralPath $ParamsFile -Force -ErrorAction SilentlyContinue

New-Item -ItemType Directory -Force -Path $logDir | Out-Null

foreach ($tool in @("git", "go", "ollama", "curl.exe")) {
    if (-not (Get-Command $tool -ErrorAction SilentlyContinue)) {
        throw "$tool not found on PATH. Install it on the Windows VM before running e2e."
    }
}

Write-Info "git=$(git --version)  go=$(go version)  ollama=$(ollama --version 2>$null | Select-Object -First 1)"

# --- sync source -------------------------------------------------------------
$gitDir = Join-Path $clientDir ".git"
if (-not (Test-Path -LiteralPath $gitDir)) {
    if (Test-Path -LiteralPath $clientDir) {
        throw "$clientDir exists but is not a git checkout. Clone inferoute-client there or set WIN_CLIENT_DIR."
    }
    Write-Info "cloning $gitRepo -> $clientDir (branch $gitBranch)"
    $parent = Split-Path -Parent $clientDir
    if ($parent) { New-Item -ItemType Directory -Force -Path $parent | Out-Null }
    git clone --branch $gitBranch $gitRepo $clientDir
} elseif ($gitPull -eq "1") {
    Write-Info "fetching origin/$gitBranch in $clientDir"
    git -C $clientDir fetch --prune origin $gitBranch
    git -C $clientDir checkout $gitBranch
    git -C $clientDir reset --hard "origin/$gitBranch"
    Write-Ok "now at $(git -C $clientDir rev-parse --short HEAD) - $(git -C $clientDir log -1 --pretty=%s)"
} else {
    Write-Warn "CLIENT_GIT_PULL=0 - building current checkout at $clientDir"
}

# --- cloudflared (client launches it; must be on PATH) -----------------------
$cf = Get-Command cloudflared -ErrorAction SilentlyContinue
if (-not $cf) {
    $cfDir = Join-Path $env:LOCALAPPDATA "inferoute\bin"
    New-Item -ItemType Directory -Force -Path $cfDir | Out-Null
    $cfExe = Join-Path $cfDir "cloudflared.exe"
    Write-Info "cloudflared not on PATH - downloading to $cfExe"
    Invoke-WebRequest -Uri "https://github.com/cloudflare/cloudflared/releases/latest/download/cloudflared-windows-amd64.exe" -OutFile $cfExe -UseBasicParsing
    Add-PathIfExists $cfDir
}
Write-Ok "cloudflared: $((Get-Command cloudflared).Source)"

# --- build -------------------------------------------------------------------
Push-Location $clientDir
try {
    $version = git describe --tags --always --dirty 2>$null
    if (-not $version) { $version = "dev" }
    $commit = git rev-parse --short HEAD 2>$null
    if (-not $commit) { $commit = "none" }
    $date = [DateTime]::UtcNow.ToString("yyyy-MM-ddTHH:mm:ssZ")
    Write-Info "building inferoute-client.exe (version=$version commit=$commit)"
    $ldflags = "-X main.version=$version -X main.commit=$commit -X main.date=$date"
    go build -ldflags $ldflags -o inferoute-client.exe ./cmd
    if (-not (Test-Path -LiteralPath ".\inferoute-client.exe")) {
        throw "go build succeeded but inferoute-client.exe is missing"
    }
    Write-Ok "build complete: $((Resolve-Path .\inferoute-client.exe).Path)"
} finally {
    Pop-Location
}

# --- config (Ollama only; native Windows does not run vLLM) -----------------
$example = Join-Path $clientDir "config.yaml.example"
$configPath = Join-Path $clientDir $configFile
if (-not (Test-Path -LiteralPath $example)) {
    throw "config.yaml.example missing in $clientDir"
}
Copy-Item -LiteralPath $example -Destination $configPath -Force
$content = [System.IO.File]::ReadAllText($configPath)
$logDirYaml = ($logDir -replace '\\', '/')
$content = [regex]::Replace($content, '(?m)^(\s*port:\s*).*$', '${1}8080')
$content = [regex]::Replace($content, '(?m)^(\s*api_key:\s*).*$', "`${1}`"$providerApiKey`"")
$content = [regex]::Replace($content, '(?m)^(\s*url:\s*).*$', "`${1}`"$platformUrl`"")
$content = [regex]::Replace($content, '(?m)^(\s*provider_type:\s*).*$', '${1}"ollama"')
$content = [regex]::Replace($content, '(?m)^(\s*llm_url:\s*).*$', "`${1}`"$ollamaUrl`"")
$content = [regex]::Replace($content, '(?m)^(\s*log_dir:\s*).*$', "`${1}`"$logDirYaml`"")
Write-Utf8File $configPath $content
Write-Ok "wrote $configPath (provider_type=ollama url=$platformUrl)"

# --- Ollama ------------------------------------------------------------------
$ollamaTags = ($ollamaUrl.TrimEnd("/") + "/api/tags")
if (-not (Test-HttpOk $ollamaTags)) {
    Write-Info "starting ollama serve"
    $ollamaExe = (Get-Command ollama).Source
    $ollamaLog = Join-Path $logDir "ollama.log"
    Start-BreakawayProcess -FilePath $ollamaExe -ArgumentList "serve" -WorkingDirectory $clientDir `
        -RedirectStandardOutput $ollamaLog -RedirectStandardError (Join-Path $logDir "ollama.err.log")
    Wait-HttpOk "ollama" $ollamaTags 120
} else {
    Write-Ok "ollama already serving"
}

Write-Info "ensuring model $ollamaModel"
& ollama pull $ollamaModel
if ($LASTEXITCODE -ne 0) { throw "ollama pull $ollamaModel failed (exit $LASTEXITCODE)" }

$deadline = (Get-Date).AddSeconds(120)
$haveModel = $false
while ((Get-Date) -lt $deadline) {
    $tags = & curl.exe -sf --max-time 8 $ollamaTags 2>$null
    if ($tags -and ($tags -like "*$ollamaModel*")) {
        $haveModel = $true
        break
    }
    Start-Sleep -Seconds 5
}
if (-not $haveModel) { throw "ollama does not list $ollamaModel after pull" }
Write-Ok "ollama has $ollamaModel"

# --- inferoute-client --------------------------------------------------------
# --console: skip tray/systray (SSH sessions have no desktop). Must break
# away from the OpenSSH job — Start-Process is killed when this session ends.
Stop-NamedProcess "inferoute-client"
# Leftover tunnel from a previous run; the new client starts its own.
Stop-NamedProcess "cloudflared"

$exe = Join-Path $clientDir "inferoute-client.exe"
$clientLog = Join-Path $logDir "inferoute-client.log"
$clientErr = Join-Path $logDir "inferoute-client.err.log"
Write-Info "starting $exe --console --config $configPath (breakaway from SSH job)"
Start-BreakawayProcess -FilePath $exe -ArgumentList "--console --config `"$configPath`"" `
    -WorkingDirectory $clientDir `
    -RedirectStandardOutput $clientLog -RedirectStandardError $clientErr

Wait-HttpOk "inferoute-client" "http://127.0.0.1:8080/api/health" 180 5
Write-Ok "remote setup complete"

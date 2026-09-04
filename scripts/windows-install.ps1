# Requires -Version 5.1
# Inferoute Client Windows installation script.
# Mirrors scripts/install.sh: cloudflared + inferoute-client on PATH, config at ~/.config/inferoute.
#
# Usage:
#   $env:PROVIDER_API_KEY="your-key"; irm https://raw.githubusercontent.com/inferoute/inferoute-client/main/scripts/windows-install.ps1 | iex
#
# Optional env: PROVIDER_API_KEY, PROVIDER_TYPE, INFEROUTE_SKIP_SETUP=1

$ErrorActionPreference = "Stop"
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12

function Write-Info([string]$Message) { Write-Host $Message -ForegroundColor Cyan }
function Write-Ok([string]$Message) { Write-Host $Message -ForegroundColor Green }
function Write-Warn([string]$Message) { Write-Host $Message -ForegroundColor Yellow }
function Write-Err([string]$Message) { Write-Host $Message -ForegroundColor Red }

function Write-Utf8File([string]$Path, [string]$Content) {
    $utf8 = New-Object System.Text.UTF8Encoding $false
    [System.IO.File]::WriteAllText($Path, $Content, $utf8)
}

function Add-UserPath([string]$Dir) {
    $userPath = [Environment]::GetEnvironmentVariable("PATH", "User")
    if ([string]::IsNullOrEmpty($userPath)) {
        $userPath = ""
    }
    $parts = $userPath -split ";" | Where-Object { $_ -ne "" }
    if ($parts -contains $Dir) {
        return
    }
    $newPath = if ($userPath.Trim() -eq "") { $Dir } else { "$userPath;$Dir" }
    [Environment]::SetEnvironmentVariable("PATH", $newPath, "User")
    if ($env:PATH -notlike "*$Dir*") {
        $env:PATH = "$env:PATH;$Dir"
    }
}

if (-not [Environment]::Is64BitOperatingSystem) {
    Write-Err "32-bit Windows is not supported. Use a 64-bit OS."
    exit 1
}

$SkipSetup = $env:INFEROUTE_SKIP_SETUP -eq "1"

$ProviderApiKey = $env:PROVIDER_API_KEY
$ProviderType = $env:PROVIDER_TYPE
if ([string]::IsNullOrWhiteSpace($ProviderType)) {
    $ProviderType = "ollama"
}

$ServerPort = $env:SERVER_PORT
if ([string]::IsNullOrWhiteSpace($ServerPort)) {
    $ServerPort = "8080"
}

$LlmUrl = $env:LLM_URL
if ([string]::IsNullOrWhiteSpace($LlmUrl)) {
    if ($ProviderType -eq "vllm") {
        $LlmUrl = "http://127.0.0.1:8000"
    } elseif ($ProviderType -eq "freetoken") {
        $LlmUrl = "http://127.0.0.1:1919"
    } else {
        $LlmUrl = "http://127.0.0.1:11434"
    }
}

$HomeDir = $env:USERPROFILE
$BinDir = Join-Path $env:LOCALAPPDATA "inferoute\bin"
$ConfigDir = Join-Path $HomeDir ".config\inferoute"
$LogDir = Join-Path $HomeDir ".local\state\inferoute\log"
$GitHubRepo = "inferoute/inferoute-client"
$CloudflaredUrl = "https://github.com/cloudflare/cloudflared/releases/latest/download/cloudflared-windows-amd64.exe"
$ClientZipUrl = "https://github.com/$GitHubRepo/releases/latest/download/inferoute-client-windows-amd64.zip"

Write-Info "=== Inferoute Client Installation ==="
Write-Info "OS: Windows amd64"
Write-Info "Binaries: $BinDir"
Write-Info "Config:   $ConfigDir\config.yaml"

New-Item -ItemType Directory -Path $BinDir -Force | Out-Null
New-Item -ItemType Directory -Path $ConfigDir -Force | Out-Null
New-Item -ItemType Directory -Path $LogDir -Force | Out-Null

$CloudflaredExe = Join-Path $BinDir "cloudflared.exe"
Write-Info "Installing cloudflared..."
try {
    Invoke-WebRequest -Uri $CloudflaredUrl -OutFile $CloudflaredExe -UseBasicParsing
    Write-Ok "cloudflared installed."
} catch {
    Write-Err "Failed to download cloudflared: $_"
    Write-Warn "Install manually from https://github.com/cloudflare/cloudflared/releases or: winget install Cloudflare.cloudflared"
    exit 1
}

$ClientZip = Join-Path $env:TEMP "inferoute-client-windows-amd64.zip"
Write-Info "Downloading inferoute-client..."
try {
    Invoke-WebRequest -Uri $ClientZipUrl -OutFile $ClientZip -UseBasicParsing
} catch {
    Write-Err "Failed to download inferoute-client: $_"
    Write-Warn "Check releases at https://github.com/$GitHubRepo/releases"
    exit 1
}

$ExtractDir = Join-Path $env:TEMP "inferoute-client-extract"
if (Test-Path $ExtractDir) {
    Remove-Item $ExtractDir -Recurse -Force
}
New-Item -ItemType Directory -Path $ExtractDir -Force | Out-Null
Expand-Archive -Path $ClientZip -DestinationPath $ExtractDir -Force

$DownloadedExe = Get-ChildItem -Path $ExtractDir -Filter "inferoute-client*.exe" -Recurse | Select-Object -First 1
if (-not $DownloadedExe) {
    Write-Err "Zip did not contain inferoute-client.exe"
    exit 1
}
Copy-Item $DownloadedExe.FullName (Join-Path $BinDir "inferoute-client.exe") -Force
Remove-Item $ExtractDir -Recurse -Force -ErrorAction SilentlyContinue
Remove-Item $ClientZip -Force -ErrorAction SilentlyContinue
Write-Ok "inferoute-client installed."

Add-UserPath $BinDir
Write-Ok "Added $BinDir to your user PATH."

$exe = Join-Path $BinDir "inferoute-client.exe"
$ConfigPath = Join-Path $ConfigDir "config.yaml"

if ($SkipSetup) {
    if ([string]::IsNullOrWhiteSpace($ProviderApiKey)) {
        Write-Err "PROVIDER_API_KEY is required when INFEROUTE_SKIP_SETUP=1"
        exit 1
    }
    Write-Info "Downloading config template..."
    $ExampleUrl = "https://raw.githubusercontent.com/inferoute/inferoute-client/main/config.yaml.example"
    try {
        Invoke-WebRequest -Uri $ExampleUrl -OutFile $ConfigPath -UseBasicParsing
    } catch {
        Write-Err "Failed to download config.yaml.example: $_"
        exit 1
    }
    $content = [System.IO.File]::ReadAllText($ConfigPath)
    $content = [regex]::Replace($content, '(?m)^(\s*port:\s*).*$', "`${1}$ServerPort")
    $content = [regex]::Replace($content, '(?m)^(\s*api_key:\s*).*$', "`${1}`"$ProviderApiKey`"")
    $content = [regex]::Replace($content, '(?m)^(\s*provider_type:\s*).*$', "`${1}`"$ProviderType`"")
    $content = [regex]::Replace($content, '(?m)^(\s*llm_url:\s*).*$', "`${1}`"$LlmUrl`"")
    Write-Utf8File $ConfigPath $content
    Write-Ok "Configuration written."
} else {
    $setupArgs = @("setup", "--config", $ConfigPath)
    if (-not [string]::IsNullOrWhiteSpace($ProviderApiKey)) {
        $setupArgs += @("--api-key", $ProviderApiKey)
    }
    if (-not [string]::IsNullOrWhiteSpace($env:PROVIDER_TYPE)) {
        $setupArgs += @("--engine", $ProviderType)
    }
    Write-Info "Starting setup wizard..."
    & $exe @setupArgs
    if ($LASTEXITCODE -ne 0) {
        Write-Warn "Setup wizard exited with code $LASTEXITCODE. Re-run: inferoute-client setup"
    }
}

$Programs = [Environment]::GetFolderPath("Programs")
$ShortcutDir = Join-Path $Programs "Inferoute"
New-Item -ItemType Directory -Path $ShortcutDir -Force | Out-Null
$Wsh = New-Object -ComObject WScript.Shell
$Shortcut = $Wsh.CreateShortcut((Join-Path $ShortcutDir "Inferoute Client.lnk"))
$Shortcut.TargetPath = $exe
$Shortcut.Arguments = "--config `"$ConfigPath`""
$Shortcut.WorkingDirectory = $BinDir
$Shortcut.Description = "Start Inferoute Provider Client"
$Shortcut.IconLocation = "$exe,0"
$Shortcut.Save()
Write-Ok "Start Menu shortcut created."

Write-Host ""
Write-Ok "Installation complete."
Write-Info "Config: $ConfigPath"
Write-Info "Logs:   $LogDir"
Write-Host ""
Write-Info "Start the client:"
Write-Host "  1. Start Menu -> Inferoute -> Inferoute Client  (notification area)"
Write-Host "  OR"
Write-Host "  2. inferoute-client"
Write-Host "     (open a new terminal so PATH updates apply; the prompt returns and the client stays in the tray)"
Write-Host ""
Write-Info "Re-run the wizard anytime:  inferoute-client setup"
Write-Info "Right-click the Inferoute tray icon -> Open dashboard for live status."
Write-Host "  inferoute-client --console   (terminal UI instead of tray)"
Write-Host ""
Write-Warn "Windows: Ollama or FreeToken. Allow Windows Firewall if prompted. NVIDIA GPU metrics need nvidia-smi on PATH."
Write-Warn "Unsigned download: SmartScreen may show 'Windows protected your PC' -> More info -> Run anyway."

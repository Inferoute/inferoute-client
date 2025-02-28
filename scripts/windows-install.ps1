# Inferoute Client Windows Installation Script

# Set error action preference to stop on any error
$ErrorActionPreference = "Stop"

# Define colors for output
function Write-ColorOutput {
    param (
        [Parameter(Mandatory=$true)]
        [string]$Message,
        
        [Parameter(Mandatory=$false)]
        [string]$ForegroundColor = "White"
    )
    
    $originalColor = $host.UI.RawUI.ForegroundColor
    $host.UI.RawUI.ForegroundColor = $ForegroundColor
    Write-Output $Message
    $host.UI.RawUI.ForegroundColor = $originalColor
}

# Create installation directory
$InstallDir = Join-Path $env:USERPROFILE "inferoute-client"
if (-not (Test-Path $InstallDir)) {
    Write-ColorOutput "Creating installation directory: $InstallDir" "Cyan"
    New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
}

# Change to installation directory
Set-Location $InstallDir
Write-ColorOutput "Working in installation directory: $InstallDir" "Cyan"

# Detect architecture
$Architecture = if ([Environment]::Is64BitOperatingSystem) { "amd64" } else { "386" }
Write-ColorOutput "Detected OS: Windows, Architecture: $Architecture" "Cyan"

# Check if config.yaml exists
if (-not (Test-Path "config.yaml")) {
    Write-ColorOutput "config.yaml not found." "Yellow"
    
    # Check if config.yaml.example exists, download if not
    if (-not (Test-Path "config.yaml.example")) {
        Write-ColorOutput "Downloading config.yaml.example..." "Cyan"
        try {
            Invoke-WebRequest -Uri "https://raw.githubusercontent.com/Inferoute/inferoute-client/main/config.yaml.example" -OutFile "config.yaml.example"
        }
        catch {
            Write-ColorOutput "Failed to download config.yaml.example: $_" "Red"
            exit 1
        }
    }
    
    # Create config.yaml from example
    Write-ColorOutput "Creating config.yaml from example..." "Yellow"
    Copy-Item "config.yaml.example" "config.yaml"
    Write-ColorOutput "Please edit config.yaml to add your NGROK authtoken and other settings." "Yellow"
    Write-ColorOutput "You can do this by running: notepad $InstallDir\config.yaml" "Yellow"
    Write-ColorOutput "Press Enter to continue after editing the file..." "Yellow"
    Read-Host
}

# Check if NGROK authtoken is in config.yaml
$configContent = Get-Content "config.yaml" -Raw
$authTokenMatch = [regex]::Match($configContent, 'ngrok:(?:.*\n)*?\s+authtoken:\s*"([^"]*)"')
if (-not $authTokenMatch.Success -or [string]::IsNullOrWhiteSpace($authTokenMatch.Groups[1].Value)) {
    Write-ColorOutput "Error: NGROK authtoken not found in config.yaml" "Red"
    Write-ColorOutput "Please add 'authtoken: ""your_ngrok_authtoken_here""' under the ngrok section in config.yaml" "Red"
    Write-ColorOutput "You can do this by running: notepad $InstallDir\config.yaml" "Red"
    exit 1
}
$NgrokAuthToken = $authTokenMatch.Groups[1].Value

# Install NGROK if not already installed
$NgrokPath = Join-Path $env:USERPROFILE "AppData\Local\ngrok"
$NgrokExe = Join-Path $NgrokPath "ngrok.exe"

if (-not (Test-Path $NgrokExe)) {
    Write-ColorOutput "NGROK not found. Installing..." "Yellow"
    
    # Create temp directory
    $TempDir = Join-Path $env:TEMP "ngrok_install_$(Get-Random)"
    New-Item -ItemType Directory -Path $TempDir -Force | Out-Null
    Set-Location $TempDir
    
    # Download NGROK based on architecture
    $NgrokUrl = "https://bin.equinox.io/c/bNyj1mQVY4c/ngrok-v3-stable-windows-$Architecture.zip"
    Write-ColorOutput "Downloading NGROK from: $NgrokUrl" "Cyan"
    
    try {
        Invoke-WebRequest -Uri $NgrokUrl -OutFile "ngrok.zip"
        
        # Extract NGROK
        Write-ColorOutput "Extracting NGROK..." "Cyan"
        Expand-Archive -Path "ngrok.zip" -DestinationPath $NgrokPath -Force
        
        # Add NGROK to PATH
        $UserPath = [Environment]::GetEnvironmentVariable("PATH", "User")
        if (-not $UserPath.Contains($NgrokPath)) {
            Write-ColorOutput "Adding NGROK to PATH..." "Yellow"
            [Environment]::SetEnvironmentVariable("PATH", "$UserPath;$NgrokPath", "User")
            $env:PATH = "$env:PATH;$NgrokPath"
        }
        
        Write-ColorOutput "NGROK installed successfully." "Green"
    }
    catch {
        Write-ColorOutput "Failed to download or install NGROK: $_" "Red"
        Write-ColorOutput "Please install NGROK manually from https://ngrok.com/download" "Red"
        exit 1
    }
    
    # Clean up
    Set-Location $InstallDir
    Remove-Item -Path $TempDir -Recurse -Force
}
else {
    Write-ColorOutput "NGROK is already installed." "Green"
}

# Configure NGROK
Write-ColorOutput "Configuring NGROK..." "Cyan"
try {
    Start-Process -FilePath $NgrokExe -ArgumentList "config", "add-authtoken", $NgrokAuthToken -NoNewWindow -Wait
    Write-ColorOutput "NGROK configured successfully." "Green"
}
catch {
    Write-ColorOutput "Failed to configure NGROK: $_" "Red"
    exit 1
}

# Download inferoute-client binary
$InferouteExe = Join-Path $InstallDir "inferoute-client.exe"
if (-not (Test-Path $InferouteExe)) {
    Write-ColorOutput "Downloading inferoute-client binary..." "Cyan"
    
    # Set GitHub repository and latest release info
    $GitHubRepo = "inferoute/inferoute-client"
    $BinaryName = "inferoute-client-windows-$Architecture"
    $DownloadUrl = "https://github.com/$GitHubRepo/releases/latest/download/$BinaryName.zip"
    
    # Create temp directory
    $TempDir = Join-Path $env:TEMP "inferoute_install_$(Get-Random)"
    New-Item -ItemType Directory -Path $TempDir -Force | Out-Null
    Set-Location $TempDir
    
    Write-ColorOutput "Downloading from: $DownloadUrl" "Cyan"
    
    try {
        Invoke-WebRequest -Uri $DownloadUrl -OutFile "$BinaryName.zip"
        
        # Extract binary
        Write-ColorOutput "Extracting binary..." "Cyan"
        Expand-Archive -Path "$BinaryName.zip" -DestinationPath $TempDir -Force
        
        # Move binary to installation directory
        Move-Item -Path "$BinaryName.exe" -Destination $InferouteExe -Force
        
        Write-ColorOutput "inferoute-client downloaded successfully." "Green"
    }
    catch {
        Write-ColorOutput "Failed to download inferoute-client binary: $_" "Red"
        Write-ColorOutput "Please check if the release exists at: https://github.com/$GitHubRepo/releases" "Yellow"
        exit 1
    }
    
    # Clean up
    Set-Location $InstallDir
    Remove-Item -Path $TempDir -Recurse -Force
}
else {
    Write-ColorOutput "inferoute-client binary already exists." "Green"
}

# Create run directory
$RunDir = Join-Path $InstallDir "run"
if (-not (Test-Path $RunDir)) {
    New-Item -ItemType Directory -Path $RunDir -Force | Out-Null
}

# Create start script
$StartScript = @"
# Inferoute Client Start Script
`$ErrorActionPreference = "Stop"

# Get the directory of this script
`$ScriptDir = Split-Path -Parent `$MyInvocation.MyCommand.Path
`$RootDir = Split-Path -Parent `$ScriptDir
Set-Location `$RootDir

# Get server port from config.yaml
`$ConfigContent = Get-Content "config.yaml" -Raw
`$ServerPortMatch = [regex]::Match(`$ConfigContent, 'server:(?:.*\n)*?\s+port:\s*(\d+)')
`$ServerPort = if (`$ServerPortMatch.Success) { `$ServerPortMatch.Groups[1].Value } else { "8080" }
if (-not `$ServerPortMatch.Success) {
    Write-Output "Server port not found in config.yaml, using default: `$ServerPort"
}

# Start NGROK in background
Write-Output "Starting NGROK tunnel..."
`$NgrokProcess = Start-Process -FilePath "ngrok" -ArgumentList "http", `$ServerPort, "--log=stdout", "--host-header=localhost:`$ServerPort" -NoNewWindow -PassThru -RedirectStandardOutput (Join-Path `$ScriptDir "ngrok.log")

# Save PID for later cleanup
`$NgrokProcess.Id | Out-File -FilePath (Join-Path `$ScriptDir "ngrok.pid")

# Wait for NGROK to start
Write-Output "Waiting for NGROK to start..."
Start-Sleep -Seconds 5

# Get NGROK public URL
Write-Output "Getting NGROK public URL..."
`$NgrokPublicUrl = `$null
`$MaxAttempts = 30
`$Attempt = 0

while (-not `$NgrokPublicUrl -and `$Attempt -lt `$MaxAttempts) {
    `$Attempt++
    Write-Output "Trying to get NGROK public URL (attempt `$Attempt/`$MaxAttempts)..."
    try {
        `$NgrokApi = Invoke-RestMethod -Uri "http://localhost:4040/api/tunnels" -ErrorAction SilentlyContinue
        if (`$NgrokApi.tunnels.Count -gt 0) {
            `$NgrokPublicUrl = `$NgrokApi.tunnels[0].public_url
        }
    }
    catch {
        # API not ready yet
    }
    
    if (-not `$NgrokPublicUrl) {
        Write-Output "NGROK not ready yet, waiting..."
        Start-Sleep -Seconds 2
    }
}

if (-not `$NgrokPublicUrl) {
    Write-Output "Failed to get NGROK public URL after `$MaxAttempts attempts."
    Write-Output "Check run/ngrok.log for details."
    Write-Output "Stopping NGROK..."
    Stop-Process -Id `$NgrokProcess.Id -Force
    exit 1
}

Write-Output "NGROK public URL: `$NgrokPublicUrl"

# Update config.yaml with NGROK URL
`$ConfigContent = `$ConfigContent -replace '(ngrok:(?:.*\n)*?\s+url:\s*")[^"]*(")', "`${1}`$NgrokPublicUrl`${2}"
`$ConfigContent | Out-File -FilePath "config.yaml" -Encoding utf8

# Start inferoute-client
Write-Output "Starting inferoute-client..."
Start-Process -FilePath (Join-Path `$RootDir "inferoute-client.exe") -ArgumentList "-config", "config.yaml" -NoNewWindow -Wait
"@

$StartScript | Out-File -FilePath (Join-Path $RunDir "start.ps1") -Encoding utf8

# Create stop script
$StopScript = @"
# Inferoute Client Stop Script
`$ErrorActionPreference = "Stop"

# Get the directory of this script
`$ScriptDir = Split-Path -Parent `$MyInvocation.MyCommand.Path
Set-Location `$ScriptDir

# Check if NGROK is running
if (Test-Path "ngrok.pid") {
    `$NgrokPid = Get-Content "ngrok.pid"
    try {
        `$NgrokProcess = Get-Process -Id `$NgrokPid -ErrorAction SilentlyContinue
        if (`$NgrokProcess) {
            Write-Output "Stopping NGROK (PID: `$NgrokPid)..."
            Stop-Process -Id `$NgrokPid -Force
        }
        else {
            Write-Output "NGROK is not running."
        }
    }
    catch {
        Write-Output "NGROK is not running."
    }
    Remove-Item "ngrok.pid" -Force
}
else {
    Write-Output "NGROK PID file not found."
}

# Find and kill inferoute-client process
`$InferouteProcess = Get-Process -Name "inferoute-client" -ErrorAction SilentlyContinue
if (`$InferouteProcess) {
    Write-Output "Stopping inferoute-client (PID: `$(`$InferouteProcess.Id))..."
    Stop-Process -Id `$InferouteProcess.Id -Force
}
else {
    Write-Output "inferoute-client is not running."
}

Write-Output "All processes stopped."
"@

$StopScript | Out-File -FilePath (Join-Path $RunDir "stop.ps1") -Encoding utf8

# Create batch wrappers for PowerShell scripts
$StartBat = @"
@echo off
powershell -ExecutionPolicy Bypass -File "%~dp0start.ps1"
"@

$StopBat = @"
@echo off
powershell -ExecutionPolicy Bypass -File "%~dp0stop.ps1"
"@

$StartBat | Out-File -FilePath (Join-Path $RunDir "start.bat") -Encoding ascii
$StopBat | Out-File -FilePath (Join-Path $RunDir "stop.bat") -Encoding ascii

# Create desktop shortcuts
$WshShell = New-Object -ComObject WScript.Shell
$StartupPath = [Environment]::GetFolderPath('Desktop')

$Shortcut = $WshShell.CreateShortcut((Join-Path $StartupPath "Inferoute Client - Start.lnk"))
$Shortcut.TargetPath = Join-Path $RunDir "start.bat"
$Shortcut.WorkingDirectory = $RunDir
$Shortcut.Description = "Start Inferoute Client"
$Shortcut.Save()

$Shortcut = $WshShell.CreateShortcut((Join-Path $StartupPath "Inferoute Client - Stop.lnk"))
$Shortcut.TargetPath = Join-Path $RunDir "stop.bat"
$Shortcut.WorkingDirectory = $RunDir
$Shortcut.Description = "Stop Inferoute Client"
$Shortcut.Save()

# Installation complete
Write-ColorOutput "Installation complete!" "Green"
Write-ColorOutput "Inferoute Client has been installed to: $InstallDir" "Cyan"
Write-ColorOutput "Desktop shortcuts have been created for starting and stopping the client." "Cyan"
Write-ColorOutput "To start inferoute-client with NGROK:" "Cyan"
Write-ColorOutput "  1. Double-click the 'Inferoute Client - Start' shortcut on your desktop" "White"
Write-ColorOutput "  OR" "Yellow"
Write-ColorOutput "  2. Run: $RunDir\start.bat" "White"
Write-ColorOutput "To stop all services:" "Cyan"
Write-ColorOutput "  1. Double-click the 'Inferoute Client - Stop' shortcut on your desktop" "White"
Write-ColorOutput "  OR" "Yellow"
Write-ColorOutput "  2. Run: $RunDir\stop.bat" "White"
Write-ColorOutput "Note: NGROK admin interface will be available at http://localhost:4040" "Yellow" 
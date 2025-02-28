@echo off
setlocal enabledelayedexpansion

echo === Inferoute Client Windows Installer ===
echo.

:: Check for PowerShell
where powershell >nul 2>&1
if %ERRORLEVEL% neq 0 (
    echo ERROR: PowerShell is required but not found on this system.
    echo Please install PowerShell from https://docs.microsoft.com/en-us/powershell/
    pause
    exit /b 1
)

:: Download the PowerShell script if it doesn't exist
if not exist "%~dp0windows-install.ps1" (
    echo Downloading installation script...
    powershell -Command "& {Invoke-WebRequest -Uri 'https://raw.githubusercontent.com/Inferoute/inferoute-client/main/scripts/windows-install.ps1' -OutFile '%~dp0windows-install.ps1'}"
    if %ERRORLEVEL% neq 0 (
        echo Failed to download the installation script.
        pause
        exit /b 1
    )
)

:: Check if running as administrator
net session >nul 2>&1
if %ERRORLEVEL% neq 0 (
    echo This script requires administrative privileges.
    echo.
    echo Please right-click on this batch file and select "Run as administrator".
    pause
    exit /b 1
)

:: Run the PowerShell script with bypass execution policy
echo Running installation script...
powershell -ExecutionPolicy Bypass -File "%~dp0windows-install.ps1"

if %ERRORLEVEL% neq 0 (
    echo.
    echo Installation failed. Please check the error messages above.
) else (
    echo.
    echo Installation completed successfully!
)

pause
exit /b 
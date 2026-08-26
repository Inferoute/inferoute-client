@echo off
setlocal
echo === Inferoute Client Windows Installer ===
echo.

where powershell >nul 2>&1
if %ERRORLEVEL% neq 0 (
    echo ERROR: PowerShell is required.
    echo Install PowerShell from https://learn.microsoft.com/powershell/
    pause
    exit /b 1
)

if exist "%~dp0windows-install.ps1" (
    powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0windows-install.ps1"
) else (
    powershell -NoProfile -ExecutionPolicy Bypass -Command "[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12; iex (Invoke-WebRequest -UseBasicParsing -Uri 'https://raw.githubusercontent.com/inferoute/inferoute-client/main/scripts/windows-install.ps1').Content"
)

if %ERRORLEVEL% neq 0 (
    echo.
    echo Installation failed. See messages above.
    pause
    exit /b 1
)

echo.
echo Installation completed successfully.
pause
exit /b 0

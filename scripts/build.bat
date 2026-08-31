@echo off
setlocal
echo === Inferoute Client Windows Build ===
echo.

where powershell >nul 2>&1
if %ERRORLEVEL% neq 0 (
    echo ERROR: PowerShell is required.
    pause
    exit /b 1
)

if not exist "%~dp0build.ps1" (
    echo ERROR: build.ps1 not found next to this script.
    pause
    exit /b 1
)

powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0build.ps1"
if %ERRORLEVEL% neq 0 (
    echo.
    echo Build failed. See messages above.
    pause
    exit /b 1
)

echo.
pause
exit /b 0

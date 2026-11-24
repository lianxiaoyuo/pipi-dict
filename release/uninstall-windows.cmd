@echo off
chcp 65001 >nul
setlocal
echo Exit Pipi Dict via Ctrl+Alt+Q before uninstalling.
set "TARGET=%LOCALAPPDATA%\Programs\PipiDict"
powershell -NoProfile -ExecutionPolicy Bypass -Command "$p=Get-Process -ErrorAction SilentlyContinue | Where-Object { $_.ProcessName -match '^(HoverDict|PipiDict)' }; if ($p) { exit 2 } else { exit 0 }"
if errorlevel 1 (
  echo App is still running. Exit it first.
  pause
  exit /b 2
)
reg delete "HKCU\Software\Microsoft\Windows\CurrentVersion\Run" /v HoverDict /f >nul 2>&1
if exist "%APPDATA%\Microsoft\Windows\Start Menu\Programs\Pipi Dict.lnk" del "%APPDATA%\Microsoft\Windows\Start Menu\Programs\Pipi Dict.lnk"
if exist "%TARGET%\PipiDict.exe" del "%TARGET%\PipiDict.exe"
echo App removed. Saved words were NOT deleted.

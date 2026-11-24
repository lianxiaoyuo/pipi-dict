@echo off
chcp 65001 >nul
setlocal
set "TARGET=%LOCALAPPDATA%\Programs\PipiDict"
echo [Pipi Dict 5.0] Installing...
powershell -NoProfile -ExecutionPolicy Bypass -Command "$p=Get-Process -ErrorAction SilentlyContinue | Where-Object { $_.ProcessName -match '^(HoverDict|PipiDict)' }; if ($p) { exit 2 } else { exit 0 }"
if errorlevel 1 (
  echo Pipi Dict is still running. Press Ctrl+Alt+Q to exit the old version first.
  pause
  exit /b 2
)
if not exist "%TARGET%" mkdir "%TARGET%" || goto :fail
copy /Y "%~dp0PipiDict.exe" "%TARGET%\PipiDict.exe" >nul || goto :fail
powershell -NoProfile -ExecutionPolicy Bypass -Command "$w=New-Object -ComObject WScript.Shell; $s=$w.CreateShortcut([Environment]::GetFolderPath('Programs')+'\Pipi Dict.lnk'); $s.TargetPath=$env:LOCALAPPDATA+'\Programs\PipiDict\PipiDict.exe'; $s.WorkingDirectory=$env:LOCALAPPDATA+'\Programs\PipiDict'; $s.Save()" || goto :fail
start "" "%TARGET%\PipiDict.exe"
echo Installed successfully. Your saved words are kept in the user profile.
exit /b 0
:fail
echo Installation failed. Existing saved words were not changed.
pause
exit /b 1

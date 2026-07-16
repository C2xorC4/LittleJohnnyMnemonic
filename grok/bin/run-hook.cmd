@echo off
REM Platform entry for native Windows (cmd.exe / CreateProcess PATHEXT).
REM Delegates to run-hook.ps1. Unix uses grok/bin/run-hook (bash).
setlocal
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0run-hook.ps1" %*
exit /b %ERRORLEVEL%

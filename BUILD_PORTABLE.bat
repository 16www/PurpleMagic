@echo off
setlocal
cd /d "%~dp0"
python deploy\windows\build_portable_launcher.py %*
exit /b %errorlevel%

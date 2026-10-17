@echo off
setlocal
cd /d "%~dp0"
python deploy\windows\build_flutter_launcher.py %*
exit /b %errorlevel%

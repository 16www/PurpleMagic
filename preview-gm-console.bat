@echo off
cd /d "%~dp0gm-console"
if not exist node_modules call npm install
call npm run dev -- --host 127.0.0.1 --port 8778 --strictPort

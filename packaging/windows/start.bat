@echo off
rem Double-clicking this file starts the server. It runs in its own window so
rem that the log stays readable and closing that window is how the server is
rem stopped; `cmd /k` keeps the window up even when the server exits at once,
rem which is the only way a startup error is ever seen.
cd /d "%~dp0"
start "wfeature server" cmd /k wfeature-server.exe %*

rem The server binds a moment after it is launched, so the page is opened after
rem a short wait rather than into a port that is not listening yet.
timeout /t 2 /nobreak >nul
start "" "http://127.0.0.1:11541"

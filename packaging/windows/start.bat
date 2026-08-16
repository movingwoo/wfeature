@echo off
rem Double-clicking this file starts the server. It runs in its own window so
rem that the log stays readable and closing that window is how the server is
rem stopped; `cmd /k` keeps the window up even when the server exits at once,
rem which is the only way a startup error is ever seen.
rem
rem The page is opened by the server's own -open once the port is answering,
rem rather than here after a guessed wait.
cd /d "%~dp0"
start "wfeature server" cmd /k wfeature-server.exe -open %*

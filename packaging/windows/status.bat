@echo off
rem Double-clicking this file says whether the server is up and on which port.
rem The window is held open with pause, because a double-clicked .bat closes the
rem moment it ends and the answer would never be read.
cd /d "%~dp0"
wfeature-server.exe status %*
pause

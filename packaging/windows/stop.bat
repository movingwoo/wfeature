@echo off
rem Double-clicking this file stops the server. Closing the server's own window
rem does the same thing; this is for the window that was closed without stopping
rem it, or a server still running from an earlier login.
rem
rem Nothing is killed on the strength of a port alone: the server is asked what
rem it is first, and a stranger holding the port is reported and left alone.
rem That rule lives in the binary now, so this window is a line rather than a
rem netstat-and-tasklist reimplementation of what the other systems do.
cd /d "%~dp0"
wfeature-server.exe stop %*
pause

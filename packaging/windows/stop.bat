@echo off
rem Double-clicking this file stops the server. Closing the server's own window
rem does the same thing; this is for the window that was closed without stopping
rem it, or a server still running from an earlier login.
rem
rem Nothing is killed on the strength of a port alone: the process holding it has
rem to be wfeature-server.exe, and anything else is reported and left alone.
setlocal
cd /d "%~dp0"

set PORT=%1
if "%PORT%"=="" set PORT=11541

set PID=
for /f "tokens=5" %%p in ('netstat -ano -p tcp ^| findstr /r /c:":%PORT% .*LISTENING"') do set PID=%%p
if "%PID%"=="" goto notrunning

tasklist /fi "PID eq %PID%" /fo csv /nh | find /i "wfeature-server.exe" >nul
if errorlevel 1 goto notours

echo wfeature 서버를 멈춘다 (pid %PID%).
rem 저장 중이면 마무리하고 끝나도록 먼저 정상 종료를 요청한다.
taskkill /pid %PID% >nul 2>&1
timeout /t 3 /nobreak >nul
tasklist /fi "PID eq %PID%" /fo csv /nh | find /i "wfeature-server.exe" >nul
if errorlevel 1 goto stopped

echo 정상 종료에 응답하지 않아 강제로 멈춘다.
taskkill /f /pid %PID% >nul 2>&1

:stopped
echo 멈췄다.
goto done

:notours
echo 포트 %PORT% 를 쓰는 것은 wfeature 서버가 아니다. 건드리지 않는다.
goto done

:notrunning
echo 포트 %PORT% 에는 wfeature 서버가 없다. 이미 멈춰 있다.

:done
pause

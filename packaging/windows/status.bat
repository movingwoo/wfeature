@echo off
rem Double-clicking this file says whether the server is up and on which port.
rem The window is held open with pause, because a double-clicked .bat closes the
rem moment it ends and the answer would never be read.
rem
rem Who holds the port is asked first and what it is second: a stranger on the
rem port answers "not found" to a request meant for this server, which reads the
rem same as no answer at all.
setlocal
cd /d "%~dp0"

set PORT=%1
if "%PORT%"=="" set PORT=11541

set PID=
for /f "tokens=5" %%p in ('netstat -ano -p tcp ^| findstr /r /c:":%PORT% .*LISTENING"') do set PID=%%p
if "%PID%"=="" goto notrunning

rem curl ships with Windows 10 and later. Without it the image name answers.
where curl >nul 2>&1
if errorlevel 1 goto byname

curl -s --max-time 2 "http://127.0.0.1:%PORT%/api/status" > "%TEMP%\wfeature-status.txt" 2>nul
find """server"":""wfeature""" "%TEMP%\wfeature-status.txt" >nul 2>&1
if errorlevel 1 goto notours

echo wfeature 서버가 돌고 있다.
echo   주소     http://127.0.0.1:%PORT%
type "%TEMP%\wfeature-status.txt"
echo.
echo   멈추기   stop.bat
goto done

:byname
tasklist /fi "PID eq %PID%" /fo csv /nh | find /i "wfeature-server.exe" >nul
if errorlevel 1 goto notours
echo wfeature 서버가 돌고 있다 (pid %PID%).
echo   주소     http://127.0.0.1:%PORT%
echo   멈추기   stop.bat
goto done

:notours
echo 포트 %PORT% 를 쓰는 것이 있지만 wfeature 서버가 아니다 (pid %PID%).
goto done

:notrunning
echo 포트 %PORT% 에는 wfeature 서버가 없다. start.bat 으로 띄운다.

:done
del "%TEMP%\wfeature-status.txt" 2>nul
pause

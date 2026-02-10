@echo off
REM ============================================
REM Antigravity Claude Proxy - Windows Setup
REM ============================================

setlocal enabledelayedexpansion

REM Configuration
set "INSTALL_DIR=%ProgramFiles%\AntigravityProxy"
set "CONFIG_DIR=%USERPROFILE%\.config\antigravity-proxy"
set "SERVICE_NAME=AntigravityProxy"

REM Colors (Windows 10+)
for /F %%a in ('echo prompt $E ^| cmd') do set "ESC=%%a"

echo.
echo ============================================
echo   Antigravity Claude Proxy - Windows Setup
echo ============================================
echo.

REM Check for admin rights
net session >nul 2>&1
if %errorlevel% neq 0 (
    echo [ERROR] This script requires administrator privileges.
    echo Right-click and select "Run as administrator"
    pause
    exit /b 1
)

REM Parse arguments
if "%1"=="uninstall" goto :uninstall
if "%1"=="service" goto :install_service
goto :install

:install
echo [INFO] Installing Antigravity Claude Proxy...
echo.

REM Create install directory
if not exist "%INSTALL_DIR%" (
    echo [INFO] Creating install directory...
    mkdir "%INSTALL_DIR%"
    mkdir "%INSTALL_DIR%\public"
)

REM Check for binary
set "BINARY_SRC="
if exist "go-backend\build\antigravity-proxy.exe" (
    set "BINARY_SRC=go-backend\build\antigravity-proxy.exe"
) else if exist "antigravity-proxy.exe" (
    set "BINARY_SRC=antigravity-proxy.exe"
) else (
    echo [ERROR] Binary not found!
    echo Please build first: cd go-backend ^&^& go build -o build\antigravity-proxy.exe .\cmd\server
    pause
    exit /b 1
)

echo [INFO] Copying binary from %BINARY_SRC%...
copy /Y "%BINARY_SRC%" "%INSTALL_DIR%\antigravity-proxy.exe" >nul

REM Copy public directory
if exist "public" (
    echo [INFO] Copying frontend assets...
    xcopy /E /I /Y "public" "%INSTALL_DIR%\public" >nul
)

REM Create config directory
if not exist "%CONFIG_DIR%" (
    echo [INFO] Creating config directory...
    mkdir "%CONFIG_DIR%"
)

REM Create default config
if not exist "%CONFIG_DIR%\config.json" (
    echo [INFO] Creating default config...
    (
        echo {
        echo   "apiKey": "",
        echo   "webuiPassword": "",
        echo   "devMode": false,
        echo   "maxRetries": 5,
        echo   "maxAccounts": 100,
        echo   "accountSelection": {
        echo     "strategy": "hybrid"
        echo   },
        echo   "port": 8080,
        echo   "host": "0.0.0.0"
        echo }
    ) > "%CONFIG_DIR%\config.json"
)

echo.
echo [SUCCESS] Installation complete!
echo.
echo   Install Dir: %INSTALL_DIR%
echo   Config Dir:  %CONFIG_DIR%
echo.
echo   To run manually:
echo     cd "%INSTALL_DIR%"
echo     antigravity-proxy.exe
echo.
echo   To install as Windows service:
echo     %~f0 service
echo.
goto :end

:install_service
echo [INFO] Installing Windows service...
echo.

REM Check for NSSM
where nssm >nul 2>&1
if %errorlevel% neq 0 (
    echo [WARN] NSSM not found in PATH.
    echo.
    echo Please download NSSM from: https://nssm.cc/download
    echo Then run:
    echo   nssm install %SERVICE_NAME% "%INSTALL_DIR%\antigravity-proxy.exe"
    echo   nssm set %SERVICE_NAME% AppDirectory "%INSTALL_DIR%"
    echo   nssm start %SERVICE_NAME%
    echo.
    pause
    exit /b 1
)

REM Remove existing service
nssm status %SERVICE_NAME% >nul 2>&1
if %errorlevel% equ 0 (
    echo [INFO] Removing existing service...
    nssm stop %SERVICE_NAME% >nul 2>&1
    nssm remove %SERVICE_NAME% confirm >nul 2>&1
)

REM Install service
echo [INFO] Installing service with NSSM...
nssm install %SERVICE_NAME% "%INSTALL_DIR%\antigravity-proxy.exe"
nssm set %SERVICE_NAME% AppDirectory "%INSTALL_DIR%"
nssm set %SERVICE_NAME% AppParameters "--strategy=hybrid"
nssm set %SERVICE_NAME% DisplayName "Antigravity Claude Proxy"
nssm set %SERVICE_NAME% Description "Anthropic-compatible API proxy for Google Cloud Code"
nssm set %SERVICE_NAME% Start SERVICE_AUTO_START
nssm set %SERVICE_NAME% AppStdout "%INSTALL_DIR%\logs\stdout.log"
nssm set %SERVICE_NAME% AppStderr "%INSTALL_DIR%\logs\stderr.log"

REM Create logs directory
if not exist "%INSTALL_DIR%\logs" mkdir "%INSTALL_DIR%\logs"

REM Start service
echo [INFO] Starting service...
nssm start %SERVICE_NAME%

REM Check status
timeout /t 2 >nul
nssm status %SERVICE_NAME%

echo.
echo [SUCCESS] Service installed!
echo.
echo   Service Name: %SERVICE_NAME%
echo.
echo   Commands:
echo     nssm status %SERVICE_NAME%
echo     nssm start %SERVICE_NAME%
echo     nssm stop %SERVICE_NAME%
echo     nssm restart %SERVICE_NAME%
echo     nssm remove %SERVICE_NAME% confirm
echo.
goto :end

:uninstall
echo [INFO] Uninstalling Antigravity Claude Proxy...
echo.

REM Stop and remove service
nssm status %SERVICE_NAME% >nul 2>&1
if %errorlevel% equ 0 (
    echo [INFO] Stopping service...
    nssm stop %SERVICE_NAME% >nul 2>&1
    echo [INFO] Removing service...
    nssm remove %SERVICE_NAME% confirm >nul 2>&1
)

REM Remove install directory
if exist "%INSTALL_DIR%" (
    echo [INFO] Removing install directory...
    rmdir /S /Q "%INSTALL_DIR%"
)

echo.
echo [SUCCESS] Uninstalled!
echo.
echo [WARN] Config preserved: %CONFIG_DIR%
echo        To remove: rmdir /S /Q "%CONFIG_DIR%"
echo.
goto :end

:end
pause

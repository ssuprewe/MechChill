@echo off
setlocal
echo =======================================================
echo          Building MechChill (Release Build)
echo =======================================================
echo.

go build -ldflags "-s -w" -o MechChill.exe .
if %ERRORLEVEL% EQU 0 (
    echo.
    echo [SUCCESS] MechChill.exe successfully built!
    echo Run MechChill.exe or configure it in the CMD interface.
    echo.
) else (
    echo.
    echo [ERROR] Build failed with exit code %ERRORLEVEL%.
    echo Please make sure Go 1.21+ is installed and on your PATH.
    echo.
)

endlocal

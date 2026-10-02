@echo off
rem 双击或命令行运行：等价于 powershell -File scripts\build.ps1，参数原样透传。
rem 例：build.bat -Clean -Nsis -Version 1.0.0
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0build.ps1" %*

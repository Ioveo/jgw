@echo off
chcp 65001 >nul
echo.
echo ╔══════════════════════════════════════════╗
echo ║   OCI 甲骨文自动抢机器 - 构建脚本       ║
echo ╚══════════════════════════════════════════╝
echo.

:: 检查 Go 环境
where go >nul 2>&1
if %errorlevel% neq 0 (
    echo [错误] 未找到 Go，请先安装 Go 1.21+
    pause
    exit /b 1
)

:: 检查 GCC（fyne 需要 CGO）
where gcc >nul 2>&1
if %errorlevel% neq 0 (
    echo [警告] 未检测到 GCC/MinGW，fyne GUI 需要 CGO
    echo        请安装 MinGW-w64: https://www.mingw-w64.org/
    echo        或通过 Scoop: scoop install mingw
    echo.
    pause
)

:: 下载依赖
echo [1/4] 下载依赖...
go mod tidy
if %errorlevel% neq 0 (
    echo [错误] go mod tidy 失败
    pause
    exit /b 1
)

:: 构建 CLI 版本
echo [2/4] 构建 CLI 版本 (oci-grabber-cli.exe)...
go build -o oci-grabber-cli.exe .
if %errorlevel% neq 0 ( echo [错误] CLI 构建失败 & pause & exit /b 1 )
echo       ✓ oci-grabber-cli.exe

:: 构建 Setup 向导
echo [3/4] 构建 Setup 向导 (oci-setup.exe)...
go build -o oci-setup.exe ./cmd/setup/
if %errorlevel% neq 0 ( echo [错误] Setup 构建失败 & pause & exit /b 1 )
echo       ✓ oci-setup.exe

:: 构建 GUI 版本（无控制台窗口）
echo [4/4] 构建 GUI 版本 (oci-grabber-gui.exe)...
go build -ldflags="-H windowsgui" -o oci-grabber-gui.exe ./cmd/gui/
if %errorlevel% neq 0 ( echo [错误] GUI 构建失败（确认已安装 MinGW GCC） & pause & exit /b 1 )
echo       ✓ oci-grabber-gui.exe

echo.
echo ══════════════════════════════════════════
echo  构建完成！
echo  ├─ CLI: oci-grabber-cli.exe  （命令行版）
echo  └─ GUI: oci-grabber-gui.exe  （图形界面版）
echo.
echo  首次使用：
echo    1. 复制 config.example.toml → config.toml
echo    2. 填写 Tenancy OCID / User OCID 等信息
echo    3. 双击 oci-grabber-gui.exe 启动图形界面
echo ══════════════════════════════════════════
echo.
pause

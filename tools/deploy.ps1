# 把编译好的 ragentd 部署到路由器的 OpenWrt/ImmortalWrt 系统（无需重新编译固件）。
#
# 前置：路由器已刷入第三方固件、已开 SSH，且本机与路由器在同一网段。
# 用法（PowerShell）：
#   .\tools\deploy.ps1
#   .\tools\deploy.ps1 -RouterHost 192.168.31.1 -User root
param(
    [string]$RouterHost = "192.168.1.1",
    [string]$User = "root",
    [int]$Port = 22,
    [string]$Bin = "dist\ragentd"
)

$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot

# 定位本机产物
$binPath = Join-Path $root $Bin
if (-not (Test-Path $binPath)) { throw "找不到 $binPath，先执行编译：go build -o dist\ragentd .\cmd\ragentd" }
$initSrc = Join-Path $root "firmware\package\ragent\files\ragentd.init"
$confSrc = Join-Path $root "firmware\package\ragent\files\ragent.config"
foreach ($f in @($initSrc, $confSrc)) {
    if (-not (Test-Path $f)) { throw "缺少 $f" }
}

foreach ($cmd in @("scp", "ssh")) {
    if (-not (Get-Command $cmd -ErrorAction SilentlyContinue)) {
        throw "未找到 $cmd：请在 Windows 可选功能里启用 OpenSSH 客户端"
    }
}

$target = "$User@$RouterHost"
$sshArgs = @("-p", "$Port", "-o", "StrictHostKeyChecking=no", $target)
$scpArgs = @("-P", "$Port", "-o", "StrictHostKeyChecking=no")

Write-Host "==> 上传到 ${target}:/tmp"
& scp @scpArgs $binPath "${target}:/tmp/ragentd"
& scp @scpArgs $initSrc "${target}:/tmp/ragentd.init"
& scp @scpArgs $confSrc "${target}:/tmp/ragent.config"

$remote = @(
    "set -e",
    "mv /tmp/ragentd /usr/sbin/ragentd",
    "mv /tmp/ragentd.init /etc/init.d/ragentd",
    "[ -f /etc/config/ragent ] || mv /tmp/ragent.config /etc/config/ragent",
    "chmod +x /usr/sbin/ragentd /etc/init.d/ragentd",
    "/etc/init.d/ragentd enable",
    "/etc/init.d/ragentd restart",
    "sleep 2",
    "echo '--- 运行状态 ---'",
    "pgrep -f /usr/sbin/ragentd >/dev/null && echo running || echo 'not running'",
    "echo '--- 本机 API 自检 ---'",
    "curl -s -m 3 http://127.0.0.1:8080/api/v1/status || true"
) -join "; "

Write-Host "==> 安装并启动"
& ssh @sshArgs $remote

Write-Host "`n完成。下一步："
Write-Host "  1) 生成配对二维码：  ssh $target 'ragent-pair'"
Write-Host "  2) 浏览器打开：      http://$RouterHost/ragent-pair.png"
Write-Host "  3) iOS App 扫码配对；或用 curl 自检："
Write-Host "     curl http://${RouterHost}:8080/api/v1/status"

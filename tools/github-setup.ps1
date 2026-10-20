<#
用 GitHub API 一键完成：创建仓库 → 上传代码 → 触发云端固件编译。
本机无需安装 git / Docker。

前置：在 https://github.com/settings/tokens 生成 Personal access token (classic)，
      勾选 repo 与 workflow 两个权限。

用法（PowerShell）：
  .\tools\github-setup.ps1 -Token ghp_xxxxxxxxxxxxxxxxxxxx
  .\tools\github-setup.ps1 -Token ghp_xxx -Repo ax6000-fw -Private   # 建私有仓库
  .\tools\github-setup.ps1 -Token ghp_xxx -NoDispatch                # 只上传，不触发编译
#>
param(
    [Parameter(Mandatory = $true)][string]$Token,
    [string]$Repo = "ax6000-fw",
    [string]$Branch,
    [switch]$Private,
    [switch]$NoDispatch
)

$ErrorActionPreference = "Stop"
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12

$root = Split-Path -Parent $PSScriptRoot
$api = "https://api.github.com"
$headers = @{
    "Authorization"        = "Bearer $Token"
    "Accept"               = "application/vnd.github+json"
    "X-GitHub-Api-Version" = "2022-11-28"
    "User-Agent"           = "ax6000-fw-uploader"
}

function Invoke-GH {
    param([string]$Method, [string]$Uri, [hashtable]$Body)
    $json = $null
    if ($Body) { $json = $Body | ConvertTo-Json -Depth 10 -Compress }
    try {
        if ($json) {
            return Invoke-RestMethod -Method $Method -Uri $Uri -Headers $headers `
                -Body ([Text.Encoding]::UTF8.GetBytes($json)) -ContentType "application/json"
        }
        return Invoke-RestMethod -Method $Method -Uri $Uri -Headers $headers
    } catch {
        $detail = ""
        if ($_.ErrorDetails.Message) { $detail = $_.ErrorDetails.Message }
        throw "GitHub API $Method $Uri 失败: $($_.Exception.Message) $detail"
    }
}

# 1) 确认身份
$me = Invoke-GH GET "$api/user" $null
$owner = $me.login
Write-Host "==> GitHub 用户: $owner"

# 2) 创建/复用仓库
$repoObj = $null
try { $repoObj = Invoke-GH GET "$api/repos/$owner/$Repo" $null } catch { $repoObj = $null }
if (-not $repoObj) {
    Write-Host "==> 创建仓库 $owner/$Repo"
    $repoObj = Invoke-GH POST "$api/user/repos" @{
        name        = $Repo
        private     = [bool]$Private
        auto_init   = $false
        description = "红米 AX6000 定制固件工程（自研控制平面 + iOS App）"
    }
} else {
    Write-Host "==> 仓库已存在，复用"
}

if (-not $PSBoundParameters.ContainsKey('Branch') -or [string]::IsNullOrWhiteSpace($Branch)) {
    $Branch = $repoObj.default_branch
}
Write-Host "==> 目标分支: $Branch"

# 3) 上传文件
$skipDirs = @('dist', '.build', '.git', '.gocache', '.gopath', 'data', 'node_modules')
$files = Get-ChildItem $root -Recurse -File -Force | Where-Object {
    $rel = $_.FullName.Substring($root.Length + 1)
    -not (($rel -split '[\\/]') | Where-Object { $skipDirs -contains $_ })
}
Write-Host "==> 共 $($files.Count) 个文件待上传"
$i = 0
$fail = 0
foreach ($f in $files) {
    $i++
    $rel = ($f.FullName.Substring($root.Length + 1)) -replace '\\', '/'
    $b64 = [Convert]::ToBase64String([IO.File]::ReadAllBytes($f.FullName))
    $body = @{ message = "chore: 初始化 AX6000 固件工程"; content = $b64; branch = $Branch }
    $ok = $false
    for ($r = 0; $r -lt 3 -and -not $ok; $r++) {
        try {
            Invoke-GH PUT "$api/repos/$owner/$Repo/contents/$rel" $body | Out-Null
            $ok = $true
        } catch {
            # 已存在的文件需要带 sha 覆盖
            try {
                $cur = Invoke-RestMethod -Method GET -Uri "$api/repos/$owner/$Repo/contents/$rel" -Headers $headers
                $body.sha = $cur.sha
            } catch { Start-Sleep -Seconds 2 }
        }
    }
    if (-not $ok) { $fail++; Write-Warning "跳过: $rel" }
    Write-Progress -Activity "上传文件" -Status $rel -PercentComplete ([int]($i / $files.Count * 100))
}
Write-Progress -Activity "上传文件" -Completed
Write-Host "==> 上传完成（失败 $fail 个）"

# 4) 触发固件编译
if (-not $NoDispatch) {
    Write-Host "==> 等待 GitHub 识别 workflow ..."
    Start-Sleep -Seconds 12
    Invoke-GH POST "$api/repos/$owner/$Repo/actions/workflows/firmware.yml/dispatches" @{ ref = $Branch } | Out-Null
    Write-Host "==> 已触发云端固件编译"
}

Write-Host ""
Write-Host "查看编译进度: https://github.com/$owner/$Repo/actions"
Write-Host "（首次约 1.5~3 小时，完成后在 Artifacts 下载 ax6000-firmware-*）"

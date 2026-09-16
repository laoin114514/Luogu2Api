# 一次跑通两个 module 的构建与测试。
#
# 为什么需要它：pkg/luoguClient 是嵌套 module，`go build ./...` / `go test ./...`
# 不会进入其中，所以 SDK 必须单独跑一遍。
#
# 用法（Windows PowerShell 5.1 或 pwsh 7 均可）:
#   pwsh scripts/check.ps1
#   powershell -ExecutionPolicy Bypass -File scripts/check.ps1

$ErrorActionPreference = 'Continue'   # 原生命令写到 stderr 的警告不应中断脚本
$root = Split-Path -Parent $PSScriptRoot
$fail = 0

function Invoke-Check([string]$label, [string]$dir, [string[]]$commands) {
    Write-Host "`n== $label ==" -ForegroundColor Cyan
    Push-Location $dir
    try {
        foreach ($cmd in $commands) {
            Write-Host "> $cmd"
            Invoke-Expression $cmd
            if ($LASTEXITCODE -ne 0) {
                Write-Host "FAIL: $cmd (exit $LASTEXITCODE)" -ForegroundColor Red
                $script:fail++
            }
        }
    } finally {
        Pop-Location
    }
}

Invoke-Check '服务 Luogu2Api' $root @(
    'go build ./...',
    'go vet ./...',
    'go test -count=1 ./...'
)

Invoke-Check 'SDK pkg/luoguClient（嵌套 module）' (Join-Path $root 'pkg/luoguClient') @(
    'go build ./...',
    'go vet ./...',
    'go test -race -count=1 ./...'
)

if ($fail -gt 0) {
    Write-Host "`n$fail 项失败" -ForegroundColor Red
    exit 1
}
Write-Host "`n全部通过" -ForegroundColor Green

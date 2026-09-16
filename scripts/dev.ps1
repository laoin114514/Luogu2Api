param(
    [string]$EnvFile = 'configs/.env',
    [switch]$Override
)

$ErrorActionPreference = 'Stop'

# 本地开发启动脚本：把 configs/.env 导出到**当前会话**再 go run，退出即失效。
#
# 为什么不把这个逻辑写进 Go 代码：部署侧（compose/k8s/服务）本来就负责注入环境变量，
# 二进制读 .env 只在本地有用，却会让"配置来源"变成依赖工作目录的隐藏输入——镜像里
# 残留一个 .env 就能把忘记配置的必填项悄悄补上，正好破坏 fail-fast。
#
# 规则：
#   * 已存在的非空环境变量默认**不覆盖**（与 compose env_file 的优先级一致，
#     也与 README 里"显式配置优先"的说法对齐）；要强制以文件为准加 -Override。
#   * 支持 ` #` 行内注释（# 前需有空白），紧贴值的 # 按字面量保留；
#     值里要含 " #" 请用引号包起来。
#   * 找不到文件直接报错，不做静默跳过——这正是"以为配了其实没读"的来源。
#
# 用法：
#   pwsh scripts/dev.ps1
#   powershell -ExecutionPolicy Bypass -File scripts/dev.ps1
#   pwsh scripts/dev.ps1 -EnvFile configs/.env.local -Override

$root = Split-Path -Parent $PSScriptRoot
$path = Join-Path $root $EnvFile

if (-not (Test-Path -LiteralPath $path)) {
    Write-Host "找不到配置文件: $path" -ForegroundColor Red
    Write-Host "先复制一份样例：Copy-Item configs/env.example configs/.env" -ForegroundColor Yellow
    exit 1
}

$applied = 0
$skipped = 0

foreach ($line in [System.IO.File]::ReadAllLines($path)) {
    $text = $line.Trim()
    if ($text -eq '' -or $text.StartsWith('#')) { continue }
    if ($text -notmatch '^([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(.*)$') { continue }

    $key = $Matches[1]
    $value = $Matches[2].Trim()

    # 行内注释：只有 ` #`（# 前有空白）才是注释起点
    $hash = $value.IndexOf(' #')
    if ($hash -ge 0) { $value = $value.Substring(0, $hash).TrimEnd() }

    # 去掉成对的引号（引号内的 # 已被保留）
    if ($value.Length -ge 2) {
        $first = $value.Substring(0, 1)
        $last = $value.Substring($value.Length - 1, 1)
        if (($first -eq '"' -and $last -eq '"') -or ($first -eq "'" -and $last -eq "'")) {
            $value = $value.Substring(1, $value.Length - 2)
        }
    }

    $existing = [Environment]::GetEnvironmentVariable($key, 'Process')
    if (-not $Override -and $existing) {
        $skipped++
        continue
    }

    Set-Item -Path "env:$key" -Value $value
    $applied++
}

Write-Host "已从 $EnvFile 导入 $applied 项配置" -NoNewline
if ($skipped -gt 0) { Write-Host "（$skipped 项因已存在而非空被跳过，加 -Override 可强制覆盖）" -NoNewline }
Write-Host ""

Push-Location $root
try {
    & go run ./cmd/api
    exit $LASTEXITCODE
} finally {
    Pop-Location
}

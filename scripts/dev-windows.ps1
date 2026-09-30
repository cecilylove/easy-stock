param(
    [ValidateSet('Backend', 'Frontend', 'Desktop', 'Shell')]
    [string]$Service = 'Shell'
)

$ErrorActionPreference = 'Stop'
$repoRoot = Split-Path -Parent $PSScriptRoot
$runtimeRoot = Join-Path $repoRoot '.runtime'
$dataRoot = Join-Path $runtimeRoot $(if ($Service -eq 'Desktop') { 'desktop-data' } else { 'web-data' })
$localGo = Join-Path $runtimeRoot 'tools/go/bin'
if (Test-Path (Join-Path $localGo 'go.exe')) {
    $env:Path = $localGo + [IO.Path]::PathSeparator + $env:Path
}

New-Item -ItemType Directory -Force $dataRoot | Out-Null
$env:A_STOCK_SETTINGS_PATH = Join-Path $dataRoot 'settings.json'
$env:A_STOCK_REVIEW_DB = Join-Path $dataRoot 'reviews.db'
$env:A_STOCK_PORTFOLIO_DB = Join-Path $dataRoot 'portfolio-inspections.db'
$env:A_STOCK_RESEARCH_DB = Join-Path $dataRoot 'stock-research.db'
$env:A_STOCK_MARKET_EMOTION_DB = Join-Path $dataRoot 'market-emotion.db'
$env:A_STOCK_THEME_RADAR_DB = Join-Path $dataRoot 'theme-radar.db'
$env:A_STOCK_MASTERY_CACHE = Join-Path $dataRoot 'trading-mastery'
$env:A_STOCK_LOG_DIR = Join-Path $dataRoot 'logs'
$env:A_STOCK_HERMES_HOME = Join-Path $dataRoot 'hermes-home'
$env:A_STOCK_HERMES_WORKDIR = Join-Path $dataRoot 'hermes-workspace'
$env:A_STOCK_USER_DATA_DIR = $dataRoot
$env:A_STOCK_HERMES_RUNTIME_ROOT = Join-Path $repoRoot 'desktop/resources/hermes-runtime'
$env:UV_PYTHON_INSTALL_DIR = Join-Path $runtimeRoot 'tools/python'
$env:UV_CACHE_DIR = Join-Path $runtimeRoot 'tools/uv-cache'
$env:A_STOCK_ADDR = '127.0.0.1:20081'
$env:VITE_A_STOCK_BACKEND_URL = 'http://127.0.0.1:20081'

Push-Location $repoRoot
try {
    switch ($Service) {
        'Backend' {
            Set-Location (Join-Path $repoRoot 'backend')
            & go run ./cmd/server
            if ($LASTEXITCODE -ne 0) { throw "Backend exited with code $LASTEXITCODE" }
        }
        'Frontend' {
            & npm.cmd --workspace frontend run dev -- --strictPort
            if ($LASTEXITCODE -ne 0) { throw "Frontend exited with code $LASTEXITCODE" }
        }
        'Desktop' {
            & node desktop/scripts/ensure-hermes-runtime.mjs
            if ($LASTEXITCODE -ne 0) { throw 'Hermes runtime preparation failed' }
            $env:ELECTRON_RENDERER_URL = 'http://127.0.0.1:20073'
            & node (Join-Path $repoRoot 'node_modules/electron/cli.js') (Join-Path $repoRoot 'desktop')
            if ($LASTEXITCODE -ne 0) { throw "Desktop exited with code $LASTEXITCODE" }
        }
        'Shell' {
            Write-Host "Development environment ready. Data: $dataRoot"
            Write-Host 'Dot-source this script to keep the environment in your current PowerShell session.'
        }
    }
} finally {
    Pop-Location
}

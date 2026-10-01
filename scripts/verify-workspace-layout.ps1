param(
    [string]$Url = 'http://127.0.0.1:20073',
    [string]$OutputDirectory = '.runtime/workspace-layout'
)

$ErrorActionPreference = 'Stop'
$repoRoot = Split-Path -Parent $PSScriptRoot
$browserCommand = Join-Path $repoRoot 'node_modules/.bin/agent-browser.cmd'
$session = 'workspace-layout-' + [guid]::NewGuid().ToString('N').Substring(0, 8)
$outputPath = if ([IO.Path]::IsPathRooted($OutputDirectory)) { $OutputDirectory } else { Join-Path $repoRoot $OutputDirectory }
New-Item -ItemType Directory -Force $outputPath

function Invoke-Browser {
    param([string[]]$BrowserArguments)
    Write-Host "Browser: $($BrowserArguments -join ' ')"
    & $browserCommand --session $session --json @BrowserArguments
    if ($LASTEXITCODE -ne 0) { throw "Browser command failed: $($BrowserArguments -join ' ')" }
}

function Invoke-BrowserScript {
    param([string]$Script)
    $Script | & $browserCommand --session $session --json eval --stdin
    if ($LASTEXITCODE -ne 0) { throw 'Browser layout assertion failed' }
}

$measure = @'
(async () => {
    await new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)));
    const check = (condition, message) => { if (!condition) throw new Error(message); };
    const rect = selector => document.querySelector(selector).getBoundingClientRect();
    const mode = document.querySelector('.workspace-frame').className.split(' ').find(name => name.startsWith('workspace-') && name !== 'workspace-frame').slice(10);
    const mobile = innerWidth <= 760;
    const desktop = innerWidth >= 1200 && innerHeight >= 800;
    const expanded = document.querySelector('.workspace-frame').classList.contains('sidebar-expanded');
    const root = document.documentElement;
    check(root.scrollWidth <= root.clientWidth + 1, 'Page horizontal overflow');
    if (!mobile) {
        const sidebar = rect('.app-sidebar');
        for (const button of document.querySelectorAll('.sidebar-bottom-actions > button')) {
            const box = button.getBoundingClientRect();
            check(box.left >= sidebar.left && box.right <= sidebar.right, `Sidebar button overflow: ${button.getAttribute('aria-label')}`);
            if (!expanded && button.querySelector('span')) check(getComputedStyle(button.querySelector('span')).display === 'none', 'Compact navigation has a visible label');
        }
    }
    if (mobile) {
        document.querySelector('.mobile-nav-trigger').click();
        await new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)));
        const sidebar = document.querySelector('.app-sidebar');
        check(getComputedStyle(sidebar).visibility === 'visible', 'Mobile navigation did not open');
        for (const button of sidebar.querySelectorAll('nav button')) {
            const label = button.querySelector('span');
            check(label && getComputedStyle(label).display !== 'none' && label.getBoundingClientRect().width > 0, 'Mobile navigation label hidden by compact preference');
            check(getComputedStyle(button).justifyContent !== 'center', 'Mobile navigation still uses compact alignment');
        }
        document.querySelector('.mobile-nav-close').click();
        await new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)));
    }
    const shell = rect('.app-shell');
    const padding = parseFloat(getComputedStyle(document.querySelector('.app-shell')).paddingLeft);
    const canvas = document.querySelector('.workspace-content').clientWidth;
    for (const element of document.querySelectorAll('.app-shell > .topbar, .app-shell > .data-footer, .workspace-content > section, .workspace-content > div')) {
        const box = element.getBoundingClientRect();
        check(Math.abs(box.left - shell.left - padding) <= 2, `Excess left margin: ${element.className}`);
        check(Math.abs(box.right - shell.right + padding) <= 2, `Excess right margin: ${element.className}`);
    }
    for (const selector of ['.trading-layout', '.review-layout', '.stock-ai-shell', '.portfolio-inspection-workspace', '.market-overview-workspace', '.mastery-layout', '.ai-chat-workspace']) {
        const layout = document.querySelector(selector);
        if (!layout) continue;
        const box = layout.getBoundingClientRect();
        for (const child of layout.children) {
            const childBox = child.getBoundingClientRect();
            check(childBox.left >= box.left - 1 && childBox.right <= box.right + 1, `Layout column outside its canvas: ${child.className}`);
        }
    }
    if (mode === 'themes') {
        const columns = getComputedStyle(document.querySelector('.trading-layout')).gridTemplateColumns.split(' ').length;
        check(columns === (canvas > 1500 ? 3 : canvas > 900 ? 2 : 1), `Theme columns do not follow canvas width ${canvas}: ${columns}`);
    }
    if (mode === 'reviews') {
        const layout = document.querySelector('.review-layout');
        if (layout) check(getComputedStyle(layout).gridTemplateColumns.split(' ').length === (canvas > 1200 ? 3 : canvas > 700 ? 2 : 1), 'Review columns do not follow canvas width');
    }
    if (desktop && ['market', 'mastery'].includes(mode)) {
        check(root.scrollHeight <= innerHeight + 1, 'Desktop workspace is taller than viewport');
        const pane = document.querySelector(mode === 'market' ? '.market-news-feed' : '.mastery-markdown');
        if (pane) {
            check(pane.clientHeight >= 180, 'Reading pane has insufficient usable height');
            check(pane.getBoundingClientRect().bottom <= innerHeight + 1, 'Reading pane below viewport');
            check(getComputedStyle(pane).overflowY === 'auto', 'Reading pane is not independently scrollable');
        }
    }
    const text = document.querySelector(mode === 'market' ? '.market-news-feed strong' : mode === 'mastery' ? '.mastery-markdown li' : '.unused-font-check');
    if (text) check(parseFloat(getComputedStyle(text).fontSize) >= 15, 'Content font is too small');
    return { mode, width: innerWidth, height: innerHeight, expanded, desktop, passed: true };
})()
'@

$views = @(
    @{ Mode = 'themes'; Ready = '.theme-item' },
    @{ Mode = 'market/pulse'; Ready = '.market-news-feed strong' },
    @{ Mode = 'limit-up'; Ready = '.limit-up-workspace' },
    @{ Mode = 'stock-detail'; Ready = '.stock-detail-workspace' },
    @{ Mode = 'stock-ai'; Ready = '.stock-ai-workspace' },
    @{ Mode = 'portfolio-inspection'; Ready = '.portfolio-inspection-workspace' },
    @{ Mode = 'ai'; Ready = '.ai-chat-workspace' },
    @{ Mode = 'reviews'; Ready = '.review-diary' },
    @{ Mode = 'mastery'; Ready = '.mastery-prose' },
    @{ Mode = 'token-usage'; Ready = '.token-usage-workspace' }
)
$results = @()
try {
    Invoke-Browser -BrowserArguments @('open', "$Url/#mastery")
    Invoke-Browser -BrowserArguments @('set', 'media', 'reduced-motion')
    foreach ($size in @(@(2558, 1304), @(1920, 1080), @(1440, 900), @(1024, 768), @(390, 844), @(320, 740), @(1440, 600))) {
        Invoke-Browser -BrowserArguments @('set', 'viewport', [string]$size[0], [string]$size[1])
        foreach ($view in $views) {
            $mode = $view.Mode
            Invoke-Browser -BrowserArguments @('open', "$Url/#$mode")
            Invoke-Browser -BrowserArguments @('wait', $view.Ready)
            foreach ($expanded in @($true, $false)) {
                Invoke-BrowserScript "localStorage.setItem('easy-stock.sidebar-expanded.v1', '$($expanded.ToString().ToLower())'); true"
                Invoke-Browser -BrowserArguments @('reload')
                Invoke-Browser -BrowserArguments @('wait', $view.Ready)
                Invoke-BrowserScript $measure
                $results += @{ mode = $mode; width = $size[0]; height = $size[1]; expanded = $expanded; passed = $true }
                Write-Host "$mode $($size[0])x$($size[1]) expanded=${expanded}: passed"
            }
        }
    }
    Invoke-Browser -BrowserArguments @('set', 'viewport', '2558', '1304')
    foreach ($view in $views) {
        $mode = $view.Mode
        Invoke-Browser -BrowserArguments @('open', "$Url/#$mode")
        Invoke-Browser -BrowserArguments @('wait', $view.Ready)
        $imageName = ($mode.Replace('/', '-')) + '-fluid.png'
        Invoke-Browser -BrowserArguments @('screenshot', (Join-Path $outputPath $imageName))
    }
    @{ passed = $true; cases = $results; count = $results.Count } | ConvertTo-Json -Depth 6 | Set-Content -Encoding UTF8 (Join-Path $outputPath 'result.json')
    Write-Host "Workspace layout checks passed: $($results.Count) cases."
} finally {
    Invoke-Browser -BrowserArguments @('close')
}

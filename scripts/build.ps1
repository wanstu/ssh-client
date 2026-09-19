$ErrorActionPreference = 'Stop'

$Root = Split-Path -Parent $PSScriptRoot
$Desktop = Join-Path $Root 'cmd\ssh-client-desktop'
$IconSource = Join-Path $Desktop 'assets\appicon.png'
$BuildDir = Join-Path $Desktop 'build'
$AppIcon = Join-Path $BuildDir 'appicon.png'
$WindowsIcon = Join-Path $BuildDir 'windows\icon.ico'

Push-Location $Root
try {
    go run github.com/wanstu/wails-desktop-kit/cmd/desktopkit icon generate --output $IconSource --symbol terminal --background '#2463EB'
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

    New-Item -ItemType Directory -Force -Path $BuildDir | Out-Null
    Copy-Item -LiteralPath $IconSource -Destination $AppIcon -Force
    if (Test-Path $WindowsIcon) {
        Remove-Item -LiteralPath $WindowsIcon -Force
    }

    node --check cmd/ssh-client-desktop/frontend/app.js
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

    go test ./...
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

    go vet ./...
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

    Push-Location $Desktop
    try {
        wails build -clean
        if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
    }
    finally {
        Pop-Location
    }
}
finally {
    Pop-Location
}

Write-Host ''
Write-Host 'Build complete:'
Write-Host (Join-Path $Desktop 'build\bin\ssh-client.exe')

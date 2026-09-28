$ErrorActionPreference = 'Stop'

$Root = Split-Path -Parent $PSScriptRoot
$Desktop = Join-Path $Root 'cmd\ssh-client-desktop'
$IconSource = Join-Path $Desktop 'assets\appicon.png'

Push-Location $Root
try {
    go run github.com/wanstu/wails-desktop-kit/cmd/desktopkit icon generate --output $IconSource --symbol terminal --background '#2463EB'
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

    go run github.com/wanstu/wails-desktop-kit/cmd/desktopkit icon prepare-wails --input $IconSource --desktop-dir $Desktop
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

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


$Exe = Join-Path $Desktop 'build\bin\ssh-client.exe'
$MetadataPath = Join-Path $Desktop 'frontend\desktopkit-build-info.json'
$ManifestPath = Join-Path $Desktop 'wails.json'

$ExpectedProductVersion = ''
$ExpectedDisplayVersion = ''
if (Test-Path $MetadataPath) {
    $Metadata = Get-Content -Raw $MetadataPath | ConvertFrom-Json
    $ExpectedProductVersion = [string]$Metadata.product_version
    $ExpectedDisplayVersion = ([string]$Metadata.version).TrimStart('v')
}
if (-not $ExpectedProductVersion) {
    $Manifest = Get-Content -Raw $ManifestPath | ConvertFrom-Json
    $ExpectedProductVersion = [string]$Manifest.info.productVersion
    $ExpectedDisplayVersion = $ExpectedProductVersion
}
if (-not $ExpectedProductVersion) {
    throw 'Unable to determine expected Windows product version.'
}

$VersionInfo = (Get-Item $Exe).VersionInfo
$ExpectedRawVersion = "$ExpectedProductVersion.0"
if ($VersionInfo.FileVersionRaw.ToString() -ne $ExpectedRawVersion) {
    throw "Windows FileVersionRaw mismatch: expected $ExpectedRawVersion, got $($VersionInfo.FileVersionRaw)"
}
if ($VersionInfo.ProductVersionRaw.ToString() -ne $ExpectedRawVersion) {
    throw "Windows ProductVersionRaw mismatch: expected $ExpectedRawVersion, got $($VersionInfo.ProductVersionRaw)"
}
if ($VersionInfo.FileVersion -ne $ExpectedProductVersion) {
    throw "Windows FileVersion mismatch: expected $ExpectedProductVersion, got '$($VersionInfo.FileVersion)'"
}
if ($VersionInfo.ProductVersion -ne $ExpectedDisplayVersion) {
    throw "Windows ProductVersion mismatch: expected $ExpectedDisplayVersion, got '$($VersionInfo.ProductVersion)'"
}

Write-Host "Windows version metadata verified: FileVersion=$($VersionInfo.FileVersion), ProductVersion=$($VersionInfo.ProductVersion)"

Write-Host ''
Write-Host 'Build complete:'
Write-Host $Exe

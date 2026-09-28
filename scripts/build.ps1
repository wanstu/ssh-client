$ErrorActionPreference = 'Stop'

$Root = Split-Path -Parent $PSScriptRoot
$Desktop = Join-Path $Root 'cmd\ssh-client-desktop'
$IconSource = Join-Path $Desktop 'assets\appicon.png'
$MetadataPath = Join-Path $Desktop 'frontend\desktopkit-build-info.json'
$ManifestPath = Join-Path $Desktop 'wails.json'
$WindowsInfoPath = Join-Path $Desktop 'build\windows\info.json'
$ExpectedProductVersion = ''
$ExpectedDisplayVersion = ''
$LocalBuildMetaPrepared = $false
$BuildMetaBackupDir = $null
$HadMetadata = $false
$HadWindowsInfo = $false

Push-Location $Root
try {
    if (-not $env:GITHUB_ACTIONS) {
        $BuildMetaBackupDir = Join-Path ([System.IO.Path]::GetTempPath()) ("ssh-client-buildmeta-" + [guid]::NewGuid().ToString('N'))
        New-Item -ItemType Directory -Force -Path $BuildMetaBackupDir | Out-Null
        Copy-Item $ManifestPath (Join-Path $BuildMetaBackupDir 'wails.json') -Force
        $HadMetadata = Test-Path $MetadataPath
        if ($HadMetadata) {
            Copy-Item $MetadataPath (Join-Path $BuildMetaBackupDir 'desktopkit-build-info.json') -Force
        }
        $HadWindowsInfo = Test-Path $WindowsInfoPath
        if ($HadWindowsInfo) {
            Copy-Item $WindowsInfoPath (Join-Path $BuildMetaBackupDir 'windows-info.json') -Force
        }
        $LocalBuildMetaPrepared = $true

        $LocalVersion = if ($env:APP_VERSION) { $env:APP_VERSION } else { 'dev' }
        $LocalCommit = (& git rev-parse HEAD).Trim()
        go run github.com/wanstu/wails-desktop-kit/cmd/desktopkit buildmeta prepare --root $Root --desktop-dir $Desktop --version $LocalVersion --commit $LocalCommit
        if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
    }

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
    if ($LocalBuildMetaPrepared) {
        Copy-Item (Join-Path $BuildMetaBackupDir 'wails.json') $ManifestPath -Force
        if ($HadMetadata) {
            Copy-Item (Join-Path $BuildMetaBackupDir 'desktopkit-build-info.json') $MetadataPath -Force
        }
        else {
            Remove-Item $MetadataPath -Force -ErrorAction SilentlyContinue
        }
        if ($HadWindowsInfo) {
            Copy-Item (Join-Path $BuildMetaBackupDir 'windows-info.json') $WindowsInfoPath -Force
        }
        else {
            Remove-Item $WindowsInfoPath -Force -ErrorAction SilentlyContinue
        }
        Remove-Item $BuildMetaBackupDir -Recurse -Force -ErrorAction SilentlyContinue
    }
}


$Exe = Join-Path $Desktop 'build\bin\ssh-client.exe'
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

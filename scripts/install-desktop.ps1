#Requires -Version 5.1
[CmdletBinding()]
param(
    [string]$Dir = "$env:LOCALAPPDATA\Programs\agy-server",
    [switch]$NoStart
)

$ErrorActionPreference = 'Stop'
$repo = 'AFSlayer/antigravity-server'

function Write-Step($message) { Write-Host "  $([char]0x2713) $message" -ForegroundColor Green }
function Write-Info($message) { Write-Host "  $message" }
function Write-Fail($message) {
    Write-Host ""
    Write-Host "  $([char]0x2715) $message" -ForegroundColor Red
    Write-Host ""
    exit 1
}

Write-Host ""
Write-Host "  Antigravity Server" -ForegroundColor White
Write-Host ""

$arch = switch ($env:PROCESSOR_ARCHITECTURE) {
    'AMD64' { 'amd64' }
    'ARM64' { 'arm64' }
    default { Write-Fail "Unsupported architecture: $env:PROCESSOR_ARCHITECTURE" }
}

$url = if ($env:AGY_BINARY_URL) {
    $env:AGY_BINARY_URL
} else {
    "https://github.com/$repo/releases/latest/download/agy-server_windows_$arch.zip"
}

$work = Join-Path ([System.IO.Path]::GetTempPath()) ("agy-server-" + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $work -Force | Out-Null

try {
    Write-Info "Downloading for windows/$arch..."
    $archive = Join-Path $work 'agy-server.zip'
    Invoke-WebRequest -Uri $url -OutFile $archive -UseBasicParsing

    Expand-Archive -Path $archive -DestinationPath $work -Force
    $binary = Join-Path $work 'agy-server.exe'
    if (-not (Test-Path $binary)) { Write-Fail 'The archive did not contain agy-server.exe.' }

    New-Item -ItemType Directory -Path $Dir -Force | Out-Null
    $installed = Join-Path $Dir 'agy-server.exe'
    Copy-Item -Path $binary -Destination $installed -Force
    Unblock-File -Path $installed
    Write-Step "Installed $installed"

    $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
    if ($userPath -notlike "*$Dir*") {
        [Environment]::SetEnvironmentVariable('Path', "$userPath;$Dir", 'User')
        Write-Step 'Added it to your PATH (restart your terminal to use "agy-server")'
    }

    if (-not $NoStart) {
        Write-Host ""
        Write-Info 'Starting... a control panel with a QR code will open in your browser.'
        Write-Host ""
        & $installed
    } else {
        Write-Host ""
        Write-Info 'Run it with: agy-server'
        Write-Host ""
    }
} finally {
    Remove-Item -Recurse -Force $work -ErrorAction SilentlyContinue
}

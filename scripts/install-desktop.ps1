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
Write-Host "  ! The desktop companion no longer works." -ForegroundColor Yellow
Write-Host "    Since 2.34.0 the Antigravity desktop app keeps its language server's access token"
Write-Host "    from other programs, and the app updates itself, so agy-server cannot connect to it"
Write-Host "    and the web UI stays blank."
Write-Host "    Use Google's official remote at antigravity.google.com instead." -ForegroundColor DarkGray
Write-Host "    https://github.com/AFSlayer/antigravity-server/issues/98" -ForegroundColor DarkGray
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

    # -NoStart is kept so that older instructions still run; it never starts now.
    Write-Host ""
    Write-Info 'Not started, since it cannot attach to the desktop app, as noted above.'
    Write-Host ""
} finally {
    Remove-Item -Recurse -Force $work -ErrorAction SilentlyContinue
}

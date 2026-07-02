# Javinizer CLI Installer for Windows (PowerShell)
#
# One-shot install:
#   irm https://raw.githubusercontent.com/javinizer/javinizer-go/main/scripts/install.ps1 | iex
#   # install the newest release including prereleases:
#   & ([scriptblock]::Create((irm https://raw.githubusercontent.com/javinizer/javinizer-go/main/scripts/install.ps1))) -PreRelease
#
# Mirrors scripts/install.sh: downloads the latest release asset, verifies its
# SHA256 against checksums.txt, removes the Mark-of-the-Web (Unblock-File) so
# Smart App Control / Defender does not block first run with "Access is denied"
# (see issue #72), and installs to %LOCALAPPDATA%\javinizer\bin (no admin
# required) with the folder added to the user PATH.

#Requires -Version 5.1
[CmdletBinding()]
param(
    [string] $InstallDir = "",
    [switch] $PreRelease
)

$ErrorActionPreference = 'Stop'
$Repo = 'javinizer/javinizer-go'
$BinaryName = 'javinizer'

function Write-Step($msg) { Write-Host "==> $msg" -ForegroundColor Cyan }
function Write-OK($msg)   { Write-Host "    $msg" -ForegroundColor Green }
function Write-Warn($msg) { Write-Host "    $msg" -ForegroundColor Yellow }
function Die($msg)        { Write-Host "    ERROR: $msg" -ForegroundColor Red; exit 1 }

# --- 1. Detect architecture -------------------------------------------------
# Only windows-amd64 is published today (see .github/workflows/cli-release.yml).
$arch = $env:PROCESSOR_ARCHITECTURE
if ($arch -eq 'AMD64') {
    $assetName = 'javinizer-windows-amd64.exe'
} else {
    Die "No prebuilt release for Windows $arch yet. Use 'go install' or Docker (see README), or build from source."
}

Write-Step 'Javinizer CLI Installer'

# --- 2. Resolve latest release version --------------------------------------
# By default only the latest STABLE release is installed (GitHub's
# /releases/latest excludes prereleases). If no stable release exists yet, the
# installer stops and points the user at -PreRelease (or the Releases page)
# rather than silently installing a prerelease -- prereleases are opt-in.
# With -PreRelease, the /releases list endpoint returns the newest release
# including prereleases (e.g. v1.0.0-rc3).
function Get-LatestTag {
    # Branch on -PreRelease first so the list endpoint is the primary path when
    # opted in: otherwise a newer prerelease is ignored whenever a stable exists.
    if ($PreRelease) {
        $list = Invoke-RestMethod -Uri "https://api.github.com/repos/$Repo/releases?per_page=1" -Headers @{ 'User-Agent' = 'Javinizer-Installer' }
        $latest = @($list)[0]
        if (-not $latest -or -not $latest.tag_name) { Die 'Failed to fetch latest release version.' }
        return $latest.tag_name
    }

    try {
        $r = Invoke-RestMethod -Uri "https://api.github.com/repos/$Repo/releases/latest" -Headers @{ 'User-Agent' = 'Javinizer-Installer' } -ErrorAction Stop
        if ($r.tag_name) { return $r.tag_name }
    } catch {
        Die "No stable release is available yet. Javinizer is currently in pre-release. To install the latest pre-release, re-run with -PreRelease, or download a specific release from https://github.com/$Repo/releases"
    }
    Die "No stable release is available yet. To install the latest pre-release, re-run with -PreRelease, or download from https://github.com/$Repo/releases"
}

$tag = Get-LatestTag
if ($PreRelease -and $tag -match '-') { Write-Warn "Note: $tag is a pre-release." }
Write-OK "Latest version: $tag"

# --- 3. Resolve install directory (user-local, no admin) --------------------
if (-not $InstallDir) {
    $InstallDir = Join-Path $env:LOCALAPPDATA 'javinizer\bin'
}
if (-not (Test-Path $InstallDir)) {
    New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
}

# --- 4. Download asset + checksums -----------------------------------------
$tmp = New-Item -ItemType Directory -Force -Path (Join-Path $env:TEMP "javinizer-install-$(Get-Random)")
$assetPath = Join-Path $tmp.FullName $assetName
$checksumPath = Join-Path $tmp.FullName 'checksums.txt'

$assetUrl = "https://github.com/$Repo/releases/download/$tag/$assetName"
$checksumUrl = "https://github.com/$Repo/releases/download/$tag/checksums.txt"

Write-Step "Downloading $assetName"
Invoke-WebRequest -Uri $assetUrl -OutFile $assetPath -UseBasicParsing

Write-Step 'Verifying checksum'
try {
    Invoke-WebRequest -Uri $checksumUrl -OutFile $checksumPath -UseBasicParsing
} catch {
    Die "Could not download checksums.txt from $checksumUrl"
}

# checksums.txt is sha256sum output: "<hash>  <name>". Match the asset field
# exactly (a substring match can pick a sibling asset like a .sig file).
$expected = $null
foreach ($line in Get-Content -Path $checksumPath) {
    $fields = $line -split '\s+'
    if ($fields.Count -ge 2 -and ($fields[1] -in @($assetName, "*$assetName"))) {
        $expected = $fields[0]
        break
    }
}
if (-not $expected) { Die "Checksum for $assetName not found in checksums.txt" }

$actual = (Get-FileHash -Algorithm SHA256 -Path $assetPath).Hash.ToLower()
if ($actual -ne $expected.ToLower()) {
    Die "Checksum mismatch! Expected $expected, got $actual -- refusing to install."
}
Write-OK 'Checksum verified'

# --- 5. Unblock (remove Mark-of-the-Web) ------------------------------------
# This is the fix for issue #72: downloads carry a Zone.Identifier stream that
# Smart App Control / Defender can block with "Access is denied" before the
# program even starts. Unblock-File strips it.
Unblock-File -Path $assetPath
Write-OK 'Removed Mark-of-the-Web (Unblock-File)'

# --- 6. Install to %LOCALAPPDATA%\javinizer\bin -----------------------------
$dest = Join-Path $InstallDir "$BinaryName.exe"
Copy-Item -Path $assetPath -Destination $dest -Force
Write-OK "Installed to $dest"

# --- 7. Add to user PATH (if missing) --------------------------------------
$userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
if ($userPath -and ($userPath.Split(';') -contains $InstallDir)) {
    Write-OK 'Install dir already on user PATH'
} else {
    $newPath = if ($userPath) { "$userPath;$InstallDir" } else { $InstallDir }
    [Environment]::SetEnvironmentVariable('Path', $newPath, 'User')
    Write-OK "Added $InstallDir to user PATH"
}

# --- 8. Cleanup + next steps ------------------------------------------------
Remove-Item -Recurse -Force $tmp.FullName -ErrorAction SilentlyContinue

Write-Host ''
Write-Step 'Installation complete'
Write-Host "    Open a NEW terminal (to pick up the PATH), then run:"
Write-Host "      javinizer version" -ForegroundColor White
Write-Host ''
Write-Host "    Start the web UI:"
Write-Host "      javinizer init" -ForegroundColor White
Write-Host "      javinizer web" -ForegroundColor White
Write-Host ''
Write-Warn "To update later: run this installer again, or 'javinizer upgrade'."

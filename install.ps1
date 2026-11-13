# Alyysha Router — built by YannClipper — https://t.me/YannClippers
<#
.SYNOPSIS
  Alyysha Router one-line installer for Windows: downloads the latest release
  .exe from GitHub, installs it, and prints next steps.

.DESCRIPTION
  Run in PowerShell (no admin needed):

    irm https://raw.githubusercontent.com/ShiroSdk/alyysha-router/main/install.ps1 | iex

  Optional overrides (set before piping, or pass when saved locally):

    $env:VERSION = "v1.9.2"   # default: latest
    $env:BINDIR  = "C:\Tools" # default: $env:LOCALAPPDATA\alyysha
#>
[CmdletBinding()]
param(
  [string]$Version = $env:VERSION,
  [string]$InstallDir = $env:BINDIR
)

$ErrorActionPreference = 'Stop'

$Repo = 'ShiroSdk/alyysha-router'
if ([string]::IsNullOrWhiteSpace($Version)) { $Version = 'latest' }
# The install DIRECTORY keeps the pre-rename name on purpose: every install
# that predates the rebrand already has it on the user PATH, and moving the
# binary to a new directory would leave that PATH entry pointing at the old
# (now stale) exe. Only the installed FILE name changed to alyysha.exe.
if ([string]::IsNullOrWhiteSpace($InstallDir)) {
  $InstallDir = Join-Path $env:LOCALAPPDATA 'alyysha'
}

function Write-Info($msg) { Write-Host "[Alyysha Router] $msg" -ForegroundColor Green }

# Windows release asset is amd64-only; refuse ARM with a clear message.
$arch = $env:PROCESSOR_ARCHITECTURE
if ($arch -ne 'AMD64') {
  throw "Unsupported architecture: $arch (Windows asset is amd64-only; see https://github.com/$Repo/releases/latest)"
}

$Asset = 'alyysha-windows-amd64.exe'
# The release asset keeps its alyysha-* name (CI, the updater, and
# scoreArchiveEntry all match it), but it is installed as `alyysha.exe` — the
# name binaryName() reports and the schtasks backend references.
if ($Version -eq 'latest') {
  $Url = "https://github.com/$Repo/releases/latest/download/$Asset"
} else {
  $Url = "https://github.com/$Repo/releases/download/$Version/$Asset"
}

# A running instance may be the new install or a pre-rename one — the old
# binary name is spelled in two parts so a bulk rename cannot undo this:
# the installer must be able to replace a alyysha-go process too.
if (Get-Process -Name 'alyysha', ('alyysha' + '-go') -ErrorAction SilentlyContinue) {
  throw 'Alyysha Router is currently running — stop it first (Ctrl+C in its terminal), then re-run the installer.'
}

if (-not (Test-Path $InstallDir)) {
  New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
}
$Dest = Join-Path $InstallDir 'alyysha.exe'
$Tmp = Join-Path $env:TEMP 'alyysha-download.exe'

Write-Info "downloading $Url ..."
Invoke-WebRequest -Uri $Url -OutFile $Tmp -UseBasicParsing
Move-Item -Path $Tmp -Destination $Dest -Force

# Add install dir to the *user* PATH so `alyysha` works in new terminals.
$currentUserPath = [Environment]::GetEnvironmentVariable('Path', 'User')
$newPath = $null
if ([string]::IsNullOrWhiteSpace($currentUserPath)) {
  $newPath = $InstallDir
} elseif ((($currentUserPath -split ';')) -notcontains $InstallDir) {
  $newPath = $currentUserPath.TrimEnd(';') + ';' + $InstallDir
}
if ($newPath) {
  [Environment]::SetEnvironmentVariable('Path', $newPath, 'User')
  Write-Info "added $InstallDir to your user PATH (restart the terminal to use it)"
}

#!/usr/bin/env pwsh
<#
.SYNOPSIS
Downloads the porthook CLI agent for Windows, verifies it against the
release's SHA256SUMS manifest, and installs it. Refuses to install anything
that fails checksum verification. Only the CLI agent publishes Windows
binaries; porthook-gateway and porthook-control-plane are Linux-container
images (see docs/UPGRADING.md).

.PARAMETER Version
Release to install, e.g. v0.17.0 (default: latest).

.PARAMETER InstallDir
Install directory (default: $env:LOCALAPPDATA\porthook\bin).

.PARAMETER LocalDir
Verify and install from a local directory (e.g. a `make release-build`
dist/ output) instead of downloading from GitHub. Skips version resolution
and attestation verification.
#>
param(
    [string]$Version = "latest",
    [string]$InstallDir = "$env:LOCALAPPDATA\porthook\bin",
    [string]$LocalDir = ""
)

$ErrorActionPreference = "Stop"
$repo = "voiteco/porthook"
$Binary = "porthook"

switch ([System.Runtime.InteropServices.RuntimeInformation]::ProcessArchitecture) {
    "X64" { $arch = "amd64" }
    "Arm64" { $arch = "arm64" }
    default {
        Write-Error "install.ps1: unsupported architecture: $_"
        exit 1
    }
}

if (-not $LocalDir -and $Version -eq "latest") {
    $release = Invoke-RestMethod -Uri "https://api.github.com/repos/$repo/releases/latest"
    $Version = $release.tag_name
    if (-not $Version) {
        Write-Error "install.ps1: could not resolve the latest release version"
        exit 1
    }
}

$asset = "${Binary}_windows_${arch}.exe"

$tmpDir = Join-Path ([System.IO.Path]::GetTempPath()) ([System.Guid]::NewGuid())
New-Item -ItemType Directory -Path $tmpDir | Out-Null

try {
    $assetPath = Join-Path $tmpDir $asset
    $sumsPath = Join-Path $tmpDir "SHA256SUMS"

    if ($LocalDir) {
        Write-Host "install.ps1: using local build at $LocalDir"
        Copy-Item -Path (Join-Path $LocalDir $asset) -Destination $assetPath
        Copy-Item -Path (Join-Path $LocalDir "SHA256SUMS") -Destination $sumsPath
    } else {
        Write-Host "install.ps1: downloading $asset $Version"
        $baseUrl = "https://github.com/$repo/releases/download/$Version"
        Invoke-WebRequest -Uri "$baseUrl/$asset" -OutFile $assetPath
        Invoke-WebRequest -Uri "$baseUrl/SHA256SUMS" -OutFile $sumsPath
    }

    Write-Host "install.ps1: verifying checksum"
    $expectedLine = Select-String -Path $sumsPath -Pattern "  $asset$|  \*$asset$" | Select-Object -First 1
    if (-not $expectedLine) {
        Write-Error "install.ps1: no checksum entry for $asset in SHA256SUMS"
        exit 1
    }
    $expectedHash = ($expectedLine.Line -split '\s+')[0].ToLower()
    $actualHash = (Get-FileHash -Path $assetPath -Algorithm SHA256).Hash.ToLower()
    if ($actualHash -ne $expectedHash) {
        Write-Error "install.ps1: checksum mismatch for $asset (expected $expectedHash, got $actualHash)"
        exit 1
    }

    if (-not $LocalDir -and (Get-Command gh -ErrorAction SilentlyContinue)) {
        if (gh auth status 2>$null) {
            Write-Host "install.ps1: verifying build provenance attestation"
            $verified = gh attestation verify $assetPath --repo $repo 2>$null
            if ($LASTEXITCODE -eq 0) {
                Write-Host "install.ps1: build provenance attestation verified"
            } else {
                Write-Warning "install.ps1: could not verify a build provenance attestation for $asset"
            }
        }
    }

    New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
    $destination = Join-Path $InstallDir "$Binary.exe"
    Copy-Item -Path $assetPath -Destination $destination -Force

    Write-Host "install.ps1: installed $Binary $Version to $destination"
    & $destination version

    if (($env:Path -split ";") -notcontains $InstallDir) {
        Write-Host ""
        Write-Host "Add $InstallDir to your PATH, for example:"
        Write-Host "  [Environment]::SetEnvironmentVariable('Path', `"`$env:Path;$InstallDir`", 'User')"
    }
} finally {
    Remove-Item -Recurse -Force $tmpDir -ErrorAction SilentlyContinue
}

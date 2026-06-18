# install.ps1 — fetch okt.exe and hand off to `okt setup` for the
# bubbletea picker / env-var headless path. The PowerShell profile
# wrapper is now written by `okt setup` itself (see
# internal/installer.WritePowerShellWrappers) so this script stays a
# thin download + exec shim, mirroring install.sh on the Unix side.

$ErrorActionPreference = "Stop"

$Repo = "This-Is-NPC/omakiten"
$InstallDir = if ($env:INSTALL_DIR) { $env:INSTALL_DIR } else { "$env:LOCALAPPDATA\Programs\okt" }
$ChecksumBase = "https://github.com"

if ($env:OKT_ALLOW_MIRROR_CHECKSUM -eq "1") {
  if (-not $env:OKT_CHECKSUM_BASE) {
    throw "OKT_ALLOW_MIRROR_CHECKSUM=1 requires OKT_CHECKSUM_BASE to name the checksum mirror"
  }
  $ChecksumBase = $env:OKT_CHECKSUM_BASE.TrimEnd("/")
}

function Get-LatestTag {
  $release = Invoke-RestMethod -Uri "https://api.github.com/repos/$Repo/releases/latest"
  return $release.tag_name.TrimStart("v")
}

function Get-Arch {
  switch ($env:PROCESSOR_ARCHITECTURE) {
    "AMD64" { return "x86_64" }
    "ARM64" { return "arm64" }
    default { throw "unsupported architecture: $env:PROCESSOR_ARCHITECTURE" }
  }
}

# Verify-Checksum fetches the goreleaser-published checksums.txt for the
# release and verifies the downloaded archive against it BEFORE Expand-Archive
# and before okt.exe is ever copied to InstallDir / run. This mirrors the
# in-app updater's sha256-verify-before-extract gate
# (internal/cli/update.go:362-383) so the very first okt binary a user runs is
# verified the same way every later `okt update` is.
#
# Trust assumption: by default the canonical hash comes from checksums.txt
# fetched over HTTPS from GitHub, independent of any artifact mirror override.
# Mirrored checksums are allowed only via the explicit OKT_ALLOW_MIRROR_CHECKSUM
# + OKT_CHECKSUM_BASE opt-in, which means the caller is choosing that checksum
# trust root. This is NOT a substitute for signing / notarization / SLSA
# provenance (out of scope here).
function Verify-Checksum {
  param(
    [string]$Archive,
    [string]$Asset,
    [string]$Tag,
    [string]$TmpDir
  )
  $sumsUrl = "$ChecksumBase/$Repo/releases/download/v$Tag/checksums.txt"
  $sumsPath = Join-Path $TmpDir "checksums.txt"

  Write-Host "=> Verifying checksum against $sumsUrl"
  try {
    Invoke-WebRequest -Uri $sumsUrl -OutFile $sumsPath
  } catch {
    throw "failed to download checksums.txt from ${sumsUrl}: $($_.Exception.Message)"
  }

  # checksums.txt lines are "<sha256>  <filename>"; pull the row for our asset.
  $expected = $null
  foreach ($line in Get-Content $sumsPath) {
    $fields = $line -split '\s+', 2
    if ($fields.Count -eq 2 -and $fields[1].Trim() -eq $Asset) {
      $expected = $fields[0].Trim()
      break
    }
  }
  if (-not $expected) {
    throw "$Asset not listed in checksums.txt; aborting"
  }

  $actual = (Get-FileHash -Algorithm SHA256 -Path $Archive).Hash

  if ($expected.ToLowerInvariant() -ne $actual.ToLowerInvariant()) {
    throw ("checksum mismatch for {0}`n       expected: {1}`n       actual:   {2}`n       refusing to install a tampered or corrupt archive" -f $Asset, $expected, $actual)
  }
  Write-Host "=> Checksum OK ($($actual.ToLowerInvariant()))"
}

function Add-ToPath {
  param([string]$Dir)
  $current = [Environment]::GetEnvironmentVariable("Path", "User")
  if ($current -notlike "*$Dir*") {
    [Environment]::SetEnvironmentVariable("Path", "$current;$Dir", "User")
    Write-Host "=> Added $Dir to your user PATH. Restart your terminal to use 'okt'."
  }
}

$tag = if ($env:VERSION) { $env:VERSION } else { Get-LatestTag }
$arch = Get-Arch
$asset = "okt_Windows_${arch}.zip"
$url = "https://github.com/$Repo/releases/download/v$tag/$asset"
$tmpdir = [System.IO.Path]::GetTempPath() + [System.Guid]::NewGuid().ToString()

Write-Host "=> Installing okt v$tag for Windows $arch..."
Write-Host "=> Downloading $url"

New-Item -ItemType Directory -Path $tmpdir -Force | Out-Null
Invoke-WebRequest -Uri $url -OutFile "$tmpdir\$asset"

# Verify BEFORE extracting/executing: a mismatch throws ($ErrorActionPreference
# = "Stop") which exits non-zero here, before any okt.exe reaches InstallDir or
# PATH.
Verify-Checksum -Archive "$tmpdir\$asset" -Asset $asset -Tag $tag -TmpDir $tmpdir

Expand-Archive -Path "$tmpdir\$asset" -DestinationPath $tmpdir -Force

New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
Copy-Item -Path "$tmpdir\okt.exe" -Destination "$InstallDir\okt.exe" -Force
Remove-Item -Recurse -Force $tmpdir

Add-ToPath -Dir $InstallDir

$version = & "$InstallDir\okt.exe" --version
Write-Host "=> Installed $version"

& "$InstallDir\okt.exe" setup
if ($LASTEXITCODE -ne 0) {
  throw "okt setup failed with exit code $LASTEXITCODE"
}

Write-Host "=> Run 'okt --help' to get started"

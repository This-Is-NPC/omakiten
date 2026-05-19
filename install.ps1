# install.ps1 — fetch okt.exe and hand off to `okt setup` for the
# bubbletea picker / env-var headless path. The PowerShell profile
# wrapper is now written by `okt setup` itself (see
# internal/installer.WritePowerShellWrappers) so this script stays a
# thin download + exec shim, mirroring install.sh on the Unix side.

$ErrorActionPreference = "Stop"

$Repo = "This-Is-NPC/omakiten"
$InstallDir = if ($env:INSTALL_DIR) { $env:INSTALL_DIR } else { "$env:LOCALAPPDATA\Programs\okt" }

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

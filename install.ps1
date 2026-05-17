# install.ps1 — fetch okt.exe, write the PowerShell-profile wrapper,
# then exec `okt setup` for the bubbletea picker / env-var headless path.

$ErrorActionPreference = "Stop"

$Repo = "This-Is-NPC/omakiten"
$InstallDir = if ($env:INSTALL_DIR) { $env:INSTALL_DIR } else { "$env:LOCALAPPDATA\Programs\okt" }
# Sentinels byte-identical with internal/installer.WrapperBegin/End.
$WrapperBegin = "# >>> okt wrapper >>>"
$WrapperEnd = "# <<< okt wrapper <<<"

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

function Install-OktWrapper {
  $profilePath = $PROFILE.CurrentUserAllHosts
  if (-not (Test-Path $profilePath)) {
    New-Item -ItemType File -Path $profilePath -Force | Out-Null
  }
  $block = @"
$WrapperBegin
# Auto-installed by omakiten. Lets `okt tui` cd the parent shell into the
# project chosen on the Home screen when the TUI exits.
function okt {
    `$cdFile = if (`$env:OKT_CD_FILE) { `$env:OKT_CD_FILE } elseif (`$env:XDG_RUNTIME_DIR) { Join-Path `$env:XDG_RUNTIME_DIR 'okt-cd' } else { Join-Path `$env:TEMP 'okt-cd' }
    if (Test-Path `$cdFile) { Remove-Item `$cdFile -Force -ErrorAction SilentlyContinue }
    & okt.exe @args
    `$rc = `$LASTEXITCODE
    if (Test-Path `$cdFile) {
        `$target = (Get-Content `$cdFile -TotalCount 1).Trim()
        Remove-Item `$cdFile -Force -ErrorAction SilentlyContinue
        if (`$target -and (Test-Path `$target -PathType Container)) { Set-Location `$target }
    }
    return `$rc
}
$WrapperEnd
"@
  $existing = if (Test-Path $profilePath) { Get-Content $profilePath -Raw } else { "" }
  if ($existing -match [regex]::Escape($WrapperBegin)) {
    $pattern = [regex]::Escape($WrapperBegin) + "[\s\S]*?" + [regex]::Escape($WrapperEnd)
    Set-Content -Path $profilePath -Value ([regex]::Replace($existing, $pattern, $block)) -NoNewline
  } else {
    Add-Content -Path $profilePath -Value "`n$block"
  }
  Write-Host "=> Installed okt() wrapper into $profilePath"
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
Install-OktWrapper

$version = & "$InstallDir\okt.exe" --version
Write-Host "=> Installed $version"

# `--skip-wrapper` because Install-OktWrapper already wrote the PS profile.
& "$InstallDir\okt.exe" setup --skip-wrapper
if ($LASTEXITCODE -ne 0) {
  throw "okt setup failed with exit code $LASTEXITCODE"
}

Write-Host "=> Run 'okt --help' to get started"

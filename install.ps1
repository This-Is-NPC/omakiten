$ErrorActionPreference = "Stop"

$Repo = "This-Is-NPC/omakiten"
$InstallDir = if ($env:INSTALL_DIR) { $env:INSTALL_DIR } else { "$env:LOCALAPPDATA\Programs\okt" }

# Sentinels delimit the okt() PowerShell wrapper block in $PROFILE. Must
# match uninstall.ps1 byte-for-byte.
$WrapperBegin = "# >>> okt wrapper >>>"
$WrapperEnd = "# <<< okt wrapper <<<"

# Harnesses the multi-select prompt offers. Order matters: the prompt
# numbers options 1..N in this order. Keep in sync with the bash list
# in install.sh and with internal/agentsetup.SupportedHarnesses.
$SupportedHarnesses = @(
  "claude-code",
  "claude-desktop",
  "opencode",
  "crush",
  "github-copilot",
  "codex"
)

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

function Resolve-HarnessSelection {
  param([string]$Raw)
  $result = New-Object System.Collections.Generic.List[string]
  if (-not $Raw) { return $result }
  $tokens = [regex]::Split($Raw.Trim(), '[,\s]+') | Where-Object { $_ -ne '' }
  foreach ($token in $tokens) {
    if ($token -match '^\d+$') {
      $idx = [int]$token - 1
      if ($idx -ge 0 -and $idx -lt $SupportedHarnesses.Count) {
        $result.Add($SupportedHarnesses[$idx]) | Out-Null
      } else {
        Write-Host "warn: index $token out of range" -ForegroundColor DarkYellow
      }
    } elseif ($SupportedHarnesses -contains $token) {
      $result.Add($token) | Out-Null
    } else {
      Write-Host "warn: ignoring unknown harness `"$token`"" -ForegroundColor DarkYellow
    }
  }
  return $result
}

function Select-Harnesses {
  if ($env:OKT_HARNESSES) {
    return Resolve-HarnessSelection -Raw $env:OKT_HARNESSES
  }
  if (-not [Environment]::UserInteractive) {
    return @()
  }

  Write-Host ""
  Write-Host "=> Wire up MCP for your AI agents (optional)"
  for ($i = 0; $i -lt $SupportedHarnesses.Count; $i++) {
    Write-Host ("   {0}) {1}" -f ($i + 1), $SupportedHarnesses[$i])
  }
  $raw = Read-Host "`n   Enter numbers (e.g. 1,3,5), names, or press Enter to skip"
  return Resolve-HarnessSelection -Raw $raw
}

function Invoke-HarnessSetup {
  param([string]$OktBin, [System.Collections.Generic.List[string]]$Harnesses)
  if (-not $Harnesses -or $Harnesses.Count -eq 0) { return }
  foreach ($h in $Harnesses) {
    Write-Host "=> Configuring MCP for $h"
    & $OktBin mcp setup --harness $h --force 2>&1 | Out-Null
    if ($LASTEXITCODE -ne 0) {
      Write-Host ("   (skipped {0}, exit {1} - re-run manually: {2} mcp setup --harness {0} --force)" `
        -f $h, $LASTEXITCODE, $OktBin)
    }
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
# Remove with the bundled uninstall.ps1; do not edit by hand.
function okt {
    `$cdFile = if (`$env:OKT_CD_FILE) { `$env:OKT_CD_FILE } elseif (`$env:XDG_RUNTIME_DIR) { Join-Path `$env:XDG_RUNTIME_DIR 'okt-cd' } else { Join-Path `$env:TEMP 'okt-cd' }
    if (Test-Path `$cdFile) { Remove-Item `$cdFile -Force -ErrorAction SilentlyContinue }
    & okt.exe @args
    `$rc = `$LASTEXITCODE
    if (Test-Path `$cdFile) {
        `$target = (Get-Content `$cdFile -TotalCount 1).Trim()
        Remove-Item `$cdFile -Force -ErrorAction SilentlyContinue
        if (`$target -and (Test-Path `$target -PathType Container)) {
            Set-Location `$target
        }
    }
    return `$rc
}
$WrapperEnd
"@

  $existing = if (Test-Path $profilePath) { Get-Content $profilePath -Raw } else { "" }
  if ($existing -match [regex]::Escape($WrapperBegin)) {
    $pattern = [regex]::Escape($WrapperBegin) + "[\s\S]*?" + [regex]::Escape($WrapperEnd)
    $updated = [regex]::Replace($existing, $pattern, $block)
    Set-Content -Path $profilePath -Value $updated -NoNewline
  } else {
    Add-Content -Path $profilePath -Value "`n$block"
  }
  Write-Host "=> Installed okt() wrapper into $profilePath"
  Write-Host "   Open a new PowerShell session to enable cd-on-exit."
}

Install-OktWrapper

$version = & "$InstallDir\okt.exe" --version
Write-Host "=> Installed $version"

$selections = Select-Harnesses
Invoke-HarnessSetup -OktBin "$InstallDir\okt.exe" -Harnesses $selections

Write-Host "=> Run 'okt --help' to get started"

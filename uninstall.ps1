$ErrorActionPreference = "Stop"

$InstallDir = if ($env:INSTALL_DIR) { $env:INSTALL_DIR } else { "$env:LOCALAPPDATA\Programs\okt" }

# Sentinels delimit the okt() wrapper block. Must match install.ps1
# byte-for-byte.
$WrapperBegin = "# >>> okt wrapper >>>"
$WrapperEnd = "# <<< okt wrapper <<<"

function Remove-OktWrapper {
  $profilePath = $PROFILE.CurrentUserAllHosts
  if (-not (Test-Path $profilePath)) {
    return
  }
  $existing = Get-Content $profilePath -Raw
  if ($existing -notmatch [regex]::Escape($WrapperBegin)) {
    return
  }
  $pattern = "(?s)\s*" + [regex]::Escape($WrapperBegin) + ".*?" + [regex]::Escape($WrapperEnd) + "\s*"
  $updated = [regex]::Replace($existing, $pattern, "`n")
  Set-Content -Path $profilePath -Value $updated -NoNewline
  Write-Host "=> Removed okt() wrapper from $profilePath"
}

Remove-OktWrapper

if (Test-Path "$InstallDir\okt.exe") {
  Remove-Item "$InstallDir\okt.exe" -Force
  Write-Host "=> Removed $InstallDir\okt.exe"
}

Write-Host "=> Done. Open a new PowerShell session for the wrapper change to take effect."

$ErrorActionPreference = "Stop"

$InstallDir = if ($env:INSTALL_DIR) { $env:INSTALL_DIR } else { "$env:LOCALAPPDATA\Programs\okt" }

# Sentinels delimit the okt() wrapper block. Must match install.ps1
# byte-for-byte.
$WrapperBegin = "# >>> okt wrapper >>>"
$WrapperEnd = "# <<< okt wrapper <<<"

function Remove-OktWrapperFrom {
  param([string]$ProfilePath)
  if (-not (Test-Path $ProfilePath)) {
    return
  }
  $existing = Get-Content $ProfilePath -Raw
  if ($existing -notmatch [regex]::Escape($WrapperBegin)) {
    return
  }
  $pattern = "(?s)\s*" + [regex]::Escape($WrapperBegin) + ".*?" + [regex]::Escape($WrapperEnd) + "\s*"
  $updated = [regex]::Replace($existing, $pattern, "`n")
  Set-Content -Path $ProfilePath -Value $updated -NoNewline
  Write-Host "=> Removed okt() wrapper from $ProfilePath"
}

# Mirror the installer surface (internal/installer.PowerShellProfileTargets):
# scrub BOTH the PS 7 AllHosts profile (Documents\PowerShell\profile.ps1)
# and the PS 5.1 AllHosts profile (Documents\WindowsPowerShell\profile.ps1).
# `$PROFILE.CurrentUserAllHosts` only resolves to the host running this
# script, so a PS 7 uninstall would otherwise leak the wrapper into a
# PS 5.1 profile (and vice versa).
$home_ = if ($env:USERPROFILE) { $env:USERPROFILE } else { $HOME }
$profiles = @(
  (Join-Path $home_ "Documents\PowerShell\profile.ps1"),
  (Join-Path $home_ "Documents\WindowsPowerShell\profile.ps1")
)
foreach ($p in $profiles) {
  Remove-OktWrapperFrom -ProfilePath $p
}

if (Test-Path "$InstallDir\okt.exe") {
  Remove-Item "$InstallDir\okt.exe" -Force
  Write-Host "=> Removed $InstallDir\okt.exe"
}

Write-Host "=> Done. Open a new PowerShell session for the wrapper change to take effect."

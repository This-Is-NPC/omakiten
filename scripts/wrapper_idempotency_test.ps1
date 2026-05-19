#!/usr/bin/env pwsh
# Validates that `okt setup`'s PowerShell-profile wrapper writer is
# idempotent and that uninstall.ps1's `Remove-OktWrapper` inverts the
# install cleanly. Mirror of scripts/wrapper_idempotency_test.sh; the
# sentinels are byte-identical across the Go writer
# (internal/installer.WrapperBegin/End) and the PowerShell uninstaller,
# so a round-trip through both paths must leave the profile in its
# original state.
#
# Run with: pwsh -NoProfile -File scripts/wrapper_idempotency_test.ps1
$ErrorActionPreference = "Stop"

$repoRoot = Split-Path -Parent $PSScriptRoot
$tmproot = Join-Path ([System.IO.Path]::GetTempPath()) ([System.Guid]::NewGuid().ToString())
New-Item -ItemType Directory -Path $tmproot -Force | Out-Null

try {
    $tmpHome = Join-Path $tmproot "home"
    New-Item -ItemType Directory -Path $tmpHome -Force | Out-Null

    $isWin = $false
    if (Get-Variable -Name IsWindows -ErrorAction SilentlyContinue) { $isWin = $IsWindows }
    else { $isWin = $true } # PS 5.1: Windows-only

    if (-not $isWin) {
        Write-Host "SKIP: PowerShell wrapper writer is Windows-only (bash wrapper covers POSIX)"
        exit 0
    }

    $profilePath = Join-Path $tmpHome "Documents\PowerShell\profile.ps1"
    $profileDir = Split-Path -Parent $profilePath
    New-Item -ItemType Directory -Path $profileDir -Force | Out-Null
    Set-Content -Path $profilePath -Value "# user content above`n`$env:FOO = 'bar'`n" -NoNewline
    $orig = Get-Content -Path $profilePath -Raw

    $binName = if ($isWin) { "okt.exe" } else { "okt" }
    $bin = Join-Path $tmproot $binName
    Push-Location $repoRoot
    try {
        & go build -o $bin ./cmd/okt
        if ($LASTEXITCODE -ne 0) { throw "go build failed: $LASTEXITCODE" }
    } finally {
        Pop-Location
    }

    function Invoke-Setup {
        $envVars = @{
            HOME            = $tmpHome
            USERPROFILE     = $tmpHome
            OMAKITEN_HOME   = (Join-Path $tmpHome "oh")
            XDG_CONFIG_HOME = ""
            OKT_CLI_LANG    = "en"
            OKT_TUI_LANG    = "en"
            OKT_AGENT_LANG  = "en"
            OKT_PRESET      = "omakase"
            OKT_HARNESSES   = "0"
        }
        $saved = @{}
        foreach ($k in $envVars.Keys) {
            $saved[$k] = [Environment]::GetEnvironmentVariable($k, "Process")
            [Environment]::SetEnvironmentVariable($k, $envVars[$k], "Process")
        }
        try {
            & $bin setup --skip-harnesses | Out-Null
            if ($LASTEXITCODE -ne 0) { throw "okt setup failed: $LASTEXITCODE" }
        } finally {
            foreach ($k in $saved.Keys) {
                [Environment]::SetEnvironmentVariable($k, $saved[$k], "Process")
            }
        }
    }

    function Count-Sentinel {
        $content = Get-Content -Path $profilePath -Raw
        return ([regex]::Matches($content, [regex]::Escape("# >>> okt wrapper >>>"))).Count
    }

    Invoke-Setup
    $firstCount = Count-Sentinel
    if ($firstCount -ne 1) {
        Write-Host "FAIL: after first install, sentinel count = $firstCount (want 1)"
        exit 1
    }

    Invoke-Setup
    $secondCount = Count-Sentinel
    if ($secondCount -ne 1) {
        Write-Host "FAIL: re-install added duplicate sentinels (count = $secondCount)"
        exit 1
    }

    $content = Get-Content -Path $profilePath -Raw
    if ($content -notmatch [regex]::Escape("# user content above")) {
        Write-Host "FAIL: wrapper install clobbered unrelated content"
        exit 1
    }
    if ($content -notmatch [regex]::Escape("`$env:FOO = 'bar'")) {
        Write-Host "FAIL: wrapper install dropped the seeded line"
        exit 1
    }

    # uninstall.ps1 removes the block using the same sentinels; pin
    # HOME / USERPROFILE so $PROFILE.CurrentUserAllHosts resolves under
    # the seeded tmpdir rather than the developer's real profile.
    $saved = @{
        HOME        = [Environment]::GetEnvironmentVariable("HOME", "Process")
        USERPROFILE = [Environment]::GetEnvironmentVariable("USERPROFILE", "Process")
    }
    [Environment]::SetEnvironmentVariable("HOME", $tmpHome, "Process")
    [Environment]::SetEnvironmentVariable("USERPROFILE", $tmpHome, "Process")
    try {
        & pwsh -NoProfile -File (Join-Path $repoRoot "uninstall.ps1") | Out-Null
    } finally {
        foreach ($k in $saved.Keys) {
            [Environment]::SetEnvironmentVariable($k, $saved[$k], "Process")
        }
    }

    $content = Get-Content -Path $profilePath -Raw
    if ($content -match [regex]::Escape("# >>> okt wrapper >>>")) {
        Write-Host "FAIL: uninstall left sentinels in the profile"
        exit 1
    }
    if ($content -notmatch [regex]::Escape("`$env:FOO = 'bar'")) {
        Write-Host "FAIL: uninstall removed unrelated content"
        exit 1
    }

    Write-Host "OK: okt setup PowerShell wrapper writer is idempotent; uninstall.ps1 is surgical."
} finally {
    if (Test-Path $tmproot) { Remove-Item -Recurse -Force $tmproot }
}

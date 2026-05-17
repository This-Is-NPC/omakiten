#!/usr/bin/env pwsh
# Smoke-tests `okt setup` headless env-var path on Windows. Mirror of
# scripts/installer_select_test.sh; the interactive picker itself is
# covered by internal/cli/setup_picker_test.go.
#
# Run with: pwsh -NoProfile -File scripts/installer_select_test.ps1
$ErrorActionPreference = "Stop"

$repoRoot = Split-Path -Parent $PSScriptRoot
$tmproot = Join-Path ([System.IO.Path]::GetTempPath()) ([System.Guid]::NewGuid().ToString())
New-Item -ItemType Directory -Path $tmproot -Force | Out-Null
try {
    $bin = Join-Path $tmproot "okt.exe"
    Push-Location $repoRoot
    try {
        & go build -o $bin ./cmd/okt
        if ($LASTEXITCODE -ne 0) { throw "go build failed: $LASTEXITCODE" }
    } finally {
        Pop-Location
    }

    function Invoke-Setup {
        param([hashtable]$Env)
        $home = Join-Path $tmproot ("case-" + [System.Guid]::NewGuid().ToString("N"))
        New-Item -ItemType Directory -Path $home -Force | Out-Null
        $oh = Join-Path $home "oh"
        $stderrFile = Join-Path $home "stderr.txt"
        $envVars = @{
            HOME              = $home
            USERPROFILE       = $home
            OMAKITEN_HOME     = $oh
            XDG_CONFIG_HOME   = ""
        }
        foreach ($k in $Env.Keys) { $envVars[$k] = $Env[$k] }
        $saved = @{}
        foreach ($k in $envVars.Keys) {
            $saved[$k] = [Environment]::GetEnvironmentVariable($k, "Process")
            [Environment]::SetEnvironmentVariable($k, $envVars[$k], "Process")
        }
        try {
            $stdout = & $bin setup --skip-wrapper --skip-harnesses 2>$stderrFile
            return $stdout
        } finally {
            foreach ($k in $saved.Keys) {
                [Environment]::SetEnvironmentVariable($k, $saved[$k], "Process")
            }
        }
    }

    function Json-Get {
        param([string]$Path, [string]$Json)
        $obj = $Json | ConvertFrom-Json
        $cur = $obj.data
        foreach ($p in $Path.Split('.')) {
            if ($null -eq $cur) { return $null }
            $cur = $cur.$p
        }
        return $cur
    }

    function Assert-EnvelopeOk {
        param([string]$Label, [string]$Json)
        $obj = $Json | ConvertFrom-Json
        if (-not $obj.ok) {
            Write-Host "FAIL: $Label — envelope.ok = $($obj.ok)"
            Write-Host "  json: $Json"
            exit 1
        }
    }

    function Assert-Equal {
        param($Got, $Want, [string]$Label)
        if ("$Got" -ne "$Want") {
            Write-Host "FAIL: $Label"
            Write-Host "  got:  $Got"
            Write-Host "  want: $Want"
            exit 1
        }
    }

    # --- env-var driven harness selection ---

    $base = @{
        OKT_CLI_LANG   = "en"
        OKT_TUI_LANG   = "en"
        OKT_AGENT_LANG = "en"
        OKT_PRESET     = "omakase"
    }

    $out = Invoke-Setup ($base + @{ OKT_HARNESSES = "claude-code,opencode" })
    Assert-EnvelopeOk "harnesses csv (names)" $out
    Assert-Equal ((Json-Get "harnesses_planned" $out) -join ",") "claude-code,opencode" "harnesses_planned (names)"

    $out = Invoke-Setup ($base + @{ OKT_HARNESSES = "1,3" })
    Assert-EnvelopeOk "harnesses csv (indexes)" $out
    Assert-Equal ((Json-Get "harnesses_planned" $out) -join ",") "claude-code,opencode" "indexes resolve to canonical names"

    $out = Invoke-Setup ($base + @{ OKT_HARNESSES = "0" })
    Assert-EnvelopeOk "harnesses skip sentinel" $out
    $planned = Json-Get "harnesses_planned" $out
    if ($planned -ne $null -and $planned.Count -ne 0) {
        Write-Host "FAIL: skip sentinel must clear harness list (got $planned)"
        exit 1
    }

    # --- preset resolution ---

    $out = Invoke-Setup ($base + @{ OKT_PRESET = "izakaya"; OKT_HARNESSES = "0" })
    Assert-EnvelopeOk "preset izakaya" $out
    Assert-Equal (Json-Get "preset.name" $out) "izakaya" "OKT_PRESET=izakaya wins"

    $out = Invoke-Setup ($base + @{ OKT_PRESET = "bogus"; OKT_HARNESSES = "0" })
    Assert-EnvelopeOk "preset fallback" $out
    Assert-Equal (Json-Get "preset.name" $out) "omakase" "unknown OKT_PRESET falls back to omakase"

    # --- language resolution ---

    $out = Invoke-Setup @{
        OKT_CLI_LANG   = "pt-br"
        OKT_TUI_LANG   = "pt-br"
        OKT_AGENT_LANG = "Portugues"
        OKT_PRESET     = "omakase"
        OKT_HARNESSES  = "0"
    }
    Assert-EnvelopeOk "pt-br languages" $out
    Assert-Equal (Json-Get "languages.cli" $out) "pt-br" "languages.cli = pt-br"
    Assert-Equal (Json-Get "languages.tui" $out) "pt-br" "languages.tui = pt-br"

    Write-Host "OK: okt setup headless env-var path behaves as expected."
} finally {
    if (Test-Path $tmproot) { Remove-Item -Recurse -Force $tmproot }
}

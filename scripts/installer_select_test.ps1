#!/usr/bin/env pwsh
# Validates install.ps1's harness selection helpers using AST-driven extraction
# (avoids running the install.ps1 main flow, which downloads a release).
#
# Run with: pwsh -NoProfile -File scripts/installer_select_test.ps1
$ErrorActionPreference = "Stop"

$repoRoot = Split-Path -Parent $PSScriptRoot
$installScript = Join-Path $repoRoot "install.ps1"

$tokens = $null
$errors = $null
$ast = [System.Management.Automation.Language.Parser]::ParseFile($installScript, [ref]$tokens, [ref]$errors)
if ($errors -and $errors.Count -gt 0) {
    Write-Host "FAIL: install.ps1 has parse errors:"
    $errors | ForEach-Object { Write-Host "  $($_.Message)" }
    exit 1
}

# Extract: $SupportedHarnesses assignment + helper function definitions.
$wantedFunctions = @(
    "Resolve-HarnessSelection",
    "Select-Harnesses",
    "Invoke-HarnessSetup"
)

$snippets = New-Object System.Collections.Generic.List[string]

# Variable assignment for $SupportedHarnesses
$ast.FindAll({
    param($node)
    $node -is [System.Management.Automation.Language.AssignmentStatementAst] -and
    $node.Left.Extent.Text -eq '$SupportedHarnesses'
}, $true) | ForEach-Object { $snippets.Add($_.Extent.Text) | Out-Null }

# Function definitions
$ast.FindAll({
    param($node)
    $node -is [System.Management.Automation.Language.FunctionDefinitionAst] -and
    $wantedFunctions -contains $node.Name
}, $true) | ForEach-Object { $snippets.Add($_.Extent.Text) | Out-Null }

if ($snippets.Count -lt ($wantedFunctions.Count + 1)) {
    Write-Host "FAIL: extracted $($snippets.Count) snippets; expected $($wantedFunctions.Count + 1) (1 var + $($wantedFunctions.Count) functions)"
    exit 1
}

Invoke-Expression ($snippets -join "`n")

function Assert-Equal {
    param([string]$Got, [string]$Want, [string]$Label)
    if ($Got -ne $Want) {
        Write-Host "FAIL: $Label"
        Write-Host "  got:  '$Got'"
        Write-Host "  want: '$Want'"
        exit 1
    }
}

# --- Resolve-HarnessSelection: shape ---
# Resolve-HarnessSelection returns @{Harnesses=List[string]; Status=string}.
# Status is one of: ok | invalid | skip | empty (drives the retry loop).

function Get-Selection {
    param([string]$Raw)
    $r = Resolve-HarnessSelection -Raw $Raw 6>$null
    return ($r.Harnesses) -join "|"
}

function Get-Status {
    param([string]$Raw)
    return (Resolve-HarnessSelection -Raw $Raw 6>$null).Status
}

Assert-Equal (Get-Selection "1,3,5") "claude-code|opencode|github-copilot" "numeric comma list"
Assert-Equal (Get-Selection "1 3 5") "claude-code|opencode|github-copilot" "numeric space list"
Assert-Equal (Get-Selection "claude-code,codex") "claude-code|codex" "name comma list"
Assert-Equal (Get-Selection "1 codex") "claude-code|codex" "mixed numeric and name"
Assert-Equal (Get-Selection "") "" "empty input"
Assert-Equal (Get-Selection "99 bogus claude-code") "claude-code" "invalid tokens skipped, valid kept"
Assert-Equal (Get-Selection "1,bogus") "claude-code" "partial-valid input keeps valid"

# --- Resolve-HarnessSelection: status drives the retry loop ---

Assert-Equal (Get-Status "1,3,5")     "ok"      "status ok on full-valid input"
Assert-Equal (Get-Status "1,bogus")   "ok"      "status ok on partial-valid input"
Assert-Equal (Get-Status "")          "empty"   "status empty on blank input"
Assert-Equal (Get-Status "   ")       "empty"   "status empty on whitespace-only input"
Assert-Equal (Get-Status "bogus")     "invalid" "status invalid when no token matched"
Assert-Equal (Get-Status "99")        "invalid" "status invalid on out-of-range numeric"
Assert-Equal (Get-Status "0")         "skip"    "status skip on '0'"
Assert-Equal (Get-Status "skip")      "skip"    "status skip on 'skip'"
Assert-Equal (Get-Status "Skip")      "skip"    "status skip on 'Skip' (case-insensitive)"
Assert-Equal (Get-Status "NONE")      "skip"    "status skip on 'NONE' (case-insensitive)"
Assert-Equal (Get-Status "1,0")       "skip"    "skip wins over a valid token"
Assert-Equal (Get-Status "bogus,skip") "skip"   "skip wins over invalid tokens"

# Skip status must produce an empty Harnesses list.
Assert-Equal (Get-Selection "1,0") "" "'1,0' yields no harnesses (skip wins)"
Assert-Equal (Get-Selection "skip") "" "'skip' yields no harnesses"

# --- Select-Harnesses honors OKT_HARNESSES ---

$env:OKT_HARNESSES = "claude-code,opencode"
try {
    $got = (Select-Harnesses) -join "|"
    Assert-Equal $got "claude-code|opencode" "OKT_HARNESSES env override"
} finally {
    Remove-Item Env:OKT_HARNESSES -ErrorAction SilentlyContinue
}

$env:OKT_HARNESSES = "crush`tcodex, ,"
try {
    $got = (Select-Harnesses) -join "|"
    Assert-Equal $got "crush|codex" "OKT_HARNESSES tolerates mixed separators"
} finally {
    Remove-Item Env:OKT_HARNESSES -ErrorAction SilentlyContinue
}

# OKT_HARNESSES with skip sentinel yields nothing (env-override path obeys skip).
$env:OKT_HARNESSES = "1,0"
try {
    $got = (Select-Harnesses) -join "|"
    Assert-Equal $got "" "OKT_HARNESSES with '0' honors skip"
} finally {
    Remove-Item Env:OKT_HARNESSES -ErrorAction SilentlyContinue
}

# --- $SupportedHarnesses count must match agentsetup.SupportedHarnesses ---

$wantCount = 6
if ($SupportedHarnesses.Count -ne $wantCount) {
    Write-Host "FAIL: SupportedHarnesses has $($SupportedHarnesses.Count) entries, want $wantCount"
    Write-Host "      (sync with internal/agentsetup.SupportedHarnesses)"
    exit 1
}

Write-Host "OK: install.ps1 harness selection helpers behave as expected."

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

# --- Resolve-HarnessSelection ---

$got = (Resolve-HarnessSelection -Raw "1,3,5") -join "|"
Assert-Equal $got "claude-code|opencode|github-copilot" "numeric comma list"

$got = (Resolve-HarnessSelection -Raw "1 3 5") -join "|"
Assert-Equal $got "claude-code|opencode|github-copilot" "numeric space list"

$got = (Resolve-HarnessSelection -Raw "claude-code,codex") -join "|"
Assert-Equal $got "claude-code|codex" "name comma list"

$got = (Resolve-HarnessSelection -Raw "1 codex") -join "|"
Assert-Equal $got "claude-code|codex" "mixed numeric and name"

$got = (Resolve-HarnessSelection -Raw "") -join "|"
Assert-Equal $got "" "empty input"

$got = (Resolve-HarnessSelection -Raw "99 bogus claude-code" 6>$null) -join "|"
Assert-Equal $got "claude-code" "invalid tokens skipped, valid kept"

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

# --- $SupportedHarnesses count must match agentsetup.SupportedHarnesses ---

$wantCount = 6
if ($SupportedHarnesses.Count -ne $wantCount) {
    Write-Host "FAIL: SupportedHarnesses has $($SupportedHarnesses.Count) entries, want $wantCount"
    Write-Host "      (sync with internal/agentsetup.SupportedHarnesses)"
    exit 1
}

Write-Host "OK: install.ps1 harness selection helpers behave as expected."

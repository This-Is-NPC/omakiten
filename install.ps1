# install.ps1 — fetch okt.exe and hand off to `okt setup` for the
# bubbletea picker / env-var headless path. The PowerShell profile
# wrapper is now written by `okt setup` itself (see
# internal/installer.WritePowerShellWrappers) so this script stays a
# thin download + verify + exec shim, mirroring install.sh on the Unix side.
#
# TWO VERIFICATION MODES ($env:OKT_VERIFY_MODE):
#
#   checksum (default) — the convenience path. Downloads the archive and
#     verifies its SHA-256 against the published checksums.txt before
#     extracting. This is a corruption and swap check, NOT an authenticity
#     check: it trusts the installer script you just fetched and it trusts
#     GitHub's TLS. It is not end-to-end verified.
#
#   strict — the verified bootstrap path. Requires a Cosign you installed
#     yourself, authenticates the version-bound manifest, checksums.txt, and
#     SLSA provenance against a pinned OIDC issuer and a pinned release
#     workflow identity, and only then compares the archive digest. It fails
#     closed: there is no unsigned fallback anywhere inside strict mode, and
#     the installer never downloads or implicitly executes a verifier.

$ErrorActionPreference = "Stop"
$ProgressPreference = "SilentlyContinue"

# Native command failures are surfaced through explicit $LASTEXITCODE checks so
# the installer can explain *which* trust decision failed instead of letting
# PowerShell rethrow cosign's raw stderr.
if (Get-Variable -Name PSNativeCommandUseErrorActionPreference -ErrorAction SilentlyContinue) {
  $PSNativeCommandUseErrorActionPreference = $false
}

# Stop-Install writes an unwrapped refusal to stderr and exits non-zero.
# PowerShell's exception formatter re-wraps long lines and injects ANSI
# decoration, which mangles multi-line trust errors; a refusal reason must stay
# readable — and greppable — exactly as written.
function Stop-Install {
  param([string]$Message)
  [Console]::Error.WriteLine("error: $Message")
  exit 1
}

$Repo = "This-Is-NPC/omakiten"
$InstallDir = if ($env:INSTALL_DIR) { $env:INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA "Programs\okt" }

# Base hosts default to GitHub but are overridable for release lookup and
# artifact downloads so hermetic tests (and any release mirror) can point those
# requests at a local endpoint without rewriting URL logic.
$ApiBase = if ($env:GITHUB_API_BASE) { $env:GITHUB_API_BASE.TrimEnd("/") } else { "https://api.github.com" }
$DlBase = if ($env:GITHUB_DL_BASE) { $env:GITHUB_DL_BASE.TrimEnd("/") } else { "https://github.com" }
$ChecksumBase = "https://github.com"

$VerifyMode = if ($env:OKT_VERIFY_MODE) { $env:OKT_VERIFY_MODE } else { "checksum" }
if ($VerifyMode -ne "checksum" -and $VerifyMode -ne "strict") {
  Stop-Install "OKT_VERIFY_MODE must be 'checksum' or 'strict' (got '$VerifyMode')"
}

# --- pinned release trust anchors -------------------------------------------
#
# These values are the PowerShell twin of the constants compiled into
# internal/releaseverify, the strict verifier `okt update` uses. They are
# deliberately exact strings, never patterns: a regex identity would let any
# repository or ref whose SAN merely *contains* the expected substring pass.
# internal/installscript asserts they stay in step with the Go package.
$CosignOidcIssuer = "https://token.actions.githubusercontent.com"
$CosignCertIdentity = "https://github.com/$Repo/.github/workflows/release.yml@refs/heads/master"
# Last release published without signed metadata. Anything at or before it can
# never be authenticated after the fact, so strict mode refuses it outright.
$SignedReleaseCutoff = "0.30.0"
# Bundles are published in the Sigstore bundle format cosign v3 writes.
$CosignMinVersion = "3.0.0"
# The complete published archive matrix, mirroring releasemeta.ArchiveNames().
# Used to reject a truncated or padded manifest.
$ReleaseArchives = @(
  "okt_Darwin_arm64.tar.gz",
  "okt_Darwin_x86_64.tar.gz",
  "okt_Linux_arm64.tar.gz",
  "okt_Linux_x86_64.tar.gz",
  "okt_Windows_arm64.zip",
  "okt_Windows_x86_64.zip"
)

# The mirrored-checksum opt-in only exists for the convenience path, where the
# checksum host *is* the trust root. Strict mode derives trust from the
# signature rather than from the host, so the opt-in is inert there and mirrors
# are covered by the "authenticated metadata or nothing" rule instead.
if ($VerifyMode -eq "checksum" -and $env:OKT_ALLOW_MIRROR_CHECKSUM -eq "1") {
  if (-not $env:OKT_CHECKSUM_BASE) {
    Stop-Install "OKT_ALLOW_MIRROR_CHECKSUM=1 requires OKT_CHECKSUM_BASE to name the checksum mirror"
  }
  $ChecksumBase = $env:OKT_CHECKSUM_BASE.TrimEnd("/")
}

function Get-LatestTag {
  $release = Invoke-RestMethod -Uri "$ApiBase/repos/$Repo/releases/latest"
  return $release.tag_name.TrimStart("v")
}

function Get-Arch {
  switch ($env:PROCESSOR_ARCHITECTURE) {
    "AMD64" { return "x86_64" }
    "ARM64" { return "arm64" }
    default { Stop-Install "unsupported architecture: $env:PROCESSOR_ARCHITECTURE" }
  }
}

# Verify-Checksum fetches the goreleaser-published checksums.txt for the
# release and verifies the downloaded archive against it BEFORE Expand-Archive
# and before okt.exe is ever copied to InstallDir / run.
#
# Trust assumption: by default the canonical hash comes from checksums.txt
# fetched over HTTPS from GitHub, independent of any artifact mirror override.
# Mirrored checksums are allowed only via the explicit OKT_ALLOW_MIRROR_CHECKSUM
# + OKT_CHECKSUM_BASE opt-in, which means the caller is choosing that checksum
# trust root. This is NOT signature verification: use OKT_VERIFY_MODE=strict
# for an authenticated install.
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
    Stop-Install "failed to download checksums.txt from ${sumsUrl}: $($_.Exception.Message)"
  }

  $expected = Get-ChecksumEntry -SumsPath $sumsPath -Asset $Asset
  if (-not $expected) {
    Stop-Install "$Asset not listed in checksums.txt; aborting"
  }

  $actual = (Get-FileHash -Algorithm SHA256 -Path $Archive).Hash

  if ($expected.ToLowerInvariant() -ne $actual.ToLowerInvariant()) {
    Stop-Install ("checksum mismatch for {0}`n       expected: {1}`n       actual:   {2}`n       refusing to install a tampered or corrupt archive" -f $Asset, $expected, $actual)
  }
  Write-Host "=> Checksum OK ($($actual.ToLowerInvariant()))"
}

# Get-ChecksumEntry reads the "<sha256>  <filename>" row for one asset.
function Get-ChecksumEntry {
  param([string]$SumsPath, [string]$Asset)
  foreach ($line in Get-Content $SumsPath) {
    $fields = $line -split '\s+', 2
    if ($fields.Count -eq 2 -and $fields[1].Trim() -eq $Asset) {
      return $fields[0].Trim()
    }
  }
  return $null
}

# Write-ConvenienceTrustNotice states plainly what the default path does and
# does not prove, so nobody reads "Checksum OK" as "authenticated".
function Write-ConvenienceTrustNotice {
  Write-Host "=> Convenience mode: this install is NOT signature-verified."
  Write-Host "   It trusts the installer script you fetched and GitHub's TLS; the checksum"
  Write-Host "   check below only proves the archive matches the checksums.txt served"
  Write-Host "   alongside it. For an authenticated install, install Cosign yourself and"
  Write-Host "   re-run with OKT_VERIFY_MODE=strict."
}

# --- strict verification ----------------------------------------------------

# Compare-ReleaseVersion returns -1, 0, or 1 for "$Left <=> $Right" over
# MAJOR.MINOR.PATCH with SemVer prerelease ordering, matching
# releaseverify.CompareVersions for the tags the release workflow can emit.
function Compare-ReleaseVersion {
  param([string]$Left, [string]$Right)
  $l = $Left.TrimStart("v")
  $r = $Right.TrimStart("v")
  $lSplit = $l -split "-", 2
  $rSplit = $r -split "-", 2
  $lCore = $lSplit[0]; $lPre = if ($lSplit.Count -eq 2) { $lSplit[1] } else { "" }
  $rCore = $rSplit[0]; $rPre = if ($rSplit.Count -eq 2) { $rSplit[1] } else { "" }
  $lParts = $lCore -split '\.'
  $rParts = $rCore -split '\.'
  for ($i = 0; $i -lt 3; $i++) {
    $comparison = Compare-NumericIdentifier $lParts[$i] $rParts[$i]
    if ($comparison -ne 0) { return $comparison }
  }
  if ($lPre -eq $rPre) { return 0 }
  if (-not $lPre) { return 1 }
  if (-not $rPre) { return -1 }
  $lIds = $lPre -split '\.'; $rIds = $rPre -split '\.'
  for ($i = 0; $i -lt $lIds.Count -and $i -lt $rIds.Count; $i++) {
    if ($lIds[$i] -ceq $rIds[$i]) { continue }
    $lNumeric = $lIds[$i] -cmatch '^[0-9]+$'
    $rNumeric = $rIds[$i] -cmatch '^[0-9]+$'
    if ($lNumeric -and -not $rNumeric) { return -1 }
    if (-not $lNumeric -and $rNumeric) { return 1 }
    if ($lNumeric) { return Compare-NumericIdentifier $lIds[$i] $rIds[$i] }
    $comparison = [string]::CompareOrdinal($lIds[$i], $rIds[$i])
    if ($comparison -lt 0) { return -1 }
    return 1
  }
  if ($lIds.Count -lt $rIds.Count) { return -1 }
  if ($lIds.Count -gt $rIds.Count) { return 1 }
  return 0
}

function Compare-NumericIdentifier {
  param([string]$Left, [string]$Right)
  if ($Left.Length -lt $Right.Length) { return -1 }
  if ($Left.Length -gt $Right.Length) { return 1 }
  $comparison = [string]::CompareOrdinal($Left, $Right)
  if ($comparison -lt 0) { return -1 }
  if ($comparison -gt 0) { return 1 }
  return 0
}

function Assert-ReleaseVersionShape {
  param([string]$Tag)
  if ($Tag -notmatch '^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-((0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*)(\.(0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*))*))?$') {
    Stop-Install "strict mode needs a normalized release version like 1.2.3 (got '$Tag')"
  }
}

function Get-CosignMissingGuidance {
  return @"
OKT_VERIFY_MODE=strict requires Cosign v$CosignMinVersion or newer, and none was found.
       Strict mode never downloads or runs a verifier for you — install Cosign
       yourself, from a source you trust, and re-run:
         Windows amd64 : download cosign-windows-amd64.exe from
                         https://github.com/sigstore/cosign/releases
         Windows arm64 : Sigstore publishes no native windows/arm64 Cosign build. The
                         supported arrangement is the signed cosign-windows-amd64.exe
                         running under Windows-on-ARM x64 emulation.
       Set OKT_COSIGN to the full path if the binary is not on PATH.
"@
}

# Resolve-Cosign returns the user-installed verifier and enforces the minimum
# supported version. It runs BEFORE anything is downloaded, so a host without a
# verifier never fetches a release at all.
function Resolve-Cosign {
  $path = $env:OKT_COSIGN
  if (-not $path) {
    $command = Get-Command cosign -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1
    if ($command) { $path = $command.Source }
  }
  if (-not $path -or -not (Test-Path -LiteralPath $path)) {
    Stop-Install (Get-CosignMissingGuidance)
  }

  $raw = (& $path version 2>&1 | Out-String)
  if ($LASTEXITCODE -ne 0) {
    Stop-Install (Get-CosignMissingGuidance)
  }
  $match = [regex]::Match($raw, '(?m)^\s*GitVersion:\s*v?([0-9][0-9.]*)')
  if (-not $match.Success) {
    Stop-Install "could not determine the version of $path; refusing to verify with an unknown verifier"
  }
  $version = $match.Groups[1].Value
  if ((Compare-ReleaseVersion -Left $version -Right $CosignMinVersion) -lt 0) {
    Stop-Install "$path is v$version; strict mode needs v$CosignMinVersion or newer (older Cosign cannot read the Sigstore bundle format these releases publish)"
  }
  Write-Host "=> Strict verification mode — using $path v$version"
  return $path
}

# Assert-SignedRelease refuses any target at or before the signed cutoff. There
# is no override: those releases have no authenticated metadata and never will,
# so strict mode simply does not support them.
function Assert-SignedRelease {
  param([string]$Tag)
  Assert-ReleaseVersionShape -Tag $Tag
  if ((Compare-ReleaseVersion -Left $Tag -Right $SignedReleaseCutoff) -le 0) {
    Stop-Install (("okt v{0} is at or before the signed-release cutoff v{1}.`n" +
      "       Releases up to and including v{1} were published without signed metadata`n" +
      "       and can never be authenticated after the fact, so strict mode does not`n" +
      "       support them. Install a release newer than v{1}.") -f $Tag, $SignedReleaseCutoff)
  }
}

# Get-RequiredReleaseFile downloads one release file or aborts. Strict mode has
# no unsigned fallback, so a missing metadata file is a hard failure — a
# stripped release must never demote the client to checksum-only trust.
function Get-RequiredReleaseFile {
  param([string]$Uri, [string]$OutFile, [string]$Label)
  try {
    Invoke-WebRequest -Uri $Uri -OutFile $OutFile
  } catch {
    Stop-Install ("could not download the {0} from {1}`n       strict mode has no unsigned fallback; refusing to install." -f $Label, $Uri)
  }
}

# Invoke-CosignVerifyBlob authenticates a detached blob signature. No
# --insecure-ignore-* flag and no regex matcher is ever passed, so cosign's
# default Rekor transparency-log and Fulcio SCT requirements stay in force.
function Invoke-CosignVerifyBlob {
  param([string]$Cosign, [string]$Bundle, [string]$Artifact, [string]$Label)
  & $Cosign verify-blob `
    --bundle $Bundle `
    --certificate-identity $CosignCertIdentity `
    --certificate-oidc-issuer $CosignOidcIssuer `
    $Artifact | Out-Null
  if ($LASTEXITCODE -ne 0) {
    Stop-Install (("could not authenticate the {0}.`n" +
      "       The signature must come from {1}`n" +
      "       issued by {2}, with Rekor transparency-log evidence.`n" +
      "       Refusing to install.") -f $Label, $CosignCertIdentity, $CosignOidcIssuer)
  }
}

# Invoke-CosignVerifyAttestation authenticates the SLSA provenance and requires
# the archive to appear as a digest-bound subject of the signed statement.
function Invoke-CosignVerifyAttestation {
  param([string]$Cosign, [string]$Bundle, [string]$Artifact)
  & $Cosign verify-blob-attestation `
    --bundle $Bundle `
    --certificate-identity $CosignCertIdentity `
    --certificate-oidc-issuer $CosignOidcIssuer `
    --type slsaprovenance1 `
    $Artifact | Out-Null
  if ($LASTEXITCODE -ne 0) {
    Stop-Install (("could not authenticate the SLSA provenance for {0}.`n" +
      "       The archive is not a signed subject of this release's provenance statement.`n" +
      "       Refusing to install.") -f (Split-Path -Leaf $Artifact))
  }
}

function Get-ManifestDigest {
  param($Manifest, [string]$Name)
  $entry = $Manifest.artifacts | Where-Object { $_.name -ceq $Name } | Select-Object -First 1
  if (-not $entry) { return $null }
  return $entry.sha256
}

function Read-ExactChecksums {
  param([string]$Path)
  $expected = [System.Collections.Generic.HashSet[string]]::new([StringComparer]::Ordinal)
  foreach ($name in $ReleaseArchives) { [void]$expected.Add($name) }
  $result = [System.Collections.Generic.Dictionary[string,string]]::new([StringComparer]::Ordinal)
  foreach ($line in Get-Content -LiteralPath $Path) {
    $fields = $line -split '\s+'
    if ($fields.Count -ne 2 -or $fields[0] -cnotmatch '^[0-9a-f]{64}$' -or -not $expected.Contains($fields[1]) -or $result.ContainsKey($fields[1])) {
      Stop-Install "signed checksums.txt must contain exactly one entry for each published archive and no others"
    }
    $result.Add($fields[1], $fields[0])
  }
  if ($result.Count -ne $ReleaseArchives.Count) {
    Stop-Install "signed checksums.txt must contain exactly one entry for each published archive and no others"
  }
  return $result
}

function Assert-AuthenticatedMetadata {
  param($Manifest, [string]$ManifestPath, [string]$ChecksumsPath, [string]$ProvenanceBundle, [string]$Tag, [string]$SourceCommit)

  $expectedNames = @($ReleaseArchives + @("checksums.txt"))
  if ($Manifest -isnot [PSCustomObject] -or $Manifest.artifacts -isnot [System.Array] -or @($Manifest.artifacts).Count -ne $expectedNames.Count) {
    Stop-Install "signed manifest must contain exactly $($expectedNames.Count) release artifacts"
  }
  foreach ($name in $expectedNames) {
    $entries = @($Manifest.artifacts | Where-Object { $_.name -ceq $name })
    if ($entries.Count -ne 1 -or $entries[0] -isnot [PSCustomObject] -or $entries[0].name -isnot [string] -or
        $entries[0].sha256 -isnot [string] -or $entries[0].sha256 -cnotmatch '^[0-9a-f]{64}$') {
      Stop-Install "signed manifest must contain exactly one valid digest for $name"
    }
  }

  $checksums = Read-ExactChecksums -Path $ChecksumsPath
  foreach ($name in $ReleaseArchives) {
    if ($checksums[$name] -cne (Get-ManifestDigest -Manifest $Manifest -Name $name)) {
      Stop-Install "signed checksums.txt digest for $name disagrees with the signed manifest"
    }
  }
  $wholeDigest = (Get-FileHash -Algorithm SHA256 -LiteralPath $ChecksumsPath).Hash.ToLowerInvariant()
  if ($wholeDigest -cne (Get-ManifestDigest -Manifest $Manifest -Name "checksums.txt")) {
    Stop-Install "signed manifest digest for checksums.txt does not match the authenticated whole file"
  }

  try {
    $bundle = Get-Content -Raw -LiteralPath $ProvenanceBundle | ConvertFrom-Json
    if ($bundle -isnot [PSCustomObject] -or $bundle.dsseEnvelope -isnot [PSCustomObject] -or
        $bundle.dsseEnvelope.payloadType -isnot [string] -or $bundle.dsseEnvelope.payload -isnot [string] -or
        $bundle.dsseEnvelope.payloadType -cne "application/vnd.in-toto+json") { throw "wrong payload type" }
    $payload = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String($bundle.dsseEnvelope.payload))
    $statement = $payload | ConvertFrom-Json
  } catch {
    Stop-Install "authenticated provenance bundle does not carry a valid in-toto DSSE JSON payload"
  }
  $workflow = $CosignCertIdentity
  $sourceUri = "git+https://github.com/$Repo@refs/tags/v$Tag"
  $definition = $statement.predicate.buildDefinition
  $dependency = @($definition.resolvedDependencies)
  $dependencyDigestProperties = @($dependency[0].digest.PSObject.Properties)
  $invocationPattern = '^https://github\.com/' + [regex]::Escape($Repo) + '/actions/runs/[0-9]+/attempts/[0-9]+$'
  if ($statement -isnot [PSCustomObject] -or $statement._type -isnot [string] -or $statement.predicateType -isnot [string] -or
      $statement.predicate -isnot [PSCustomObject] -or $definition -isnot [PSCustomObject] -or
      $definition.externalParameters -isnot [PSCustomObject] -or $statement.predicate.runDetails -isnot [PSCustomObject] -or
      $statement.predicate.runDetails.builder -isnot [PSCustomObject] -or $statement.predicate.runDetails.metadata -isnot [PSCustomObject] -or
      $definition.buildType -isnot [string] -or $statement.predicate.runDetails.builder.id -isnot [string] -or
      $definition.externalParameters.repository -isnot [string] -or $definition.externalParameters.tag -isnot [string] -or
      $definition.externalParameters.commit -isnot [string] -or $definition.resolvedDependencies -isnot [System.Array] -or
      $statement.predicate.runDetails.metadata.invocationId -isnot [string] -or
      $statement._type -cne "https://in-toto.io/Statement/v1" -or
      $statement.predicateType -cne "https://slsa.dev/provenance/v1" -or
      $definition.buildType -cne $workflow -or
      $statement.predicate.runDetails.builder.id -cne $workflow -or
      $definition.externalParameters.repository -cne $Repo -or
      $definition.externalParameters.tag -cne "v$Tag" -or
      $definition.externalParameters.commit -cne $SourceCommit -or
      $dependency.Count -ne 1 -or $dependency[0] -isnot [PSCustomObject] -or $dependency[0].uri -isnot [string] -or
      $dependency[0].digest -isnot [PSCustomObject] -or $dependency[0].uri -cne $sourceUri -or
      $dependencyDigestProperties.Count -ne 1 -or $dependencyDigestProperties[0].Name -cne "gitCommit" -or
      $dependency[0].digest.gitCommit -isnot [string] -or $dependency[0].digest.gitCommit -cne $SourceCommit -or
      $statement.predicate.runDetails.metadata.invocationId -cnotmatch $invocationPattern) {
    Stop-Install "authenticated provenance source identity, build type, builder, or exact resolved dependency is not bound to this release"
  }
  $subjects = @($statement.subject)
  if ($statement.subject -isnot [System.Array] -or $subjects.Count -ne $ReleaseArchives.Count) {
    Stop-Install "authenticated provenance must contain exactly $($ReleaseArchives.Count) archive subjects"
  }
  foreach ($name in $ReleaseArchives) {
    $entries = @($subjects | Where-Object { $_.name -ceq $name })
    $digestProperties = if ($entries.Count -eq 1) { @($entries[0].digest.PSObject.Properties) } else { @() }
    if ($entries.Count -ne 1 -or $entries[0] -isnot [PSCustomObject] -or $entries[0].name -isnot [string] -or
        $entries[0].digest -isnot [PSCustomObject] -or $digestProperties.Count -ne 1 -or
        $digestProperties[0].Name -cne "sha256" -or $entries[0].digest.sha256 -isnot [string] -or
        $entries[0].digest.sha256 -cne (Get-ManifestDigest -Manifest $Manifest -Name $name)) {
      Stop-Install "authenticated provenance subjects do not exactly match $name and its manifest digest"
    }
  }
}

# Invoke-StrictVerification is the fail-closed bootstrap gate. It follows the
# same order internal/releaseverify uses for `okt update`: authenticate the
# metadata bytes first, only then parse them, then bind the provenance to the
# archive, and only then compare digests. Nothing is extracted until every step
# passed.
function Invoke-StrictVerification {
  param(
    [string]$Cosign,
    [string]$Archive,
    [string]$Asset,
    [string]$Tag,
    [string]$TmpDir
  )
  $base = "$DlBase/$Repo/releases/download/v$Tag"

  $manifestPath = Join-Path $TmpDir "release-manifest-v$Tag.json"
  $manifestBundle = Join-Path $TmpDir "release-manifest-v$Tag.sigstore.json"
  $checksumsPath = Join-Path $TmpDir "checksums.txt"
  $checksumsBundle = Join-Path $TmpDir "checksums-v$Tag.sigstore.json"
  $provenanceBundle = Join-Path $TmpDir "release-provenance-v$Tag.sigstore.json"

  Write-Host "=> Fetching signed release metadata for v$Tag"
  Get-RequiredReleaseFile -Uri "$base/release-manifest-v$Tag.json" -OutFile $manifestPath -Label "release manifest"
  Get-RequiredReleaseFile -Uri "$base/release-manifest-v$Tag.sigstore.json" -OutFile $manifestBundle -Label "release manifest signature"
  Get-RequiredReleaseFile -Uri "$base/checksums.txt" -OutFile $checksumsPath -Label "checksums.txt"
  Get-RequiredReleaseFile -Uri "$base/checksums-v$Tag.sigstore.json" -OutFile $checksumsBundle -Label "checksums.txt signature"
  Get-RequiredReleaseFile -Uri "$base/release-provenance-v$Tag.sigstore.json" -OutFile $provenanceBundle -Label "release provenance"

  # 1. Authenticate the manifest bytes.
  Invoke-CosignVerifyBlob -Cosign $Cosign -Bundle $manifestBundle -Artifact $manifestPath -Label "release manifest"
  Write-Host "=> Signature OK (release manifest)"

  # 2. Authenticate the checksums.txt bytes.
  Invoke-CosignVerifyBlob -Cosign $Cosign -Bundle $checksumsBundle -Artifact $checksumsPath -Label "checksums.txt"
  Write-Host "=> Signature OK (checksums.txt)"

  # 3. Only now is it safe to read the manifest, and only for this tag.
  $manifest = Get-Content -Raw -LiteralPath $manifestPath | ConvertFrom-Json
  if ($manifest -isnot [PSCustomObject] -or $manifest.schema_version -isnot [long] -or $manifest.schema_version -ne 1) {
    Stop-Install "unsupported release manifest schema version '$($manifest.schema_version)'"
  }
  if ($manifest.repository -isnot [string] -or $manifest.repository -cne $Repo) {
    Stop-Install "signed manifest names repository '$($manifest.repository)', not '$Repo'"
  }
  if ($manifest.tag -isnot [string] -or $manifest.tag -cne "v$Tag") {
    Stop-Install (("signed manifest is bound to tag '{0}', not the requested tag 'v{1}'`n" +
      "       refusing a replayed release manifest") -f $manifest.tag, $Tag)
  }
  if ($manifest.source_commit -isnot [string] -or $manifest.source_commit -cnotmatch '^[0-9a-f]{40}([0-9a-f]{24})?$') {
    Stop-Install "signed manifest does not record a valid source commit"
  }

  # 4. Bind the provenance to the exact archive bytes on disk.
  Invoke-CosignVerifyAttestation -Cosign $Cosign -Bundle $provenanceBundle -Artifact $Archive
  Write-Host "=> Provenance OK ($Asset)"

  Assert-AuthenticatedMetadata -Manifest $manifest -ManifestPath $manifestPath -ChecksumsPath $checksumsPath -ProvenanceBundle $provenanceBundle -Tag $Tag -SourceCommit $manifest.source_commit

  # 5. Cross-bind the three authenticated views of the archive digest.
  $expected = Get-ManifestDigest -Manifest $manifest -Name $Asset
  if ($expected -isnot [string] -or $expected -cnotmatch '^[0-9a-f]{64}$') {
    Stop-Install "signed manifest does not list $Asset"
  }
  $recorded = Get-ChecksumEntry -SumsPath $checksumsPath -Asset $Asset
  if ($recorded -cne $expected) {
    Stop-Install (("signed checksums.txt digest for {0} disagrees with the signed manifest`n" +
      "       checksums.txt: {1}`n       manifest:      {2}") -f $Asset, $recorded, $expected)
  }
  $actual = (Get-FileHash -Algorithm SHA256 -Path $Archive).Hash.ToLowerInvariant()
  if ($actual -cne $expected) {
    Stop-Install (("{0} digest mismatch against the authenticated release metadata`n" +
      "       expected: {1}`n       actual:   {2}`n" +
      "       refusing to install a tampered or corrupt archive") -f $Asset, $expected, $actual)
  }
  Write-Host "=> Digest OK ($actual)"
  Write-Host "=> Verified okt v$Tag — source commit $($manifest.source_commit)"
}

function Add-ToPath {
  param([string]$Dir)
  # User-scope environment variables only exist on Windows. On PowerShell 5.1
  # $IsWindows is undefined, which is also Windows.
  if ($null -ne $IsWindows -and -not $IsWindows) {
    Write-Host "=> Add $Dir to your PATH to use 'okt'."
    return
  }
  $current = [Environment]::GetEnvironmentVariable("Path", "User")
  if ($current -notlike "*$Dir*") {
    [Environment]::SetEnvironmentVariable("Path", "$current;$Dir", "User")
    Write-Host "=> Added $Dir to your user PATH. Restart your terminal to use 'okt'."
  }
}

if ($env:OKT_INSTALLER_TEST_LIBRARY -eq "1") { return }

$tag = if ($env:VERSION) { $env:VERSION } else { Get-LatestTag }

# Both strict preconditions run before a single byte is downloaded, so a host
# with no verifier — or a target with no signed metadata — never fetches a
# release at all.
$cosign = $null
if ($VerifyMode -eq "strict") {
  $cosign = Resolve-Cosign
  Assert-SignedRelease -Tag $tag
} else {
  Write-ConvenienceTrustNotice
}

$arch = Get-Arch
$asset = "okt_Windows_${arch}.zip"
$url = "$DlBase/$Repo/releases/download/v$tag/$asset"
$tmpdir = Join-Path ([System.IO.Path]::GetTempPath()) ([System.Guid]::NewGuid().ToString())

Write-Host "=> Installing okt v$tag for Windows $arch..."
Write-Host "=> Downloading $url"

New-Item -ItemType Directory -Path $tmpdir -Force | Out-Null
$archivePath = Join-Path $tmpdir $asset
Invoke-WebRequest -Uri $url -OutFile $archivePath

# Verify BEFORE extracting/executing: any failure throws ($ErrorActionPreference
# = "Stop") which exits non-zero here, before any okt.exe reaches InstallDir or
# PATH.
if ($VerifyMode -eq "strict") {
  Invoke-StrictVerification -Cosign $cosign -Archive $archivePath -Asset $asset -Tag $tag -TmpDir $tmpdir
} else {
  Verify-Checksum -Archive $archivePath -Asset $asset -Tag $tag -TmpDir $tmpdir
}

Expand-Archive -Path $archivePath -DestinationPath $tmpdir -Force

New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
$installedExe = Join-Path $InstallDir "okt.exe"
Copy-Item -Path (Join-Path $tmpdir "okt.exe") -Destination $installedExe -Force
Remove-Item -Recurse -Force $tmpdir

Add-ToPath -Dir $InstallDir

$version = & $installedExe --version
Write-Host "=> Installed $version"

& $installedExe setup
if ($LASTEXITCODE -ne 0) {
  Stop-Install "okt setup failed with exit code $LASTEXITCODE"
}

Write-Host "=> Run 'okt --help' to get started"

[CmdletBinding(SupportsShouldProcess = $true)]
param(
    [Parameter(Mandatory = $true)]
    [ValidatePattern('^\d+\.\d+\.\d+$')]
    [string]$Version,

    [switch]$IncludeAarch64,

    [switch]$Apply
)

$ErrorActionPreference = 'Stop'
$RepositoryRoot = (Resolve-Path (Join-Path $PSScriptRoot '..\..')).Path
$CurrentLock = Join-Path $RepositoryRoot 'platform\alpine\base.lock'
$CandidateDirectory = Join-Path $RepositoryRoot 'build\alpine'
$Parts = $Version.Split('.')
$Branch = 'v' + $Parts[0] + '.' + $Parts[1]
$AportsTag = 'v' + $Version
$AportsRepository = 'https://gitlab.alpinelinux.org/alpine/aports.git'
$TagRefName = 'refs/tags/' + $AportsTag
$PeeledRefName = $TagRefName + '^{}'
$AportsRefs = @(& git ls-remote --tags $AportsRepository $TagRefName $PeeledRefName)
if ($LASTEXITCODE -ne 0 -or $AportsRefs.Count -eq 0) {
    throw ('No official Alpine aports tag exists for ' + $AportsTag)
}

$TagObjectRecord = $AportsRefs | Where-Object { ($_ -split '\s+')[1] -eq $TagRefName } | Select-Object -First 1
if (-not $TagObjectRecord) {
    throw ('No exact official Alpine aports tag ref exists for ' + $AportsTag)
}
$PeeledRecord = $AportsRefs | Where-Object { ($_ -split '\s+')[1] -eq $PeeledRefName } | Select-Object -First 1
$AportsTagObject = (($TagObjectRecord -split '\s+')[0]).ToLowerInvariant()
$AportsCommit = if ($PeeledRecord) {
    (($PeeledRecord -split '\s+')[0]).ToLowerInvariant()
} else {
    $AportsTagObject
}
if ($AportsTagObject -notmatch '^[0-9a-f]{40}$' -or $AportsCommit -notmatch '^[0-9a-f]{40}$') {
    throw 'The resolved aports tag or commit is not a full Git object ID.'
}

$Inputs = @(
    [pscustomobject]@{ Key = 'X86'; Architecture = 'x86'; Flavor = 'extended' },
    [pscustomobject]@{ Key = 'X86_64'; Architecture = 'x86_64'; Flavor = 'extended' }
)
if ($IncludeAarch64) {
    $Inputs += [pscustomobject]@{ Key = 'AARCH64'; Architecture = 'aarch64'; Flavor = 'standard' }
}

$Lines = @(
    '# HHSoft PC Tester Alpine base lock.',
    '# Update only through scripts/build/Update-AlpineBase.ps1 and review the diff.',
    'SCHEMA=1',
    'UPSTREAM_GPG_FINGERPRINT=0482D84022F52DF1C4E7CD43293ACD0907D9495A',
    ('VERSION=' + $Version),
    ('BRANCH=' + $Branch),
    ('APORTS_TAG=' + $AportsTag),
    ('APORTS_TAG_OBJECT=' + $AportsTagObject),
    ('APORTS_COMMIT=' + $AportsCommit)
)

foreach ($InputRecord in $Inputs) {
    $Image = 'alpine-' + $InputRecord.Flavor + '-' + $Version + '-' + $InputRecord.Architecture + '.iso'
    $Uri = 'https://dl-cdn.alpinelinux.org/alpine/' + $Branch + '/releases/' + $InputRecord.Architecture + '/' + $Image + '.sha256'
    $Response = Invoke-RestMethod -UseBasicParsing -Uri $Uri
    $Checksum = (($Response.ToString() -split '\s+')[0]).ToLowerInvariant()
    if ($Checksum -notmatch '^[0-9a-f]{64}$') {
        throw ('Invalid SHA-256 response for ' + $Image)
    }

    $Lines += $InputRecord.Key + '_IMAGE=' + $Image
    $Lines += $InputRecord.Key + '_SHA256=' + $Checksum
}

New-Item -ItemType Directory -Force -Path $CandidateDirectory | Out-Null
$CandidatePath = Join-Path $CandidateDirectory ('base.' + $Version + '.candidate.lock')
Set-Content -LiteralPath $CandidatePath -Value $Lines -Encoding ASCII
Write-Output ('Candidate: ' + $CandidatePath)

if ($Apply -and $PSCmdlet.ShouldProcess($CurrentLock, 'Replace Alpine base lock')) {
    Set-Content -LiteralPath $CurrentLock -Value $Lines -Encoding ASCII
    Write-Output ('Applied: ' + $CurrentLock)
}

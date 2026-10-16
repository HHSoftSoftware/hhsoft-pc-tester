[CmdletBinding()]
param(
    [string]$Path = (Join-Path $PSScriptRoot '..\..\platform\alpine\base.lock')
)

$ErrorActionPreference = 'Stop'
$Lock = @{}
foreach ($Line in Get-Content -LiteralPath $Path) {
    if (-not $Line -or $Line.StartsWith('#')) {
        continue
    }

    $Pair = $Line.Split('=', 2)
    if ($Pair.Count -ne 2 -or -not $Pair[0] -or -not $Pair[1]) {
        throw ('Malformed base-lock line: ' + $Line)
    }
    if ($Lock.ContainsKey($Pair[0])) {
        throw ('Duplicate base-lock key: ' + $Pair[0])
    }
    $Lock[$Pair[0]] = $Pair[1]
}

$Required = @('SCHEMA', 'UPSTREAM_GPG_FINGERPRINT', 'VERSION', 'BRANCH', 'APORTS_TAG', 'APORTS_TAG_OBJECT', 'APORTS_COMMIT', 'AARCH64_IMAGE', 'AARCH64_SHA256', 'X86_IMAGE', 'X86_SHA256', 'X86_64_IMAGE', 'X86_64_SHA256')
foreach ($Key in $Required) {
    if (-not $Lock.ContainsKey($Key)) {
        throw ('Missing base-lock key: ' + $Key)
    }
}

if ($Lock.SCHEMA -ne '1' -or $Lock.VERSION -notmatch '^\d+\.\d+\.\d+$') {
    throw 'Unsupported schema or invalid Alpine version.'
}
$VersionParts = $Lock.VERSION.Split('.')
$ExpectedBranch = 'v' + $VersionParts[0] + '.' + $VersionParts[1]
if ($Lock.BRANCH -ne $ExpectedBranch -or $Lock.APORTS_TAG -ne ('v' + $Lock.VERSION)) {
    throw 'Alpine branch, tag, and version do not agree.'
}
if ($Lock.APORTS_TAG_OBJECT -notmatch '^[0-9a-f]{40}$' -or $Lock.APORTS_COMMIT -notmatch '^[0-9a-f]{40}$') {
    throw 'Aports objects must be full lowercase revisions.'
}
if ($Lock.UPSTREAM_GPG_FINGERPRINT -notmatch '^[0-9A-F]{40}$') {
    throw 'UPSTREAM_GPG_FINGERPRINT must be a full uppercase fingerprint.'
}
foreach ($Architecture in @('AARCH64', 'X86', 'X86_64')) {
    if ($Lock[$Architecture + '_SHA256'] -notmatch '^[0-9a-f]{64}$') {
        throw ('Invalid SHA-256 for ' + $Architecture)
    }
    if ($Lock[$Architecture + '_IMAGE'] -notlike ('*-' + $Lock.VERSION + '-*.iso')) {
        throw ('Image filename does not contain the locked version for ' + $Architecture)
    }
}

[pscustomobject]@{
    Version = $Lock.VERSION
    Branch = $Lock.BRANCH
    AportsCommit = $Lock.APORTS_COMMIT
    Architectures = ($Lock.Keys | Where-Object { $_ -like '*_IMAGE' }).Count
}

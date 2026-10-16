[CmdletBinding()]
param(
    [string]$Path = (Join-Path $PSScriptRoot '..\..\platform\buildroot\base.lock')
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

$Required = @(
    'SCHEMA', 'VERSION', 'SOURCE', 'SHA256', 'SIGNED_MANIFEST',
    'UPSTREAM_GPG_FINGERPRINT', 'LINUX_VERSION', 'LINUX_SOURCE', 'LINUX_SHA256',
    'LINUX_SIGNATURE', 'LINUX_SIGNER_FINGERPRINT'
)
foreach ($Key in $Required) {
    if (-not $Lock.ContainsKey($Key)) {
        throw ('Missing base-lock key: ' + $Key)
    }
}

if ($Lock.SCHEMA -ne '1' -or $Lock.VERSION -notmatch '^\d{4}\.\d{2}\.\d+$') {
    throw 'Unsupported schema or invalid Buildroot LTS version.'
}
if ($Lock.SOURCE -ne ('buildroot-' + $Lock.VERSION + '.tar.xz')) {
    throw 'Buildroot source filename and version do not agree.'
}
if ($Lock.SIGNED_MANIFEST -ne ($Lock.SOURCE + '.sign')) {
    throw 'Buildroot signed-manifest filename does not agree.'
}
if ($Lock.SHA256 -notmatch '^[0-9a-f]{64}$') {
    throw 'Invalid Buildroot SHA-256.'
}
if ($Lock.UPSTREAM_GPG_FINGERPRINT -notmatch '^[0-9A-F]{40}$') {
    throw 'UPSTREAM_GPG_FINGERPRINT must be a full uppercase fingerprint.'
}
if ($Lock.LINUX_VERSION -notmatch '^\d+\.\d+\.\d+$' -or
    $Lock.LINUX_SOURCE -ne ('linux-' + $Lock.LINUX_VERSION + '.tar.xz') -or
    $Lock.LINUX_SHA256 -notmatch '^[0-9a-f]{64}$' -or
    $Lock.LINUX_SIGNATURE -ne ('linux-' + $Lock.LINUX_VERSION + '.tar.sign') -or
    $Lock.LINUX_SIGNER_FINGERPRINT -notmatch '^[0-9A-F]{40}$') {
    throw 'Invalid locked Linux release.'
}

$PlatformRoot = Split-Path -Parent (Split-Path -Parent (Resolve-Path -LiteralPath $Path))
$Defconfig = Join-Path $PlatformRoot 'buildroot\configs\hhpt_legacy_x86_defconfig'
$ExpectedVersion = 'BR2_LINUX_KERNEL_CUSTOM_VERSION_VALUE="' + $Lock.LINUX_VERSION + '"'
if (-not (Select-String -LiteralPath $Defconfig -SimpleMatch $ExpectedVersion -Quiet)) {
    throw 'Buildroot defconfig kernel version differs from base.lock.'
}
$ExpectedHash = 'sha256  ' + $Lock.LINUX_SHA256 + '  ' + $Lock.LINUX_SOURCE
foreach ($HashPath in @(
    (Join-Path $PlatformRoot 'buildroot\patches\linux\linux.hash'),
    (Join-Path $PlatformRoot 'buildroot\patches\linux-headers\linux-headers.hash')
)) {
    if (-not (Select-String -LiteralPath $HashPath -SimpleMatch $ExpectedHash -Quiet)) {
        throw ('Kernel source hash differs from base.lock: ' + $HashPath)
    }
}

[pscustomobject]@{
    Version = $Lock.VERSION
    Source = $Lock.SOURCE
    LinuxVersion = $Lock.LINUX_VERSION
    LinuxSigningFingerprint = $Lock.LINUX_SIGNER_FINGERPRINT
    SigningFingerprint = $Lock.UPSTREAM_GPG_FINGERPRINT
}

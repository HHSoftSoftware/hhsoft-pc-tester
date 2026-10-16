[CmdletBinding(SupportsShouldProcess = $true)]
param(
    [Parameter(Mandatory = $true)]
    [ValidateNotNullOrEmpty()]
    [string]$VmName,

    [Parameter(Mandatory = $true)]
    [ValidateNotNullOrEmpty()]
    [string]$VmDirectory,

    [Parameter(Mandatory = $true)]
    [ValidateNotNullOrEmpty()]
    [string]$IsoPath,

    [Parameter(Mandatory = $true)]
    [ValidateNotNullOrEmpty()]
    [string]$VirtualDiskPath,

    [ValidateSet('BIOS', 'EFI')]
    [string]$Firmware = 'EFI',

    [ValidateRange(1, 64)]
    [int]$CpuCount = 2,

    [ValidateRange(512, 262144)]
    [int]$MemoryMiB = 4096,

    [ValidateRange(16, 256)]
    [int]$VideoMemoryMiB = 64,

    [switch]$StartHeadless,

    [string]$VBoxManagePath
)

Set-StrictMode -Version 3.0
$ErrorActionPreference = 'Stop'

function Resolve-RegularFile {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Path,

        [Parameter(Mandatory = $true)]
        [string]$Description,

        [Parameter(Mandatory = $true)]
        [string]$Extension
    )

    if ($Path -match '^(?i:\\\\[.?]\\|/dev/|host:)') {
        throw "$Description must be a regular file, not a physical-device or host-media path: $Path"
    }

    $item = Get-Item -LiteralPath $Path -Force -ErrorAction Stop
    if ($item.PSProvider.Name -ne 'FileSystem' -or $item.PSIsContainer) {
        throw "$Description must be a regular filesystem file: $Path"
    }

    if (($item.Attributes -band [System.IO.FileAttributes]::ReparsePoint) -ne 0) {
        throw "$Description must not be a symbolic link or reparse-point leaf: $Path"
    }

    if (-not [string]::Equals($item.Extension, $Extension, [System.StringComparison]::OrdinalIgnoreCase)) {
        throw "$Description must use the $Extension extension: $($item.FullName)"
    }

    if ($item.Length -eq 0) {
        throw "$Description must not be empty: $($item.FullName)"
    }

    return $item
}

function Invoke-VBoxManage {
    param(
        [Parameter(Mandatory = $true)]
        [string[]]$ArgumentList
    )

    $output = @(& $script:VBoxManageExecutable @ArgumentList 2>&1)
    $exitCode = $LASTEXITCODE
    $outputLines = @($output | ForEach-Object { [string]$_ })

    if ($exitCode -ne 0) {
        throw "VBoxManage $($ArgumentList[0]) failed with exit code $exitCode.$([Environment]::NewLine)$($outputLines -join [Environment]::NewLine)"
    }

    return $outputLines
}

if ($VmName -ne $VmName.Trim() -or $VmName -in @('.', '..') -or $VmName.Contains('"') -or
    $VmName.IndexOfAny([System.IO.Path]::GetInvalidFileNameChars()) -ge 0) {
    throw 'VmName must be a non-empty, unquoted filename-safe name without leading or trailing whitespace.'
}

if ($VmDirectory -match '^(?i:\\\\[.?]\\|/dev/|host:)') {
    throw "VmDirectory must be a regular filesystem directory: $VmDirectory"
}

if ($VBoxManagePath) {
    $VBoxManageItem = Get-Item -LiteralPath $VBoxManagePath -Force -ErrorAction Stop
    if ($VBoxManageItem.PSProvider.Name -ne 'FileSystem' -or $VBoxManageItem.PSIsContainer) {
        throw "VBoxManagePath must point to the VBoxManage executable: $VBoxManagePath"
    }
    $script:VBoxManageExecutable = $VBoxManageItem.FullName
}
else {
    $VBoxManageCommand = Get-Command VBoxManage.exe -ErrorAction SilentlyContinue
    if (-not $VBoxManageCommand) {
        $VBoxManageCommand = Get-Command VBoxManage -ErrorAction SilentlyContinue
    }

    if ($VBoxManageCommand) {
        $script:VBoxManageExecutable = $VBoxManageCommand.Source
    }
    else {
        $defaultVBoxManagePath = Join-Path $env:ProgramFiles 'Oracle\VirtualBox\VBoxManage.exe'
        if (-not (Test-Path -LiteralPath $defaultVBoxManagePath -PathType Leaf)) {
            throw 'VBoxManage was not found. Install VirtualBox or pass -VBoxManagePath.'
        }
        $script:VBoxManageExecutable = $defaultVBoxManagePath
    }
}

$versionOutput = (Invoke-VBoxManage -ArgumentList @('--version')) -join [Environment]::NewLine
$versionMatch = [System.Text.RegularExpressions.Regex]::Match($versionOutput, '^(?<major>\d+)\.(?<minor>\d+)')
if (-not $versionMatch.Success -or [int]$versionMatch.Groups['major'].Value -lt 7 -or
    ([int]$versionMatch.Groups['major'].Value -eq 7 -and [int]$versionMatch.Groups['minor'].Value -lt 2)) {
    throw "VirtualBox 7.2 or newer is required. Found: $versionOutput"
}

$isoItem = Resolve-RegularFile -Path $IsoPath -Description 'IsoPath' -Extension '.iso'
$virtualDiskItem = Resolve-RegularFile -Path $VirtualDiskPath -Description 'VirtualDiskPath' -Extension '.vdi'
if ([string]::Equals($isoItem.FullName, $virtualDiskItem.FullName, [System.StringComparison]::OrdinalIgnoreCase)) {
    throw 'IsoPath and VirtualDiskPath must identify different files.'
}

$mediumInfo = (Invoke-VBoxManage -ArgumentList @('showmediuminfo', 'disk', $virtualDiskItem.FullName)) -join [Environment]::NewLine
if ($mediumInfo -notmatch '(?mi)^Storage format:\s*VDI\s*$') {
    throw 'VirtualDiskPath must be a VirtualBox VDI, not a raw-disk descriptor or another virtual-disk format.'
}
if ($mediumInfo -notmatch '(?mi)^(?:Format variant|Variant):\s*.*\bdynamic\b') {
    throw 'VirtualDiskPath must be dynamically allocated (sparse), not fixed or raw.'
}

$inUseMatch = [System.Text.RegularExpressions.Regex]::Match($mediumInfo, '(?mi)^In use by VMs:\s*(?<value>[^\r\n]*)')
if ($inUseMatch.Success) {
    $inUseValue = $inUseMatch.Groups['value'].Value.Trim()
    if ($inUseValue -and $inUseValue -notmatch '^(?i:<none>|none|not attached to any VM)$') {
        throw "VirtualDiskPath is already attached to another VM: $inUseValue"
    }
}

$registeredVms = (Invoke-VBoxManage -ArgumentList @('list', 'vms')) -join [Environment]::NewLine
$registeredVmPattern = '(?m)^"' + [System.Text.RegularExpressions.Regex]::Escape($VmName) + '"\s+\{[0-9A-Fa-f-]+\}\s*$'
if ($registeredVms -match $registeredVmPattern) {
    throw "A registered VirtualBox VM named '$VmName' already exists; no changes were made."
}

$resolvedVmDirectory = [System.IO.Path]::GetFullPath($VmDirectory)
if (Test-Path -LiteralPath $resolvedVmDirectory) {
    $vmDirectoryItem = Get-Item -LiteralPath $resolvedVmDirectory -Force
    if (-not $vmDirectoryItem.PSIsContainer -or $vmDirectoryItem.PSProvider.Name -ne 'FileSystem') {
        throw "VmDirectory must be a filesystem directory: $resolvedVmDirectory"
    }
}

$machineDirectory = Join-Path $resolvedVmDirectory $VmName
if (Test-Path -LiteralPath $machineDirectory) {
    throw "Target VM directory already exists; no changes were made: $machineDirectory"
}

$serialLogPath = Join-Path $machineDirectory 'serial.log'
$firmwareMode = $Firmware.ToLowerInvariant()
$action = "create and register isolated $Firmware VirtualBox VM in '$machineDirectory'"
if (-not $PSCmdlet.ShouldProcess($VmName, $action)) {
    return
}

if (-not (Test-Path -LiteralPath $resolvedVmDirectory)) {
    $null = New-Item -ItemType Directory -Path $resolvedVmDirectory
}

$null = Invoke-VBoxManage -ArgumentList @(
    'createvm', '--name', $VmName, '--basefolder', $resolvedVmDirectory,
    '--platform-architecture', 'x86', '--register'
)
$null = Invoke-VBoxManage -ArgumentList @(
    'modifyvm', $VmName,
    '--firmware', $firmwareMode,
    '--cpus', "$CpuCount",
    '--memory', "$MemoryMiB",
    '--vram', "$VideoMemoryMiB",
    '--ioapic', 'on',
    '--graphicscontroller', 'vmsvga',
    '--accelerate-3d', 'off',
    '--boot1', 'dvd', '--boot2', 'disk', '--boot3', 'none', '--boot4', 'none',
    '--nic1', 'none',
    '--clipboard-mode', 'disabled',
    '--clipboard-file-transfers', 'disabled',
    '--drag-and-drop', 'disabled',
    '--usb-ohci', 'off', '--usb-ehci', 'off', '--usb-xhci', 'off',
    '--audio-enabled', 'off',
    '--nested-hw-virt', 'off',
    '--vrde', 'off',
    '--recording', 'off',
    '--uart1', '0x3F8', '4',
    '--uart-mode1', 'file', $serialLogPath
)
$null = Invoke-VBoxManage -ArgumentList @(
    'storagectl', $VmName, '--name', 'HHPT SATA', '--add', 'sata',
    '--controller', 'IntelAhci', '--portcount', '2', '--bootable', 'on'
)
$null = Invoke-VBoxManage -ArgumentList @(
    'storageattach', $VmName, '--storagectl', 'HHPT SATA', '--port', '0',
    '--device', '0', '--type', 'hdd', '--medium', $virtualDiskItem.FullName,
    '--mtype', 'normal', '--nonrotational', 'on'
)
$null = Invoke-VBoxManage -ArgumentList @(
    'storageattach', $VmName, '--storagectl', 'HHPT SATA', '--port', '1',
    '--device', '0', '--type', 'dvddrive', '--medium', $isoItem.FullName,
    '--passthrough', 'off'
)

if ($StartHeadless) {
    $null = Invoke-VBoxManage -ArgumentList @('startvm', $VmName, '--type', 'headless')
}

[pscustomobject]@{
    VmName              = $VmName
    MachineDirectory    = $machineDirectory
    Firmware            = $Firmware
    IsoPath             = $isoItem.FullName
    VirtualDiskPath     = $virtualDiskItem.FullName
    SerialLogPath       = $serialLogPath
    StartedHeadless     = [bool]$StartHeadless
}

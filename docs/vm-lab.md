# VirtualBox VM Lab

This runbook creates an isolated VirtualBox VM for HHSoft PC Tester image
development. The VM has no network adapter, shared folders, clipboard, drag and
drop, USB controller, physical-device passthrough, or remote display. Its only
attached storage is one caller-supplied ISO and one caller-supplied dynamic VDI.

The provisioning script does not start the VM unless `-StartHeadless` is
explicitly passed. It never partitions, formats, opens, or attaches a physical
disk or Windows physical-device path.

## Prerequisites

- Oracle VirtualBox 7.2 or newer with `VBoxManage` installed.
- An HHSoft PC Tester ISO.
- A writable lab filesystem with enough free space for the VM and virtual disk.
- Windows PowerShell 5.1 or PowerShell 7.

If the scratch SSD is assigned a drive letter, use a directory on that mounted
filesystem. Choose a lab directory on that filesystem for the example below.
Never pass a physical-device path or raw VMDK to this workflow.

## Create Sparse Media

Create a new, dynamically allocated VDI in a media directory. `--variant
Standard` is VirtualBox's dynamically allocated format; do not use `Fixed` or
`RawDisk`.

```powershell
$VBoxManage = 'C:\Program Files\Oracle\VirtualBox\VBoxManage.exe'
$LabRoot = 'D:\hhpt-lab' # Example: replace with your mounted lab directory.
$VirtualDiskPath = Join-Path $LabRoot 'media\hhpt-test.vdi'

New-Item -ItemType Directory -Force -Path (Split-Path $VirtualDiskPath)
& $VBoxManage createmedium disk --filename $VirtualDiskPath --size 65536 --format VDI --variant Standard
```

The script intentionally does not create or convert media. It accepts only an
existing, unattached, dynamic `.vdi` file and checks VirtualBox's medium metadata
before changing VM state. The ISO and VDI must be regular files rather than
symbolic links or reparse-point leaves.

## Create the VM

VirtualBox places the VM in `<VmDirectory>\<VmName>`. Keep virtual media in a
sibling directory so a leftover VM directory can always trigger the refusal
guard.

```powershell
$VmName = 'hhpt-bios'
$VmDirectory = Join-Path $LabRoot 'vms'
$IsoPath = 'C:\path\to\hhsoft-pc-tester.iso'

.\scripts\lab\New-HhptVirtualBoxVm.ps1 `
  -VmName $VmName `
  -VmDirectory $VmDirectory `
  -IsoPath $IsoPath `
  -VirtualDiskPath $VirtualDiskPath `
  -Firmware BIOS
```

Use `-Firmware EFI` for UEFI coverage. Optional `-CpuCount`, `-MemoryMiB`, and
`-VideoMemoryMiB` values control VM capacity. Reusing a registered name or an
existing target VM directory stops before any VM changes.

## Serial Console

COM1 is connected to `<VmDirectory>\<VmName>\serial.log` at I/O address `0x3f8`
and IRQ 4. Configure the guest kernel with `console=ttyS0,115200n8` to capture
boot and test logs. The ignored `*.log` file remains local.

## Start and Capture

Start headless during provisioning only when required:

```powershell
.\scripts\lab\New-HhptVirtualBoxVm.ps1 `
  -VmName $VmName `
  -VmDirectory $VmDirectory `
  -IsoPath $IsoPath `
  -VirtualDiskPath $VirtualDiskPath `
  -Firmware EFI `
  -StartHeadless
```

For an already-created VM, start it and take screenshots with separate
VirtualBox commands. `screenshotpng` requires a running VM.

```powershell
$ScreenshotDirectory = Join-Path (Join-Path $VmDirectory $VmName) 'screenshots'
New-Item -ItemType Directory -Force -Path $ScreenshotDirectory

& $VBoxManage startvm $VmName --type headless
& $VBoxManage controlvm $VmName screenshotpng (Join-Path $ScreenshotDirectory 'boot.png') 0
```

Request an orderly shutdown when the guest supports ACPI. Reserve `poweroff`
for a hung disposable test VM because it is equivalent to removing power.

```powershell
& $VBoxManage controlvm $VmName acpipowerbutton
# Emergency only: & $VBoxManage controlvm $VmName poweroff
```

## Safety Boundaries

- The VDI extension and VirtualBox-reported `VDI` dynamic variant are required.
- Device paths, raw-disk descriptors, fixed disks, attached disks, and linked
  media are refused.
- The default VM is offline and cannot access the host clipboard, filesystem,
  USB devices, webcam, or network.
- Add connectivity or host-device access only in a separate, reviewed workflow.
- Destructive guest tests must target only the disposable VDI, never the ISO,
  host boot medium, report destination, or a physical disk.

## Report-Destination Validation

Use a dedicated file-backed raw disk attached through the VM's emulated USB
mass-storage controller; do not enable host USB passthrough. Give the emulated
device a fixed test serial, partition and format it before boot, and attach a
separate read-only ISO containing only a signed test report plus non-production
test keys. The smoke must prove discovery, incorrect-code and modified-HMAC
denial, required mount-option enforcement, signed-source verification,
exclusive creation, final identity revalidation, and absence of rejected
files. Unmount before stopping the VM, then discard the fixture media.

## Builder Separation

Keep boot-test VMs offline. When a guest needs packages or build sources,
create or repurpose a separately named builder and use VirtualBox NAT only.
Bind any SSH forward to `127.0.0.1`, provision an ephemeral key with
`scripts/lab/Send-VBoxText.ps1`, disable passwords, and keep the key under an
ignored build directory. Never use a network-enabled builder result as an
offline-runtime acceptance test.

The builder itself may remain diskless, but image work must not use its small
RAM-backed root filesystem. Mount an already verified, disposable file-backed
VDI partition by its dedicated `HHPT_VM_SCRATCH` label with `nodev,nosuid` and
place aports, Buildroot downloads, work trees, and candidate artifacts there.
Formatting requires a separate first-time action after VirtualBox confirms the
attachment is a dynamic VDI; recovery must never format an inferred device.
Keep production release/signing keys off that VDI.

A diskless reset removes installed tools, temporary developer keys, and `/tmp`.
Pinned source and download caches on the VDI survive, allowing an incremental
rebuild after the toolchain is reinstalled and its network is explicitly
enabled with `scripts/lab/prepare-alpine-builder.sh`. Refresh the SSH host-key
pin only after comparing the new key to the builder console through a separate
channel.

The initial builder proved key-only automation from the official Alpine
bootstrap. That ISO matched its official SHA-256 and detached GPG signature,
and both BIOS and native UEFI bootstrap VMs reached a login prompt. Secure
Boot was not enabled in this proof and is covered by `docs/secure-boot.md`.

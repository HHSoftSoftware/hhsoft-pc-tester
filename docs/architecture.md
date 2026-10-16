# Architecture

Status: accepted direction through Phase 1. Revisit after the first
real-hardware pilot, not during every implementation choice.

## Product boundaries

HHSoft PC Tester is a diagnostics appliance, not a repair toolkit, hardware
database service, or unattended destructive burn-in image. It answers four
questions for one current hardware configuration:

1. What components and externally testable ports are present?
2. Did every applicable quick, functional, and health check pass?
3. What human-observed checks remain incomplete?
4. Is this the same component set seen in an earlier trusted report?

The appliance must work offline. Internet access, package updates, driver
downloads, benchmark downloads, and report upload are never part of boot.

## Platform decision

Use a pinned Alpine Linux stable branch in diskless mode for the first
production architecture.

- Alpine provides x86, x86_64, and AArch64 builds and a small, auditable base.
- Diskless mode loads a read-only image into RAM and fits the no-host-changes
  default better than a normal installation.
- `apk`, OpenRC, and Alpine image profiles keep image customization explicit.
- Alpine community packages have a shorter support window than main packages,
  so releases pin every package and rebuild frequently instead of updating an
  appliance in place.

Buildroot is the bounded legacy base only. It offers the precise pre-SSE2 CPU
baseline that Alpine no longer provides, at the cost of project-owned kernel,
driver, package, and update lifecycle. It remains minimal rather than
duplicating every Alpine functional test.
Debian Live and current SystemRescue images simplify some desktop dependencies,
but their primary live-image path is 64-bit x86 and therefore does not meet the
required 32-bit x86 baseline.

Prototype against Alpine `v3.24`. `platform/alpine/base.lock` is the only
source for the Alpine patch release and exact aports revision. An explicit
base-update script creates a candidate lock for review; no build reads a
floating latest-stable reference. A branch upgrade is a reviewed pull request
that refreshes package locks and passes the full VM and hardware matrix.

Primary references:

- https://www.alpinelinux.org/releases/
- https://wiki.alpinelinux.org/wiki/Architecture
- https://wiki.alpinelinux.org/wiki/Diskless_Mode
- https://wiki.alpinelinux.org/wiki/How_to_make_a_custom_ISO_image_with_mkimage
- https://buildroot.org/downloads/manual/manual.html

## Boot artifacts

Ship distinct artifacts rather than one hard-to-debug universal image:

| Artifact | Firmware | CPU baseline | Stage |
| --- | --- | --- | --- |
| `hhpt-x86_64.iso` | Legacy BIOS and x64 UEFI | x86-64 | Required |
| `hhpt-x86.iso` | Legacy BIOS and IA32 UEFI where supported | i686, CMOV, SSE2 | Required |
| `hhpt-legacy-x86.iso` | Legacy BIOS or CSM | Pentium MMX, no SSE2 | Required fallback |
| `hhpt-aarch64.iso` | Standards-compliant AArch64 UEFI | ARMv8+ | Beta |
| PXE bundle | BIOS/UEFI matching its build | Same as image | Later |

Use Syslinux or Isolinux for legacy BIOS and GRUB EFI binaries for UEFI. Include
an open-source Memtest86+ entry separately from the Linux environment. Keep a
safe graphics fallback and a serial-console recovery entry.

The AArch64 artifact is a beta modern-media target. It uses generic AArch64
UEFI rather than legacy ARM board-specific boot flows; build and QEMU boot
validation are still required, followed by device-specific firmware and driver
qualification on representative ARM PCs. Its beta label must remain visible
until that work is complete.

Legacy BIOS has no Secure Boot. It may use CSM on a UEFI machine. The normal
UEFI path does not need CSM, but current and first production images require
Secure Boot off. Secure Boot is an optional, deferred enhancement rather than
a gate for functional hardware testing. Its trust contracts remain recorded
so a later implementation does not weaken the rest of the appliance.

Signing the EFI loader is not the whole security boundary. The enrolled-key
path binds kernel, initramfs, command line, and appliance trust roots into the
signed UKI, prohibits unsigned modules, and preserves the runtime, report, and
storage policy controls after boot. See `docs/secure-boot.md`.

The 32-bit Alpine baseline excludes pre-SSE2 PCs. A separate Buildroot i586
artifact therefore boots from BIOS/CSM on Pentium MMX-class and newer systems,
including older x64 systems. It runs the same `GO386=softfloat` inventory and
report core, BusyBox/mdev device discovery, and supported low-cost read-only
checks. Its report states reduced driver/test coverage. Pre-MMX x86 is outside
the supported boundary.

## Runtime components

- `hhptd`: root-owned Go orchestrator and local API. It owns discovery,
  policy, scheduling, evidence, fingerprinting, and final report state.
- `hhpt-ui`: unprivileged local operator UI. It cannot open raw storage or run
  commands. The first UI may be a keyboard-driven TUI plus dedicated graphical
  test screens; browser-kiosk mode is optional on architectures that support it.
- test adapters: reviewed Go adapters around sysfs and pinned programs such as
  smartmontools, nvme-cli, stress-ng, fio, Mesa utilities, libinput, ALSA,
  V4L2 or libcamera, and NetworkManager or iwd.
- report verifier: a host-side cross-platform utility and static report viewer
  that verify manifests without trusting active content in a report.

Do not adopt Phoronix Test Suite as the privileged core. It is useful as an
optional advanced benchmark adapter, but runtime profile downloads and general
shell profile execution conflict with the fixed, reviewed image boundary.
Likewise, `hw-probe` may inform inventory compatibility but its upload workflow
is opt-in and is not HHSoft PC Tester identity or audit state.

## Graphics driver strategy

Modern images use package-locked Alpine kernel DRM/KMS drivers, Linux firmware,
and Mesa Gallium/EGL/GBM from the same pinned distro release. This supports a
single review/update path and open drivers such as amdgpu, i915, and nouveau
where the kernel, device firmware, and Mesa version support the hardware. The
core image does not bundle proprietary AMD or NVIDIA drivers; missing signed
firmware, unsupported hardware, or software fallback is reported as a coverage
gap rather than a pass. Driver- and firmware-aware image slimming is allowed
only after it preserves the hardware matrix.

The Phase 1 `hhpt graphics` command is the first active test. It discovers DRM
devices and connector modes, records the Mesa/GBM renderer, detects software
fallback, and can run a bounded KMS pattern on the first connected card. The
`hhpt display` worker additionally applies a bounded libdrm preferred-mode
pattern to all connected outputs per card and captures compact P/F/I operator
findings. It is not a complete automated visual-quality, safe-mode-restoration,
stress, VRAM, video-codec, or hybrid-graphics qualification. Normal finalization
includes both evidence records in the signed aggregate payload and standalone
HTML/PDF report views.

## Discovery and planning

Build a stable hardware graph before offering tests:

1. Enumerate buses and kernel driver bindings from sysfs, udev, PCI, USB, ACPI,
   device tree, DMI, V4L2, DRM, ALSA, IIO, networking, and block devices.
2. Normalize vendor data without losing raw evidence; empty, generic, and
   known-placeholder serials never become identity inputs.
3. Map graph capabilities to declarative test requirements.
4. Show detected, testable, unsupported, and missing-driver states separately.
5. Freeze the plan before execution; hotplug can add a new explicit test step
   but cannot silently change a destructive target.
6. Re-observe the stable graph before signing, bind every result to the frozen
   plan and planned graph HMAC, and record added, removed, or changed component
   bindings in a `run.integrity.quick` result.

Official test definitions are embedded in the image. A definition declares its
ID and version, architecture, applicability, privilege, external commands,
network access, storage access, estimated duration, timeout, required operator
action, and produced evidence. The daemon rejects undeclared execution.
The current `hhpt plan` slice resolves a subset of that contract, creates a
deployment-keyed hardware-graph binding and deterministic frozen ID, and makes
safe boot execute only `scheduled` tests. Planned coverage reports
`unsupported` until its reviewed implementation exists.

A graph change does not discard evidence. It fails run integrity and preserves
the final report with the original/final graph HMACs and component-level delta,
so a verifier can see that one unchanged hardware graph did not span the run.

Modern explicit-key mode mounts a separately controlled deployment token
read-only and binds its block, partition, and USB IDs as test fixtures. Those
fixtures are omitted from the HUT fingerprint and frozen capability graph but
remain mounted so report-media discovery excludes them. Automatic and legacy
boots remain per-session and unlinkable.

Initial CPU and RAM workers are in-process reviewed Go adapters. Each receives
a global deadline, an independent thermal monitor, bounded memory/output, and
no target-storage, network, or external-command authority. A fail, error, or
safety abort prevents later active quick tests in that run.
Automatic boot adds a separate process-level kill deadline beyond each
worker's recorded internal deadline.

## Privilege and process model

Run the UI as a dedicated user and `hhptd` as root. Prefer read-only sysfs,
device ACLs, and Linux capabilities. An adapter receives only the device nodes
and output directory it needs. Every subprocess gets a timeout, process group,
Linux parent-death signal, bounded stdout and stderr, and a fixed environment
and working directory.

The initial graphics adapter narrows this contract further: absolute image tool
paths, validated frame counts, a 20-second subprocess timeout, and a 64 KiB
combined-output limit. It has no network or storage-mount capability.

Bind the API to a Unix socket. If a browser UI is used, place a random session
capability in the launch URL, bind only to loopback, reject cross-origin calls,
and never allow API access from report HTML.

## Storage and persistent state

Root is tmpfs backed by immutable image contents. Do not auto-mount internal
filesystems, replay foreign journals, import pools, or auto-activate RAID/LVM.
Mount report media with `nodev`, `nosuid`, and `noexec`.

The current report-destination flow discovers only separately inserted,
removable USB partitions. It requires an HMAC-authenticated typed challenge,
binds authorization to the current kernel boot and 15-minute monotonic expiry,
then writes only a verified signed bundle through an exclusively created plain
filename at the fixed RAM-prepared mount point. The operator must explicitly
mount the target; filesystem parsing is not automatic.

A finished USB layout will use a boot partition or hybrid image plus an
optional writable `HHPT_DATA` partition. Runtime writes are limited to current
session state and report bundles. Raw captured artifacts are bounded so FAT32
can remain a compatibility fallback; exFAT is preferred for a dedicated data
partition after legacy-machine testing.

The local lab stores VDI or QCOW2 files on NTFS and never passes a physical disk
through to test storage functions. Disposable drive-test VMs get a separate
sparse disk whose identity is recorded when the VM is created.

## Result state machine

Each check ends in exactly one state:

- `pass`: acceptance criteria met.
- `fail`: hardware behavior violated measurable acceptance criteria.
- `skip`: not applicable or intentionally declined, with a reason.
- `inconclusive`: applicable but insufficient evidence or operator input.
- `error`: test infrastructure or execution failed.
- `abort`: operator, thermal limit, power state, or safety policy stopped it.

Warnings annotate a state but do not replace it. Final overall state preserves
required-test policy; it is not a simple count of green rows.

## Security and trust

Security by design and layered containment are release requirements. Assume
firmware, attached media, local networks, device metadata, individual test
tools, and operator input can each be hostile or mistaken. Prevention,
least privilege, containment, detection, and recovery must remain independent.

Release images are reviewed immutable artifacts with an SBOM, checksums,
signatures, and GitHub build-provenance attestation. Runtime does not execute
content obtained from a website, report, inserted storage filesystem, or remote
test definition. Network checks contact only user-selected or allowlisted
targets and keep uploads disabled by default.

Report hashes prove content consistency, not truth. Strong auditability comes
from signed report manifests, key provenance, evidence, operator identity, and
an optional external append-only submission service. Fingerprints are privacy
sensitive and must never be treated as authentication secrets.

## Acceptance targets

- Boot to an operator menu on BIOS and UEFI VMs with no writable internal-disk
  mount and no network dependency.
- Produce the same canonical inventory and applicable plan on repeated boots.
- Complete a VM quick profile and verify its report on Windows and Linux.
- Refuse destructive storage operations against unapproved, changed, boot, or
  report media targets.
- Make every included binary traceable to its version, source, and license.

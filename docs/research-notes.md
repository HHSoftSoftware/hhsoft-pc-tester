# Research Notes

Checked 2026-10-12. Sources are official project documentation unless noted.

## Base OS and hypervisor

- Alpine 3.24.2 is the current stable release and v3.24 main packages are
  supported to 2028-06-01. Official builds include x86, x86_64, and AArch64;
  Alpine x86 means i686 or later with CMOV and SSE2.
- Alpine diskless mode runs from RAM over read-only image contents and supports
  configuration overlays. Its supported `mkimage` path can construct custom
  ISO profiles with BIOS and UEFI boot components.
- Current installation guidance requires the bootstrap ISO SHA-256 and
  detached GPG signature, while stock installation requires Secure Boot off.
- Debian official live images currently target amd64. SystemRescue is a mature
  BIOS and UEFI reference but its current image is x86_64. Buildroot creates
  controlled Linux appliances and live CD images across broad CPU targets, but
  requires more project-owned packaging.
- The legacy fallback uses Buildroot LTS 2025.02.18, Linux 6.12.112, an i586
  userspace baseline, and Go `GO386=softfloat`. This supports Pentium MMX-class
  CPUs without requiring SSE2. Buildroot can create an Isolinux hybrid live CD
  that boots as optical media or a BIOS USB disk. The Linux archive's detached
  signature was verified against Greg Kroah-Hartman's exact kernel.org signing
  fingerprint before it was locked.
- WSL 2 still enables the Microsoft Hyper-V architecture. Oracle documents
  that VirtualBox 7.2 can use Hyper-V as its engine but may degrade
  significantly. Microsoft still describes VirtualBox plus WSL 2 as unsupported
  and says third-party hypervisors can fall back to slower execution.

Decision: keep WSL disabled during the first VirtualBox lab. If the project
later standardizes on Hyper-V or QEMU under WSL 2, benchmark that route before
changing the workstation hypervisor.

Secure Boot revisit: Debian is the closest mature alternative because it has a
Microsoft-signed shim and signed EFI, GRUB, and kernel packages. Its official
live images remain amd64-only, however, while SystemRescue remains amd64-only
and does not support Secure Boot. Maintaining Debian for one firmware mode and
Alpine for the required x86 compatibility would outweigh that optional gain.
Keep Alpine as the single base and defer Secure Boot.

Sources:

- https://www.alpinelinux.org/downloads/
- https://www.alpinelinux.org/releases/
- https://wiki.alpinelinux.org/wiki/Architecture
- https://wiki.alpinelinux.org/wiki/Diskless_Mode
- https://wiki.alpinelinux.org/wiki/Installing_Alpine
- https://wiki.alpinelinux.org/wiki/UEFI_Secure_Boot
- https://docs.oracle.com/en/virtualization/virtualbox/7.2/user/EN-VBOX-7-2-USER.pdf
- https://learn.microsoft.com/en-us/windows/wsl/compare-versions
- https://wiki.debian.org/SecureBoot
- https://www.debian.org/CD/live/index.html
- https://www.system-rescue.org/Download/
- https://buildroot.org/download.html
- https://buildroot.org/downloads/manual/manual.html
- https://go.dev/wiki/MinimumRequirements
- https://kernel.org/

## Existing open projects

- Hardware Probe inventories hardware and performs a few short operability
  checks. Its privacy model salts serial and MAC hashes, but HHSoft reports need
  local-first evidence, per-component comparison, and deployment-controlled
  identifiers.
- Phoronix Test Suite already solves automated benchmark selection and result
  collection across many architectures. Treat it as an optional benchmark
  adapter only: its downloadable shell-based test profiles exceed the core
  image execution boundary.
- stress-ng is a suitable stress engine. It is not a hardware acceptance policy
  by itself; adapters must choose verifiable stressors, thermal limits, and
  duration.
- Memtest86+ is open source and now supports x86, x86-64, AArch64, and
  LoongArch64. Use it instead of proprietary PassMark binaries.

Sources:

- https://github.com/linuxhw/hw-probe
- https://github.com/phoronix-test-suite/phoronix-test-suite
- https://github.com/ColinIanKing/stress-ng
- https://memtest.org/readme

## Web design anti-pattern review

Current design criticism repeatedly identifies the same generic output:
purple-blue gradient heroes, glass panels, Inter everywhere, oversized centered
copy, soft shadows, and three identical rounded feature cards. Avoid replacing
those defaults with another arbitrary style prompt. HHSoft visuals should come
from its operator workflow: square status panels, port maps, dense evidence
tables, unambiguous warnings, and printed service-record cues. The GOV.UK
system is a useful accessibility and task-writing reference, not a visual theme
to copy.

Sources:

- https://design-system.service.gov.uk/
- https://blog.interfacekit.io/what-makes-a-website-look-ai-generated
- https://www.925studios.co/blog/ai-slop-design-tells

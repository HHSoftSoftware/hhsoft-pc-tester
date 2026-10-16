# Secure Boot Plan

Status: optional and deferred until after the functional release. Nothing in
this document is a beta or 1.0 release gate.

## Current support

The VirtualBox lab has verified the official Alpine x86_64 bootstrap ISO at a
login prompt under SeaBIOS and under plain UEFI. It has not verified Secure
Boot. The stock Alpine ISO is not trusted by factory-default Secure Boot and
must currently boot with Secure Boot disabled.

| Firmware mode | CSM | Secure Boot | Planned support |
| --- | --- | --- | --- |
| Legacy BIOS | not applicable | unavailable | required |
| UEFI using legacy path | on | unavailable | compatibility only |
| UEFI native | off | off | required |
| UEFI with HHSoft key enrolled | off | on | optional later enhancement |
| UEFI with factory Microsoft trust | off | on | optional audited goal |

## Two signed paths

For controlled facilities, publish an HHSoft-signed unified kernel image plus
the public enrollment certificate. The operator verifies its fingerprint and
enrolls it in firmware, after which Secure Boot remains enabled. One UKI binds
the EFI stub, kernel, initramfs, command line, and appliance trust roots. Do
not store a private signing key on the USB device.

For machines that must boot with factory-default keys, use a current reviewed
shim signed by Microsoft UEFI CA 2023. The shim path adds distro key custody,
SBAT generation and revocation handling, reproducible-build evidence, a
security contact, and an update-continuity commitment. It cannot be treated as
a last-minute build option.

## Defense in depth after firmware

- Verify every post-firmware object before execution and reject unsigned
  kernel modules.
- Keep normal x86 BIOS and native UEFI entries separate from recovery and
  signed entries so compatibility does not silently weaken policy.
- Mount the appliance root read-only, minimize EFI filesystem writes, and keep
  report output outside the trusted runtime.
- Use runtime capability boundaries, test-definition signatures, destructive
  storage confirmations, and signed reports even after a trusted boot.
- Publish key fingerprints, revocations, SBAT data, and verification steps
  independently of each image.

## Acceptance tests

Use QEMU with Secure Boot-enabled OVMF and a disposable variables image. Test
firmware rejection before key enrollment, acceptance after enrolling only the
public HHSoft test certificate, and rejection after changing the UKI. Before a
factory-trusted release, repeat against OVMF containing Microsoft UEFI CA 2023
and a real representative PC in stock firmware state.

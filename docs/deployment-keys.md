# Offline Deployment Keys

## Purpose

Normal automatic boots generate a fresh 256-bit fingerprint key in RAM. That
protects privacy but deliberately prevents reports from different boots being
linked. An operator who needs return-part swap detection can instead use one
dedicated HHSoft deployment-key USB token. The same token and epoch produce the
same HMAC pseudonyms for the same durable component identifiers without
exporting raw serial numbers.

The deployment key is used only for HMAC pseudonyms and removable-media
authorization. It is not a report-signing, release-signing, submission, or
encryption key.

## Provisioning

Prepare a separate USB partition as FAT32, exFAT, or ext4. Use a hardware
write-protection switch where available. The device must expose a non-generic
USB or storage serial so hot-plug replacement can be bound safely. On a trusted
offline workstation, use the Windows utility or a current Linux binary:

```powershell
hhpt.exe deployment-key init -epoch shop-west-2026-1 `
  -output E:\hhpt-deployment-key.v1
hhpt.exe deployment-key inspect E:\hhpt-deployment-key.v1
```

The filename and filesystem-root placement are fixed. `init` creates the file
exclusively with a cryptographically random 256-bit key, an ASCII epoch, and a
SHA-256 corruption check. It never prints the key and refuses to overwrite an
existing token. `inspect` prints only the non-secret epoch and key identifier.

Keep at least two physically secured backup tokens or offline copies. Losing
every copy makes earlier HMAC identities impossible to reproduce. Never copy a
real key into source control, build inputs, issue attachments, reports, boot
images, or general-purpose technician USB drives.

## Boot Use

1. Insert the HHSoft boot USB and the separate deployment-key token.
2. Interrupt the 15-second automatic start and select `K`.
3. Select the displayed removable token and type `LOAD`.
4. Review the stable epoch/key ID shown by the boot and final report.
5. Use a different removable partition when saving the report.

The product probes the selected filesystem with fixed-path tools, accepts only
FAT32/exFAT/ext4, mounts the exact partition `ro,nodev,nosuid,noexec`, verifies
the mount and hot-plug binding before and after reading, and accesses only the
fixed key filename without following links. The key remains in RAM. The token
remains mounted read-only and is excluded from report-write selection. Its
exact block-parent, partition, and USB-device component IDs are also bound into
RAM and removed from the hardware-under-test inventory and frozen plan; if any
bound fixture ID is missing during either inventory step, the run fails.

Automatic starts never mount a key token or reuse identities. Explicit key
activation failure stops testing rather than silently producing an ephemeral
report with a misleading stable epoch. The no-SSE legacy image remains
ephemeral-only and labels its lower assurance.

## Comparison and Rotation

Use the same deployment token for both sessions, then verify and compare:

```powershell
hhpt.exe compare -self-issued -output return-comparison.json `
  original.hhpcr returned.hhpcr
```

Comparison verifies every signed bundle before matching component identities.
It rejects contradictory aggregate fingerprints and reports a key-ID or epoch
difference as unresolved rather than comparing unlike pseudonyms.

Create a new epoch and new random key for a planned rotation. Reports under the
new epoch are intentionally unlinkable to the old epoch without a future
authorized enrollment/key-bridging service. Do not relabel old key material as
a new epoch.

## Reduced Assurance

The alpha token file is checksummed but not encrypted at rest. Security relies
on physical custody, offline storage, read-only use, and backup controls. A
stolen key permits correlation and fabrication of HMAC pseudonyms for that
epoch. Shared tokens also broaden compromise scope, so larger deployments
should wait for the planned authenticated key service, TPM-bound collectors,
and trusted report signers. A normal self-issued report detects modification
of its saved bytes but does not authenticate who performed the test.

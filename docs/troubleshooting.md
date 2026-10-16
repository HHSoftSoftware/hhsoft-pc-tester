# Troubleshooting HHSoft PC Tester

This guide covers the bootable images and offline quick workflow. Start with the
least invasive action. Never format, mount read-write, repair, or initialize a
customer disk merely to make a test pass.

## Capture the failure marker

The boot console reports stable `HHPT_` markers. Photograph or transcribe the
last marker before rebooting. Useful checkpoints are:

- `HHPT_BOOT`: the UI selected an image mode and started the quick profile.
- `HHPT_PLAN`: discovery completed and the applicable plan was frozen.
- `HHPT_CPU`, `HHPT_MEMORY`, and other test markers: a worker completed or was
  classified as not scheduled.
- `HHPT_RUN_INTEGRITY`: the final graph either matched the frozen plan or records
  a component delta.
- `HHPT_READY`: the signed report exists in RAM and can be saved.
- `HHPT_FAILED` or `HHPT_SAFETY_FAIL`: the run stopped without a usable report.

Retry a hang only once. Record the image filename, checksum, firmware mode, and
the last marker when reporting a reproducible failure.

## Image will not boot

1. Verify the ISO SHA-256 against its adjacent `.sha256` file.
2. Confirm the entire ISO, rather than its extracted files, was written by a
   trusted image-writing tool.
3. Disable Secure Boot for the current development images. Do not disable TPM,
   measured boot, or firmware passwords unless the machine owner authorizes it.
4. Try the modern x86_64 image in UEFI mode first, then legacy BIOS/CSM mode.
5. For pre-SSE2 hardware only, try the separate `legacy-x86` image in BIOS/CSM
   mode. It intentionally provides lower coverage and assurance.
6. Disconnect unnecessary USB devices except input devices, the boot device,
   and an explicitly selected report or deployment-key device.

IA32 UEFI, Secure Boot, PXE, and physical AArch64 support are not yet qualified.
Use a supported route instead of forcing an unqualified firmware path.

## Auto mode did not start

The safe quick profile starts after 15 seconds only when no input is observed.
Keyboard, pointer, touchpad, or touchscreen activity cancels the countdown. Use
`1` or Enter to start it manually. A drifting pointer or defective touchscreen
can repeatedly count as activity; temporarily disconnect that peripheral and
record the symptom for its later input test.

## Empty inventory or safety stop

`HHPT_SAFETY_FAIL empty-inventory` means the image could not establish a minimum
hardware graph. `writable-block-mount` means a block-backed filesystem became
writable. Do not bypass either stop. Remove nonessential media, restore firmware
storage settings, and boot a fresh verified image. If the failure repeats, file
a security report and include only sanitized console markers.

The safe profile leaves network interfaces down and does not mount tested disks
read-write. An active network or unexpected writable mount is a safety failure,
not permission to relax the policy.

## Missing or unsupported tests

- `not_present` means no qualifying component was observed.
- `missing_driver` means compatible-looking hardware exists, but no bound
  provider was discovered.
- `unsupported` means this image deliberately does not implement that test.
- `inconclusive` means the adapter ran but could not prove readiness or a pass.
- `aborted` means cancellation or an earlier safety/correctness failure stopped
  the adapter.
- `error` means the bounded worker or evidence validation failed.

These states are not equivalent to a passing component. Try the correct modern
architecture, remove unsupported docks or adapters, and collect a new report.
Do not install ad-hoc vendor binaries in the live environment.

## Display is blank or low resolution

Wait for disk activity to settle, then change only the firmware boot path. A
serial console can expose the `HHPT_` markers in a qualified lab. A low native
resolution or software renderer should appear as limited graphics evidence,
not be silently labeled as accelerated. Record connector, resolution, artifact,
flicker, and scaling findings when the operator display pattern appears.

A display pass requires every applicable pattern and all operator criteria to
pass. Accepting a pattern on one output cannot clear a failed or incomplete
pattern on another. Record only what was observed, retain partial findings, and
repeat with stable connections when an output could not be checked. Automated
fixtures do not qualify physical multi-display behavior.

## Deployment-key device is rejected

The modern image accepts a separately selected removable, read-only token. It
must contain one exact `hhpt-deployment-key.v1` record and must not be the boot,
internal, or report device. Recreate damaged media with the offline deployment
key command; do not copy a production key into an image, source checkout, chat,
or issue attachment. Retain its epoch label for comparisons after rotation.

Without an accepted token, a report uses an ephemeral session identity. Such a
report proves internal consistency but cannot reliably detect a later parts
swap across boots.

## Report cannot be saved

The original signed report remains in RAM after `HHPT_SAVE_FAILED`. Keep the
machine powered on, insert one separate removable destination, and retry the
interactive save flow. The workflow rejects boot media, internal disks, mounted
devices, ambiguous USB trees, and destinations whose authorization changed.
Never reformat a drive to satisfy the save flow.

Use a separately prepared FAT32, exFAT, or compatible ext4 report partition.
Ext4 must use extents; any journal must be internal, and stored mount-option
overrides are rejected. Unknown, ambiguous, or unsupported filesystem headers
stop the save before a writable mount. Header recognition is a bounded read,
not a filesystem integrity check or media-health test. Try another prepared
report USB if the type is rejected; do not bypass the check or repair customer
media. Keep the selected destination connected throughout the save.

A destination copy is not valid unless the save receipt, deterministic bundle
digest, and final read-back verification all succeed. Preserve the report and
its verifier output together.

## Report finalization rejects evidence

Worker status alone cannot authorize a signed pass. Missing required
observations, malformed or ambiguous evidence, zero completed work, and safety
contradictions can stop finalization. Preserve the sanitized failure marker and
image version, then repeat from a verified image. Do not edit evidence or change
its status to force a report. Valid partial non-pass observations remain
reportable; an error or abort without usable worker evidence can instead carry
an explicit supervisor execution note.

## Report verification fails

Use the version-matched `hhpt` verifier and the expected public key/key ID. A
self-issued report can be checked only with the explicit self-issued mode and is
not equivalent to a deployment-signed release. Treat a bad signature, malformed
archive, duplicate entry, digest mismatch, or unexpected active content as
evidence rejection. Do not open embedded HTML/PDF views first to decide whether
the report is trustworthy.

Verification establishes bundle integrity and signature validity. It does not
repeat the evidence checks performed during capture/finalization or prove the
health of the tested hardware.

## Hardware changed during a run

An `HHPT_RUN_INTEGRITY status=fail` record is still signed evidence. It means
the final component graph did not match the graph frozen in the test plan. Save
the report if possible, reconnect the expected fixed hardware, and run again.
Do not remove the delta from a report or reinterpret it as a pass.

## Reporting a defect securely

Follow `SECURITY.md` for vulnerabilities. For ordinary defects, include product
version, image architecture, firmware boot mode, verified image checksum, final
console marker, and sanitized signed-report verifier output. Exclude serial
numbers, deployment keys, credentials, private paths, and raw customer reports
unless the maintainer explicitly requests an approved private transfer.

# HHSoft PC Tester Operator Guide

## Current Release Status

This guide covers the current `0.1.0-dev.0` implementation. It is an alpha
hardware-inspection appliance, not a complete certification, repair, data
recovery, erase, or burn-in product. Use it only on equipment you are
authorized to inspect.

The normal workflow boots an offline live image, freezes a hardware-aware
quick-test plan, performs the currently implemented checks, creates a signed
`.hhpcr` report in RAM, and optionally saves that report to a separate removable
USB partition. The environment must not mount an internal disk read-write or
configure a network connection.

## Choose the Correct Image

Release files use names such as
`hhsoft-pc-tester-VERSION-x86_64.iso`. Select one image before creating the
boot USB.

| Image suffix | Use for | Firmware | Current coverage and limits |
| --- | --- | --- | --- |
| `x86_64` | Most 64-bit Intel/AMD PCs | Legacy BIOS or native x64 UEFI | Preferred modern image; full current quick workflow and the broadest open-driver set |
| `x86` | 32-bit PCs with CMOV and SSE2, and unusual IA32-UEFI PCs | Legacy BIOS or IA32 UEFI where firmware supports it | Modern workflow, but 32-bit firmware and driver compatibility varies |
| `legacy-x86` | Pentium MMX-class or newer x86 PCs without SSE2 | Legacy BIOS, or UEFI with CSM/legacy support enabled | Reduced-coverage, lower-assurance fallback; no deployment-key mode and no active graphics/display quick tests |
| `aarch64` | ARMv8+ systems with standards-compliant AArch64 UEFI | Native AArch64 UEFI | Beta; generic QEMU boot is validated, but representative Snapdragon/X Elite hardware and device drivers are not yet qualified |

The modern images use pinned Alpine Linux packages. The legacy image uses a
separate minimal Buildroot/Linux base so that old non-SSE2 systems remain
bootable. Prefer `x86_64` or `x86` whenever the machine supports it. Do not use
`legacy-x86` merely because an x64 machine is booting through Legacy BIOS.

## Firmware Setup

Current images require **Secure Boot disabled**.

- On native UEFI, disable Secure Boot and leave CSM disabled. Select the UEFI
  entry for the HHSoft USB.
- On Legacy BIOS, select USB/optical-media boot normally. Secure Boot does not
  exist in this mode.
- Use CSM/legacy mode only for `legacy-x86`, or as a compatibility fallback
  for a modern x86 image when native UEFI boot fails.
- Do not enroll unknown keys, clear platform keys, or change TPM state to make
  this alpha boot. Those actions are unnecessary and can affect the installed
  operating system.
- Restore the customer's original boot order and firmware settings after the
  session.

If a UEFI boot entry and a non-UEFI entry are both displayed, choosing the
wrong entry can silently switch firmware modes. Confirm the expected image and
assurance label on the HHSoft splash screen.

## Prepare a Session

Use separate physical media for each role:

1. **Boot USB:** contains the selected HHSoft ISO.
2. **Report USB:** a formatted removable partition used only to receive the
   `.hhpcr` file. FAT32, exFAT, and ext4 are currently accepted.
3. **Deployment-key token:** optional, modern images only, and needed only for
   comparable return/parts-swap reports. It must not be used as the boot or
   report USB.

Verify the ISO's SHA-256 against the release `.sha256` sidecar on a trusted
workstation before writing it. Creating a boot USB normally erases that USB;
check the selected target twice and never select a customer's internal drive.
The appliance has no in-place updater, so a new product release means verifying
and writing a new image to the boot USB.

Before booting, connect AC power where appropriate, provide adequate cooling,
disconnect unnecessary USB devices, and unplug network cables unless network
link detection is part of the inspection. Leave Wi-Fi unassociated. Back up
customer data before handling suspect hardware even though the quick profile
is designed to be non-destructive.

## Safe Quick Start

After the HHSoft splash appears, a 15-second inactivity timer begins.

- Do nothing to start the automatic safe read-only quick profile.
- Press any key, move a mouse, or use a touch pointer to cancel automatic
  start and show the operator menu.
- If input monitoring is unavailable, automatic start is cancelled rather
  than started blindly.
- Choose `1` or press Enter to start the same safe quick profile.
- Choose `W` to remain at the menu. The automatic timer stays disabled.
- On a modern image, choose `K` to select a deployment-key token before the
  test. Legacy images do not offer `K`.

The quick profile is offline and uses read-only device discovery. It runs a
short CPU known-answer load, bounded RAM data/address patterns, device-node
readiness checks, and modern-image graphics/display checks where applicable.
Storage readiness opens eligible block devices read-only and issues no sector
read. USB readiness issues no USB transfers. Network readiness configures
nothing and sends no traffic. An active non-loopback interface or writable
block mount causes safe mode to fail closed.

The graphics pattern and CPU/RAM checks are active loads despite the read-only
storage policy. Supervise unstable equipment. If there is smoke, sparking,
swelling, abnormal heat, or mechanical distress, remove power rather than
waiting for software shutdown.

## Optional Deployment-Key Token

Normal boots create a fresh fingerprint key in RAM. Reports from separate
normal boots are intentionally unlinkable. Use a deployment-key token when the
business purpose is to compare the same machine at sale and return.

Provision the token on a trusted offline Windows workstation:

```powershell
hhpt.exe deployment-key init -epoch shop-west-2026-1 `
  -output E:\hhpt-deployment-key.v1
hhpt.exe deployment-key inspect E:\hhpt-deployment-key.v1
```

`init` refuses to overwrite an existing file. Store the fixed
`hhpt-deployment-key.v1` in the root of a dedicated FAT32, exFAT, or ext4 USB
partition with a stable non-generic hardware serial. Keep at least two secured
offline backup copies and use a hardware write-protect switch when available.

At the modern boot menu:

1. Select `K`.
2. Select the listed removable token.
3. Type `LOAD` exactly.
4. Check that the expected epoch and non-secret key ID are displayed.
5. Leave the token inserted. HHSoft keeps it mounted read-only and excludes its
   block and USB components from the system under test.

The token holds a plaintext 256-bit HMAC key plus a corruption check. It is
not a report-signing key, release key, encryption key, or proof of operator
identity. Theft permits correlation and fabrication of hardware pseudonyms
for that epoch. Never place it in source control, a report, a general-purpose
technician USB, or the public project. Use the same exact token for baseline
and return sessions. A new key/epoch deliberately makes later reports
unlinkable to earlier ones.

## Watch the Run

The console shows compact `HHPT_*` progress markers. Important markers are:

- `HHPT_PLAN`: the test plan and initial hardware graph have been frozen.
- `HHPT_CPU`, `HHPT_MEMORY`, `HHPT_INPUT_READINESS`,
  `HHPT_AUDIO_READINESS`, `HHPT_NETWORK_READINESS`,
  `HHPT_STORAGE_READINESS`, `HHPT_USB_READINESS`, `HHPT_GRAPHICS`, and
  `HHPT_DISPLAY`: individual outcomes.
- `HHPT_RUN_INTEGRITY status=pass`: the final component graph matches the
  frozen plan. A `fail` plus nonzero added, removed, or changed counts means
  hardware changed during the run; preserve the report but repeat the test
  with a stable setup.
- `HHPT_READY`: the bundle was finalized in RAM and the save menu is ready.
- `HHPT_SAFETY_FAIL` or `HHPT_FAILED`: the safe appliance guarantee or
  workflow failed. Do not treat the session as a completed inspection.

The display check may require operator observations because software cannot
reliably judge pixels, color, flicker, scaling, overscan, or visible artifacts
by itself. A readiness result only proves that the associated kernel interface
could be accessed, not that every key, button, port, pixel, cell, link, or
media sector works.

## Save the Signed Report

The only authoritative artifact is the `.hhpcr` bundle. Its HTML and PDF views
are convenient signed content inside the bundle, but a copied-out view cannot
be independently verified.

After `HHPT_READY`:

1. Insert a separate, formatted report USB. Do not use the boot USB or the
   deployment-key token.
2. Press Enter to rescan.
3. Select one eligible removable partition from the numbered list.
4. Verify the displayed device and size, then type its `RPT-...` confirmation
   code exactly.
5. Wait for `Saved and read-back verified` before removing the report USB.
6. Retain the complete `.hhpcr` file and copy it to operator-controlled backup
   storage on a trusted workstation.

HHSoft mounts only the selected target with `nodev,nosuid,noexec`, verifies
the self-issued report before writing, creates a new file without overwriting
an old report, synchronizes it, reads it back byte-for-byte, and unmounts. The
target must be a directly attached removable USB partition with a stable
identity; mounted, mapped, read-only, internal, boot, and token media are
excluded. If no destination is eligible, use another separately formatted USB
device. A report is limited to 64 MiB.

Select `Q` to leave without writing. The report then remains only in volatile
RAM and is lost on power-off or reboot. If saving fails, do not assume a partial
file is valid; rescan or use another target and preserve the machine's power
until a verified copy exists.

The current quick workflow creates an ephemeral **self-issued** Ed25519
signature. It makes the saved bundle tamper-evident and binds all results to
the frozen plan, but does not establish trusted operator identity, trusted
time, custody, or an enrolled HHSoft/deployment signer.

## Interpret Results

Read both the frozen plan status and the final result. They answer different
questions.

| Status | Operator meaning | Required action |
| --- | --- | --- |
| `scheduled` / supported | Compatible hardware and an implemented provider were present, so the plan selected the test | Check the final test result; this alone is not a pass |
| `pass` | The bounded implemented check completed without its defined failure | Confirm the check's stated scope before certifying the device |
| `fail` | The test observed a defined wrong result, unsafe condition, or changed run-integrity graph | Investigate, stabilize the setup, and retest; do not convert it to `inconclusive` |
| `not_present` | The relevant hardware was not detected | Confirm it is truly absent; if it should be present, check firmware, connection, and driver state |
| `unsupported` | This product version, image architecture, or selected profile does not implement the test | Use the modern image where possible or perform a documented external/manual test |
| `missing_driver` | Hardware was detected but no usable kernel/provider device appeared | Record as untested, try the current modern image, and investigate driver compatibility; absence of a failure is not a pass |
| `inconclusive` | Evidence was insufficient or ambiguous, including software rendering or inaccessible/partial readiness | Repeat under known-good conditions or use a controlled external fixture |
| `aborted` | A safety/correctness stop or earlier prerequisite failure prevented the test | Resolve the earlier result and repeat the entire session |
| `error` | The collector, tool, timeout, or report workflow failed | Treat the test as incomplete; reboot into a freshly verified image and retry |
| `skip` | A requested optional action was not run, or the implemented check deliberately did not apply | Review its reason and complete a manual/other-profile check if required |

An `integrity=fail` result does not invalidate the signature. It means the
signed report is evidence that the hardware graph changed between planning and
finalization. Do not use that session as a clean sale/return baseline.

## Verify and Compare on Windows

Use the matching `hhpt.exe` on a trusted Windows workstation. Do not open the
HTML or PDF before verification when a report came from untrusted hardware or
media.

Verify a normal self-issued report offline:

```powershell
hhpt.exe verify -self-issued .\hhpt-SESSION.hhpcr
```

Success reads:
`verified: contents/signature valid; signer is self-issued and does not prove operator identity`.
The verifier checks the canonical outer bundle, manifest digests, signature,
expected files, path safety, size limits, and static view restrictions.

Compare two sessions made with the same deployment-key token:

```powershell
hhpt.exe compare -self-issued -output .\return-comparison.json `
  .\original.hhpcr .\returned.hhpcr
```

The comparison verifies both bundles first. Matching deployment-key epoch
labels are not enough: the exact HMAC key ID must match. Added, removed, and
same-location changed identities are useful swap indicators. Unidentifiable
parts, unavailable serials, different keys/epochs, and ambiguous topology
remain unresolved rather than becoming false matches. The JSON comparison is
not itself a trusted authorization decision.

Provisioned signer verification syntax exists for future/trusted deployments:

```powershell
hhpt.exe verify -public-key .\signer.pub -key-id SIGNER_ID .\report.hhpcr
```

The current boot workflow does not enroll such an operator signer, so do not
use this mode unless your deployment has independently provisioned and
controlled the corresponding key pair.

## Update and Recovery

- **Update:** HHSoft does not download drivers, packages, profiles, or updates
  at runtime. Verify a newly released ISO and its checksum on a trusted
  workstation, write it to a fresh/re-imaged boot USB, and retain the previous
  known-good media until the new image passes local acceptance testing.
- **Failed boot:** Reconfirm architecture and firmware mode, keep Secure Boot
  off, try a different USB port/media, then try the serial/recovery or safe
  graphics boot entry if present in that image. Use `legacy-x86` only when the
  CPU lacks the modern baseline or no modern firmware path works.
- **Failed test/session:** Preserve any successfully saved `.hhpcr`, remove
  newly connected devices, cold-boot, and rerun from freshly verified media.
  Reboot is the runtime reset; there is no persistent appliance state to
  repair.
- **Failed save:** Keep power on, use the save menu to rescan or select another
  report USB, and accept only a read-back-verified file.
- **Compromised media/key:** Stop using it. Re-image boot media from a verified
  release. Rotate a stolen deployment key to a genuinely new key and epoch;
  do not merely rename the old epoch. Treat reports signed or fingerprinted
  with compromised secrets according to their exposure window.

## Privacy and Security Warnings

- The report intentionally replaces many durable serial numbers and MAC-like
  identifiers with HMAC pseudonyms, but component models, capacities,
  topology, test evidence, timestamps, and report IDs may still be sensitive.
- A deployment token enables correlation across every report made with it.
  Restrict access, backups, retention, and sharing to the stated business
  purpose.
- Signatures protect saved bytes, not physical truth. Firmware, virtual
  machines, malicious peripherals, compromised collectors, and counterfeit
  hardware can lie.
- Safe quick mode is not authorization to erase, repair, flash firmware,
  change configuration, or inspect data stored on customer media.
- Do not enable networking to upload a report from the appliance. Move the
  verified bundle through controlled removable media, then use an approved
  external transfer process.
- Parse and view reports on a patched, restricted workstation. The included
  HTML is script-free and the PDF forbids active content, but untrusted files
  and filesystems remain parser risks.
- Removing the boot USB, power loss, reboot, or crash destroys unsaved RAM
  reports. Bundle integrity cannot prevent deletion; maintain independent
  backups.

## Current Limits

- Only the safe quick profile is connected to the automatic boot workflow.
  Standard, extended, destructive, PXE, server upload/share, and authenticated
  managed-deployment workflows are not operator-ready.
- Quick CPU and RAM tests are bounded samples, not sustained stress or complete
  memory qualification. Full-media memory testing remains a separate future
  boot path.
- Input testing currently checks read-only event-device access; it does not
  prove every key/button or touch/touchpad geometry in automatic mode.
- Storage testing is readiness only: no SMART/NVMe health qualification,
  sampled sector verification, throughput, optical-media read, or complete
  media scan.
- USB testing is topology/access readiness only: no data, power-delivery,
  per-port, alternate-mode, or hot-plug qualification with a known-good
  fixture.
- Network testing is hardware/driver/link readiness only: no DHCP, Wi-Fi
  association, modem session, DNS, Internet, integrity, or throughput test.
- Modern graphics uses open kernel/Mesa paths including `i915`, `amdgpu`, and
  `nouveau` where supported. It does not qualify proprietary-driver-only
  NVIDIA/AMD functionality, VRAM, video engines, sustained stability, or all
  native-resolution/scaling behavior. Legacy graphics/display tests are
  unsupported.
- Audio currently verifies ALSA device-node readiness only; tones, routes,
  volume, channels, jack behavior, and capture remain unqualified. Camera,
  full keyboard/mouse/touchpad/touchscreen interaction, card readers as media,
  rotation sensors, modems, storage functional/health, full-network, and full
  USB-port tests are not yet implemented end-to-end.
- Secure Boot is not implemented. AArch64 remains beta and real Snapdragon/X
  Elite systems are not yet qualified.
- Self-issued reports prove byte integrity only. Trusted report signers,
  custody timestamps, TPM/HSM key protection, revocation, append-only database
  receipts, and policy-signed sale/return decisions remain future work.
- Part identity depends on identifiers exposed by firmware and devices.
  Missing, duplicate, privacy-randomized, or forged identifiers limit swap
  detection; fingerprints do not prove ownership, warranty, authenticity, or
  that a replacement was unauthorized.

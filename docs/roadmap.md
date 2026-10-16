# Roadmap

## Working checklist (2026-10-15)

- [x] Select a diskless Alpine base, permissive source license, initial threat
  model, report contract, and staged test strategy.
- [x] Provision the isolated scratch disk and repeatable BIOS/UEFI VirtualBox
  labs without passing through any physical drive.
- [x] Pin and verify Alpine release media, release key, and exact aports tag.
- [x] Implement read-only Linux inventory, redacted HMAC hardware identity,
  and an Ed25519 tamper-evident report envelope.
- [x] Unit test and cross-compile the inventory core for x86, x86_64, and
  AArch64; smoke-test the x86_64 binary in Alpine.
- [x] Build and boot the custom offline x86_64 image under BIOS and UEFI.
- [x] Build and boot the custom offline x86 image; verify the SSE2 baseline.
- [x] Emit and independently verify HTML/PDF views and `.hhpcr` report artifacts.
- [x] Verify and compare two signed reports with component-level explanations
  for matched, moved, replaced, added, removed, and unresolved hardware.
- [ ] Lock packages/licenses, complete consumed-package SBOMs, and validate reproducible release
  checks before tagging `0.1.0-alpha.1`.
- [x] Add a source-only pull-request CI gate with minimum permissions,
  immutable action pins, cross-builds, lock checks, and local-material guards.
- [x] Validate the 15-second keyboard/mouse-cancellable automatic safe mode
  and lightweight console menu in BIOS and UEFI VMs.
- [x] Build a minimal BIOS/CSM Buildroot legacy image and boot it under an
  emulated pre-SSE2 CPU.
- [x] Build and UEFI-boot a beta AArch64 image under QEMU.
- [x] Add DRM/KMS discovery, Mesa acceleration/software-fallback detection,
  and a bounded KMS graphics pattern probe.
- [x] Add automated input event-device readiness and privacy-safe aggregate
  activity observation.
- [x] Add bounded CPU/memory quick workers plus no-write storage, no-traffic
  network, and no-transfer USB readiness checks to automatic safe mode.
- [x] Add signed-report verification plus same-boot typed authorization and
  exclusive persistence to a separately inserted removable USB partition.
- [x] Finalize automatic-profile evidence into a self-contained signed report
  and expose typed report saving through the normal console UI.
- [x] Add preferred/native-mode whole-card output patterns, compact operator
  findings, and signed-report integration.
- [x] Activate a purpose-separated stable fingerprint key from explicit,
  read-only USB media and exclude the token from HUT identity.
- [ ] Validate safe restoration and simultaneous multi-output operation on
  representative physical Intel, AMD, NVIDIA, and ARM graphics systems.

## Phase 0 — architecture and safe lab

- Accept Alpine diskless, separate-architecture image, Go orchestrator, and
  component-level report fingerprint decisions.
- Initialize source-only repository, BSD 3-Clause license, security model,
  test matrix, report contract, and release gates.
- Format the authorized scratch disk for file-backed VMs only.
- Boot the official local Alpine x86_64 ISO in BIOS and EFI VirtualBox VMs and
  capture repeatable serial logs and screenshots.

Exit: a new contributor can explain all trust boundaries and launch disposable
VMs without exposing a physical disk.

## Phase 1 — inventory image

- Implement `hhptd inventory`, stable hardware graph, canonical JSON report,
  simple TUI, and read-only HTML export.
- Build pinned custom x86_64 and x86 Alpine images.
- Add VM boot, no-auto-mount, and deterministic-inventory tests.
- Generate package lock, source/license inventory, checksums, and SBOM.
- Boot a Pentium MMX-class Buildroot fallback with reduced inventory coverage.
- Add the first bounded active graphics probe without claiming complete GPU or
  display qualification.

Exit: `0.1.0-alpha.1` boots offline under SeaBIOS and OVMF, including on a
pre-SSE2 BIOS CPU, produces verifiable inventory without touching internal
virtual disks, and records initial DRM/KMS/Mesa graphics evidence where
applicable.

## Phase 2 — quick functional test

- Expand the embedded capability graph beyond the initial graphics, CPU,
  memory, storage, input, USB, network, audio, and camera families.
- Add CPU, partial memory, SMART/NVMe health, read-only storage, complete
  display/GPU, network, audio, input, USB, and camera quick checks.
- Add operator prompts, clear unsupported states, thermal watchdog, and report
  signatures.
- Pilot on at least ten varied PCs without destructive tests.

Exit: `0.2.0-beta.1` gives useful resale acceptance results in under 15 minutes.

## Phase 3 — complete profiles

- Add Memtest86+ boot integration, standard and extended profiles, calibrated
  storage self-tests, sensors, battery, optical media, card reader, modem, and
  optional remote submission.
- Add controlled temporary-file writes and a physically separate destructive
  image for wipe and full write qualification.
- Expand BIOS and EFI device matrices in CI.

Exit: release candidate has documented criteria and no open critical safety or
report-integrity issue.

## Phase 4 — public release and ecosystem

- Revisit optional operator-enrolled or factory-trusted Secure Boot only after
  the functional release and update-lifecycle commitments are stable.
- Publish `1.0.0`, user guide, operator guide, PDF manual, GitHub Pages site,
  threat model, support policy, source-only extension SDK, and sample server.
- Promote the beta AArch64 UEFI media after real Snapdragon X Elite testing
  where drivers and firmware permit useful coverage.
- Reassess the pre-SSE2 image's feature coverage and real-hardware demand
  before expanding its independent package surface.

## Decision gates before implementation expansion

- UI: start the safe read-only quick profile after 15 seconds without keyboard
  or pointer activity; otherwise offer the keyboard-driven TUI. Test dedicated
  visual screens before adopting Chromium, WebKit, Qt, or Slint.
- Server: define retention, owner authentication, tenant boundaries, and key
  custody before any report upload exists.
- Storage: destructive qualification remains separate until whole-device
  authorization has independent negative tests.
- Hardware identity: operator deployments choose locally comparable SHA-256 or
  server-comparable HMAC identifiers; raw serial export stays explicit.

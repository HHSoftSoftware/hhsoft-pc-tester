# HHSoft PC Tester Master Checklist

Status date: 2026-10-17

This is the project-level source of truth derived from the original product
requirements. Check an item only when implementation and applicable validation
are complete. Architecture and policy documents alone do not make an item
complete; real-hardware requirements remain open until physically tested.

## Product and repository

- [x] Use the product name **HHSoft PC Tester**.
- [x] Use Semantic Versioning from the repository `VERSION` through artifacts.
- [x] Publish under the BSD 3-Clause license with HHSoft provenance and the
  non-endorsement clause.
- [x] Provide contributor and security-reporting instructions.
- [x] Guard reference-only material from commits and release images.
- [x] Record clean-room functionality lessons without copying reference code.
- [x] Define a stable third-party test-adapter/test-definition API.
- [x] Add project governance, maintainer, and merge-review rules.
- [x] Create and verify the public repository under `HHSoftSoftware`.
- [ ] Publish the first signed Semantic Version release.

## Boot platforms

- [x] Boot x86_64 under legacy BIOS.
- [x] Boot x86_64 under UEFI with Secure Boot disabled.
- [x] Boot modern x86 under legacy BIOS with its SSE2 baseline.
- [ ] Validate modern x86 under IA32 UEFI on representative firmware.
- [x] Boot the separate no-SSE2 legacy image under BIOS/CSM.
- [x] Build and QEMU-boot the beta generic AArch64 UEFI image.
- [ ] Validate beta AArch64 on representative Windows-on-ARM/X Elite hardware.
- [ ] Add PXE artifacts for supported BIOS/UEFI architectures.
- [ ] Add an optional, documented Secure Boot path.
- [ ] Qualify USB boot and recovery behavior on varied physical PCs.

## Base and release engineering

- [x] Use a pinned diskless Alpine branch for modern images.
- [x] Use pinned Buildroot/Linux inputs for the legacy fallback.
- [x] Centralize base-version upgrades in lock and update scripts.
- [x] Validate upstream media hashes, exact aports revision, build scripts, and
  Semantic Version format.
- [x] Build modern x86, x86_64, and AArch64 from one image profile.
- [x] Generate architecture-specific checksums, package locks, and SPDX SBOMs.
- [x] Generate the legacy image's SPDX inventory from Buildroot legal-info,
  binding the ISO digest and preserving upstream license declarations.
- [x] Run source-only CI with minimal permissions and immutable actions.
- [ ] Consume a digest-pinned package cache as a release-build input.
- [ ] Pin the builder toolchain and verify the host tools used for release images.
- [x] Add a fixed-path host inventory gate before and after image builds, with
  a strict development-version exception, native failure-case tests, and an
  actual-host comparison. Reviewed input manifests are committed; full builder
  and image qualification remain open.
- [x] Compile modern image applications from verified committed source during
  each invocation, bind source/compiler-policy/target/binary records, and verify
  the actual overlay copy. Native failure-case fixtures and fresh offline
  compilation with actual overlay checks passed for all three modern targets;
  independent finished-ISO extraction remains open.
- [x] Bind legacy application compilation to exact committed source, run its
  Go tests and build offline with a source cache, and recheck the installed
  executable and its exclusive receipt after image stages. The full 179-test
  native build suite passes with no skips. Fresh offline compilation, embedded
  version/target inspection, and real post-build installation checks pass;
  fresh legacy image construction and finished-ISO extraction remain open.
- [x] Require the pinned Go package for both image-host profiles, observe fixed
  global download/loader configuration, and reject extended attributes,
  capabilities, and ACLs in the bounded host inventory. All 42 native inventory
  tests pass; this does not approve a host or complete toolchain qualification.
- [x] Add cache-lock generation/verification and an offline Alpine build mode
  that refuses unreviewed cache bytes and reused apk roots.
- [x] Assemble unchanged Alpine package inputs from original indexes with
  metadata/checksum validation, explicit gap reports, and independent native
  checks against authentic upstream archives; signature trust and dependency
  closure remain separate requirements.
- [x] Require a reviewed legacy source cache and network-isolated Make phases,
  with fresh independent copies, policy binding, and pre/post-build checks.
  Native failure-case fixtures and the pinned upstream license-copy helper
  pass. The 29-archive source input lock is reviewed and committed; a complete
  fresh image build remains open.
- [x] Review and freeze the initial alpha host, modern package, and legacy
  source inputs with upstream authentication, independent byte checks, and
  native offline installation evidence. This approves controlled build inputs;
  it does not approve images or release publication.
- [ ] Validate the offline cache mode against the preserved builder inputs and
  boot every resulting image before completing the release-input milestone.
- [ ] Build and boot images automatically in CI rather than only cross-building
  application binaries.
- [ ] Add release signatures, protected-environment approval, provenance, and
  public release automation.
- [x] Implement a draft-release publication workflow requiring all four ISOs,
  authenticated lab records, approved tag signers, source validation, companion
  utilities, a signed checksum statement, and approval before attestation.
- [x] Reject agent-instruction backup variants, outgoing unreferenced commits,
  replaced Git objects, and merge-only historical secrets in publication checks.
- [ ] Configure GitHub release protections and validate the first complete
  publication run; image production and lab reviews remain external to CI.
- [x] Publish reviewed source with passing GitHub CI, protect `main` and release
  tags, and enable GitHub secret scanning, push protection, and immutable releases.
- [ ] Automate supported-branch update checks and security rebuild reminders.

## User interface and profiles

- [x] Provide a lightweight console UI.
- [x] Start the safe profile after 15 seconds without interaction.
- [x] Cancel automatic start on keyboard, mouse, touchpad, or touch activity.
- [x] Cancel automatic start when input monitoring fails, recognize generic
  touch devices by capabilities, and validate disconnect and transient-error
  behavior with native Linux tests on amd64 and 386 soft-float.
- [x] Keep networking off and tested disks unmounted by default.
- [x] Show reduced coverage/lower assurance on the legacy image.
- [x] Show branded, versioned console text before test execution.
- [x] Build the declarative capability graph and freeze an applicable test plan.
- [ ] Provide complete quick, standard, extended, and destructive profiles.
- [ ] Provide per-test selection, configuration, cancellation, timeout, and
  remaining-time/status displays.
- [ ] Provide localized UI and accessibility review.

## Hardware discovery and identity

- [x] Inventory CPU, memory, firmware, PCI, USB, block, network, and DRM data.
- [x] Redact raw stable identifiers from normal exported attributes.
- [x] Verify that collected input-device identifiers stay out of signed
  inventory, HTML, and PDF reports while useful device details remain.
- [x] Generate deployment-keyed component and system HMAC identities.
- [x] Exclude mutable or low-quality identifiers from stable identity inputs.
- [x] Provision a stable deployment key/epoch from a separate explicitly
  selected, read-only USB token on modern images.
- [x] Reject deployment-token FIFOs and other non-regular files before data
  access, with bounded native tests and a pinned-directory replacement case.
- [x] Record hot-plug changes and bind each test plan/result to the observed
  hardware graph.
- [x] Compare signed reports and clearly identify added, removed, or replaced
  components.
- [x] Build an offline Windows x86_64 utility for report verification and
  comparison.
- [ ] Implement privacy-preserving enrollment and key-rotation service flows.

## Functional tests

- [x] Run a bounded CPU known-answer quick worker with independent thermal and
  global deadline monitoring.
- [x] Run bounded RAM quick patterns over a safely selected working set.
- [x] Discover DRM cards, bound drivers, render nodes, connectors, and modes.
- [x] Detect Mesa hardware acceleration versus software-rendering fallback.
- [x] Run a bounded KMS display pattern on a connected display.
- [x] Check read-only Linux event readiness for present keyboards, mice,
  touchpads, and touchscreens without recording key codes.
- [x] Check storage mount safety and read-only block-node readiness without
  reading or changing media contents.
- [x] Check Ethernet/Wi-Fi driver and link readiness while failing closed on
  active non-loopback interfaces and generating no network traffic.
- [x] Check USB root/device topology, authorization, speed, and read-only USBFS
  node readiness without issuing USB transfers.
- [x] Check ALSA card and PCM/control node readiness without opening audio
  streams.
- [ ] Select and restore native/safe display modes on every connector.
- [x] Collect operator display findings for pixels, geometry, color, flicker,
  scaling, overscan, and artifacts.
- [ ] Test GPU VRAM, compute, sustained rendering, thermals, reset/recovery,
  hybrid graphics, and video-codec engines.
- [ ] Test CPU correctness, instruction coverage, performance, stability, power,
  and thermal behavior.
- [ ] Test available RAM from Linux with a bounded quick/extended worker.
- [ ] Add the separate Memtest86+ boot entry for complete memory coverage.
- [ ] Test audio output, microphone input, channels, volume, and jack behavior.
- [ ] Test keyboard, mouse, touchpad, touchscreen, pen, and buttons.
- [ ] Test USB controllers and every operator-identified external port.
- [ ] Test card readers using read-only discovery and a removable fixture.
- [ ] Test HDD/SSD/NVMe/ODD discovery, health, read correctness, throughput,
  surface/media errors, and supported self-tests.
- [ ] Test Ethernet link/addressing/DNS/throughput with opt-in network fixtures.
- [ ] Test Wi-Fi radio/association/signal/throughput with opt-in fixtures.
- [ ] Test cameras, microphones, rotation/other sensors, and supported modems.
- [ ] Add short versus long variants and capability-based applicability for all
  completed test families.

## Reports and auditability

- [x] Generate sanitized offline HTML inventory.
- [x] Generate and independently verify an Ed25519 `.hhpcr` report envelope.
- [x] Require the entire bundle's deterministic ZIP bytes and persisted copy
  digest to match the version independently verified.
- [x] Create output files exclusively instead of silently overwriting evidence.
- [x] Integrate graphics through a generic functional-result record in the
  signed payload and HTML report.
- [x] Save verified signed reports to a separately authorized removable USB
  destination with exclusive creation, read-back, sync receipt, and final
  revalidation.
- [x] Reject incomplete persistence receipts and directory synchronization
  failures before announcing a successful save; preserve recoverable files
  and validate that retries cannot overwrite them.
- [x] Return to fresh destination selection and typed confirmation after a
  failed save and successful unmount; preserve partial files and stop when
  cleanup is uncertain or the session is cancelled. Failure-injection tests
  cover recovery; fresh ISO recovery qualification remains a release check.
- [x] Finalize normal quick-profile evidence once and provide an interactive,
  typed-confirmation save flow without requiring a root shell.
- [x] Export a deterministic static PDF view without active content inside each
  signed report bundle.
- [x] Verify present HTML/PDF views against the signed inventory and version,
  including correctly signed hostile views, with bounded rendering.
- [ ] Submit reports to an authenticated web service and/or network share.
- [ ] Add append-only ingestion receipts and server-side return comparisons.
- [ ] Implement report/key revocation, rotation, archival, and deletion policy.

## Security and safety

- [x] Maintain a layered threat model and explicit trust boundaries.
- [x] Prevent automatic writable block mounts in safe mode.
- [x] Use only virtual file-backed disks in development VMs.
- [x] Run fixed-path graphics tools without a shell, inherited environment, or
  network access; bound time and captured output.
- [x] Generate local state in RAM and keep production signing keys out of images.
- [x] Scan candidate source on every build and scan full Git history in CI for
  private identity terms, credentials, local paths/endpoints, keys, and forbidden artifacts.
- [x] Add an independently pinned generic credential scanner to CI and require
  GitHub secret scanning and push protection for publication.
- [x] Exclude agent instructions/state and inspect actual staged bytes plus
  author, committer, and tag metadata before the first push.
- [x] Audit completed readiness/reporting claims against independent failure
  cases and add regression checks for mount authorization, zero-work results,
  contradictory evidence, and CPU/RAM/graphics/display failure preservation.
- [x] Gate appliance startup and ready markers on independent mount/network
  observations, including mapped storage and software network interfaces.
  Native metadata and supervisor fixtures pass; fresh ISO boot qualification
  remains required for release.
- [ ] Separate the unprivileged UI, privileged daemon, and sandboxed workers.
- [ ] Add thermal, hang, process-group, and global run watchdogs.
- [x] Implement deployment-keyed, same-boot typed authorization for separate
  removable USB report destinations with mounted boot/internal-media exclusion.
- [ ] Implement typed authorization for destructive targets with boot-media,
  internal-drive, RAID, LVM, multipath, and report-media exclusion.
- [ ] Add parser fuzzing and containment for hostile EDID, SMART, media, camera,
  network, and report data.
- [x] Seed and fuzz hostile report archives and exact comparison JSON schemas.
- [ ] Add signed vulnerability disclosure and incident-response procedures.

## Validation lab

- [x] Provision the authorized 1 TB disk as isolated VM scratch storage.
- [x] Boot modern x86_64 under VirtualBox BIOS and UEFI.
- [x] Boot modern x86 under VirtualBox BIOS.
- [x] Boot legacy x86 with an emulated no-SSE/SSE2 CPU.
- [x] Boot AArch64 UEFI through a repeatable offline QEMU harness.
- [x] Validate automatic-start and fail-safe menu behavior.
- [x] QEMU-validate a repeatable whole-system fingerprint across boots using the same
  file-backed read-only deployment-key USB token.
- [x] QEMU-inject USB/block hot-plug after plan freeze and verify the signed
  run-integrity failure and component delta.
- [x] QEMU-emulate an HDA codec and verify signed ALSA readiness evidence.
- [x] QEMU-validate the real report-save path on disposable file-backed USB:
  reject wrong confirmation with zero writes, then extract and independently
  verify the authorized saved report.
- [x] Verify image checksums and no writable tested-disk mounts.
- [x] VM-validate USB topology and direct read-only USBFS node access on
  modern x86/x86_64, no-SSE legacy x86, and AArch64 images.
- [ ] Add device fixture/fault injection for every automated test adapter.
- [ ] Test releases on at least ten varied PCs before beta/1.0.
- [ ] Establish representative Intel, AMD, NVIDIA, ARM, laptop, tablet, and
  workstation physical-hardware qualification sets.

## Documentation and community

- [x] Document architecture, security, test strategy, report integrity, builds,
  release gates, and Secure Boot direction.
- [x] Author a complete operator user guide and troubleshooting guide.
- [ ] Build a GitHub Pages site under `HHSoftSoftware`.
- [ ] Define a deliberately non-generic visual system using square geometry and
  researched interface anti-examples.
- [ ] Generate a versioned PDF documentation manual.
- [x] Apply the documented issue labels in GitHub after repository creation;
  templates, bootstrap CODEOWNERS, and feature/test ownership are complete.

# HHSoft PC Tester Master Checklist

Status date: 2026-10-26

This is the project-level source of truth derived from the original product
requirements. Check an item only when implementation and applicable validation
are complete. Architecture and policy documents alone do not make an item
complete; real-hardware requirements remain open until physically tested.

The 2026-10-24 requirements audit compared the retained test matrix with the
catalog and execution paths. Development source now contains eleven implemented
safe quick test definitions, subject to hardware and architecture applicability.
The pointer movement/button and wheel-direction increments passed automated
and native Linux software validation. Guided keyboard software qualification
also passed; rebuilt-image and physical gates remain open for both. The planner
also accepts standard, extended, and destructive profile names, but
their complete execution workflows remain unfinished. Burn-in is a planned
profile. The unchecked requirements below include those broader matrix items;
the alpha still requires the separate documented release gates.

## Product and repository

- [x] Use the product name **HHSoft PC Tester**.
- [x] Use Semantic Versioning from the repository `VERSION` through artifacts.
- [x] Publish under the BSD 3-Clause license with HHSoft provenance and the
  non-endorsement clause.
- [x] Provide contributor and security-reporting instructions.
- [x] Guard reference-only material from commits and release images.
- [x] Record clean-room functionality lessons without copying reference code.
- [x] Define a stable third-party test-adapter/test-definition API.
- [ ] Implement third-party adapter package validation, protocol negotiation,
  confined execution, and API conformance tests.
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
- [x] Validate frozen catalog rows, component references, and unambiguous plan
  JSON before report finalization. Reject rehashed applicability changes when
  the verified hardware graph is unchanged; preserve genuine hot-plug evidence.
- [x] Choose implemented safe quick tests in the console, freeze exclusions in
  the plan, and preserve them in signed results. Native amd64/386 tests and both
  startup supervisors retain independent safety checks, architecture limits,
  and unavailable-hardware reasons. Application menu-to-report verification
  passes for modern x64/x86 and legacy x86; rebuilt-image gates remain separate.
- [ ] Provide complete quick, standard, extended, burn-in, and destructive
  profiles, including burn-in scheduling and its adapter-contract support.
- [x] Report the active quick worker, elapsed time, and remaining shared budget;
  support console cancellation and signed stop reasons in the quick session.
- [ ] Extend per-test selection, configuration, cancellation controls, and
  remaining-time/status displays to broader profiles.
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
- [x] Add twelve fixed scalar binary64 checks to each CPU worker, with per-worker
  counts and retained mismatch bits in CPU v3 evidence. Shared validation binds
  CPU definition 2 through capture, v5 sessions, signed bundles and HTML/PDF
  reports; historical CPU v2 hashes and v4/v5 verification remain supported.
- [x] Validate the scalar CPU increment with the full host suite: 2,543
  test/subtest passes across 25 packages, 17 platform test skips and three
  packages without host test files. Vet, 28 Linux test-binary builds and five
  application builds passed. Native Linux amd64/386 ran 28 suites with 4,608
  test events; six race suites passed 1,745 events, with no native skips. All
  30 CLI scenarios passed, including standalone and session CPU capture and
  signed finalization on amd64, x86 SSE2 and legacy x86 soft-float.
- [ ] Qualify the scalar CPU increment in rebuilt images and on representative
  physical CPUs. Legacy soft-float execution establishes software arithmetic
  coverage; hardware floating-point coverage remains separate.
- [x] Include Linux hwmon temperature readings in the CPU/RAM thermal guard,
  filtering unavailable, disabled, faulted, uncalibrated, and known threshold
  channels. The earlier hwmon milestone passed native amd64 and 386 soft-float
  fixture tests without skips;
  representative physical sensor validation remains open. The separate
  run-wide implementation and its qualification gates are tracked below.
- [x] Implement and validate the strengthened local CPU/RAM thermal guard in
  automated and native Linux fixtures. The guard bounds the initial and later
  readings, joins the observer before returning evidence, reports
  available/partial/unavailable coverage, and blocks signing when cleanup is
  unconfirmed. That milestone introduced CPU and memory v2 evidence; known
  calculation and memory failures remain failures. Windows tests, vet, five
  application builds, native amd64/386 suites, race checks, and command-line
  workflows passed; counts are recorded in
  [requirements validation](docs/requirements-validation.md).
- [ ] Qualify the strengthened local CPU/RAM thermal guard in rebuilt bootable
  images and on representative physical sensors.
- [x] Run bounded RAM quick patterns over a safely selected working set.
- [x] Discover DRM cards, bound drivers, render nodes, connectors, and modes.
- [x] Detect Mesa hardware acceleration versus software-rendering fallback.
- [x] Run a bounded KMS display pattern on a connected display.
- [x] Check read-only Linux event readiness for present keyboards, mice,
  touchpads, and touchscreens without recording key codes.
- [x] Implement and validate the guided relative-pointer worker and signed
  session integration with portable and native Linux fixtures. It observes `EV_REL` X/Y movement in
  both directions and primary plus advertised right/middle button cycles on
  each interface; absolute/mixed interfaces and broader mouse/touch coverage
  remain unsupported. Cancellation, device changes, queued event loss, forged
  capabilities, and retained partial passes have regression coverage.
- [x] Validate the pointer wheel-direction increment with the full host suite,
  vet, 28 Linux test-binary builds and five application builds. The suite recorded
  2,364 test passes across 24 packages, 17 platform test skips and four packages
  without host test files. Both advertised wheel directions, stream selection,
  strict evidence/plan binding and interrupted-read regressions are covered.
- [x] Validate pointer wheel evidence v2 and definition 2 with native Linux,
  race and signed CLI checks. The 28 Linux amd64/386 suite runs passed 4,114
  test/subtest events; five race suites passed 1,508, with no skips. All 24 CLI
  cases passed, including three standalone and three pointer-only v5 sessions.
  Unsupplied VM actions remained inconclusive or deadline-limited; standalone
  and session consumers preserve historical v1 scope. These results do not
  establish successful physical actions or qualify rebuilt images.
- [ ] Qualify guided pointer actions in rebuilt images and on physical devices,
  including no-wheel, vertical/horizontal, high-resolution and multiple pointers,
  withheld directions, cancellation and disconnects.
- [x] Validate the guided keyboard increment with host and native Linux fixtures.
  Six fixed press/release controls per interface, strict typed evidence,
  exclusive console ownership, final neutral/idle/identity checks and cleanup
  uncertainty have validated v5 integration. Its eleven workers preserve strict
  historical v4 verification. Windows passed 2,156 test/subtest events across 23
  packages, with 21 platform skips; vet, 26 Linux test builds and five application
  targets passed. Native Linux amd64/386 passed 3,784 events across 26 suite
  runs, and five race suites passed 1,461 events, with no skips. All 21 CLI
  scenarios passed with confirmed cleanup.
- [ ] Qualify the six-control keyboard check in rebuilt images and on physical
  keyboards, including multiple devices, held keys, incomplete actions,
  disconnects and cancellation. Full layouts and rollover remain separate.
- [x] Check storage mount safety and read-only block-node readiness without
  reading or changing media contents.
- [x] Bind storage readiness to canonical registry, pathname and open-descriptor
  device numbers; reject duplicate identities and final-component
  symlinks, recheck registry identity, and withhold current-device credit after
  cancellation or failed closure. Focused and independent simulated checks pass;
  the readiness v1 schema and no-media-I/O boundary are unchanged.
- [x] Validate storage identity hardening with the full host suite: 2,369
  test/subtest passes across 25 packages, 17 platform test skips and three
  packages without host test files. Vet, 28 Linux test-binary builds and five
  application builds passed against the same source.
- [x] Validate storage identity hardening with native Linux amd64/386 fixtures.
  The 28 suite runs passed 4,220 test/subtest events, and five existing race
  suites passed 1,508, with no skips. All 24 CLI cases and cleanup checks passed;
  this does not establish physical-disk behavior or media-read correctness.
- [x] Harden storage-readiness mount admission with the shared bounded safety
  parser. Empty or malformed observations, unknown mounted block devices,
  unsupported anonymous filesystems and writable overlays stop before node
  opens. Previously observed writable counts survive later parse, registry or
  cancellation failures; the existing read-only overlay policy is unchanged.
- [x] Check the mount-policy increment with 109 focused Windows test/subtest
  passes and 139 simulated Linux-function events. Independent review added
  25 events, for 164 simulated events with no failures; these are not native
  kernel or physical-device tests.
- [x] Validate the mount-policy increment with the full host suite: 2,406
  test/subtest passes across 25 packages, 17 platform test skips and three
  packages without host test files. Vet, 28 Linux test-binary builds and five
  application builds passed against the same source.
- [x] Validate the mount-policy increment on native Linux amd64/386: 28 suite
  runs passed 4,364 test/subtest events and five existing race suites passed
  1,545, with no skips. All 24 CLI cases and cleanup checks passed. Storage
  fixtures use simulated metadata and descriptors; storage has no separate
  race suite in this cohort.
- [ ] Qualify storage readiness, replacement and cancellation in rebuilt images
  and on physical devices. Matching kernel numbers do not prove persistent
  physical identity or detect every same-number replacement.
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
- [ ] Cross-check memory capacity, topology, speed, and exposed SPD data against
  firmware and OS observations; monitor corrected and uncorrected ECC deltas.
- [ ] Add the separate Memtest86+ boot entry for complete memory coverage.
- [ ] Test audio output, microphone input, channels, volume, and jack behavior.
- [ ] Test keyboard, mouse, touchpad, touchscreen, pen, and buttons.
- [ ] Test USB controllers and every operator-identified external port.
- [ ] Test card readers using read-only discovery and a removable fixture.
- [ ] Test HDD/SSD/NVMe/ODD discovery, health, read correctness, throughput,
  surface/media errors, and supported self-tests.
- [ ] Test Ethernet link/addressing/DNS/throughput with opt-in network fixtures.
- [ ] Test Wi-Fi radio/association/signal/throughput with opt-in fixtures.
- [ ] Test Bluetooth discovery, pairing, services, data exchange, and temporary
  pairing cleanup with known-good fixtures.
- [ ] Test cameras, microphones, rotation/other sensors, and supported modems.
- [ ] Collect adapter/battery health and charge telemetry and test AC/battery
  transitions where supported.
- [ ] Monitor temperatures, fans, clocks, power, and throttling throughout a run
  and test cooling response under sustained load.
- [ ] Report Secure Boot, TPM, measured-boot, and firmware-lock capabilities
  without changing firmware state or provisioning keys.
- [ ] Test PCIe, Thunderbolt, and dock topology, link properties, and controlled
  hot-plug behavior with appropriate fixtures.
- [ ] Measure RTC/monotonic discontinuities and drift against a trusted reference
  over a sufficient observation period.
- [ ] Add operator physical-inspection findings for chassis, hinges, ports,
  damage, cooling noise, and battery swelling.
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
- [x] Retain every frozen catalog row in signed reports and HTML/PDF views,
  including excluded, absent, unsupported, and missing-driver dispositions.
  Label direct capture as limited coverage and preserve historical rendering.
- [x] Include the quick session's independent mount/network safety observations,
  execution states, and stop reason in the signed report, with a fresh safety
  observation before signing. Unsafe or interrupted readiness blocks signing.
- [ ] Preserve corrections and retests as new signed report revisions linked
  to the prior report digest.
- [ ] Add explicitly consented encrypted restricted-evidence export while
  keeping raw identifiers out of normal reports, views, filenames, and logs.
- [ ] Submit reports to an authenticated web service and/or network share.
- [ ] Add append-only ingestion receipts and server-side return comparisons.
- [ ] Implement report/key revocation, rotation, archival, and deletion policy.

## Security and safety

- [x] Maintain a layered threat model and explicit trust boundaries.
- [x] Prevent automatic writable block mounts in safe mode.
- [x] Use only virtual file-backed disks in development VMs.
- [x] Run fixed-path graphics tools without a shell, inherited environment, or
  network access; bound time and captured output.
- [x] Clean up Linux workers and same-process-group descendants before reaping
  their leader, with bounded cleanup and output draining. Unconfirmed cleanup
  stops later graphics/display work and blocks capture or appliance signing.
  Escaped process groups remain outside this cleanup guarantee. Quick-session
  cancellation is implemented separately by the coordinator.
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
- [x] Implement and validate periodic read-only mount/network safety monitoring
  throughout quick execution and final safety, using a
  one-second interval and observation deadline, a 2,500-millisecond maximum
  gap, and a two-second join allowance. The first failure cancels the session
  and blocks signing; journal v3 records bounded aggregate coverage with the
  same ten worker outcomes. Windows and native Linux suites, race checks,
  cross-builds, and 15 signed CLI scenarios passed. Each 2.5-second deadline
  case retained two periodic samples and an exact matching signed summary.
- [ ] Qualify periodic mount/network monitoring in rebuilt bootable images and
  on physical hardware. Application and isolated VM tests do not close these
  gates or establish continuous safety, thermal, or heartbeat coverage.
- [x] Extract the shared periodic observer engine in source, preserving the
  safemode API and v1 evidence. Focused checks passed 217 test events with no
  failures or skips; six exact wire cases were executed before extraction and
  remain unchanged. Full Windows tests, vet and cross-builds passed. Native
  Linux amd64/386 suites passed 2,724 test events; race checks passed 1,105,
  and all 15 CLI scenarios passed with confirmed cleanup. These results qualify
  the extraction; the subsequent run-wide increment has separate gates below.
- [x] Implement sampled thermal monitoring throughout the safe quick session,
  including an immediate observation with all tests excluded, every worker,
  cleanup, final inventory, and a final observation before signing. Journal v4
  binds both monitor joins, coverage and local CPU/RAM heat evidence through a
  one-use in-process final gate. Heat stops new work but remains reportable after
  confirmed cleanup; monitoring failures block signing.
- [x] Complete full host, native Linux, race, and CLI qualification for the
  run-wide thermal/v4 increment. Windows passed 1,754 test/subtest events across
  22 packages with 21 platform skips and no failures; vet, 24 Linux test-binary
  builds and all five application targets passed. The 24 native Linux amd64/386
  suite runs passed 3,032 events and four race suites passed 1,203, with no skips.
  All 15 CLI scenarios and ownership/cleanup checks passed. All twelve session
  scenarios honestly reported unavailable temperature telemetry; all-off runs
  retained initial/final observations and deadline runs retained two periodic
  observations. These results do not establish physical sensor health.
- [ ] Qualify run-wide thermal/v4 monitoring in rebuilt bootable images and on
  representative physical PCs. The beta pilot and release gates remain open.
- [ ] Separate the unprivileged UI, privileged daemon, and sandboxed workers.
- [ ] Complete heartbeat, hang, and deadline watchdogs beyond the quick-session
  coordinator, terminal outer guard, and worker process-group cleanup; extend
  thermal supervision to the remaining profiles and telemetry sources.
- [x] Coordinate safe quick tests under one session deadline and each frozen
  test's complete budget; support Ctrl-C and Linux termination signals, retain
  completed failures, and finalize clean stops with fresh bounded contexts.
  A terminal outer guard blocks ready/save on failure. Kernel stalls, escaped
  process groups, and continuous coverage remain separate limits; periodic
  sampling does not establish continuous safety or heartbeat monitoring.
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

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
- [x] Route both comparison reports and applicable public keys through bounded
  regular-file acquisition with checked handle closure, propagate cancellation
  before output and use fixed input/verification diagnostics. Preserve trust
  defaults, HMAC identity policy and output ownership; host and selected Linux
  amd64/386 fixtures passed (see the [validation record](docs/requirements-validation.md#comparison-input-ownership)).
- [x] Acquire capture's fingerprint and signing keys through bounded regular-file
  reads with checked ownership, fixed input diagnostics and cancellation before
  collection. Preserve worker cleanup exit 12, signing and output policy;
  host and selected Linux amd64/386 fixtures passed (see the
  [validation record](docs/requirements-validation.md#capture-input-ownership)).
- [x] Check finalizer fingerprint-key input ownership. Accept exactly 32 bytes
  only after checked input closure, reject invalid contexts locally and preserve
  the fresh session finalization context. Host and selected Linux amd64/386
  fixtures passed (see the
  [validation record](docs/requirements-validation.md#finalizer-fingerprint-input-ownership)).
- [x] Implement checked fingerprint-key acquisition for report-target discovery
  and authorization. Require exactly 32 bytes after checked input closure, use
  fixed input diagnostics and stop on cancellation before the selected consumer.
  Preserve catalog status, authorization and output policies.
- [x] Validate this report-target input implementation on the Windows host and
  selected Linux amd64/386 fixtures: 5,159 host test passes and 244 native named
  passes across four suites. See the
  [validation record](docs/requirements-validation.md#report-target-fingerprint-input-ownership).
  Four legacy raw-key calls remain after this implementation: deployment
  discovery, two persistence inputs and save.
- [x] Implement checked save fingerprint-key acquisition after existing usage and
  audit initialization. Require exactly 32 bytes after checked input closure,
  use fixed key-input diagnostics and preserve the report-saving policy.
- [x] Qualify this boundary with 5,240 host test/subtest passes and 260 native
  outcomes across two Linux suites and 56 root executions. All 130 selected
  save names passed on the host and on each native architecture; see the
  [validation record](docs/requirements-validation.md#save-fingerprint-input-ownership).
  This implementation reduces the four legacy calls recorded at the report-target
  stage to three: deployment discovery and two persistence inputs. Audit owners,
  frozen-plan/evidence readers and persistence's live source remain separate work.
- [x] Implement checked 32-byte fingerprint and trusted public-key inputs for
  persistence, with fixed input diagnostics and error/shape checks before
  post-read cancellation. Preserve authorization decoding, report verification
  and persistence policy.
- [x] Qualify the persistence key-input boundary with 5,374 host test/subtest
  passes and 444 selected native passes across Linux amd64 and 386; see the
  [scoped validation evidence](docs/requirements-validation.md#persistence-key-input-ownership).
  This implementation reduces the three legacy calls recorded at the save stage
  to one in deployment discovery. Authorization-file, audit, frozen-plan/evidence
  and live report-source ownership remain separate work.
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
- [x] Extend bounded RAM patterns with moving inversions and final verification.
  Memory v3 and definition 2 retain partial work and explicit mismatches through
  capture, v5 sessions and signed consumers; historical v2 hashes remain valid.
- [x] Validate the RAM increment with the full Windows suite: 2,755 test/subtest
  passes across 25 packages, 17 platform test skips and three packages without
  host test files. Vet, 28 Linux test-binary builds and five application builds
  passed. Compiled x86 inspection confirmed buffer loads, stores and reverse
  indexing; it does not establish physical DRAM traffic or cache bypass.
- [x] Qualify the RAM increment with native Linux suites, race tests and signed
  direct-capture, standalone-finalization and session workflows: 5,000 native
  test/subtest passes across 28 suites, 1,894 race-test passes across six suites
  and 39 CLI cases, including nine RAM cases. These VM checks do not establish
  physical-memory or temperature-sensor coverage.
- [ ] Qualify the RAM increment in rebuilt beta images and on representative
  physical memory systems. Broader working-set and hardware-fault coverage remain
  open.
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
- [x] Implement the guided-pointer acquisition increment with bounded registry
  and identity reads, metadata checks before readable node access, checked owned
  closure and hard-error precedence over cancellation. Remove the unused shared
  cleanup mode while preserving reachable keyboard behavior.
- [x] Validate this acquisition increment with the full host suite, formatting,
  vet and Linux amd64/386/arm64 cross-builds. All 75 selected host roots and
  their 320 test/subtest outcomes pass, including ten new roots and 39 children.
- [x] Validate the selected acquisition fixtures on Linux amd64/386: 848
  test/subtest outcomes across four suites, all 194 required root executions,
  no failures or skips, and confirmed cleanup.
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
- [x] Harden guided keyboard acquisition with bounded registry/identity reads,
  metadata checks before readable access and checked closure of every owned
  handle. Preserve hard faults and cleanup uncertainty across cancellation;
  retain earlier passes only after final idle, neutral and identity proof.
- [x] Validate the keyboard acquisition increment with the full host suite,
  formatting, vet and Linux amd64/386/arm64 cross-builds. All 86 selected host
  roots and their 385 test/subtest outcomes pass, including eleven new roots
  and 54 children.
- [x] Validate selected keyboard acquisition fixtures on Linux amd64/386:
  1,196 test/subtest outcomes across four suites, all 238 required root
  executions, no failures or skips, and confirmed cleanup.
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
- [x] Reject contradictory filesystem types for one canonical zero-major
  device in the shared mount parser. Valid same-type aliases and diskless views
  remain supported, and earlier writable block counts survive a later conflict.
  Runtime-safety v1 and storage-readiness v1 evidence remain unchanged.
- [x] Validate the mount-identity correction with focused Windows regressions
  and the full host suite: 2,791 test/subtest passes across 25 packages, 17
  platform test skips and three packages without host test files. Formatting,
  vet, 28 Linux test-binary builds and five application builds passed.
- [x] Validate the mount-identity correction on native Linux amd64/386: 5,072
  test/subtest passes across 28 suites, 1,930 race-test passes across six suites
  and all 39 CLI cases, with no native or race skips and confirmed cleanup.
  Mount/storage fixtures do not establish physical-device or media-read behavior.
- [x] Bound registry enumeration before the existing 256-entry admission limit.
  Require complete EOF and successful directory closure; reject overflow without
  truncation and preserve lexical error order, diskless views and normal sysfs
  links. The limit counts every entry before supported-media filtering.
- [x] Validate bounded enumeration with focused regressions and the full Windows
  suite: 2,848 test/subtest passes across 25 packages, 17 platform test skips and
  three packages without host test files. Formatting, vet, 28 Linux test-binary
  builds and five application builds passed.
- [x] Validate bounded enumeration on native Linux amd64/386: 5,256 test/subtest
  passes across 28 suites, 1,930 race-test passes across six suites and all 39 CLI
  cases, with no native/race skips or failures and confirmed cleanup. Directory
  fixtures use temporary files; block metadata and descriptors remain simulated.
- [x] Require valid capacity metadata for supported non-partition media. Bound
  size, flag and state reads to 256 raw bytes after EOF and confirmed closure;
  retain valid zero-capacity media and optional unknown values. Recheck
  cancellation before metadata credit and the no-media conclusion.
- [x] Validate scalar admission with focused regressions and the full Windows
  suite: 2,910 test/subtest passes across 25 packages, 17 platform test skips and
  three packages without host test files. Formatting, vet, 28 Linux test-binary
  builds and five application builds passed.
- [x] Validate scalar admission on native Linux amd64/386: 5,538 test/subtest
  passes across 28 suites, 1,930 race-test passes across six suites and all 39 CLI
  cases, with no native/race skips or failures and confirmed cleanup. Scalar
  fixtures use temporary files; block metadata and descriptors remain simulated.
- [x] Enumerate inventory directories through actual EOF using bounded requests,
  with no total device-count cutoff. Close acquired handles before admitting
  names; reject directory failures and malformed or duplicate entries without
  returning a successful partial snapshot. Preserve legitimate absent paths and
  diskless systems.
- [x] Keep collection context and errors separate for each call, preserve
  source-owned slices, and check cancellation around acquisition, processing
  and final snapshot creation. Reject collection errors at inventory, plan,
  capture and finalize boundaries before output or later report work.
- [x] Validate this increment with 1,900 focused Windows test/subtest passes
  across eight packages, six platform test skips and no failures. Formatting
  and focused vet passed; all 29 new portable test functions passed.
- [x] Complete full host validation with 3,047 Windows test/subtest passes across
  25 packages, 17 platform test skips, three packages without tests and no failures.
  Vet passed; 28 cross-built test binaries and five application targets built
  successfully. Source, documentation and host artifacts passed the privacy check.
- [x] Complete native qualification with 5,816 test/subtest passes across 28
  suites, 2,106 race-test passes across seven suites and all 39 CLI cases, with
  no native/race skips or failures and confirmed cleanup. All 31 new test
  functions passed on amd64 and 386; the 28 inventory functions and three
  command-boundary functions also passed in their corresponding race suites.
- [ ] Qualify this increment in rebuilt images and on physical devices; complete
  the pilot and required release approval.
- [x] Bound inventory scalar values to 64 KiB (65,536 raw bytes), with one extra
  probe byte, exact EOF and confirmed closure. Cover every declared attribute
  and direct scalar role through 105 category/role cases; preserve optional
  unknown values and all instances. Type, protocol, overflow and cleanup
  failures stop collection.
- [x] Read DRM connector status and enabled fields separately from controller
  and render-node PCI fields. Keep unrelated device-link directories out of
  scalar acquisition while preserving failures for invalid declared attributes.
- [x] Validate scalar acquisition with 2,215 focused Windows test/subtest passes
  across eight packages, six platform test skips and no failures. Formatting
  and focused vet passed; all 26 new portable test functions passed.
- [x] Complete scalar full host validation with 3,362 test/subtest passes across
  25 packages, 17 platform test skips, three packages without tests and no failures.
  Vet passed; 28 Linux test binaries and five application targets built. All 26
  new portable functions, 105 scalar category/role cases and 12 full-collection
  command cases passed. The host artifact privacy check passed all 487 files with zero findings.
- [x] Complete scalar/DRM native qualification with 6,468 test/subtest passes
  across 28 suites, 2,432 race-test passes across seven suites and all 39 CLI
  cases. All 31 functions, including five Linux-only functions, passed on both
  amd64 and 386 and in their corresponding race suites. No native/race skips
  or failures occurred; cleanup and the 584-file privacy check passed.
- [x] Bound primary proc memory, CPU and input-device acquisition to 256 KiB,
  16 MiB and 8 MiB respectively, with exact EOF, one probe byte and confirmed
  closure. Preserve decorator dispatch and grow buffers from a small allocation.
- [x] Validate the proc increment with 2,309 focused test/subtest passes across
  eight packages, six platform skips and no failures. Formatting, vet, all 15
  new portable functions and the 407-file privacy check passed.
- [x] Complete proc full host validation with 3,456 test/subtest passes across
  25 packages, 17 platform test skips, three packages without tests and no
  failures. Formatting, vet, 28 Linux test-binary builds and five application
  targets passed. All 15 new portable functions and the 493-file host
  privacy check passed.
- [x] Complete proc native qualification with 6,660 test/subtest passes across
  28 suites, 2,528 race-test passes across seven suites and all 39 CLI
  cases. All 17 new functions, including two Linux-only functions, passed
  on amd64 and 386 and in their corresponding race suites. No native/race
  failures or skips occurred; cleanup, six root postchecks and the
  590-file artifact privacy check passed.
- [ ] Bound total acquisition, aggregate/peak allocation and parser CPU use;
  improve optional field-error visibility. Legacy aggregate providers can
  allocate before returning, and cancellation cannot interrupt a blocked call.
- [ ] Qualify storage readiness, replacement and cancellation in rebuilt images
  and on physical devices. Matching kernel numbers do not prove persistent
  physical identity or detect every same-number replacement.
- [x] Implement the inactive storage sampling result reader with complete typed
  topology/fixture reconstruction, bounded counters and retained failure and
  cleanup semantics. Evidence contains no media data or content fingerprint.
- [x] Add explicit v6 session read support through signed-report verification,
  HTML/PDF rendering and comparison, including bundles without optional views.
  Preserve v4/v5 wire behavior; current emission stays v5 with eleven workers
  and the storage sampling catalog entry remains inactive.
- [x] Validate the preceding admission and observation model with 140 Windows
  test/subtest passes across 30 test functions, no skips, formatting and vet.
  This fixture result does not qualify the later integrated report reader.
- [x] Validate the integrated reader and report consumers on Windows: 3,776
  test/subtest passes across 27 packages, including all 67 candidate test
  functions; 17 existing test skips and three empty packages. Formatting, vet
  and Linux amd64/386/arm64 cross-builds passed using fixtures without media I/O.
- [x] Qualify that reader source on Linux amd64 and 386: 1,582 test/subtest
  passes across 14 suite runs, all 67 candidate roots on each architecture,
  and 62 verify/compare CLI cases. No failures or skips; nine runtime
  ownership and 32 archive checks passed with six final checks and
  confirmed cleanup. The CLI matrix uses provisioned trust and no/both
  views. This predates the separate output-completion change and grants
  no native close-fault, media, rebuilt-image or physical qualification.
- [ ] Implement and qualify the media worker, backing-device and loop-lifetime
  safeguards, supervised acceptance and catalog activation. Rebuilt images,
  physical media and release approval remain separate gates.
- [x] Check Ethernet/Wi-Fi driver and link readiness while failing closed on
  active non-loopback interfaces and generating no network traffic.
- [x] Bound network registry, scalar, driver-link and fallback acquisition
  with exact EOF and owned closure. Preserve earlier complete interface
  diagnostics after later faults.
- [x] Complete corrected network acquisition host validation: all ten portable
  functions, formatting, vet and Linux amd64/386/arm64 whole-source builds.
- [x] Execute all 21 required network/command functions on Linux amd64 and
  386, including ten portable, seven new Linux and four retained Linux
  functions, with zero failures or skips and confirmed cleanup.
- [ ] Bound total/peak network acquisition allocation and establish blocked
  kernel-operation deadlines and persistent physical identity. Functional
  traffic, rebuilt-image, physical-hardware and release gates remain open.
- [x] Check USB root/device topology, authorization, speed, and read-only USBFS
  node readiness without issuing application USB transfers.
- [x] Bind USB node readiness to the selected registry device number, address
  and authorization, with matching pathname/open-descriptor identity and
  successful close. Reject duplicate identities/addresses before node access;
  retain partial diagnostics and stop later USB opens after closure failure
  or cancellation. The existing result schema and session behavior remain.
- [x] Complete host validation and Linux cross-test compilation of the USB
  identity/closure correction.
- [x] Complete selected USB identity/closure tests on Linux amd64 and 386,
  including owned-process and archive checks with confirmed cleanup.
- [ ] Qualify rebuilt images and physical USB ports separately from the
  bounded synthetic readiness evidence.
- [x] Bound every USB readiness scalar to 256 raw bytes with one overflow
  probe, exact EOF and owned closure. Read class once; preserve unknown
  optional values only after clean absence or successful malformed reads.
- [x] Bound USB registry enumeration to 2,048 raw entries before the separate
  512-device limit. Reject overflow without truncation; retain ordinary
  sysfs links, cached names and complete directory closure.
- [x] Validate the acquisition increment with the full Windows suite, all
  seven new portable functions, formatting, vet and three Linux builds.
- [x] Execute all 44 required USB/command functions on Linux amd64 and 386:
  18 acquisition functions and 26 retained functions, with zero skips or
  failures and confirmed cleanup.
- [ ] Bound total USB acquisition/allocation and establish kernel-operation
  deadlines; retain separate image, physical-port and release gates.
- [x] Check ALSA card and PCM/control node readiness without opening audio
  streams.
- [x] Bound ALSA registry and attribute acquisition with exact completion,
  cached names and checked closure of every owned metadata handle. Retain
  root containment, final-component no-follow and descriptor identity checks.
- [x] Validate the audio acquisition increment with the full Windows suite,
  all twelve new portable functions, formatting, vet and three Linux builds.
- [x] Execute and reconcile all 41 required audio/command functions on Linux
  amd64 and 386: 438 test/subtest events across four suites, all 82 required
  function executions, and no failures or skips.
- [ ] Establish blocked-kernel operation deadlines and qualify rebuilt images
  and physical audio hardware separately from metadata-readiness evidence.
- [x] Bound input-readiness registry and identity acquisition with exact EOF,
  metadata-only node verification, pinned readable descriptors and checked
  owned closure. Retain earlier complete diagnostics after later faults.
- [x] Preserve aggregate-only input activity, final cancellation handling and
  existing command/session/finalizer behavior; reject non-pass promotion.
- [x] Validate input acquisition with 4,205 passing host test/subtest events
  across 29 packages, formatting, vet and Linux amd64/386/arm64 cross-builds;
  all ten new portable roots and 24 named children pass.
- [x] Execute selected input/command tests on Linux amd64 and 386: 364 passing
  test/subtest events across four suites, all 70 required root executions,
  no failures or skips, and confirmed cleanup.
- [ ] Establish blocked-kernel deadlines and qualify physical input devices,
  rebuilt images and release behavior separately from fixture evidence.
- [ ] Select and restore native/safe display modes on every connector.
- [x] Collect operator display findings for pixels, geometry, color, flicker,
  scaling, overscan, and artifacts.
- [ ] Test GPU VRAM, compute, sustained rendering, thermals, reset/recovery,
  hybrid graphics, and video-codec engines.
- [ ] Test CPU correctness, instruction coverage, performance, stability, power,
  and thermal behavior.
- [ ] Extend the RAM quick worker to broader working-set coverage and extended testing.
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
- [x] Treat write, short-write and owned-file close failures as CLI output
  failures. Preserve combined errors, close owned destinations exactly once,
  retain caller-owned stdout, and print finalizer success only after close.
  Uncertain worker cleanup retains exit status 12 over output failure.
- [x] Validate output completion in an isolated Windows candidate: 3,878
  test/subtest passes across 27 packages, including all nine new test
  functions and 93 named child cases; 17 existing test skips and three
  empty packages. Formatting, vet and Linux amd64/386/arm64 builds passed.
- [x] Qualify the selected injected output faults on Linux amd64/386: 204
  test/subtest outcomes across two suites, including all nine roots and 93
  named children on each architecture, with no failures or skips. All 18
  root executions, nine runtime ownership cases, 32 archive checks and six
  final checks passed with cleanup confirmed.
  These fixtures do not qualify kernel-close or fsync faults or installed-CLI
  execution. Closing a CLI destination does not establish synchronization,
  durable storage or image readiness. The existing durability, rebuilt-image,
  physical-PC and release gates remain open.
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
- [x] Add opt-in linked retests to session/finalize and one-hop verification.
  A new signed report uses a distinct canonical UUIDv4 and references both
  the verified parent's manifest digest and canonical bundle SHA-256.
  Preserve the parent, independent trust choices, current evidence and the
  existing live safety/cleanup gates. Child-only verification leaves the
  predecessor relationship unchecked; no asset or history claim is implied.
  Validation recorded 4,804 passing host test/subtest outcomes across 29
  packages and 698 selected native outcomes across eight suites, including
  all 110 required root executions on Linux amd64/386. Native runs had no
  failures or skips and confirmed cleanup. Image and physical-PC gates stay open.
- [ ] Add explicit signed correction history, separate from retesting;
  preserve finalized originals and distinguish correction, supersession,
  revocation and custody semantics.
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
- [x] Implement bounded safemode metadata acquisition with exact completion,
  contained metadata/readable-descriptor identity checks, and confirmed closure
  of owned root, registry and attribute handles. Preserve known unsafe counts
  after later faults; incomplete observations cannot certify safety.
- [x] Validate this acquisition increment with host tests, formatting, vet and
  Linux amd64/386/arm64 cross-builds. All 50 required host roots and their
  311 test/subtest outcomes passed.
- [x] Execute the selected native Linux acquisition fixtures: 640 test/subtest
  outcomes and all 108 required root executions passed across amd64 and 386,
  with zero failures or skips and confirmed cleanup.
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

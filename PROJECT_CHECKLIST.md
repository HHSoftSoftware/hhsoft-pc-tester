# HHSoft PC Tester Master Checklist

Status date: 2026-10-31

This is the project-level source of truth derived from the original product
requirements. Check an item only when implementation and applicable validation
are complete. Architecture and policy documents alone do not make an item
complete; real-hardware requirements remain open until physically tested.

The 2026-10-24 requirements audit compared the retained test matrix with the
catalog and execution paths. Development source now contains fourteen
implemented safe Quick definitions and sixteen implemented catalog rows,
subject to hardware, architecture and explicit operator applicability.
The bounded Extended memory route is implemented and has passed corrected
Windows host validation; native and full-duration integration remain open.
The pointer movement/button and wheel-direction increments passed automated
and native Linux software validation. Guided keyboard software qualification
also passed; rebuilt-image and physical gates remain open for both. The bounded
standard CPU workflow has host, selected native and elapsed-window fixture
evidence. Complete Standard, Extended and Destructive workflows remain
unfinished, and burn-in is a planned profile. The unchecked requirements below
include those broader matrix items;
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
- [x] Implement and host-validate explicit standard console selection with
  matching frozen profile/architecture handoff and plan/session profiles.
  Automatic quick, modern quick deployment-key selection and legacy quick
  behavior remain available; standard requires explicit restoration of actual
  exclusions. The host suite passed 8,236 test/subtest outcomes across 30 packages,
  with 18 test skips and one package skip. Of 326 selected names, 325 passed and
  the permitted Windows symlink fixture skipped.
- [x] Qualify the selected standard-console source on Linux amd64 and 386:
  652 named outcomes across 92 roots and four package executions passed, with
  560 direct-parent relations checked and no missing, extra, duplicate, skipped
  or failed outcomes. All 18 startup-shell fixture methods, nine process-ownership
  cases, 32 archive cases and six final host checks passed. The shell fixtures use
  owned command stubs; they do not establish the actual Standard workflow.
- [x] Qualify the Standard console-to-plan-to-session-to-signed-report workflow
  in native Linux amd64 and 386 VM runs, including cancellation smoke, a full
  600-second CPU load on each architecture, verified self-issued reports and
  confirmed cleanup. Thermal observations were synthetic at 35 C. Qualification
  of all four rebuilt bootable images, physical hardware, the ten-PC pilot and
  release approval remains open.
  See [the console contract](docs/requirements-validation.md#standard-console-launch).
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
  stage to three: deployment discovery and two persistence inputs. That key-input
  work did not cover frozen-plan/evidence readers; see the later
  [small-document reader increment](docs/requirements-validation.md#small-regular-file-input-closure).
  Audit owners and persistence's live source remain separate work.
- [x] Implement checked 32-byte fingerprint and trusted public-key inputs for
  persistence, with fixed input diagnostics and error/shape checks before
  post-read cancellation. Preserve authorization decoding, report verification
  and persistence policy.
- [x] Qualify the persistence key-input boundary with 5,374 host test/subtest
  passes and 444 selected native passes across Linux amd64 and 386; see the
  [scoped validation evidence](docs/requirements-validation.md#persistence-key-input-ownership).
  This implementation reduces the three legacy calls recorded at the save stage
  to one in deployment discovery. Authorization-file and frozen-plan/evidence
  closure were outside that key-input scope and are covered by the later
  [small-document reader increment](docs/requirements-validation.md#small-regular-file-input-closure).
  Audit and live report-source ownership remain separate work.
- [x] Replace the final legacy raw-key input with checked 32-byte discovery-key
  acquisition for deployment-key media loading. Preserve usage/audit precedence,
  media selection, confirmation, token and activation policies; remove the unused
  raw-key reader after its final caller is migrated.
- [x] Validate the deployment discovery-key input boundary with host and selected
  native software fixtures: 5,473 host test/subtest passes and 202 native named
  passes across two suites and 24 root executions. See the
  [validation scope](docs/requirements-validation.md#deployment-discovery-key-input-ownership).
  The one legacy call recorded at the persistence stage is now removed.
  Frozen-plan/evidence reader closure is covered by the later
  [small-document reader increment](docs/requirements-validation.md#small-regular-file-input-closure).
  Audit, token/mount, activation-output and live report-source ownership remain
  separate work.
- [x] Close the separate direct fingerprint-key reads in `inventory` and `plan`
  with checked, exact 32-byte regular-file acquisition and owner closure before
  collection. Preserve usage and plan-selection precedence, opaque key errors,
  cancellation boundaries and existing successful output. These two direct reads
  were outside the completed `readRawKey` migration; incorrect lengths were
  already rejected downstream.
- [x] Validate the inventory/plan increment on Windows: 7,943 test/subtest passes
  across 30 passing packages, with 17 test skips and one package skip. The focused
  and full-suite selections each passed 1,101 named outcomes; the retained
  selection-symlink case was the sole permitted skip. Five candidate outputs
  exactly matched independently checked pre-change inventory/plan bytes.
- [x] Qualify this increment on native Linux amd64/386: each architecture passed
  1,071 named outcomes under 112 roots, including eight actual intermediate
  groups, with no failures, skips, extra or duplicate names and confirmed cleanup.
  See the [implementation, host/native evidence and remaining gates](docs/requirements-validation.md#inventory-plan-fingerprint-input-ownership).
- [x] Require successful input closure before the shared small-document reader
  returns bytes. Cover quick selection, persistence authorization, finalizer and
  session plans, optional evidence, and session journals. Reject read or cleanup
  faults with no returned bytes; preserve the separate Standard launch reader.
- [x] Validate the reader increment on Windows amd64 and 386: 8,303 full-suite
  passes and 3,207 command-package passes respectively. All 67 selected names
  and 59 parent relationships passed on each architecture, with no selected skips.
- [x] Qualify the selected reader fixtures on Linux amd64 and 386 in a VM:
  152 named passes across two suites and 22 test roots, with no failures or skips.
  See the [implementation and limits](docs/requirements-validation.md#small-regular-file-input-closure);
  physical media, kernel-close faults, preemption and durability remain separate.
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
- [x] Validate the implemented opt-in standard CPU stress path with host and
  native fixtures, including a real shared elapsed window on both selected
  Linux architectures:
  explicit matching `session -profile standard`, a 900-second default session,
  a shared 600-second load window and an origin-anchored 660-second watchdog.
  That CPU qualification used Standard v8 with thirteen workers and default
  Quick v7 with twelve. The touchpad activation used Standard v10/fourteen
  and Quick v9/thirteen. Strict stress v1 evidence reaches finalization,
  signed verification,
  HTML/PDF and comparison; a raw command's zero exit means a valid diagnostic,
  not a passing CPU. The full host suite passed, including all 493 required named
  outcomes across 69 selected roots. Selected native fixtures passed 6,040 named
  outcomes across 18 package executions on Linux amd64 and 386, with no failures
  or skips. Separate API-level runs completed a real shared 600-second window on
  both architectures with two workers and fixed synthetic temperature input;
  cleanup was confirmed. Physical, all-core and utilization claims remain open. See [the contract](docs/requirements-validation.md#bounded-standard-cpu-stress).
- [ ] Qualify this bounded stress path on representative physical CPUs and
  sensors, in rebuilt images and the ten-PC pilot. CPU utilization, instruction
  completeness, performance, throttling and physical-health claims remain outside
  this implementation; complete standard, extended and burn-in profiles remain open.

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
- [x] Implement the standalone `hhpt memory-stress` diagnostic in development
  source: one owned 4-64 MiB working set, fresh availability and thermal
  admission, a fixed 1,800-second load request and a 1,860-second watchdog,
  checked cycle accounting, final verification and confirmed cleanup before
  diagnostic output. See [operator guidance](docs/memory-stress.md).
- [x] Keep standalone stress JSON distinct from quick-memory; a diagnostic file
  alone does not supply signed-session authority.
- [x] Validate this standalone addition with 19 host commands: formatting/checks
  for 13 Go files, 196 named fixture passes without skips on each of Windows
  amd64 and 386, and 11,434 full-suite passes with 18 retained platform skips.
  Vet, three Linux application builds and nine Linux test-binary builds passed;
  those host commands compiled the Linux artifacts without executing them.
- [x] Qualify the selected standalone memory fixtures on Linux amd64 and 386:
  392 named passes across 62 root executions and six package executions, with
  all 330 declared-parent intervals checked and no failures or skips. All nine
  ownership cases, 32 archive cases and six postchecks passed; cleanup was
  confirmed. These controlled fixtures do not establish a real 1,800-second load.
- [x] Qualify the standalone producer's real 1,800-second load on Linux amd64
  and 386. Each API fixture used one 4 MiB working set and synthetic availability
  and thermal inputs; final verification and cleanup passed. All nine ownership
  cases, 32 archive cases and six postchecks passed. The earlier failed elapsed
  attempt remains unqualified; this result does not qualify a full Extended
  session or installed CLI.
- [ ] Qualify the full Linux suite, race checks, ARM runtime and the installed
  CLI for this addition; separately qualify physical memory and thermal behavior.
- [x] Complete host validation of the implemented Extended memory route before
  exterior definition 2. That source connects explicit selection, a frozen plan, the real coordinator/finalizer,
  v13 journals and strict signed report/view/comparison consumers. Its fifteen
  workers add memory stress after successful quick-memory checks; CPU stress
  stays Standard-only. Quick v11/fourteen workers and Standard v12/fifteen
  workers with a 900-second budget are unchanged. Extended uses 2,700 seconds
  for work and a 2,750-second outer supervisor policy; these are cooperative
  bounds, not a cleanup guarantee.
- [x] Validate the corrected Extended route on Windows amd64 and 386: each
  focused run passed 242 test/subtest cases across 41 roots. The full Windows
  suite passed 11,641 cases with 18 platform skips; 33 packages passed and one
  had no tests. Formatting, vet, three Linux application builds and twelve
  Linux test-binary builds passed. The Linux artifacts were compiled only;
  they supply no native or race execution evidence.
- [x] Validate the updated startup scripts with two shell syntax checks and
  all 22 mock-fixture methods, including 22 new subcases, without failures or
  skips. The run used the standalone baseline plus the three startup/fixture
  files; no product session or installed supervisor was executed. Ownership,
  archive and all six postchecks passed with cleanup confirmed.
- [ ] Qualify the installed Extended startup supervisors, full-duration session,
  installed CLI/images and physical memory/thermal behavior. Selected native
  session fixtures are qualified below. The development
  catalog has sixteen implemented rows and six inactive families; complete
  profile coverage, the four images, physical-PC and ten-PC pilot, and release
  approval remain open.
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
- [x] Validate the keyboard query-phase and report-value correction with
  Go 1.27.2 on Windows amd64 and 386. Keep the initial neutral/queue-flush
  boundary and continue reading completed keyboards while another is
  unfinished. Post-baseline neutral queries belong only to final proof and
  checked closure; racing terminal input may return inconclusive without
  restarting the window. Ignore undefined `SYN_REPORT` values while retaining
  loss and malformed-transition rejection. Each focused suite passed 345
  test/subtest outcomes, including all 150 selected names.
- [x] Qualify the selected correction fixtures on native Linux amd64 and 386:
  each passed all 150 names across 25 roots and 125 parent intervals, with no
  failures or skips and confirmed cleanup. Nine ownership and 32 archive cases
  also passed. The injected queue model does not establish a deployed-kernel
  race reproduction or physical-keyboard qualification.
- [ ] Qualify the six-control keyboard check in rebuilt images and on physical
  keyboards, including multiple devices, held keys, incomplete actions,
  disconnects and cancellation. Full layouts and rollover remain separate.
- [x] Implement the legacy single-contact touchpad core and Linux adapter,
  first qualified as inactive components: bounded complete-frame observation,
  fixed contact anchors, signed axis thresholds, checked acquisition and
  reverse closure. Keep key-state
  queries at baseline and terminal proof, with no active query.
- [x] Qualify these components with Go 1.27.2 software fixtures. Focused Windows
  amd64/386 suites each passed 541 outcomes, including all 196 selected names;
  the full Windows suite passed 9,148 outcomes with 18 permitted skips and vet.
  Native Linux amd64/386 each passed all 395 selected names across 47 roots,
  with no skips or failures and confirmed cleanup. AArch64 is compile-only.
- [x] Implement finite touchpad candidate inventory and strict functional
  evidence validation, including checked MSC observations, complete capability
  sets, producer-phase diagnostics, cleanup precedence and plan/inventory
  binding. Candidate discovery does not establish device support.
- [x] Qualify the inventory/evidence increment with Go 1.27.2 Windows fixtures:
  each focused amd64/386 suite passed 2,332 outcomes with one platform skip and
  all 686 selected names; the full suite passed 9,509 outcomes with 18 permitted
  skips, and vet passed. Linux/native qualification for this increment is open.
- [x] Qualify the standalone command with Go 1.27.2 Windows fixtures: each
  focused amd64/386 suite passed 5,647 outcomes with seven permitted skips and
  all 108 CLI names; the full suite passed 9,617 outcomes with 18 permitted
  skips, and vet passed. This adds no Linux/native or real-device evidence.
- [x] Implement and host-qualify v9/v10 session evidence, checked worker
  dispatch and owned-file finalization before catalog activation. Fixtures cover
  original-key HMAC binding, publication/duration checks and cancellation/cleanup
  precedence without enabling the catalog. Go 1.27.2 focused Windows amd64/386
  runs each passed 6,277 outcomes with seven permitted skips; the full suite
  passed 9,982 with 18 permitted skips, and vet passed. All 151 session and 214
  command names passed in all three runs.
- [x] Implement app validation, signed-bundle checks and bounded
  HTML/PDF touchpad views. Candidate/integrity consistency, cleanup uncertainty
  and copied public data retain explicit checks and historical report handling.
- [x] Qualify the pre-activation app/report/render fixtures on Windows with
  Go 1.27.2.
  Focused amd64/386 runs and the full amd64 suite each passed all 260 selected
  names across 25 roots; vet passed.
- [x] Activate touchpad definition 2 through current catalog and frozen-plan
  admission, Quick defaults/menu selection, checked dispatch and owned
  finalization. That touchpad activation used Quick v9 with thirteen workers
  and Standard v10 with fourteen.
  Candidate discovery remains distinct from supported-device evidence.
- [x] Validate the connected route with Go 1.27.2 Windows fixtures. Focused
  amd64/386 runs each passed 4,838 outcomes with six permitted skips; the full
  amd64 suite passed 10,341 with 18 permitted test skips. All 583 activation
  names across 73 roots passed in all three runs, and vet passed.
- [ ] Complete Linux/native validation of the later integrated route, real
  evdev/kernel evidence, broader touch and gesture coverage, rebuilt images,
  physical devices, the pilot and release gates. See
[scope](docs/requirements-validation.md#touchpad-inventory-and-strict-evidence).
- [x] Implement bounded single-contact touchscreen observation and candidate
  discovery. Require fresh X/Y coordinates, both directions on each axis within
  one released contact, terminal checks and confirmed cleanup. Candidate
  discovery and generic activity do not establish functional support.
- [x] Qualify the producer/discovery slice with Windows amd64 and 386 software
  fixtures: each focused run passed 2,250 test/subtest outcomes with one existing
  platform skip. Six Linux test builds cover amd64, 386 and arm64; these builds
  provide compile-only evidence.
- [x] Qualify the complete touchscreen command, current plan/menu, session,
  owned finalization and signed report/view route with Windows software tests.
  Quick v11 has fourteen workers and Standard v12 has fifteen. Focused amd64
  and 386 runs each passed 8,369 test/subtest outcomes with seven platform skips;
  the full suite passed 11,238 with eighteen skips. All 794 new named checks
  passed in each run. Formatting, vet and twelve Linux command/test builds
  passed; those builds initially provided compile-only evidence.
- [x] Validate selected touchscreen fixtures on native Linux amd64 and 386:
  2,952 named passes across six package/architecture runs and 344 root
  executions, with no skips or failures. Nine ownership cases, 32 archive cases
  and all six postchecks passed, with cleanup confirmed.
- [ ] Validate real evdev/device behavior, installed commands, ARM runtime and
  race checks, including incomplete actions, cancellation and disconnects.
  Multi-touch, stylus, pressure, calibration and whole-panel coverage remain
  outside this bounded check. Rebuilt images, physical devices, the ten-PC
  pilot and release gates remain open; see
  [touchscreen scope](docs/touchscreen-quick.md).
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
  without silently truncating the device list. Close handles before admitting
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
- [x] Bound aggregate inventory-directory admission to 65,536 records and
  8 MiB of name bytes per collection, with fresh counters for each call.
  Exhaustion returns no partial snapshot; exact-limit EOF, checked closure
  and declared source overrides remain mandatory. These limits bound owned
  retention, not callback allocations or blocked kernel calls.
- [x] Validate the aggregate directory-budget increment on Windows amd64 and
  386, including all 39 new names. The full Windows suite, vet and Linux amd64,
  386 and AArch64 production builds passed. Native runtime, image and physical
  gates remain open.
- [x] Implement and host-test cumulative scalar/proc acquisition budgets. The collector
  shares 65,536 file attempts and 64 MiB of raw bytes per collection. Missing,
  empty and repeated attempts count, as do discarded bytes and bytes returned
  with an error. The owned reader may consume one unretained detection byte;
  exhaustion returns a fixed error and no partial snapshot. Per-file limits,
  checked closure and declared source overrides remain mandatory.
  All 61 new names passed on Windows amd64 and 386 and in the full Windows
  suite. Vet and Linux amd64, 386 and AArch64 production builds passed.
  Native runtime, image, physical-device and release gates remain open.
- [x] Implement shared proc parser budgets for CPU, memory and input discovery:
  65,536 raw record blocks, 262,144 units of visited line/word work and 16 MiB
  of cumulative retained-text accounting per collection. Empty, malformed,
  duplicate and overwritten work keeps its charges. Reserve before selected
  record, field, alias and first component allocations; reset each collection.
  Exhaustion returns a fixed error and no partial snapshot.
- [x] Validate the proc parser-budget increment with complete inventory-package
  tests on Windows amd64 and 386: each passed 1,673 cases with one retained
  platform skip, including all 48 new names without skips. All nine commands
  passed: five-file formatting/checks, both package suites, vet and three Linux
  amd64/386/AArch64 test-binary builds. The Linux binaries were compiled only.
- [x] Validate the combined Extended memory and inventory source with three
  host commands. The full Windows amd64 suite passed 11,689 cases with 18
  retained platform skips; the Windows 386 affected-family suite passed all
  290 names across 55 roots and 235 declared-parent intervals in five packages.
  Vet and all seven postchecks passed.
- [x] Qualify the selected combined Extended memory and proc parser fixtures on
  Linux amd64 and 386: 580 named outcomes across 110 roots, 470 declared-parent
  intervals and ten package executions passed without failures or skips.
  All nine ownership cases, 32 archive cases and six postchecks passed;
  cleanup was confirmed. This covers the connected fixture paths, with
  installed commands/supervisors, ARM runtime, the full Linux suite, race checks
  and a full elapsed Extended session still open.
- [x] Remove temporary token arrays from input handler and capability scans.
  Preserve Unicode word boundaries, 32/64-bit bitmap ordering, selected-word
  parsing and existing keyboard, pointer and touchscreen discovery decisions.
- [x] Validate input token iteration on Windows amd64 and 386: each passed
  4,209 affected-package and caller test/subtest cases with one retained
  platform skip, including all 113 new names without skips. All 14 commands
  passed, including formatting, vet and six Linux amd64/386/AArch64 application
  and inventory-test builds. Those Linux artifacts were compiled only.
- [x] Remove temporary word arrays from inventory value normalization.
  Preserve Unicode whitespace, raw non-whitespace bytes and fingerprint inputs.
  Empty and single-word results need no allocation; multiword results own a
  buffer sized to the normalized value. Large-padding regressions pass.
- [x] Validate normalization across Windows amd64 and 386: 12,053 test/subtest
  passes and 18 retained skips per architecture, including all 49 new names.
  The full amd64 run passed. The initial full 386 run reached its CLI package
  time limit; completed other packages plus a fresh complete CLI run provide
  the 386 coverage. Formatting, vet and six Linux application/inventory-test
  builds passed. Linux artifacts were compiled only.
- [x] Add a same-pass, fixed-size diagnostic companion for optional inventory
  file reads. Only `hhpt inventory` consumes it, as one bounded stderr warning
  with scalar, CPU, memory and input failure counts. Clean absence and completed
  empty reads remain quiet; paths and provider error text are excluded. Schema-1
  snapshots, component identities and fingerprint/correction inputs are retained.
- [x] Validate the observation-diagnostic increment on Windows amd64 and 386:
  12,152 test/subtest passes and the same 18 retained skips per architecture,
  including all 99 new names. Regressions cover error classification without
  provider callbacks, count limits, fatal/closure/cancellation precedence,
  fresh-call reset, 325 instances and CLI output compatibility. Formatting, vet
  and six Linux application/inventory-test builds passed; these Linux artifacts
  were compiled only. Linux runtime and physical-device validation remain open.
- [ ] Bound remaining parser expansion, aggregate/peak allocation and parser
  CPU time; add field-specific diagnostics and warning coverage for other
  inventory/report consumers. Logical parser and
  acquisition budgets do not establish a whole-heap bound or kernel preemption.
  Legacy providers may allocate before returning, and cancellation cannot
  interrupt a blocked call. Broader Linux runtime, rebuilt-image, physical-device,
  pilot and release gates for this increment remain open.
- [ ] Qualify storage readiness, replacement and cancellation in rebuilt images
  and on physical devices. Matching kernel numbers do not prove persistent
  physical identity or detect every same-number replacement.
- [x] Implement the inactive storage sampling result reader with complete typed
  topology/fixture reconstruction, bounded counters and retained failure and
  cleanup semantics. Evidence contains no media data or content fingerprint.
- [x] Add explicit v6 session read support through signed-report verification,
  HTML/PDF rendering and comparison, including bundles without optional views.
  Preserve historical v4/v5 wire behavior. That reader increment retained the
  v5 emitter. Its later qualification used v7 sessions with twelve workers;
  the storage sampling catalog entry remains inactive.
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
- [x] Implement isolated storage sampling and discovery components: finite read
  schedules, strict worker replies, parent accounting, result projection, bounded
  topology/mount/swap parsing, owned metadata capture, held metadata scope and
  canonical-fact classification. These components do not yet form a live sampler.
- [x] Validate the earlier 27-file component candidate on Windows amd64 and 386: 773
  test/subtest passes across two packages on each target, including all 495
  selected names under 68 roots and 427 direct parent relationships. No failures
  or skips occurred. Formatting, vet, six Linux test-binary cross-builds and all
  seven postchecks passed. The arm64 builds remain compile-only evidence.
- [x] Qualify the selected component and owned-path fixtures on Linux amd64 and
  386: 505 passes under 70 roots and 435 direct parent relationships per
  architecture across two packages, or 1,010 passes across four suite runs.
  Nine ownership fixtures, 32 archive checks and all six final checks passed;
  cleanup and transfer EOF were confirmed. These runs sampled no real media.
- [x] Connect canonical-ancestor acquisition to the held metadata scope and
  classifier. Admit finite class/subsystem links, retain directory owners and
  recheck identities and facts before returning a closed result. Classification
  remains descriptive and leaves original boot backing unknown.
- [x] Validate the current 31-file successor on Windows amd64 and 386: 848
  test/subtest passes across two packages and all 119 package roots per target,
  including exactly 570 selected names under 76 roots and 494 direct parent
  relationships. No failures or skips; focused vet, six Linux test-binary
  cross-builds and all seven postchecks passed. The module minimum is Go 1.25.0;
  these checks used Go 1.27.2 and do not qualify execution on Go 1.25.
- [x] Pass the full-module suite and vet on Windows amd64 with Go 1.27.2:
  8,873 test/subtest passes, 18 allowed skips, 31 package passes and one package
  without tests. All 32 packages, 1,349 required roots and the exact selected
  570-name cohort were reconciled.
- [x] Run the current selected Linux cohort on amd64 and 386: 667 named outcomes
  under 91 roots and 576 direct parent relationships per architecture, or 1,334
  passes across four package runs. No failures or skips; nine ownership
  fixtures, 32 archive checks and six final checks passed, with cleanup and
  transfer EOF confirmed. Linux arm64 remains compile-only; these owned-fixture
  runs do not qualify the Linux full-module suite or real media.
- [ ] Implement protected early boot-source production and consumption.
  Unknown original boot backing must not become an empty exclusion set.
- [ ] Implement and qualify continuous target/backing ownership, the supervised
  media worker and checked CLI/result composition before catalog activation.
  Repeated metadata is not a lifetime guarantee; loops remain rejected.
  All four rebuilt images, physical media, the physical-PC pilot and release
  approval remain separate gates.
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
- [ ] Extend RAM coverage beyond the bounded Quick and Extended working sets.
- [x] Implement finite Linux SMBIOS Type 17 memory-device inventory: capacity,
  memory type and form factor with explicit field states, private source strings,
  unavailable identity and ordinary graph/report/comparison integration.
  See [the bounded contract](docs/requirements-validation.md#memory-device-inventory).
- [x] Qualify this finite memory-device implementation with the full host suite
  and selected native Linux amd64/386 fixtures, including actual 32-bit capacity
  boundaries, owned acquisition failures and signed consumers. The host recorded
  7,639 named test/subtest passes and 30 package passes, with 17 test skips and
  one package skip. Selected native runs recorded 5,268 named passes across
  14 package executions, with no skips or failures. Physical and release
  qualification remain separate.
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
- [x] Implement finite Linux power-supply inventory: eleven fields with explicit
  reported, unavailable, unsupported, malformed or out-of-range states. Use
  nonpersistent ordinal rows with unavailable identity and no raw supply names
  or serial export; missing or empty supply enumeration reports no data.
- [x] Qualify the integrated power-supply collector, graph policy, signed reports,
  views and comparison: the full host suite passed 6,239 test/subtest outcomes
  across 30 packages; selected native Linux amd64/386 fixtures passed 3,100
  named outcomes across 14 suites and 582 root executions, with no failures
  or skips and confirmed cleanup. The implementation
  adds no test worker and does not qualify battery health or power transitions.
  See [the bounded contract](docs/requirements-validation.md#power-supply-inventory).
- [ ] Collect adapter/battery health and charge telemetry and test AC/battery
  transitions where supported.
- [ ] Monitor temperatures, fans, clocks, power, and throttling throughout a run
  and test cooling response under sustained load.
- [x] Implement finite Linux firmware/TPM inventory: UEFI platform width,
  independent SecureBoot and SetupMode flags, and enumerated TPM interface major
  versions, with reported, unavailable, malformed or unsupported states. Keep
  raw names private, identity unavailable and finite observations bound to the
  configuration graph; no firmware changes or key provisioning.
- [x] Qualify the integrated firmware collector, graph, signed reports, views
  and comparison: 6,948 host test/subtest passes across 30 packages, including
  all 31 new roots and 216 names. Selected native Linux amd64/386 fixtures
  passed 3,886 named outcomes across 14 suites and 736 root executions, with
  no failures or skips and confirmed cleanup. Physical firmware behavior and
  broader security features remain unqualified; see
  [the finite contract](docs/requirements-validation.md#firmware-and-tpm-inventory).
- [ ] Implement measured-boot verification, firmware-lock capability reporting
  and broader firmware-security assessment without changing firmware state or
  provisioning keys. Finite inventory does not establish a security verdict,
  attestation, TPM health or physical firmware qualification.
- [ ] Test PCIe, Thunderbolt, and dock topology, link properties, and controlled
  hot-plug behavior with appropriate fixtures.
- [ ] Measure RTC/monotonic discontinuities and drift against a trusted reference
  over a sufficient observation period.
- [x] Implement the historical v1 bounded operator-reported exterior-enclosure observation,
  without moving or opening the device. Finite D/N/I responses, incomplete
  input and retained defects reach planning, the session coordinator,
  finalization, signed verification, HTML/PDF views and linked comparison.
  That qualification used v7 sessions with twelve workers; the catalog has
  22 test rows.
- [x] Validate the complete historical v1 exterior-inspection source with the full host suite:
  5,942 test/subtest passes across 30 packages, 17 platform test skips and one
  skipped package. Formatting, vet, Linux amd64/386/arm64 application builds
  and all eight postchecks passed.
- [x] Qualify the selected historical v1 exterior-inspection and regression fixtures on native
  Linux amd64/386: 4,580 named passes across 16 suites and 836 root executions,
  with no failures or skips and confirmed cleanup. Each architecture passed
  2,290 names and 418 roots, including the selected Linux-only cases. See
  [validation details](docs/requirements-validation.md#bounded-exterior-inspection).
  The broader inspection, ten-PC pilot, four-image and release gates remain open.

- [x] Implement four fixed operator-reported visual criteria for enclosure
  integrity, hinge/mount condition, port exterior condition and surface finish.
  Each distinguishes unobserved, no issue observed and issue present. Only the
  hinge accepts explicit not-applicable when no hinge exists; hidden or
  uninspected hinges remain unobserved. One owned four-answer batch shares a
  45-second bound, with no free text or operator identity.
- [x] Connect exterior definition 2 through the real command, current planning,
  menu/coordinator, strict finalization and signed HTML/PDF/report consumers.
  Quick v14, Standard v15 and Extended v16 retain fourteen/fifteen/fifteen
  workers. Preserve historical v1 evidence and v11/v12/v13 journal authority.
- [x] Validate the connected four-criterion source on the host: 11,891 full
  Windows amd64 test/subtest passes with 18 retained platform skips, and 593
  focused Windows 386 passes with no skips. Formatting, vet, 21 Linux
  compile-only builds and all seven postchecks passed.
- [x] Validate selected current inspection fixtures on Linux amd64/386:
  1,296 passes across 12 streams, with no skips or failures, covering 162 root
  executions and 1,134 declared parent intervals. Nine ownership cases,
  32 archive cases, six postchecks and cleanup passed. One separate synthetic
  standalone inspection CLI exercise passed. Installed CLI and physical
  qualification remain open; the earlier v1 and 580-outcome native evidence
  retain their historical scope.
- [ ] Complete broader physical findings and validation for hinge movement,
  functional ports, hidden damage, cooling noise and battery swelling. Visual
  operator assertions do not establish automated hardware passes.
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
- [x] Implement standalone signed display correction addenda with connected
  `correction create`, `verify`, and `view` commands for four public System
  fields. A separate comparison preserves original observations, identifiers,
  fingerprints, and test evidence, with exact report binding and independent
  signature trust. See [signed display corrections](docs/corrections.md).
- [x] Validate signed display corrections on Windows amd64 and 386: full
  suites each passed 12,300 tests/subtests with 18 retained skips, including
  all 148 initial correction names. Five additional parser-budget cases passed
  separately on each architecture without production changes. Formatting,
  vet, and Linux amd64/386/AArch64 application and updated correction-test
  builds passed; Linux artifacts were compiled only.
- [ ] Qualify corrections on Linux at runtime and in installed images; complete
  physical-PC pilot and release gates.
- [ ] Add complete signed correction history beyond one selected addendum,
  separate from retesting; preserve finalized originals and distinguish
  correction, supersession, revocation and custody semantics.
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

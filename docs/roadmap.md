# Roadmap

The 2026-10-24 requirements audit is reflected in the
[master checklist](../PROJECT_CHECKLIST.md). The current automatic workflow is
the bounded safe quick profile. Broader profile names in the planner do not
establish complete execution coverage; burn-in remains planned. The initial
alpha requires its release gates, while full matrix coverage remains later work.

Next work follows the existing dependencies:

1. Complete the current alpha's source/image inspection, source and license
   packaging, authenticated evidence, and approved publication checks.
2. Expand profile orchestration beyond the safe quick-session coordinator,
   adding configuration and continuous run-wide monitoring around the frozen
   plan and signed evidence paths, retaining explicit unsupported results.
3. Implement the remaining applicable functional tests with independent
   failure-case coverage for each adapter. Record physical validation separately
   from implementation and VM checks.

## Working checklist (2026-10-16)

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
- [x] Exercise the real removable-USB save path in QEMU, proving zero writes
  after incorrect confirmation and independently verifying the saved artifact.
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
- Reuse dedicated scratch storage for file-backed VMs and preserve test evidence.
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

The CPU/RAM hwmon guard, frozen-plan/report validation, operator-selected
quick-test execution, complete frozen-catalog reporting, bounded worker cleanup,
quick-session deadlines/cancellation, and signed runtime safety history are
implemented and covered by automated failure tests.
The selection flow also passes native menu-to-signed-report verification for
modern x64/x86 and legacy x86 applications. Broader profile orchestration,
run-wide watchdogs, rebuilt-image qualification, the remaining functional
adapters, and the physical pilot below are still open.

The original guided-pointer increment adds relative movement and primary/advertised
right/middle button cycles per interface. It expanded quick selection to ten
modern and eight legacy choices and introduced ten outcomes in session journal v2.
Absolute or mixed interfaces and the rest of the input matrix remain outside
this bounded check. The catalog change requires fresh plans while preserving
historical signed-report verification. Portable and native Linux tests, race
checks, cross-builds, and signed CLI workflows pass for this increment.
Rebuilt-image and physical qualification remain open; earlier image results do
not qualify the new worker.

Pointer definition 2 now adds both directions of each advertised wheel axis.
It preserves standalone finalization and historical pointer-v1 report scope;
current sessions keep the eleven-worker v5 journal. Windows and native Linux
amd64/386 suites, race checks, and standalone and signed-session CLI checks passed
for this extension. Fresh images and physical wheel devices remain separate
gates; wheel precision, extra buttons and touch gestures remain planned.

The guided-pointer acquisition increment bounds registry and selected identity
reads, verifies metadata before readable event access, and checks every owned
close. Hard acquisition, protocol, identity and cleanup errors survive later
cancellation. Complete observations remain diagnostic; a device pass also needs
its own idle queue and final identity proof. The unused shared cleanup option is
removed by preserving its reachable behavior for keyboard callers. Pointer
schema, catalog, movement/button/wheel criteria and session policy are unchanged.

The full host suite, formatting, vet and Linux amd64/386/arm64 cross-builds
passed for this increment. All 75 selected host roots and their 320 outcomes
passed. Selected Linux amd64/386 fixtures passed 848 test/subtest outcomes
across four suite runs and all 194 required root executions, with no failures or
skips and confirmed cleanup. Broader pointer/keyboard functions, aggregate
allocation and blocked-kernel deadlines, rebuilt images, physical devices and
release gates remain open.

The periodic safety increment implements read-only mount/network checks
through quick execution and the final safety observation. One owned observer
runs serialized samples at a one-second interval, with a one-second observation
deadline, a 2,500-millisecond maximum gap, and a two-second join allowance.
Its first unsafe, unknown, late, or excessively delayed observation cancels
the session and prevents signing. Journal v3 adds the bounded coverage summary
without changing the catalog or ten worker outcomes. Automated and native
Linux suites, race checks, cross-builds, and 15 signed CLI scenarios passed.
Rebuilt-image and physical gates remain open. Sampling does not establish
continuous coverage, thermal or heartbeat monitoring, or preemption of a
blocked kernel operation. The remaining functional adapters and beta/release
requirements below still apply.

The safemode acquisition increment adds bounded metadata and registry
reads, exact completion, contained descriptor identity checks and confirmed
owned-handle closure. Known writable-mount or active-interface counts remain
failures after later errors or cancellation; incomplete observations never
certify safety. It preserves runtime-safety v1, the shared mount parser, catalog
and current session policy. Host validation and three Linux cross-builds have
passed, including all 50 required host roots. Selected Linux amd64/386 fixtures
also passed 640 test/subtest outcomes and all 108 required root executions.
It adds
no device-data access or network traffic. Blocked-kernel deadlines, aggregate
allocation, continuous safety, rebuilt images, physical-PC and release gates
remain open, as do the later functional and profile work below.

The shared mount parser rejects conflicting filesystem types for one canonical
zero-major device while retaining valid same-type aliases, diskless views and
previously observed writable block counts. Focused and full Windows suites,
vet and cross-builds passed, along with native Linux amd64/386 suites, race
checks and all 39 CLI cases. The existing runtime-safety/readiness v1 evidence,
catalog and session versions are unchanged. Sampled storage reads remain
planned; rebuilt-image and physical-device qualification remain open.

Storage readiness now enumerates the block registry with bounded requests
through actual EOF and closes the directory before processing identities. The
existing 256-entry limit still counts excluded interfaces and partitions;
overflow fails without truncation. Lexical error order, diskless views and normal
sysfs links are preserved. Host and native fixture validation passed; see
[requirements validation](requirements-validation.md).

Storage scalar admission now requires valid size metadata for supported
non-partition media and bounds size, flag and state reads to 256 raw bytes after
EOF and successful closure. Valid zero capacity and optional unknown values
retain their existing meanings. Cancellation is rechecked before metadata
credit and the no-media conclusion; required-capacity and cleanup failures
retain precedence. Focused/full host checks and native Linux amd64/386 fixture
qualification passed. Broader inventory completeness, error reporting and
acquisition bounds remain open, along with sampled-media reads, fresh images
and physical-device qualification.

Inventory enumeration now consumes production directory streams through EOF
without a total device-count cutoff, closes handles before admission and reports
hard directory failures instead of successful partial snapshots. Per-call state,
owned slices and cancellation checks cover acquisition, processing and final
snapshot creation. Inventory, plan, capture and finalize reject collection errors
before their later output or report work; legitimate missing interfaces and
diskless operation remain supported.

Focused Windows regressions, formatting and vet passed, including all 29 new
portable test functions. Full host validation also passed: 3,047 test/subtest events
across 25 packages, 28 cross-built test binaries and five application targets.
Native validation passed 5,816 test/subtest events across 28 suites, 2,106 race-test
events across seven suites and all 39 CLI cases, with no skips or failures and
confirmed cleanup. All 31 new functions, including both Linux-only tests, passed
on amd64 and 386 and in their corresponding inventory or command race suites;
see [requirements validation](requirements-validation.md).

Inventory scalar acquisition now applies a 64 KiB raw-value budget and one-byte
probe to every declared attribute and direct scalar role, covered through 105
category/role cases. The owned reader requires a regular file, exact EOF and
successful closure; protocol,
overflow and cleanup failures stop collection. Ordinary unavailable observations
remain unknown, decorators keep their declared ReadFile behavior, and there is
no device-count cutoff. Focused Windows validation passed 2,215 test/subtest
events across eight packages, including all 26 new portable test functions.
Full host validation passed 3,362 test/subtest events across 25 packages, with
17 platform test skips, three packages without tests and no failures. Vet,
28 Linux test-binary builds and five application targets passed; the host artifact
privacy check passed all 487 files with zero findings.

Native Linux qualification passed 6,468 test/subtest events across 28 suites,
2,432 race-test events across seven suites and all 39 CLI cases. There were
no native/race failures or skips, and cleanup was confirmed. All 31 scalar
and DRM test functions, including five Linux-only functions, passed on
amd64 and 386 and in the corresponding race suites. The native artifact
privacy check passed all 584 files with zero findings.

DRM collection now separates connector status fields from controller and render
PCI fields. This avoids treating nested device-link directories as scalar values
while retaining strict type checks on the attributes each category owns.

Primary proc acquisition now bounds memory records to 256 KiB, CPU records
to 16 MiB and input-device records to 8 MiB. The owned reader requires a
regular file, exact EOF and successful closure, with one extra probe byte.
Buffers grow from a small initial allocation; decorator dispatch is
preserved. Focused validation of this later increment passed 2,309
test/subtest events across eight packages, with six platform skips and
no failures. Formatting, vet and all 15 new portable functions passed,
including 36 real command cases and four composed closure cases. The
407-file privacy check passed with zero findings.

Full host validation of the proc increment passed 3,456 test/subtest events
across 25 packages, with 17 platform test skips, three packages without tests
and no failures. Formatting and vet passed; 28 Linux test binaries and five
application targets built successfully. All 15 new portable functions passed,
including the 36 real command cases and four composed closure cases. The host
privacy check passed all 493 files with zero findings.

Native Linux qualification of the proc increment passed 6,660 test/subtest
events across 28 suites, 2,528 race-test events across seven suites and all
39 CLI cases. All 17 new functions, including two Linux-only functions,
passed on both amd64 and 386 and in their corresponding race suites.
There were no native/race failures or skips; cleanup and all six root
postchecks passed. The native artifact privacy check passed all 590
files with zero findings.

Total acquisition, aggregate/peak allocation, parser CPU budgets and optional
field-error visibility remain open. Legacy aggregate providers may allocate
before returning, and cancellation cannot interrupt a blocked source call.
Inactive catalog entries remain inactive. Rebuilt images, physical devices,
sampled-media reads, the physical pilot and release approval remain separate
gates.

The inactive storage result reader now checks complete typed topology and fixture
facts, finite read observations and accepted versus diagnostic counters. Explicit
v6 session read support reaches signed-report verification, HTML/PDF rendering
and comparison, including bundles without views. Historical v4/v5 handling stays
supported. Current session emission remains v5 with eleven workers, and the
storage sampling catalog entry remains inactive.

The preceding admission and observation model passed 140 Windows test/subtest
events across 30 test functions, with no skips; formatting and vet passed. Those
fixture checks cover that model cohort only. The integrated reader then passed
3,776 Windows test/subtest events across 27 packages, including all 67 candidate
test functions. Formatting, vet and Linux amd64/386/arm64 cross-builds passed.
The same reader source subsequently passed 1,582 native Linux amd64/386
test/subtest events across 14 suite runs, all 67 candidate roots on each
architecture and 62 verify/compare CLI cases, without failures or skips.
Cleanup and all six final checks passed. That native cohort predates
the separate CLI output-completion change. The media worker, supervised
acceptance and complete backing-device safeguards remain planned.
Rebuilt images, physical media and release qualification remain open;
these fixtures accessed no media.

CLI evidence outputs now propagate write, short-write and owned-file close
failures, including simultaneous errors. Exclusive creation and caller-owned
stdout are preserved; uncertain worker cleanup keeps exit status 12, and
finalization announces success only after output close. The isolated
candidate passed 3,878 Windows test/subtest events across 27 packages, all
nine new test functions, formatting, vet and three Linux architecture
builds. This change does not add fsync or remove partial output files.

Selected native output-fault qualification subsequently passed 204
test/subtest outcomes across two Linux amd64/386 suites: all nine roots
and 93 named children on each architecture, with no failures or skips.
All 18 root executions, nine runtime ownership cases, 32 archive checks
and six final checks passed with cleanup confirmed. The tests use injected
writers and closers plus controlled temporary-file fixtures, including Linux
mode-0600 permissions. They qualify the selected error and ownership behavior.
Kernel-close and fsync faults and installed-CLI execution are outside this
fixture evidence. The existing durability, rebuilt-image, physical-PC and
release qualification gates remain open.

The next thermal prerequisite is implemented in source: local CPU/RAM guards
bound the initial and periodic observations, join before returning results, and
record the coverage first introduced in CPU/memory v2 evidence. Unconfirmed cleanup blocks
later work and signing; observed calculation or memory failures are preserved.
Windows tests, vet, cross-builds, native Linux suites, race checks, and
command-line workflows passed. Rebuilt-image and physical-sensor qualification
remain open. That local-guard increment did not add a catalog row or change
journal v3. Its 250-millisecond sampling wait remains separate from the
run-wide observer below. Heartbeat/hang supervision, privilege separation, and
the physical pilot remain subsequent work.

The shared periodic observer engine is now extracted in source, with the
existing safemode API and v1 evidence preserved. Focused checks passed 217 test
events, including compatibility with six wire cases executed before extraction.
Full Windows tests, vet, cross-builds, native Linux suites, race checks and all
15 CLI scenarios passed with confirmed cleanup. Those results qualify the
extraction, not subsequent changes to its lifecycle or report integration.

Run-wide sampled thermal monitoring is now implemented in development source
for the safe quick session. It observes immediately, repeats at a one-second
interval, and requires a final observation after inventory preparation. Even an
all-off selection gets initial and final observations. Both session monitors
remain active through execution, cleanup, inventory and the final safety gate;
a one-use in-process handoff requires both joins before key generation or
signing. Journal v4 adds typed coverage, timing and local CPU/RAM source bindings
without changing the catalog. Heat cancels work while monitoring continues;
monitoring failure or an unconfirmed join blocks signing. Unavailable telemetry
remains a limitation, not a thermal pass. Full Windows tests, vet and cross-builds
passed. Native Linux amd64/386 suites passed 3,032 test/subtest events and four
race suites passed 1,203, with no skips; all 15 CLI scenarios passed with confirmed
cleanup. The twelve session scenarios reported unavailable temperature telemetry.
Rebuilt images, physical sensor validation, the beta pilot and release gates
remain open. See [requirements validation](requirements-validation.md).

Guided keyboard source now implements six fixed controls on each evdev keyboard,
with no typed text or raw event stream retained. It activates the existing row
as definition version 2 and uses an eleven-worker v5 journal while preserving
strict v4 verification. Windows tests, vet and cross-builds, native Linux
amd64/386 suites, race checks and command-line workflows passed. Rebuilding and
qualifying all four beta images and testing physical keyboards remain open.
Layout-aware coverage, other keys, rollover and ghosting remain planned. The
ten-PC beta pilot and the initial alpha's separate release gates remain open;
this increment does not expand the frozen alpha candidate's scope.

The keyboard acquisition increment bounds registry and selected identity reads
and establishes metadata eligibility before readable event access. Every acquired
handle has checked closure. Hard faults and cleanup uncertainty survive later
cancellation, while clean interruption remains distinct. Earlier completed rows
retain pass only after final idle, neutral and identity proof; bounded metadata
checks for the already-selected interface may finish that proof after a stop
without opening another event device or crediting new actions. The existing
six-control criteria, functional evidence v1, definition 2, cleanup exit 12 and
session v5 policy remain unchanged.

The full host suite, formatting, vet and Linux amd64/386/arm64 cross-builds
passed, including all 86 selected host roots and 385 test/subtest outcomes.
Selected native Linux amd64/386 fixtures passed 1,196 outcomes across four
suites and all 238 required root executions, with no failures or skips and
confirmed cleanup. The acquisition cohort adds no standalone CLI or race run.
Physical key actions, broader layouts and rollover, aggregate allocation,
blocked-kernel deadlines, rebuilt images, the ten-PC pilot and release gates
remain open; see [requirements validation](requirements-validation.md).

The CPU worker now adds twelve fixed scalar binary64 operations per suite, with
per-worker counts and exact mismatch bits. Definition 2 uses CPU v3 evidence in
the existing eleven-worker v5 session; CPU v2 evidence keeps its historical
definition and typed hash. Focused tests cover arithmetic, stop boundaries and
signed consumers. Full host and native Linux validation passed, including race
checks and standalone/session CLI paths. Rebuilt images and physical CPU checks
for this increment remain open. Legacy soft-float coverage is software
arithmetic only. Wider ISA, compression, sustained stress, performance, power and
throttling checks remain planned.

The RAM worker adds a fixed moving-inversion sequence after its three existing
patterns. Memory v3 records completed phases and passes, accepted and uncredited
logical work, and an explicit mismatch, including offset zero. Definition 2
uses the existing eleven-worker v5 journal; historical memory v2 keeps its typed
hash. Full Windows tests, vet and cross-builds passed; compiled x86 inspection
confirmed buffer accesses and reverse indexing. Native Linux suites, race tests
and signed direct, standalone and session workflows passed. Rebuilt images,
physical memory, broader working sets, SPD/ECC, extended stress and Memtest86+
remain open. The frozen alpha scope is unchanged.

The audio readiness acquisition increment checks closure of its owned root,
registry and attribute handles, bounds metadata reads and retains earlier
diagnostics after later faults. It preserves root containment and inspects
device-node metadata without opening playback or capture streams. The result
schema, catalog and eleven-worker v5 session remain unchanged: audio errors
remain reportable, while aborted work follows the existing session stop policy.
The full Windows suite, twelve new portable functions, vet, formatting and
three Linux builds passed. Selected Linux amd64 and 386 fixtures also passed:
438 test/subtest events across four suites, including all 82 required function
executions, with no failures or skips. This is fixture evidence, not physical
audio qualification.
Functional playback, capture, channels, volume, jack behavior, rebuilt images
and physical audio hardware remain open. Subsequent input, safe-mode and
pointer work retains its own implementation and validation gates; this
increment does not complete those paths or alter the frozen alpha scope.

The input-readiness acquisition candidate bounds registry and selected identity
reads, verifies metadata before readable node access, and closes every owned
handle before accepting readiness. Bounded activity uses those same descriptors
and retains aggregate counters only. Hard acquisition or close errors survive
later cancellation; completed earlier rows remain diagnostic without an overall
pass. The existing schema, catalog and eleven-worker v5 session policy remain.
Implementation, host validation and selected Linux amd64/386 fixture execution
are complete. The host suite records 4,205 passing test/subtest events; the
selected native cohort records 364 across four suites and all 70 required root
executions, without failures or skips. These results do not complete guided
keyboard or pointer coverage, physical input qualification, safe display-mode
selection, blocked-kernel deadlines, rebuilt images or release gates.

Opt-in linked retests create a new signed report with a distinct UUIDv4 and
an exact reference to one independently verified predecessor: both its
manifest digest and canonical bundle SHA-256. Session/finalize preserve
current evidence and the existing live safety gates. Verify can check the
child alone, with the predecessor relationship explicitly unchecked, or
match one supplied parent under separate trust choices. This does not
establish hardware continuity, chronology, a complete chain or inherited
outcomes. Historical v1 reports remain readable; correction history,
revocation and custody remain future work. Image, physical-PC and release
qualification remain separate from implementation and software tests.
The linked-profile increment passed its isolated Windows suite and selected
native Linux amd64/386 fixtures; the [validation record](requirements-validation.md#linked-retest-contract)
keeps their measured scope distinct from these remaining gates.

The comparison input increment now uses checked owned artifact/public-key
acquisition, propagates cancellation and contains input diagnostics while
preserving trust defaults, HMAC policy and output behavior. Its isolated host
suite passed 4,875 test/subtest outcomes; the selected Linux amd64/386 cohort
passed 938 across eight suites and 120 required root executions. The
[validation record](requirements-validation.md#comparison-input-ownership)
separates these software fixtures from installed-CLI and physical evidence.
Capture has separate input qualification below; remaining legacy key inputs
and persistence's live source ownership remain separate work. This increment
does not activate sampled storage reads or change the existing image,
physical-PC or release gates.

The capture-input increment now checks ownership and closure of its fingerprint
and signing-key inputs, uses fixed input diagnostics and observes cancellation
before collection. It preserves later worker/signing/output policy and cleanup
exit 12. The isolated host suite passed 5,004 test/subtest outcomes; selected
Linux amd64/386 fixtures passed 1,074 across eight suites and 100 required root
executions. The [validation record](requirements-validation.md#capture-input-ownership)
records the six raw-key calls left at that capture stage in finalization,
discovery, persistence and save, separately from persistence's live source
ownership. The finalizer increment below reduces the remaining key calls to five.
Existing image, physical-PC and release gates are unchanged.

Finalization checks the exact 32-byte fingerprint input and both owned closures,
contains input diagnostics and handles local malformed contexts before linked
verification or key acquisition. It preserves the verified parent summary,
current evidence and the session's fresh finalization context. The host suite
passed 5,053 test/subtest outcomes and all 121 required host roots. Selected
Linux amd64/386 fixtures passed 1,934 outcomes across eight suites and 256 root
executions, with zero failures or skips and confirmed cleanup. Five legacy
raw-key calls in discovery, persistence and save remain. Frozen-plan/evidence
closure, persistence's live source owner and the broader feature roadmap remain
separate work; existing image, physical-PC and release gates are unchanged.

The report-target fingerprint input implementation now uses the existing
bounded 32-byte owned reader before either discovery or authorization. Fixed
input diagnostics and checked closure prevent key faults from reaching the
selected consumer; cancellation is checked before consumer entry. Catalog
status, authorization, target policy and output behavior remain unchanged.
Windows host validation passed 5,159 tests and 29 packages. Selected Linux
amd64/386 fixtures passed 244 named outcomes across four suites and 44 root
executions, with no native failures or skips, as recorded in
[requirements validation](requirements-validation.md#report-target-fingerprint-input-ownership).

This implementation reduces the five legacy calls recorded after finalizer
qualification to four: deployment discovery, two persistence inputs and save.
Persistence's live source ownership and the broader feature roadmap remain
separate work. Other-OS acquisition, installed-CLI and physical-media execution
remain unqualified. Existing image, physical-PC and release gates remain open.

- Expand the embedded capability graph beyond the initial graphics, CPU,
  memory, storage, input, USB, network, audio, and camera families.
- Add CPU, partial memory, SMART/NVMe health, read-only storage, complete
  display/GPU, network, audio, input, USB, and camera quick checks.
- Extend operator prompts and run-wide watchdog coverage around the existing
  signed reports, explicit unsupported states, and bounded CPU/RAM safeguards.
- Extend the bounded quick-session safeguards to broader profiles, add
  heartbeat supervision, and expand thermal telemetry beyond exposed
  temperature inputs, preserving signed stop reasons and earlier unsafe evidence.
- Complete the [physical pilot](beta-pilot.md) on at least ten varied PCs
  without destructive tests.

Exit: `0.2.0-beta.1` gives useful resale acceptance results in under 15 minutes.

## Phase 3 — complete profiles

- Add Memtest86+ boot integration, standard, extended, and burn-in profiles,
  calibrated storage self-tests, sensors, battery, optical media, card reader,
  modem, and optional remote submission.
- Complete the remaining matrix coverage for SPD/ECC, Bluetooth, power and
  cooling, firmware-security reporting, expansion/dock fixtures, clock drift,
  and operator physical inspection.
- Add controlled temporary-file writes and a physically separate destructive
  image for wipe and full write qualification.
- Implement the adapter package/protocol/runtime contract, including burn-in
  support, and validate third-party adapter conformance.
- Add signed correction/supersession history beyond the implemented one-hop
  retest reference, preserving immutable originals and independent trust.
- Add separately consented encrypted restricted-evidence export with the
  required privacy controls.
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
- Hardware identity: use deployment-keyed HMAC identities for comparisons
  within a deployment and key epoch; automatic sessions use unlinkable
  per-session keys unless an explicit deployment token is selected. Cross-epoch
  or cross-deployment comparison and restricted raw-identifier export remain
  separately controlled future work.

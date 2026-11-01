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
that pointer increment retained the eleven-worker v5 journal. Windows and native Linux
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
without silently truncating device lists, closes handles before admission and reports
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
remain unknown, decorators keep their declared ReadFile behavior, and the
per-value budget does not truncate device lists. Focused Windows validation passed 2,215 test/subtest
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

The later aggregate directory-budget increment limits each inventory collection
to 65,536 directory records and 8 MiB of admitted name bytes across its directory
reads. Exhaustion fails without a partial snapshot; exact-limit EOF, checked
closure and source overrides remain mandatory. The counters reset for each
collection. These bounds cover collector-owned retention, not allocations inside
callbacks or opaque entry values, total heap use, or blocked kernel calls.

All 39 new regression names passed on Windows amd64 and 386 and in the
full Windows suite. Vet and Linux amd64, 386 and AArch64 production builds
passed. Native Linux runtime, rebuilt-image and physical-device qualification
remain open.

Cumulative scalar/proc acquisition shares 65,536 file attempts
and 64 MiB of raw bytes across one inventory collection. Missing, empty and
repeated attempts count; discarded prefixes and bytes returned with an error
remain charged. Counters reset for each collection. The owned reader may use
at most one additional, unretained byte to detect overflow. Exhaustion returns
a fixed error and no partial snapshot. Existing per-file limits, exact EOF,
checked closure and declared source overrides remain mandatory.

All 61 new regression names passed on Windows amd64 and 386 and in the
full Windows suite. Vet and Linux amd64, 386 and AArch64 production builds
passed. Native Linux runtime, rebuilt-image and physical-device qualification
remain open.

The proc parser-budget increment implements one shared allowance for CPU,
memory and proc input-device parsing in each collection: 65,536 raw record
blocks, 262,144 units of visited line/word work and 16 MiB of cumulative
retained-text accounting. Empty, malformed, duplicate and overwritten work
keeps its charges. Selected record, field, CPU-alias and first component
allocations require admission first; exhaustion returns a fixed error and no
partial snapshot. Fresh collections reset the counters. Existing acquisition,
source overrides, cancellation and checked closure remain in force.

The parser increment passed complete inventory-package tests on Windows
amd64 and 386: each recorded 1,673 passes and one retained platform skip.
All 48 new named cases passed on each architecture. All nine commands passed,
covering formatting/checks for five Go files, both package suites, vet and
three Linux amd64/386/AArch64 test-binary builds. Those artifacts were compiled
only. These results qualify this inventory slice.

A subsequent three-command check of the combined Extended memory and
inventory source passed the full Windows amd64 suite with 11,689 passes
and 18 retained platform skips, all 290 selected Windows 386 names across
55 roots and 235 declared-parent intervals in five packages, and vet.
All seven postchecks passed. The same combined source then passed 580 selected
Linux amd64/386 outcomes across ten package executions, including all 110 roots
and 470 declared-parent intervals, with no failures or skips. Nine ownership
cases, 32 archive cases, six postchecks and cleanup passed. Installed
command/supervisor behavior, the full Linux suite, race checks, ARM runtime
and a full elapsed Extended session remain separate gates.

Input handler and capability scans now iterate over words without temporary
token arrays. Unicode boundaries, 32/64-bit bitmap ordering, selected-word
parsing and existing classification precedence are preserved. Valid token scans
have zero-allocation regression checks with prebuilt large inputs; classifier
name copies, malformed-number errors, aggregate memory and CPU limits remain
outside that claim.

The input token increment passed 4,209 affected-package and caller test/subtest
cases on each of Windows amd64 and 386, with one retained platform skip per
architecture. All 113 new names passed on both. All 14 commands passed,
including formatting, vet and six Linux amd64/386/AArch64 application and
inventory-test builds. Those artifacts were compiled only; this increment adds
no Linux runtime or physical-device qualification.

Remaining parser expansion, aggregate/peak allocation, CPU time budgets and
optional field-error visibility remain open. These logical limits exclude
allocator overhead, raw source storage, later downstream copies and provider
allocations; they do not establish a whole-heap bound or kernel preemption.
Cancellation cannot interrupt a blocked source call. This inventory increment
does not activate catalog entries. Broader Linux runtime, rebuilt images,
physical devices, sampled-media reads, the physical pilot and release approval
remain separate gates.

The inactive storage result reader now checks complete typed topology and fixture
facts, finite read observations and accepted versus diagnostic counters. Explicit
v6 session read support reaches signed-report verification, HTML/PDF rendering
and comparison, including bundles without views. Historical v4/v5 handling stays
supported. That reader qualification used v7 sessions with twelve workers;
the storage sampling catalog entry remains inactive.

The preceding admission and observation model passed 140 Windows test/subtest
events across 30 test functions, with no skips; formatting and vet passed. Those
fixture checks cover that model cohort only. The integrated reader then passed
3,776 Windows test/subtest events across 27 packages, including all 67 candidate
test functions. Formatting, vet and Linux amd64/386/arm64 cross-builds passed.
The same reader source subsequently passed 1,582 native Linux amd64/386
test/subtest events across 14 suite runs, all 67 candidate roots on each
architecture and 62 verify/compare CLI cases, without failures or skips.
Cleanup and all six final checks passed. That native cohort predates
the separate CLI output-completion change. Rebuilt images, physical media
and release qualification remain open; these fixtures accessed no media.

The next storage increment has complete isolated components for finite read
schedules, strict worker replies, parent accounting and result projection,
plus bounded topology/mount/swap parsing, owned metadata capture, a held
metadata scope and canonical-fact classification. The earlier 27-file candidate passed
773 test/subtest outcomes across two packages on each of Windows amd64 and 386.
Each run included the exact 495 selected names under 68 roots and 427 direct
parent relationships, without failures or skips. Formatting, vet and all seven
postchecks passed. Six Linux test binaries compiled; the arm64 builds remain
compile-only evidence. Selected native component and owned-path suites then
passed on Linux amd64 and 386: 505 names under 70 roots and 435 direct parent
relationships per architecture across two packages. All 1,010 named outcomes
passed without skips, missing/extra names or duplicate lifecycles. Nine
ownership fixtures, 32 archive checks and all six final checks passed, with
cleanup and transfer EOF confirmed. These runs sampled no real media.

The successor now connects finite canonical-ancestor acquisition to the held
metadata scope and classifier. Directory owners and link/classification facts
are rechecked before its result is released. This supplies descriptive facts,
not boot provenance or continuous target ownership.

Its current 31-file candidate passed 848 test/subtest outcomes and all 119
package roots across two packages on each Windows architecture. The exact
selected cohort was 570 names under 76 roots and 494 direct parent relationships,
with no failures or skips. Focused vet, six Linux test-binary cross-builds and
all seven postchecks passed. The module minimum is now Go 1.25.0; validation
used Go 1.27.2 and does not qualify execution on Go 1.25.

The full-module suite and vet also passed on Windows amd64: 8,873 test/subtest
passes, 18 allowed skips, 31 package passes and one package without tests.
All 32 packages, 1,349 required roots and the selected 570-name cohort were
reconciled.

The current selected Linux cohort passed on amd64 and 386, with 667 named
outcomes under 91 roots and 576 direct parent relationships per architecture.
All 1,334 outcomes across four package runs passed without skips. Nine ownership
fixtures, 32 archive checks and six final checks passed, including cleanup and
transfer EOF. Linux arm64 remains compile-only; this is selected component and
owned-filesystem evidence, not Linux full-module or real-media qualification.

The live sampling route is still incomplete.
The remaining connected work includes protected early boot-source production
and consumption, continuous target/backing ownership, and an actual supervised
worker joined to the CLI and result consumers. Boot and ownership designs are
not implementation or runtime evidence. Unknown boot backing is not an empty
set, and repeated metadata does not establish continuous ownership. The
components leave discovery/backing admission incomplete and reject loops;
`storage.quick` remains inactive until the connected route is qualified.
All four rebuilt images, physical media, the physical-PC pilot and release
approval remain separate gates.

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
as definition version 2; that increment introduced an eleven-worker v5 journal while preserving
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

A source-reviewed keyboard correction separates key-state queries by phase.
The initial neutral check may flush stale queued keys before action credit
starts. After that baseline, completed keyboards remain observed while another
is unfinished, with no active key-state query that could remove queued events.
Final neutrality is established only during the bounded terminal proof, followed
by checked closure. Racing input can revoke completion and return inconclusive
without restarting the remaining observation window. Undefined `SYN_REPORT`
values do not alter frame boundaries; event loss and malformed transitions
still reject.

With Go 1.27.2, the focused Windows amd64 and 386 suites each passed 345
test/subtest outcomes, including all 150 selected keyboard names. The full
Windows amd64 suite passed 8,952 outcomes with 18 permitted skips; whole-module
vet also passed. The same new regression tests expose the predicted failures in
the predecessor and pass with the corrected source.

Native Linux amd64 and 386 each passed the 150-name correction selection across
25 roots and 125 parent intervals, with no failures or skips. Nine ownership
fixtures, 32 archive cases and all six root postchecks passed with confirmed
cleanup. Linux amd64, 386 and AArch64 test artifacts compiled; AArch64 remains
compile-only. Earlier keyboard results above retain their original scope. This
injected queue-model regression is not a deployed-kernel race reproduction. The six controls,
evidence schema and session policy are unchanged, and rebuilt-image,
physical-device, pilot and release gates remain open.

The legacy single-contact touchpad core and Linux adapter were first qualified
as inactive components. They observe complete frames with fixed per-contact X/Y
anchors, finite signed-axis thresholds and a primary-button cycle. Checked
acquisition, terminal identity/idle/neutral proof and reverse closure prevent
incomplete actions or cleanup uncertainty from becoming a pass. Unsupported
capabilities remain visible; multi-touch and broader gesture coverage are not
inferred from a supported subset.

With Go 1.27.2, focused Windows amd64/386 suites each passed 541 outcomes,
including the exact 196-name component selection. The full Windows amd64 suite
passed 9,148 outcomes with 18 permitted skips, and vet passed. Native Linux
amd64/386 each passed all 395 selected names across 47 roots, with no failures or
skips and confirmed cleanup. Linux AArch64 remains compile-only.

These component results qualify software and owned fixtures. The subsequent
inventory/evidence increment adds finite candidate discovery, checked MSC
observations and strict functional evidence with inventory/plan bindings.
Go 1.27.2 focused Windows amd64/386 suites each passed 2,332 outcomes with one
platform skip, including all 686 selected names. The full Windows suite passed
9,509 outcomes with 18 permitted skips, and vet passed. This increment's
Linux/native qualification remains open.

The standalone command is also host-qualified with Go 1.27.2: focused Windows
amd64/386 runs each passed 5,647 outcomes with seven permitted skips, and the
full Windows suite passed 9,617 outcomes with 18 permitted skips. All 108 CLI
names passed in each run, and vet passed. This adds no Linux/native or
real-device qualification.

The pre-activation v9/v10 session evidence and command/finalizer increment
passed Go 1.27.2 Windows fixture qualification. Focused amd64/386 runs each passed
6,277 outcomes with seven permitted skips; the full suite passed 9,982 with
18 permitted skips, and vet passed. All 151 session and 214 command names
passed in each run, including strict binding and cancellation/cleanup cases.

The pre-activation app/report increment implemented strict validation,
signed-bundle checks, bounded HTML/PDF views and public candidate projection.
They preserve historical handling, reject contradictory evidence and retain cleanup uncertainty. Their
new app, bundle, comparison, render and privacy fixtures have Go 1.27.2
Windows qualification: focused amd64/386 runs each passed 4,586 outcomes
with six permitted skips; the full amd64 suite passed 10,242 with 18 permitted
skips. All 260 app names passed in each run, and vet passed.

Touchpad definition 2 now connects the catalog, frozen plan, Quick defaults
and menu, checked session dispatch, owned finalization and signed consumers.
The provider requires the finite derived `touchpad_candidate=true` marker;
a category label alone cannot schedule the worker, and candidacy does not
establish device support. That touchpad activation used Quick v9 with thirteen
workers and Standard v10 with fourteen, with touchpad inserted once after
pointer. Its Quick selection offered thirteen modern checks and eleven on
legacy x86. Explicit exclusions remain
Quick-only; Standard rejects a nonempty exclusion set.

Current execution requires a freshly decoded plan matching the current catalog.
Rehashing an obsolete plan does not admit it. Historical v4-v10 signed reports
and journals retain their version-specific read rules without authorizing
historical plans for current execution.

Go 1.27.2 focused Windows amd64/386 runs each passed 4,838 outcomes with six
permitted skips; the full amd64 suite passed 10,341 with 18 permitted test skips.
All 583 activation names across 73 roots passed in each run. Formatting, vet
and all seven postchecks passed. These are software-fixture results.

Linux/native validation of the later integrated route, real evdev/kernel
behavior, edges, taps, multi-touch, scrolling and gestures, rebuilt images,
physical devices, the pilot and release approval remain open. See
[route scope](requirements-validation.md#touchpad-inventory-and-strict-evidence)
and [tests](test-strategy.md#touchpad-inventory-and-evidence-host-validation).

### Bounded single-contact touchscreen development

The touchscreen producer observes a narrow single-contact absolute-input
interface. Each interface needs fresh coordinates, movement in both directions
on both axes within one contact, a release and checked terminal cleanup.
Activity alone cannot pass. Multi-touch, stylus, pressure, calibration and
whole-panel coverage are outside this check; touchpad behavior is unchanged.

The connected implementation makes touchscreen definition 2 applicable
through the derived candidate marker. The initial activation used Quick
v11/fourteen workers, Standard v12/fifteen and fifteen implemented/seven inactive
catalog rows. Current Quick v14 and Standard v15 retain that worker position;
the catalog now has sixteen implemented/six inactive rows. Modern Quick
selection has fourteen choices and legacy x86 has twelve. Current execution
requires a fresh schema-3 plan. Historical v4-v13 journals keep their own
decoding rules without authorizing stale-plan execution.

Earlier producer/discovery qualification passed 2,250 test/subtest outcomes
with one existing platform skip on each focused Windows architecture, plus
six compile-only Linux test builds.

The complete connected route has now passed Windows software qualification.
Focused amd64 and 386 runs each passed 8,369 test/subtest outcomes with seven
platform skips; the full suite passed 11,238 with eighteen skips. All 794 new
named checks passed in each run. Formatting, vet and twelve Linux builds passed:
three application builds and nine command/input/inventory test builds across
amd64, 386 and arm64. At that host-only stage, those Linux artifacts had not
been executed. Subsequent selected native Linux amd64 and 386 fixtures passed
2,952 named outcomes across six package/architecture runs and 344 root
executions, with no skips or failures. Nine ownership cases, 32 archive cases
and all six postchecks passed, with cleanup confirmed.

Real evdev/device behavior, installed-command checks, ARM runtime, race checks,
rebuilt images, physical touchscreens, the ten-PC pilot and release approval
remain open. See [operator guidance and scope](touchscreen-quick.md).

The CPU worker now adds twelve fixed scalar binary64 operations per suite, with
per-worker counts and exact mismatch bits. Definition 2 uses CPU v3 evidence in
that increment's eleven-worker v5 session; CPU v2 evidence keeps its historical
definition and typed hash. Focused tests cover arithmetic, stop boundaries and
signed consumers. Full host and native Linux validation passed, including race
checks and standalone/session CLI paths. Rebuilt images and physical CPU checks
for this increment remain open. Legacy soft-float coverage is software
arithmetic only. At that milestone, wider ISA, compression, sustained stress,
performance, power and throttling checks remained planned. The bounded standard
CPU-stress increment below addresses only its named load and evidence contract.

The RAM worker adds a fixed moving-inversion sequence after its three existing
patterns. Memory v3 records completed phases and passes, accepted and uncredited
logical work, and an explicit mismatch, including offset zero. Definition 2
used the then-current eleven-worker v5 journal; historical memory v2 keeps its typed
hash. Full Windows tests, vet and cross-builds passed; compiled x86 inspection
confirmed buffer accesses and reverse indexing. Native Linux suites, race tests
and signed direct, standalone and session workflows passed. At that milestone,
rebuilt images, physical memory, broader working sets, SPD/ECC, extended stress
and Memtest86+ remained open. The bounded stress increments below retain
separate evidence. The frozen alpha scope is unchanged.

A separate `hhpt memory-stress` command is implemented in development source.
It requests a fixed 30-minute load over one 4-64 MiB working set selected against
fresh available memory. Initial and ongoing thermal admission, checked logical
work, a final read and confirmed cleanup share a 31-minute watchdog. Its strict
JSON diagnostic is separate from quick-memory and signed-session authority.
See [the command and scope guide](memory-stress.md).

All 19 host-validation commands passed: formatting/checks for 13 Go files,
196 named fixture passes without skips on each of Windows amd64 and 386,
11,434 full-suite passes with 18 retained platform skips, vet, three Linux
application builds and nine Linux test-binary builds. That host step compiled
the Linux artifacts without executing them. Subsequent selected Linux
amd64/386 runs executed six test binaries and passed 392 named outcomes across
62 roots, with all 330 declared-parent intervals checked and no failures or
skips. All nine ownership cases, 32 archive cases and six postchecks passed;
cleanup was confirmed.

Those controlled-clock runs qualify the selected native software fixtures.
A later standalone API test completed a real 1,800-second load on each of
Linux amd64 and 386 with one 4 MiB working set and synthetic availability and
thermal inputs. Final verification, cleanup, nine ownership cases, 32 archive
cases and six postchecks passed. The earlier failed elapsed attempt remains
unqualified. Race checks, the full Linux suite, ARM runtime, installed-command
execution and physical-memory/thermal qualification remain open. The earlier
RAM quick qualification above retains its original scope.

Development source now connects an explicitly selected Extended memory profile.
Its current v16 journal has fifteen workers, adding memory stress after successful
quick-memory checks while CPU stress stays Standard-only. The 2,700-second work
budget and 2,750-second outer supervisor policy preserve the producer's fixed
1,800-second load and 1,860-second watchdog; they are not a wall-clock or
complete-cleanup guarantee. The real finalizer binds original evidence bytes,
typed evidence, the frozen plan and journal before signing. Strict consumers,
canonical views and comparisons retain those checks.

The following Extended and combined qualifications used the earlier
v11/v12/v13 source, before exterior definition 2. They do not qualify current
v14/v15/v16 inspection. Corrected Extended host validation passed 242 focused test/subtest cases
on each of Windows amd64 and 386. The full Windows suite passed 11,641
cases with 18 platform skips; formatting, vet and fifteen Linux compile-only
builds passed. Earlier standalone host and native fixtures keep their original
scope. The later combined source also passed the selected Linux amd64/386
session fixtures recorded above; a full elapsed Extended session remains open.
Current Quick v14/fourteen workers and Standard v15/fifteen workers retain the
same schedules; Standard keeps its 900-second budget.
The development catalog has sixteen implemented rows and six inactive families:
`audio.quick`, `camera.quick`, `network.quick`, `storage.quick`,
`storage.destructive` and `usb.quick`.

The startup scripts separately passed two shell syntax checks and all 22
mock-fixture methods, including 22 new subcases, without failures or skips.
These checks used the standalone baseline plus three startup/fixture files;
they did not execute installed supervisors or a product session. Ownership,
archive and six postchecks passed with cleanup confirmed.

Installed startup and full-duration Extended integration, broader
working sets, SPD/ECC, Memtest86+, ARM runtime, all four rebuilt images, physical
systems, complete profiles, the ten-PC pilot and release approval remain open.
See the
[Extended contract](requirements-validation.md#bounded-extended-memory-session).

The audio readiness acquisition increment checks closure of its owned root,
registry and attribute handles, bounds metadata reads and retains earlier
diagnostics after later faults. It preserves root containment and inspects
device-node metadata without opening playback or capture streams. The result
schema, catalog and eleven-worker v5 session were preserved by that increment: audio errors
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
pass. That increment preserved the existing schema, catalog and eleven-worker
v5 session policy.
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
raw-key calls in discovery, persistence and save remained at that stage.
Frozen-plan/evidence closure is addressed by the later
[small-document reader increment](requirements-validation.md#small-regular-file-input-closure).
Persistence's live source owner and the broader feature roadmap remain separate
work; existing image, physical-PC and release gates are unchanged.

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

The save fingerprint-input increment is implemented and qualified by host and
selected native fixtures. It checks the exact 32-byte owned input after existing
usage and audit initialization, contains key-input diagnostics and separates
exact clean context cancellation from input/closure faults. The report source,
verification, menu, confirmation, retry, mount/persistence/unmount and receipt
policies are unchanged. The host passed 5,240 test/subtest outcomes across 29
packages; two native Linux suites passed 260 outcomes with 56 root executions.
The [validation record](requirements-validation.md#save-fingerprint-input-ownership)
keeps selected coverage, host skips and platform limits explicit.

This implementation reduces the four legacy calls recorded at the report-target
stage to three in deployment discovery and persistence. Frozen-plan/evidence
closure was outside that key-input scope; see the later
[small-document reader increment](requirements-validation.md#small-regular-file-input-closure).
Audit and live report owners and the broader feature roadmap remain separate
work. It adds no unsupported-platform, installed-CLI, physical-media,
deadline, rollback or durability qualification. Existing image, physical-PC and
release gates remain open.

The persistence key-input increment is implemented and qualified with host and
selected native Linux fixtures. It applies the owned reader to the fingerprint
key and, in trusted mode,
the public key. Authorization-file reading and shape decoding remain first;
report opening still precedes the trusted-key read. Error and length rejection
precede post-read cancellation, without save's clean-cancellation exception.
Current report/trust, authorization/HMAC, persistence, receipt and output policies
remain unchanged. The [validation record](requirements-validation.md#persistence-key-input-ownership)
records 5,374 host test/subtest passes and 444 native passes in two suites, with
28 roots and 222 required names passing on each native architecture.

This increment reduces the three legacy raw-key calls recorded at the save stage
to one in deployment discovery. Authorization-file and frozen-plan/evidence
closure were outside that key-input scope; see the later
[small-document reader increment](requirements-validation.md#small-regular-file-input-closure).
Audit and live report-source ownership remain separate work, alongside the
broader feature roadmap. It adds no unsupported-platform, installed-CLI, physical-media,
deadline, rollback or durability qualification. Existing image, physical-PC and
release gates remain open.

The deployment discovery-key input increment passed host and selected native
software-fixture validation. It closes the final legacy raw-key consumer using the
owned 32-byte input role, retaining usage/audit precedence and rejecting input
errors or wrong lengths before post-read cancellation. Activation availability,
media selection, confirmation, token loading and activation follow successful
key closure. Existing token/activation, downstream error and read-only mount
policies are unchanged. Host validation passed 5,473 test/subtest outcomes; two
native suites passed 202 named outcomes and 24 root executions without failures
or skips. The [validation record](requirements-validation.md#deployment-discovery-key-input-ownership)
separates these results from the historical persistence evidence.

The preceding one-call count describes the persistence stage; this increment
removes that final caller and the unused reader. Authorization-file and
frozen-plan/evidence closure were outside that discovery-key scope; see the later
[small-document reader increment](requirements-validation.md#small-regular-file-input-closure).
Audit, token/mount, activation and live report lifetimes remain distinct work
alongside the broader feature roadmap. This adds
no unsupported-platform, installed-CLI, physical-media, rollback, deadline or
durability qualification. Existing image, physical-PC and release gates stay open.

The inventory/plan fingerprint-key increment closes two separate direct key
reads that remained outside the completed `readRawKey` helper migration.
Incorrect key lengths were already rejected downstream. Both commands now
require checked, exact 32-byte regular-file acquisition and successful owner
closure before collection, with fixed key diagnostics and cancellation checks
before later effects. Existing usage, plan-selection and collection-error
precedence, profiles, schemas and successful output semantics are preserved.

Windows host validation passed 7,943 test/subtest outcomes across 30 packages,
with 17 test skips and one package skip. Focused and full-suite selections each
passed 1,101 of 1,102 required named outcomes; the retained selection-symlink case
was explicitly skipped. Five output artifacts matched the captured pre-change
bytes, whose semantics and HMAC expectations had been independently checked.
Native Linux amd64/386 qualification passed 1,071 named outcomes under 112 roots
and eight actual intermediate groups per architecture, with no failures, skips,
extra or duplicate names and confirmed cleanup. The
[validation record](requirements-validation.md#inventory-plan-fingerprint-input-ownership)
separates host/native execution from arm64 and FreeBSD compilation-only evidence.

This closes the two identified command key reads, without establishing complete
ownership coverage for every CLI input. Authorization-file and frozen-plan/evidence
closure were outside this key-input increment; see the later
[small-document reader increment](requirements-validation.md#small-regular-file-input-closure).
Audit and live report-source lifetimes remain separate work.
Unsupported-OS runtime, installed-command/image behavior, kernel-call preemption,
race/durability checks, physical-PC qualification, the beta pilot and all four
release-image gates remain open for this increment.

The shared small-document reader now requires successful metadata/readable
closure before returning bytes to its six existing consumer sites. Read or
cleanup failure returns no bytes. It preserves document limits and decoding,
the Linux pinned-object policy, the existing non-Linux acquisition policy and
the separate Standard launch-record reader. Windows amd64 full-suite validation
passed 8,303 named outcomes; the Windows 386 command package passed 3,207.
Both architectures passed all 67 selected reader names and 59 parent
relationships. Linux amd64/386 software fixtures in a VM passed 152 named
outcomes across two suites and 22 roots, with no failures or skips. The
[validation record](requirements-validation.md#small-regular-file-input-closure)
separates host/native execution, arm64/FreeBSD compilation and remaining ownership work.
Audit, token/mount, activation-output and live report owners remain open, as do
kernel-call preemption, durability, installed images, physical PCs and release gates.

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

### Bounded exterior-enclosure observation

The historical v1 finite operator-reported check is implemented through planning, the quick
coordinator, signed finalization, no-view verification, HTML/PDF rendering and
linked comparison. It covers safely visible enclosure surfaces without moving
or opening the device. N records no listed defect observed on inspected
surfaces; uncertain or incomplete work cannot pass. A complete defect survives
a later stop, and hard input errors remain errors.

The whole-device operator row requires fresh schema-3 plans. That qualification
used v7 sessions with twelve workers and 22 catalog rows. Historical v4/v5/v6
remain supported; storage sampling is inactive. Full host validation passed
5,942 test/subtest outcomes across 30 packages, with 17 test skips and one
skipped package. Formatting, vet and three Linux application cross-builds
passed. The selected native Linux amd64/386 cohort passed 4,580 named outcomes
across 16 suites and 836 root executions, with no failures or skips and confirmed
cleanup. Each architecture passed 2,290 names and 418 roots; see
[the exact scope](requirements-validation.md#bounded-exterior-inspection).

The current definition-2 increment separates enclosure integrity, visible
hinge/mount condition, port exterior condition and surface finish. All start
unobserved; only an explicit no-hinge answer permits hinge not-applicable.
No free text or identity is collected. One prompt and one owned four-answer
batch share the existing 45-second bound, preserving earlier adverse findings.
The real command, catalog/plan, coordinator, signed finalizer, no-view readers,
HTML/PDF views and comparisons use Quick v14, Standard v15 and Extended v16
authority, with historical v1 and v11/v12/v13 interpretation preserved.

Current host qualification passed 11,891 full Windows amd64 cases with 18
retained platform skips and 593 focused Windows 386 cases with no skips.
Formatting, vet and 21 Linux compile-only builds passed. Subsequent selected Linux amd64/386 validation passed 1,296 inspection
fixture outcomes across 12 streams with no skips or failures, plus one
separate synthetic standalone CLI exercise. Installed CLI and physical
qualification remain open. The earlier v1 qualification and the combined
714-source/698-Go native cohort of 580 outcomes retain their historical scope;
neither qualifies current native inspection. Hinge movement,
functional ports, hidden damage, cooling noise and battery swelling remain
broader work. The ten-PC pilot, all four bootable images, installed-command
qualification and release approval remain open. No alpha gate is removed.

### Finite power-supply inventory

The Linux inventory implementation adds eleven finite driver-observation fields
and five explicit value states. It enumerates all reported supplies, using
nonpersistent ordinal rows and unavailable identity instead of exporting raw
kernel names or serials. Missing or empty enumeration reports no data, not
hardware absence. Ordinary scalar observation failures discard returned bytes;
existing ownership, bounds, protocol and cancellation policy remains in force.

The new-type-only volatility policy preserves the existing graph and comparison
rules for other components. That increment added no worker or catalog entry:
its qualification used v7 with twelve workers and 22 catalog rows, preserving
historical contracts. The complete host suite passed 6,239 test/subtest outcomes
across 30 packages, with 17 test skips and one skipped package. Selected native Linux
amd64/386 fixtures passed 3,100 named outcomes across 14 suites and 582 root
executions, with no failures or skips and confirmed cleanup;
see [requirements and limits](requirements-validation.md#power-supply-inventory).

These independent observations do not establish battery health, physical
continuity or AC/battery transitions. Those broader features, the ten-PC pilot,
installed-command and rebuilt-image qualification, durability and release
approval remain open. This increment does not expand the approved alpha gates.

### Finite firmware and TPM inventory

The finite metadata producer and consumed report path are implemented. Linux
collection reports UEFI platform width, separate SecureBoot/SetupMode flags and
each enumerated TPM interface's major version, with four explicit field states.
Raw names and stable identifiers stay out of public rows; identity is unavailable
and TPM IDs are nonpersistent ordinals. Missing or empty observations report no
data, not proven absence. Ordinary scalar observation errors discard returned
bytes while existing ownership, bounds, protocol and cancellation failures remain
mandatory.

Unlike volatile power telemetry, these configuration observations stay in the
frozen graph and ordinary comparison. A changed final configuration retains the
signed failed-integrity diagnostic and exact changed component references.
No-observation bytes, generic signed/linked/view behavior and independent parent
trust remain unchanged. The full host suite passed 6,948 test/subtest outcomes
across 30 packages, including all 31 new roots and 216 required names. Selected
native Linux amd64/386 fixtures passed 3,886 named outcomes across 14 suites and
736 root executions, with no failures or skips and confirmed cleanup. See
[the finite contract](requirements-validation.md#firmware-and-tpm-inventory).

This inventory-only slice adds no worker or catalog row and does not reduce the
remaining inactive functional families listed below. It establishes no security
verdict, TPM health, measured-boot verification, attestation, firmware-lock
assessment or physical continuity. Those broader features, optional Secure Boot
image support, physical-PC pilot, installed-command/image qualification and
release approval remain open. That inventory increment preserved Quick v7,
Standard v8 and their budgets.

### Finite memory device inventory

The Linux inventory implements selected SMBIOS Type 17 capacity, memory-type
and form-factor observations with explicit reported, unknown, unavailable,
malformed and unsupported states. Raw source strings and identifiers stay
private; nonpersistent ordinals retain unavailable identity. The observations
participate in the configuration graph, signed reports and ordinary comparison,
including signed failed-integrity diagnostics for changed configurations.

Host tests and selected native Linux amd64/386 fixtures passed for this finite
addition, including actual 32-bit capacity boundaries. The 34 new roots and
691 named source requirements are tracked separately from measured outcomes.
See [the finite contract, results and remaining gates](requirements-validation.md#memory-device-inventory).
Earlier CPU and firmware results remain evidence for those earlier changes.

This bounded inventory slice added no worker or catalog row. At that increment,
nine functional families were inactive, and Quick v7 and explicit Standard v8
retained their schedules and budgets. Broader SPD, ECC, topology, speed,
firmware/OS capacity
reconciliation, RAM correctness beyond the bounded memory workers, and
pre-boot Memtest86+ requirements remain open. Selected firmware observations
establish neither
usable OS RAM nor module health or physical continuity. Image/sysfs availability,
the physical-PC pilot, all four release images, installed-command/image behavior,
race/durability and release approval still require their documented qualification.
The later [Standard console qualification](requirements-validation.md#standard-console-launch)
records completed native VM 600-second CPU loads with synthetic thermal
observations; that evidence is separate from this inventory slice.

### Bounded standard CPU stress

The consumed standard path is implemented and has passed host, selected native
and bounded real elapsed-window fixtures. Broader qualification remains open.
It requires explicit matching profile consent, defaults to a 900-second session,
and inserts CPU stress immediately after a passing CPU quick result. The shared
600-second elapsed load request and 660-second local watchdog retain checked
work, strict initial and ongoing accepted thermal coverage, finite non-pass
outcomes and confirmed cleanup. The CPU qualification below used Standard
v8/thirteen and Quick v7/twelve. The touchpad activation used Standard
v10/fourteen and Quick v9/thirteen. Newly generated plans bind the current
catalog digest; historical signed reports retain their original rules.

The complete source passed the host suite: 6,732 test/subtest passes across
30 packages, with 17 test skips and one package skip. All 493 required named
outcomes across 69 selected roots passed. Selected native tests passed on Linux
amd64 and 386: 6,040 test/subtest outcomes across 18 package executions, covering
517 roots and 3,020 names per architecture, with no failures or skips.

A separate API-level qualification completed a real shared 600-second load
window on Linux amd64 and 386, using two workers and fixed synthetic temperature
observations of 35 degrees C. Both runs passed with cleanup confirmed. This
establishes elapsed-window completion and checked worker participation within
that fixture. CPU time, utilization, all-core saturation and physical thermal
behavior remain unqualified.
At the touchpad qualification stage, the 22-row catalog had fourteen
implemented rows and eight inactive families: audio functionality, camera,
touchscreen, memory stress,
network functionality, storage sampling,
destructive storage and USB functionality. Their readiness or offline evidence
readers do not constitute active functional implementations. Full standard,
extended and burn-in coverage, sensor/clock/power/throttling instrumentation,
physical cooling behavior and performance grading remain open. This bounded
increment does not alter the alpha candidate, four-image release, physical pilot
or publication gates. See [the exact requirements](requirements-validation.md#bounded-standard-cpu-stress).


### Explicit standard console launch

The development console implements explicit standard selection while automatic
start and the ordinary quick choices remain quick. Modern deployment-key entry
also remains quick; standard uses session-local identity and requires the user
to restore any actual quick exclusions. A strict versioned launch record binds
profile, architecture and exclusions. Both startup scripts freeze that record
and the plan and pass matching profiles to planning and the session. Standard
uses a 900-second session budget and 950-second outer supervisor policy; quick
keeps 300 and 350 seconds. The outer policies do not prove whole-process cleanup.

The host suite passed 8,236 test/subtest outcomes across 30 packages, with 18 test
skips and one package skip. The selected contract contains 326 names: 325 passed,
and one permitted Windows symlink fixture skipped. Native Linux amd64 and 386
validation then passed all 652 named outcomes across 92 roots and four package
executions, with all 560 direct-parent relations checked and no missing, extra,
duplicate, skipped or failed outcomes. All 18 startup-shell fixture methods
passed using owned command stubs; the ownership, archive and final host checks
also passed.

The native Linux amd64 and 386 VM console-to-signed-report workflow is now
validated with cancellation smoke and a full 600-second CPU load on each
architecture, verified self-issued reports and confirmed cleanup. These runs
used synthetic 35 C thermal observations. The earlier CPU API evidence retains
its original scope. Complete profiles, physical sensors and CPUs, installed
boot images and all four rebuilt images, the ten-PC pilot, race/durability and
release approval remain open. The 950-second Standard supervisor remains a
policy limit. See
[the console requirements](requirements-validation.md#standard-console-launch).


## Phase 3 — complete profiles

- Complete Standard and Extended matrix coverage beyond their bounded
  implemented routes; add Memtest86+ boot integration and burn-in scheduling,
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

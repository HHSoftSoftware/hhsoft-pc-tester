# Test Strategy

This document defines how HHSoft PC Tester selects, runs, and evaluates
hardware tests. A run must preserve user data by default, expose missing
coverage, and produce evidence sufficient to audit the result.

The full matrix below includes intended future coverage. Use the master
checklist for current implementation status and the
[requirements validation process](requirements-validation.md) for milestone
reviews and independent failure-case testing.

## Test principles

- **Present-if-applicable:** inventory first and schedule a test only when its
  target and prerequisites are present.
- **Read-only by default:** quick, standard, extended, and burn-in tests do not
  write to installed persistent storage. A report target and named sacrificial
  fixtures are the only exceptions.
- **Destruction is separate:** overwrite, erase, format, sanitize, and firmware
  changes require the destructive profile and per-target authorization.
- **Evidence over assertions:** every status has structured observations, its
  criterion, and adapter identity. Operator judgments are labeled.
- **No silent downgrade:** missing drivers, fixtures, credentials, permissions,
  and actions become visible skips or errors, never passes.
- **Bounded execution:** every case has deadlines and a timeout.
- **Conservative claims:** pass covers only the named behavior under recorded
  conditions; it does not certify lifetime or untested behavior.

## Profiles

The modern Alpine images target most systems from the Windows 8 era onward.
The BIOS/CSM-only Buildroot legacy image targets Pentium MMX-class through older
x64 PCs. It shares report semantics but
exposes a visibly reduced test catalog; unavailable driver-dependent tests are
`skip/unsupported`, never pass.

Profiles are versioned declarative manifests. The plan records the exact
manifest, thresholds, fixtures, selections, and exclusions before execution.
Operators may deselect a test, but it remains `skip` in the report. A custom
profile inherits one below and discloses every override.

The implemented operator selection currently covers the safe quick catalog:
twelve tests on modern images and ten on legacy x86. Only otherwise scheduled
tests become `excluded`, reported as `skip` with the exclusion reason. Absent
and missing-driver states retain their applicability reason, and unsupported
or destructive work cannot be enabled through selection. Reports retain explicit
selection reasons and the frozen exclusion intent. The broader profile budgets
and coverage below remain targets, not completed workflows. The implemented
bounded standard CPU path uses a 15-minute session budget; it does not claim
the complete standard matrix or the target budget below.
The [validation milestones](requirements-validation.md) distinguish each
qualified catalog, including the current keyboard increment. Software
qualification does not close rebuilt-image or physical gates.

| Profile | Objective and default budget | Required coverage | Explicit exclusions |
| --- | --- | --- | --- |
| **Quick** | Triage and obvious-defect screening, 5-15 minutes. | Inventory, health flags, display, short CPU/GPU/memory load, sampled storage reads, audio/input prompts, links, enumeration. | Full-media scans, pre-boot memory, sustained load, destructive operations. |
| **Standard** | Resale/return qualification, 30-90 minutes. | Every applicable automated and operator-assisted functional test; moderate load; sampled reads; practical network/peripheral checks. | Full-device reads beyond budget, overwrite/sanitize, endurance claims. |
| **Extended** | Thorough diagnosis, 2-6 hours plus optional pre-boot phases. | Standard coverage with longer stress, full storage reads, supported drive self-tests, throughput, and one complete Memtest86+ pass. | Installed-target writes and overnight endurance claims. |
| **Burn-in** | Stability screening; 8 hours, configurable to 24-72 hours. | Repeated mixed load, temperature stabilization, error-delta monitoring, periodic attended checks, final cool-down comparison. | Target writes absent independent destructive authorization; future-reliability claims. |
| **Destructive** | Targeted write/erase qualification; media-dependent. | Standard baseline then approved full-range write/read, native sanitize/secure erase, or sacrificial-media operations on named targets. | Automatic selection, unattended authorization, boot media, active report destination. |

Quick and standard use short and normal variants; extended uses full/long
variants. Burn-in repeats long variants but rotates workloads unless a whole-PC
power/thermal test is selected. Destructive is an independent capability
overlay, not an escalation caused by failure.

## Applicability and scheduling

For each component, the planner:

1. Establishes stable identity without activating or mounting foreign LVM,
   RAID, encrypted, or ordinary filesystems.
2. Confirms an adapter and required architecture, firmware, kernel, and driver.
3. Confirms fixtures/configuration: known-good cable/media, local endpoint,
   Wi-Fi credentials, SIM, operator attendance, AC power, and cooling.
4. Reserves targets and shared resources. Interfering tests are serialized;
   intentional mixed-load overlap is recorded.
5. Revalidates authorization before each persistent write and after reboot or
   hardware re-enumeration.

Absence becomes `skip/not-present`. Failed initialization is `fail` when a
supported interface and evidence implicate hardware, but `error` when no valid
diagnosis is possible. Known unsupported hardware is `skip/unsupported` until
an adapter exists.

## Automation and access

- `A` is fully automated after profile/fixture setup.
- `OA` requires an operator observation or action. Prompt, response, responder,
  and response time are evidence.
- `MIX` combines automated measurements and operator judgment.
- `R/O` makes no host-visible write to tested persistent media; volatile
  register changes and RAM logs are allowed.
- `E/W` sends network data or writes a temporary file only to the report target
  or named sacrificial removable fixture, never an installed drive.
- `D/W` changes target data, erase state, partitions, or firmware configuration;
  it is allowed only by the destructive profile.

## Hardware test matrix

### Current Phase 1 graphics subset

Phase 1 implements only the first read-only graphics subset of this matrix.
`hhpt graphics` enumerates DRM cards, drivers, render nodes, connector state,
and advertised modes, then queries Mesa through GBM to distinguish a hardware
renderer from llvmpipe or another software fallback. A frame count of zero is
discovery-only; `1` through `600` presents a short KMS pattern on the first
connected card within the current 20-second tool timeout and the shared
30-second graphics command limit. `hhpt display` uses
the locked libdrm `modetest` worker to apply its preferred-mode pattern to all
connected outputs on each card. One compact seven-criterion P/F/I finding per
connector is recorded, with invalid or absent operator input classified as
inconclusive rather than guessed. Its 45-second command limit covers all cards,
patterns, and the finding prompt together. A parent session deadline can shorten
either command's remaining budget.

The probe reports absent DRM devices as skipped and insufficient acceleration
evidence as inconclusive. Preferred-mode and finding orchestration still needs
physical validation for simultaneous outputs and safe prior-mode restoration;
it does not automatically grade pixels or artifacts, test VRAM/video engines,
or perform meaningful stress. Virtual machines validate orchestration and
fallback detection only; visual and acceleration acceptance needs real
hardware plus operator findings.

The current automated input adapter checks that present keyboard, pointer,
touchpad, and touchscreen event nodes open read-only. Its optional bounded
activity mode accepts only category-appropriate input transitions, treats lost
kernel event records as inconclusive, and retains counts rather than key codes
or typed text. This readiness/activity evidence does not replace the complete
operator coverage criteria below.

The guided `input.keyboard.quick` worker checks only left Shift, right Shift,
Up, Down, Left and Right. Each keyboard interface must support all six controls
and show its own press-and-release pairs within a default 30-second window and
90-second command limit. Initially held controls must become neutral before
new actions count. Complete event frames, an idle queue, neutral controls and
a final identity check are required before a device can retain a pass. Lost or
partial records, replacement, disconnect, missing actions and uncertain cleanup
cannot produce a complete pass. Evidence contains fixed action booleans, with
no raw events, typed text or event sequence. One console owner handles Ctrl-C
and discards queued input before a later display prompt. Full layout maps,
rollover, ghosting and other keys remain outside this implemented subset.
Windows and native Linux amd64/386 software qualification passed, including
race checks and CLI workflows. Rebuilt-image and physical keyboard qualification
remain pending.

The keyboard acquisition increment adds these failure requirements:

- Prove eligible descriptor type, device number, containment and identity before
  readable event access. Exercise exact-limit/overflow metadata, EOF, invalid
  counts, zero progress and ambiguous errors through the production acquisition
  path. Track each acquired owner once, including a handle returned with an
  error; close readable handles before their metadata handles and the root last.
- Preserve hard faults through concurrent cancellation, evidence collection,
  finalization and failed output. Keep uncertain cleanup at exit 12. A clean
  pre-cancelled initial state and a pure stopped-context zero read remain aborted;
  invalid counts and joined hard errors remain faults.
- Compare the literal earlier completed row after a later device fault. Require
  final idle, neutral and identity proof before retaining its pass. A final
  zero-byte retry withholds pass without creating a hard error or another read.
  After cancellation, permit only bounded proof for the already-selected
  interface, with no fresh event endpoint or action credit.
- During read-loop failure, later sources may already be owned. Assert no later
  event processing and exact cleanup of those owners; reserve no-later-open
  assertions for acquisition-stage failures.
- Exercise real fixture-root rejection through command/session consumers,
  separately from injected owner/result composition. Establish a valid scheduled
  plan, preserve decoded signed evidence through the live gate, and check the
  intended rejection of unbound or incomplete retained passes before signing.

The contract adds 22 roots (eleven portable and eleven Linux-only) and retains
97. All 86 selected host roots passed, including 54 new named children. Selected
native suites passed 1,196 test/subtest outcomes with all 119 roots on each of
Linux amd64 and 386, no failures or skips, and confirmed cleanup; the full host
and per-suite counts are in [requirements validation](requirements-validation.md).
The unchanged shared primitives also supply boundary coverage. These tests do
not exhaust every combination of final-proof context, joined errors, invalid
counts, socket type and registry metadata-close failure. No real evdev nodes or
key capture, standalone CLI or race run, physical-input qualification, or
blocked-kernel deadline guarantee is claimed by this acquisition cohort.

The separate guided `input.pointer.quick` worker observes relative `EV_REL`
interfaces with both X and Y axes. During a default 30-second window, every
supported interface must show left/right and up/down movement, a primary-button
press/release cycle, cycles for each advertised right or middle button, and both
directions of every advertised wheel axis. One device cannot satisfy another
device's requirements. High-resolution streams take precedence over coarse
companions without double counting or fallback. Absolute `EV_ABS` or mixed
interfaces cannot pass this check. Wheel precision, extra buttons, accuracy,
touch geometry and gestures remain outside its scope.
Evidence contains bounded per-interface counters and action summaries, not raw
events or typed text. Lost records, incomplete actions, or device changes must
not produce a pass. Windows and native Linux amd64/386 suites, race checks and
CLI workflows passed for the movement/button worker and wheel evidence v2.
Fixtures cover absent/single/multiple axes, high-resolution-only
capabilities, opposed or delayed coarse companions, net-zero packets, counter
bounds, whole-packet rejection and no post-stop credit. Strict version, final
inventory, standalone and session bindings have independent negative cases.
Interrupted reads retry without establishing queue idle; stale baseline input,
queued event loss and partial tails cannot earn a pass through that boundary.
The native CLI cases retained incomplete observations without supplied actions.
Rebuilt images, physical devices and the broader input matrix below remain open;
see [requirements validation](requirements-validation.md) for the exact counts.

The guided-pointer acquisition increment adds these failure requirements:

- Exercise exact-limit and overflow registry/identity reads, exact EOF, invalid
  counts, zero progress, ambiguous errors and close failures. Prove contained
  relative sysfs links succeed while absolute and escaping links are rejected.
- Verify eligible observed device numbers, metadata and descriptor identity
  before readable access. Reject substitutions, malformed providers and typed
  nils; track every acquired owner through one close, including handle-plus-error.
- Keep hard faults ahead of cancellation and timeout through runner and command
  handling. Close the readable handle before its metadata handle, then close the
  root. Allow no later acquisition after a hard failure.
- Use positive-count hard-error reads to distinguish complete diagnostic frames
  from partial packets and pass credit. Compare literal earlier completed rows
  after a later fault, including concurrent cancellation and an untouched third
  device. Retain an independently verified earlier pass only with its own proof.
- Require idle queue and final identity before each device pass; later positive
  bytes revoke pass credit. Keep the existing movement, button and wheel criteria.
- Exercise the real fixture-root producer through command/session/finalizer
  consumers and distinguish that path from injected close-result composition.
  Establish the scheduled definition-2 plan before checking the exact promotion
  rejection. Compare strict decoded signed evidence and preserve the source file.

The source contract adds 17 test roots: ten portable and seven Linux-only.
The isolated Windows suite records 4,406 passing test/subtest outcomes across
29 packages, with 17 existing test skips and one empty package. All 75 selected
host roots and their 320 outcomes pass: 49 belong to the ten new portable roots
and their 39 children, and 271 to retained coverage. Formatting, vet and Linux
amd64/386/arm64 cross-builds pass separately from Linux test execution.

Selected native Linux amd64/386 fixtures passed 848 test/subtest outcomes
across four suites, with all 97 selected roots on each architecture (194 root
executions), no failures or skips, and confirmed cleanup. This includes the
seven new and fifteen retained Linux roots. Retained adapters cover the shared
keyboard helper's reachable behavior after removal of
its unused cleanup mode; new acquisition cases invoke actual pointer operations
with injected owners. The fixtures create no evdev nodes and collect no real
input. Physical actions, broader input coverage, blocked-kernel deadlines,
rebuilt images and release gates remain separate.

The current storage readiness adapter rejects writable block mounts and checks
canonical unsigned 32-bit major/minor identities in both the block registry and
mount records. Duplicate registry identities fail, including excluded mappings
and loop devices. Each supported media-bearing interface must match its registry
identity at both pathname inspection and descriptor inspection after a read-only
`O_NOFOLLOW` open. A later registry recheck and successful descriptor close are
required for credit. Cancellation cannot credit the current observation, and a
close failure remains an error; previously observed unsafe mounts are retained.

Mount admission now shares the bounded runtime-safety parser. Empty or malformed
mount observations fail, and every mounted nonzero device number must occur in
the complete registry before any block-node open. Unsupported anonymous
filesystems, including major-zero Btrfs and FUSE mounts, fail closed. Writable
overlays are rejected; the existing permission for read-only overlays is
unchanged. Parsed writable counts remain visible after later syntax, registry
or cancellation failures, independent of map iteration order.

The readiness v1 evidence schema is unchanged. These metadata observations do
not establish persistent physical identity or detect every replacement that
reuses the same kernel device number. No sector data is read or written, so the
adapter cannot claim health, read correctness, throughput, or surface coverage.

For the preceding identity-hardening increment, focused Windows checks and
independent simulated metadata/descriptor cases passed,
as did Linux vet and amd64/386 test-binary cross-builds. The full Windows suite,
vet, 28 Linux test-binary builds and five application builds also passed against
the same source. Native Linux amd64/386 suites, existing race suites and CLI
workflows also passed. Storage fixtures use simulated metadata and descriptors;
these results do not qualify physical disks or media reads. Rebuilt-image and
physical checks remain open; see
[requirements validation](requirements-validation.md).

The mount-policy increment has 109 focused Windows test/subtest passes and
139 simulated Linux-function events. Independent review added 25 events and
passed all 164 simulated events, including missing registries, malformed tails,
record-order permutations and valid diskless controls. Linux vet and four
amd64/386 package test-binary cross-builds also passed. Full Windows qualification
passed 2,406 test/subtest events across 25 packages, with 17 platform test skips
and three packages without host test files. Vet, 28 Linux test-binary builds and
five application builds passed against the same source. Native Linux amd64/386
qualification passed 4,364 test/subtest events across 28 suite runs, including
102 storage events per architecture. Five existing race suites passed 1,545
events; these do not include a storage race suite. Both groups had no skips.
All 24 CLI cases and cleanup checks passed. Storage fixtures continue to use
simulated metadata and descriptors, without physical-device or media-read
qualification. AArch64 was cross-built only. Rebuilt-image and physical checks
remain open; sampled-media reads and their stricter backing-device exclusions
remain unimplemented.

The mount-identity follow-up adds these acceptance cases:

- Conflicting known filesystem types for the same canonical zero-major device
  fail in either order; same-type aliases and valid diskless views remain valid.
- A conflict before, between or after writable block records preserves exactly
  the unsafe counts already observed. Separate observations share no alias state.
- Malformed aliases retain the existing rejection rules, including duplicate
  mount IDs, noncanonical numbers, unknown types and ambiguous access options.
- A conflict in the last of 4,096 records is detected at the exact 4 MiB input
  limit. An extra byte is rejected; existing record and token limits remain.
- Literal runtime-safety v1 JSON retains pass, error and failure after an earlier
  unsafe observation. An already-cancelled observation remains aborted without
  claiming storage or network observations.

Focused and full Windows suites, formatting, vet and cross-builds passed for
this correction. Native Linux amd64/386 suites, six race suites and all 39 CLI
cases also passed with confirmed cleanup. These fixtures exercise metadata
admission without media reads; sampled-media reads, backing-device proof and
physical qualification remain open.

Bounded storage registry enumeration adds these acceptance cases:

- Require actual EOF after short successful batches; reject partial errors,
  joined EOF/errors, no progress, nil entries and responses exceeding the request.
- Exercise 255, 256 and 257 total entries before filtering, including excluded
  interfaces and partitions. Detect a malformed or duplicate final identity at
  exactly 256 and reject overflow without returning a truncated prefix.
- Close every acquired handle once. Preserve read/admission/closure failures
  over simultaneous cancellation; a cancelled successful close prevents name
  processing. Check cancellation again at the caller, including an empty view.
- Compare literal first-error and partial-device-prefix expectations across
  directory permutations. Preserve writable counts and readiness-v1 JSON while
  counting directory ownership separately from block-node credit.
- Exercise controlled temporary directories, normal sysfs-style links, diskless
  views, missing/non-directory roots and invalid mount observations.

Focused and full Windows suites, formatting, vet and cross-builds passed for
this increment. Native Linux amd64/386 suites, six race suites and all 39 CLI
cases passed with confirmed cleanup; see
[requirements validation](requirements-validation.md). Block metadata and
descriptors remain simulated, and no media-content I/O is claimed.

Storage scalar admission adds these acceptance cases:

- Distinguish 255/256 raw bytes plus EOF from 257 bytes, including trailing
  whitespace. Require complete EOF after fragmented reads; reject partial
  errors, no progress and impossible read counts without trusting Stat.Size.
- Check descriptor type before Read, close every acquired handle once and
  preserve cleanup failures over cancellation or earlier observation failures.
  Exercise owned regular files and normal sysfs-style links.
- Place missing, malformed, oversized and overflowing required capacity beside
  a distinct passing media device in either lexical position. Preserve observed
  zero capacity, optional partition capacity, excluded interfaces, optional
  flags/state and successfully read unusable kernel states.
- Preserve required-size and known-overflow failures during cancellation;
  optional observation failures may remain unknown, while any failed scalar
  close is hard. Recheck cancellation before current metadata credit and the
  no-media conclusion, including final zero, partition and excluded entries.
- Compare literal readiness-v1 JSON and retained writable counts/unsafe prefixes;
  separate scalar handle ownership from block-node access and readiness credit.

Focused and full Windows suites, formatting, vet and cross-builds passed for
this increment. Native Linux amd64/386 suites, six race suites and all 39 CLI
cases passed with confirmed cleanup; see
[requirements validation](requirements-validation.md). Scalar file operations
use controlled temporary fixtures; block metadata and descriptors remain
simulated. Broader inventory completeness/error reporting and acquisition bounds,
sampled-media reads, fresh images and physical qualification remain open.

Inventory directory completeness adds these acceptance cases:

- Read through actual EOF after short and exact-size batches, retaining all
  instances beyond 256. The request size is not a total device-count limit.
  Reject partial errors, joined EOF/errors, no progress and oversized responses.
- Close every acquired handle once before validating names. Treat failed closure
  as hard failure; reject nil handles/info/entries, non-directories, invalid names
  and duplicates. Accept only sole clean open-time absence, never mixed failures
  or absence reported after a handle has been acquired.
- Preserve provider-owned slices and lexical order; cache validated names. Compare
  complete distinct-device views across permutations and repeated calls, and keep
  concurrent collection contexts independent when the source supports concurrency.
- Check cancellation before and after acquisition, parsed records and processing,
  including final empty/absent/positive reads, nonmatching entries, nested USB scans,
  early breaks, bitmap observation and snapshot canonicalization. A cancelled or
  failed collection returns no snapshot and starts no later source acquisition.
- Exercise inventory, plan, capture and finalize with read/close/cancellation
  failures alongside successful controls. Require failure before output, capture
  workers or signing; verify the positive reports through their real verifiers.
- Exercise controlled production directories with more than 256 entries and a
  portable closed-handle oracle. Linux-only cases check normal sysfs-style links
  and read-only, nonblocking, close-on-exec descriptor flags.

Focused Windows validation passed 1,900 test/subtest events across eight packages,
with six platform test skips and no failures; formatting and focused vet passed.
All 29 new portable test functions passed. The full host cohort passed 3,047
test/subtest events across 25 packages, with 17 platform test skips, three packages
without tests and no failures. Host vet passed; 28 Linux test binaries and five
application targets built successfully, and the host artifact privacy check passed.
Native validation passed 5,816 test/subtest events across 28 suites and 2,106
race-test events across seven suites; all 39 CLI cases passed. There were no
native/race skips or failures, and cleanup was confirmed. Each native architecture
(amd64 and 386) executed all 31 new test functions: 26 portable inventory tests,
two Linux-only directory tests and three command-boundary tests. The 28 inventory
functions also ran in the inventory race suite; the three command functions ran
in the command race suite. Their individual successful events were checked
alongside the suite totals. The artifact privacy check passed all 573 files.
See [requirements validation](requirements-validation.md).

Inventory scalar acquisition adds these acceptance cases:

- Distinguish 65,535 and 65,536 raw bytes plus exact EOF from a 65,537-byte
  overflow, including trailing whitespace and fragmented reads. Check the final
  one-byte probe; do not treat wrapped or joined EOF as complete data.
- Reject malformed counts, no progress and unproved regular-file type before
  admitting a value. Close every acquired handle once. Preserve overflow and
  protocol failures over observation errors or cancellation, and give failed
  closure precedence over every earlier outcome.
- Keep ordinary open/read errors caused by absence, permission denial or
  unsupported operations unknown, discarding partial bytes. A failed Stat call
  remains a hard acquisition error. Check cancellation at open, Stat, read, EOF,
  probe, closure and final observation boundaries without granting partial credit.
- Exercise every declared attribute and direct scalar role through 105
  category/role cases and their real prerequisites. Preserve decorator ReadFile dispatch,
  the three separate bulk proc paths, 325 distinct devices, repeated calls and
  independent concurrent collection state.
- Require inventory, plan, capture and finalize to reject full Collect overflow,
  nonregular values and cancellation before output or later report work. Reader
  close-error injections and command error-propagation controls provide composed
  cleanup coverage; they do not simulate an OS Close failure inside a command.
- Exercise owned temporary files and closure. Linux-only controls check normal
  sysfs-style links, descriptor flags and FIFO rejection without device I/O.

DRM category cases require controller/render PCI leaves and connector status
fields to remain separate, reject invalid declared values, and preserve plan
and comparison semantics. Linux-only fixtures cover the corresponding symlink
topology, optional absence and invalid leaf types. Their native execution passed.

Focused Windows validation passed 2,215 test/subtest events across eight packages,
with six platform test skips and no failures. Formatting and focused vet passed;
all 26 new portable test functions passed. The isolated artifact privacy check
passed all 397 files. Full host validation passed 3,362 test/subtest events across
25 packages, with 17 platform test skips, three packages without tests and no
failures. Host vet passed; 28 Linux test binaries and five application targets
built successfully. The host cohort also executed all 26 new portable functions,
105 scalar category/role cases and 12 full-collection command cases. Its artifact privacy
check passed all 487 files with zero findings.

Native Linux qualification passed 6,468 test/subtest events across 28 suites,
2,432 race-test events across seven suites and all 39 CLI cases. There were
no native/race failures or skips, and cleanup was confirmed. All 31 scalar
and DRM test functions, including five Linux-only functions, passed on
amd64 and 386 and in the corresponding race suites. The native artifact
privacy check passed all 584 files with zero findings.

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

The inactive storage reader has a separate qualification boundary from a media
worker. Its earlier admission and observation model passed 140 focused Windows
test/subtest events across 30 test functions, with no skips; formatting and vet
passed. These were fixture checks without media access.

The integrated reader passed 3,776 Windows test/subtest events across 27
packages, including all 67 candidate test functions. Seventeen existing tests
and three empty packages were skipped. Formatting, vet and Linux
amd64/386/arm64 cross-builds passed. A subsequent native reader run
passed 1,582 test/subtest events across seven suites on each of Linux
amd64 and 386, including all 134 required root executions, with no
failures or skips. All 62 verify/compare CLI cases, nine runtime
ownership cases and 32 archive cases passed; six final checks and
cleanup were confirmed. The 15 signed fixture cases include accepted
failure, vanished diagnostics, paired hidden-claim controls, signed
forgeries and cleanup rejection. The CLI subset uses provisioned trust
with no views or both views. Self-issued, single-view and literal legacy
CLI variants receive no execution credit from that subset. AArch64
remains cross-built only. This native cohort predates the separate
output-completion fix and does not qualify its injected close faults.

The fixture tests exercise the shared finite decoder through signed-bundle
verification with and without views, HTML/PDF rendering and comparison. Cases
include exact v4/v5 preservation, required v6 source bindings,
duplicate/escaped/case-aliased and misplaced claims, maximum node and byte bounds,
integer fixture masks, whole-family exclusions, and explicit empty discovery.
Accepted failures must survive later stops; unaccepted diagnostic counts cannot
become accepted coverage. An unfinished eligible target needs an explicit stop.
Retained passed devices require exact final inventory, while a vanished
nonaccepted diagnostic does not erase an earlier valid pass. Cleanup uncertainty
must reach every consumer as an error.

Current emission is v7 with twelve workers and the sampling catalog entry
stays inactive. The reader validates signed assertions and feasible time fields;
it does not reproduce precise parent timing, kernel ownership or media behavior.
Media-worker supervision, loop lifetime, native sampling, rebuilt images and
physical qualification require their own evidence. Earlier inventory native
results do not qualify this new reader or a future sampler.

CLI output-completion regressions use injected writers and closers to
exercise open failures, partial writes, nil-error short writes, final
newline failures, close failures and combined write/close errors. Each
owned handle closes once even after writing fails; a stdout value that
also implements Close remains caller-owned. Real filesystem controls
check exclusive creation and preservation of existing content. Caller
tests cover inventory, planning, comparison, report capture/finalization
and quick-test outputs, including cleanup exit status 12 and suppression
of the finalizer success banner after close failure.

The isolated output candidate passed 3,878 Windows test/subtest events
across 27 packages: all nine new roots and 93 named children passed.
Seventeen existing tests and three empty packages were skipped.
Formatting, vet and Linux amd64/386/arm64 builds passed, with all eight
postchecks satisfied. Those host results alone do not establish native
fault execution, fsync durability, rebuilt-image or physical-PC qualification.

The separate selected native run passed 204 test/subtest outcomes across
two Linux amd64/386 suites, with no failures or skips. Each architecture
passed the same nine roots and all 93 named children: 102 outcomes per
architecture and 18 root executions overall. All eight explicit command
groups completed their descendants before passing, and every required
name appeared exactly once. The event totals were recounted from the
closed logs. Nine runtime ownership cases, 32 archive checks and all six
final checks passed with cleanup confirmed.

These native fixtures exercise injected writers and closers, controlled
command dependencies and owned temporary files. The real-file permission
assertion, excluded on Windows, passed with mode 0600 on both Linux targets.
The tests preserve exit status 12 for cleanup uncertainty and reject finalizer
success after close failure. They do not induce kernel-close or fsync faults
and do not execute standalone installed-CLI scenarios; these are limits of
the fixture evidence. The existing durability, rebuilt-image, physical-PC
and release qualification gates remain open.

The current network readiness adapter checks driver attachment and link state
without configuring an address, associating Wi-Fi, or generating traffic. The
safe profile fails closed when any non-loopback interface is administratively
active, including software-created interfaces not treated as hardware evidence.

The network acquisition tests cover the following boundaries:

- Exact scalar/fallback EOF and overflow boundaries, finite read requests,
  regular-descriptor proof and exactly one close attempt for every acquired
  handle, including a handle returned with an open error. Reject invalid
  counts, no progress, partial errors and wrapped or joined EOF.
- Registry 128/129 boundaries, positive batches no larger than 32, explicit
  EOF probes, safe unique cached names and closure before attribute access.
  Preserve ordinary sysfs directory links and reject silent truncation.
- Optional absence only at a sole missing-file open without a handle. Permit
  a sole zero-byte first-read `EINVAL` only for carrier, speed and duplex;
  reject the same error at other phases, after progress or with another
  fault. Preserve the established optional-value parsing rules.
- Bounded driver-link/fallback acquisition, including the real syscall
  negative-count/error convention, absent link with valid fallback, absent
  link and fallback, and generic invalid-count rejection. Presence probes
  must distinguish ordinary absence from permission and other failures.
- Close-error precedence over cancellation, cancellation at each acquisition
  boundary and no later operations. Retain an exact earlier interface row
  after a later read, close or close-plus-cancel fault; the next interface
  must remain unopened and all acquired handles must be accounted for.
- Real command/session paths preserving diagnostic outcomes and stopping
  unsafe session progression. Standalone-finalizer fixtures must first
  establish a scheduled network plan, then retain each status or reject the
  intended evidence/status mismatch. An unrelated plan rejection cannot
  satisfy a negative test. Signed consumer fixtures use self-issued bundle
  verification; injected producer faults and those consumer cases remain
  compositional, without provisioned-key or physical-device credit.

The corrected host run passed all ten portable functions and 87 exact named
children, including the four finalizer status cases and the retained-prefix
case. Each finalizer fixture now establishes the scheduled network plan;
negative cases require the intended evidence mismatch before inventory
collection or report output. Full-suite totals and build evidence are recorded
in [requirements validation](requirements-validation.md).

The matching native run passed all 21 required functions on Linux amd64
and 386: ten portable, seven new Linux and four retained Linux functions.
Independent event review confirmed the full function selection, the host
named-case selection and the Linux fallback and retained-prefix witnesses
on both architectures. Measured totals and cleanup evidence are recorded
in [requirements validation](requirements-validation.md). These acquisition
changes add no traffic generation and retained the schema, then-current v5
emitter and catalog.
Functional network tests, blocked-kernel deadlines, total/peak allocation,
persistent identity, images, physical hardware and release approval remain
separate open gates.

The USB readiness adapter records authorized kernel topology, class,
speed, and direct USBFS character-node access. It opens nodes read-only
without application USB transfers, resets or driver operations. It does
not identify external ports or qualify their data integrity, power,
orientation or modes. Identity/closure acceptance cases require:

- Observed selected-entry device numbers, including minor zero, and
  matching bus/device/authorization observations, without inferred minor
  arithmetic or a borrowed parent attribute. Duplicate observed device numbers
  and addresses must fail before any node observation/open.
- Rejection of unrelated matching character nodes, symlinks, changed
  filesystem/inode identity, changed registry metadata and final-path
  replacement. Ordinary sysfs registry-directory links remain valid.
- Every metadata role at 256 raw bytes and a one-byte overflow probe,
  including initial and optional attributes and vendor presence. Require
  exact EOF and regular-descriptor proof; reject partial errors, invalid
  counts, no progress and typed nils. A sole open-time absence may be
  unknown for optional fields; acquired-handle, permission, read, type,
  overflow and close faults must not become unknown.
- Raw registry boundaries at 2,048/2,049 entries, positive requests no
  larger than 64, separate EOF probes and the independent 512/513-device
  boundary. Reject unsafe, duplicate or nil entries; cache names once,
  preserve ordinary sysfs links and close before later acquisition.
- One class observation with prior numeric spellings retained; initial,
  optional and mandatory-tail cancellation cases must identify the actual
  phase reached and prove no later acquisition. The mandatory-tail cases
  arm only after the selected device-number observation has closed.
- Exactly one close attempt for every acquired handle, including a
  handle returned with an open error. Close failure overrides concurrent
  cancellation; cancellation after successful close revokes current
  credit. Earlier diagnostics survive and no later USB node opens.
- Real command/session/signed-finalizer paths retaining non-pass and
  rejecting pass promotion. Producer-derived consumer fixtures cover
  missing identity, empty discovery and cancellation; positive and
  close-error consumer cases compose Result values rather than induce
  an operating-system Close fault inside a command. No USB global
  exit status 12 is added.

The preceding identity/closure contract contains 20 new functions: eight portable and
12 Linux-only, plus six retained USB functions. The eight portable functions
and all 47 of their subtests passed in the closed host run. Native
execution then passed all 21 selected USB and five command functions on
each of amd64 and 386, including the 12 new Linux-only functions and six
retained USB functions. Complete test2json lifecycles, zero skips/failures,
exact input/archive membership and confirmed cleanup were reconciled.
These selected tests use injected node observations and owned temporary
fixtures; they do not perform application USB transfers or inspect real
USB devices. Physical devices, functional transfers, rebuilt images and
release approval remain separate gates.

The acquisition increment adds 18 functions: seven portable and eleven
Linux-only, while separately retaining the 26 preceding functions. Its
closed Windows run passed all seven portable roots and 60 exact named
children. Full host-suite, formatting, vet and three Linux build evidence
is recorded in [requirements validation](requirements-validation.md).
The matching native run passed all 44 required functions on Linux amd64
and 386, including all eleven new Linux functions and both explicitly
armed mandatory-discovery tail cancellation cases on each architecture.
The measured totals and cleanup evidence are recorded in requirements
validation. The new Linux producer-to-finalizer fixtures use self-issued
bundle verification;
injected close faults and public consumer cases remain compositional,
without provisioned-key or physical-device qualification. Total allocation
and blocked-kernel deadlines remain open.

Audio readiness acquisition has a separate metadata-only test contract:

- Exercise registry boundaries at 4,096 and 4,097 entries with positive
  requests no larger than 32, cached names and exact completion. Reject
  empty nonterminal batches, invalid counts, unsafe or duplicate names,
  typed nils and partial or wrapped EOF errors.
- Exercise 64-byte attribute reads and the 65th-byte overflow witness before
  parsing device-number grammar. Reader acceptance alone is not a valid
  device number. Check no-progress, partial-error and cancellation boundaries.
- Preserve contained sysfs directory links, final-component no-follow,
  regular-file proof and matching metadata/readable-descriptor identity.
  Real root operations must distinguish an absent registry and missing node
  from joined, wrong-phase and acquired-handle failures.
- Compare every nonnil acquired handle with an exact one-close map, including
  handles returned with open errors. Require readable closure before its
  paired metadata handle and root closure last; the registry may close before
  later attributes are acquired. Test root, directory and both attribute
  close failures, including simultaneous cancellation.
- Prove final clean-close cancellation revokes tentative success and earlier
  hard failures survive later stops. Compare literal retained card/control/PCM
  records and counters after a later read or close fault, including close
  failure with cancellation and a third endpoint that is never opened.
- Keep actual producer fault tests and public consumer tests distinct.
  Command, session and signed-finalizer cases retain diagnostic results and
  reject pass promotion. Finalizer negatives first prove the scheduled-plan
  precondition and then the intended rejection reason. Self-issued fixture
  verification is not provisioned-key or physical-hardware qualification.
- Preserve audio's session policy: a reportable error alone does not force
  unrelated workers to stop or prevent signing; aborted work follows the
  existing generic stop path. Do not infer a new global cleanup exit status.

The contract adds 22 functions: twelve portable and ten Linux-only. The
closed host run passed all twelve portable functions and their 62 named
children; full-suite and cross-build totals are recorded in
[requirements validation](requirements-validation.md). Native execution of
these functions and the 19 retained Linux functions passed on amd64 and 386:
438 test/subtest events in four suites, all 82 required function executions,
and no failures or skips. The non-Linux cancellation implementation does
not qualify the Linux function with the same name; that function executed
in the native cohort. Node-creation fixtures retain explicit executed
outcomes. Unconfirmed closure cannot yield an overall readiness pass; prior
Ready rows remain diagnostic. No playback or capture streams are opened by
this increment;
functional audio, blocked-kernel deadlines, images, physical hardware and
release approval remain separate gates.

Input-readiness acquisition has a separate owned-handle test contract:

- Check raw-byte, line, record and selected-interface limits with exact-limit
  positives and overflow negatives, including irrelevant records and values
  whose whitespace would hide overflow after trimming. Require exact EOF;
  reject invalid counts, zero progress, partial errors and ambiguous EOF.
- Verify observed device-number eligibility, metadata/readable identity,
  contained sysfs links and rejection of wrong types, symlink leaves,
  substitutions and duplicate identities before readable credit.
- Track each actual nonnil acquired handle, including handle-plus-error,
  through one close attempt. Typed nils cannot become ordinary absence.
  Compare literal retained rows after a later close error, including concurrent
  cancellation and a third device that is never acquired.
- Keep final parser faults ahead of cancellation; arm late identity tests
  after event ownership. Activity tests use fixed synthetic frames, bounded
  nonblocking reads, exact aggregate counters, evidence-loss reporting and
  final cancellation. No keycodes, raw event payloads or timestamps are exported.
- Exercise the real producer with owned non-device fixtures, then command,
  session and signed-finalizer consumers. Prove readiness is scheduled in the
  frozen plan before testing the exact pass-promotion rejection. Keep injected
  close-fault coverage distinct from a same-run end-to-end claim.

The source contract contains 27 new test functions: ten portable and seventeen
Linux-only, plus eight retained readiness functions. The closed Windows host
suite records 4,205 passing test/subtest events across 29 packages, with 17
existing test skips and one empty package. All ten new portable roots and 24
named children pass. Formatting, vet and complete Linux amd64/386/arm64 builds
pass separately from Linux test execution.

The selected Linux amd64/386 cohort executes all 35 required roots per
architecture, including the retained functions: 70 root executions and 364
passing test/subtest events across four suites. Each architecture records 163
input and 19 command events, with no failures or skips. Nine ownership controls,
32 archive controls and six final checks pass with confirmed cleanup. The
selected command tests do not imply separate standalone CLI invocation evidence.

The tests create no evdev nodes and collect no real input. Guided
keyboard/pointer, physical-device, blocked-kernel deadline, image and release
qualification retain separate gates.

`Q`, `S`, `E`, `B`, and `D` identify profiles selecting a case by default. Each
row is conditional on presence and prerequisites. Times are per component;
the duration precedes the hard timeout.

| Area | Test and acceptance criterion | Mode / access | Profiles | Duration / timeout | Minimum evidence |
| --- | --- | --- | --- | --- | --- |
| Identity | Enumerate board, chassis, CPU, memory, graphics, storage, network, and peripherals without duplicate or contradictory identities. | A / R/O | Q S E B D | 30 sec / 2 min | Sanitized identifiers, topology, source interfaces, conflicts. |
| Identity | Re-enumerate at end and identify component additions, removals, or replacements. | A / R/O | Q S E B D | 30 sec / 2 min | Before/after fingerprint delta. |
| Firmware | Record BIOS/UEFI, SMBIOS, ACPI, and boot path; fail on table errors that prevent required hardware use. | A / R/O | Q S E B D | 20 sec / 90 sec | Firmware metadata, relevant events. |
| CPU | Verify topology/ISA and stable per-worker computation checksums. | A / R/O | Q S E B D | 30 sec / 2 min | Identity, worker count, checksum/errors. |
| CPU | Mixed integer, floating-point, compression, and crypto stress without wrong calculations, hangs, unexpected throttling, or machine checks. | A / R/O | Q S E B D | Q 1m; S 10m; E 45m; B repeat / budget + 5m | Load, clocks, temperature, throttle/error deltas. |
| CPU | Grade throughput only against a valid model/platform baseline; otherwise report it without pass/fail. | A / R/O | S E B | 2 min / 5 min | Workload, result, baseline, tolerance. |
| Memory | Check capacity/topology, SPD where exposed, speed, and firmware/OS consistency. | A / R/O | Q S E B D | 20 sec / 90 sec | Slots, SPD hashes, mismatches. |
| Memory | Exercise the largest safe userspace working set with multiple patterns; tolerate no bit errors. | A / R/O | Q S E B D | Q 2m; S 15m; E 60m; B repeat / budget + 5m | Bytes, coverage, patterns, errors. |
| Memory | Boot Memtest86+ for unavailable regions; require zero errors for completed passes. | A / R/O | E | 1 pass / 8 hr | Version, map, passes, errors. |
| Memory | Monitor ECC deltas; any new uncorrected error fails, and corrected errors are thresholded/disclosed. | A / R/O | Q S E B D | Whole run / +2 min | Capability, counters, decoded events. |
| Display | Enumerate connectors/EDID and set native and safe fallback modes without link errors. | A / R/O | Q S E B D | 30 sec/output / 2 min | Connector, EDID hash, mode result. |
| Display | Show color, ramp, geometry, and motion patterns; assess pixels, uniformity, flicker, artifacts, scaling, and overscan. | OA / R/O | Q S E B D | 90 sec / 3 min | Pattern IDs, findings/display. |
| Display | Exercise brightness and internal-panel sleep/wake where supported. | MIX / R/O | S E B | 60 sec / 3 min | Controls, transitions, response. |
| Touch | Verify one contact over surface/edges, mapping, release, accuracy, and no ghost contacts. | MIX / R/O | Q S E B D | 90 sec / 3 min | Trace heat map, dropped/ghost events. |
| Touch | Verify advertised contacts and pen pressure, buttons, hover, tilt, and palm rejection. | MIX / R/O | S E B | 2 min / 5 min | Contact/event traces by capability. |
| GPU | Attach intended driver and initialize accelerated rendering on each GPU. | A / R/O | Q S E B D | 30 sec / 2 min | GPU/driver, renderer, fallback/errors. |
| GPU | Render deterministic scenes without checksum corruption, reset, hang, or visual artifacts. | MIX / R/O | Q S E B D | Q 1m; S 10m; E 45m; B repeat / budget + 5m | Frames/checksums, resets, operator response. |
| GPU | Decode/encode supported representative video without corruption or hidden software fallback. | A / R/O | S E B | 3 min / 10 min | Codec/mode, drops/errors, engine. |
| GPU | Collect thermal, clock, power, throttle, and error deltas without asserting missing sensors. | A / R/O | Q S E B | Whole load / +2 min | Time series, throttle/error reasons. |
| Audio | Enumerate routes and open playback/capture streams without xrun or transport failures. | A / R/O | Q S E B D | 30 sec / 2 min | Codec/routes, format, xrun count. |
| Audio | Play left/right/stereo tones; operator confirms channels, clarity, and usable volume. | OA / R/O | Q S E B D | 60 sec/route / 3 min | Tone, route, volume, rating. |
| Audio | Record each microphone; assess level, clipping, dropouts, and playback intelligibility. | MIX / R/O | S E B D | 60 sec/source / 3 min | Levels/noise/dropouts, rating. |
| Input | Implemented quick subset: press/release left Shift, right Shift and four arrows on every keyboard interface. | OA / R/O | Q | 30 sec / 90 sec, shortened by session | Fixed action booleans, neutral state, identity and cleanup; software qualified, image and physical gates open. |
| Input | Planned full coverage: detect every keyboard key in a layout-aware map, including modifiers, function keys, and selected rollover combinations. | OA / R/O | Q S E B D | 2 min / 5 min | Press/release map, missing/stuck keys. |
| Input | Verify mouse buttons, wheel/axis, movement, and event continuity. | MIX / R/O | Q S E B D | 60 sec / 3 min | Event count/rate, confirmation. |
| Input | Verify touchpad edges, click, tap, two-finger scroll, and advertised gestures without unintended input. | MIX / R/O | Q S E B D | 90 sec / 4 min | Event traces and capabilities. |
| Input | Verify trackpoints, controllers, dials, remotes, and digitizers with capability-specific event maps. | OA / R/O | S E B D | 2 min / 5 min | Controls and activation results. |
| USB | Record controllers, hubs, speed, power role, and topology; flag descriptor, reset, disconnect, and over-current errors. | A / R/O | Q S E B D | 30 sec / 2 min | Topology and relevant events. |
| USB | Insert a known-good fixture into each external port and verify connect/disconnect, minimum speed, and transfers. | MIX / E/W | S E B D | 60 sec/port / 4 min | Port map, speed, transfer result. |
| USB | Verify power only with a safe meter fixture; do not infer delivery from enumeration. | MIX / E/W | E B D | 60 sec/port / 4 min | Fixture, voltage/current, fault result. |
| USB | Validate USB-C orientation, alternate modes, and power/data roles using compatible fixtures. | MIX / E/W | S E B D | 2 min/function / 6 min | Orientation, role/mode evidence. |
| Card reader | Detect each slot with known-good media and read samples across the medium. | MIX / R/O | S E B D | 2 min/slot / 6 min | Slot/media, reads, rate/errors. |
| Card reader | Create and verify a temporary file only on operator-declared sacrificial media. | MIX / E/W | S E B D | 2 min/slot / 6 min | Target confirmation, bytes, cleanup. |
| Storage | Enumerate HDD, SSD, NVMe, ODD, eMMC/UFS, RAID members/volumes, and removable media without writable mounts. | A / R/O | Q S E B D | 30 sec / 3 min | Topology, size, model, firmware, serial hash. |
| Storage | Read SMART/NVMe/health/error logs; fail declared critical warnings or thresholds and preserve raw counters. | A / R/O | Q S E B D | 20 sec/drive / 2 min | Capability, state, attributes, errors. |
| Storage | Read distributed blocks with exact repeated-data checks and no I/O/medium/transport errors. | A / R/O | Q S E B D | Q 2m; S 10m / Q 4m; S 20m | Seed/offsets, bytes/errors, throughput. |
| Storage | Read the full accessible block range with no I/O errors or repeat-read mismatches. | A / R/O | E | Media-dependent / estimate +20% | Coverage, unreadable ranges, rate/errors. |
| Storage | Run supported non-destructive device self-tests; inability to start is error unless explicitly unsupported. | A / R/O | S E B | Native duration / advertised +30% | Command/result, completion/failure location. |
| Storage | With a known-good pressed disc, verify optical recognition, seek/read inner and outer areas, and tray/eject. | MIX / R/O | S E B D | 5 min / 15 min | Disc/drive, offsets/rates/errors, eject. |
| Storage | Full-range write/read verification of selected HDD, SSD, NVMe, flash, or removable media. | A / D/W | D | Media-dependent / estimate +25% | Authorization, seed, coverage/errors. |
| Storage | Native sanitize, secure erase, discard, partition, format, or firmware-changing operation. | MIX / D/W | D | Device-dependent / advertised +50% | Command class, authorization, completion. |
| Ethernet | Verify carrier, speed/duplex, addressing, gateway, and packet-error deltas. | A / E/W | Q S E B D | 60 sec/port / 3 min | Interface/driver/MAC hash, link/errors. |
| Ethernet | Exchange deterministic data with an approved local endpoint, verifying integrity and thresholded throughput. | A / E/W | S E B | 2 min/port / 6 min | Endpoint, direction, hash/rate/loss. |
| Wi-Fi | Enumerate bands; scan for approved SSID, associate/authenticate, address, and check loss. | A / E/W | Q S E B D | 90 sec/radio / 5 min | Adapter, band/channel/RSSI, result/loss. |
| Wi-Fi | Exchange deterministic data with an approved local endpoint; grade only in a calibrated setup. | A / E/W | S E B | 3 min/band / 8 min | RSSI, hash/rate/retries/loss. |
| Bluetooth | Discover, pair, connect, and exchange data with a known-good fixture; remove temporary pairing. | MIX / E/W | S E B D | 3 min / 10 min | Controller/fixture, services, cleanup. |
| Network | Check DNS/public reachability only when Internet testing is enabled; grade local results independently. | A / E/W | Q S E B D | 60 sec / 3 min | Scope, endpoint results, latency/loss/portal. |
| Camera | Enumerate cameras and stream supported modes without corruption, timestamp faults, or excessive drops. | A / R/O | Q S E B D | 60 sec/camera / 3 min | Mode, frames/drops/corruption, errors. |
| Camera | Show a standard target; operator assesses focus, exposure, color, orientation, shutter, and LED. | OA / R/O | S E B D | 90 sec/camera / 4 min | Test-frame hash, findings. |
| Sensors | Prompt supported poses and compare orientation events with display transforms. | MIX / R/O | S E B D | 2 min / 6 min | Pose sequence/events/mismatches. |
| Sensors | Check accelerometer, gyro, compass, light, proximity, hinge, tablet-mode, and lid sensors for plausible response. | MIX / R/O | S E B D | 60 sec/sensor / 4 min | Type/range/rate, baseline/delta. |
| Modem | Enumerate WWAN/modem/SIM state and errors without provisioning or transmission. | A / R/O | Q S E B D | 60 sec / 3 min | Modem/driver, registration/SIM state. |
| Modem | With cost/credential consent, attach and test data; calls/SMS require separate operator authorization. | MIX / E/W | S E B D | 3 min / 10 min | Consent, network/signal, session result. |
| Power | Read adapter and battery presence, charge, capacity, health, cycles, and critical telemetry. | A / R/O | Q S E B D | 20 sec / 2 min | Source, energy/capacity, cycles/health. |
| Power | Confirm charge/discharge transition and operation on AC and battery where applicable. | MIX / R/O | S E B | 5 min/state / 15 min | Transitions, energy/rate series. |
| Thermal/cooling | Monitor temperatures, fans, clocks, power, throttling, and shutdown events throughout the run. | A / R/O | Q S E B D | Whole run / +2 min | Sensor series, thresholds/events. |
| Thermal/cooling | Require safe completion/stabilization under load; never grade absent sensors. | A / R/O | S E B | S 15m; E 60m; B repeat / +5m | Ambient if available, peak/slope/events. |
| Firmware security | Report Secure Boot, TPM, measured-boot, and firmware-lock capabilities without provisioning, keys, passwords, or policy changes. | A / R/O | S E B D | 30 sec / 2 min | Capability/state/read errors; policy grade. |
| Expansion | Enumerate PCIe/Thunderbolt/docks, link width/speed, IOMMU group, and driver attachment. | A / R/O | Q S E B D | 30 sec / 2 min | Topology, links, hotplug/errors. |
| Expansion | Hotplug external docks/ports and require all expected child functions after reconnect. | MIX / E/W | S E B D | 3 min / 10 min | Fixture, before/after topology/errors. |
| Clock | Compare RTC/monotonic clocks; grade drift only with trusted reference time and adequate duration. | A / R/O | S E B | Whole run / +2 min | Clocks, reference, drift/discontinuities. |
| Physical | Check chassis, hinges, screws, liquid indicators, port damage, noise, odor, and battery swelling. | OA / R/O | Q S E B D | 3 min / 10 min | Checklist, optional redacted photos. |

Thresholds must be model-, link-, fixture-, and environment-specific. Without a
reviewed baseline, performance is informational; integrity/protocol errors,
impossible readings, and critical health warnings can still fail. Fixture
identity and calibration date are required when they affect grading.

## Status and aggregation

Every scheduled case ends in exactly one state:

- `pass`: execution completed and every applicable criterion passed.
- `skip`: the case did not run because it was not present, not selected,
  unsupported, unsafe, declined, or lacked fixture/configuration. A controlled
  reason and discovered target are required.
- `fail`: valid execution demonstrated a criterion was not met. A later pass
  does not erase an earlier failure.
- `error`: no valid determination was possible because the adapter, tool,
  environment, evidence pipeline, or control channel failed.
- `inconclusive`: observations exist but cannot establish the required
  criterion, such as an unconfirmed visual finding or missing driver.
- `aborted`: work stopped before completion; the stop reason and any completed
  observations remain explicit.

Any required `fail` makes the run fail. In the absence of a fail, a required
`error` makes the run an error. A run passes only when all required applicable
cases pass; optional skips remain visible and reduce coverage. A run with no
applicable cases for its declared purpose is skipped, never passed. Minimum
coverage rules cannot convert `skip`, `fail`, or `error` into `pass`.
Selecting no quick tests therefore records no completed quick-test coverage.
A ready-report marker or passing hardware-graph integrity result does not turn
those skips into a successful hardware qualification.

## Timeouts and recovery

The implemented quick-session coordinator runs workers synchronously under one
shared budget, defaulting to five minutes. The `session -timeout` range is
`10ms` through `15m`; boot scripts select `5m`. Each worker receives the earlier
of the session deadline and its frozen catalog deadline. The graphics limit is
30 seconds across its tools and pattern; display has 45 seconds across all
cards, patterns, and the finding prompt. Shortening a command timeout cannot
extend either frozen limit. Cleanup and stopped-run finalization have separate
bounded allowances.

Keyboard and pointer observation each default to 30 seconds with a 90-second
whole-command limit, also shortened by the session deadline. Each worker shares
one observation window across its selected interfaces. Enabling the existing pointer catalog
row changes the catalog digest, so execution requires a newly frozen plan.
The pointer increment introduced ten worker outcomes in journal v2. The periodic
safety increment introduced journal v3 without changing those outcomes or
the catalog, and the run-wide thermal increment retained ten outcomes in v4.
The current keyboard definition version 2 changes the catalog digest and
requires fresh plans. Journal v5 inserts its eleventh worker between input
readiness and pointer testing. Original v4 journals retain their strict
ten-worker validation; earlier signed evidence keeps its historical meaning.
Direct capture continues to omit both guided workers.

Ctrl-C uses one owned console reader shared with display findings. Linux
SIGINT/SIGTERM also cancel the session. The coordinator stops starting new work,
retains explicit completed failures, and records unfinished scheduled work as
`aborted` or `error`. Excluded and unavailable dispositions remain unchanged.
The input reader must finish and restore its flags and terminal mode before
signing or handing the console to save. A complete failure finding survives a
cancellation race; a partial line or late passing response cannot become a pass.

Linux subprocess cleanup now keeps the leader identity reserved until its
process group has been stopped. Cleanup, child reaping, and output draining
share a two-second allowance. Unconfirmed cleanup stops later graphics/display
work and blocks capture or appliance finalization, while retaining available
diagnostics. This is limited to the owned process group; it does not establish
containment of processes that escape the group.

The signed report's run-integrity evidence includes a validated session journal
containing the frozen plan binding, stop reason, budget and elapsed time, every
quick worker's state and outcome, and ordered mount/network safety snapshots.
The coordinator takes snapshots at
session preflight, before each started worker, and after execution stops. The
finalizer validates that history against the plan, outcomes, and worker evidence.
The current live final gate supplies the fresh pre-sign observation before the
completed v7 journal is written. Missing, contradictory, unsafe, or
incomplete observations block signing. A clean cancellation/deadline can use a
fresh finalization context after confirmed cleanup, but cannot turn an aborted
safety-readiness check or an earlier unsafe observation into permission to sign.
An all-off selection still requires safety observations and records no completed
quick-test coverage.

The periodic safety increment introduced a bounded aggregate summary in journal v3.
One owned observer runs serialized read-only mount/network samples at a
one-second interval, with a one-second observation deadline and a
2,500-millisecond maximum gap. It starts with the coordinator, remains active
through workers, input cleanup, and final safety, then joins within two seconds.
The first unsafe, unknown, late, or excessively delayed sample cancels active
work and blocks signing; safe later observations cannot clear that failure.
Operator cancellation does not turn a clean in-flight sample into a safety
failure, but signing still requires its confirmed completion and monitor join.
The summary records leading and trailing coverage, and a short session before
the first one-second tick records zero periodic samples rather than invented
coverage. Independent preflight, final, and pre-sign snapshots remain required.
Automated and native Linux validation passed, including 1,886 native test events
without skips and 321 race-test events without skips or race reports. Fifteen
real CLI scenarios passed; each 2.5-second session deadline case on modern
x64/x86 and legacy x86 retained two periodic observations, with the signed
summary exactly matching the monitor diagnostic. Rebuilt-image, physical,
broader beta, and release qualification remain separate open gates. Detailed
milestone counts appear in [requirements validation](requirements-validation.md).

The shell independently checks safety before launching the session and after
report creation. These outer checks gate `HHPT_READY` and save; their separate
files are not the signed session history. Both boot scripts also use a terminal
350-second KILL guard around the session command. Any nonzero session exit
blocks ready/save, even if a report and ready file were already written. Killing
the command does not establish nested-worker cleanup, and the failure marker
explicitly leaves cleanup unconfirmed.

Synchronous collectors use cooperative cancellation; a context deadline cannot
preempt a blocked kernel call. The terminal shell guard is a failure path, not a
claim of safe recovery or hardware qualification. Periodic observations are not
continuous coverage. Heartbeat supervision, hang detection, and other profile
execution remain open; the run-wide thermal increment is described below.
Direct capture and finalization without a session journal keep their older
limited evidence scope; they do not acquire session stop/safety claims.

The safemode acquisition increment is tested against these failure requirements:

- Check 64-byte scalar and 4 MiB mount limits, positive reads no larger than
  4 KiB, exact EOF and one overflow byte. Check 4,096/4,097 registry entries,
  batches no larger than 32, cached names and no successful partial registry.
  Invalid counts, no progress, joined errors, duplicates and typed nils must
  fail closed.
- Use actual owned temporary roots and real file identity to prove contained
  sysfs links, final-component no-follow, metadata type checks and matching
  descriptors. Reject escapes, substitutions and final identity changes before
  granting observation credit.
- Compare exact acquired and closed handle sets, including handle-plus-error
  results. Require readable closure before its metadata handle and category
  root closure last. Check cancellation at acquisition and final closure,
  hard-error precedence and no later opens after cancellation.
- Retain independently observed unsafe categories and literal network-prefix
  counts after later read or close faults, including close plus cancellation
  with a third interface never acquired. Discard bytes whose final identity
  proof failed; known unsafe counts must never become a safe result.
- Exercise the actual periodic monitor with the owned producer fault. Keep
  exact-result CLI/session/finalizer composition distinct from a same-run
  kernel fault through signing. Require a valid self-issued signing control,
  the intended rejection reason and no signable output for uncertain or
  promoted safety observations.

Host execution of this increment passed 4,357 test/subtest outcomes across
29 packages, with 17 existing test skips and one empty package. All 50 required
host roots and 311 selected outcomes passed, including 19 new roots and their
133 children. Formatting, vet and Linux amd64/386/arm64 cross-builds passed.
Selected native Linux amd64/386 execution passed 640 test/subtest outcomes
across four suites, including all 108 required root executions and the four new
Linux-only roots on each architecture. No tests failed or skipped, and cleanup
was confirmed. These are owned-fixture results. Physical hardware, rebuilt
images, blocked-kernel deadlines, aggregate allocation, continuous safety and
release approval remain separate gates.

The shared periodic-engine extraction preserves the safemode API, v1 evidence
and existing scheduling/cleanup rules. Six exact wire cases were executed
against the previous implementation before migration. Full host tests, vet and
cross-builds passed; 16 native Linux amd64/386 suite runs passed 2,724 test events,
and periodic/safemode/command race checks passed 1,105 events, with no native
skips. All 15 CLI scenarios passed with confirmed cleanup. Independent checks
reconciled saved log counts, archive hashes and all six final checks. This
qualifies the application change in the VM; fresh images and physical hardware
remain separate gates.

The strengthened local CPU/RAM thermal guard passed Windows tests, vet,
cross-builds, native Linux suites, race checks, and command-line workflows. It
supervises the first observation before load, limits each reading to one second,
waits 250 milliseconds after sample completion before repeating, and allows two
seconds to join. Normal completion waits for the active bounded reading before
freezing evidence. CPU/memory v2 records coverage and cleanup explicitly.
Missing or partial telemetry remains a coverage limitation, even when the
computation passes. This does not provide thermal coverage across other workers
or an all-off selection; a separate session observer now supplies that scope in
development source.

Independent acceptance cases for this prerequisite include:

- An initial blocked, panicking, or invalid observer starts no load; a late
  result cannot restore a successful monitor state.
- Heat arriving during cleanup appears in the returned result. Known CPU/RAM
  failures survive cancellation, thermal failure, and cleanup uncertainty.
- Empty, disabled, malformed, mixed-valid, and disappearing sensor inventories
  produce truthful coverage without inventing temperatures or hiding heat.
- An observer that cannot join within the cleanup allowance forces exit 12,
  preserves available diagnostics, and stops direct capture/session progress
  before later workers or signing, including after an output-write failure.
- Finalization rejects missing cleanup fields, duplicate or case-aliased JSON,
  v1 execution evidence, impossible counts, and existing unreadable evidence.
  A missing-file execution note cannot substitute for malformed existing data.
- Cleanly joined thermal stops retain their observations in verified reports;
  historical signed reports remain verifiable. Native filesystem fixtures
  passed; representative physical sensor validation remains open.

The qualified run-wide v4 increment added initial, one-second periodic and final
thermal observations across the quick session. Its v4 journal binds worker intervals,
preparation and join boundaries, available/partial/unavailable coverage, and
the exact-file and canonical full-evidence digests for local CPU/RAM heat.
The one-use final gate completes after final inventory and before key generation
or signing. Both session monitors must join under one common bounded deadline.
Heat stops work without stopping monitoring; infrastructure failure or uncertain
cleanup blocks signing. Encoding, output and saving occur after this monitored
boundary.

Full Windows qualification passed 1,754 test/subtest events across 22 packages,
with 21 platform skips and no failures; vet, 24 Linux test-binary builds and all
five application targets passed. Independent acceptance cases cover:

- Immediate/all-off initial and final coverage, and initial heat preventing
  worker dispatch without invalidating the independent safety preflight.
- Actual coordinator/monitor/finalizer integration with heat during each of the
  ten workers, input cleanup, final inventory and pre-sign safety; later cooling
  cannot erase a finding, and late heat controls signed and displayed outcomes.
- Local heat surviving unavailable global telemetry, with changed, cooler,
  disappeared, reformatted or rehashed contradictory source evidence rejected.
- Missing/duplicate live gates, false worker timelines, unsafe executed
  readiness, mismatched typed readiness, and malformed or oversized nested
  journal claims rejected by shared validation.
- Both Stop calls receiving one cleanup deadline; terminal failure cannot
  cancel its own cleanup reserve or generate a key before confirmed joins.

Native Linux amd64/386 qualification passed 3,032 test/subtest events across
24 suite runs; four race suites passed 1,203 events, with no native or race skips.
All 15 CLI cases passed, comprising twelve session and three guided-pointer
scenarios. Nine compiler and nine runtime ownership cases, 32 archive cases and
13 shell-supervisor cases passed with confirmed cleanup. The session scenarios
all reported unavailable temperature telemetry. All-off runs retained initial
and final observations; deadline runs retained four observations including two
periodic samples. Rebuilt images, physical sensor behavior, the beta pilot and
release gates remain open. Synthetic heat exercises software responses; it does
not establish a physical cooling or thermal-stress result. See
[requirements validation](requirements-validation.md) for the qualification scope
and resolved test-harness failure.

The keyboard increment preserved these monitoring boundaries in v5.
Its acceptance cases must also establish:

- Six complete press/release pairs on each interface; no credit from another
  keyboard, initially held controls, stale events, repeats or partial frames.
- Final idle, neutral and identity checks for every retained device pass,
  including partial results after cancellation. Finalization binds those passes
  to unchanged frozen components and final inventory.
- One console owner and a completed input-discard handoff before later prompts.
  Ordinary input failure blocks success with exit 1; typed unconfirmed cleanup
  forces exit 12, including after diagnostic-write failure or an earlier error.
- Strict v5 functional evidence and eleven ordered outcomes, with duplicate,
  escaped, case-aliased or hidden claims rejected even without bundle views.
  Actual observations require the current live finalization gate. Standalone
  frozen-plan finalization may use exact nonexecution notes only for
  `excluded`, `not_present`, `unsupported` or `missing_driver` keyboards.
- Unchanged original v4 fixture interpretation and no stale keyboard evidence
  in an excluded or never-started row. The 21 catalog rows plus run integrity
  still produce 22 report results.

Keyboard software qualification passed independently of the earlier v4
milestone. The [current validation record](requirements-validation.md) gives its
own host, native, race and CLI counts; rebuilt images and physical devices remain
open. Positive heat fixtures require accepted safety coverage before inducing
heat. Fake-clock success cases acknowledge the exact due timer before advancing
time. The zero-first-tick negative still requires a sampling-gap failure, and
late or withheld-timer negatives retain their original guards. These fixture
changes do not relax production deadlines, observation requirements or cleanup.

Regression acceptance for the current coordinator requires:

- Sequential tools and multi-card display work stay within the complete frozen
  test deadline and the shared run budget; child cleanup has its own finite
  allowance and cannot leave a worker running after a claimed stop.
- Cancellation during computation, subprocess output, and operator input stops
  new scheduled work without consuming another prompt's response or leaving a
  console reader behind. Excluded and unavailable outcomes remain unchanged.
- Completed failures survive cancellation and timeout. Every unfinished
  scheduled test receives an explicit incomplete outcome; missing or partial
  evidence cannot become a pass.
- Stopped-run finalization uses a fresh bounded context after cleanup and
  preserves the original plan binding. It does not broadly admit `aborted`
  readiness outcomes or erase earlier unsafe observations to reach signing.
- Independent mount/network checks remain active when readiness tests, or all
  quick tests, are excluded. Signed session history cannot omit a required
  observation, and unsafe or incomplete final observations still block the
  normal ready/save path.
- Periodic monitoring permits only one observation in flight. Unsafe-to-safe,
  unknown-to-safe, timeout-to-late-pass, missed-tick, and cancellation races keep
  the first failure and prevent signing. A blocked observer cannot be replaced
  by overlapping observations, and an uncertain join remains terminal.
- Coverage validation rejects missing or contradictory timing, excessive
  leading/trailing gaps, fabricated zero-tick coverage, and an older execution
  journal. Clean operator stops preserve completed failures after a confirmed
  monitor join; all twelve v7 worker outcomes remain present. Original v4
  journals retain their ten-worker rules and historical report verification.
- Deterministic monitor fixtures control observation completion and time rather
  than relying on sleeps. Native checks use private mount/network namespaces;
  unsafe transition fixtures do not require changing real device mounts or
  bringing up real network interfaces.
- Terminal watchdog failure or invalid ready metadata blocks ready/save even
  when report artifacts exist. Shell fixtures exercise only the handoff; Go
  tests cover worker scheduling, cancellation, and journal validation.

The scalar CPU increment adds twelve fixed binary64 vectors to the bounded
worker. Its acceptance cases include:

- Independent bit oracles for addition, subtraction, multiplication and
  division, with first/last and later-suite faults on a nonzero worker.
- Stop before launch, between known-answer operations, during every scalar
  operation and after a complete suite. Successful late work gets no credit;
  an observed late mismatch remains a failure through cancellation and cleanup.
- Ordered worker rows, exact cadence and checked counter sums. Distinct integer
  and SHA failures require distinct eligible workers; a scalar mismatch cannot
  serve as the same worker's earlier failure. Overflow cannot regain a signable
  schema after a later diagnostic change.
- Historical CPU v2 typed hashes and v4/v5 interpretation, current CPU v3 with
  definition 2, and rejection of unknown, mismatched, duplicate or hidden claims.
  Shared validation must also cover bundles without HTML/PDF views.
- Real coordinator/finalizer tests retaining scalar failure details through
  operator cancellation and local heat, without later work or signing before
  confirmed monitor joins.

Focused Windows regression checks exercise these boundaries. A controlled
cancellation-propagation check also verifies that the coordinator test's wait
uses an independent harness timeout and retains every numeric assertion.
The full Windows suite passed 2,543 test/subtest events across 25 packages, with
17 platform test skips and three packages without host tests. Vet, 28 Linux
test-binary builds and five application builds passed. Native amd64/386 ran
28 suites with 4,608 passing events; six race suites passed 1,745 events, with
no native skips. All 30 CLI scenarios passed, including six standalone/session
CPU capture and signed-finalization cases on amd64, x86 SSE2 and legacy
soft-float. Rebuilt-image checks and physical CPU coverage remain open. Legacy soft-float results establish software arithmetic
coverage only; cross-compilation does not establish native AArch64 execution.
At that milestone, broader CPU stress, instruction coverage and performance
grading remained planned. The bounded standard CPU-stress increment below has
its own implementation and qualification requirements.

The RAM moving-inversion acceptance cases include:

- Independent byte and counter oracles for all ten phases and six read/write
  sweeps, with forward/reverse traversal and first/last-byte or chunk-tail faults.
- Final-only corruption at offset zero, later-pass faults and overwrite-before-
  verification controls. A logical alias model and equivalent corruption between
  real small-span calls do not imply physical aliasing between Go slice indices
  or detection of every hardware fault.
- Stop before allocation, exact deadline equality, phase/chunk boundaries and
  provisional completion. Correct late work remains uncredited; a late mismatch
  remains a failure through heat, cancellation and joined cleanup.
- Checked 64-bit arithmetic, partial and fully uncredited terminal chunks,
  explicit offset-zero mismatch and impossible phase-prefix rejection. Negative
  or overflowing availability values cannot wrap into a large recommendation;
  the unreadable-file fallback remains 4 MiB.
- Historical v2 canonical bytes, nested safety and v4/v5 typed hashes; current
  definition 2/v3 pairing; direct/session/finalizer and comparison paths; bundles
  with or without views; copied HTML/PDF summaries.

Focused Windows checks passed 2,082 test/subtest events across nine packages,
with sixteen platform test skips and no failures. Real 4 MiB runs exercise
capture and command/session/finalizer paths; completed-result fault fixtures
separately exercise signed failure boundaries. Full Windows validation passed
2,755 test/subtest events across 25 packages, with seventeen platform test skips
and three packages without host tests. Vet, 28 Linux test-binary builds and five
application builds passed. Compiled x86 inspection confirms buffer accesses and
reverse indexing, not physical DRAM traffic or cache bypass.

Native Linux validation passed 5,000 test/subtest events across 28 amd64/386
suites, with no skipped tests or failures. Six race suites passed 1,894 events,
also without skips or failures. All 39 CLI cases passed across amd64, x86 SSE2
and legacy x86, including nine RAM cases covering signed standalone finalization,
memory-only sessions and direct capture. Modern standalone/session cases use
64 MiB and two passes; legacy cases use 16 MiB and one pass. Direct-capture
cases use 4 MiB and one pass. The 30-second RAM worker limit is unchanged,
and cleanup and terminal restoration were confirmed. VM thermal coverage is
explicitly unavailable. AArch64 is cross-built only; rebuilt images, physical
memory, SPD/ECC, extended stress and Memtest86+ remain open.

The broader target policy remains:

- Unless the matrix says otherwise, adapter startup times out after 30 seconds
  and an operator prompt after 120 seconds. The implemented quick limits above
  take precedence for current commands.
- Automated workers heartbeat every 15 seconds. Two missed heartbeats trigger
  cancellation; failure to stop after 15 more seconds triggers termination and
  target isolation. This heartbeat policy is not yet implemented.
- Timeout is `error` when completion was needed for diagnosis. It is `fail` only
  when response time is itself the criterion and target evidence is conclusive.
- Retries must be declared before execution. All attempts remain in evidence;
  recovery may mark a defect intermittent but cannot hide it.
- Hang, reset, disconnect, critical thermal state, uncorrected error, or unsafe
  condition stops affected tests. Resume requires an explicit safe state;
  continuous monitoring and general recovery remain future work.

## Linked Retest Validation

Use independently encoded canonical archives and envelope signing inputs to
preserve v1 trusted/self-issued profiles with absent, HTML, PDF and both views.
For v2, re-sign malformed fixtures so rejection reaches the intended semantic
boundary: exact profile fields, both digest grammars, product identity and
canonical UUIDv4 even without views. Check complete regenerated views and
static reference wording separately from local trust output.

Construct two independently valid parents whose manifest digests match but
whose signed IDs/times or trusted signing keys differ. Assert that precondition
before checking rejection of the substituted full-bundle hash. Exercise each
child/parent trust combination, wrong keys, equal child/parent IDs, foreign
legacy-product parents, child-only unchecked references and an already-linked
parent without fetching its grandparent.

Route each artifact/key role through the real bounded acquisition body with
independently injected metadata/readable Close faults and exact ownership
ledgers, including cancellation. Linux/Windows fixtures also exercise the
actual platform adapters; unsupported OSes must reject. These tests use owned
regular fixtures, not private inputs or physical devices.

Exercise the real session adapter, consumed live gate, finalizer, signer and
bundle verifier together. Preserve known current failures and plan bindings;
reject parent failure before workers, invalid child IDs, reused gates, unsafe
pre-sign observations and uncertain cleanup. A clean operator stop can use the
existing fresh integrity context. Standalone cancellation suppresses output.
Check exclusive outputs, caller-owned stdout, comparison policy and verified
bytes passed to a fake persister. Injected output completion is not a claim of
kernel-close faults, fsync durability or installed-media qualification.

The closed 2026-10-28 host run recorded 4,804 passing test/subtest outcomes
in 29 packages, 17 skipped tests and one empty package. The selected native
cmd, bundle, render and report suites ran on both Linux amd64 and 386:
698 outcomes, all 110 required root executions and 349 required named
outcomes per architecture passed with no failures or skips. Native event
conversion reused two previously qualified test2json tools; those tools were
not rebuilt from the linked-retest source. Cleanup, nine owned-resource
cases, 32 archive cases and six root postchecks also passed. These are
software fixture results, not installed-CLI or physical-media outcomes.

## Comparison Input Ownership Validation

Establish trust, profile and identity preconditions with independently verified
signed fixtures before injecting input faults. The comparison integration
exercises both report roles and both public-key roles through the actual checked
acquisition body. Readable and metadata Close faults, including concurrent
cancellation, require exact acquired/closed owner counts and close order, fixed
diagnostics, no later input acquisition and no comparison output. The default
after-key path is acquired again. Literal matched rows and full signed reference
fields guard the valid comparison oracle; shared loader tests alone do not
establish command integration.

Malformed providers, rejected metadata, changed readable identity, short/invalid
read progress and the exact-completion probe are exercised through the shared
core. Context cases cover real dispatch pre-cancellation, every closed-capture
role, acquisition/read/final-metadata/Close cancellation and hard-error priority.
The entered-writer case cancels before its exclusive-output adapter opens, then
checks cancellation at callback entry: zero payload, one checked Close and an
allowed empty destination. It does not claim rollback or an interruptible
arbitrary blocking writer. Retained output-error diagnostics and caller-owned
stdout remain part of the contract.

Trusted and self-issued viewless v1/v2 fixtures cover after-key defaults and
overrides, explicit false Boolean semantics, strict result/reference decoding,
HMAC key/epoch incompatibility and unresolved identity. Wrong keys, hostile
signed product/payload/policy fields and noncanonical archives must fail with
fixed input diagnostics in either report slot. Real Linux/Windows regular-file
command paths preserve all source bytes, reject an existing output and do not
close caller-owned stdout. Unsupported platforms fail without an unchecked
reader fallback. Existing loader/platform and typed-evidence tests are retained
coverage, distinct from the new comparison integration cases.

The final isolated Windows run passed 4,875 test/subtest outcomes and 29 packages,
including all 36 selected roots, with 17 test skips and one skipped package.
All eight commands and eight postchecks passed. The selected Linux amd64/386
comparison and retained report cohort passed 938 outcomes across eight suites,
all 120 required root executions and 469 required named outcomes per architecture,
with zero failures/skips and confirmed cleanup. Nine owned-resource cases,
32 archive cases and six root postchecks also passed. These are software fixture
results, not installed-CLI, physical-media, blocked-kernel deadline, kernel-close
fault or fsync/durability qualification. Existing image/physical/release gates
and the remaining legacy key/persistence input-ownership work remain separate.

## Capture Input Ownership Validation

Capture tests exercise both key roles through the actual owned acquisition
body. Metadata/readable Close faults, including concurrent cancellation,
require exact acquired/closed owner ledgers and close order, fixed diagnostics
and no later key, randomness, collection or output. The first key's owners
close before the second acquisition. Valid controls independently verify the
signed artifact and retain the limited direct-capture coverage, HMAC/epoch,
session-ID and worker/output policy. Uncertain worker cleanup retains exit 12.

Finite-role cases cover exact 32-byte fingerprint/public inputs and the
64-byte private key. A malformed successful reader is rejected before seed
access; a valid fingerprint followed by an independently inconsistent 64-byte
private key reaches the intended consistency check. Exact final data with EOF
still requires the overflow probe. Provider, identity, malformed-progress,
Close and cancellation cases use the shared core; command cases also check
required dependencies and cancellation before later effects. They do not add
a late worker/signing policy or an interruptible arbitrary OS call.

Portable injected-owner coverage is distinct from real Linux/Windows file
acquisition. Retained worker/output fixtures use injected owned inputs only
where real acquisition is unsupported; production has no unchecked fallback.
Two unsupported-only candidate roots were not executed by the Windows/Linux
qualification runs.

The isolated Windows run passed 5,004 test/subtest outcomes and 29 packages,
including 28 changed-file host roots and all 44 broader-contract roots, with
17 test skips and one skipped package. All eight commands and eight postchecks
passed. Selected Linux amd64/386 execution passed 1,074 outcomes across eight
suites, all 100 required root executions and 537 required named outcomes per
architecture, with zero failures/skips and confirmed cleanup. Nine runtime
ownership cases, 32 archive cases and six root postchecks passed as well.
These measurements are software fixture evidence, not installed-CLI,
physical-media, kernel-close fault, blocked-kernel deadline, key-erasure or
fsync/durability qualification. Remaining raw-key consumers and persistence's
live source owner stay outside this increment; existing release gates remain.

## Finalizer Fingerprint Input Ownership Validation

The finalizer consumer tests establish a valid planned report before injecting
key faults. They use the actual shared owned-input body for metadata/readable
Close faults, concurrent cancellation and a representative acquired-owner-plus-error
case. Exact acquired/closed ledgers and readable-before-metadata order accompany
fixed diagnostics and zero later random, collector, clock, gate and destination
effects. Malformed successful byte lengths and cancellation after closed
successful acquisition have separate consumer checks. The existing primitive
protocol matrix remains distinct from these integration witnesses.

Otherwise-valid explicit-parent and verified-summary fixtures expose both early
typed-nil-context boundaries. The immutable-parent test permits exactly one
fingerprint path/role read and rejects every parent reopen. Independent fixed
HMAC vectors, signed references, raw-key absence and current result fields
prevent self-derived positive oracles. Retained real session-adapter coverage
keeps clean worker stops separate from a canceled finalization context and
preserves consumed/unsafe-gate, completed-failure and cleanup-exit-12 policy.

Portable injected fixtures do not qualify an unsupported real adapter. The
isolated Windows run passed 5,053 test/subtest outcomes and all 121 required
host roots, with 29 packages passing, 17 test skips and one skipped package.
All eight commands and eight postchecks passed; the unsupported-only candidate
root was not executed.

Selected Linux amd64/386 execution passed 1,934 test/subtest outcomes across
eight suites and 256 root executions. Each architecture passed all 128 selected
roots and 967 required names, including the four symlink leaves skipped by the
Windows host. Zero native failures or skips occurred, and cleanup was confirmed.
The eight explicit nested output-command groups per architecture passed 144
child-start ordering checks in total; slash characters alone do not create an
intermediate parent event. Nine ownership cases, 32 archive cases and six root
postchecks also passed. This software fixture evidence does not qualify
unsupported real adapters, installed-CLI execution, physical media, kernel-close
faults, blocked-call deadlines, secure key erasure, fsync/durability or release.
Existing image, physical-PC and release gates remain open.

## Report-target Fingerprint Input Ownership Validation

The authored command tests start with valid flags and synthetic key/result
fixtures for both catalog and authorization modes. The catalog positive compares
the complete decoded data. The authorization positive calls the real pure
producer and compares its complete result with a fixed independent HMAC,
device binding, boot lifetime and output policy vector. Neither positive relies
only on exit status or a nonempty body.

Consumer faults run through the actual shared owned-input core. Separate
readable and metadata Close failures, including concurrent cancellation, require
exactly-once acquired/closed ledgers and readable-before-metadata order. Acquired
owners returned with errors, malformed owners, object identity, hard read and
overflow-probe failures must stop all discovery, authorization and output.
Malformed successful lengths and cancellation after closed capture separately
test the command boundary. Raw key bytes, input paths and provider markers must
not appear in successful public output or fixed acquisition diagnostics.

The tests preserve usage ordering, literal arguments, required dependencies and
both result policies. Non-pass catalogs remain serializable; authorization and
output failures retain their existing diagnostics. Real destination helpers
exercise exclusive creation, existing-file preservation, write/short-write/Close
failure and caller-owned stdout. No new cancellation policy applies after a
consumer begins.

Linux/Windows regular-file cases cover 31, 32 and 33 bytes with injected
consumers, avoiding host-media discovery. Real dispatch rejects non-regular
input. Other OSes have an explicit unsupported-input case, not a portable
production fallback. Ten portable and two supported-platform roots require
106 source-declared names; slash-containing child labels introduce no nested
group by themselves. The native policy selection passed ten relevant existing
report-target roots and 16 names per architecture, including nine Linux-only
roots, separately from persistence and mount validation.

The Windows host run passed all 106 required command names within 5,159 test
passes and 29 package passes; 17 tests and one package were skipped. Eight host
commands and eight postchecks passed. Linux amd64 and 386 each passed the exact
122 selected names and 22 roots, totaling 244 named passes, four suites and
44 root executions. There were no native failures, skips, extra named outcomes
or declared nested groups. Nine runtime ownership cases, 32 archive cases and
six root postchecks passed, with cleanup confirmed.

The other-OS rejection fixture remains unexecuted. This evidence does not
qualify installed-CLI execution, physical-media operations or persistence/mount
validation. Existing image, physical-PC and release gates remain open.

## Save Fingerprint Input Ownership Validation

The host run passed 5,240 test/subtest outcomes across 29 packages, with 17 test
skips and one package skip outside the selected save set. All 130 selected save
names passed, including 81 new and 49 retained names. Native Linux `amd64` and
`386` each passed the same 130 names and 28 roots: 260 outcomes, two suites and
56 root executions, with no failures or skips and cleanup confirmed. Slash-rich
labels are direct `t.Run` names; this selection has no nested groups. The
other-platform-only rejection root was not executed in this qualification.

Consumer cases satisfy usage and audit preconditions before testing the key
boundary, pass the original context/path and exact fingerprint role, and compare
complete positive save outcomes. They use the real owned-input core with
injected owners for readable and metadata Close failures, acquired-owner-plus-
error returns, malformed providers, hard read/probe faults and cancellation.
Exact ledgers verify acquisition and that the readable handle closes before its
metadata handle. Source-order review plus paired valid-report/missing-report
precedence cases establish rejection before report access. Observed console
input, menu and discovery/authorization/mount/persistence effects remain absent
on key failure; there is no direct counter for every internal report open.

Only nil bytes plus the exact cancellation/deadline error equal to the context
error use the clean stop. Non-nil bytes with errors, wrapped/joined errors,
invalid successful lengths and uncertain closure retain fixed input-failure
diagnostics even with cancellation. Cases cover malformed contexts, required-
reader admission, pre-read cancellation and cancellation after successful closed
capture. Earlier audit initialization and its deferred closer remain observable;
input rejection does not prevent all earlier effects or improve audit closure.

The tests retain report verification, confirmation/menu, retry, write-attempt,
unmount and receipt oracles, including exact legacy and linked report bytes.
Real regular-file size cases run on supported platforms with downstream media
operations injected; production dispatch rejects a non-regular key before a
missing report. Key/path/provider-marker disclosure is rejected in new input
diagnostics. A test-only owned-input adapter provides no production-platform
credit.

These fixtures do not establish installed-CLI execution, unsupported-platform
acquisition, physical media, kernel-close failures, blocked-call deadlines,
secure erasure, rollback or durability. Three legacy raw-key calls remain after
this source change; audit, frozen-plan/evidence and live report ownership remain
separate work. Existing image, physical-PC and release gates stay open.

## Persistence Key Input Ownership Validation

The completed consumer cases and retained regressions were qualified on the host
and native Linux amd64 and 386. The host recorded 5,374 test/subtest passes across
29 packages, with 17 test skips and one package skip; all eight commands and eight
postchecks passed. All 28 selected roots and 222 required names passed. The two
native suites recorded 444 passes and 56 root executions, with zero failures or
skips. Each architecture passed 134 new consumer names and 88 retained names.
Selection was derived from the completed source and host outcomes; it contains
no nested groups. Ownership/archive checks and all six native postchecks passed,
and cleanup was confirmed. The other-OS rejection root remains unexecuted.

Positive cases must first establish valid authorization and independently verified
current v1/v2 report fixtures in both self-issued and trusted modes. Compare the
exact original context, literal paths, finite roles, complete authorization and
fingerprint bytes, still-open source bytes/size/digest, explicit/default name and
one complete receipt. Use an independent authorization HMAC vector; a permissive
injected persister proves forwarding, not MAC or physical-target enforcement.
Same-path trusted keys still require two acquisitions. Keep the original linked-
report and receipt/durability-rejection caller assertions.

Exercise both key positions through the actual owned-input core with injected
owners: readable and metadata Close faults, acquired-owner-plus-error returns,
malformed owners, hard read/probe failures and cancellation. Exact ledgers must
establish completion prerequisites, each owner's single closure and readable-
before-metadata order. At the command boundary, returned errors and wrong lengths
must win over later cancellation, including pure or joined context errors and
bytes-plus-error results. Persist has no save-style clean-cancellation exception.
Also distinguish pre-stopped/invalid context and missing reader/persister admission
from earlier malformed authorization, and from cancellation after valid closure.

Retain source-order review and paired valid/missing-report precedence witnesses
for first-key rejection, plus observed reader, persister and receipt effects.
There is no dedicated report-open counter: trusted second-key rejection follows
an already-open report. Wrong trusted keys/IDs/signatures must reach the real
verifier with otherwise valid prerequisites. Preserve downstream error and
incomplete-receipt rejection, exclusive-output collision and caller-owned stdout
failure behavior; output failure after persistence does not imply rollback.

Supported Linux/Windows ordinary-file fixtures must isolate the real key loader
from media persistence with an injected persister. Any real-dispatch negative
needs barriers that prevent a media write. Other-OS rejection or a test-only
forwarding adapter gives no supported acquisition credit. The remaining deployment
raw-key call and authorization/audit/frozen-plan/evidence/live-source owners stay
separate. These tests add no installed-CLI, physical-media, kernel-close,
blocked-call deadline, secure-erasure or durability claim; existing image,
physical-PC and release gates remain open.

## Deployment Discovery Key Input Ownership Validation

Host validation passed 5,473 test/subtest outcomes across 29 packages, with 17
test skips and one package skip; all eight commands and eight postchecks passed.
The selected 12 roots and 101 named outcomes passed. Native Linux amd64 and 386
binaries then passed the same 101 names and 12 roots each: 202 named outcomes,
two suites and 24 root executions, with no failures or skips. Nine ownership
cases, 32 archive cases and six root postchecks passed, with cleanup confirmed.

The source-derived selection includes 99 changed-file names and two directly
relevant retained policy names per architecture. There are no declared nested
groups; slash characters in literal labels do not invent intermediate parents.
These are this increment's outcomes, not borrowed persistence results.

Establish successful prerequisites independently: a token record with a literal
HMAC expectation, complete deployment record, exact activation fields, fixture
exclusions, shared confirmation reader, success audit and stdout. Verify original
context/path/finite role and the accepted key forwarded to discovery. Inject audit,
media and activation operations so a successful command fixture cannot touch real
media or write activation files. Keep eligibility and fixture-ID regression checks.

Exercise key acquisition through the real owned-input core with explicit metadata
and readable owners. Require ledgers proving each owner closes once, readable
before metadata, with read and terminal-probe prerequisites reached before their
faults. Cover malformed owners, handle-plus-error, read/probe and Close errors,
wrong successful lengths and cancellation. Hard or shape faults must precede
post-read cancellation; exact context errors are also input failures. Distinguish
pre-stopped/invalid context, missing operations and cancellation after valid
closure from earlier usage or audit failure.

Observe that failed key admission prevents activation availability, selection,
confirmation reads, token loading and activation writes. Preserve the existing
post-key order and downstream error text. Test confirmation rejection, token and
fixture failures, activation failure and retained unmount calls without claiming
that ignored unmount or audit-close errors have become checked. Success still
leaves the token media mounted read-only. This is not an output-fault fix.

Linux/Windows ordinary-file cases must exercise exact key length through the real
loader while injecting later effects. Real wrapper/dispatcher negatives must fail
before device access; the other-OS rejection root remains unexecuted. The final raw-key caller/definition removal needs an
exact source census, not a keyword-only claim of complete ownership. Audit/token,
mount, activation-output, authorization, frozen-plan/evidence and live-source
owners remain separate. No installed-CLI, physical-media, kernel-close, deadline,
secure-erasure, rollback or durability credit is implied; existing image,
physical-PC and release gates remain open.

## Bounded Exterior Inspection Validation

Core tests enumerate all 16 admitted finite evidence tuples and independent
reducer cases. They reject ambiguous shape, invalid scalar types, out-of-range
observation time and inconsistent completion/finding/status. Command tests
distinguish complete CR/LF input, incomplete EOF and conservative empty broker
results. Wrapped or joined clean EOF/context leaves stay clean; any hard sibling
wins. Returned cancellation is retained even if outer notification lags.
Exact-deadline and late decisions cannot pass; a complete defect stays adverse.

Timing tests measure prompt attempt through decision, exclude owner preparation
and join, and cap credit at the earlier effective deadline. The Linux broker
test forces the actual cancellation branch using handshakes and a real queued
overflow, without consuming or requeuing the result in the test. Request-only
overflow still has a clean actual final owner receipt. Separate persistent-input
and restoration faults reach the real failed finish receipt.

Plan tests require whole-device operator applicability without invented
hardware. Integration tests reach the actual coordinator, fresh live final
gate, finalizer, signing and companion verification. They check owner closure
before effects, no signing on invalid evidence, retained defects, exclusions
and missing-row rejection. Legacy v4/v5/v6 fixtures remain literal data.

Independent bundle encoders correctly re-sign malformed evidence and omit
views, so signature success cannot replace semantic validation. Ordinary and
linked profiles, trusted and self-issued choices, and optional views are
covered. A valid alternate parent with the same manifest content but a
different canonical bundle is rejected by pair verification. Rendering uses
the shared validated result; absent-section bytes and identified-component
comparison rules stay unchanged.

The complete host run passed 5,942 test/subtest outcomes across 30 packages,
with 17 test skips and one skipped package. Formatting, vet, three Linux
application cross-builds and all eight postchecks passed. The selected native
Linux amd64/386 cohort passed 4,580 named outcomes in 16 suites, covering 836
root executions. Each architecture passed exactly 2,290 required names and 418
roots, with no failures, skips or extra names. The 15 source-declared groups
per architecture and all 252 direct-child ordering relationships were checked;
literal slash labels were not treated as extra groups. All 43 selected
Linux-only names and four names skipped on Windows passed on each native
architecture. This includes the real Linux broker and owner fixtures.

Nine ownership cases, 32 archive cases and six postchecks passed, with cleanup
confirmed. These are fixture suites, including retained regressions; they are
not installed-command or race runs. Other-platform console behavior, rebuilt
images, physical inspection, actual media access, kernel-close/fsync fault
injection and output durability remain outside this evidence. See
[requirements validation](requirements-validation.md#bounded-exterior-inspection).

## Power-Supply Inventory Validation

The complete feature passed 6,239 host test/subtest outcomes across 30 packages,
with 17 platform test skips and one skipped package. All 30 changed-file roots
and 297 required names passed once. Formatting checks, vet, Linux amd64/386/arm64
application builds, eight commands and eight postchecks passed.

The selected native Linux amd64/386 cohort passed 3,100 named outcomes across
14 suites and 582 root executions: all 1,550 names and 291 roots per architecture,
with no failures, skips, extra names or duplicates. Both architectures passed
the four Linux-only descriptor/link/FIFO roots and the collector fixture skipped
on the Windows host. Nine ownership cases, 32 archive cases and six postchecks
passed, with confirmed cleanup. These runs add no race-detector, installed-CLI,
physical-device, image, durability or release qualification.

The source contract requires full instance enumeration, including names with
identifier canaries and rows whose fields are all unavailable. Missing and empty
supply classes must reproduce the literal pre-power payload. Ordinal IDs must
contain no raw name or digest, and raw names, serials and rejected field bytes
must remain absent from payloads, views, signed artifacts and diagnostics.
Absence of rows cannot be asserted as physical absence.

Independent field oracles must cover all eleven values and five states, exact
enum labels including `Unknown`, clean unsupported text, malformed text and
numeric boundaries. Include online 2, cycle count 0, signed energy/charge extrema
and full greater than design. Parse before normalization; repeated internal
enum spaces are unsupported, while edge spaces and controls are malformed.
An authentic data-plus-read-error fixture must discard favorable partial bytes
and report unavailable. Ordinary read unavailability must stay distinct from
existing protocol, bounds, checked-close, cancellation and directory failures.

Plan and comparison fixtures must vary each finite value/state only for the
new type, retain existing shared and other-type behavior, and reject blanket
filtering of unknown power attributes. Literal independent HMACs and complete expected rows establish an
identified control alongside unavailable power identity. A renamed private
entry at unchanged count must not be presented as detectable replacement or
physical continuity. Expected comparison results must not be generated by the
comparison implementation itself.

Downstream fixtures use real ordinary-file collection through payload creation,
finalization, signing, linked parent verification, no/both views and command
comparison. Incorrect views and forged power identities need independently
valid signed prerequisites before their respective consumer rejects them.
Signature success alone is not a generic strict power-attribute validator.
Retain fixed historical payload/view oracles and unchanged catalog/session
fixtures. Consumer composition with the real collector is separate from
injecting every hidden owner fault at each consumer boundary.

These are fixture requirements, not evidence of hardware battery condition or
power transitions. They do not imply installed-CLI, unsupported-platform,
physical-PC, rebuilt-image, kernel-close/fsync or durability qualification.
Existing release gates remain open; see
[the field and validation contract](requirements-validation.md#power-supply-inventory).

## Firmware and TPM Inventory Validation

The complete feature passed 6,948 host test/subtest outcomes across 30 packages,
with 17 platform test skips and one skipped package. All 31 new roots and
216 required names passed once with parent ordering checked. Formatting, vet,
Linux amd64/386/arm64 application builds, eight commands and eight postchecks
passed.

Selected native Linux amd64/386 fixtures passed 3,886 named outcomes across
14 suites and 736 root executions: all 1,943 selected names and 368 roots per
architecture, with no failures or skips. Nine ownership cases, 32 archive cases
and six root postchecks passed, with confirmed cleanup. These runs qualify
selected software fixtures; they do not establish physical firmware behavior,
unsupported-platform execution, installed-CLI, race or durability coverage.

Literal parser and collector oracles cover every finite field/state, raw ASCII
before normalization, explicit non-ASCII input, EFI little-endian attributes,
malformed lengths, unsupported values and conflicting 1/1 flags. No-data fixtures
preserve literal earlier payloads and unchanged-renderer views. Completed invalid
observations retain state-only rows, every enumerated TPM interface retains an
ordinal, and all-unavailable EFI observations produce no row. Private-name and
rejected-byte canaries must not appear in public results.

Authentic scalar fixtures reach read/probe and checked-close failures for each
role, including favorable data-plus-error discard. Directory batches, cached
names, terminal probes, final-field cancellation and concurrent collectors have
separate witnesses. Owned-core fault ledgers establish the acquired resources
and exact operations reached; aggregate PathSource failures establish a narrower
consumer boundary. Neither is a claim that every hidden owner fault is injected
at every consumer.

Independent graph/identity HMAC literals and exact comparison rows bind each
new finite value/state and retain an identified control alongside unavailable
firmware/TPM identity. Ordinal shifts and same-count replacement must not be
upgraded to physical identity. Every new field remains in the graph rather than
inheriting power telemetry's volatility policy.

Consumer fixtures use the real collector with independently encoded signed
legacy and linked reports, valid trusted/self-issued prerequisites, absent or
both views, and re-signed semantic-view failures. Actual finalization preserves
an unchanged-configuration positive and a signed failed-integrity configuration
delta; real signed comparison checks exact references and rejects forged
identities. Ordinary-file nonregular and oversized scalar failures reach the
finalizer before entropy or output, with explicit tripwires. Signature success
alone is not a generic finite-attribute semantic validator.

The broader firmware-security matrix row still requires measured-boot and
firmware-lock coverage and policy grading. Its worker budgets are not fulfilled
by these four inventory fields. This slice adds no TPM commands, provisioning,
firmware-policy changes, physical-device, installed-CLI, image, race or durability
qualification. See [requirements and limits](requirements-validation.md#firmware-and-tpm-inventory).

## Bounded Standard CPU Stress Validation

The new standard path must be qualified as a consumed workflow, separately from
retained quick-worker tests and synthetic evidence fixtures. Default quick and
explicit standard need independent profile/plan admission cases. Standard alone
uses thirteen workers and v8; historical quick v7, v4/v5/v6 wire bytes, old exact
supervisor notes and absent report sections must retain their prior meaning.

Core acceptance needs independent known-answer bits and a controlled nonzero
worker at the readiness barrier. Each launched worker reports readiness or
termination; failure/cancellation must wake parked peers and join every owner.
Check lifetime cadence separately from preparation and accepted load deltas.
One ready iteration and twelve checks do not establish sustained participation;
each worker needs additional load iterations and twelve further scalar checks.
The valid iteration-256/check-24 boundary must survive a late recurrence without
being rounded into an extra iteration.

Test initial unavailable/partial coverage before load, later coverage loss,
timeout followed by valid heat, mismatch followed by cancellation, failed cleanup
and overflow. Exercise exact and fractional deadline boundaries against both
real monotonic time and serialized offsets. No shortened parent budget or
599,999.999 ms observation may become a 600-second pass. A real-duration run is
separate from injected-clock tests and still does not prove CPU utilization or
physical sensor coverage.

Consumer cases require valid strict prerequisites before changing one relation:
worker timestamps/counts, finite facts/status, journal order, source hashes,
source-null/dispatched state, deep copies and matching profile/plan. Independently
re-signed no-view bundles must reject opaque pass/fail/error/aborted rows; exact
historical failure notes remain positive controls. Real standard coordinator and
finalizer paths must preserve adverse facts through unlinked/linked signing and
canonical HTML/PDF. Identified hardware and literal HMAC/classification controls
keep comparison acceptance independent of the implementation under test.

The complete source passed the host suite: 6,732 test/subtest passes across
30 packages, with 17 test skips and one package skip. All 493 required named
outcomes across 69 selected roots passed. Selected native tests passed on Linux
amd64 and 386: 6,040 test/subtest outcomes across 18 package executions, covering
517 roots and 3,020 names per architecture, with no failures or skips.
The native selection covered 124 whole test files across nine packages, including
all changed test files and relevant retained compatibility files. Every required
name ran and passed once per architecture, including 15 Windows-skipped and 23
Linux-only names. All genuine parent-group orderings were retained.

A separate API-level qualification completed a real shared 600-second load
window on Linux amd64 and 386, using two workers and fixed synthetic temperature
observations of 35 degrees C. Both runs passed with cleanup confirmed. This
establishes elapsed-window completion and checked worker participation within
that fixture. CPU time, utilization, all-core saturation and physical thermal
behavior remain unqualified.

The duration test calls the production stress API and strict decoder directly. Its two executions are
separate from the ordinary native fixture totals above. Race, installed-CLI, rebuilt
image, physical-PC/sensor, durability and release evidence remain separate.
See [the bounded requirements](requirements-validation.md#bounded-standard-cpu-stress).


## Evidence contract

Each case emits a structured result containing:

- run, case, component, adapter, profile, configuration, and schema versions;
- component identity/fingerprint and relevant topology;
- UTC and monotonic start/end times, duration, boot identity, and attempt;
- mode, access class, prerequisites, criterion, and timeout;
- status, controlled reason, observations, units, threshold, and threshold
  source;
- bounded stdout/stderr, relevant kernel/firmware events, counter deltas, and
  hashes of larger artifacts;
- operator prompt/response when applicable, never inferred by automation; and
- authorization plus before/after target identity for `E/W` and `D/W` cases.

Finalization hashes the report, test-plan manifest, and evidence inventory.
Opt-in retests create a new signed report linked by the verified parent's
manifest and canonical bundle digests; finalized results are not overwritten.
Explicit signed correction history remains planned work. Serial numbers, credentials, camera/audio content,
and network identifiers are excluded or redacted unless the operator enables a
documented private-report mode.

### File-backed QEMU report-save validation

On a Linux host with Python 3, QEMU x86_64, dosfstools, and mtools, run:

```sh
sh scripts/lab/test-report-save-qemu.sh IMAGE.iso HHPT_BINARY NEW_WORK_DIRECTORY
```

Use an x86_64 image with the save command's serial audit markers and a matching
Linux `hhpt` verifier. The work directory must not exist; its parent must exist.
The harness creates a fresh 65 MiB disk image containing one FAT32 partition,
boots with networking disabled, and lets the safe quick run finish. It uses the
interactive save flow to reject an incorrect typed confirmation, requires zero
emulated disk writes and unchanged image bytes, then types the current target's
exact confirmation. After QEMU exits, mtools extracts the saved report from a
read-only partition copy. Its size and digest must match the save receipt, and
`hhpt verify -self-issued` must validate its contents and signature.

The guest has a 240-second deadline; host tools and child cleanup have separate
finite timeouts. No host block device, loop device, or mount is used. Logs, disk
images, the extracted report, and `validation.json` remain in the new directory
for review. This exercises the live image's real report mount/persistence path
on emulated USB storage; it does not establish physical hot-plug behavior or
external signer identity.

## Hardware and environment limitations

- The live OS cannot test memory occupied by itself. A separate Memtest86+
  boot entry remains planned; pre-boot testing cannot prove indefinite
  fault-free operation.
- A VM validates planning, statuses, timeout/recovery, reports, and synthetic
  failures. It cannot certify ports, analog audio, pixels, cameras, sensors, RF,
  thermals, power delivery, battery behavior, or physical condition.
- Firmware, undocumented controllers, vendor RAID, switchable graphics, and
  unsupported devices may limit access. Disclose the coverage gap; never
  substitute a generic pass.
- SMART, NVMe health, ECC, thermal, fan, power, and battery telemetry may be
  absent, stale, reset, virtualized, vendor-specific, or inaccurate.
- Storage sampling can miss localized defects. Full-range reads cover only
  currently addressable blocks; RAID, caches, remapping, thin provisioning, and
  controllers can obscure the medium.
- Device self-tests and sanitize have vendor-specific semantics. Successful
  completion does not prove every physical cell was independently exercised.
- Network performance varies with endpoint, cable, channel, interference,
  signal, regulatory domain, and upstream capacity. Grade only controlled local
  tests; treat Internet results as diagnostic.
- Operator checks are subjective and fixture-dependent. Reports distinguish
  their observations from automated evidence.
- Burn-in results apply only to recorded ambient conditions, firmware, power,
  cooling, components, duration, and workload.
- Passing does not prove data-recovery safety. Existing user data remains
  untrusted and must not be exposed, indexed, repaired, or mounted writable.

## Destructive safeguards

Destructive mode requires boot-time enablement and fresh typed confirmation
bound to stable path, fingerprint, model, capacity, and exact operation. Refuse
the boot medium, active report target, ambiguous multipath devices, arrays with
unknown members, and targets that change identity. Loss of the report/evidence
destination pauses writes. Cancellation states whether data changed and never
claims rollback. These are enforceable strategy requirements, not optional UI
warnings.

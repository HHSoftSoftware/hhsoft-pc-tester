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
eleven tests on modern images and nine on legacy x86. Only otherwise scheduled
tests become `excluded`, reported as `skip` with the exclusion reason. Absent
and missing-driver states retain their applicability reason, and unsupported
or destructive work cannot be enabled through selection. Reports retain explicit
selection reasons and the frozen exclusion intent. The broader profile budgets
and coverage below remain targets, not completed workflows.
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

Current emission remains v5 with eleven workers and the sampling catalog entry
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
postchecks satisfied. These results do not establish native fault
execution, fsync durability, rebuilt-image or physical-PC qualification.

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
changes add no traffic generation and retain the schema, current v5 emitter
and catalog.
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
completed v5 journal is written. Missing, contradictory, unsafe, or
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

The current keyboard increment preserves these monitoring boundaries in v5.
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
  monitor join; all eleven v5 worker outcomes remain present. Original v4
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
Broader CPU stress, instruction coverage and performance grading remain planned.

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
Corrections create a new signed revision linked to the prior hash; finalized
results are not overwritten. Serial numbers, credentials, camera/audio content,
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

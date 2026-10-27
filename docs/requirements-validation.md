# Requirements-based validation

The master checklist records product claims. Tests must establish the intended
claim and its failure boundaries, not merely reproduce the current code's
output. A completed implementation item is not physical-hardware qualification.

At each feature milestone and before publication:

1. Compare the operator guide, safety rules, and checklist with the actual code
   paths. Inspect TODOs, stubs, ignored errors, default success values, and
   unsupported branches. An absence of TODO comments is not proof of completion.
2. State the intended invariant before choosing a fixture. Include the missing,
   contradictory, substituted, and zero-work cases that must not pass.
3. Exercise relevant entry points, including direct capture and finalization.
   A helper test alone cannot establish that every signing path calls it.
4. Test external tool behavior with the real tool when its argument semantics
   affect correctness. Keep these integration fixtures isolated and offline.
5. Record what ran, what was skipped, and what still needs VM or physical
   evidence. Check off the item only after its applicable validation passes.

## Current review boundaries

The initial requirements review found no implementation TODO/FIXME placeholders
in the application, internal packages, or scripts. It did identify behavioral
gaps despite existing passing tests:

| Intended behavior | Independent failure case |
| --- | --- |
| Safe storage rejects every writable block mount | Writable dm, md, and loop devices omitted from hardware inventory; mount aliases and escaped paths |
| USB readiness never passes unexamined devices | Readable root hub with an unauthorized or unknown-authorization peripheral; zero authorized devices |
| Report saving writes only to the currently authorized destination | Replacement paths, expired authorization, upstream hub identities, mapped or unregistered sibling partitions, unsupported or ambiguous filesystem headers, and external ext4 journals; descriptor binding through the mount operation |
| Signed pass results contain sufficient consistent observations | Header-only results, zero work, missing safety fields, ambiguous JSON, and contradictory mount/traffic evidence |
| Display acceptance requires every applicable pattern and operator criterion to pass | One successful output must not hide another failed or incomplete pattern; partial operator findings and explicit failures must survive aggregation and finalization |
| Operator exclusions remain explicit in the frozen plan and signed report | Default selections changing plan bytes; excluded workers running; absent or missing-driver reasons being overwritten; omitted frozen test rows; stale evidence for an excluded worker |
| Guided keyboard claims cover six fixed controls on every applicable interface | One complete device hiding another incomplete device; initially held or stale actions; partial frames; disconnect/replacement; retained passes without final neutral/identity checks; hidden functional claims in execution notes |
| Frozen test deadlines bound complete work, and cancellation preserves evidence | Sequential subprocesses or multiple display cards exceeding a row's timeout; stopped workers or descendants surviving; cancelled finalization losing completed failures |
| Build inventories retain the exact selected package versions | Native APK resolver behavior differs from a mocked command runner |
| Source publication scans the objects actually transferred | Dangling outgoing commits, replacement refs, merge-only secrets, and deleted instruction backups |

The 2026-10-15 review passed the full Go test suite and `go vet` on Windows
(Go 1.27.2) and Alpine Linux (Go 1.26.9), plus application cross-builds for
Linux x86_64, x86, legacy x86, AArch64, and Windows x86_64. Linux executed all
19 build and 31 release Python tests, including native APK resolution and real
GPG signature/tamper checks. The 24 security tests passed across the two hosts:
Windows skipped the Linux symlink fixture; Linux skipped the two Gitleaks
integration cases, which ran with pinned Gitleaks on Windows. Workflow lint,
platform-lock checks, and the combined publication scan also passed. These
results cover this source review milestone, not subsequent changes or images.

On 2026-10-16, an x86_64 development image passed the real QEMU USB report-save
flow. Before authorization, after a wrong code, and after the next selection,
both QEMU write counters were zero and the whole disk image digest was unchanged.
Correct confirmation produced a 184,105-byte report; after unmount and guest
shutdown, mtools extracted it from a read-only partition copy. Its digest matched
the guest receipt and the independent verifier accepted its self-issued signature.
The MBR and bytes outside the selected partition remained unchanged. Graphics
and display remained inconclusive; no physical-device qualification is implied.

This development fixture reused independently copied, completed kernel/package
sections after fixing a nested fakeroot overlay failure. Its ISO SHA-256 was
`f0d33af03229fb0d3f8b798288d81b8c6b0ba1714e4772d1e766f9898fe63def`.
The same image also booted in the preserved VirtualBox BIOS and UEFI fixtures;
CPU, memory, storage safety, input readiness, and run integrity passed, with
zero writable block mounts and self-issued reports. Original VM media and
serial-log settings were restored afterward.
It is not a fresh offline release build or a published download. The new native
fakeroot regression verifies that the overlay inherits the outer session's fake
ownership without changing real file ownership. All 28 build and 10 lab Python
tests passed on Alpine; Windows skipped the three native build integrations.
The Go suite and vet passed on both hosts, all five application targets built
with Go 1.27.2, and a Go 1.27.2 Linux test executable also passed the save tests.
The builder's native Go suite used Go 1.26.9. CI runs the maintained harness
failure fixtures, including its embedded Python syntax check; full image boot
and report-save integration still run in the separate lab.

The 2026-10-25 safe quick selection milestone passed the Windows Go suite and
`go vet` with Go 1.27.2, plus all five application build targets. The suite
recorded 735 test/subtest pass events across 16 packages, zero failures, and
13 skips. Native Linux amd64 and 386 each recorded 83 planner and 109 CLI
test/subtest pass events with no skips. The actual generated modern startup
script and legacy startup script also passed their isolated native supervisor
fixtures without skips.

Native menu-to-plan-to-signed-report checks exercised all nine exclusions on
x86_64 and x86, and all seven supported exclusions on legacy x86. Each report in
this milestone retained all nine implemented quick-worker outcomes; legacy
graphics and display were not selectable.
Each target rejected three special-file selection inputs. The checks verified
signed report contents and signatures, including the frozen exclusion intent.
A comparison against the pre-selection implementation also confirmed unchanged
default plan bytes and ID. ISO rebuilds, physical beta qualification, and
release approval remain open for this milestone.

The subsequent 2026-10-25 report-coverage and worker-cleanup milestone passed
the full Windows Go suite and `go vet` with Go 1.27.2: 809 test/subtest pass
events across 16 packages, zero failures, and 13 skips. All five application
targets built. Native Linux amd64 and 386 each passed seven affected packages
with 630 test/subtest pass events and no skips. The 19 generated-modern and
legacy supervisor fixtures passed without skips, including terminal cleanup
failure before later work, finalization, or saving. Native Go 1.26.9 race testing
passed all 43 workerexec test/subtest cases without skips or race findings.

Real x86_64, x86, and legacy x86 menu-to-plan-to-finalize-to-verify workflows
produced signed reports containing all 21 frozen catalog rows plus run integrity,
with exact planned dispositions and exclusion intent. Failure tests cover
descendants retaining or closing output pipes, cancellation/exit races,
unconfirmed cleanup, diagnostic-write failures, and capture stopping before
later collectors or signing. Report tests reject ambiguous or contradictory
new coverage claims, including viewless bundles. Four historical HTML/PDF byte
digests captured from the preceding implementation remain unchanged. These
checks validate applications and fixtures; new ISO builds, whole-run deadlines,
physical qualification, and release approval remain open.

The 2026-10-25 quick-session milestone passed the full Windows Go suite and
`go vet`: 948 test/subtest passes across 16 packages, zero failures, and 13
platform skips. All five application targets built with Go 1.27.2. Native
Linux amd64 and 386 each passed 661 test/subtest cases across the four affected
packages without skips. The actual generated modern and legacy shell scripts
passed 82 isolated handoff scenarios across 12 integration methods, plus the
fixture syntax test. This includes a report-written-then-watchdog-stop case
that must not announce readiness or save. Worker orchestration is tested in the
Go coordinator rather than duplicated in the shell fixture.

Twelve real CLI sessions exercised all-off completion, a shared deadline,
Ctrl-C, and SIGTERM on each modern x64/x86 and legacy x86 application. Every
report retained all 22 results, exact frozen dispositions, signed stop reason
and worker states, and safe observations including the fresh pre-sign check.
Independent verification passed, and owned pseudo-terminal modes and descriptor
flags were restored. Native Go 1.26.9 race testing passed 120 session, journal,
finding-input, and signal test/subtest cases without skips or race findings.
The source audit found no abandoned implementation stubs or orphaned helpers.
Rebuilt ISO qualification, continuous monitoring, remaining functional adapters,
and the physical beta pilot remain open; cooperative cancellation does not
establish preemption of an uninterruptible kernel operation.

The subsequent guided-pointer milestone adds `input.pointer.quick` to the
implemented quick path. The Windows suite passed 1,094 test events across 17
packages, with 12 platform skips and no failures; vet and all five application
targets also passed. Native Linux amd64 and 386 suites passed 1,650 test events
across input, inventory, graphics, storage, safety, and CLI code, without skips.
Pointer/session race checks passed 241 test events without skips or race reports.
The historical counts above describe their original catalogs. Selection at the
pointer milestone had ten modern and eight legacy choices; the
existing 21 catalog rows plus run integrity still produce 22 results. Enabling
pointer changes the catalog digest and introduced fresh plans and a ten-outcome
`hhsoft.pc-tester.quick-session/v2` journal for that milestone. Historical
signed-report verification and rendering remain unchanged; direct capture continues to
omit guided pointer testing.

Portable and native fixtures independently cover relative `EV_REL` X/Y movement in both
directions and primary plus advertised right/middle button cycles on every
interface. Negative cases include one complete device hiding another
incomplete device, absolute/mixed interfaces, missing actions, lost or malformed
events, disconnect/replacement, cancellation/deadline races, contradictory
evidence, stale plans/journals, and privacy leakage. Initial queued events are
discarded, and a completed sequence cannot hide a subsequently queued loss or
disconnect. Capability checks use independently observed kernel bitmap width;
an ambiguous 32/64-bit interpretation cannot omit advertised buttons. Retained
per-device passes inside cancelled or partial results require the same hardware
binding as an aggregate pass. Reports retain bounded
action summaries without raw input events or typed text. The default observation
window is 30 seconds, the whole-command limit is 90 seconds, and the quick
session's five-minute budget remains independent and shared.

Fifteen real CLI scenarios passed on modern x64/x86 and legacy x86 applications:
twelve session cases covered all exclusions, deadlines, Ctrl-C, and SIGTERM;
three pointer-to-finalizer cases preserved incomplete observations from two
virtual interfaces in verified 22-row signed reports. The shell handoff suite
also passed. These exercise execution and honest non-pass reporting; they do
not establish successful physical pointer actions. Rebuilt-image qualification,
the physical pilot, continuous monitoring, and remaining functional adapters
remain open.

The periodic safety milestone implements bounded read-only mount/network
monitoring through quick-session execution, input cleanup, and final safety. It uses one
owned serialized observer, a one-second interval and observation deadline, a
2,500-millisecond maximum gap, and a two-second join allowance. Its first unsafe,
unknown, timed-out, or excessively delayed sample cancels active work and blocks
signing, even if a later sample is safe. A clean operator stop may still finalize
after the monitor joins and the independent pre-sign check passes.

Journal `hhsoft.pc-tester.quick-session/v3` adds the bounded coverage summary
while keeping the catalog, plan digest, and ten worker outcomes unchanged.
Leading and trailing gaps must remain bounded; a session before the first
one-second tick records zero samples rather than claiming observations it did
not make. Fresh v3 execution journals are required, while historical signed
verification remains unchanged. The boot guard increases to 350 seconds to
include the monitor join allowance.

For this milestone, the Windows suite passed 1,212 test events across 17 packages,
with 12 platform skips and no failures. Vet, all five application targets, and
cross-builds of 12 native test binaries passed. Native Linux amd64 and 386 passed
1,886 test events across six suites without skips. Race checks passed 321 test
events without skips or race reports. Nine compiler-supervision fixtures
confirmed child cleanup, and six independent environment and input checks
passed.

Fifteen real CLI scenarios passed: twelve quick sessions and three standalone
pointer-to-finalizer flows across modern x64/x86 and legacy x86 applications.
Each 2.5-second session deadline case retained two periodic observations.
The signed monitor evidence exactly matched `session-safety-monitor.json` in
the evidence directory, and reports retained all 22 results. These isolated VM
checks validate the application paths and bounded reporting claims; they do not
replace rebuilt-image or physical qualification. Historical milestone counts
above remain separate evidence.

Rebuilt bootable images, the physical pilot, remaining functional adapters,
broader beta qualification, and release gates remain open. Those mount/network
observations do not provide continuous coverage, thermal or heartbeat monitoring,
stress qualification, privilege isolation, or preemptive containment of a
blocked kernel operation. These results do not establish publication readiness.

The subsequent local thermal prerequisite is implemented in source. It introduced
`hhsoft.pc-tester.cpu/v2` and
`hhsoft.pc-tester.memory/v2`. The initial and subsequent observations have
one-second deadlines; subsequent samples start after a 250-millisecond wait
following completion. Results are constructed after a bounded two-second
observer join, including a final in-flight reading during normal completion.
Unconfirmed cleanup returns exit code 12 and prevents later work or signing,
even if diagnostic output fails. Known calculation and RAM mismatches remain
failures. Available, partial, and unavailable thermal coverage is explicit;
a computation pass does not establish thermal health when telemetry is absent
or incomplete.

Focused Windows command, finalizer, and session-journal regression tests passed
571 test events across 59 top-level tests, with no failures and four symlink
cases skipped. Controls cover output failures, both contradictory cleanup
claims, malformed/duplicate/old-version evidence, existing unreadable evidence,
impossible aggregate counts, retained upper-range heat, and preserved worker
failures. A missing evidence file retains the documented error/aborted fallback;
an existing invalid CPU/RAM file cannot use that fallback to bypass validation.
The subsequent full Windows suite passed 1,532 test events across 17 packages
with 21 platform skips and no failures. Vet and five application targets passed:
Linux amd64, x86 with SSE2, 386 soft-float, arm64, and Windows amd64. Seven test
suites also cross-compiled for both Linux amd64 and 386.

Native validation of this increment completed on 2026-10-26. The 14 suite runs
across Linux amd64 and 386 passed 2,690 test/subtest events with no skips. Race
checks for the quick-worker and command packages passed 1,125 events with no
skips. All 15 command-line workflows passed: 12 quick-session cases and three
pointer cases. Nine compiler and nine runtime process-ownership controls also
passed with confirmed fixture cleanup. Pass counts were independently checked
against the test logs. Historical signed-report verification and journal v3 are
unchanged. That milestone retained ten implemented workers and 21 catalog rows,
plus run integrity in the report.

This local guard is a prerequisite for the separate run-wide increment. It
does not monitor other workers or an all-off selection, implement heartbeat or
general hang supervision, or introduce privilege isolation. Fresh images,
physical sensor validation, the beta pilot, all four alpha ISOs, source/image
privacy review, and publication approval remain separate open gates.

The subsequent shared periodic-engine extraction preserves the safemode API,
duration constants and v1 evidence. Six exact JSON cases were executed against
the preceding implementation before extraction; their literal bytes remain
unchanged. Focused engine, safemode and command integration checks passed 217
test/subtest events with no failures or skips. These include deadline and
concurrent-Stop behavior, independent immutable snapshots, raw-result policy
classification, assessment-panic cleanup, and completion timestamps captured
before policy evaluation. The full Windows suite then passed 1,549 test/subtest
events across 18 packages, with 21 Windows-specific skips and no failures. Vet
passed; 16 Linux test binaries across eight packages on amd64/386 and all five
application targets compiled successfully. The subsequent 16 native Linux
amd64/386 suite executions passed 2,724 test/subtest events with no skips.
Periodic, safemode and command race checks passed 1,105 events with no skips;
all 15 CLI scenarios passed. Nine compiler and nine runtime ownership cases
confirmed fixture cleanup without signaling foreign process groups. Root checks
reconciled the actual logs and archive against the frozen inputs, confirmed
cleanup, and passed all six postchecks. The extraction itself added no run-wide
thermal coverage or new catalog/report rows. Rebuilt-image and physical
qualification remain open.

The qualified run-wide increment implemented sampled thermal supervision in the
safe quick session and introduced `hhsoft.pc-tester.quick-session/v4` with embedded
`hhsoft.pc-tester.run-thermal/v1`. It retained ten worker outcomes and the existing
catalog. Initial thermal completion precedes execution and preparation, even
with all tests excluded. Serialized one-second sampling continues through all
workers, input cleanup and final inventory, with a one-second reading deadline
and a 2,500-millisecond maximum gap. A final observation is required after
inventory preparation. The local CPU/RAM guard retains its separate
250-millisecond wait between readings.

A one-use in-process final gate obtains pre-sign safety, requests the final
thermal observation, and synchronously joins both session monitors under one
common allowance of at most two seconds. Normal final observation and joins
fit within the 30-second preparation/finalization bound. An earlier error has
one common two-second cleanup allowance. Monitoring failure blocks signing;
heat cancels work while monitoring continues and remains reportable after clean
joins. Late heat determines the final signed stop reason and readiness marker,
without changing a valid worker failure or conflating heat with graph integrity.
The monitored boundary excludes subsequent report encoding, output and saving.

Journal v4 discloses available, partial or unavailable telemetry without
promoting absent readings to a thermal pass. Local CPU/RAM heat is retained even
when the global sampler has no sensors. Each local source binds its execution
interval, original file bytes and canonical full typed worker evidence. The live
finalizer rechecks both digests; the offline verifier recomputes the typed digest
and checks the original-file digest's format. Shared validators reject false
timelines, missing joins, oversized nested journals and inconsistent source
bindings. Historical signed evidence retains its previous interpretation.

Shared-evidence and command acceptance tests exercise the real
coordinator, thermal and safemode adapters, finalizer, signed-bundle verifier
and shared report consumer with synthetic observations. Cases include initial
heat and all-off runs; heat during every worker, input cleanup, final inventory
and pre-sign safety; cooling after a latched finding; retained local failures;
changed, cooler, disappeared or reformatted evidence; common monitor deadlines;
failure during cleanup; duplicate/missing gates; and typed readiness mismatches.

Full Windows qualification passed 1,754 test/subtest events across 22 packages,
with 21 platform skips and no failures. Vet, cross-builds of 24 Linux test binaries
and all five application targets passed. Native Linux amd64/386 qualification
then passed 3,032 events across 24 suite runs. The periodic, safemode, thermal and
command race suites passed 1,203 events. Native and race runs had no skips.
All 15 CLI cases passed: twelve session scenarios and three guided-pointer
scenarios across the modern and legacy x86 applications. Nine compiler ownership
and nine runtime ownership cases, 32 archive cases and 13 shell-supervisor cases
passed with confirmed cleanup. All six postchecks passed and all 301 input pins
remained unchanged. Verbose log counts and retained receipts were independently
reconciled.

The first native attempt failed because the harness launched a test binary
outside its package directory, so relative fixture paths did not resolve. That
failure evidence was preserved. A fresh attempt using the correct package
working directories passed without changes to product source.

All twelve CLI session cases reported `thermal_coverage=unavailable`, matching
the VM's lack of temperature telemetry. All-off sessions retained their required
initial and final observations; each deadline session retained four observations,
including two periodic samples. Injected heat exercises the real software
coordinator and finalizer, but does not establish physical cooling performance.
These native runs cover Linux amd64 and 386; AArch64 was cross-built only. Rebuilt
images, representative physical sensors, the beta pilot and release gates remain
open.

The guided-keyboard increment completed software qualification on 2026-10-26.
It activates definition version 2 of `input.keyboard.quick`, requires fresh
frozen plans, and introduces session v5
with eleven ordered workers. Keyboard runs between input readiness and pointer
testing. Selection offers eleven modern and nine legacy quick tests, subject
to applicability. The catalog remains 21 rows plus run integrity, producing 22
report results. These changes do not relabel any preceding v4 or earlier
validation results as v5 evidence.

The bounded check requires a press and release of left Shift, right Shift, Up,
Down, Left and Right on every applicable keyboard interface. It waits for a
neutral baseline and accepts complete event frames only. Every retained device
pass requires final idle, neutral and identity checks before its descriptor
closes, including when cancellation leaves other devices incomplete. One
interface cannot supply another's actions. Evidence records fixed completion
booleans, not typed text, raw input events or their sequence. The default window
is 30 seconds within a 90-second command limit and the remaining five-minute
session budget. Full layout, rollover, ghosting and other keys remain open.

The session's single console owner handles Ctrl-C and discards queued keyboard
input before later display findings. Ordinary console failures block success
with exit 1 after confirmed cleanup. Unconfirmed descriptor or console cleanup
forces exit 12, even after another failure or failed diagnostic output. A later
successful cleanup attempt cannot erase an earlier uncertain receipt.

Shared decoding binds functional evidence to v5 worker status and timing;
finalization additionally binds each retained device pass to the unchanged
frozen component and final inventory. Actual keyboard observations require the
current live finalization gate. Without a session journal, standalone
frozen-plan finalization accepts only exact nonexecution notes for `excluded`,
`not_present`, `unsupported` or `missing_driver` keyboards. Such notes contain
no functional observations. Original v4 journals retain their strict ten-worker
rules; v1-v3 evidence keeps its historical interpretation. Duplicate, escaped,
case-aliased or hidden modern claims cannot use a historical or viewless-bundle
bypass. Direct capture still omits guided keyboard and pointer workers.

Windows qualification with Go 1.27.2 passed 2,156 test/subtest events across 23
packages, with 21 platform-specific skips and no failures. Vet, 26 Linux test
binary cross-builds and all five application targets passed. Native Linux
amd64/386 qualification passed 3,784 events across 26 suite runs; five race
suites passed 1,461 events, with no skips. All 21 CLI scenarios passed: twelve
session, three pointer, three standalone keyboard and three keyboard-session
cases. Owned-process checks passed nine compiler and nine runtime cases with
confirmed teardown; all 32 archive and 13 supervisor checks passed.

The three standalone keyboard cases observed an interface but no completed
press/release pairs and correctly remained inconclusive at the observation
timeout. The three keyboard-session cases retained an error at the run deadline,
with no completed pairs. Their terminal state and descriptor flags were restored,
children were reaped and cleanup was confirmed. These expected outcomes validate
bounded behavior; they do not establish successful physical keyboard actions.

All fifteen session cases reported unavailable temperature telemetry. All-off
cases retained initial and final observations; run-deadline cases retained four
observations, including two periodic samples. No VM result establishes safe
physical temperatures.

Earlier unsuccessful native attempts remain recorded separately. The positive
heat fixture now waits for a second serialized safety callback, which proves
the first safe completion was accepted before heat is induced. Fake-clock
success cases wait for the exact next timer to be registered before advancing
time. Independent counterexamples demonstrate the timing assumptions; the
original native interleavings were not recorded, so their precise causes remain
unconfirmed. A separate negative requires zero samples at the first interval to
retain a sampling-gap failure despite confirmed cleanup. Production timing,
coverage, cancellation, cleanup and signing guards were not relaxed.

These results cover Linux amd64 and 386; AArch64 was cross-built only. Fresh
construction and qualification of all four beta images, physical multi-keyboard
and held-key/disconnect checks, and the pilot on at least ten varied PCs remain
open. They do not replace or expand the frozen initial alpha candidate's
separate source, image and release gates. Full layouts, rollover, ghosting and
other keyboard functions remain planned.

The pointer wheel-direction extension passed Windows and native Linux amd64/386
software qualification on 2026-10-26. Definition 2 and functional evidence v2 require
both directions of every advertised vertical/horizontal axis on each supported
relative pointer. High-resolution streams are preferred; their coarse companions
are accepted when advertised but are not counted again. Net-zero packets do not
credit a direction, and malformed packets cannot partially satisfy criteria.

Shared validation preserves the exact historical v1 wire shape and scope. New
v2 evidence is bound to the advertised wheel bits in final inventory and the
frozen plan, including retained device passes in partial results. Standalone
pointer finalization remains supported without session monitoring. V5 sessions
also bind worker status and duration; v4 cannot claim pointer definition 2.
Raw hidden, duplicate, aliased and viewless claims select strict validation.
The full host suite passed 2,364 test events across 24 packages, with 17
platform test skips and four packages without host test files. Vet, 28 Linux
test-binary builds and five application builds passed against the same source.
Independent review reproduced and verified the correction of an interrupted-read
bug: an interrupted syscall now retries without establishing queue idle, so stale
baseline input and queued event loss cannot earn a pass through that boundary.

Native Linux amd64/386 passed 4,114 test/subtest events across 28 suite runs.
The input, periodic, safemode, thermal and command race suites passed 1,508
events. Native and race runs had no skips. All 24 CLI cases passed: twelve
session lifecycle cases, six keyboard cases, and six pointer cases. The pointer
cases covered three standalone reports and three pointer-only signed v5 sessions
across modern x64/x86 and legacy x86 applications. Nine compiler ownership,
nine runtime ownership, 32 archive and 13 supervisor checks passed with confirmed
cleanup. Returned verbose logs, source bindings and all six final environment
checks were reconciled independently.

The pointer CLI cases observed two virtual interfaces, one supported for relative
actions, without supplied movement, button or wheel actions. The interfaces
advertised two wheel axes in total and selected no high-resolution stream.
Standalone results remained inconclusive; pointer-only sessions retained
deadline errors and clean teardown. These are successful workflow checks, not
successful physical actions or high-resolution hardware qualification.
Session thermal evidence honestly retained unavailable telemetry. AArch64 was
cross-built only for this increment, not executed natively.

All four fresh beta images, physical wheel and sensor checks, and the pilot on
at least ten varied PCs remain open. The frozen initial alpha candidate and its
separate source, image and release-approval gates are unchanged.

Storage readiness now binds the expected canonical unsigned 32-bit major/minor
identity to both pathname metadata and the opened block descriptor. Registry and
mount parsing reject numeric aliases and malformed identities. Duplicate device
numbers fail across the complete registry, including excluded interfaces.
Read-only opens use `O_NOFOLLOW`, and a later registry recheck must still match.
Each acquired descriptor gets one close attempt; failed closure remains an error.
Cancellation at the helper or caller boundary revokes the current observation's
readable flag and credit while retaining earlier unsafe mount observations.

Focused Windows checks passed five test/subtest events without skips or failures.
Independent simulated registry, metadata and descriptor cases passed 71 events
without failures, including stable wrong nodes, swaps, symlink insertion,
duplicate identities, high device-number bits, close failures and cancellation.
These cases exercised production decisions with inert device operations; they
did not execute Linux kernel device operations. Linux storage vet and amd64/386
test-binary cross-builds also passed. Full Windows qualification then passed
2,369 test/subtest events across 25 packages, with 17 platform test skips,
three packages without host test files and no failures. Vet, 28 Linux test-binary
builds and five application builds passed against the same source.

Native Linux amd64/386 qualification passed 4,220 test/subtest events across
28 suite runs, including 67 storage events on each architecture. The five
existing input, periodic, safemode, thermal and command
race suites passed 1,508 events; native and race runs had no skips. All 24 CLI
cases passed with confirmed cleanup. Nine compiler ownership, nine runtime
ownership, 32 archive and 13 supervisor checks passed, and all six final
environment checks were reconciled. Storage identity fixtures used simulated
metadata and descriptors without accessing physical disks or reading media
contents. AArch64 was cross-built only; these results establish no physical
storage qualification.

A subsequent mount-policy increment shares one bounded parser between runtime
safety and storage readiness. Empty observations and malformed mount records
are errors. Every mounted nonzero device number must exist in the complete
registry before any node open. Unsupported major-zero filesystems, including
Btrfs and FUSE, fail closed; writable overlays are rejected while the existing
read-only overlay policy remains unchanged. Already parsed writable counts
survive later malformed input, missing or incomplete registries and cancellation.

This increment passed 109 focused Windows test/subtest events and 139 simulated
Linux-function events. Independent review added 25 events, passing all 164
simulated events without failures. The added cases cover record-order and map
iteration independence, unknown anonymous disk-backed filesystems, retained
unsafe counts after malformed tails or missing registries, cancellation, and
valid versus empty diskless observations. The original identity, no-follow,
close and caller-cancellation logic remains unchanged. Linux vet and four
amd64/386 package test-binary cross-builds passed. These checks use inert metadata
and descriptors, not native kernel device operations. Full Windows qualification
passed 2,406 test/subtest events across 25 packages, with 17 platform test skips,
three packages without host test files and no failures. Vet, 28 Linux test-binary
builds and five application builds passed against the same source.

Fresh native Linux amd64/386 qualification of this increment passed 4,364
test/subtest events across 28 suite runs, including 102 storage events per
architecture. Five existing race suites passed 1,545 events; storage is not a
separate race suite in this cohort. Neither group had skips. All 24 CLI cases,
nine compiler ownership, nine runtime ownership, 32 archive and 13 supervisor
checks passed, with confirmed cleanup and all six final environment checks
reconciled. Storage fixtures still use simulated metadata and descriptors:
native execution does not establish physical-disk or media-read behavior.
AArch64 was cross-built only. Rebuilt-image and physical checks remain open.

The storage-readiness v1 schema, catalog and session journal are unchanged, and
the adapter performs no media-content reads or writes. Agreement between these
metadata observations does not establish persistent physical identity or detect
every replacement that reuses a kernel number. Health telemetry, sampled/full
reads and the broader storage matrix remain planned. Rebuilt-image, physical
replacement/cancellation, pilot and frozen-alpha release gates remain open.

The next CPU increment implements twelve fixed binary64 addition, subtraction,
multiplication and division vectors in the bounded worker. CPU v3 evidence adds
ordered per-worker counts, exact mismatch bits and separate integer, SHA-256 and
scalar failure flags. Current CPU definition 2 uses the existing v5 journal and
22-row report. Historical CPU v2 hashes and definition 1 remain valid under their
original strict v4/v5 rules; memory evidence is unchanged.

Affected-package Windows regression tests passed 1,788 test/subtest events across
nine packages, with 16 platform test skips and no failures. Cases cover cadence,
late work, distinct failure workers, counter overflow, historical typed hashes,
version pairing, no-view verification and HTML/PDF summaries. Completed-result
fixtures with first, last and later-suite faults on a nonzero worker cross the
real session coordinator and finalizer, including operator cancellation and local
heat. Separate producer tests exercise the operation and worker-coordination
boundaries. Full host validation passed 2,543 test/subtest events across 25
packages, with 17 platform test skips and three packages without host tests.
Vet, 28 Linux test-binary builds and five application builds passed. The 28
native Linux amd64/386 suites passed 4,608 events; six race suites passed 1,745
events, with no native skips. All 30 CLI scenarios passed, including six new
standalone/session CPU capture and signed-finalization cases across amd64, x86
SSE2 and legacy x86 soft-float. Fresh beta images and physical CPU checks
remain open. The fixed suite does not establish wider instruction coverage or
stress/performance behavior; legacy soft-float validates software arithmetic,
not hardware FPU execution. The frozen initial alpha candidate and its release
gates are unchanged.

Regression coverage for these cases belongs with the corresponding feature,
not with a blanket claim of complete product coverage. Linux mount/device
fixtures and real APK selection require the appropriate Linux environment;
Windows compilation alone does not execute them. Real signature verification
uses disposable keys and an isolated keyring.

Report mounting reads only the first 2 KiB of the authorized partition to
recognize an unambiguous `vfat`, `exfat`, or `ext4` header before requesting a
writable mount. Ext4 must use extents; any journal must be internal, and stored
mount-option overrides are rejected. This probe does not establish filesystem
integrity or media health. The kernel must still accept the explicit filesystem
type. The mount uses bound device/directory descriptors, repeated authorization
checks, and `nodev,nosuid,noexec`. Linux fixtures exercise pathname replacement
and authorization expiry without mounting real media. They do not establish
immunity to physical removal, device reuse, or other hardware hot-plug races.

Direct quick capture and worker finalization validate evidence before creating
signed results. Schema validation rejects missing required fields, duplicate or
unknown fields, and ambiguous field spelling. Semantic validation requires work
and observations sufficient for the claimed bounded check: for example, CPU
known answers, complete RAM operation counts, consistent readiness inventories,
and no contradictory storage writes or network traffic. Valid partial non-pass
observations remain reportable. A supervisor-recorded error or abort can produce
an explicit execution note when usable worker evidence is unavailable; it cannot
become a pass or discard a valid worker failure. CPU, memory, graphics, and
display failures cannot be replaced by a supervisor error or abort. A RAM byte
mismatch, including at offset zero, cannot be labeled inconclusive, error, or
aborted by either direct capture or finalization. Explicit renderer or pattern
failures also require an overall graphics failure. Matching failure and partial
results retain their original evidence in the signed report. These checks do
not upgrade readiness into physical qualification.
The standalone report verifier checks bundle integrity and signatures. Strict
v4/v5 claims additionally require the shared journal and local-thermal evidence
checks described above; v5 functional keyboard claims also require their shared
decoder and final-inventory bindings. Verification does not rerun hardware
tests or establish physical truth.

Both appliance startup scripts check mount/network safety before handing the
frozen plan to the quick-session coordinator and again before announcing a ready
report. The coordinator independently observes safety before work, before every
started worker, and after cleanup. The live final gate adds a fresh pre-sign
observation and binds the completed journal in signed run-integrity evidence.
Writable block mounts,
active non-loopback interfaces, contradictory details, or incomplete observations
block signing and the normal ready/save path while retaining RAM diagnostics.
Device numbers identify mapped and aliased mounts even without a storage test;
software interfaces are checked without a hardware readiness provider. Selecting
no quick tests still requires the safety observations and preserves them in the
signed session history. The extra shell checks remain separate operational
gates. Passing graph integrity establishes an unchanged observed graph, not
completed hardware-test coverage.

The coordinator enforces the shared run deadline and each frozen worker's
complete deadline. Graphics tools share one 30-second limit; display cards,
patterns, and findings share one 45-second limit. Ctrl-C and Linux SIGINT/SIGTERM
stop new work, preserve completed failures and exclusions, and record unfinished
scheduled tests. Console input must finish and restore its state before fresh,
bounded final safety and finalization contexts begin. Unsafe, interrupted, or
incomplete network/storage readiness cannot authorize signing. Unconfirmed
worker cleanup also blocks finalization. The terminal shell guard prevents
ready/save after any nonzero session exit, even if a report already exists.

The quick-session and pointer milestones above validate this cooperative
orchestration and its bounded input path. These snapshots and
deadlines do not establish continuous thermal/heartbeat monitoring, preemption
of an uninterruptible kernel operation, containment of escaped process groups,
or the still-planned privilege separation and broader profile workflows.

Display regressions cover a successful pattern alongside another output's
modeset failure, failed/error/aborted/inconclusive/skipped patterns in either
order, mixed operator findings, and an absent display. Passing observations for
one output cannot erase another output's failure or incomplete result. A display
pass requires every applicable pattern and all seven operator criteria for each
successfully patterned connector to pass. These fixtures do not replace real
multi-display testing or operator observation of pixels, color, and artifacts.

The hardware matrix in [test-strategy.md](test-strategy.md) includes future
requirements. Full SMART/media qualification, complete RAM tests, GPU stress,
physical ports, privilege separation, comprehensive watchdogs, Secure Boot,
and the broader profiles remain open where marked in
[the master checklist](../PROJECT_CHECKLIST.md). VM success must not be used
to close physical-hardware requirements.

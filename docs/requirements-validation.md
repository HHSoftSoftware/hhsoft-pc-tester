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
The standalone report verifier checks bundle integrity and signatures; it does
not rerun the worker semantic checks or establish physical truth.

Both appliance startup scripts independently inspect kernel mount and network
metadata before active tests, between active stages, after readiness checks,
and before announcing a ready report. Writable block mounts, active non-loopback
interfaces, or incomplete observations block the normal ready/save path and
retain diagnostic evidence in RAM. Device numbers identify mapped and aliased
mounts even without an applicable storage test; software interfaces are checked
even without a hardware readiness provider. Ready markers contain measured
counts. These independent snapshots currently remain runtime evidence in RAM;
the finalizer does not include them in the signed report. Selecting no quick
tests still runs the independent safety gates, but its signed report contains
the test skips, exclusion intent, and hardware-graph integrity result without
those independent mount/network snapshots. Passing graph integrity establishes
an unchanged observed graph, not completed hardware-test coverage. Signed
coverage of the independent snapshots remains open, as do continuous monitoring
and the remaining watchdog and isolation work.

The frozen catalog's per-test timeout values are not yet enforced as complete
worker deadlines throughout either startup script. Modern graphics has separate
subprocess limits, and display work can repeat a subprocess budget for each
card before waiting for operator findings. A shared run deadline and operator
cancellation that stops and finalizes the whole quick session remain open.
Worker cleanup and individual bounded waits alone do not establish these claims.

Future acceptance must cover sequential graphics tools and multiple display
cards under one frozen row deadline, cancellation during work and console waits,
and descendants retaining or closing inherited output pipes. A stopped run must
retain completed failures and frozen exclusions, record each unfinished scheduled
test explicitly, and finalize only after cleanup under a separate bounded
context. This requires a stopped-run path that preserves independent safety
checks; accepting `aborted` or unsafe readiness observations as generally safe
would not satisfy it. Unsafe or incomplete final observations must still block
the normal ready/save path.

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

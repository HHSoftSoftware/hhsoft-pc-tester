# Architecture

Status: accepted direction through Phase 1. Revisit after the first
real-hardware pilot, not during every implementation choice.

## Product boundaries

HHSoft PC Tester is a diagnostics appliance, not a repair toolkit, hardware
database service, or unattended destructive burn-in image. It answers four
questions for one current hardware configuration:

1. What components and externally testable ports are present?
2. Did every applicable quick, functional, and health check pass?
3. What human-observed checks remain incomplete?
4. Is this the same component set seen in an earlier trusted report?

The appliance must work offline. Internet access, package updates, driver
downloads, benchmark downloads, and report upload are never part of boot.

## Platform decision

Use a pinned Alpine Linux stable branch in diskless mode for the first
production architecture.

- Alpine provides x86, x86_64, and AArch64 builds and a small, auditable base.
- Diskless mode loads a read-only image into RAM and fits the no-host-changes
  default better than a normal installation.
- `apk`, OpenRC, and Alpine image profiles keep image customization explicit.
- Alpine community packages have a shorter support window than main packages,
  so releases pin every package and rebuild frequently instead of updating an
  appliance in place.

Buildroot is the bounded legacy base only. It offers the precise pre-SSE2 CPU
baseline that Alpine no longer provides, at the cost of project-owned kernel,
driver, package, and update lifecycle. It remains minimal rather than
duplicating every Alpine functional test.
Debian Live and current SystemRescue images simplify some desktop dependencies,
but their primary live-image path is 64-bit x86 and therefore does not meet the
required 32-bit x86 baseline.

Prototype against Alpine `v3.24`. `platform/alpine/base.lock` is the only
source for the Alpine patch release and exact aports revision. An explicit
base-update script creates a candidate lock for review; no build reads a
floating latest-stable reference. A branch upgrade is a reviewed pull request
that refreshes package locks and passes the full VM and hardware matrix.

Primary references:

- https://www.alpinelinux.org/releases/
- https://wiki.alpinelinux.org/wiki/Architecture
- https://wiki.alpinelinux.org/wiki/Diskless_Mode
- https://wiki.alpinelinux.org/wiki/How_to_make_a_custom_ISO_image_with_mkimage
- https://buildroot.org/downloads/manual/manual.html

## Boot artifacts

Ship distinct artifacts rather than one hard-to-debug universal image:

| Artifact | Firmware | CPU baseline | Stage |
| --- | --- | --- | --- |
| `hhpt-x86_64.iso` | Legacy BIOS and x64 UEFI | x86-64 | Required |
| `hhpt-x86.iso` | Legacy BIOS and IA32 UEFI where supported | i686, CMOV, SSE2 | Required |
| `hhpt-legacy-x86.iso` | Legacy BIOS or CSM | Pentium MMX, no SSE2 | Required fallback |
| `hhpt-aarch64.iso` | Standards-compliant AArch64 UEFI | ARMv8+ | Beta |
| PXE bundle | BIOS/UEFI matching its build | Same as image | Later |

Use Syslinux or Isolinux for legacy BIOS and GRUB EFI binaries for UEFI. Include
an open-source Memtest86+ entry separately from the Linux environment. Keep a
safe graphics fallback and a serial-console recovery entry.

The AArch64 artifact is a beta modern-media target. It uses generic AArch64
UEFI rather than legacy ARM board-specific boot flows; build and QEMU boot
validation are still required, followed by device-specific firmware and driver
qualification on representative ARM PCs. Its beta label must remain visible
until that work is complete.

Legacy BIOS has no Secure Boot. It may use CSM on a UEFI machine. The normal
UEFI path does not need CSM, but current and first production images require
Secure Boot off. Secure Boot is an optional, deferred enhancement rather than
a gate for functional hardware testing. Its trust contracts remain recorded
so a later implementation does not weaken the rest of the appliance.

Signing the EFI loader is not the whole security boundary. The enrolled-key
path binds kernel, initramfs, command line, and appliance trust roots into the
signed UKI, prohibits unsigned modules, and preserves the runtime, report, and
storage policy controls after boot. See `docs/secure-boot.md`.

The 32-bit Alpine baseline excludes pre-SSE2 PCs. A separate Buildroot i586
artifact therefore boots from BIOS/CSM on Pentium MMX-class and newer systems,
including older x64 systems. It runs the same `GO386=softfloat` inventory and
report core, BusyBox/mdev device discovery, and supported low-cost read-only
checks. Its report states reduced driver/test coverage. Pre-MMX x86 is outside
the supported boundary.

## Runtime components

These are the planned service boundaries. Current quick execution uses one Go
coordinator with in-process workers; separate UI/service processes and confined
adapter processes remain unimplemented.

- `hhptd`: root-owned Go orchestrator and local API. It owns discovery,
  policy, scheduling, evidence, fingerprinting, and final report state.
- `hhpt-ui`: unprivileged local operator UI. It cannot open raw storage or run
  commands. The first UI may be a keyboard-driven TUI plus dedicated graphical
  test screens; browser-kiosk mode is optional on architectures that support it.
- test adapters: reviewed Go adapters around sysfs and pinned programs such as
  smartmontools, nvme-cli, stress-ng, fio, Mesa utilities, libinput, ALSA,
  V4L2 or libcamera, and NetworkManager or iwd.
- report verifier: a host-side cross-platform utility and static report viewer
  that verify manifests without trusting active content in a report.

Do not adopt Phoronix Test Suite as the privileged core. It is useful as an
optional advanced benchmark adapter, but runtime profile downloads and general
shell profile execution conflict with the fixed, reviewed image boundary.
Likewise, `hw-probe` may inform inventory compatibility but its upload workflow
is opt-in and is not HHSoft PC Tester identity or audit state.

## Graphics driver strategy

Modern images use package-locked Alpine kernel DRM/KMS drivers, Linux firmware,
and Mesa Gallium/EGL/GBM from the same pinned distro release. This supports a
single review/update path and open drivers such as amdgpu, i915, and nouveau
where the kernel, device firmware, and Mesa version support the hardware. The
core image does not bundle proprietary AMD or NVIDIA drivers; missing signed
firmware, unsupported hardware, or software fallback is reported as a coverage
gap rather than a pass. Driver- and firmware-aware image slimming is allowed
only after it preserves the hardware matrix.

The Phase 1 `hhpt graphics` command is the first active test. It discovers DRM
devices and connector modes, records the Mesa/GBM renderer, detects software
fallback, and can run a bounded KMS pattern on the first connected card. The
`hhpt display` worker additionally applies a bounded libdrm preferred-mode
pattern to all connected outputs per card and captures compact P/F/I operator
findings. It is not a complete automated visual-quality, safe-mode-restoration,
stress, VRAM, video-codec, or hybrid-graphics qualification. Normal finalization
includes both evidence records in the signed aggregate payload and standalone
HTML/PDF report views.

## Discovery and planning

Build a stable hardware graph before offering tests:

1. Enumerate buses and kernel driver bindings from sysfs, udev, PCI, USB, ACPI,
   device tree, DMI, V4L2, DRM, ALSA, IIO, networking, and block devices.
2. Normalize vendor data without losing raw evidence; empty, generic, and
   known-placeholder serials never become identity inputs.
3. Map graph capabilities to declarative test requirements.
4. Show detected, testable, unsupported, and missing-driver states separately.
5. Freeze the plan before execution; hotplug can add a new explicit test step
   but cannot silently change a destructive target.
6. Re-observe the stable graph before signing, bind every result to the frozen
   plan and planned graph HMAC, and record added, removed, or changed component
   bindings in a `run.integrity.quick` result.

Official test definitions are embedded in the image. A definition declares its
ID and version, architecture, applicability, privilege, external commands,
network access, storage access, estimated duration, timeout, required operator
action, and produced evidence. The daemon rejects undeclared execution.
The current `hhpt plan` slice resolves a subset of that contract, creates a
deployment-keyed hardware-graph binding and deterministic frozen ID, and makes
safe boot execute only `scheduled` tests. Planned coverage reports
`unsupported` until its reviewed implementation exists.

The console can exclude implemented safe quick tests before the plan is frozen.
It writes a new RAM selection file with an explicit `excluded_tests` array; the
planner rejects ambiguous, unknown, duplicate, or architecture-incompatible
choices. Canonically ordered exclusions become part of the plan ID. An empty
selection preserves the default plan encoding and ID within the same catalog.
Exclusions only change tests that would otherwise be scheduled, preserving absent-hardware and
missing-driver evidence. Both boot supervisors retain their unconditional mount
and network safety checks and pass all twelve quick outcomes to finalization.
Signed run-integrity evidence includes the exclusions, including choices for
hardware that was not available.

Finalization rejects ambiguous JSON members and plans whose catalog rows,
limits, statuses, or component references are inconsistent, even if their
unkeyed plan IDs were recomputed. It rebuilds applicability from the observed
inventory and fingerprint key; when the stable hardware graph is unchanged,
the rebuilt and frozen plan IDs must agree before signing. Reconstruction carries
the frozen exclusions so an operator's choices remain bound to the report.

Finalized reports retain all 22 frozen catalog rows and one run-integrity row.
Each catalog result carries its definition version and `planned_status`, even
when no adapter runs. The signed `coverage_scope` is `frozen_plan`; HTML/PDF
views show the planned disposition and its reason alongside the final outcome.
Direct capture instead labels its seven collected checks `direct_capture` and
does not run guided keyboard/pointer checks or exterior observation, or claim a
frozen plan. Optional absent
coverage fields preserve historical report rendering; new reports require a
matching rebuilt companion verifier.
The quick-session coordinator adds a signed session journal inside run-integrity
evidence: stop reason, budget and elapsed time, each worker's execution state,
independent mount/network safety observations, and bounded monitor summaries.
The current development implementation uses a one-use in-process final gate
after inventory and result preparation. It collects pre-sign safety and final
thermal observations, joins both session monitors, and returns an immutable
journal. Finalization validates that journal against the frozen plan and retained
evidence before generating a report key or signing.

The guided pointer increment used the existing `input.pointer.quick` row and
retained its then-current 21 catalog results plus run integrity. Enabling that row changes
the catalog digest and requires fresh plans. That increment introduced
`hhsoft.pc-tester.quick-session/v2` with ten worker records. The subsequent
periodic-monitor increment introduced `hhsoft.pc-tester.quick-session/v3`.
The run-wide thermal increment introduced `hhsoft.pc-tester.quick-session/v4`, keeping
the catalog, plan digest, and ten records. It adds execution intervals, final
preparation/join boundaries, thermal coverage and local heat-source bindings.
A serialized journal cannot substitute for the live final gate. Historical
signed-report verification and rendering remain unchanged. Each increment's
validation is tracked separately from the earlier session milestone.
Direct capture and finalization without a session journal do not claim that
session safety history.

Pointer definition 2 uses functional evidence v2 for advertised vertical and
horizontal wheel directions. One credited event stream per axis prefers
high-resolution events; legacy companion events are not added to that count.
A shared decoder validates exact v1/v2 wire shapes and binds retained passes to
final inventory and the frozen plan. Standalone pointer finalization remains
available without a session-safety claim; v2 evidence in a v5 session also
requires the worker interval. Historical v4/v5 reports with pointer v1 retain
movement/button scope. Windows and native Linux amd64/386 suites, race checks,
and standalone and signed-session CLI checks passed for the wheel increment.
Rebuilt-image and physical-device qualification remain open.

The guided keyboard increment activates definition version 2 of the existing
`input.keyboard.quick` row and requires fresh plans. Journal v5 inserts its
eleventh worker between input readiness and pointer testing. Shared validation
dispatches v4 to its original ten-worker rules, v5 to eleven-worker rules,
v6 to its twelve-worker storage read format and v7 to its twelve-worker exterior
inspection format. Current emission is v7; unknown versions and relabeled
journals cannot bypass those checks. A shared
keyboard decoder binds fixed action summaries, cleanup, worker timing and
unchanged per-device identity/capabilities. Raw input events and typed text are
never exported. Standalone finalization may retain strict nonexecution notes
for excluded, absent, unsupported or missing-driver keyboards without claiming
session coverage. Keyboard execution uses the live v7 workflow; historical
v5/v6 reports retain their explicit functional-evidence rules.
Windows and native Linux amd64/386 qualification, including race checks and CLI
workflows, passed for the v5 keyboard integration. This qualifies that application
source;
rebuilt images and physical keyboards remain separate gates. See
[requirements validation](requirements-validation.md).

The inactive storage evidence reader shares one finite Result validator across
report verification, rendering and comparison. Explicit v6 read support binds a
storage source to its typed result, reconstructs topology/fixture eligibility and
keeps accepted work separate from diagnostics. Historical v4/v5 wire handling is
preserved. Current emission is v7 with twelve workers; the sampling catalog
entry remains inactive. This reader adds no media acquisition backend; see
[report integrity](report-integrity.md) for signed-assertion and cleanup limits.

A graph change does not discard evidence. It fails run integrity and preserves
the final report with the original/final graph HMACs and component-level delta,
so a verifier can see that one unchanged hardware graph did not span the run.

Modern explicit-key mode mounts a separately controlled deployment token
read-only and binds its block, partition, and USB IDs as test fixtures. Those
fixtures are omitted from the HUT fingerprint and frozen capability graph but
remain mounted so report-media discovery excludes them. Automatic and legacy
boots remain per-session and unlinkable.

Initial CPU and RAM workers are in-process reviewed Go adapters. Each receives
a global deadline, an independent thermal monitor, bounded memory/output, and
no target-storage, network, or external-command authority. A fail, error, or
safety abort prevents later active quick tests in that run.
Automatic boot hands the frozen plan to one synchronous Go session coordinator.
Its default five-minute deadline is shared across the selected workers. Each
worker also receives the smaller remaining session or frozen-test budget.
Graphics shares 30 seconds across its tools; display shares 45 seconds across
cards and the operator prompt. No timed-out adapter is detached to allow later
work to proceed. A 350-second outer KILL guard is terminal: nonzero exit blocks
ready/save even if a report was already written. It does not confirm cleanup
of nested processes or establish preemption of a blocked kernel call.

One console reader owns cancellation and requested finding lines. Ctrl-C,
SIGINT, or SIGTERM stops new work, preserves completed failures, and leaves
non-scheduled dispositions unchanged. Before a clean stop is finalized, the
reader is joined and terminal mode restored. Final safety has a fresh five-second
context. Final inventory, pre-sign safety, final thermal observation and both
monitor joins fit within a separate 30-second preparation/finalization context.
The two Stop calls share one deadline of at most two seconds, bounded by the
remaining preparation time. A monitoring failure cannot cancel its own cleanup
reserve, but it still prevents signing. An earlier preparation error has one
shared two-second cleanup allowance. Unsafe observations, interrupted safety
readiness, or unconfirmed cleanup prevent signing. Ordinary clean stops retain
explicit incomplete results and a signed stop reason.

The periodic mount/network monitor starts with the coordinator and remains
active through workers, console cleanup, final inventory and the final gate.
It owns one serialized read-only
mount/network observation at a time, using a one-second interval, a one-second
observation deadline, and a 2,500-millisecond maximum observation gap. Its first
unsafe, unknown, timed-out, or excessively delayed sample is retained and
cancels active work. A later safe result cannot clear it. Operator cancellation
does not invalidate an otherwise clean in-flight sample: the monitor must
finish and join before signing can proceed. A blocked observation or uncertain
join prevents signing, even if the final snapshot is safe.

The v3 milestone recorded bounded aggregate timing and coverage through final safety,
including leading and trailing gaps. A session shorter than the first
one-second tick can record zero periodic samples; its independent preflight
and final snapshots remain required. This is periodic sampling, not continuous
coverage or preemptive kernel containment. Automated and native validation of
this increment passed, including race checks and signed CLI workflows.
Rebuilt-image and physical qualification remain open.
The 350-second outer guard allows the five-minute run, worker cleanup (two
seconds), input cleanup (two), final safety (five), finalization (30), one common
error-cleanup allowance (two), and nine seconds of margin. Normal monitor joins
are inside the finalization bound, not separate serial allowances per monitor.

The `internal/periodic` engine now owns the observer lifecycle, scheduling,
deadlines, coverage gaps and immutable Stop result. Safemode supplies its
observation policy and unchanged v1 serialization. Focused tests preserve six
pre-extraction wire cases and the existing timing/cleanup rules. Full Windows
tests, vet, cross-builds, native Linux suites, race checks and CLI scenarios
passed with confirmed cleanup. These results qualify the extraction; the
subsequent run-wide implementation has separate qualification gates.

The run-wide thermal adapter uses the shared engine with an immediate initial
observation, a one-second interval and per-observation deadline, a
2,500-millisecond maximum gap, and a final request bounded to two seconds.
Initial completion precedes worker dispatch and report preparation, including
when every test is excluded. The final request follows inventory preparation.
Policy assessment and aggregation use immutable typed values; timed-out or
otherwise ineligible returns cannot increase accepted coverage, and a frozen
result cannot change after return.

Heat at or above 90 C cancels active and later work while monitoring continues.
The finding is independent of monitor failure and hardware-graph integrity.
Both monitors must join before signing; their final evidence determines the
signed stop reason and readiness marker. A local CPU/RAM heat observation also
latches the session finding, bound to the worker interval, exact source-file
digest and canonical full-evidence digest. Unavailable or partial telemetry
stays explicit and cannot establish safe temperatures. The monitoring boundary
includes execution, cleanup, final inventory and the final gate; report encoding,
output and saving are outside it.

Full Windows tests, vet and cross-builds passed for the earlier v4 thermal
increment. Its 24 native Linux amd64/386 suite runs passed 3,032 test/subtest
events and four race suites
passed 1,203, with no skips. All 15 CLI scenarios passed with confirmed cleanup;
the twelve session scenarios retained explicit unavailable temperature coverage.
Rebuilt images, physical sensor validation and the beta pilot remain open.

The local CPU/RAM monitor supervises an initial Linux thermal-zone/hwmon
observation before starting load. Each observation has a one-second deadline;
after a completed sample, the monitor waits 250 milliseconds before the next.
Only one observation is in flight. Normal completion permits the current
bounded reading to finish, and result construction waits for a confirmed join
within a separate two-second allowance. A blocked kernel call cannot be
preempted: an unconfirmed join instead stops the session and blocks signing.

The sampler filters disabled or faulted readings, uncalibrated VT1211/WM831x
channels, and PECI control/threshold values. Exact path aliases count once;
equal values do not establish sensor identity. Typed snapshots distinguish
available, partial, and unavailable telemetry, including rejected readings and
enumeration failures. Missing or partial telemetry does not establish safe
temperatures, even if the bounded computation passes. The initial CPU/memory v2 evidence
retains aggregate coverage, peak, monitor failure, and cleanup state only after
joining; a known calculation or RAM mismatch remains a failure during cleanup.
Old v1 execution evidence cannot substitute for these observations, while
historical signed-report verification remains unchanged.

This prerequisite passed Windows tests, vet, cross-builds, native Linux suites,
race checks, and command-line workflows. Rebuilt-image qualification remains open.
Fan, power, throttling and representative physical sensor validation remain
open. The local CPU/RAM guard retains its 250-millisecond sampling wait; the
separate run-wide adapter covers the other session phases and all-off runs.

The CPU scalar increment runs a fixed twelve-vector binary64 suite before the
first integer iteration and at each 256-iteration boundary. Each operation
checks cancellation, a shared failure latch and the actual immutable deadline;
late successful work receives no credit, while an in-flight mismatch remains a
failure. Joined per-worker rows form CPU v3 evidence with checked counter sums.
Counter overflow makes the result structurally unsignable, even if cleanup
changes its diagnostic. The shared decoder supplies copied scalar summaries to
consumers and preserves CPU v2's immutable wire encoding for historical hashes.
The CPU definition-2 increment retained the then-current v5 journal without
adding a catalog row or worker.
Host and native Linux validation passed, including race tests and signed CLI
paths. Rebuilt-image and physical checks for this increment remain open.

The RAM increment keeps the three existing fill/verify patterns, then writes
`0xaa`, verifies upward while writing `0x55`, verifies downward while writing
`0xaa`, and verifies upward again. Each completed pass has ten phases, six read
sweeps and six write sweeps. These are logical buffer operations, not bandwidth
or physical DRAM measurements.

Chunks of at most 256 KiB are admitted against the earliest parent or monitor
deadline, including checks before allocation and final success. Correct work
observed after a stop remains uncredited; an observed mismatch remains a failure
through cancellation, heat and cleanup. Checked counter overflow makes the
evidence structurally unsignable. The separate non-inlined kernel has no per-byte
callbacks. Inspection of amd64, x86 SSE2 and legacy x86 application builds
confirmed loads, comparisons, stores and descending indexing, without a claim
of cache bypass or physical-memory coverage. Full host checks, native Linux
suites, race tests and signed CLI workflows passed. Rebuilt-image and physical
qualification remain pending.

## Bounded exterior observation

`inspection.exterior.quick` definition 1 uses the explicit `operator.exterior`
capability and `whole_device_operator` applicability. Frozen plan schema 3
can select or exclude it without fabricating a hardware component. The current
catalog has 22 rows; modern images offer twelve selectable quick checks and
legacy x86 offers ten. Current v7 appends inspection after the eleven v5
workers. Historical v4/v5/v6 retain their original wire rules; v6 storage
reading does not activate storage sampling in v7.

The real Linux console owner is shared by session cancellation and operator
prompts. Exterior observation borrows that owner and discards input preceding
its prompt. The standalone command owns its input receipt. Both paths require
confirmed owner cleanup before successful output or signing; cleanup-uncertain
exit 12 is unchanged. A queued request fault survives concurrent cancellation.

Observation time covers prompt attempt through decision, capped at the earlier
effective or parent deadline. Owner preparation and join are excluded. A
decision at or after that deadline cannot pass even if context notification
lags; the evidence value is not a hard operating-system return deadline.

Finalization binds typed evidence to the plan, graph, result status and,
for a session claim, the v7 worker interval. Its fresh live safety gate and
complete cleanup precede signing. A shared validated accessor supplies a copied
result to rendering; no-view bundle verification applies the same claim rules.
Linked retesting records a fresh child observation and preserves exact parent
binding and independent trust. Comparison does not use an operator observation
as component identity or physical continuity.

Full host validation and the selected native Linux amd64/386 fixture suites
passed. Signed bytes authenticate an operator assertion, not the physical truth
or completeness of the inspection.

## Privilege and process model

The target privilege separation remains unimplemented. Run the UI as a
dedicated user and `hhptd` as root. Prefer read-only sysfs,
device ACLs, and Linux capabilities. An adapter receives only the device nodes
and output directory it needs. Every subprocess gets a timeout, process group,
Linux parent-death signal, bounded stdout and stderr, and a fixed environment
and working directory.

The initial graphics adapter narrows this contract further: absolute image tool
paths, validated frame counts, a 20-second subprocess timeout, and a 64 KiB
combined-output limit. It has no network or storage-mount capability.

On Linux, worker cleanup reserves the leader PID with non-reaping observation,
terminates its process group, and checks for remaining live members before
reaping. Cleanup, reaping, and output draining share a two-second allowance.
Unconfirmed cleanup returns a distinct error; graphics, display, and direct
capture stop later work and return exit code 12. The modern supervisor preserves
available diagnostics in RAM and stops before finalization or report saving.
This covers members of the owned process group, not processes that escape it,
arbitrary kernel stalls, or a killed supervisor. Sampled thermal observations
do not establish continuous coverage; heartbeat and hang supervision remain
incomplete.

Bind the API to a Unix socket. If a browser UI is used, place a random session
capability in the launch URL, bind only to loopback, reject cross-origin calls,
and never allow API access from report HTML.

## Storage and persistent state

Root is tmpfs backed by immutable image contents. Do not auto-mount internal
filesystems, replay foreign journals, import pools, or auto-activate RAID/LVM.
Mount report media with `nodev`, `nosuid`, and `noexec`.

The current report-destination flow discovers only separately inserted,
removable USB partitions. It requires an HMAC-authenticated typed challenge,
binds authorization to the current kernel boot and 15-minute monotonic expiry,
then writes only a verified signed bundle through an exclusively created plain
filename at the fixed RAM-prepared mount point. The operator must explicitly
mount the target; filesystem parsing is not automatic.

A finished USB layout will use a boot partition or hybrid image plus an
optional writable `HHPT_DATA` partition. Runtime writes are limited to current
session state and report bundles. Raw captured artifacts are bounded so FAT32
can remain a compatibility fallback; exFAT is preferred for a dedicated data
partition after legacy-machine testing.

The local lab stores VDI or QCOW2 files on NTFS and never passes a physical disk
through to test storage functions. Disposable drive-test VMs get a separate
sparse disk whose identity is recorded when the VM is created.

## Result state machine

Each check ends in exactly one state:

- `pass`: acceptance criteria met.
- `fail`: hardware behavior violated measurable acceptance criteria.
- `skip`: not applicable or intentionally declined, with a reason.
- `inconclusive`: applicable but insufficient evidence or operator input.
- `error`: test infrastructure or execution failed.
- `abort`: operator, thermal limit, power state, or safety policy stopped it.

Warnings annotate a state but do not replace it. Final overall state preserves
required-test policy; it is not a simple count of green rows.

## Security and trust

Security by design and layered containment are release requirements. Assume
firmware, attached media, local networks, device metadata, individual test
tools, and operator input can each be hostile or mistaken. Prevention,
least privilege, containment, detection, and recovery must remain independent.

Release images are reviewed immutable artifacts with an SBOM, checksums,
signatures, and GitHub build-provenance attestation. Runtime does not execute
content obtained from a website, report, inserted storage filesystem, or remote
test definition. Network checks contact only user-selected or allowlisted
targets and keep uploads disabled by default.

Report hashes prove content consistency, not truth. Strong auditability comes
from signed report manifests, key provenance, evidence, operator identity, and
an optional external append-only submission service. Fingerprints are privacy
sensitive and must never be treated as authentication secrets.

## Acceptance targets

- Boot to an operator menu on BIOS and UEFI VMs with no writable internal-disk
  mount and no network dependency.
- Produce the same canonical inventory and applicable plan on repeated boots.
- Complete a VM quick profile and verify its report on Windows and Linux.
- Refuse destructive storage operations against unapproved, changed, boot, or
  report media targets.
- Make every included binary traceable to its version, source, and license.

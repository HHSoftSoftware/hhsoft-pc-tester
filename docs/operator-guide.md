# HHSoft PC Tester Operator Guide

## Current Release Status

This guide covers the `0.1.0-alpha.1` qualification target. Downloads remain
unpublished until the release checks and approval pass. It is an alpha
hardware-inspection appliance, not a complete certification, repair, data
recovery, erase, or burn-in product. Use it only on equipment you are
authorized to inspect.

The normal workflow boots an offline live image, freezes a hardware-aware
quick-test plan, performs the currently implemented checks, creates a signed
`.hhpcr` report in RAM, and optionally saves that report to a separate removable
USB partition. The environment must not mount an internal disk read-write or
configure a network connection.

## Choose the Correct Image

Release files use names such as
`hhsoft-pc-tester-VERSION-x86_64.iso`. Select one image before creating the
boot USB.

| Image suffix | Use for | Firmware | Current coverage and limits |
| --- | --- | --- | --- |
| `x86_64` | Most 64-bit Intel/AMD PCs | Legacy BIOS or native x64 UEFI | Preferred modern image; full current quick workflow and the broadest open-driver set |
| `x86` | 32-bit PCs with CMOV and SSE2 | Legacy BIOS | Modern workflow; IA32 UEFI is not qualified or claimed for this alpha |
| `legacy-x86` | Pentium MMX-class or newer x86 PCs without SSE2 | Legacy BIOS, or UEFI with CSM/legacy support enabled | Reduced-coverage, lower-assurance fallback; no deployment-key mode and no active graphics/display quick tests |
| `aarch64` | ARMv8+ systems with standards-compliant AArch64 UEFI | Native AArch64 UEFI | Beta; generic QEMU boot is validated, but representative Snapdragon/X Elite hardware and device drivers are not yet qualified |

The modern images use pinned Alpine Linux packages. The legacy image uses a
separate minimal Buildroot/Linux base so that old non-SSE2 systems remain
bootable. Prefer `x86_64` or `x86` whenever the machine supports it. Do not use
`legacy-x86` merely because an x64 machine is booting through Legacy BIOS.

## Firmware Setup

Current images require **Secure Boot disabled**.

- On native UEFI, disable Secure Boot and leave CSM disabled. Select the UEFI
  entry for the HHSoft USB.
- On Legacy BIOS, select USB/optical-media boot normally. Secure Boot does not
  exist in this mode.
- Use CSM/legacy mode only for `legacy-x86`, or as a compatibility fallback
  for a modern x86 image when native UEFI boot fails.
- Do not enroll unknown keys, clear platform keys, or change TPM state to make
  this alpha boot. Those actions are unnecessary and can affect the installed
  operating system.
- Restore the customer's original boot order and firmware settings after the
  session.

If a UEFI boot entry and a non-UEFI entry are both displayed, choosing the
wrong entry can silently switch firmware modes. Confirm the expected image and
assurance label on the HHSoft splash screen.

## Prepare a Session

Use separate physical media for each role:

1. **Boot USB:** contains the selected HHSoft ISO.
2. **Report USB:** a formatted removable partition used only to receive the
   `.hhpcr` file. Use FAT32, exFAT, or compatible ext4. Ext4 must use extents;
   any journal must be internal, and stored mount-option overrides are rejected.
3. **Deployment-key token:** optional, modern images only, and needed only for
   comparable return/parts-swap reports. It must not be used as the boot or
   report USB.

Verify the ISO's SHA-256 against the release `.sha256` sidecar on a trusted
workstation before writing it. Creating a boot USB normally erases that USB;
check the selected target twice and never select a customer's internal drive.
The appliance has no in-place updater, so a new product release means verifying
and writing a new image to the boot USB.

Before booting, connect AC power where appropriate, provide adequate cooling,
disconnect unnecessary USB devices, and unplug network cables unless network
link detection is part of the inspection. Leave Wi-Fi unassociated. Back up
customer data before handling suspect hardware even though the quick profile
is designed to be non-destructive.

## Safe Quick Start

After the HHSoft splash appears, a 15-second inactivity timer begins.

- Do nothing to start the automatic safe read-only quick profile.
- Press any key, move a mouse, or use a touch pointer to cancel automatic
  start and show the operator menu.
- If input monitoring is unavailable, automatic start is cancelled rather
  than started blindly.
- Choose `1` or press Enter to start the safe quick profile with the current
  test choices. All applicable quick tests are selected by default.
- Choose `C` to choose quick tests. Enter a displayed number to toggle a test,
  `A` to restore the defaults, or Enter to return to the main menu. The choices
  apply when you start the run, including when you choose `K`.
- Choose `W` to remain at the menu. The automatic timer stays disabled.
- On a modern image, choose `K` to select a deployment-key token before the
  test. Legacy images do not offer `K`.

The quick profile is offline and uses read-only device discovery. It runs a
short CPU known-answer load, bounded RAM patterns and inversions, device-node
readiness checks, guided keyboard, relative-pointer and bounded single-contact
touchpad and touchscreen actions, modern-image graphics/display checks where
applicable, and the bounded exterior observation. Current development selection
covers fourteen tests on modern targets and twelve on legacy x86, subject to
applicability. Candidate discovery does not establish device support; see the
[touchpad limits](#bounded-touchpad-check) and
[touchscreen limits](#bounded-touchscreen-check).
Storage readiness opens eligible block devices read-only and issues no sector
read. USB readiness issues no application USB transfers. Network readiness configures
nothing and sends no traffic. An active non-loopback interface or writable
block mount causes safe mode to fail closed.

Network acquisition bounds discovery reads and closes each acquired handle
before accepting an observation. Permission, type,
overflow, incomplete-read and close failures become errors, with earlier
complete interface diagnostics retained and later acquisition stopped.
Optional missing attributes or successfully read malformed values remain
unknown. Only carrier, speed and duplex additionally permit a sole
first-read `EINVAL` with zero bytes as unavailable; partial or later read
errors do not qualify. Driver discovery falls back to bounded device
metadata when the link is absent or a successfully acquired target has an
invalid driver basename. NUL, overlong targets and acquisition faults
remain hard errors.

`hhpt network` returning zero means diagnostic JSON was written, not that
readiness passed. Network readiness is a session safety check: an error or
aborted result stops later workers and session signing. Separate manual
finalization may retain non-pass diagnostics, but cannot promote them to
pass. These acquisition changes add no configuration, interface activation
or network traffic. They do not establish deadlines for blocked kernel
calls, total allocation bounds or persistent physical identity.

USB readiness requires the selected registry identity and authorization to
agree with the direct device node and the opened descriptor. The descriptor
must close successfully before that device earns readable credit. Missing
or malformed required identity, duplicate device addresses, or a failed
close produce an error. An absent, inaccessible or replaced node remains
inconclusive; cancellation aborts the check. A simultaneous close failure
remains an error. Earlier device diagnostics are retained, and later USB
nodes are not opened after a close failure or cancellation.

USB discovery also limits attribute reads and registry enumeration before
using the observations. Oversized or incomplete input, invalid descriptor
types, read failures and failed closure produce an error rather than a
truncated readiness result. Optional values remain unknown only after a
clean missing-file observation or a successfully read malformed value.
Unreadable or oversized vendor metadata now fails discovery as well.
Earlier diagnostics remain available; total allocation and blocked kernel
calls are not qualified by these bounds.

`hhpt usb` returning zero means it wrote diagnostic JSON, not that USB
readiness passed. USB errors remain non-passing evidence through session
reporting and finalization. They do not introduce exit status 12 or a
global stop of unrelated session workers. No application USB data
transfer, reset or driver operation is issued; opening the kernel
interface can still cause normal runtime-resume activity.

The development CPU worker includes twelve fixed binary64 arithmetic checks.
Reports show the validated scalar check count, workers with a completed suite,
and distinct integer, SHA-256 and scalar failure flags. A worker may complete an
earlier suite and fail later; its completed count does not erase the failure.
These are bounded correctness samples, not performance scores or complete CPU
coverage. On legacy x86, soft-float checks software arithmetic and does not
validate a hardware FPU. Host and native Linux validation passed. Rebuilt-image
and physical qualification of this scalar increment remain pending.

The development RAM worker adds ascending and descending moving inversions
after the three original patterns, then verifies the final `0xaa` pattern.
Reports show completed passes and phases, accepted work, observed uncredited
work and any byte mismatch. Interrupted work is not a completed phase. A retained
mismatch remains a failure even when cancellation or heat also stops the worker.

The quick-session RAM worker keeps its 30-second limit and shares the remaining
session budget. Modern automatic selection uses at most a quarter of reported
available memory, capped at 64 MiB; legacy sessions use 16 MiB and one pass.
Unreadable availability data retains the conservative 4 MiB fallback. Explicit
standalone limits for `hhpt memory` stay at 4-512 MiB and one to eight passes.
This is an
owned userspace working set, not all installed RAM. Full Windows and native
Linux checks passed, including race tests and signed CLI workflows. Rebuilt-image
and physical-memory qualification remain pending.

Skipping a quick test does not disable the independent mount and network safety
checks. The report records the choices and marks applicable skipped tests as
`excluded`; hardware that is absent or lacks a driver keeps its corresponding
status. Legacy graphics/display checks remain unsupported. A test choice does
not enable an unsupported test or a destructive action.

The strengthened CPU/RAM guard in development source reports thermal coverage
as `available`, `partial`, or `unavailable`. A passing computation with missing
or partial telemetry does not prove safe temperatures. This local guard checks
before load and during those two workers, waiting 250 milliseconds after a
completed reading before sampling again. Each read has a one-second limit and
observer cleanup has a separate two-second allowance. Unconfirmed cleanup stops later
work and report signing. This change passed Windows tests, vet, cross-builds,
native Linux suites, race checks, and command-line workflows. Rebuilt-image and
physical-sensor qualification remain open.

Development source now adds separate thermal observations across the quick
session, including when all tests are excluded. It reads initially, at one-second
intervals, and after final inventory preparation. Observed heat stops work while
monitoring continues through cleanup and the final safety gate. A cleanly joined
stop can retain a signed report; it does not establish that the PC passed.
Missing telemetry is reported as a coverage limitation. Windows tests, vet,
cross-builds, native Linux amd64/386 suites, race checks and all 15 CLI scenarios
passed. The VM session scenarios reported unavailable temperature telemetry;
rebuilt-image and physical-sensor qualification remain open.

The graphics pattern and CPU/RAM checks are active loads despite the read-only
storage policy. Supervise unstable equipment. If there is smoke, sparking,
swelling, abnormal heat, or mechanical distress, remove power rather than
waiting for software shutdown.

## Optional Deployment-Key Token

Normal boots create a fresh fingerprint key in RAM. Reports from separate
normal boots are intentionally unlinkable. Use a deployment-key token when the
business purpose is to compare the same machine at sale and return.

Provision the token on a trusted offline Windows workstation:

```powershell
hhpt.exe deployment-key init -epoch shop-west-2026-1 `
  -output E:\hhpt-deployment-key.v1
hhpt.exe deployment-key inspect E:\hhpt-deployment-key.v1
```

`init` refuses to overwrite an existing file. Store the fixed
`hhpt-deployment-key.v1` in the root of a dedicated FAT32, exFAT, or ext4 USB
partition with a stable non-generic hardware serial. Keep at least two secured
offline backup copies and use a hardware write-protect switch when available.

At the modern boot menu:

1. Select `K`.
2. Select the listed removable token.
3. Type `LOAD` exactly.
4. Check that the expected epoch and non-secret key ID are displayed.
5. Leave the token inserted. HHSoft keeps it mounted read-only and excludes its
   block and USB components from the system under test.

The token holds a plaintext 256-bit HMAC key plus a corruption check. It is
not a report-signing key, release key, encryption key, or proof of operator
identity. Theft permits correlation and fabrication of hardware pseudonyms
for that epoch. Never place it in source control, a report, a general-purpose
technician USB, or the public project. Use the same exact token for baseline
and return sessions. A new key/epoch deliberately makes later reports
unlinkable to earlier ones.

## Watch the Run

The console shows compact `HHPT_*` progress markers. Important markers are:

- `HHPT_PLAN`: the test plan and initial hardware graph have been frozen.
- `HHPT_CPU`, `HHPT_CPU_STRESS`, `HHPT_MEMORY`, `HHPT_INPUT_READINESS`,
  `HHPT_KEYBOARD`, `HHPT_POINTER`, `HHPT_TOUCHPAD`, `HHPT_AUDIO_READINESS`,
  `HHPT_NETWORK_READINESS`, `HHPT_STORAGE_READINESS`, `HHPT_USB_READINESS`,
  `HHPT_GRAPHICS`, `HHPT_DISPLAY`, and `HHPT_EXTERIOR_INSPECTION`:
  individual outcomes.
- `HHPT_RUN_INTEGRITY status=pass`: the final component graph matches the
  frozen plan. A `fail` plus nonzero added, removed, or changed counts means
  hardware changed during the run; preserve the report but repeat the test
  with a stable setup.
- `HHPT_SESSION_STOP`: why quick work ended, including completion, operator
  cancellation, a session/test deadline, or a worker stop. Review the recorded
  outcomes even when the stop reason is `completed`.
- `HHPT_READY`: the bundle was finalized in RAM and the save menu is ready.
- `HHPT_SAFETY_FAIL` or `HHPT_FAILED`: the safe appliance guarantee or
  workflow failed. Do not treat the session as a completed inspection.

Quick-session work has a shared five-minute budget. The `session` command
accepts a `-timeout` from `10ms` through `15m`; the boot workflow uses `5m`.
Each scheduled test also uses its frozen plan limit. Graphics has a 30-second
whole-test limit and display has a 45-second limit shared across sequential
tools, cards, patterns, and the operator-finding prompt. Cleanup and safe report
finalization have separate bounded allowances.

The keyboard step asks for left Shift, right Shift, Up, Down, Left and Right.
Press and release each control on every keyboard; do not type text. Its default
window is 30 seconds within a 90-second limit and the remaining session budget.
Release initially held controls before beginning. Every interface must support
and complete all six controls; one keyboard cannot supply another's actions.
Missing actions mean incomplete coverage, not proof of a defective key. Full
layout maps, other keys, rollover and ghosting remain untested. Reports retain
fixed action-completion booleans, without text or an event sequence. The console
owner discards queued keyboard input before accepting later display findings.
Software qualification passed on Windows and native Linux amd64/386, including
race checks and command-line workflows. Rebuilt images and physical keyboard
validation remain separate gates.

The pointer step observes all supported relative pointers during one default
30-second window, within a 90-second whole-test limit and the remaining session
budget. Move each pointer left, right, up, and down. Press and release its
primary button and every advertised right or middle button. Scroll each
advertised vertical or horizontal wheel in both directions. Each interface must
complete its own actions; using one mouse does not qualify another. The check
requires relative X/Y events. Absolute or mixed relative/absolute interfaces,
including many touch devices, are outside this check and cannot pass it.
A device without an advertised wheel does not require wheel actions. Reports
show positive/negative frame counts, not detents or precision. High-resolution
events are used when advertised; their coarse companion events are not counted
again. Extra buttons, tracking accuracy, wheel precision and touch gestures
remain untested.
Only bounded action summaries and counts enter evidence, not raw events or
typed text. Missing actions, lost records, and disconnects leave the inspection
incomplete. Pointer definition 2 requires a fresh plan. Windows and native Linux
amd64/386 suites, race checks and CLI workflows passed for this increment.
VM observations without supplied actions remained incomplete; rebuilt images
and physical qualification remain open. Historical pointer v1 reports retain
their movement/button scope.

Press Ctrl-C to request a stop. One owned console reader handles cancellation
and display/exterior findings, so those inputs do not compete. Linux SIGINT and SIGTERM
also request cancellation. New scheduled work stops, completed failures remain
failures, and unfinished work is recorded as incomplete. The console reader is
joined and its input flags and terminal mode are restored before signing or
saving. A clean stop can still produce a valid signed report when cleanup and
safety checks succeed; that report records an incomplete inspection, not a
hardware pass.

The display check may require operator observations because software cannot
reliably judge pixels, color, flicker, scaling, overscan, or visible artifacts
by itself. Every applicable display pattern and every operator criterion must
pass for the display result to pass. A passing observation on one output cannot
override a failed or incomplete pattern on another; partial findings remain in
the result. A readiness result only proves that the associated kernel interface
could be accessed, not that every key, button, port, pixel, cell, link, or
media sector works.

## Bounded Touchpad Check

Development source runs the touchpad check after pointer in Quick v14 and
Standard v15. A detected candidate may still be unsupported; the implemented
subset is legacy single-contact absolute-event input, not multi-touch or
gesture coverage. Quick's `C` menu can exclude the check. Standard requires
the default set and rejects nonempty Quick exclusions.

Follow the displayed instructions: lift all fingers, then use one finger to
move left/right and up/down repeatedly. Lift to finish the contact, and press
and release the primary button. Repeat motions if needed; unchanged axes may
delay a fresh baseline. Each interface must supply its own observations.
Missing actions are incomplete coverage, not proof of a defective touchpad.

The default observation window is 30 seconds within a 90-second command limit
and the remaining session budget. A standalone diagnostic is available:

```sh
hhpt touchpad -duration 30s -timeout 90s -output touchpad.json
```

Exit 0 means validated diagnostic JSON was written, including non-pass results;
it does not mean the touchpad passed. Incomplete actions, lost records, changed
devices and uncertain cleanup cannot become a pass. Cleanup uncertainty retains
exit 12 and stops session signing. Evidence contains bounded action summaries,
not raw event traces or coordinate samples. Direct capture omits this guided
check along with keyboard, pointer and exterior observation.

The connected route passed Windows software fixtures, including signed
finalization and report views. Earlier native Linux core/adapter fixtures do
not qualify the later integrated route. Integrated Linux/native checks, real
evdev/kernel behavior, installed-command and rebuilt-image checks, physical
touchpads, the pilot and release approval remain open. Edges, tap, multi-touch,
scroll and gestures remain outside this check. See
[the exact scope](requirements-validation.md#touchpad-inventory-and-strict-evidence).

## Bounded Touchscreen Check

The development menu labels this step **Touchscreen movement and release**.
Connected Windows software qualification covers the command/session,
finalization and signed report/view route. Selected native Linux amd64 and 386
fixtures also passed all 2,952 named outcomes without skips or failures.
Real evdev/device behavior, installed-command checks, ARM runtime, race checks,
rebuilt images, physical touchscreens, the pilot and release approval remain
open. See the [touchscreen scope and validation status](touchscreen-quick.md).

Lift every finger before starting. Touch with one finger, move left/right and
up/down while keeping that contact, then lift to finish it. Repeat if needed;
unchanged coordinates can delay a fresh starting point. Each supported screen
must supply its own complete movement and release. Missing actions are
incomplete coverage, not proof of a defective screen.

The development command uses a 45-second observation window within a
120-second command limit and the remaining session budget:

```sh
hhpt touchscreen -duration 45s -timeout 120s -output touchscreen.json
```

Exit 0 means validated diagnostic JSON was written, including non-pass results;
it does not mean the screen passed. Cleanup uncertainty retains exit 12 and
prevents session signing. Evidence contains bounded action summaries rather
than coordinate traces. Multi-touch, stylus, pressure, calibration, full-panel
coverage and gesture recognition are unsupported by this check. Direct capture
retains input readiness and does not run guided touchscreen actions.

## Start Standard from the Development Console

The updated startup scripts offer `S` followed by Enter to request standard.
The menu discloses the 600-second CPU load and 15-minute session budget before
you choose it. Automatic start, Enter and `1` still select quick. On modern
images, `K` remains a quick run with deployment-key media; `S` uses session-local
identity on both modern and legacy paths.

Standard requires the default test set. If you excluded quick tests, enter `C`,
choose `A` to restore the defaults, return to the main menu, then select `S`.
A refused standard choice leaves your quick selections intact. No selection is
silently discarded to start the longer profile.

The console exposes this choice when `ui -launch-selection-output` names the
new handoff file. `plan -launch-selection-file` consumes that file with the same
profile and architecture. The new UI flag cannot be combined with
`-selection-output`, and the new planner flag cannot be combined with
`-selection-file`. An explicitly empty new path is rejected; existing quick
selection flags remain supported.

The updated scripts freeze the launch selection and plan and pass the same
profile to planning and the session. Quick keeps a five-minute session budget
and 350-second outer supervisor policy; standard uses 15 minutes and 950 seconds.
The outer deadlines do not establish complete process cleanup. Invalid handoffs
and failed planning prevent the session; session or final safety failures prevent
ready/save progression and retain available diagnostics.

The earlier Standard console increment passed host validation and selected
native Linux amd64/386 tests, along with all 18 startup-shell fixture methods
using owned command stubs. Native
Linux amd64 and 386 VM runs also exercised the explicit `S` path through a
matching frozen plan, session and CLI verification of a self-issued report.
Cancellation smoke and one full 600-second CPU load per architecture completed
with confirmed cleanup. Those runs used synthetic 35 C thermal observations
and preceded touchpad activation. The later integrated touchpad route retains
its separate Linux/native gate. Qualification of the four rebuilt bootable
images, physical hardware, the
ten-PC pilot and release approval remains separate; see the
[console validation record](requirements-validation.md#standard-console-launch).

## Optional Standard CPU Stress

This development path is opt-in. The earlier CPU stress increment passed host,
selected native and real elapsed-window fixtures; image and physical
qualification remain open.
Automatic boot and an omitted session profile continue to select quick; the
updated development console requires explicit `S` to choose standard. A standard
plan does not by itself authorize the longer load; select the same profile in
both commands:

```sh
hhpt plan -profile standard -fingerprint-key fingerprint.key -key-epoch local-1 -output standard-plan.json
hhpt session -profile standard -plan standard-plan.json -fingerprint-key fingerprint.key -key-epoch local-1 -output standard.hhpcr
```

Standard defaults to a 900-second session budget, the existing maximum. It adds
one worker after a passing CPU quick check. Current Standard v15 has fifteen
workers and Quick v14 has fourteen.
CPU stress requests 600 seconds of shared elapsed load
following preparation, within a 660-second local watchdog that starts at invocation.
A shorter remaining session budget may prevent load or leave an incomplete result;
the command does not silently shorten the requested window into a pass.

Before load, exposed temperature observations must have available accepted
coverage. Partial or unavailable initial coverage ordinarily prevents load and
produces an inconclusive result; observed heat at or above 90°C or an observer
fault takes precedence. Later coverage loss stops work. Accepted readings do
not prove that all CPU temperatures are measured. Cancellation retains prior
failures, and unconfirmed cleanup prevents signing. Review the outcome and stop
reason even when a diagnostic command exits successfully.

`hhpt cpu-stress -workers 0 -output cpu-stress.json` emits standalone diagnostic
JSON, not signed standard-session coverage. Zero workers chooses the detected
CPU count up to 64; explicit counts are 1 through 64. Duration, watchdog and
thermal limits have no command-line override. Exit 0 means a validated diagnostic
was emitted, including a non-pass outcome; 2 indicates invalid arguments/options,
1 indicates failed accounting, validation or output, and 12 indicates uncertain
cleanup. The signed session requires its own frozen plan and fresh safety gate.

The bounded check is not a benchmark, utilization measurement, burn-in certificate
or proof of physical CPU health. The full host suite and selected native fixtures
on Linux amd64 and 386 passed. Separate API-level runs completed the real shared
600-second window on both architectures with two workers and fixed synthetic
temperature observations of 35 degrees C. All-core saturation and physical
thermal behavior remain unqualified. Rebuilt images and physical validation
remain open; see [the validation record](requirements-validation.md#bounded-standard-cpu-stress).


## Standalone Memory Stress

Development source provides a separate `hhpt memory-stress` JSON diagnostic.
It requests a fixed 30-minute load over one owned working set, with fresh
availability and thermal admission, final verification and a shared 31-minute
watchdog. It is not run by automatic boot, Quick or Standard.

```sh
hhpt memory-stress -output memory-stress.json
hhpt memory-stress -bytes 16777216 -output memory-stress-16m.json
```

Automatic size selection uses at most one quarter of freshly observed available
memory, capped at 64 MiB and rounded down to whole MiB. An explicit size must be
4-64 MiB and fit within the same fraction. Unlike the quick worker, this command
has no conservative fallback when availability cannot be established. It has
no duration, pass-count, alternate-root or thermal-limit override.

Choose a new output file. Exit 0 means valid diagnostic JSON was written;
inspect `status` and `stop_reason` to determine the outcome. Exit 12 means
cleanup was not confirmed and no normal diagnostic was emitted. A mismatch
cannot become a pass because cancellation or heat followed it. See
[the complete memory-stress guide](memory-stress.md) for the other exit codes,
thermal requirements, timing and coverage limits.

At the initial standalone checkpoint, `memory.stress` remained inactive and the
catalog had fifteen implemented/seven inactive rows. Current source connects it
only through explicitly selected Extended v16; the catalog has sixteen
implemented/six inactive rows. Quick v14/fourteen workers and Standard
v15/fifteen workers with its 900-second budget retain their schedules. This
standalone JSON still does not provide signed-session authority.

Host fixture tests passed all 196 named cases without skips on each of Windows
amd64 and 386. The full Windows suite and vet passed; that host step only
compiled the Linux application and test artifacts. Subsequent selected native
Linux amd64/386 fixtures passed 392 named outcomes across 62 roots and six
package executions, with all 330 declared-parent intervals checked and no
failures or skips. Nine ownership cases, 32 archive cases and six postchecks
also passed, with cleanup confirmed.

These controlled fixtures do not qualify a real 1,800-second run. Race checks,
the full Linux suite, ARM runtime, installed-command execution, four-image,
physical-memory/thermal, ten-PC pilot and release qualification remain open.

## Observe the Visible Exterior

The current menu includes **Visible exterior condition (operator reported)**.
One prompt lists four criteria in this order:

1. Enclosure integrity.
2. Visible hinge and mount condition.
3. Visible port exterior condition.
4. Visible surface finish.

Enter one answer and press Enter for each criterion: `D` for an observed issue,
`N` for no issue observed, or `I` for unobserved or uncertain. Empty answers also
remain unobserved. Only the hinge answer accepts `A`, meaning you explicitly
report that the device has no hinge. A hidden or uninspected hinge stays
unobserved; the software never infers not-applicable from the device type.

All four answers share one 45-second window, shortened by a parent deadline.
The timer does not restart for each criterion. A manual pass requires explicit
`N` for enclosure, port exterior and surface finish, plus `N` or `A` for the
hinge. Any issue remains adverse; any unobserved criterion prevents a pass.
Completed observations survive a stop between answers, and a completed issue
remains adverse after cancellation. Hard input or owner-cleanup faults retain
their error behavior. No free text, operator name or photograph is collected.

This is a visual observation of the surfaces actually inspected, without moving
or opening the device. It is not an automated hardware pass, a check of hinge
movement or port function, or a verdict on hidden damage, cooling noise, battery
condition or repairs.

For the current standalone development check on Linux:

```sh
hhpt inspect-exterior-condition -timeout 45s -output exterior-condition.json
```

The default timeout is 45 seconds; accepted values are `10ms` through `45s`.
Input uses LF or CRLF line endings; a bare CR is incomplete. The JSON diagnostic
has schema v2 and fixed operator-reported visual scope. It is not a signed
hardware report. Exit 0 means the record was emitted, not that the device passed
inspection. An existing output file is not replaced. The real console path is
Linux-only; Windows companion verification supplies no Windows console reader.

The unchanged legacy command emits the original one-answer v1 diagnostic:

```sh
hhpt inspect-exterior -timeout 45s -output exterior-inspection.json
```

Current session plans use exterior definition 2 and Quick v14, Standard v15 or
Extended v16 journals. Historical v1 observations retain their original meaning.
A fixed error/aborted nonexecution note has no condition observations; it cannot
substitute for the matching current journal required by actual v2 evidence.

Reports label the value **Bounded observation time (ms)**. It covers the one
prompt attempt through the last response or stop decision, capped at the
effective deadline. Preparation and final cleanup are excluded. Decisions at
or after the deadline cannot pass; this is not a return-time guarantee.
Unconfirmed input cleanup prevents output or signing and retains exit 12.

Earlier exterior qualification belongs to the legacy v1 source. The corrected
v2 source passed Windows host tests, formatting, vet and Linux cross-builds.
The host-stage Linux builds were compile-only. Subsequent selected Linux
amd64/386 inspection fixtures passed 1,296 outcomes across 12 streams with
no skips or failures, and one separate synthetic standalone CLI exercise
passed. Installed-command and physical inspection qualification remain open.
See
[validation details](requirements-validation.md#bounded-exterior-inspection).
Installed-command behavior, the physical-PC pilot, rebuilt images and release
approval remain open.

## Read Power-Supply Inventory

The implemented Linux inventory shows driver-reported power supplies as
`power_supply:1`, `power_supply:2`, and subsequent rows. These labels are local
to a collection and may change; they are not serial numbers or evidence that
the same battery or adapter was seen again. Raw supply names and serials are
not exported. No supply rows means no reported data, not proof of absent hardware
or operation on AC power.

Every field has a state. `reported` provides an admitted driver value, including
the literal enum value `Unknown`. `unavailable` provides no value;
`unsupported`, `malformed` and `out_of_range` also omit the rejected value.
None of these states supplies a health verdict. Missing `present` does not
mean present. `online` distinguishes 0 (offline), 1 (fixed supply online) and
2 (programmable supply online); it is not a Boolean.

Capacity is a reported percentage. Energy values are in micro-watt-hours and
charge values in micro-amp-hours; these units are not interchangeable. A zero
cycle count is shown as unavailable, not zero wear. Full and design values are
preserved without computing a health ratio or remaining runtime. Fields are
read separately and do not establish a power transition or simultaneous state.

The full host suite and selected native Linux amd64/386 fixtures passed; see
[validation details](requirements-validation.md#power-supply-inventory).
Battery-health assessment, AC/battery transition tests, physical-PC testing and
rebuilt-image qualification remain separate work.

## Read Firmware and TPM Inventory

The Linux inventory reports a finite set of firmware observations:

| Field | Meaning of a reported value |
| --- | --- |
| UEFI platform bits | The exposed firmware interface reports 32 or 64 bits. |
| SecureBoot | The firmware reports the flag as 0 or 1. |
| SetupMode flag | The firmware reports this separate flag as 0 or 1. |
| TPM major version | An enumerated interface reports version 1 or 2. |

Every field also has a state. `reported` includes an admitted value;
`unavailable` has no value because the observation was unavailable. `malformed`
and `unsupported` omit rejected input. Unavailable does not mean disabled or
absent, and a reported TPM version does not prove that the TPM works or is trusted.
SecureBoot and SetupMode are separate samples, not a complete firmware-mode or
security assessment. Even the 1/1 combination is retained literally.

The `firmware_security:uefi` row appears when any of its three reads completes.
TPM rows use `tpm:1`, `tpm:2` and subsequent labels local to the collection.
These labels may change and do not identify the same physical component across
reports. Raw names and stable identifiers are not exported; identity remains
unavailable. No rows means no reported data, not proof that a capability or
device is absent.

These fields belong to the frozen configuration. If they change before
finalization, the signed report retains a failed-integrity diagnostic and the
changed component references; prepare a new plan before future execution on the
new configuration. Comparison preserves both observations without inventing
component continuity or a security verdict.

The full host suite and selected native Linux amd64/386 fixtures passed. The
producer performs no measured-boot verification, TPM command, provisioning or firmware-policy
change. It does not add Secure Boot image support: current images still require
Secure Boot disabled. Keep the existing firmware setup instructions and physical
qualification limits. See [the field contract](requirements-validation.md#firmware-and-tpm-inventory).

## Read Memory Device Inventory

When Linux exposes eligible SMBIOS Type 17 entries, inventory includes
`memory_device:N` rows. Read each value together with its `_state` field:

| Field | Meaning when reported |
| --- | --- |
| `capacity_bytes` | Firmware's selected capacity observation, in decimal bytes. |
| `memory_type` | A finite type label such as `ddr4` or `ddr5`. |
| `form_factor` | A finite form-factor label such as `dimm` or `sodimm`. |

`reported` includes a value. `unknown` means the firmware field does not
supply a known value; `unavailable` means the entry could not be observed.
`malformed` means the record or selected field fails its required structure.
`unsupported` means the encoding is outside the product's admitted set.
The last four states omit the value; none substitutes zero.

A reported capacity of `0` is firmware's empty-entry observation. It does not
prove that a physical slot is empty. No rows means no reported data. Do not sum
these rows to infer usable OS memory, or infer RAM health from any of these
fields. `observation_source=smbios_type17` identifies the observation source,
without authenticating firmware.

Ordinals can shift between collections. They are neither slot labels nor stable
module identifiers, and identity remains unavailable. Raw source names, strings
and serial/location data are excluded. A comparison can show changed values,
states or rows without proving whether a physical module changed.

These observations belong to the frozen configuration. If they change before
finalization, the signed report retains a failed-integrity diagnostic and exact
changed component references. Prepare a new plan before future execution on the
new configuration. Host tests and selected native Linux amd64/386 fixtures
passed; physical systems and image availability need separate qualification. See
[the field contract and remaining gaps](requirements-validation.md#memory-device-inventory).

### Read the Memory-Capacity Summary

Current report views show the exact decimal-byte "Sum of reported firmware
entries", or "Reported-entry subtotal (partial)" when some observations are
incomplete and at least one valid reported capacity exists. If none exists,
the firmware amount is unavailable; all-unknown data does not become a zero
subtotal. A valid reported zero remains an explicit unpopulated observation.
The OS observation also shows its original KiB value; 1 KiB is 1,024 bytes.
Populated, unpopulated, unknown, unavailable, unsupported, malformed and invalid
counts stay distinct.

An equal, OS-lower or OS-higher relationship appears only for complete,
unambiguous eligible firmware observations and a positive valid OS total.
Type eligibility applies to populated entries: mixed, nonvolatile, unknown or
unsupported types there prevent comparison. An empty entry's unknown type does
not itself disqualify eligible populated entries.

Invalid, ambiguous or overflowing firmware data suppresses the firmware
aggregate; an invalid or ambiguous OS observation, or OS conversion overflow,
suppresses the OS amount. Either prevents comparison, while a valid observation
on the other side stays visible. Missing values are never filled with zero.
No firmware rows and a reported all-empty set remain different states, and
neither establishes a comparison.

These are recorded observations, not a measurement of installed or usable RAM,
complete module coverage or health. Firmware scope, reserved memory and
collection timing can differ from the OS view. The summary uses the original
inventory, not a correction addendum, and does not change test outcomes.
Windows fixture validation of this view profile passed. Linux execution,
installed-image and physical-PC qualification remain open; see the
[current validation scope](requirements-validation.md#memory-capacity-report-view).
The memory-device inventory tests above retain their original scope.

### Read Maximum PCIe Link Observations

A PCI inventory row may include `max_link_speed_mts` and `max_link_width_lanes`,
with `max_link_speed_state` and `max_link_width_state` fields. Speed is in millions of transfers per second:
`8000` means 8 GT/s. Width is the advertised lane count. These are kernel-reported
device capabilities; they do not measure the negotiated link, usable throughput
or the limits of the complete path through bridges and docks.

A value appears only with `reported`. `unknown`, `unavailable`, `read_error`,
`unsupported` and `malformed` retain different observation states. Neither an
absent field nor an unreadable value becomes zero or a hardware failure. When
both capability files are cleanly absent, the existing PCI row has no new fields;
that absence does not prove whether the device supports PCIe.

Maximum observations belong to the frozen configuration. A change before
finalization retains the signed integrity diagnostic; prepare a new plan for
later execution on the changed configuration. Current-link files are not read
as maximum capabilities; their separate observations are described below.
PCI continuity remains inconclusive without stable identity,
even when two reports show equal or different capabilities.

The full Windows fixture suites passed. Native Linux, installed-image and
physical-PC qualification remain separate; see the
[current scope](requirements-validation.md#maximum-pcie-link-capability-inventory).

### Read Negotiated PCIe Link Observations

PCI rows can also show `current_link_speed_mts` and
`current_link_width_lanes`, with an independent `_state` for each field. For
example, `2500` MT/s and `1` lane are kernel-reported observations at collection
time. They do not establish measured throughput, a simultaneous speed/width
sample, the slowest bridge on a path, or a health result. A lower current value
than the advertised maximum is not automatically classified as degradation.

Only `reported` carries a number. Missing, unreadable, malformed and unsupported
values stay distinct. Speed `Unknown` is unknown; current widths `0` and `255`
are unsupported, not zero lanes or proof of an absent device. Two cleanly absent
current files add no fields. Reading the attributes can involve kernel power
management even though HHSoft does not write device controls.

Current observations are signed with the report and remain visible in HTML/PDF
and comparison. Changing current values alone does not change the frozen plan
or configuration-integrity result. Maximum/configuration changes still do.
These observations provide no stable PCI identity or continuity guarantee.
Native Linux, installed-image and physical-PC qualification remain separate
from fixture tests; see the
[negotiated field contract](requirements-validation.md#negotiated-pcie-link-observations).

## Save the Signed Report

The only authoritative artifact is the `.hhpcr` bundle. Its HTML and PDF views
are convenient signed content inside the bundle, but a copied-out view cannot
be independently verified.

After `HHPT_READY`:

1. Insert a separate, formatted report USB. Do not use the boot USB or the
   deployment-key token.
2. Press Enter to rescan.
3. Select one eligible removable partition from the numbered list.
4. Verify the displayed device and size, then type its `RPT-...` confirmation
   code exactly.
5. Wait for `Saved and read-back verified` before removing the report USB.
6. Retain the complete `.hhpcr` file and copy it to operator-controlled backup
   storage on a trusted workstation.

HHSoft mounts only the selected target with `nodev,nosuid,noexec`, verifies
the self-issued report before writing, creates a new file without overwriting
an old report, synchronizes it, reads it back byte-for-byte, and unmounts. The
target must be a directly attached removable USB partition with a stable
identity; mounted, mapped, read-only, internal, boot, and token media are
excluded. If no destination is eligible, use another separately formatted USB
device. A report is limited to 64 MiB.

Before mounting, HHSoft reads a bounded filesystem header and rejects unknown,
ambiguous, or unsupported configurations. This recognizes the filesystem type;
it does not check filesystem integrity or qualify the USB media. The save flow
rechecks authorization and binds the mount to opened device and directory
handles. Keep the selected USB connected and the setup stable throughout saving;
these protections do not eliminate every physical hot-plug race.

Select `Q` before attempting a save to leave without writing. After a failed
attempt, `Q` ends the save flow without another attempt; an unverified partial
file may remain on the USB. The original report stays in volatile RAM and is
lost on power-off or reboot.

If writing fails and HHSoft successfully unmounts the target, the save menu
returns. Rescan or select another formatted USB and type its current confirmation
code. Retries do not overwrite or remove an existing partial file, so saving to
the same target with the same filename will fail. Keep power on until a verified
copy exists. If unmounting fails, HHSoft stops further save attempts; it cannot
confirm that the USB is safe to remove or that the report was saved successfully.

The current quick workflow creates an ephemeral **self-issued** Ed25519
signature. It makes the saved bundle tamper-evident and binds all results to
the frozen plan, but does not establish trusted operator identity, trusted
time, custody, or an enrolled HHSoft/deployment signer.

Before signing, capture and finalization check that worker evidence has the
required observations and is consistent with its claimed result. A `pass`
without sufficient work or with contradictory safety observations is rejected.
Valid incomplete or unsuccessful observations can still be reported with their
non-pass status. These checks establish consistency within the implemented
check's scope; they cannot prove that hardware or a compromised collector told
the truth.

Capture checks both its 32-byte fingerprint key and 64-byte raw Ed25519 signing
key before collection. Each input must be a bounded regular file; its readable
handle must match the acquired metadata object, and both owners must close
successfully before the key is accepted. Invalid input or unconfirmed closure
stops capture with a fixed input diagnostic. Cancellation is checked between
key acquisitions and before session-ID randomness and collection. The existing
later worker, signing and output policy, including worker cleanup exit 12,
is unchanged. Host and selected Linux amd64/386 fixtures passed; these checks
do not establish blocked-call deadlines, secure key erasure or physical-media
qualification.

### Finalizer fingerprint input

The finalizer checks its 32-byte fingerprint key through
bounded regular-file acquisition. Metadata establishes eligibility before
readable acquisition; the readable handle must match that object, and both
owners must close before bytes are accepted. Input, size or closure faults
return a fixed key-input diagnostic. Observed cancellation before or just after
acquisition stops later finalization work.

These checks follow existing argument, predecessor, outcome and evidence-directory
validation and precede frozen-plan loading. Earlier authorized predecessor reads
can therefore occur. The already verified parent summary is not reopened merely
to acquire the fingerprint key. Cleanly stopped sessions retain their fresh
bounded finalization context, and later safety, signing, output and worker
cleanup-exit-12 policy remain unchanged.

Host and selected Linux amd64/386 fixtures qualified this input boundary
(see the [validation record](requirements-validation.md#finalizer-fingerprint-input-ownership)).
Five legacy raw-key calls in discovery, persistence and save remain, along with
frozen-plan/evidence readers and persistence's live verified-report owner.
These input checks add no blocked-call deadline, secure key-erasure or durability
guarantee.

### Report-target fingerprint input

The report-target input implementation accepts its fingerprint key only after
bounded regular-file acquisition and checked closure of both input owners.
It requires exactly 32 bytes. A key-input or closure fault produces a fixed
diagnostic without including the path or provider error. Cancellation observed
before acquisition or after successful closed capture stops discovery or
authorization before it begins.

The existing flags and typed confirmation remain unchanged. A catalog with
status not_present, inconclusive, error or aborted is still written as data;
a zero exit status means output succeeded, not that eligible media were found.
Authorization failures keep their existing diagnostic and produce no output.
Exclusive destination creation and caller-owned standard output are unchanged.

Windows host and selected Linux amd64/386 fixtures passed for this command
boundary. The fixtures inject discovery and authorization or use synthetic
media metadata; they do not qualify installed-CLI or physical-media execution.
The explicit unsupported-input case on other OSes remains unexecuted.
Four legacy raw-key calls remain: deployment discovery, two persistence inputs
and save. Those inputs and persistence's live report owner remain separate work.
Existing image, physical-PC and release gates remain open.

### Save fingerprint input

Save accepts the 32-byte fingerprint key only after checking the input file and
successfully closing its input handles. Usage checks and audit initialization
still happen first. Key-input or cleanup failures stop before report access and
later save operations, with a fixed diagnostic that omits the key path and
provider details. Cancellation cannot hide an input or cleanup failure.

Key-input failure and cancellation retain save's failure exit status 1. Report
verification, menu and confirmation, retry, write-attempt, unmount and receipt
behavior remain unchanged. Host and native fixture results are recorded in the
[validation record](requirements-validation.md#save-fingerprint-input-ownership).

The four-call count above describes the report-target stage. This implementation
leaves three legacy raw-key calls: deployment discovery and two persistence
inputs. Audit and live report-source ownership still need separate work. These
fixtures do not qualify installed-CLI execution, unsupported-platform acquisition,
physical media, blocked-call deadlines, secure erasure or durability. Existing
image, physical-PC and release gates remain open.

### Persistence key inputs

Persist accepts each required 32-byte key only after checking its input
and successfully closing the key's handles. Usage checks and authorization-file
reading and decoding still happen first. Decoding alone does not authorize a
write to the target.

Fingerprint-key failure stops before the report is opened. Trusted verification
still opens the report before reading its public key; self-issued verification
does not read a public key. Key-input and cleanup failures use fixed diagnostics
and return status 1. Cancellation cannot hide those failures. Existing report
verification, target authorization, receipt and output behavior remain unchanged;
see the [validation record](requirements-validation.md#persistence-key-input-ownership).

The preceding three-call count describes the save stage. This update leaves one
legacy raw-key call in deployment discovery. Authorization-file and live report
ownership still need separate work. It adds no installed-CLI, unsupported-platform,
physical-media or durability qualification; existing image, physical-PC and
release gates remain open.

### Deployment discovery-key input

`deployment-key load-media` accepts the required 32-byte session discovery key
only after its input checks and key-handle closure succeed. Usage checks and
the audit-output attempt still happen first. Key-input or cleanup failure stops
before activation availability, media discovery, confirmation or token loading;
cancellation cannot turn that failure into success.

The existing choice and confirmation flow is unchanged. The selected token still
supplies the activated deployment key and epoch, and successful activation leaves
its media mounted read-only. Input failures return 1 with a fixed diagnostic;
existing downstream diagnostics and activation/output behavior are unchanged.
Host and selected native software fixtures passed; see the
[validation record](requirements-validation.md#deployment-discovery-key-input-ownership).

This removes the final legacy raw-key call recorded at the persistence stage.
It does not finish audit, token, mount, activation-output or live report ownership
work. It adds no installed-CLI, unsupported-platform, physical-media or durability
qualification; existing image, physical-PC and release gates remain open.

## Interpret Results

Read both the frozen plan status and the final result. They answer different
questions.

Current finalized reports list every frozen catalog test, including tests that
did not run, and one hardware-graph integrity result. The HTML/PDF views show
each planned status and reason separately from the outcome. A direct-capture
report labels its limited collected scope and does not claim full quick-profile
coverage. Neither scope establishes an overall hardware pass.

Reports produced by the quick-session workflow also sign the stop reason,
worker execution states, and mount/network safety history: session preflight,
checks before started workers, final observations, and a fresh check before
signing. The periodic-monitor increment introduced a journal v3 summary while retaining
the same ten worker outcomes. It samples read-only mount and network metadata
once per second, allows one second per observation and at most 2,500 milliseconds
between observations. As in the qualified v4/v5 workflows, current v14/v15/v16
monitoring remains active through final inventory and the final gate.
Its first unsafe,
unknown, timed-out, or excessively delayed observation stops active work and
blocks signing; a later safe result cannot erase it. The monitor must join
within two seconds before signing. A clean operator stop can still be reported.

The monitor writes `session-safety-monitor.json` in the configured evidence
directory (`/run/hhpt` at boot). It records bounded timing, sample counts, cleanup
confirmation, and any first failure. On a signed session, the journal's monitor
summary matches this diagnostic file. Preserve it with the other session
evidence when investigating a stop; it cannot authorize signing by itself.
The sibling `session-thermal-monitor.json` contains the run-wide thermal
observations, coverage, findings and cleanup result; its signed summary must
also match the diagnostic file.

A session shorter than the first one-second tick honestly records no periodic
sample and still requires preflight and final snapshots. Periodic sampling
cannot detect every change between observations and is not continuous coverage.
Automated and native Linux suites, race checks, and signed CLI workflows passed;
rebuilt-image and physical qualification remain open. The extra shell checks
before session launch and after report creation remain separate operational gates. Direct
capture and direct finalization without a session journal retain their older
limited scope and do not claim this session history.

Reports using supported v4-v16 journals also show run-wide thermal coverage
and any observed limit.
The initial and final thermal readings are required even in a sub-second or all-off
session. `thermal_finding=limit_reached` means a valid global or local CPU/RAM
observation reached the limit; `no_limit_observed` means no such observation was
recorded. Read that finding together with `thermal_coverage=available`, `partial`
or `unavailable`. No readings, or incomplete readings, do not prove safe
temperatures. A valid local heat finding remains visible even when global
telemetry is unavailable, and earlier computation failures remain failures.

Both session monitors must stop with confirmed cleanup before signing. The final
signed journal determines `session_stop` and the readiness thermal fields;
hardware-graph integrity remains a separate result. The thermal monitoring
window includes execution, cleanup, final inventory and the final safety gate.
Report encoding, output and saving are outside it. Host and native software
qualification passed for the original v4 thermal increment; rebuilt images,
physical sensors and the beta pilot remain open.

| Status | Operator meaning | Required action |
| --- | --- | --- |
| `scheduled` / supported | The implemented check met its applicability rules and the plan selected it; operator-only checks do not require detected hardware | Check the final test result; this alone is not a pass |
| `pass` | The bounded implemented check completed without its defined failure | Confirm the check's stated scope before certifying the device |
| `fail` | The test observed a defined wrong result, unsafe condition, or changed run-integrity graph | Investigate, stabilize the setup, and retest; do not convert it to `inconclusive` |
| `not_present` | The relevant hardware was not detected | Confirm it is truly absent; if it should be present, check firmware, connection, and driver state |
| `unsupported` | This product version, image architecture, or selected profile does not implement the test | Use the modern image where possible or perform a documented external/manual test |
| `missing_driver` | Hardware was detected but no usable kernel/provider device appeared | Record as untested, try the current modern image, and investigate driver compatibility; absence of a failure is not a pass |
| `excluded` | The operator omitted this applicable quick test, or it is outside the selected profile | Treat it as untested; the signed outcome is `skip` with the exclusion reason |
| `inconclusive` | Evidence was insufficient or ambiguous, including software rendering or inaccessible/partial readiness | Repeat under known-good conditions or use a controlled external fixture |
| `aborted` | A safety/correctness stop or earlier prerequisite failure prevented the test | Resolve the earlier result and repeat the entire session |
| `error` | The collector, tool, timeout, or report workflow failed | Treat the test as incomplete; reboot into a freshly verified image and retry |
| `skip` | A requested optional action was not run, or the implemented check deliberately did not apply | Review its reason and complete a manual/other-profile check if required |

An `integrity=fail` result does not invalidate the signature. It means the
signed report is evidence that the hardware graph changed between planning and
finalization. Do not use that session as a clean sale/return baseline.

## Verify and Compare on Windows

Use the matching `hhpt.exe` on a trusted Windows workstation. Do not open the
HTML or PDF before verification when a report came from untrusted hardware or
media.

Verify a normal self-issued report offline:

```powershell
hhpt.exe verify -self-issued .\hhpt-SESSION.hhpcr
```

Success reads:
`verified: contents/signature valid; signer is self-issued and does not prove operator identity`.
The verifier checks the canonical outer bundle, manifest digests, signature,
expected files, path safety, and size limits. Present HTML and PDF views must
exactly match canonical rendering of signed inventory and signed software
version under the authenticated manifest schema/profile. Current ordinary v3 and linked v4 bundles
require `view_profile=hhsoft.pc-tester.memory-capacity-view/v1` and at least one
canonical HTML or PDF view. HTML-only and PDF-only bundles are supported;
mixed legacy/current views, custom or empty views, active content and views
from a different inventory fail even with a valid signature.

Historical v1/v2 bundles retain their optional-view and viewless rules. A failed
current profile cannot fall back to legacy verification. Use the matching
report utility; no previously released compatible version is implied.
Supported functional claims also pass shared schema and binding checks,
including in historical viewless bundles. This includes explicit v6 storage evidence read support;
accepted work stays distinct from diagnostics, and unconfirmed cleanup rejects
the report. The utility does not rerun hardware tests or reconstruct original
collection events. Current development commands emit Quick v14 with fourteen
workers, Standard v15 with fifteen, or Extended v16 with fifteen, without
sampled storage reads. Historical v4-v13 reports remain readable under their own rules. This offline
compatibility does not authorize an old plan for current execution; freeze a
new plan against the current catalog.

Compare two sessions made with the same deployment-key token:

```powershell
hhpt.exe compare -self-issued -output .\return-comparison.json `
  .\original.hhpcr .\returned.hhpcr
```

The comparison verifies both bundles first. Matching deployment-key epoch
labels are not enough: the exact HMAC key ID must match. Added, removed, and
same-location changed identities are useful swap indicators. Unidentifiable
parts, unavailable serials, different keys/epochs, and ambiguous topology
remain unresolved rather than becoming false matches. The JSON comparison is
not itself a trusted authorization decision.

Comparison captures each report and applicable public key through checked
regular-file acquisition on Linux and Windows. Metadata establishes eligibility
before a readable handle is opened; that handle must match the admitted object.
Both owners must close successfully before the captured bytes are verified.
Input or verification failures produce fixed diagnostics without provider or
payload details and prevent comparison output.

Global `-self-issued` verifies both embedded signers. In trusted mode, the
before key and key ID also apply to the after report unless both after-key
options are supplied; the default key path is acquired again for the after
report. `-self-issued=false` retains trusted-mode behavior. HMAC comparison
policy and existing output-error diagnostics are unchanged.

Cancellation is checked before input work and output dispatch, and again in
the destination callback. A callback that observes cancellation after the
output file has been opened returns failure without a comparison payload;
an empty new file may remain. This is not rollback or a deadline for a blocked
OS call. Host and selected native fixture evidence is recorded in the
[validation record](requirements-validation.md#comparison-input-ownership).

Inventory, plan, comparison, report capture/finalization and quick-test
evidence outputs refuse to overwrite an existing destination. A write,
short write or file-close failure makes output unsuccessful; a partial
file may remain. Choose a new filename after investigating the error.
Do not treat an existing file or finalizer banner alone as durable
storage: ordinary CLI output does not synchronize it. The separately
verified USB persistence flow has its own read-back and sync checks.
Uncertain worker cleanup still reports exit status 12 when output also
fails. Standard output remains owned by the calling process.

Provisioned signer verification syntax exists for future/trusted deployments:

```powershell
hhpt.exe verify -public-key .\signer.pub -key-id SIGNER_ID .\report.hhpcr
```

The current boot workflow does not enroll such an operator signer, so do not
use this mode unless your deployment has independently provisioned and
controlled the corresponding key pair.

## Create and Verify a Linked Retest

Linked retesting is opt-in. It creates a new report from the current run and
leaves the earlier bundle unchanged. The command-line options do not add a
new automatic boot-menu workflow. Current linked reports use manifest v4 with
the memory-capacity view profile; current ordinary reports use v3. Verification
requires explicit support for that schema/profile, without implying a released
minimum version. Historical ordinary v1 and linked v2 archive bytes, signatures
and optional-view behavior remain supported. Parent and child trust remain
independent. Image and physical-PC qualification remain separate.

Add `-previous-report PRIOR.hhpcr` to an otherwise valid `session` or
`finalize` command, together with exactly one independent parent trust mode:

- `-previous-self-issued` explicitly accepts the parent's embedded signer
  at reduced identity assurance.
- `-previous-public-key PRIOR.pub -previous-key-id PRIOR_KEY_ID` uses a
  separately provisioned raw 32-byte Ed25519 public key and expected key ID.

Current session/finalize commands still create self-issued children. Accepting
a provisioned parent key does not provision the child signer or increase the
child's identity assurance.

Parent trust options require a parent path. Partial or conflicting trust
options, including an explicitly false self-issued option, are rejected.
The parent is fully verified and its input handles are closed before linked
session work begins. Its outcomes, raw inventory and keys are not copied
into the new report. The current run still needs its own plan, worker
results, safety observations and successful cleanup before signing.

For standalone `finalize`, keep all required current `-outcome` arguments.
Omitting `-session-id` generates a new canonical UUIDv4. If supplied in linked
mode, the ID must be a nonempty lowercase UUIDv4 different from the verified
parent's ID. Session finalization generates that new ID automatically.
An explicit empty or invalid ID is not an instruction to reuse the parent.

Verify a self-issued child against one explicitly accepted self-issued parent:

```powershell
hhpt.exe verify -self-issued -previous-report .\prior.hhpcr `
  -previous-self-issued .\retest.hhpcr
```

The child and parent may instead use separate provisioned public keys. The
ordinary `-public-key` and `-key-id` options apply only to the child; the
`-previous-*` equivalents apply only to the parent. One report's trust does
not upgrade the other. A supplied parent is accepted only when both its
verified manifest digest and canonical bundle SHA-256 match the child's
signed reference, and the two report IDs differ.

The existing child-only command remains useful:

```powershell
hhpt.exe verify -self-issued .\retest.hhpcr
```

For a linked child, this authenticates the child and displays the signed
reference while reporting `predecessor relation: unchecked; no parent was
supplied`. It does not search for the parent. Supplying one parent checks
exactly that hop; even if the parent is also linked, no grandparent is
opened automatically. A matched reference proves neither the same asset,
chronology, supersession, a complete history nor inherited test results.
Comparison still needs the existing independently verified identity and
HMAC-key/epoch rules.

Report and public-key acquisition for `verify` and linked parent selection
is supported on Linux and Windows. Each report must be a regular file within
22 bytes to 64 MiB; each external public key must contain exactly 32 bytes.
Malformed or incomplete inputs, substituted readable objects and uncertain
closure do not authorize verification success. The readable object must remain
the exact acquired object; a Windows pathname rename alone does not invalidate
that proof. Other OS input adapters fail explicitly. These checks
do not establish deadlines for blocked filesystem calls. Remaining key inputs
in finalization, deployment/report-target discovery, persistence and save, and
persistence's live source handle, have not all acquired this checked-close
contract; their existing authorization and output rules remain in force.

## Update and Recovery

- **Update:** HHSoft does not download drivers, packages, profiles, or updates
  at runtime. Verify a newly released ISO and its checksum on a trusted
  workstation, write it to a fresh/re-imaged boot USB, and retain the previous
  known-good media until the new image passes local acceptance testing.
- **Failed boot:** Reconfirm architecture and firmware mode, keep Secure Boot
  off, try a different USB port/media, then try the serial/recovery or safe
  graphics boot entry if present in that image. Use `legacy-x86` only when the
  CPU lacks the modern baseline or no modern firmware path works.
- **Failed test/session:** Preserve any successfully saved `.hhpcr`, remove
  newly connected devices, cold-boot, and rerun from freshly verified media.
  Reboot is the runtime reset; there is no persistent appliance state to
  repair.
- **Failed save:** Keep power on. If unmounting succeeded, use the returned save
  menu to select another report USB and confirm it again. Existing partial files
  are preserved and cannot be overwritten. If unmounting failed, further save
  attempts stop. Accept only a save confirmed by read-back verification and
  successful unmounting.
- **Compromised media/key:** Stop using it. Re-image boot media from a verified
  release. Rotate a stolen deployment key to a genuinely new key and epoch;
  do not merely rename the old epoch. Treat reports signed or fingerprinted
  with compromised secrets according to their exposure window.

## Privacy and Security Warnings

- The report intentionally replaces many durable serial numbers and MAC-like
  identifiers with HMAC pseudonyms, but component models, capacities,
  topology, test evidence, timestamps, and report IDs may still be sensitive.
- A deployment token enables correlation across every report made with it.
  Restrict access, backups, retention, and sharing to the stated business
  purpose.
- Signatures protect saved bytes, not physical truth. Firmware, virtual
  machines, malicious peripherals, compromised collectors, and counterfeit
  hardware can lie.
- Safe quick mode is not authorization to erase, repair, flash firmware,
  change configuration, or inspect data stored on customer media.
- Do not enable networking to upload a report from the appliance. Move the
  verified bundle through controlled removable media, then use an approved
  external transfer process.
- Parse and view reports on a patched, restricted workstation. Verification
  accepts only the fixed script-free HTML and text-only PDF renderings, but
  untrusted files and filesystems remain parser risks. Verification does not
  open a view automatically.
- Removing the boot USB, power loss, reboot, or crash destroys unsaved RAM
  reports. Bundle integrity cannot prevent deletion; maintain independent
  backups.

## Current Limits

- Only the safe Quick profile is connected to automatic boot. The bounded
  Standard development path requires explicit selection; the complete Standard
  matrix, extended, destructive, PXE, server upload/share and authenticated
  managed-deployment workflows remain incomplete.
- Quick CPU and RAM tests are bounded samples, not sustained stress or complete
  memory qualification. Full-media memory testing remains a separate future
  boot path.
- Cancellation and deadlines rely on synchronous collectors cooperating;
  they cannot preempt a blocked kernel operation. The boot script's final KILL
  guard is 350 seconds for quick and 950 seconds for standard. These are supervisor
  policies, not universal cleanup deadlines. A nonzero session exit blocks
  `HHPT_READY` and save, even if report files already exist, and does
  not confirm that nested workers were cleaned up. Do not manually resume that
  workflow. Periodic mount/network sampling cannot establish continuous safety
  or stop an uninterruptible kernel call. Run-wide thermal sampling passed host
  and native software qualification; rebuilt-image and physical-sensor gates
  remain open. Heartbeat supervision and hang detection remain incomplete.
- Input readiness checks read-only event-device access. Guided pointer testing
  adds relative X/Y movement, primary/advertised right/middle button cycles and
  advertised wheel directions. Guided keyboard testing adds six fixed
  press/release controls on each keyboard. The bounded touchpad check adds
  single-contact absolute movement and a primary-button cycle. These checks do
  not prove every key, wheel precision, extra button, touch geometry or gesture.
  Direct capture retains the readiness subset and omits all three guided input
  checks. The integrated touchpad route still needs Linux/native, real-device,
  installed/image, physical and pilot qualification.
- Storage testing is readiness only: no SMART/NVMe health qualification,
  sampled sector verification, throughput, optical-media read, or complete
  media scan.
- USB testing is topology/access readiness only: no data, power-delivery,
  per-port, alternate-mode, or hot-plug qualification with a known-good
  fixture.
- Network testing is hardware/driver/link readiness only: no DHCP, Wi-Fi
  association, modem session, DNS, Internet, integrity, or throughput test.
- Modern graphics uses open kernel/Mesa paths including `i915`, `amdgpu`, and
  `nouveau` where supported. It does not qualify proprietary-driver-only
  NVIDIA/AMD functionality, VRAM, video engines, sustained stability, or all
  native-resolution/scaling behavior. Legacy graphics/display tests are
  unsupported.
- Audio currently verifies ALSA device-node readiness only; tones, routes,
  volume, channels, jack behavior, and capture remain unqualified. Camera,
  full keyboard/mouse/touchpad/touchscreen interaction, card readers as media,
  rotation sensors, modems, storage functional/health, full-network, and full
  USB-port tests are not yet implemented end-to-end.
- Secure Boot boot-image support is not implemented. AArch64 remains beta and real Snapdragon/X
  Elite systems are not yet qualified.
- Self-issued reports prove byte integrity only. Trusted report signers,
  custody timestamps, TPM/HSM key protection, revocation, append-only database
  receipts, and policy-signed sale/return decisions remain future work.
- Part identity depends on identifiers exposed by firmware and devices.
  Missing, duplicate, privacy-randomized, or forged identifiers limit swap
  detection; fingerprints do not prove ownership, warranty, authenticity, or
  that a replacement was unauthorized.

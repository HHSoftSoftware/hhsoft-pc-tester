# Bounded touchscreen check

This development check asks for movement and release from a supported
single-contact touchscreen interface. Generic touch activity, candidate
discovery and read-only input readiness cannot establish a functional pass.
The existing touchpad check is unchanged.

## Supported interface

The initial Linux interface supports one contact through `BTN_TOUCH` and
absolute `ABS_X`/`ABS_Y` coordinates, with the `DIRECT` property. Its advertised
capabilities must match that narrow contract. Interfaces that also advertise
multi-touch axes, stylus/tool controls, pressure, relative movement, other axes
or pointer properties remain unsupported by this first check. A screen can be
listed in inventory without being supported for a functional result.

Multi-touch, palm rejection, gestures, rotation mapping, calibration,
coordinate accuracy and complete panel/edge coverage are not tested. Moving a
finger through the requested directions does not prove every panel location
works. Unsupported or incomplete coverage is not a hardware-failure verdict.

## Operator actions

1. Lift every finger before the observation begins.
2. Touch with one finger and keep the same contact while moving left and right,
   then up and down. Repeat motions if needed.
3. Lift the finger to finish that contact. Each supported interface must supply
   its own evidence; one screen cannot complete another screen's actions.

The check waits for fresh coordinates after contact begins. It requires both
movement directions on both axes within one released contact, using finite
thresholds based on the advertised axis ranges and noise parameters. Motion
from a still-held contact cannot complete the result. Lost records, changing
identity, queued terminal events or unconfirmed cleanup prevent a pass.

The menu uses **Touchscreen movement and release**. The development command is:

```sh
hhpt touchscreen -duration 45s -timeout 120s -output touchscreen.json
```

The default observation window is 45 seconds within a 120-second command limit
and the remaining session budget. Cancellation requests a stop; these limits
are not deadlines that can preempt a blocked kernel operation. Exit 0 means a
validated diagnostic was written, not that its status is pass. Cleanup
uncertainty retains exit 12 and prevents session signing. Functional touchscreen evidence
contains bounded movement/contact summaries and excludes device `name` and
`model` labels, raw event traces and coordinate samples.

Touchscreen-only inventory may retain nonempty `name` and `model` labels.
Each label must be valid UTF-8, at most 256 UTF-8 bytes and free of Unicode
control characters. A label is omitted if it contains the nonempty known
`unique_id` bytes anywhere; the identifier itself is never exported. Touchpad
and shared touchpad/touchscreen candidates retain their stricter label
suppression. These checks preserve useful display labels and suppress known
identifiers; they are not a general detector of personal information.

## Connected development route

The current route inserts touchscreen once after touchpad, using Quick v11
with fourteen workers and Standard v12 with fifteen. Quick selection may
exclude it; Standard rejects nonempty Quick exclusions. The catalog remains
22 rows, with fifteen implemented and seven inactive.
Modern Quick selection has fourteen choices and legacy x86 has twelve.

Execution requires a newly decoded schema-3 plan matching the current catalog;
rehashing an obsolete plan does not authorize it. Historical v4-v10 journals
remain readable under their own rules. Signed-report verification checks the
claimed evidence and binding; it does not repeat the hardware observation.
Direct capture keeps its readiness subset and omits this guided check.

## Validation status

Earlier producer and discovery qualification passed 2,250 test/subtest outcomes
with one existing platform skip on each focused Windows architecture. Six
Linux test builds covered the input and inventory packages on amd64, 386 and
arm64; those artifacts were not executed.

The complete touchscreen command/session, owned finalization, signed-consumer
and view route has passed Windows software qualification. Focused amd64 and
386 runs each passed 8,369 test/subtest outcomes with seven platform skips.
The full suite passed 11,238 with eighteen skips, across 33 passing packages
and one package without host tests. All 794 new named checks passed in all three
runs; all 2,603 focused and 3,453 full-suite selected names passed.

The 19-command qualification also passed formatting, vet and twelve Linux
builds: three application builds and nine test builds for the command, input
and inventory packages across amd64, 386 and arm64. At that host-only stage,
those Linux artifacts had not been executed.

Selected native fixtures subsequently passed on Linux amd64 and 386 for the
command, input and session-evidence packages: 2,952 named outcomes across six
package/architecture runs and 344 root executions, with no skips or failures.
Nine ownership cases, 32 archive cases and all six postchecks passed, with
cleanup confirmed. These are bounded software fixtures, not observations of
real touchscreen devices or an installed command.

Real evdev/device behavior, installed-command checks, ARM runtime, race checks,
rebuilt images, physical touchscreens, all four bootable images, the ten-PC
pilot and release approval remain open. Broader
keyboard/mouse/touchpad/touchscreen coverage stays on the
[project checklist](../PROJECT_CHECKLIST.md).

See the [operator guide](operator-guide.md#bounded-touchscreen-check) and
[development roadmap](roadmap.md#bounded-single-contact-touchscreen-development).

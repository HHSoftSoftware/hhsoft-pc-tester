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

The current network readiness adapter checks driver attachment and link state
without configuring an address, associating Wi-Fi, or generating traffic. The
safe profile fails closed when any non-loopback interface is administratively
active, including software-created interfaces not treated as hardware evidence.

The current USB readiness adapter records authorized kernel topology, class,
speed, and direct USBFS character-node access. It opens nodes read-only without
issuing any USB transfer, reset, or driver operation. It does not identify
external ports or qualify their data integrity, power, orientation, or modes.

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

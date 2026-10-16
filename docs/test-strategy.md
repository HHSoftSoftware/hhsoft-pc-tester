# Test Strategy

This document defines how HHSoft PC Tester selects, runs, and evaluates
hardware tests. A run must preserve user data by default, expose missing
coverage, and produce evidence sufficient to audit the result.

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

The modern Alpine images target most systems from the Windows 8 era onward and
offer the complete applicable suite. The BIOS/CSM-only Buildroot legacy image
targets Pentium MMX-class through older x64 PCs. It shares report semantics but
exposes a visibly reduced test catalog; unavailable driver-dependent tests are
`skip/unsupported`, never pass.

Profiles are versioned declarative manifests. The plan records the exact
manifest, thresholds, fixtures, selections, and exclusions before execution.
Operators may deselect a test, but it remains `skip` in the report. A custom
profile inherits one below and discloses every override.

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
connected card within the current 20-second tool timeout. `hhpt display` uses
the locked libdrm `modetest` worker to apply its preferred-mode pattern to all
connected outputs on each card. One compact seven-criterion P/F/I finding per
connector is recorded, with invalid or absent operator input classified as
inconclusive rather than guessed.

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

The current storage readiness adapter rejects writable block mounts and checks
that each supported media-bearing kernel block interface remains a direct block
node when opened read-only. It deliberately reads no sector data and therefore
cannot claim health, read correctness, throughput, or surface coverage.

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
| Input | Detect every keyboard key in a layout-aware map, including modifiers, function keys, and selected rollover combinations. | OA / R/O | Q S E B D | 2 min / 5 min | Press/release map, missing/stuck keys. |
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

Any required `fail` makes the run fail. In the absence of a fail, a required
`error` makes the run an error. A run passes only when all required applicable
cases pass; optional skips remain visible and reduce coverage. A run with no
applicable cases for its declared purpose is skipped, never passed. Minimum
coverage rules cannot convert `skip`, `fail`, or `error` into `pass`.

## Timeouts and recovery

- Unless the matrix says otherwise, adapter startup times out after 30 seconds
  and an operator prompt after 120 seconds.
- Automated workers heartbeat every 15 seconds. Two missed heartbeats trigger
  graceful cancellation; failure to stop after 15 more seconds triggers worker
  termination and target isolation.
- Timeout is `error` when completion was needed for diagnosis. It is `fail` only
  when response time is itself the criterion and target evidence is conclusive.
- Retries must be declared before execution. All attempts remain in evidence;
  recovery may mark a defect intermittent but cannot hide it.
- Hang, reset, disconnect, critical thermal state, uncorrected error, or unsafe
  condition stops affected tests. Resume requires an explicit safe state.
- At the run deadline, active work is cancelled as `error/run-deadline` while
  completed statuses remain unchanged.

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

## Hardware and environment limitations

- The live OS cannot test memory occupied by itself; Memtest86+ supplies the
  pre-boot phase, but cannot prove indefinite fault-free operation.
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

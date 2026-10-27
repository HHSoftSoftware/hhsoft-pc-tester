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

The guided-pointer increment adds relative movement and primary/advertised
right/middle button cycles per interface. It expanded quick selection to ten
modern and eight legacy choices and introduced ten outcomes in session journal v2.
Absolute or mixed interfaces and the rest of the input matrix remain outside
this bounded check. The catalog change requires fresh plans while preserving
historical signed-report verification. Portable and native Linux tests, race
checks, cross-builds, and signed CLI workflows pass for this increment.
Rebuilt-image and physical qualification remain open; earlier image results do
not qualify the new worker.

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

The next thermal prerequisite is implemented in source: local CPU/RAM guards
bound the initial and periodic observations, join before returning results, and
record truthful coverage in CPU/memory v2 evidence. Unconfirmed cleanup blocks
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
as definition version 2 and uses an eleven-worker v5 journal while preserving
strict v4 verification. Windows tests, vet and cross-builds, native Linux
amd64/386 suites, race checks and command-line workflows passed. Rebuilding and
qualifying all four beta images and testing physical keyboards remain open.
Layout-aware coverage, other keys, rollover and ghosting remain planned. The
ten-PC beta pilot and the initial alpha's separate release gates remain open;
this increment does not expand the frozen alpha candidate's scope.

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

## Phase 3 — complete profiles

- Add Memtest86+ boot integration, standard, extended, and burn-in profiles,
  calibrated storage self-tests, sensors, battery, optical media, card reader,
  modem, and optional remote submission.
- Complete the remaining matrix coverage for SPD/ECC, Bluetooth, power and
  cooling, firmware-security reporting, expansion/dock fixtures, clock drift,
  and operator physical inspection.
- Add controlled temporary-file writes and a physically separate destructive
  image for wipe and full write qualification.
- Implement the adapter package/protocol/runtime contract, including burn-in
  support, and validate third-party adapter conformance.
- Add linked signed report revisions and separately consented encrypted
  restricted-evidence export with the required privacy controls.
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

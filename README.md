# HHSoft PC Tester

HHSoft PC Tester is a bootable, open-source PC inspection and qualification
appliance. It is intended to replace a collection of disconnected inventory,
stress-test, and manual verification tools with one auditable workflow.

Current version: `0.1.0-alpha.1` (Phase 1: bootable inventory, report comparison,
and bounded CPU, memory, graphics, input, storage, network, and USB-readiness
alpha).
Version tags and release artifacts follow Semantic Versioning 2.0.0.

Progress against the complete original requirements is tracked in
`PROJECT_CHECKLIST.md`; `docs/roadmap.md` explains implementation order.
The first-push checks and GitHub download workflow are documented in
[`docs/publication-readiness.md`](docs/publication-readiness.md). Downloads
remain gated on the complete reviewed bootable ISO set.

## Design goals

- Boot from USB without modifying installed operating systems or user data.
- Support legacy BIOS and UEFI on 32-bit and 64-bit x86 PCs, including a
  BIOS/CSM fallback that boots useful inventory on pre-SSE2 hardware. Track
  AArch64 as a separate beta modern UEFI image.
- Detect present hardware and build an applicable test plan automatically.
- Offer quick, standard, extended, and explicitly destructive test profiles.
- Combine automated checks with operator-assisted tests for devices that
  cannot be verified by software alone.
- Preserve evidence and generate offline HTML and static PDF reports, with
  remote submission as an optional output.
- Detect component swaps with privacy-preserving system and per-component
  fingerprints rather than exposing serial numbers by default.
- Build images from reviewed dependencies with pinned versions, checksums,
  and license records.
- Apply defense in depth at the boot chain, daemon, test-worker, network,
  storage, report, build, and release layers; no single control is trusted.

## Architecture direction

The initial platform is a pinned Alpine Linux stable branch running in
diskless mode. The Go coordinator inventories hardware, constructs and runs the
quick test plan, enforces storage-write policy, and creates reports. Separate
UI/service processes and isolated workers are planned. Current quick workers
run in process with bounded execution and signed outcomes.

Complete memory qualification is planned through a separate Memtest86+ boot entry
because an operating system cannot test memory allocated to itself. Buildroot
is used only for a reduced-feature legacy fallback.

Legacy BIOS and plain UEFI are baseline paths. CSM is needed only to use the
legacy path on UEFI firmware. Secure Boot is optional and deferred until the
functional release is complete. Alpine upgrades are centralized in one
reviewed base lock rather than being embedded throughout build scripts.
Clean apk-tools v3 roots are initialized by the wrapper instead of relying on
stale work caches.

See `docs/operator-guide.md`, `docs/troubleshooting.md`,
`docs/architecture.md`, `docs/secure-boot.md`, `docs/test-strategy.md`,
`docs/test-adapter-api.md`, `docs/report-integrity.md`, and
`docs/deployment-keys.md` for current operating guidance and contracts.

## Current development slice

`hhpt inventory` performs read-only Linux discovery, removes stable hardware
identifiers from exported attributes, and emits deployment-keyed component and
system HMACs. It requires an explicit 32-byte fingerprint key and creates a new
output file without overwriting an earlier record.

```sh
dd if=/dev/urandom of=fingerprint.key bs=32 count=1 status=none
hhpt inventory -fingerprint-key fingerprint.key -key-epoch local-1 -output inventory.json
```

For cross-session return comparisons, provision a separate removable token
with the fixed `hhpt-deployment-key.v1` file, interrupt automatic boot, and
select `K`. HHSoft mounts the explicitly selected token read-only and keeps it
mounted so it cannot also become a report destination. Automatic and legacy
boots continue to use unlinkable per-session keys.

```powershell
hhpt.exe deployment-key init -epoch shop-west-2026-1 `
  -output E:\hhpt-deployment-key.v1
hhpt.exe deployment-key inspect E:\hhpt-deployment-key.v1
```

The token contains a plaintext pseudonymization key, not a signing key. Keep it
offline and physically controlled; see `docs/deployment-keys.md` before use.

`hhpt plan` resolves embedded declarative test definitions against a fresh
hardware graph. It freezes scheduled, excluded, not-present, unsupported, and
missing-driver states under a deterministic plan ID. The graph binding is
deployment-keyed rather than a portable tracking hash. Finalization rechecks
the component graph, binds every test record to that plan, and signs an
added/removed/changed run-integrity result.

```sh
hhpt plan -fingerprint-key fingerprint.key -key-epoch local-1 -profile quick -output plan.json
```

Phase 1 also includes the first active graphics probe:

```sh
hhpt graphics -pattern-frames 120 -output graphics.json
```

It enumerates DRM cards, bound drivers, render nodes, connector state, and
advertised modes; asks Mesa/GBM for the active renderer; and, when requested,
runs a short KMS pattern on the first connected card. `hhpt display` adds a
preferred-mode pattern on every connected output of each DRM card, followed by
one compact P/F/I finding code per display for pixels, geometry, color,
flicker, scaling, overscan, and artifacts. Those findings enter the signed
normal-session report. Software rendering such
as llvmpipe is reported as inconclusive rather than accelerated. This is not
yet full graphics qualification: it does not independently measure pixel
quality or prove prior-mode restoration, VRAM, video engines, sustained
stability, or proprietary-driver-only functionality. `hhpt capture` runs the probe and
folds its fresh evidence into the signed payload and HTML/PDF report views. Automatic
safe mode finalizes the already-collected evidence into one RAM-only `.hhpcr`
without rerunning active tests, then opens the report-save menu. Its ephemeral
public key is covered inside the bundle: this detects changes to the saved copy
but deliberately does not claim an enrolled operator or deployment identity.

The safe quick profile also runs bounded CPU known-answer calculations and
RAM byte patterns and inversions. Both have short deadlines, independently
sample exposed thermal-zone and hwmon inputs, and record safety coverage in JSON
and signed reports. The strengthened v2 guard bounds readings and observer
cleanup; unconfirmed cleanup stops later work and signing. Partial or unavailable
telemetry remains explicit and does not establish safe temperatures. This local
guard passed Windows tests, vet, cross-builds, native Linux suites, race checks,
and command-line workflows. Rebuilt-image and physical-sensor qualification
remain open.

The CPU worker in development source also checks twelve fixed binary64
addition, subtraction, multiplication and division results on every worker.
CPU v3 evidence records completed checks and exact mismatch bits; integer, SHA-256
and scalar failures remain distinct. Focused regression checks cover the signed
report path and historical CPU v2 compatibility. Full host and native Linux
validation passed, including race tests and signed CLI cases. Rebuilt images
and physical CPU checks for this increment remain open. These
bounded checks are not a benchmark or comprehensive instruction test; legacy
x86 soft-float exercises software arithmetic rather than the hardware FPU.

The development standard profile now adds an explicit bounded CPU stress step.
Select `session -profile standard` with a matching frozen standard plan; the
normal boot menu and default session remain quick. Standard defaults to a
900-second session budget and has thirteen workers, with stress immediately
after a passing CPU quick check. The existing quick v7 path retains twelve
workers. The catalog still contains 22 rows; nine families remain inactive.

The stress worker requests a shared 600-second elapsed load window after every
worker has completed its initial checked arithmetic. Its 660-second local
watchdog starts at invocation, including preparation. It requires available
accepted temperature observations before load and stops if that coverage is
lost. A pass also requires further checked work from every worker; elapsed time
alone is insufficient. Counters do not measure CPU time, utilization, core
coverage, performance or physical CPU health.

The complete source passed the host suite: 6,732 test/subtest passes across
30 packages, with 17 test skips and one package skip. All 493 required named
outcomes across 69 selected roots passed. Selected native tests passed on Linux
amd64 and 386: 6,040 test/subtest outcomes across 18 package executions, covering
517 roots and 3,020 names per architecture, with no failures or skips.

A separate API-level qualification completed a real shared 600-second load
window on Linux amd64 and 386, using two workers and fixed synthetic temperature
observations of 35 degrees C. Both runs passed with cleanup confirmed. This
establishes elapsed-window completion and checked worker participation within
that fixture. CPU time, utilization, all-core saturation and physical thermal
behavior remain unqualified.
See [the standard-profile contract](docs/requirements-validation.md#bounded-standard-cpu-stress)
for finite outcomes, strict signing and the remaining validation gates.

The RAM worker follows its three original patterns with `0xaa` initialization,
ascending verification and inversion to `0x55`, descending verification and
inversion back to `0xaa`, and final ascending verification. Memory v3 distinguishes
completed work, interrupted work and retained byte mismatches. Full Windows
tests, vet, cross-builds, native Linux suites, race tests and signed CLI
workflows passed.
These checks cover an owned userspace working set, not all installed RAM or every
hardware fault. Rebuilt-image and physical-memory validation remain open.

Development source now also monitors temperatures throughout the quick session,
including excluded-test runs, cleanup and final inventory. Journal v4 records
initial, periodic and final observations and requires both session monitors to
join before signing. Heat stops work without erasing earlier failures; missing
telemetry remains a coverage limitation. Windows tests, vet, cross-builds,
native Linux amd64/386 suites, race checks and all 15 CLI scenarios passed.
The VM session scenarios reported unavailable temperature telemetry honestly;
rebuilt-image and physical-sensor qualification remain open. See
[requirements validation](docs/requirements-validation.md).

```sh
hhpt cpu -duration 3s -timeout 20s -output cpu.json
hhpt memory -passes 2 -timeout 30s -output memory.json
hhpt display -pattern-duration 5s -finding-timeout 15s -output display.json
```

`hhpt input -duration 0` checks that every present keyboard, mouse, touchpad,
or touchscreen event device can be opened read-only. An operator can add a
bounded activity window, for example `-duration 30s`; evidence retains only
aggregate event counts and never key codes or typed text. Readiness is automatic;
it does not establish complete keyboard, pointer, or touch coverage.
`hhpt capture -input-duration 30s -input-categories keyboard,pointer ...`
includes the bounded aggregate activity evidence in the signed report.

The guided `hhpt pointer` check observes relative `EV_REL` X/Y interfaces.
Move each supported pointer left, right, up, and down, then press and release
its primary button and every advertised right or middle button. Scroll every
advertised wheel axis in both directions. Each interface must supply its own
observations. Absolute `EV_ABS` and mixed relative/absolute interfaces cannot
pass this check. Extra buttons, touch gestures, tracking accuracy and wheel
resolution remain separate coverage. Evidence keeps
bounded counts and action summaries, never raw input events or typed text.

```sh
hhpt pointer -duration 30s -timeout 90s -output pointer.json
```

Pointer joins the quick session's shared five-minute budget and signed results;
direct `capture` retains its existing subset and does not run this guided check.
The selection menu offers twelve modern or ten legacy quick tests. Pointer
definition 2 and evidence v2 add advertised wheel directions and require a fresh
plan. That pointer increment retained session v5 with eleven workers. Historical
pointer v1 retains its original
movement/button scope and rendering. Windows tests, native Linux amd64/386
suites, race checks, and standalone and signed-session CLI checks passed for the
wheel increment. VM observations remained incomplete without supplied actions;
rebuilt images and physical qualification remain open.
See [requirements validation](docs/requirements-validation.md).

The guided `hhpt keyboard` check asks for a press and release of left Shift,
right Shift, Up, Down, Left and Right on each keyboard. Only these six controls
are checked; do not type text. Missing controls, incomplete actions, changed
devices and lost event records cannot pass. Evidence retains fixed completion
booleans, without typed text, raw events or the order of the operator's actions.

```sh
hhpt keyboard -duration 30s -timeout 90s -output keyboard.json
```

Keyboard definition version 2 requires fresh plans. Its v5 increment introduced
eleven workers and retained v4's thermal and safety boundaries, with 21 catalog
rows plus run integrity. Existing v4 reports continue through strict validation;
direct capture omits both guided input workers.
Windows tests, vet and cross-builds, native Linux amd64/386 suites, race checks
and command-line workflows passed. Rebuilt images, physical keyboards and full
layout/rollover validation remain open. See
[requirements validation](docs/requirements-validation.md).

The exterior-inspection check records an operator's finite observation of safely
visible enclosure surfaces, without moving or opening the device. It does not
verify physical condition or imply coverage of unseen surfaces. Current v7
sessions append this check after the existing eleven workers; finalized reports
contain 22 catalog results plus run integrity. Direct capture retains its
seven-check subset and does not acquire this observation.

The complete implementation passed the host suite, formatting, vet and three
Linux application cross-builds. The selected native Linux amd64/386 fixture
suites also passed, with no failures or skips and confirmed cleanup. See the
[operator instructions](docs/operator-guide.md#observe-the-visible-exterior)
and [validation scope](docs/requirements-validation.md#bounded-exterior-inspection).
Broader inspection, rebuilt images, physical testing and release approval remain
separate gates.

The Linux power-supply inventory implementation reports eleven finite fields
with explicit value states. It enumerates every reported supply without assuming
a particular battery name. Public rows use nonpersistent `power_supply:1`,
`power_supply:2`, and subsequent ordinals; raw kernel names and serials are not
exported, and these rows do not establish component identity. No rows means no
reported supply data, not proof that a battery or adapter is absent.

These are separate driver observations, not battery-health or AC/battery
transition tests. The feature adds no worker or catalog row and leaves current
v7 sessions and historical report contracts unchanged. The full host suite and
selected native Linux amd64/386 fixtures passed; see the [operator interpretation](docs/operator-guide.md#read-power-supply-inventory)
and [validation contract](docs/requirements-validation.md#power-supply-inventory).
Physical testing, the ten-PC pilot, rebuilt images and release approval remain
open.

The periodic-monitor milestone introduced read-only mount/network sampling
and a v3 session journal, without changing the catalog or ten worker outcomes.
The v4 increment retained that monitor summary and added thermal evidence;
the current v7 workflow keeps those same monitoring requirements.
Its first unsafe or uncertain observation stops active work and blocks signing.
This is bounded periodic coverage, not continuous safety monitoring or kernel
preemption. Automated tests, native Linux suites, race checks, and signed CLI
workflows passed; rebuilt-image and physical qualification remain open.

`hhpt audio` verifies that ALSA card, control, playback, and capture nodes match
their confined sysfs metadata. It never opens a media stream or plays/records
customer audio. Safe quick runs preserve this readiness evidence, while tone,
channel, jack, volume, and microphone-loopback tests remain operator work.

`hhpt storage` checks the mount registry, then opens supported media-bearing
block nodes read-only without issuing a sector read. It rejects symlinks and
non-block substitutions, records inaccessible or unusable devices, and makes
any writable block mount visible as an error. This is readiness evidence, not
SMART health, sampled-read correctness, throughput, or full-media coverage;
those remain planned workers. The signed capture includes this
readiness result.

Offline report reading additionally supports the finite storage sampling evidence
and explicit v6 session format through verification, HTML/PDF views and
comparison. This checks signed assertions and separates accepted work from
diagnostics; it does not perform media reads. Current quick sessions use v7 with twelve workers; explicit standard sessions
use v8 with thirteen. The sampling catalog entry remains inactive. See
[report integrity](docs/report-integrity.md) for the evidence and cleanup limits.

`hhpt network` inventories only hardware-backed Ethernet and Wi-Fi interfaces,
checks driver binding, records link state without retaining MAC addresses, and
generates no traffic. Safe mode fails closed if any non-loopback interface,
including a software tunnel, is administratively active. `-allow-active` is an
explicit diagnostic override that still does not configure or transmit. Full
addressing, association, integrity, and throughput tests require later opt-in
network fixtures.

`hhpt usb` records USB controller roots and device topology, authorization,
speed, class, and kernel device-node readiness. It opens direct USBFS character
nodes read-only but issues no control, bulk, interrupt, isochronous, reset, or
other USB transfer. It therefore proves discovery and access readiness only;
external-port mapping and data/power/alternate-mode qualification require a
known-good operator fixture in a later profile.

Persistent reports use an explicit, fail-closed two-step flow. `hhpt
report-target` lists only unmounted, writable, removable USB partitions that
have a stable deployment-keyed identity and are not the boot or internal media.
Repeating it with the displayed code creates a deployment-HMAC authorization
bound to the exact media, topology, size, fingerprint key, current kernel boot,
and a 15-minute monotonic expiry; it does not mount or write the device.
The normal post-run `hhpt save` screen performs discovery, typed authorization,
fixed-policy mount, verified copy, read-back, and unmount without exposing a
shell. It never chooses a destination automatically.

```sh
hhpt report-target -fingerprint-key /run/hhpt/fingerprint.key
hhpt report-target -fingerprint-key /run/hhpt/fingerprint.key \
  -device /dev/sdb1 -confirm RPT-ABC123 \
  -output /run/hhpt/report-target.json
mount -t vfat -o rw,nodev,nosuid,noexec /dev/sdb1 /run/hhpt/report-target
hhpt persist -authorization /run/hhpt/report-target.json \
  -fingerprint-key /run/hhpt/fingerprint.key -public-key signer.pub \
  -key-id shop-1 /run/hhpt/session.hhpcr
```

`hhpt persist` independently verifies the signed bundle, rechecks hot-plug and
mount state, binds the copy to the whole-file digest just verified, and creates
the report exclusively with file and directory sync plus byte-exact read-back.
FAT32, exFAT, and ext4 are the initial allowed filesystems. Failed or changed
targets do not produce a success receipt, and existing reports are never
overwritten. Verification also requires the complete outer ZIP to match the
canonical writer byte-for-byte, so prefixed, appended, or alternate encodings
cannot hide data outside the signed manifest.

On another PC, a self-issued session remains verifiable without a sidecar key:

```sh
hhpt verify -self-issued hhpt-SESSION.hhpcr
```

Reports produced by `hhpt session` and `hhpt finalize` are canonical ZIP
bundles containing standalone `views/report.html` and script-free
`views/report.pdf` files. Neither view is opened automatically, and the signed
`.hhpcr` remains the authoritative copy. Accepted historical and linked bundles
can omit rendered views. See the [linked retest operator guidance](docs/operator-guide.md#create-and-verify-a-linked-retest)
for the opt-in flags, independent trust choices and verifier compatibility.

This checks canonical bundle bytes, all manifest digests, and the signature,
while reporting that signer identity is untrusted. Deployment-provisioned
reports continue to require `-public-key` and `-key-id`.

`hhpt compare` independently verifies two signed bundles before matching their
deployment-keyed component identities. It reports a changed identity at the
same observed location as a replacement, unmatched identities as additions or
removals, and missing identities or key-epoch changes as unresolved instead of
claiming certainty. Matching epoch labels are insufficient: reports also carry
a non-secret HMAC key identifier derived from the exact fingerprint key.

```sh
hhpt compare -self-issued \
  -output return-comparison.json original.hhpcr returned.hhpcr
```

Normal reports remain self-issued, so this comparison establishes intact report
bytes but not operator identity. Trusted report signing remains a separate key
and release milestone.

The local comparison references both signed manifest digests. A future service
will add signed policy decisions, key-epoch bridging, and append-only receipts.

`scripts/build/build-go.sh x86_64` runs tests and emits a statically linked
Linux binary using the repository `VERSION`. Use `x86`, `legacy-x86`, or
`aarch64` for the other boot-image architectures. `legacy-x86` uses software
floating point and is reserved for the minimal BIOS/CSM fallback. These are
development binaries, not bootable release images.

Every build first runs the custom source scanner. CI scans full Git
history too, including private identity terms, captured authentication codes,
common credential forms, private-key headers, local paths/endpoints, and
forbidden generated artifacts.
Agent instructions and local agent state are also excluded. Before committing
or pushing, `python scripts/security/publication_check.py --gitleaks PATH`
checks candidate files, staged bytes, and history with both scanners. Build and
publication scripts require Python 3.11 or newer; CI uses the Go toolchain in
`.go-version`. The optional [offline package cache](docs/package-cache.md) is
mandatory for non-development Alpine images.

`scripts/build/build-go.sh windows-x86_64` cross-builds `hhpt.exe` as a
host-side report utility. It supports offline `verify` and `compare` workflows
without booting the tested PC or exposing a signing/fingerprint key. Capture
and hardware probes remain Linux boot-environment operations.

On an Alpine/QEMU builder with the split AArch64 and virtio-GPU packages,
`sh scripts/lab/test-aarch64-qemu.sh IMAGE.iso` performs the offline UEFI
smoke test with a new file-backed scratch disk.

## Safety model

The live environment must not auto-mount writable filesystems or activate
foreign LVM or RAID volumes. Inventory and normal tests are read-only. Any
test that writes to a target requires a fresh typed confirmation bound to the
stable identity, path, size, and model of that target. The boot medium and
report destination are ineligible destructive-test targets.

Physical-disk passthrough is not used in developer VMs. Local testing uses
sparse virtual disks that are safe to discard.

The current graphics probe invokes only fixed image tools with validated frame
counts, a fixed working environment, a 20-second subprocess timeout, and 64 KiB
of captured output. It neither enables networking nor mounts host or internal
filesystems.

## Repository plan

- `cmd/`: Go command entry points.
- `internal/`: orchestrator, inventory, policy, and reporting packages.
- `platform/alpine/`: pinned image profiles and package locks.
- `profiles/`: declarative test profiles with reviewed adapters.
- `schema/`: stable JSON schemas for reports and evidence.
- `ui/`: local operator interface.
- `site/`: GitHub Pages documentation and report viewer.
- `scripts/lab/`: local VirtualBox and QEMU automation.
- `docs/`: architecture decisions, contributor documentation, and operations.

Directories are created only when their first working artifact is added.

## License and contributions

Original work is licensed under the BSD 3-Clause License. It requires HHSoft
provenance to remain in redistributions and prohibits using HHSoftSoftware or
contributor names to endorse derivatives. Third-party programs included in a
boot image retain their own licenses and notices.

Contributions use Developer Certificate of Origin sign-off. See
`CONTRIBUTING.md` and `SECURITY.md`.

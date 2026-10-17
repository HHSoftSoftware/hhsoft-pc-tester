# HHSoft PC Tester

HHSoft PC Tester is a bootable, open-source PC inspection and qualification
appliance. It is intended to replace a collection of disconnected inventory,
stress-test, and manual verification tools with one auditable workflow.

Current version: `0.1.0-dev.0` (Phase 1: bootable inventory, report comparison,
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
diskless mode. A small Go service inventories hardware, constructs and runs the
test plan, enforces storage-write policy, and creates reports. A local UI talks
only to that service. Long-running and interactive tests remain isolated
workers with timeouts and bounded logs.

Complete memory qualification is a separate open-source Memtest86+ boot entry
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
RAM data/address patterns. Both have configurable short deadlines, independently
sample exposed thermal zones, and record safety coverage in JSON and signed
reports.

```sh
hhpt cpu -duration 3s -timeout 20s -output cpu.json
hhpt memory -passes 2 -timeout 30s -output memory.json
hhpt display -pattern-duration 5s -finding-timeout 15s -output display.json
```

`hhpt input -duration 0` checks that every present keyboard, mouse, touchpad,
or touchscreen event device can be opened read-only. An operator can add a
bounded activity window, for example `-duration 30s`; evidence retains only
aggregate event counts and never key codes or typed text. Readiness is automatic,
but full key/button coverage and touch geometry remain operator test work.
`hhpt capture -input-duration 30s -input-categories keyboard,pointer ...`
includes the bounded aggregate activity evidence in the signed report.

`hhpt audio` verifies that ALSA card, control, playback, and capture nodes match
their confined sysfs metadata. It never opens a media stream or plays/records
customer audio. Safe quick runs preserve this readiness evidence, while tone,
channel, jack, volume, and microphone-loopback tests remain operator work.

`hhpt storage` checks the mount registry, then opens supported media-bearing
block nodes read-only without issuing a sector read. It rejects symlinks and
non-block substitutions, records inaccessible or unusable devices, and makes
any writable block mount visible as an error. This is readiness evidence, not
SMART health, sampled-read correctness, throughput, or full-media coverage;
those remain separate contained workers. The signed capture includes this
readiness result.

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

Every verified `.hhpcr` is a canonical ZIP bundle containing standalone
`views/report.html` and script-free `views/report.pdf` files. Neither view is
opened automatically, and the signed `.hhpcr` remains the authoritative copy.

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

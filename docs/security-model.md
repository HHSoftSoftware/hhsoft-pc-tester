# Security Model

Status: mandatory architecture contract. Security by design and defense in
depth apply from source review through final report verification.

## Protected assets

- Installed operating systems, filesystems, partition tables, and user data.
- Accuracy and provenance of inventory and test results.
- Hardware serials, MAC addresses, asset labels, and component history.
- Report-signing, submission, and deployment-fingerprint keys.
- Release source, package locks, image contents, signing identities, and SBOM.
- Availability of the PC under test, including thermal and electrical safety.

## Trust boundaries

Treat firmware and attached hardware, disks and their filesystems, local
networks, internet services, runtime device metadata, upstream dependencies,
operator input, and imported reports as mutually untrusted. The official live
image is trusted only after its digest and release signature are verified.

An unmodified image does not make a measurement true. Driver behavior,
firmware, test coverage, environmental conditions, and operator observations
remain explicit evidence and limitations.

## Independent control layers

1. **Build trust:** protected tags, reviewed source-only inputs, exact package
   locks, source/license inventory, malware scanning, SBOM, repeatable builds,
   and GitHub artifact provenance.
2. **Boot trust:** signed digest manifest, payload verification in the boot
   chain, separate safe and recovery entries, and eventual Secure Boot support.
   Firmware-level Secure Boot is a feature, not permission to weaken runtime
   boundaries, and the appliance remains usable on legacy BIOS.
3. **Immutable runtime:** read-only boot content copied to RAM, fixed runtime
   packages, no auto-update, deterministic environment, no maintenance login
   in the normal appliance path, and reboot recovery.
4. **Privilege separation:** root daemon, unprivileged UI, per-adapter access,
   fixed command allowlist, process-group timeouts, output limits, and no report
   active content.
5. **Device containment:** no internal filesystem auto-mounts, no automatic
   journal replay or volume activation, read-only opens by default, and a
   central destructive-target authorization capability.
6. **Network and privacy:** networking off until needed, deny-by-default
   firewall, explicit endpoint selection, bounded network tests, TLS validation,
   and no automatic report, log, serial, or probe upload.
7. **Evidence and recovery:** append-only event log during a session, thermal
   and power abort thresholds, signed evidence manifest, clear degraded states,
   and safe shutdown or restart after worker failure.

Each release must show at least one preventive and one detection or containment
control for every critical risk. A test adapter cannot approve its own target,
expand its own privileges, sign its own final result, or silently change its
network declaration.

## Legacy assurance

The Buildroot fallback deliberately has lower assurance, not deliberate
vulnerabilities. BIOS/CSM cannot authenticate its boot chain, and its separate
minimal kernel/driver stream receives less test coverage than the modern
images. It retains no-network startup, RAM-only state, read-only inventory,
no automatic block mounts, source hashes, and signed release metadata. The UI
and reports label this mode `legacy`; operators should use the Alpine image
whenever the CPU and firmware support it.

## Storage-write capability

All storage is denied write access until the daemon issues a short-lived,
single-test capability. The capability binds these values:

- immutable session ID and test definition version;
- kernel device path and stable hardware topology path;
- size, transport, model, and privacy-preserving serial fingerprint;
- exact byte range or named temporary file;
- allowed operation and expiration time.

Immediately before opening the target, the worker asks the daemon to re-read
and compare every field. Hotplug, path reuse, size change, boot-media identity,
report-media identity, mounted state, or policy change invalidates the
capability. Destructive whole-device tests require the operator to type a
displayed short challenge after all other disks are shown.

Initial report persistence follows this fail-closed pattern without granting a
destructive-test capability. A deployment-keyed HMAC authenticates the exact
device and policy binding; kernel boot ID plus monotonic uptime limits it to 15
minutes in one live session. Persistence accepts only a separately inserted,
removable USB partition that is the sole safely mounted partition on its
parent, verifies the report signature first, creates the destination
exclusively, requires its digest to equal the verifier's whole-bundle digest,
syncs and reads it back, then rechecks hardware identity, topology, and mount
policy. It never auto-mounts report media or writes internal drives.

The normal console save command adds a narrowly typed mount step after this
authorization. It invokes only fixed `/bin/mount` and `/bin/umount` paths with
validated device/mount arguments and `rw,nodev,nosuid,noexec`; persistence then
revalidates the mounted device, parent, filesystem, topology, boot binding, and
policy before copying any report bytes. Mount failure causes no report write,
and no destination is selected without typed operator confirmation.

Stable fingerprint activation uses a separate flow and never grants write
capability. Automatic boot remains ephemeral. After an explicit `K` selection,
the operator chooses a separately inserted token and types `LOAD`; HHSoft
probes only supported filesystems, mounts the exact partition
`ro,nodev,nosuid,noexec`, reads only `hhpt-deployment-key.v1`, revalidates the
mount/topology, and leaves the token mounted read-only so report persistence
excludes it. The token's bound block/partition/USB IDs are excluded from the
hardware-under-test fingerprint and plan. Any mismatch or activation failure
stops the run.

## Runtime execution

Test definitions select typed built-in adapters; they are not generic shells.
External tools are package-locked image files whose absolute paths and digests
are in the allowlist. Arguments are constructed from validated typed fields,
not concatenated. The daemon starts a clean environment with no inherited
shell, proxy, language package, plugin, or dynamic-loader search variables.

The Phase 1 graphics probe applies that model with fixed Mesa/KMS tool paths,
a validated bounded frame count, a fixed working environment, a 20-second
subprocess timeout, and a 64 KiB combined-output limit. It does not start the
network or mount host/internal filesystems. This contains routine tool failure
but cannot fully contain malicious GPU hardware, device firmware, or a kernel
graphics-driver vulnerability.

Run workers as a dedicated account when possible. Grant device access through
ACLs, file descriptors, or narrowly scoped capabilities rather than making the
UI root. A worker cannot reach signing keys. A local browser cannot reach raw
hardware and report HTML cannot call the local API.

## Threats and required responses

| Threat | Main layers | Required response |
| --- | --- | --- |
| Wrong drive is overwritten | device, UI, evidence | fail closed and record denial |
| Malicious filesystem triggers parser | immutable, device, worker | never auto-mount; isolate explicit inspection |
| Compromised upstream tool | build, worker, evidence | pin, review, contain, revoke release |
| Hostile LAN alters test or steals data | network, privacy | no dynamic tests; TLS and egress allowlist |
| Report files are edited later | evidence, signing | digest verification fails visibly |
| Valid report is replayed for another PC | fingerprint, submission | compare signed session and component identity |
| UI is compromised | privilege, API | no direct devices; capability expires |
| Worker hangs or overheats system | worker, recovery | watchdog and independent thermal abort |
| Serial hashes are enumerated | privacy | deployment HMAC; no raw identifiers by default |
| Firmware lies about hardware | evidence | cross-check sources; label limitations |

Hostile hardware and firmware can exploit kernel or platform vulnerabilities.
The appliance needs direct device access to diagnose hardware, so it cannot
fully isolate that class of attack. Run it only on devices that the operator is
authorized to test and do not connect the boot media afterward to a sensitive
machine without reimaging or write-protecting it.

## Key separation and revocation

Use distinct keys for release signing, report submission, deployment HMAC,
and local operator identity. Never place release or server signing private keys
on boot media. If a deployment key is stored locally, prefer TPM-bound storage.
The alpha offline token is plaintext and therefore relies on physical custody
and hardware write protection; it is explicitly lower assurance than a future
encrypted or authenticated key service.

Every signature records its algorithm and key ID. Publish key rotations and a
revocation file through a second independently controlled channel. Old reports
remain verifiable with retired public keys but new reports cannot select a
revoked key.

## Security release gates

- Safe quick-profile boot succeeds with all internal filesystems unmounted.
- Negative tests prove command, path, hotplug, mount, and target guards fail
  closed.
- API authentication, origin checks, worker limits, and log redaction pass.
- Images have no undeclared executable and every executable appears in SBOM.
- Dependency and image scans have no unaccepted critical vulnerability.
- Reports verify offline and fail after changing any covered byte.

# Test Adapter and Definition API

Status: stable specification 1.0.0. This document defines the first supported
third-party extension boundary for HHSoft PC Tester. It is a compatibility
contract, not a statement that every feature below is implemented yet.

The protocol family is `hhpt.adapter/1`. Minor protocol revisions may add
optional behavior as described under version negotiation, but must preserve
the security rules in this document.

## Purpose and Scope

The API lets reviewed third-party source projects add hardware tests without
turning the appliance into a general-purpose plugin runner. A test definition
describes applicability, profiles, resources, expected duration, timeout,
operator interaction, and evidence. A sandboxed adapter performs one frozen
test and returns structured evidence. The root-owned orchestrator remains
responsible for discovery, policy, hardware identity, plan construction,
resource grants, safety monitoring, result acceptance, report signing, and
overall status.

The API applies to the modern Alpine images. A legacy image may implement a
documented subset, but it must report unsupported definitions rather than
silently changing their behavior. Runtime installation from a USB drive,
network, report, or other untrusted source is outside version 1.

Normative words `MUST`, `MUST NOT`, `REQUIRED`, `SHOULD`, and `MAY` describe
requirements on an implementation claiming conformance.

## Security Invariants

1. Adapters are untrusted after launch, even when their packages are signed.
2. Adapters run as dedicated, unprivileged workers. They never run in the
   orchestrator, UI, report signer, or verifier process.
3. A worker receives only the capabilities, devices, endpoints, helpers,
   parameters, duration, and evidence budget frozen into its plan entry.
4. A worker cannot approve its own target, add itself to a plan, change a
   profile, authorize destruction, expand a grant, mount storage, or sign a
   result or report.
5. Definitions select typed adapters and typed resources; they are not shell
   scripts. Adapter-controlled command lines, executable paths, package
   managers, interpreters with ambient imports, and dynamic plugin discovery
   are prohibited.
6. Runtime content is immutable. Reports, websites, deployment tokens, target
   filesystems, network responses, device strings, and operator text never
   become code or definitions.
7. Linux capabilities, host namespaces, ambient file descriptors, inherited
   environment variables, and unrestricted device or network access are
   denied by default.
8. The deployment HMAC key, report-signing key, release key, raw durable
   identity values, and report output location are never disclosed to a
   worker.

These controls contain routine adapter compromise. They do not remove risk
from malicious devices, firmware, kernels, drivers, or privileged kernel
interfaces. Definitions requesting a larger kernel attack surface require a
correspondingly stricter review and an explicit degraded-assurance annotation.

## Terms and Identifiers

- **Capability:** a product-owned hardware function such as `cpu`, `memory`,
  `graphics`, `display`, `input.keyboard`, `storage`, or `network` resolved
  from the normalized hardware graph.
- **Definition:** immutable declarative policy for one test ID and definition
  version.
- **Adapter:** one package entry point implementing one or more definitions.
- **Package:** a reviewed, signed, deterministic `.hhpta` archive integrated
  when the appliance image is built.
- **Worker:** one sandboxed adapter process for one scheduled test.
- **Grant:** a narrower, orchestrator-created authority usable only by one
  worker during one run.
- **Evidence:** bounded files and structured measurements produced by a worker.

Package, adapter, definition, criterion, and parameter IDs MUST contain 1 to
96 lower-case ASCII characters from `a-z`, `0-9`, `.`, `_`, and `-`. A
third-party ID MUST begin with a contributor-controlled reverse-domain or
deployment namespace, for example `com.example.camera.focus`. Names beginning
with `hhpt.`, `hhsoft.`, or an unqualified capability name are reserved.

The definition version is a positive integer and is independent of the
adapter's semantic version. A test ID never changes meaning in place. Changes
to acceptance criteria, parameter defaults, applicability, grants, duration,
evidence semantics, or operator instructions require a new definition
version.

## Package and Definition Format

An adapter package has this root layout:

```text
hhpt-adapter.json
definitions/<test-id>.json
bin/<architecture>/<entrypoint>
licenses/<files>
sbom.spdx.json
assets/<optional immutable data>
```

`hhpt-adapter.json` uses this logical schema. All top-level properties are
required unless marked optional, and unrecognized properties are rejected:

```json
{
  "schema": "hhpt.adapter.package/1",
  "package_id": "com.example.camera-tools",
  "package_version": "2.3.1",
  "api": {"minimum": "1.0", "maximum": "1.x"},
  "entrypoints": [
    {
      "id": "com.example.camera.focus-worker",
      "architectures": {"x86_64": "bin/x86_64/focus-worker"},
      "sha256": {"x86_64": "sha256:<64-lower-case-hex>"}
    }
  ],
  "definitions": ["definitions/com.example.camera.focus.json"],
  "helpers": [],
  "assets": [],
  "source": {"revision": "<immutable-source-revision>"},
  "license_files": ["licenses/LICENSE"],
  "sbom": "sbom.spdx.json"
}
```

Entries are relative slash-separated archive paths. Absolute paths, `..`,
duplicate names, links, special files, case-colliding names, and executable
files outside `bin/` are forbidden. Every listed binary, helper, definition,
asset, license, and SBOM has an independently verified size and SHA-256 entry
in the archive index. The archive is deterministic and contains nothing not
listed by its package manifest.

A definition uses this schema:

```json
{
  "schema": "hhpt.test-definition/1",
  "id": "com.example.camera.focus",
  "version": 1,
  "adapter": {
    "entrypoint": "com.example.camera.focus-worker",
    "minimum_version": "2.3.0"
  },
  "capability": "camera",
  "architectures": ["x86_64", "aarch64"],
  "profiles": ["quick", "standard", "extended"],
  "applicability": [],
  "destructive": false,
  "estimated_seconds": 20,
  "timeout_seconds": 45,
  "resources": {
    "devices": [{"capability": "camera", "access": ["read", "ioctl"]}],
    "network": {"mode": "none", "endpoints": []},
    "helpers": [],
    "operator_actions": ["visual.confirm"],
    "evidence": {"files": 8, "bytes": 8388608}
  },
  "parameters": [],
  "criteria": [
    {"id": "focus-visible", "required": true, "description": "Image focus can be judged"}
  ]
}
```

Only `quick`, `standard`, `extended`, and `destructive` are valid profiles.
`estimated_seconds` and `timeout_seconds` are positive integers; the timeout
MUST exceed the estimate and policy may lower both. `applicability` is a
product-defined allowlist of predicates over normalized inventory fields. It
cannot inspect paths, create component IDs, execute expressions, or override a
missing-driver or unsupported state. Unknown predicates fail validation.

Parameters are typed as `boolean`, bounded `integer`, closed-enum `string`, or
bounded ASCII `string`. Definitions set safe defaults and constraints.
Arbitrary JSON, filesystem paths, shell fragments, executable names, network
locations, and device paths are not valid parameter values. Effective values
are validated by the orchestrator and frozen into the plan.

## Capability and Plan Resolution

The orchestrator, not a package, owns the normalized inventory and capability
registry. A third-party definition normally consumes an existing capability.
A new capability requires core review because its discovery and
missing-driver rules affect every definition. Capability absence,
missing-driver status, unsupported architecture, excluded profile, and policy
denial remain distinct states.

Before execution, the orchestrator freezes a plan with the repository's
existing concepts: schema version, plan ID, profile, architecture, deployment-
keyed hardware-graph binding, definitions digest, capabilities and component
IDs, scheduled states, and per-test estimate and timeout. A third-party test
entry additionally binds:

- package, package version, package SHA-256, signer key ID, and trust class;
- adapter ID, adapter version, binary digest, negotiated protocol, and
  definition digest;
- exact definition version, criteria, typed parameters, and operator actions;
- selected opaque component references and each effective resource limit;
- any explicitly authorized destructive target and its one-use authorization;
- evidence and process budgets; and
- plan policy version and any degraded-assurance annotation.

The plan ID is `sha256:` plus the digest of canonical plan JSON with the ID
field empty. Any effective field above is part of that canonical input. The
adapter receives the final plan ID but cannot recalculate it because private
hardware identity material is withheld.

Hotplug after freezing does not silently amend the plan. The orchestrator may
issue a separately recorded plan amendment only after rediscovery, policy
resolution, and required operator authorization. An amendment references the
old plan ID and creates a new plan ID. Destructive authorization is never
carried across an amendment.

## Worker Sandbox and Grants

Each worker starts under a dedicated UID with no supplementary groups, no
Linux capabilities, `no_new_privs`, a fixed environment, a fixed read-only
working directory, an empty bounded `/tmp`, a private process/IPC/UTS
namespace, and a private network namespace. The runner restricts system calls,
process count, CPU, memory, wall time, and output with seccomp and cgroups or
equivalent controls. It denies mount, namespace creation, kernel module,
ptrace, BPF, keyring, reboot, raw I/O, and additional execution after start.
The adapter cannot see host `/home`, report storage, boot media, deployment-key
media, internal filesystems, or unrestricted `/proc` and `/sys` trees.

The orchestrator passes devices as opaque handles and pre-opened descriptors
with no ambient directory descriptor. The request contains sanitized
metadata, never a reusable host path or raw durable serial. Handle access is a
closed policy enum such as `read`, `ioctl`, or `event-read`. The orchestrator
maps each enum to a per-device ioctl allowlist and ACL. Unknown device classes,
broader access, or a mismatched component binding fail closed.

Internal storage is read-only unless the immutable plan contains a
destructive definition, policy explicitly authorizes that package and target,
and the operator completes the current typed challenge. Even then, a worker
receives no raw writable block descriptor. It sends bounded operations to the
root broker, which revalidates the central device capability, topology, size,
range, expiry, boot/report/deployment media exclusion, and mounted state
immediately before each operation. Brokered writes cannot exceed the frozen
range or named temporary-file policy.

Network mode is one of:

- `none` (default): no IP socket creation or network namespace attachment;
- `link`: a selected interface and local link-state observations only; or
- `endpoints`: isolated connectivity using only sockets opened by the network
  broker for frozen protocol, address, port, TLS-name, and byte limits.

DNS answers and redirects are revalidated by the broker and cannot broaden an
endpoint. Raw sockets, promiscuous capture, general LAN scanning, proxy
variables, package downloads, profile downloads, uploads, and report or serial
transmission are prohibited in version 1.

Helpers are package-locked image programs declared by ID, absolute image path,
digest, typed argument schema, and limits. The worker requests a helper by ID;
the runner validates arguments and launches it in an equal or narrower
sandbox. A definition cannot request `/bin/sh`, a generic interpreter, or an
undeclared program.

## Protocol and Version Negotiation

The runner and worker communicate over one inherited local `AF_UNIX
SOCK_SEQPACKET` control descriptor. No filesystem socket is exposed. Each
packet is one UTF-8 JSON object of at most 65,536 bytes. Extra bytes, duplicate
JSON object names, invalid UTF-8, non-finite numbers, and an unexpected message
for the current state terminate the worker.

The runner's first packet is `hello`:

```json
{
  "schema": "hhpt.adapter.hello/1",
  "protocols": ["1.1", "1.0"],
  "worker_id": "<128-bit-random-value>",
  "limits": {"packet_bytes": 65536, "control_bytes": 1048576}
}
```

The worker selects one offered version no greater than its package maximum:

```json
{
  "schema": "hhpt.adapter.accept/1",
  "protocol": "1.0",
  "adapter_id": "com.example.camera.focus-worker",
  "adapter_version": "2.3.1"
}
```

Major protocol versions are incompatible. Within major version 1, a minor
version may add an optional message, enum, or field but cannot change a default
or the meaning, privilege, accepted result, limit, or canonicalization of an
existing field. Both sides ignore only fields explicitly identified as
ignorable by the negotiated minor version; all other unknown fields fail
closed. Protocol patch releases clarify validation and do not appear on the
wire. The selected protocol is frozen in the plan and result.

## Run Request Schema

After negotiation the runner sends exactly one request. This is the complete
top-level request schema for protocol 1.0; `component_refs`, `parameters`, and
`operator_actions` may be empty arrays:

```json
{
  "schema": "hhpt.adapter.run/1",
  "protocol": "1.0",
  "worker_id": "<hello worker_id>",
  "run_id": "<128-bit-random-value>",
  "plan_id": "sha256:<64-lower-case-hex>",
  "test": {
    "id": "com.example.camera.focus",
    "definition_version": 1,
    "definition_sha256": "sha256:<64-lower-case-hex>",
    "profile": "quick",
    "destructive": false,
    "component_refs": [
      {"handle": "component-1", "capability": "camera", "metadata": {"category": "video_capture"}}
    ],
    "parameters": [],
    "criteria": ["focus-visible"],
    "operator_actions": ["visual.confirm"]
  },
  "grants": {
    "devices": [{"component": "component-1", "handle": "device-1", "access": ["read", "ioctl"]}],
    "network": {"mode": "none", "endpoints": []},
    "helpers": [],
    "evidence": {"files": 8, "bytes": 8388608}
  },
  "limits": {
    "started_before_ms": 2000,
    "deadline_monotonic_ms": 42000,
    "cancel_grace_ms": 3000,
    "memory_bytes": 268435456,
    "processes": 8,
    "stdout_stderr_bytes": 65536
  }
}
```

`metadata` is a flat map of explicitly allowlisted non-secret strings. The
request does not convey paths or authority. Device descriptors and broker
connections are associated with the JSON handles by the local runner. A
worker MUST reject a missing or duplicate handle, conflicting definition, or
request outside its compiled support instead of guessing a substitute.

## Lifecycle, Cancellation, and Failure

The lifecycle is `hello`, `accept`, `run`, zero or more bounded `progress` or
`operator_request` exchanges, and exactly one `result`. All message `run_id`
and `worker_id` values must match. The runner owns authoritative monotonic
timestamps.

A progress packet has schema `hhpt.adapter.progress/1`, the IDs, an integer
percentage from 0 through 100, and a bounded human-readable status. At most
one packet per second and 100 per run is accepted. Progress never extends the
deadline or proves that the worker remains safe.

An `operator_request/1` selects one action frozen in the request and contains
bounded display text and a closed set of choices. The UI, not the adapter,
collects input and returns an `operator_response/1` with a matching random
prompt ID. An adapter cannot request secrets, paths, arbitrary commands, rich
markup, or consent outside its declared action. At most one prompt may remain
open and its waiting time counts toward the deadline.

The runner may send `cancel/1` at any time due to the operator, global
deadline, thermal/power safety policy, device change, grant revocation, or
dependency failure. The worker stops starting work, asks children to stop,
closes device access, and returns an `abort` result during the frozen grace
period. The runner then closes broker grants and kills the worker's entire
process group and cgroup. Adapter-side timeout, crash, malformed protocol,
limit breach, absent result, or ignored cancellation produces an
orchestrator-synthesized `error`; a safety or operator cancellation produces
an orchestrator-synthesized `abort`. Neither becomes a pass.

## Result Schema

The terminal worker packet has this complete top-level schema. Unknown fields
are rejected under protocol 1.0:

```json
{
  "schema": "hhpt.adapter.result/1",
  "protocol": "1.0",
  "worker_id": "<hello worker_id>",
  "run_id": "<run request run_id>",
  "plan_id": "sha256:<run request plan_id>",
  "test_id": "com.example.camera.focus",
  "definition_version": 1,
  "state": "pass",
  "summary": "Operator confirmed usable focus.",
  "criteria": [
    {"id": "focus-visible", "state": "pass", "summary": "Confirmed by operator."}
  ],
  "measurements": [
    {"id": "frame-width", "kind": "integer", "value": 1920, "unit": "px"}
  ],
  "warnings": [],
  "evidence": [
    {
      "name": "frame-sample.png",
      "media_type": "image/png",
      "classification": "hardware-sensitive",
      "bytes": 15342,
      "sha256": "sha256:<64-lower-case-hex>"
    }
  ],
  "error": null
}
```

`state` is exactly `pass`, `fail`, `skip`, `inconclusive`, `error`, or
`abort`. `unsupported`, `not_present`, `missing_driver`, and `excluded` are
plan states and cannot be claimed by a worker. The orchestrator checks that
every required criterion has one result; it derives the accepted test state
from criterion policy and cannot be overridden by an optimistic adapter
summary. `skip` requires a preauthorized optional/declined reason.

Measurements contain a declared ID, one `boolean`, `integer`, finite `number`,
or bounded UTF-8 `string` value, and an optional declared unit. Criteria,
measurement IDs, units, limits, and interpretation come from the frozen
definition, not the worker. The orchestrator rejects impossible types,
undeclared criteria, duplicate IDs, or unbounded values.

`error` is `null` unless state is `error` or `abort`; otherwise it contains a
closed machine code, bounded message, and optional retryable boolean. Worker
messages are evidence, never instructions displayed as trusted system text.

## Evidence Limits and Acceptance

The protocol limit is 64 KiB per control packet and 1 MiB total control input.
Stdout and stderr are non-protocol diagnostic evidence, capped at 64 KiB
combined. A definition may request no more than 32 evidence files, 8 MiB for a
quick test, 32 MiB for standard, or 128 MiB for extended/destructive. Product
policy may lower these limits. A larger exception requires a new definition
version and explicit review; it cannot be negotiated by the worker.

Evidence is written only to a pre-opened empty evidence directory using
exclusive regular-file creation. Names are single safe path components.
Links, devices, FIFOs, sockets, nested paths, sparse files, and overwrites are
rejected. The runner independently reopens every file without following
links, verifies its type, computes bytes and SHA-256, and requires them to
match the terminal result. Files not named in the result are discarded and
the discrepancy is recorded. Exceeding any byte, file, descriptor, or time
limit terminates the run.

Classifications are `public`, `hardware-sensitive`, and
`operator-sensitive`. Raw serial numbers, MAC addresses, stable device
identifiers, credentials, network payloads, other users' data, and full host
memory are prohibited evidence. The orchestrator may redact, omit, or replace
adapter evidence with a digest. HTML, JavaScript, executable, or macro-enabled
evidence is never rendered as active content. Final report policy determines
which accepted evidence is embedded; omission is recorded.

Results receive an orchestrator-generated envelope containing authoritative
timing, exit and signal information, policy decisions, limit usage, grant
revocation, adapter/package provenance, and verified evidence digests. That
envelope and the accepted result enter the signed aggregate report. Adapter
signatures do not replace report signatures.

## Signing, Trust, and Image Integration

Every `.hhpta` has a detached JSON signature named `<package>.hhpta.sig`:

```json
{
  "schema": "hhpt.adapter.package-signature/1",
  "algorithm": "ed25519",
  "key_id": "sha256:<sha256-of-covered-public-key>",
  "package_sha256": "sha256:<sha256-of-exact-package-bytes>",
  "signature": "<base64-signature>"
}
```

The signature covers the domain-separated exact package digest and package
ID/version. Accepted signer public keys, namespace ownership, validity, and
revocation are a separate signed trust policy. A valid signature authenticates
provenance and package bytes; it does not prove safety, correctness, or HHSoft
endorsement.

Version 1 packages are accepted only as source-reviewed image-build inputs.
The isolated release pipeline verifies signatures and digests, scans and
builds source, creates the package SBOM/license record, tests the sandbox and
failure paths, and copies accepted artifacts into the immutable image. The
resulting release manifest identifies every integrated package. Runtime update,
download, extraction, or trust-root modification is forbidden. Deployment-
integrated packages must use a separately built and labeled image and remain
distinguishable from official HHSoft releases.

Release signing keys, report keys, and deployment HMAC keys are distinct from
adapter package keys. No private key belongs in a package, source repository,
image, report, or worker sandbox.

## Compatibility and Removal

The test-definition schema and wire protocol follow semantic versioning at
the API level. Definition integers identify test semantics, while adapter and
package versions use semantic versioning. Report readers retain the original
IDs, versions, package digest, negotiated protocol, criteria, units, and
degraded states; they never reinterpret an old result under new criteria.

The orchestrator schedules a definition only when the architecture, profile,
API range, package trust, adapter version, resources, helper digests, and
policy all match. A missing or revoked package, unsupported API, unavailable
grant, or architecture mismatch is reported with the corresponding explicit
coverage state. It never falls back to another adapter or older definition
without producing a different frozen plan.

An API major version may remove behavior only after the project publishes a
migration specification and maintains the prior report schema/verifier.
Security revocation may stop future execution immediately, but cannot alter an
already signed report. A package that disappears from a later image remains
identified by digest in historical reports.

## Conformance and Contributor Process

An adapter is conformant only when its package and every definition pass:

1. schema, canonicalization, namespace, signature, license, provenance, and
   reproducible source-build checks;
2. automated positive, cancellation, timeout, malformed-input, missing-device,
   hotplug, evidence-limit, and denied-resource tests in a VM;
3. proof that forbidden namespaces, files, storage, network endpoints, helper
   execution, and secrets remain unreachable under compromise tests;
4. result-state, criterion, measurement, privacy, report, and backwards-
   compatibility fixtures; and
5. representative real-hardware validation for every supported architecture,
   device class, driver family, and destructive or privileged operation.

A contributor first proposes the definition ID, user need, capability,
acceptance criteria, resource request, evidence, supported hardware, license,
and threat model. Review resolves namespace ownership and whether core
discovery or a new broker action is needed. Merge requests then provide source,
tests, build metadata, SBOM, licenses, protocol fixtures, and an accountable
security contact. Binary-only, copied proprietary, remotely fetched at build,
telemetry-dependent, or self-updating contributions are not accepted.

Maintainers review capability/discovery changes separately from adapter code,
and high-risk storage, network, DRM, firmware, or kernel-interface changes
require security review. The release pipeline independently rebuilds and
packages accepted source. Contributors may publish detached package signatures,
but HHSoft release metadata determines what an official image actually trusts
and ships.

# Report Integrity and Hardware Fingerprinting

## Status and Goals

This document defines the version 1 integrity profile for HHSoft PC Tester reports. It is a design specification; it does not claim that every control described here is implemented.

The design must:

- detect any change to signed report data and collected evidence;
- compare a returned system with an earlier baseline without publishing hardware serial numbers;
- identify the deployment that issued an identifier without revealing a reusable deployment secret;
- preserve enough component-level information to explain likely parts swaps;
- work offline, while allowing stronger online custody and timestamp controls; and
- keep report signing, software release signing, and fingerprinting trust separate.

Integrity does not make measurements true. It makes the captured claims tamper-evident and attributable to a trusted report-signing key.

## Report Bundle

The broader design below describes a directory tree or ZIP archive using the
`.hhpcr` extension. The implemented compact bundle profile has a stricter
transport rule: verification reconstructs the deterministic ZIP and requires
its complete bytes to match. Linked retests use that verified canonical
bundle hash as well as the signed manifest digest. The generalized evidence
layout below is not a claim that every planned evidence file is emitted.

The version 1 layout is:

```text
report.hhpcr/
  report-manifest.json
  inventory.json
  results.json
  evidence-manifest.json
  evidence/
    ...
  views/
    report.html
    report.pdf
  signatures/
    <key-id>.json
  trust/
    self-issued-ed25519.pub  # optional reduced-assurance session profile
```

- `report-manifest.json` is the signed root. It identifies the schema and canonicalization profiles, product/version, report UUID, collection start/end times, execution mode, and every covered non-signature file with its byte length, media type, and digest.
- `inventory.json` contains normalized hardware observations and redacted source/provenance details.
- `results.json` contains test definitions, parameters, start/end times, disposition, measurements, warnings, and links to evidence entries.
- `evidence-manifest.json` describes every file under `evidence/` as specified below.
- `evidence/` contains raw or loss-minimized artifacts such as screenshots, test logs, device data, SMART/NVMe health data, camera/audio samples when explicitly enabled, and collector output.
- `views/` contains human-readable renderings. They are covered derived artifacts, but verifiers must use the machine-readable files as authoritative if the views disagree.
- `signatures/` contains one or more detached signatures over `report-manifest.json`. It is excluded from the root file list so that signatures can be added without rewriting the signed manifest.

Every regular file outside `signatures/` must appear exactly once in `report-manifest.json`. Symlinks, junctions, alternate data streams, device files, duplicate normalized paths, path traversal, and case-colliding paths are forbidden. A strict verifier fails closed on missing, duplicate, malformed, or unlisted content. Signature files are untrusted until independently verified and may not point outside the bundle.

A completed bundle is immutable. The implemented linked retest profile below
creates a new report rather than editing the original. Explicit correction
history, supersession, revocation and custody records remain separate planned
work; a retest reference does not implement those policies.

### Implemented Linked Retest Profile

Opt-in session/finalize uses `hhsoft.pc-tester.bundle-manifest/v2`. Its exact
root fields are `schema`, `files`, `previous_report` and `revision_kind`;
`revision_kind` is `retest`. `previous_report` has exactly two required fields:

| Field | Authenticated parent value |
| --- | --- |
| `manifest_digest` | The SHA-256 digest in the fully verified parent envelope |
| `bundle_sha256` | SHA-256 of the exact canonical parent bundle accepted by verification |

Both values use `sha256:` followed by 64 lowercase hexadecimal digits. They
come from the same verified artifact. The manifest alone excludes the signed
envelope, so a manifest digest can be shared by parents with different signed
IDs, times or signing keys. Requiring the full canonical bundle digest also
binds that distinction. No prior blob, raw identifier, result or key is copied
into the child through this reference.

The signature still uses the unchanged envelope-v1 signing domain. A linked
v2 bundle requires a verifier with explicit v2 support; no released minimum
version is claimed. Current session/finalize commands create self-issued
children, independently of whether parent trust uses a provisioned key. V2 requires
the HHSoft PC Tester product identity and a lowercase canonical UUIDv4 child
session ID, including when optional HTML/PDF views are absent. The producer
requires that ID to differ from the exact verified parent. An eligible parent
may be v1 or v2; legacy v1 parent IDs need not be UUIDs. Existing generic v1
verification and v1 archive/rendered bytes retain their historical rules.

One-hop verification independently verifies the child and supplied parent
under their explicit trust choices, matches both digests and rejects equal
report IDs. A v2 child can also be verified alone: its signed reference is
shown, but the relationship remains unchecked. There is no recursive fetch,
automatic parent discovery or transfer of trust, outcomes, hardware identity,
chronology or complete-history claims. Static HTML/PDF views display the
reference and its limits, not a dynamic local verification result.

The command path owns acquisition of child report, child public key, parent
report and parent public key as applicable. Parsing and verification use
captured bytes released only after every acquired metadata/readable owner
closes successfully. Linux and Windows provide the supported adapters; other
OSes fail explicitly. Reports are bounded to 64 MiB and external public keys
to exactly 32 bytes. Provider errors are reduced to fixed diagnostics.
Remaining legacy key inputs in finalization, discovery, persistence and save
are outside this increment; neither blocked-kernel deadlines nor filesystem
durability follow from checked input closure.

A linked session retains only a private verified-parent summary and passes it
directly to the existing consumed finalization gate. A journal file cannot
supply that authority. Current evidence, safety observations and cleanup
remain mandatory. Linked standalone finalization also checks cancellation
before signing and output; a cleanly stopped live session retains its separate
bounded integrity context. Comparison, persistence and release trust domains
remain independent.


## Evidence Manifest

Each evidence entry contains:

- `path`: normalized bundle-relative path under `evidence/`;
- `media_type` and `byte_length`;
- `digest`: `sha256:<lowercase-hex>` over the exact stored bytes;
- `test_id` and `collector`: the test and component that produced it;
- `acquired_at`, acquisition method, and tool version;
- `source`: device/component reference using its report-local identifier;
- `classification`: `public`, `sensitive`, or `restricted`;
- `redaction`: whether content was redacted and the named deterministic rule set; and
- `truncated`: whether any source bytes or records were omitted, with reason.

Evidence must be collected without modifying the device under test whenever a read-only method exists. The report records when a test writes data, changes firmware/device settings, mounts storage read-write, or cannot preserve original evidence.

The evidence manifest is itself canonicalized and digested by the root manifest. Its entries bind all evidence files. The root manifest also lists each evidence file, allowing a simple verifier to validate the entire bundle without interpreting evidence semantics.

## Canonicalization

JSON documents use UTF-8 without BOM and the JSON Canonicalization Scheme (RFC 8785), under canonicalization profile `hhsoft-jcs-v1`.

Before canonicalization, the profile requires:

- all text values to be valid Unicode and normalized to NFC;
- object member names to be unique;
- no insignificant fields whose value is empty, null, or a platform default unless the schema requires them;
- RFC 3339 UTC timestamps with `Z` and exactly millisecond precision;
- quantities stored as integers in named base units; floating-point values, NaN, and infinity are forbidden in signed JSON;
- digest strings in lowercase hexadecimal with an explicit algorithm prefix;
- relative paths using `/`, with no empty, `.` or `..` segment; and
- component lists sorted by stable schema-defined keys, never collector discovery order.

The schema version defines identifier normalization per field. Examples include trimming outer whitespace, collapsing known padding, lowercasing hexadecimal, and mapping only documented vendor sentinel values (such as all-zero serials) to `unavailable`. Unrecognized values are retained after redaction rather than aggressively normalized. The normalization result records its rule-set version.

Canonicalization rules never apply to binary evidence. Binary files are digested byte-for-byte as stored. Compression or image re-encoding therefore creates a new report.

## Digests and Signatures

`SHA-256` is the required bundle digest in version 1. Algorithm identifiers make future migration possible. New profiles may add a second digest before retiring SHA-256; they must not silently reinterpret an existing profile.

The root manifest contains a domain string, `HHSoft PC Tester report-manifest v1`, and the `sha256` digest of every covered file. A signature signs these bytes:

```text
UTF8('HHSoft PC Tester report-manifest v1') || 0x00 || SHA256(canonical report-manifest.json)
```

`Ed25519` is the required version 1 signature algorithm. Each `signatures/<key-id>.json` records the profile version, key ID, algorithm, signature value, signer role, and optional certificate/deployment metadata. The signature file itself uses canonical JSON, but verification is performed over the byte sequence above. The key ID is the first 16 bytes of `SHA-256(canonical public-key encoding)`, encoded as lowercase hexadecimal; the complete public key is required to establish trust and resolve collisions.

HTML and PDF views display the report UUID, the root manifest digest, signature state, and a clear statement that the view is not independently verifiable without its bundle. QR codes may encode the UUID and manifest digest, but must not contain private inventory values.

The implemented PDF view uses a fixed text-only object set with no forms,
attachments, links, actions, scripts, or external resources. PDF text is escaped
and restricted to printable ASCII. For every present HTML or PDF view, the
verifier strictly decodes the signed inventory and regenerates the complete
view with the signed product and software version. The supplied bytes must
match exactly; a signature alone does not make externally supplied views safe.
The product is fixed to HHSoft PC Tester and version text is bounded. Renderer
output is limited to 32 MiB per view, including escaping expansion.
HTML evidence is limited to 1 MiB per result, 64 JSON nesting levels, and a
shared indentation budget before allocation. PDF generation limits line count
and escaped content before collecting pages; long fields wrap without copying
the whole remaining field at each line. These are parser resource limits,
not additional hardware-evidence validation.

The implemented profile accepts only its canonical inventory representation
when derived views are present. Optional absent views remain supported;
present empty or custom views fail. Compatible earlier software versions use
their signed version label, while incompatible renderer formats require the
matching report utility and fail closed. Views are never opened automatically.

The implemented inventory can also contain optional `coverage_scope` and
per-result `planned_status` fields. Finalization uses `frozen_plan` and includes
every frozen catalog row plus run integrity; direct capture uses `direct_capture`
for its limited collected checks. Planned dispositions remain distinct from
execution outcomes. New coverage claims require valid, consistent bindings and
statuses, including in bundles without views. The verifier checks their schema
and consistency; it does not reconstruct the original plan or rerun hardware
checks. Historical reports without these fields retain their existing rendering.
New reports require the matching companion utility.

Capture uses separate finite roles for its 32-byte fingerprint key and 64-byte
raw Ed25519 private key; the existing public-key input remains exactly 32 bytes.
Regular-file metadata establishes eligibility before readable acquisition, and
the readable handle must then match that object. Both owners close before key
bytes are accepted. Length is checked before private-key seed access, and the
existing seed/public-key consistency check remains. Input and cleanup faults
cannot become success when cancellation occurs concurrently.

Cancellation is checked before either key and before randomness or collection.
This increment leaves HMAC identities, signed bytes, coverage scope, later
worker/signing policy and output behavior unchanged. It adds no live-session
gate, key-erasure guarantee or durability claim. Host and selected Linux
amd64/386 fixtures qualified this capture boundary. At that stage, six legacy
raw-key calls remained in finalization, deployment/report-target discovery,
persistence and save. The finalizer increment below reduces the remaining key
calls to five; persistence's live verified-report owner remains separate work.

The finalizer reuses the finite 32-byte fingerprint role.
It accepts bytes only after checked metadata/readable closure, rejects malformed
successful reader results and preserves input-fault priority when cancellation
also occurs. Nil and typed-nil contexts are rejected locally before either
linked predecessor path can inspect them, as well as at fingerprint acquisition.
Fixed input diagnostics do not interpolate key paths or provider errors.

A verified predecessor summary remains immutable: fingerprint acquisition adds
one key read without reopening the parent or changing its signed reference.
HMAC identities, key epochs, coverage, report formats and later finalization
policy remain unchanged. A stopped worker context is not substituted for the
session's fresh bounded integrity context. Host and selected Linux amd64/386
fixtures qualified this finalizer boundary. Five legacy raw-key calls remain,
along with the separate frozen-plan/evidence and persistence-owner work.

The report-target fingerprint implementation uses the same finite 32-byte
role. Metadata eligibility precedes readable acquisition; descriptor identity
and checked readable-then-metadata closure precede accepted key bytes.
Malformed successful byte lengths, input faults and closure uncertainty stop
the selected consumer before authorization or output. Input error and shape
rejection precede the post-read cancellation check, and fixed input diagnostics
do not disclose the key path or provider error.

Windows host and selected Linux amd64/386 fixtures passed for this consumer
boundary: 244 native named outcomes across four suites and 44 root executions.
It preserves the catalog and authorization schemas, HMAC binding, same-boot
authorization lifetime, target eligibility, typed confirmation and output
ownership. It adds no cancellation rule after a consumer begins. This reduces
the five calls recorded after finalizer qualification to four: one deployment-
discovery input, two persistence inputs and one save input.
Other readers and persistence's live verified-report owner remain separate work.
Other-OS acquisition and installed-CLI execution remain unqualified; existing
image, physical-PC and release gates remain open.

The save fingerprint-input implementation reuses the finite 32-byte owned
reader after existing usage and audit initialization. Metadata eligibility
precedes readable acquisition; matching descriptor identity and checked closure
of both owners precede key acceptance. Nil or typed-nil contexts and a missing
reader are rejected locally at this boundary. Key errors and malformed successful
lengths take precedence over the post-read cancellation check, with fixed
acquisition diagnostics that omit input paths and provider errors.

The clean reader-cancellation exception requires a nil byte slice and an exact
`context.Canceled` or `context.DeadlineExceeded` equal to `ctx.Err()`. A wrapped
or joined error, a non-nil byte result with an error, or cleanup uncertainty stays
an input fault even when cancellation also occurs. Neither outcome enters the
live report-source path. Audit initialization remains earlier and its closure
contract is unchanged. The report verification, authorization/HMAC, confirmation,
retry, mount/persistence/unmount and receipt policies are preserved.

Host and native save fixtures passed with the
[recorded evidence](requirements-validation.md#save-fingerprint-input-ownership).
Removing the save call leaves three legacy raw-key consumers after the historical
four-call report-target stage: deployment discovery and two persistence inputs.
Frozen-plan/evidence, audit and live report-source owners remain separate work.
This change adds no rollback, secure-erasure, blocked-call deadline, durability,
installed-CLI, unsupported-platform or physical-media qualification. Existing
image, physical-PC and release gates remain open.

The persistence key-input implementation replaces its two legacy raw-key calls
with the finite fingerprint32 and public32 owned-reader roles. Existing usage
validation and authorization-file read/decode run first. `DecodeAuthorization`
validates schema, binding, lifetime and policy shape; it does not authenticate the
MAC or establish current boot/device authorization. Those checks remain in the
existing persistence policy.

After decoding, nil or typed-nil contexts, a missing reader, a nil persister or a
stopped context reject before key acquisition. Each role receives the original
context and literal path. A returned error or a successful result of any length
other than 32 rejects before the post-read context check. This includes an exact
context error: save's nil-byte/exact-sentinel clean-cancellation exception does
not apply to persist. A valid closed result followed by cancellation uses the
persist admission failure. Both keys require checked readable and metadata
closure; same-path keys remain two distinct role-specific acquisitions.

Fingerprint admission precedes report opening. Trusted public-key acquisition
retains its position after report opening and before verification; self-issued
mode performs no public-key read. Fixed diagnostics cover only the two new input
boundaries. Existing authorization, source-open, verifier, persister and output
diagnostics are unchanged. Current v1/v2 bundle verification, linked-reference
semantics, trust choice, authorization/HMAC, naming and synchronized-receipt
policy are preserved; no parent is reopened or granted new trust.

Host and selected native Linux fixture qualification passed; see the
[measured evidence](requirements-validation.md#persistence-key-input-ownership).
The earlier save-stage three-call count becomes one deployment-discovery call.
Authorization-file, audit, frozen-plan/evidence and live report-source ownership
remain separate work. This input change adds no late cancellation policy,
rollback, secure-erasure, blocked-call deadline, durability, installed-CLI,
unsupported-platform or physical-media qualification. Existing image, physical-PC
and release gates remain open.

The deployment discovery-key input uses the finite fingerprint32 owned-reader
role. Usage validation precedes audit acquisition. A missing audit operation
rejects locally; after a successful audit open, context and the remaining required
operations are admitted before reading the key. Nil or typed-nil context, a
missing operation or an already stopped context rejects with
`deployment-key input acquisition failed` and exit 1. The existing audit closer
remains outside the checked key-owner boundary.

The reader receives the original context and literal discovery-key path. Any
returned error or successful result whose length is not 32 rejects with
`read media discovery key: input acquisition failed` before the post-read context
check. This includes exact or joined context errors and bytes returned with an
error; save's clean-reader-cancellation exception does not apply. Cancellation
after a valid closed result uses the local admission diagnostic. Readable and
metadata owners must close successfully before activation availability, media
selection or token loading can consume the key.

The discovery key remains an input to media discovery. It does not replace the
selected token's key/epoch or bypass token validation. Media eligibility,
confirmation, fixture exclusions, activation fields, failure-unmount behavior and
the successful read-only mount policy are unchanged. Fixed diagnostics cover the
new input boundary only; downstream diagnostics and audit/output policy remain
unchanged. No later cancellation, rollback or output-completion guarantee is added.

Host and selected native software fixtures passed within the documented
[validation scope](requirements-validation.md#deployment-discovery-key-input-ownership).
The final legacy raw-key caller and the now-unused reader are removed; this does
not close audit/token/mount/activation owners, frozen-plan/evidence readers or the
live report-source lifetime. Unsupported-platform acquisition, installed CLI,
physical media, blocked-call deadlines, secure erasure and durability receive no
new credit. Existing image, physical-PC and release gates remain open.

Quick-session reports additionally bind a `session` object inside the existing
run-integrity evidence. It records the stop reason, session budget, elapsed time,
all worker states/outcomes, independent safety observations, and bounded
monitor summaries. The safety history includes a fresh pre-sign
observation. This adds no catalog row and does not turn graph integrity into
an overall hardware pass. Unstarted tests stay incomplete;
completed failures survive a stop. Unsafe or interrupted network/storage
readiness cannot authorize signing. These checks occur during session
finalization; the offline verifier authenticates the recorded claims without
rerunning them. The periodic-monitor increment introduced
`hhsoft.pc-tester.quick-session/v3`; the run-wide thermal increment
introduced v4 with the same ten worker outcomes. The enabled pointer definition
changed the catalog digest and required a fresh frozen plan. The monitoring
increments leave that digest unchanged. A journal file cannot authorize new
execution or signing in place of the one-use live final gate. Those monitoring
increments retained 21 catalog rows plus run integrity. Historical signed reports,
including earlier session evidence,
retain their existing verification and rendering behavior. Direct capture's
limited subset does not include guided keyboard, pointer or exterior-inspection
evidence. Each retained pointer
pass, including a completed device inside a partial result, must match the
frozen and final component identity and advertised capabilities. Kernel bitmap
word width is independently observed and bound in the component fingerprint;
worker evidence cannot choose a weaker button interpretation. Automated and
native validation passed for pointer v1; physical qualification remains separate.

Pointer evidence v2 records canonical advertised wheel axes and positive/negative
complete-frame counts for one selected stream per axis. Definition 2 requires
fresh plans and cannot be substituted with v1 or hidden in a historical v4
session or execution note. V5 sessions bind its status and duration to the
worker; standalone pointer reports retain their existing frozen-plan workflow
without claiming session monitoring. Exact v1 decoding and historical rendering
remain separate. Consumers validate current claims even without optional views.
Windows and native Linux amd64/386 suites, race checks, and standalone and
signed-session CLI workflows passed for the wheel increment. VM observations
without supplied actions remained inconclusive or deadline-limited; rebuilt
images and physical wheel qualification remain open.

The mount/network monitor summary records bounded aggregate coverage,
with a one-second interval, a one-second observation deadline, a
2,500-millisecond maximum gap, and a two-second join allowance. The observer
runs one sample at a time. Signing requires a clean join, consistent timing and
sample counts, bounded leading and trailing gaps, and no latched failure.
A session before the first one-second tick records zero periodic samples
honestly; required preflight and final snapshots still apply. The first unsafe,
unknown, timed-out, or excessively delayed sample cannot be erased by a later
safe one, including the independent fresh pre-sign observation. This summary
does not establish continuous coverage or preemption of blocked kernel calls.
Automated and native Linux validation passed, including race checks and signed
CLI workflows. Each 2.5-second deadline case on modern x64/x86 and legacy x86
retained two periodic observations, and the signed summary exactly matched the
monitor diagnostic. Rebuilt-image and physical gates remain open.

The subsequent local CPU/RAM guard introduced v2 worker evidence with explicit
thermal coverage, completed-observation counts, peak, monitor state/failure,
and confirmed cleanup. That local-guard increment left journal v3 and the
22-result report unchanged.
Finalization rejects old v1 execution evidence and malformed or contradictory
v2 records, including existing unreadable files. An error/aborted execution note
may represent a missing file but cannot replace invalid existing CPU/RAM data.
Unconfirmed observer cleanup blocks signing regardless of the claimed outcome;
known calculation and memory failures cannot be downgraded. Joined thermal
stops retain their observed heat. Missing or partial sensor coverage remains
explicit and does not establish thermal health. These checks do not change
verification or rendering of historical signed reports. Windows tests, vet,
cross-builds, native Linux suites, race checks, and command-line workflows passed.
Rebuilt-image and physical-sensor qualification remain open.

The subsequent v4 session journal embeds `hhsoft.pc-tester.run-thermal/v1`.
It records initial and final observations, bounded aggregate coverage, the first
heat finding, worker execution intervals, preparation boundaries and both
monitor joins. Immediate and all-off sessions still require initial and final
thermal observations. Heat makes the session stop reason `thermal_limit`; it
does not make the separate hardware-graph integrity result fail. Available,
partial and unavailable sensor coverage remain distinct from observed heat.
The signed monitoring window ends after execution, cleanup, final inventory and
the final gate, before report encoding, output and saving.

Each retained local CPU/RAM heat source binds the full validated worker evidence
and its execution interval. Its `evidence_file_sha256` hashes the original file
bytes, including whitespace. The live finalizer compares that digest with its
retained byte copy. `evidence_typed_sha256` hashes this canonical input:

```text
"hhsoft.pc-tester.local-thermal-evidence/v1" || NUL || evidence_schema || NUL ||
json.Marshal(strictly validated version-specific CPU or memory wire value)
```

Both digests use `sha256:` followed by lowercase hexadecimal SHA-256. The typed
encoding uses the full declared result version, its field order and normal Go JSON
escaping, without indentation or a trailing newline. Report-only verification
recomputes the typed digest from embedded evidence; it checks the file digest's
format but cannot reconstruct original file whitespace. Duplicate, unknown,
case-aliased, missing or contradictory claims are rejected before interpretation.
The nested journal retains its 64 KiB limit even inside run-integrity evidence.
Local duration must fit its recorded worker interval, and heat cannot be replaced
by a missing-file execution note or a later cooler source.

CPU v3 adds a fixed scalar suite, ordered per-worker counts, optional exact
mismatch bits and separate integer/SHA-256/scalar failure flags. Its definition
version is 2; current finalization requires that pair. Historical definition 1
and CPU v2 keep their original field order, omission rules and typed hash through
an immutable wire type, including in strict v4/v5 reports. New scalar evidence
requires v5, v6 or v7 for session reports, but remains supported in strict standalone
capture/finalization. Unknown versions, impossible counter or failure
relationships, hidden claims and definition downgrades fail shared validation,
including bundles without views. Current v7 emission has twelve workers and
23 report rows. Host and native Linux validation passed for the v5 CPU increment,
including signed standalone and v5 session CPU cases. Rebuilt-image and physical
qualification remain open.

Memory definition 2 pairs with `hhsoft.pc-tester.memory/v3`. Its ten-phase pass
retains accepted read/write counts, completed phases and passes, the partial
phase and uncredited terminal work. A separate mismatch object makes offset-zero
failures explicit. Shared validation requires a feasible ordered phase prefix
and checked arithmetic. Copied summaries distinguish attempted reads, observed
matching bytes and accepted work; a mismatching read is never a verified byte.

Historical memory definition 1 and v2 keep their exact field order, omission
rules and nested safety encoding, including strict v4/v5 typed hashes. Current
memory evidence supports strict direct capture and standalone finalization;
session reports require v5, v6 or v7. Definition/schema mismatches, hidden claims and
impossible counters are rejected even without bundle views. Current emission keeps
22 test rows plus run integrity, twelve workers and the existing seven-row
direct-capture subset. Full host and native Linux validation passed for the v5
increment, including signed direct-capture, standalone-finalization and v5 session
cases. Rebuilt-image
and physical gates remain open for this increment.

New v4 claims use shared structural and binding validation in finalization,
verification, comparison and HTML/PDF rendering, including bundles without
views. Historical journals without new thermal claims remain opaque to these
new rules. Full Windows tests, vet and cross-builds passed. Native Linux
amd64/386 suites passed 3,032 test/subtest events and four race suites passed
1,203, with no skips; all 15 CLI scenarios passed. All twelve session scenarios
reported unavailable temperature telemetry and retained matching diagnostic,
journal and signed summaries. Rebuilt-image and physical-sensor qualification
remain open.

The guided keyboard increment introduces session v5 with eleven ordered workers
and keyboard definition version 2, while retaining 21 catalog rows plus run
integrity. Original v4 journals remain strictly validated under their ten-worker
schema. Keyboard evidence uses `hhsoft.pc-tester.keyboard-functional/v1` and
records only fixed completion booleans for left/right Shift and four arrows.
Every retained device pass must match the final signed inventory; finalization
also checks its unchanged frozen component, worker interval and cleanup.
Complete initial/final inventory cannot be reconstructed by an offline verifier.
Case aliases, duplicate or hidden modern claims and omitted bundle views cannot
turn functional evidence into an opaque historical or unexecuted note. A strict
nonexecution note can preserve standalone finalization without a session;
functional keyboard observations require v5, v6 or v7 in session reports. Current
emission is v7. Windows and native Linux amd64/386 qualification, including
race checks and signed CLI workflows, passed for the v5 increment. These checks
establish software behavior, not physical input coverage;
rebuilt-image and physical-keyboard validation remain open.

## Bounded Exterior Inspection Evidence

Exterior inspection definition 1 is an operator assertion about safely visible
enclosure surfaces inspected without moving or opening the device. Its v1
evidence has exactly nine fields: schema, source, scope, criterion, finding,
response completion, stop reason, status and `observation_milliseconds`.
The fixed source is `operator_reported`; it contains no free text, raw input
stream, photograph or component identity.

Strict decoding admits only the finite finding/completion/stop/status tuples,
bounds observation time to 0 through 45,000 milliseconds, and rejects missing,
extra, duplicate, aliased or malformed fields. Incomplete input cannot pass.
A completed defect survives a clean stop; a hard input fault remains an error
while retaining an observed defect. Time is credited observation time, not
preparation, cleanup or a hard execution deadline.

A typed result requires the exact inspection row, definition and whole-device
operator plan binding. A session claim also requires strict v7 integrity, its
inspection worker, matching outcome and observation time no larger than that
worker's interval. Current v7 retains the earlier eleven workers and appends
inspection. V4/v5/v6 keep their historical worker lists and cannot be relabeled
as current evidence. V7 does not activate the v6 storage sampling contract.

Complete validation precedes HTML/PDF output and runs during bundle verification
even without views. Absent bounded evidence requires the exact excluded,
error or aborted supervisor note allowed by its plan and worker state;
malformed present evidence cannot become an execution note.
Whole-token detection examines hidden, duplicate, case-aliased and escaped
claims before strict validation; an unknown family cannot bypass those rules. An ordinary integrity selection reference requires its
exact validated exclusion or session binding and actual inspection row; it
cannot hide an evidence schema claim.

The shared validated result supplies the view section. Reports without an
inspection claim, including historical reports, retain their prior view bytes.
Linked finalization observes the current run and preserves the predecessor's
exact manifest and canonical bundle reference; parent and child trust stay
independent. Comparison keeps its existing component-identity rules.

A valid signature authenticates these bytes and their stated provenance.
It does not establish that an operator inspected every surface, reported the
truth or tested physical condition. Full host validation and the selected
native Linux fixture suites passed. Broader limits remain in
[requirements validation](requirements-validation.md#bounded-exterior-inspection).

## Inactive Storage Evidence Reader

Explicit v6 read support adds a twelfth worker record, `storage.quick`, after
storage readiness. The journal's required `storage_source` field is null for
validated execution notes and an object for functional
`hhsoft.pc-tester.storage-sample/v1` evidence, including non-pass results. That
object contains exactly `worker_id`,
`evidence_schema`, `evidence_file_sha256` and `evidence_typed_sha256`. Both digests
use `sha256:` plus lowercase hexadecimal SHA-256. The typed digest hashes the
validated canonical Result bytes without a newline or domain prefix. The reader
recomputes that digest; it cannot reconstruct the original evidence file's
whitespace or independently prove its file digest from an embedded typed value.

Shared validation checks the finite result, topology/fixture reconstruction,
accepted counters, worker/schema/PlanID binding and feasible disclosed duration.
Retained passed devices require matching final identity and geometry even when
the overall run is non-pass. A vanished nonaccepted device may remain diagnostic,
with no accepted credit. Accepted failures survive later stops, and unconfirmed
cleanup rejects report acceptance. These checks apply through verification,
HTML/PDF rendering and comparison, including bundles without optional views.

Source digests, close/final-binding flags and timing remain signed assertions;
they do not prove kernel behavior, precise monotonic acceptance or physical
continuity. Buffered sample consistency cannot establish drive health, uncached
I/O or full-media coverage. Historical v4/v5 wire behavior stays supported and
those schemas reject storage sampling claims. Current commands emit v7
sessions with twelve workers; this reader adds no media worker or command and
leaves the sampling catalog entry inactive. Host fixture tests and Linux
amd64/386/arm64 cross-builds passed. Reader fixtures subsequently passed
natively on Linux amd64 and 386, including 62 verify/compare CLI cases
using provisioned trust and no/both views. That reader cohort predates
the separate output-completion fix. Media, rebuilt-image, physical and
release qualification remain open.

## Privacy-Preserving Fingerprints

Raw stable identifiers are sensitive. By default, reports do not store motherboard, chassis, DIMM, drive, battery, modem, or network interface serials, system UUIDs, permanent MAC addresses, TPM endorsement identities, or similarly linkable values. They store keyed pseudonyms computed during collection and discard the raw values from report memory as soon as practicable.

For each usable identifier, normalize the value under a versioned component-type rule and compute:

```text
component_id = HMAC-SHA-256(
  K_fingerprint_epoch,
  'HHSoft component v1' || 0x00 || component_type || 0x00 || normalized_identifier
)
```

The report stores `hmac-sha256:<hex>`, the component type, identifier source, confidence, key epoch, normalization rule-set, and non-identifying comparison attributes such as vendor/model, capacity class, interface, firmware family, and physical slot when available. It must not store the normalized identifier or the secret key.

Matching epoch labels alone do not prove that two deployments used the same
fingerprint key. Each payload therefore includes a non-secret key identifier:

```text
fingerprint_key_id = HMAC-SHA-256(
  K_fingerprint_epoch,
  'HHSoft fingerprint key identifier v1' || 0x00
)
```

Comparison requires both the epoch and complete key identifier to match. The
key identifier is suitable only with a uniformly random fingerprint key; it
does not authenticate a report and never replaces its Ed25519 signature.

One component record is created per independently replaceable item. A whole-system fingerprint is a convenience index, not the only comparison value:

```text
system_fingerprint = HMAC-SHA-256(
  K_fingerprint_epoch,
  'HHSoft system v1' || 0x00 || canonical ordered component identity set
)
```

The ordered set includes only component type and `component_id`; it excludes report-local labels, volatile state, test results, timestamps, USB topology, network leases, OS-assigned device paths, and optional peripherals. Missing identifiers remain explicit and never become a shared pseudonym. Multiple identical unidentifiable parts are represented by an inventory count and attributes, not falsely claimed as unique.

Identifier confidence is recorded as:

- `durable`: manufacturer-programmed identity expected to follow one physical part;
- `indicative`: likely stable but firmware-resettable, vendor-duplicated, or incompletely documented;
- `contextual`: slot/topology or attributes useful only as supporting evidence; or
- `unavailable`: no reliable identifier was exposed.

The default privacy mode supports linkage within one deployment and key epoch, not across independent deployments. A separately consented forensic mode may put encrypted raw identifiers in `restricted` evidence for an authorized verifier, but plaintext values must never appear in logs, filenames, HTML, PDF, QR codes, or remote diagnostics.

Each frozen plan also contains deployment-keyed HMACs for the complete stable
hardware graph and every type/ID component binding. Finalization independently
collects that graph again. Every accepted test record carries the frozen plan ID
and planned graph HMAC, while `run.integrity.quick` records the final graph HMAC
and any added, removed, or changed component IDs. A changed graph is signed as
a failed run-integrity result rather than silently attaching tests to different
hardware or discarding potentially useful evidence.

## HMAC Deployment Identifiers

HMAC keys are separate from signature keys. An HMAC proves no public authenticity and a signature must never be treated as a fingerprinting secret.

An online managed deployment should keep `K_fingerprint_epoch` in an access-controlled verification service or HSM and return component pseudonyms to the collector over an authenticated channel. The implemented offline alpha supports an explicitly selected, separately controlled USB token that is mounted and revalidated read-only. Its fixed key file is checksummed but not encrypted at rest, so it is a reduced-assurance option for physically secured small deployments. Copying one shared key to every technician USB broadens the impact of one compromise and should be avoided.

The deployment itself is represented by a non-secret random UUID and a public signer key ID. Where a stable private deployment code is needed, compute it from a deployment master key:

```text
deployment_code = HMAC-SHA-256(
  K_deployment,
  'HHSoft deployment v1' || 0x00 || deployment_uuid
)
```

The report may store a truncated display form of this code only for routing; security decisions use the complete value. It does not contain `K_deployment`, `K_fingerprint_epoch`, a key derivation seed, or material from which another epoch can be derived.

Keys are purpose-separated and scoped by deployment, environment, and epoch. Key derivation uses HKDF-SHA-256 with fixed labels rather than ad hoc concatenation. Production and test deployments never share keys.

## Parts-Swap Comparison

Parts-swap comparison operates on component records, not only the whole-system hash. The verifier:

1. checks both report signatures, bundle digests, schema profiles, deployment, and HMAC epoch;
2. selects a named signed comparison policy;
3. matches durable component pseudonyms by type, allowing policy-defined multiplicity;
4. uses indicative pseudonyms and attributes only to support or qualify unmatched records;
5. separately evaluates motherboard/system, CPU package, DIMMs, internal drives, batteries, displays/panels, discrete GPUs, network/modem devices, and other policy-selected replaceable classes; and
6. emits a signed comparison record referencing both root manifest digests and explaining every match, addition, removal, ambiguous match, and unreadable identity.

Default outcomes are `match`, `authorized_change`, `suspected_swap`, `inventory_change`, and `inconclusive`. Optional, external, hot-pluggable, or policy-excluded hardware does not change the core outcome but is still listed. A single changed aggregate hash must never be the sole explanation or denial basis.

Comparison across key epochs requires an authorized service that can recompute or bridge pseudonyms from protected enrollment material. The report itself cannot bridge epochs. Different deployments are deliberately unlinkable unless both operators explicitly use a mutually trusted comparison service.

Hardware attributes and topology may explain differences but do not override a durable identity mismatch. Missing data lowers confidence. Thresholds and component criticality belong to a versioned, signed policy, not application source defaults that can change silently.

The current offline `hhpt compare` implementation verifies both `.hhpcr`
signatures, applies the named `hhsoft.parts-swap.v1` conservative matching
policy, and writes a JSON comparison referencing both manifest digests. It
fails closed across HMAC epochs or key identifiers, uses identifier source and
confidence when classifying a replacement, and keeps identity-less or
ambiguous duplicate records unresolved.
Signing policy decisions and append-only comparison receipts remain service
work; local JSON is an explanation of signed inputs, not an authorization.

Comparison verifies captured report and applicable public-key bytes only after
checked metadata/readable-handle closure. Input failure, unconfirmed closure
or cancellation observed before output dispatch prevents a comparison result;
input and untrusted-payload diagnostics do not echo provider details. The
actual command context also reaches the destination callback. Cancellation
there may leave an empty new file, without adding rollback or durability.

Canonical v1/v2 verification, global self-issued Boolean behavior, default or
separate after-key trust, comparison schema and HMAC identity policy remain
unchanged. Host and selected Linux amd64/386 software fixtures qualified this
bounded command path. The separately qualified capture boundary is described
above; remaining legacy key inputs and persistence's live source owner are not
covered by this comparison increment.

CLI output completion is separate from signature verification and durable
persistence. The shared inventory, plan, comparison, report and quick-test
output path requires successful writes and owned-file close before
reporting output success. Combined write/close failures remain visible,
and finalization prints its success banner only after close. Existing
destinations are never overwritten, but failed new outputs may remain
partial. Caller-owned stdout is not closed. These checks do not add
fsync, durable-storage or native fault qualification.

## Offline Report Persistence

`hhpt report-target` lists separately inserted, writable removable USB
partitions and derives a typed challenge without exposing the raw media serial.
Correct confirmation issues a 15-minute, same-boot, deployment-HMAC
authorization in RAM. After the operator mounts only that partition at the
fixed report path with `rw,nodev,nosuid,noexec`, `hhpt persist` independently
verifies the signed `.hhpcr`, creates a new destination file without following
links or overwriting evidence, requires the copied digest to match the exact
bytes accepted by that verification, syncs and reads it back, and revalidates
device identity and mount policy. The JSON receipt records copy size, digest,
read-back, and sync outcomes;
it is evidence for the current session rather than an external append-only
receipt. Verification reconstructs the deterministic outer ZIP and compares
every byte, rejecting prefixes, trailing data, comments, compression changes,
unlisted local records, and other ambiguous archive encodings even when a ZIP
parser could ignore them.

The normal quick profile currently uses an ephemeral self-issued Ed25519 key
that is destroyed after finalization. Its covered public key is stored at
`trust/self-issued-ed25519.pub`, and the key ID is its complete SHA-256 digest.
`hhpt verify -self-issued` therefore proves byte integrity and signer
consistency after copying, not enrollment, operator identity, custody, trusted
time, or deployment authenticity. A provisioned public key remains mandatory
for trusted-report claims. The TUI labels this distinction and requires an
eligible-media selection plus its displayed `RPT-*` code before the fixed-path
mount and single report write.

## Trust and Key Lifecycle

Three independent trust domains are required:

- software release keys authenticate boot images, packages, SBOMs, and updates;
- report signing keys authenticate report manifests and comparison records; and
- fingerprint/deployment HMAC keys create private pseudonyms.

The public project contains release public keys and verification code, but never deployment or report private keys. Deployment owners enroll report signers and distribute a local trust bundle containing key IDs, public keys, roles, validity intervals, deployment scope, and key status. Where hardware permits, private report keys should be non-exportable and signing should be protected by TPM or another hardware-backed keystore. Report signing remains possible with an encrypted software key in documented reduced-assurance mode.

Keys have activation, rotation, retirement, compromise, and revocation states. Rotation creates a new key/epoch; old reports and signatures are not rewritten. Revocation records state whether signatures made before a trusted cutoff remain acceptable. Because offline system clocks are not authoritative, high-assurance deployments add an online timestamp/transparency receipt at ingestion and record first-seen time in an append-only service.

Recovery keys are offline, access-controlled, and used only to sign key-status records. Key backups use encrypted, tested recovery procedures. No backup artifact is stored in a report bundle or public source repository.

Verification is fail closed for unknown/disabled algorithms, malformed profiles, invalid signatures, revoked or unauthorized signers, digest mismatch, or unlisted content. If trust or revocation data is stale/unavailable, tools report `integrity valid; trust undetermined` rather than `trusted`.

## Limitations

- Firmware, virtual machines, device emulators, compromised collectors, malicious devices, and privileged software can lie about identity and test results. A valid signature attests to captured bytes and signer possession, not physical truth.
- SMBIOS, storage, SPD, MAC, and other serials may be absent, duplicated, privacy-randomized, forged, or changed by legitimate repair and firmware updates.
- HMAC pseudonyms are linkable inside their deployment/epoch. A stolen key permits dictionary attacks against low-entropy identifiers and creation of forged pseudonyms; HMAC is pseudonymization, not anonymization.
- Weak offline key storage allows report forgery by anyone who extracts the signer or fingerprint key. Secure Boot alone does not protect exported reports or establish operator custody.
- Offline timestamps can be wrong or manipulated. Without signed ingestion receipts, the bundle cannot prove creation time, ordering, or that it was the first report for that hardware.
- A report cannot prove that the same machine remained connected throughout all tests. Hot-plugging and pass-through can change the observed inventory during collection.
- Component comparison cannot reliably identify parts that expose no durable identity. It must return `inconclusive` rather than invent uniqueness.
- Fingerprints do not prove ownership, warranty status, authorization, non-counterfeit status, or that a detected replacement was unauthorized.
- Deleting a report cannot be prevented by bundle integrity. Durability requires operator-controlled backups or append-only ingestion outside the tested machine.
- Opening hostile HTML, PDF, media, filesystem, SMART/NVMe, EDID, USB, network, or other evidence is a parser risk. Verification should run with resource limits and no active content; views are sanitized and scripts are forbidden.

The implementation must publish test vectors for canonicalization, digests, signatures, HMAC inputs, path rejection, malformed archives, and cross-version verification before this profile is treated as stable.

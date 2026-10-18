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

The authoritative report is a directory tree or ZIP archive using the `.hhpcr` extension. ZIP is a transport container only: ZIP ordering, timestamps, compression, comments, and extra fields are not part of the signed content. A verifier extracts the archive safely and verifies the files rather than hashing the archive bytes.

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

A completed bundle is immutable. Corrections or retests create a new report UUID and may include the prior report-manifest digest in `previous_report`. Cancellation, supersession, revocation, and custody events are separate signed records rather than edits to the original bundle.

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

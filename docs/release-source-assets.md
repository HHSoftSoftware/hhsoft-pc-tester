# Source payloads and image inspection evidence

Every download candidate includes source material, notices and image inspection
records alongside all four ISOs. The release validator checks their exact bytes
and relationships. Licensing judgments, inspection results and the scan of
nested source archives still require authenticated lab review. A manifest or
successful structural check is not release approval.

## Standard source archives

Use `hhsoft-pc-tester-VERSION.sources-01.tar` through a consecutive final part,
with no gaps. There must be between one and 16 parts. Each part is at most
1 GiB, including tar headers and padding. This is below GitHub's requirement
that each release asset be under 2 GiB; GitHub also permits at most 1,000 assets
per release. See [GitHub's release limits](https://docs.github.com/en/repositories/releasing-projects-on-github/about-releases).
The part budget is a maximum, not a claim about available runner disk space.

Each part is an ordinary uncompressed USTAR archive containing whole regular
files. An individual source archive that cannot fit in one part is rejected;
there is no implicit splitting or concatenation format. Upstream `.tar.gz`,
`.tar.xz`, `.tar.bz2`, `.tar.zst`, `.tgz`, `.tar` or `.zip` files can be included
unchanged as members. The release validator does not unpack those inner archives.
Do not include an upstream archive whose contents have not been approved for
redistribution, including firmware omitted from the images.

Outer archive paths use `project/` for the exact tagged project files and
`sources/` for other source, license and build inputs. They are relative ASCII
paths with unambiguous components. Traversal, absolute paths, backslashes,
Windows device names, case aliases, file/directory collisions, private paths,
and agent-instruction names are rejected. Symlinks, hardlinks, device entries,
directory entries, PAX/GNU extensions and hidden extra entries are not accepted.

Members are sorted by path within each part. Headers use UID/GID zero, empty
owner/group names, timestamp zero, and mode `0644` or `0755`. Project-file mode
must match its committed Git mode. Data padding and the archive ending must be
zero bytes; the archive ends on the standard 10,240-byte record boundary.
The validator derives each expected USTAR header from the manifest and compares
all header, payload and padding bytes. It never restores an archive path or
executes supplied source.

## Source manifest

`SOURCE-MANIFEST.json` has exactly these top-level fields:

| Field | Content |
| --- | --- |
| `schema` | Integer `1` |
| `version`, `source_commit` | Exact release version and full tagged commit |
| `parts` | Ordered records with `name`, `size`, and lowercase SHA-256 `sha256` |
| `files` | Globally path-sorted member records |
| `packages` | Exactly `x86_64`, `x86`, `legacy-x86`, and `aarch64` package lists |

Each file record has exactly `path`, `role`, `part`, `size`, `sha256`, and
`mode`. Supported roles are `project-source`, `source-archive`, `source-file`,
`license-text`, `build-config`, `build-patch`, and `build-script`. Each logical
file occurs once in one declared part. Every part must have members, every
member must be listed, and every non-project member must be referenced by a
package record. At most 20,000 files and 4 MiB of manifest JSON are accepted.
Duplicate JSON keys and non-finite numbers are rejected.

The complete tagged project tree is included under `project/`. The validator
compares actual checkout bytes with raw Git tree object IDs without filters or
replacement objects, then compares those bytes and modes to the archive records.
This includes the actual project configurations, patches and build-control
scripts. Missing files, extra project files or bytes from a different revision
block the candidate. The project snapshot is bounded to 8 MiB per file and
64 MiB in aggregate. Additional generated configurations and upstream build
controls belong under `sources/` and must be referenced explicitly.

Each package record has these fields:

| Field | Content |
| --- | --- |
| `inventory` | Exact parsed row from that image's package inventory |
| `source_form` | `preferred-source` or `upstream-binary-form` |
| `sources` | Nonempty list of included project/source member paths |
| `licenses` | Nonempty list of included project/license-text member paths |
| `build` | Nonempty list of included source/build-control member paths |
| `notice` | Exact UTF-8 span in `THIRD-PARTY-NOTICES.txt`: byte `offset`, `size`, `sha256` |
| `review` | Nonempty, bounded explanation of the reviewed source form and obligations |

For modern inventories, row keys are `name`, `version`, `architecture`, `license`,
`origin`, `commit` and `sha256`. For the legacy CSV, retain all seven original
column names and values. Every inventory row must appear exactly once. Modern
inventories describe consumed package inputs, which can include material later
excluded from the image; a reference to that input does not authorize distributing
its entire upstream archive. The reviewer must choose permissible source and
license material and explain the relationship accurately.

SPDX sidecars must preserve the generators' actual declarations and provenance:
the product declares `BSD-3-Clause`, other conclusions/declarations remain
`NOASSERTION`, and original upstream licenses appear in inventory-bound comments.
Modern comments also retain architecture, origin and source commit; legacy
comments retain license-file names. The document describes the image, and each
package has the appropriate `GENERATED_FROM` (modern) or `CONTAINS` (legacy)
relationship. This validates the supported generated format, not every possible
SPDX serialization or the legal sufficiency of its declarations.

Both source forms require actual included material. `upstream-binary-form` is
for a reviewed upstream distribution in that form; it is not an exemption from
license conditions or a substitute for source where source is required. There
is no `unavailable`, `not-required`, empty-source or arbitrary-skip disposition.
The validator checks references and bytes, not whether a supplied archive is
legally or technically sufficient corresponding source. The lab must review
that question for each package and its build dependencies.

`SOURCE-OFFER.md` explains how recipients find and use the included material,
including any further fulfillment procedure that the review requires. Notices
must contain the actual applicable grants and attribution. URLs alone cannot
replace the required source payloads in this candidate contract.

## Application and finished-image records

Each ISO requires `.application.json` and `.inspection.json` sidecars. These are
sanitized summaries derived from preserved real build and inspection evidence;
they must not be manufactured from filenames or a desired result.

The application summary has exactly `schema: 1`, `source_commit`, `version`,
`architecture`, `go_version`, `host_manifest`, `project_files_sha256`, and
`binary`. `host_manifest` contains the committed host-manifest basename and its
SHA-256. `binary` contains `size` and `sha256`. `project_files_sha256` is the
SHA-256 of canonical ASCII JSON mapping every tagged project path to its `size`
and `sha256`, with sorted keys and compact separators. Derive this record from
the invocation's application binding and passed host checks. It does not claim
GitHub built the image or establish broader host qualification by itself.

The inspection summary has exactly `schema: 1`, `source_commit`, `version`,
`architecture`, `image_sha256`, `application`, and `firmware`. Its `application`
record contains `path`, `size`, `sha256`, `mode`, `uid`, `gid`, `version`,
`go_version`, `target_os`, `target_arch`, and `cpu_policy`. Actual extracted
bytes must match the application summary and modern package inventory. The
installed path is `/usr/bin/hhpt`, mode is `0755`, and numeric UID/GID are zero.
Product and Go versions must match the tagged policies.

| Image | Target architecture | CPU policy |
| --- | --- | --- |
| x86_64 | `amd64` | `v1` |
| x86 | `386` | `sse2` |
| legacy-x86 | `386` | `softfloat` |
| aarch64 | `arm64` | `v8.0` |

For each modern ISO, supply the real `.firmware-exclusions.json` build receipt.
The validator compares its architecture, every removed row, counts, source
archive hash and helper/policy hashes with committed policy. Its
`release_approval: false` remains false: successful filtering is not approval.

The inspection's `firmware` object contains `policy_sha256`, `receipt_sha256`,
and `layers`. Both `initramfs` and `modloop` layers contain exactly
`status: "pass"`, `checked_paths`, `checked_sha256`, `found_paths`, and
`found_sha256`. Checked paths are the complete sorted policy path list; checked
hashes are the sorted unique stored and uncompressed payload hashes from that
policy. Both found lists must be empty. The independent inspector must check
uncompressed/alternate-name payloads too. A successful build receipt alone cannot
satisfy this finished-image inspection. The legacy inspection uses `firmware: null`.

## Authentication and privacy boundary

Release evidence, image validation and lab-record documents use schema `2`.
Every required lab record includes an exact `assets` digest map covering that
image's inventories, application/inspection records, modern firmware receipt,
the source manifest, notices and source offer. Source parts are transitively
bound by the source manifest and directly bound by release evidence and
`SHA256SUMS`.

`source-content-scan` and `application-inspection` are required for every image;
`firmware-exclusion` is additionally required for modern images. Existing boot,
license/source, package-input, image scan, runtime-safety and report gates remain.
The source-content review must scan the actual outer members and recursively
inspect supported inner source archives for secrets, private information and
agent instructions. Unsupported or incomplete inspection cannot become a pass.
Only sanitized records belong in the public candidate.

CI checks canonical archive coverage and scans all text metadata with both
source scanners. It deliberately does not treat compressed inner source bytes
as inspected text. The lab's authenticated source-content review covers that
separate boundary. Neither a passing schema nor the lab's signature proves a
dishonest signer told the truth; the release reviewer must inspect the evidence.

Tests exercise ordinary tar interoperability, complete member/part coverage,
changed header/payload/padding/trailing bytes, unsafe names and file nodes,
missing package/source/license/notices material, stale bindings and wrong
application targets, and incomplete firmware inspection. Those fixtures do not
establish source fulfillment, real image safety or physical qualification.

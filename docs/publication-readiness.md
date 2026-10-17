# Source publication and first downloads

Source can be published before the complete product is finished. The first
download release must contain the x86_64, x86, legacy x86, and beta AArch64
bootable ISOs together with the Windows/Linux report utilities. There is no
utilities-only release path.

The first target is `0.1.0-alpha.1` after these gates pass. The alpha covers the
implemented bounded checks and documents their limits; completion of the full
roadmap is the later `1.0` milestone. The alpha label does not waive source,
image, signing, licensing, or validation requirements.

The initial source push and GitHub validation passed. Current repository
protections and the single-maintainer review exception are recorded in
[repository-protections.md](repository-protections.md). The download gates
below remain separate from source publication.

## Before the first push

1. Run `python scripts/security/publication_check.py --gitleaks PATH_TO_GITLEAKS`
   with the exact version in `scripts/security/gitleaks.lock`. This scans
   publishable working files, actual index bytes, and the raw file blobs and
   commit/tag metadata reachable from every Git ref and `HEAD`. Both custom
   rules and Gitleaks inspect history; scanning raw blobs also covers secrets
   introduced only by a merge. It does not scan the ignored local lab as if it
   were source to publish.
2. Keep additional private terms in ignored `.hhpt-source-denylist`. The
   project owner's private terms must stay local; no encoded copies belong in
   the scanner. Agent instructions and state are ignored and also rejected if
   force-added or found in history. The check rejects shallow history, legacy
   grafts, and refs or tags that do not resolve to commits. It reads original
   Git objects without applying local replacements. Symlinks, submodules, and
   unmerged index entries block publication; filesystem links in candidate
   paths are rejected before reading their targets.
3. Use the approved identity `HHSoft Software <info.hhsoft@gmail.com>` for
   both author and committer. Make the initial signed-off source commit, then
   rerun the publication check. Before a first commit exists, its metadata
   cannot be verified by a scan.
4. Install the local push gate with
   `git config --local core.hooksPath scripts/security`. It uses
   `build/tools/gitleaks/gitleaks.exe`, `HHPT_GITLEAKS`, or a `gitleaks` on PATH.
   The hook supplies Git's exact outgoing object IDs to both scanners, including
   commits pushed by SHA that are no longer reachable from a local ref.
   The hook needs Python 3.11 or newer and Git. Do not bypass it with `--no-verify`.
5. Create/verify `HHSoftSoftware/hhsoft-pc-tester`, enable secret scanning and
   push protection, and push source only. Review the GitHub validation run.
   Protect `main` and release tags, require source validation and CODEOWNERS
   review, and restrict tag creation/update/deletion to the release steward.

No automatic scanner proves the absence of every possible secret. The exact
candidate file list and the scan results are the evidence for the source
publication review. Ignored keys, hardware reports, images, and lab captures
remain local. Never force-add them.

## One-time release configuration

Create a GitHub environment named `release` with required reviewer approval
and deployment rules allowing only protected `main`. Configure reviewers
according to the maintainer policy in `GOVERNANCE.md`. Enable immutable GitHub
releases. Restrict release mutations to the approved publishing principal and
freeze the candidate draft during the run. Protect `v*` creation, updates, and
deletion. Workflow concurrency alone does not prevent another writer changing
assets between the final check and publication; GitHub has no atomic
compare-and-publish operation in this workflow. The sole-writer restriction
is a prerequisite, not a protection that the script can infer.

Commit only the armored **public** keys at `security/release-signing-key.asc`
and `security/lab-signing-key.asc`. Independently verify and configure their
full uppercase primary fingerprints as repository variables
`RELEASE_SIGNING_KEY_FINGERPRINT` and `LAB_SIGNING_KEY_FINGERPRINT`.
Private keys remain outside source, images, and Actions. The lab key signs
completed evidence; the release key signs annotated source tags. An arbitrary
GitHub-verified signature does not establish an authorized release signer.
Only after those protections are checked, set `RELEASES_ENABLED` to `true`.
Missing keys, fingerprints, or authorization block publication.

The workflow uses the job-scoped GitHub token and short-lived OIDC signing
certificates. It needs no stored publishing token or release private key.
Artifact attestations require a public repository or a qualifying Enterprise
plan. See [GitHub environment protection](https://docs.github.com/en/actions/how-tos/deploy/configure-and-manage-deployments/manage-environments)
and [artifact attestations](https://docs.github.com/en/actions/concepts/security/artifact-attestations).

## Preparing a complete ISO candidate

Build from a clean, committed source revision whose `VERSION` is the proposed
release version. Development labels such as `0.1.0-dev.0` cannot be published.
Use reviewed package input locks with the [offline cache mode](package-cache.md).
Require a reviewed [builder host manifest](builder-host.md) and complete the
remaining builder qualification checks; matching that manifest alone is not
release approval.
Development builds that resolve live repositories do not meet that release gate.
Do not relabel older development images as a new release.

Place only these files in an ignored candidate directory:

| Files | Required content |
| --- | --- |
| `hhsoft-pc-tester-VERSION-ARCH.iso` | All four architectures |
| Each ISO plus `.spdx` | SPDX 2.3 package inventory |
| Each modern ISO plus `.packages.lock` | Exact package inventory |
| Legacy ISO plus `.packages.csv` | Buildroot target package inventory |
| Each ISO plus `.validation.json` | Reviewed boot and release checks |
| `THIRD-PARTY-NOTICES.txt` | Complete redistribution notices |
| `SOURCE-OFFER.md` | Working source fulfillment information for included software |
| `lab-evidence.json` | Sanitized, complete result records referenced by validation files |

Each `.validation.json` uses this shape. Replace placeholder values with the
actual source commit, image digest, and digests of supplied lab records. Keep
raw private captures out of the candidate; supplied records must contain
sanitized tool output and no hardware identifiers.

```json
{
  "schema": 1,
  "source_commit": "FULL_COMMIT",
  "image_sha256": "ISO_SHA256",
  "boot": {
    "bios": {"status": "pass", "evidence_sha256": "LAB_RECORD_SHA256"},
    "uefi": {"status": "pass", "evidence_sha256": "LAB_RECORD_SHA256"}
  },
  "reviews": {
    "locked-package-inputs": {"status": "pass", "evidence_sha256": "LAB_RECORD_SHA256"},
    "image-secret-scan": {"status": "pass", "evidence_sha256": "LAB_RECORD_SHA256"},
    "license-and-source-review": {"status": "pass", "evidence_sha256": "LAB_RECORD_SHA256"},
    "offline-and-storage-safety": {"status": "pass", "evidence_sha256": "LAB_RECORD_SHA256"},
    "report-integrity": {"status": "pass", "evidence_sha256": "LAB_RECORD_SHA256"}
  }
}
```

Boot modes are `bios` and `uefi` for x86_64, `bios` for x86, `bios-nosse2` for
legacy x86, and `uefi` for AArch64. Beta, release-candidate, and stable releases
also require a `physical-qualification` review for every image. This preserves
the real-hardware gate in the master checklist. IA32 UEFI and broad ARM laptop
compatibility remain outside the initial alpha's claims.

The image-secret review must inspect extracted image contents, including
initramfs, overlays, filesystems, embedded application metadata, and boot
configuration. Scanning compressed ISO bytes alone is insufficient. The
locked-input review must establish what exact package bytes the build consumed;
an inventory generated after resolving a live repository does not establish it.

`lab-evidence.json` has exactly `schema: 1` and `records`, a mapping from each
referenced SHA-256 to its complete result. Each result contains exactly:
`schema: 1`, `source_commit`, `image_sha256`, `check`, `status: "pass"`,
`tool`, `tool_version`, and `output`. Checks use `boot:bios`, `boot:uefi`,
`boot:bios-nosse2`, or `review:` followed by the review name above. The final
three fields are nonempty strings; retain sanitized evidence in `output`.
Record hashes use the SHA-256 of ASCII JSON with sorted keys, no whitespace,
and ASCII escapes (`release.record_digest`). Missing, changed, unrelated,
and unreferenced records are rejected. A status flag and an arbitrary hash
alone cannot pass this gate.

After the actual reviews pass, seal and independently check the candidate:

```sh
python3 scripts/release/release.py seal --candidate build/candidate \
  --tag v0.1.0-alpha.1 --commit FULL_COMMIT
gpg --armor --local-user LAB_SIGNING_KEY_FINGERPRINT --detach-sign \
  build/candidate/release-evidence.json
export LAB_SIGNING_KEY_FINGERPRINT=FULL_APPROVED_PRIMARY_FINGERPRINT
python3 scripts/release/release.py check --candidate build/candidate \
  --tag v0.1.0-alpha.1 --commit FULL_COMMIT
```

Sealing adds `release-evidence.json`, which binds every candidate file, the
source commit, and both platform locks. It validates supplied records but does
not perform VM/hardware tests. Signing adds `release-evidence.json.asc`.
The independent check verifies that signature against the configured lab key
in an isolated keyring with automatic key retrieval disabled. The environment
reviewer must inspect the actual evidence before approving any attestations.
CI authenticates the lab's statement; it does not rerun the extracted-image
secret scan or establish that a dishonest lab signer told the truth.

## Publishing downloads

1. Create a signed annotated `vVERSION` tag on the current reviewed `main`,
   using the configured release key, and push it through the source gate.
2. Create a **draft** release for that existing tag and upload only the complete
   sealed candidate. Keep it unpublished.
3. Run `gh workflow run release.yml --ref main -f tag=vVERSION`. The tag must
   equal both the dispatched `main` commit and the current remote `main` tip.
   Keep `main` at that revision throughout the run. Publisher code comes from
   protected `main`; a tag on an older ancestor cannot select an old publisher.
4. Review the successful source checks and authenticated lab evidence before
   approving preparation in the `release` environment. Preparation builds
   companion utilities, verifies their provenance, and signs `SHA256SUMS`.
   Approve publication after reviewing those outputs. The workflow rechecks
   tag/draft identity and every uploaded digest, then checks the published
   release is immutable and still contains exactly the validated bytes.

The pipeline never overwrites existing assets. If upload fails midway, the
release remains a draft; investigate and remove only that attempt's generated
companion assets before retrying. Do not modify ISO candidate assets during a
run. Publication sets prerelease status from the version and does not change
the latest-release pointer automatically. An ambiguous response to publication
requires inspecting the actual remote state; do not assume a failed job means
the release is still a draft. No automatic deletion or rollback is attempted.

Images come from the reviewed external lab. Only companion utilities receive
GitHub **build** provenance. The signed checksum manifest carries an explicit
**validation** statement covering the candidate, including this distinction.
Automated image production, QEMU runs in CI, and validation of the offline
package-cache mode remain separate checklist items.

To verify downloaded files, obtain the full commit from the signed release tag:

```sh
gh attestation verify SHA256SUMS \
  --repo HHSoftSoftware/hhsoft-pc-tester \
  --bundle checksums.intoto.jsonl \
  --signer-workflow HHSoftSoftware/hhsoft-pc-tester/.github/workflows/release.yml \
  --source-digest FULL_COMMIT \
  --predicate-type https://github.com/HHSoftSoftware/hhsoft-pc-tester/release-validation/v1
sha256sum --check SHA256SUMS
```

The manifest covers every payload and the verified `utilities.intoto.jsonl`
bundle. Only `checksums.intoto.jsonl` is excluded to avoid signing a manifest
that would need to contain its own signature.

# Release Process

The implemented draft-to-public download workflow and first-push procedure
are in [publication readiness](publication-readiness.md). The workflow requires
all four bootable images before publishing companion utilities. The broader
image-production pipeline below is the target architecture; local image builds
and recorded reviews remain necessary until its outstanding checklist gates
are implemented and validated.

## Branch and version model

Use protected `main`, short-lived pull-request branches, and signed tags.
`VERSION`, image metadata, Go build metadata, report schema, and GitHub release
must agree. A tag has form `vMAJOR.MINOR.PATCH` or a valid SemVer prerelease.

Use a Developer Certificate of Origin rather than a CLA. Require review from
owners of platform, storage policy, report security, and release workflows for
changes in those areas.

## Workflow split

1. `validate`: Go tests, schema fixtures, shell lint, documentation checks,
   source/license allowlist, generated-file cleanliness, and secret scan.
2. `image`: read the Alpine release, signer, and aports pins from
   `platform/alpine/base.lock`; require the bootstrap ISO hash and detached
   signature, locked package indexes for each architecture, and exact package
   versions and checksums. Build the legacy image separately from the pinned
   Buildroot LTS, signed Linux release, release manifests, checksums, and
   exact release-key fingerprints.
3. `boot-smoke`: boot every image through SeaBIOS or OVMF in QEMU, wait for a
   serial readiness record, run read-only fixture tests, and verify shutdown.
4. `security`: scan dependencies and final rootfs, enumerate undeclared
   executables and setuid files, test filesystem and device denials, and create
   SPDX plus CycloneDX SBOMs.
5. `attest`: create GitHub artifact build-provenance attestations and sign one
   SHA-256 manifest covering image, verifier, SBOM, notices, and user guide.
6. `release`: protected environment approval publishes immutable artifacts and
   checksums. The workflow never uploads a release from a pull-request context.
7. `pages`: publish the exact tagged documentation, security notices, public
   keys, verification steps, and support window.

Nightly work may use caches. Release builds start clean and never resolve a
floating branch, tag, package index, container tag, or test definition.

## Alpine rebase workflow

Run `scripts/build/Update-AlpineBase.ps1 -Version X.Y.Z` to fetch official
bootstrap-image checksums and resolve the exact matching aports tag. The
default action writes a reviewable candidate under ignored `build/`; `-Apply`
is the explicit source change. The script never discovers or selects latest.

Every candidate refreshes per-architecture package and source locks in a
networked dependency job. Release jobs consume only the reviewed locks and an
immutable package cache whose index and objects match their digests. This
separates routine package refreshes from Alpine branch upgrades and makes an
upgrade a small configuration change rather than a scattered rewrite.

The base-update pull request must contain an update note and pass old/new VM
boot comparison, device-fixture tests, report-schema compatibility, and at
least one real x86_64 system. Required x86 hardware runs in parallel until its
replacement is qualified. AArch64 remains optional but uses the same lock.
The separate AArch64 modern-media artifact is beta and must retain that label
until QEMU UEFI and representative real-device qualification pass.

## Legacy rebase workflow

`platform/buildroot/base.lock`, its external defconfig, and kernel source hash
are the only legacy base selectors. Update them in one reviewed pull request,
run a clean Buildroot build, compare `legal-info` manifests, and boot with an
emulated i586 CPU that has no SSE/SSE2. Keep the legacy image offline,
BIOS/CSM-only, visibly reduced in coverage, and independent of modern release
gates except report-schema compatibility.

## Update cadence

- Build and scan `main` weekly.
- Open a monthly base-package refresh pull request while the Alpine branch is
  supported.
- Cut a patch release promptly for accepted critical boot, kernel, network,
  parser, or privilege vulnerabilities.
- Rebase on a newer Alpine branch at least six months before current support
  ends, retaining the prior product release only for documented compatibility.

Do not update components on boot. Operators obtain a new signed image. This
makes a test session repeatable and avoids internet state changing execution.

## Artifact set

- architecture-specific hybrid ISO or EFI image;
- portable report verifier for Windows, Linux, and macOS where practical;
- SHA-256 digest manifest and detached signature;
- SPDX and CycloneDX SBOMs plus third-party notices and source offer links;
- build provenance attestation and reproducibility notes;
- HTML user guide and one printable PDF manual.

## Publication safety

Before creating `HHSoftSoftware/hhsoft-pc-tester`, scan Git history and current
files for reference binaries, absolute local paths, captured identifiers,
secrets, and report samples. The first push contains source and documentation
only. GitHub release storage receives built images later; third-party upstream
binaries are image inputs, not repository commits.

`scripts/security/scan-source.sh` runs automatically before each application or
image build. CI runs it with `--history` after a full-depth checkout, so terms
such as the private owner identity and captured authentication codes fail the
source gate even if removed from the current tree. It also rejects common token
forms, credential-bearing URLs, private-key headers, machine-local paths and
endpoints, and generated/private artifact paths without echoing the matched
secret. Maintainers may put additional literal terms in ignored
`.hhpt-source-denylist`; never commit that file or its contents.

CI also downloads the MIT-licensed Gitleaks CLI from the exact version and
SHA-256 in `scripts/security/gitleaks.lock`, then independently scans all
commits. This second engine broadens credential-pattern coverage;
it does not replace the deterministic private-identity rules. When the public
repository is created, keep GitHub secret scanning and push protection enabled
and treat every bypass as a reviewed security event.

GitHub Pages receives only public documentation and synthetic report fixtures.
Never publish a real hardware report by default.

## Documentation visual direction

Avoid the common generated landing-page treatment of purple gradients, glass,
wide centered hero copy, three identical rounded cards, and abstract security
art. Use square edges, visible one-pixel rules, a restrained charcoal, warm
gray, signal amber, and status-color palette, tabular hardware evidence, and
asymmetric utilitarian layouts. Accessibility and operational clarity win over
deliberate roughness. Final direction requires a separate design discussion.

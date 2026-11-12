# Governance

HHSoft PC Tester is maintained as a public, security-focused hardware
qualification project. `@HHSoftSoftware` is the initial project lead and
release steward. Maintainer additions and changes are recorded in this file by
pull request.

## Decisions and review

- Small backward-compatible fixes use normal review, focused tests, and the
  contribution checklist.
- Report schemas, identity policy, trust boundaries, destructive operations,
  boot-chain changes, release automation, and new privileged adapters require
  CODEOWNERS review and an explicit security-impact statement.
- Once two active maintainers exist, those security-sensitive changes and every
  release require approval from two maintainers. Until then, the project lead
  records a second deliberate review after tests finish rather than merging a
  security-boundary change directly from an unreviewed working tree.
- Incompatible decisions use an architecture-decision document, migration
  path, and Semantic Versioning major-version assessment.
- Maintainers disclose conflicts and do not review their own financial or
  vendor-specific exception without independent review.

## Releases

The release steward verifies the pinned inputs, checksums, SBOMs, test results,
supported-platform gates, and final provenance before approving a protected
Semantic Version tag. During the single-maintainer bootstrap, `@HHSoftSoftware`
holds the following responsibilities:

| Role | Responsibility |
| --- | --- |
| Release signer and steward | Protect the release key, review the complete candidate, sign the source tag, and approve protected publication |
| Lab-evidence signer | Review completed validation records and sign their exact artifact bindings with the separate lab key |
| Security disclosure coordinator | Receive private reports through the routes in [SECURITY.md](SECURITY.md) and coordinate fixes and disclosure |
| Incident responder | Investigate suspected compromises, assess affected releases, and coordinate key revocation or replacement and public guidance |

These assignments do not provide independent approval. The second deliberate
review and protected release-environment approval remain required. Changes to
these responsibilities require a reviewed update to this document; private-key
custody is not transferred merely by assigning a role.

## Ownership

| Area | Initial owner | Required evidence |
| --- | --- | --- |
| Boot and build inputs | `@HHSoftSoftware` | Lock validation, SBOM, VM/physical boot matrix |
| Hardware adapters | `@HHSoftSoftware` | Fixture tests, safety class, timeouts, report fields |
| Report identity and comparison | `@HHSoftSoftware` | Canonicalization, signature and adversarial-parser tests |
| Privileged/destructive operations | `@HHSoftSoftware` | Central authorization and two-maintainer review |
| Documentation and operator UI | `@HHSoftSoftware` | Tested instructions and accessibility review |

Community contributors can become owners after sustained reviewed work in an
area. Ownership grants review responsibility, not control over release keys or
deployment fingerprint keys.

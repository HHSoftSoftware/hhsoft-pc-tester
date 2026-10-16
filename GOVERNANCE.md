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
Semantic Version tag. Release-signing, disclosure, and incident-response roles
must be delegated before the first public alpha.

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

# Contributing

Contributions and merge requests are welcome. Until the first public release,
open an issue before substantial work so interfaces do not diverge.

Issue templates use the labels defined in
[.github/labels.json](.github/labels.json). Report security vulnerabilities
privately using [SECURITY.md](SECURITY.md), rather than a public issue.

## Requirements

1. Do not commit binaries, disk images, captured serial numbers, private keys,
   proprietary material, or third-party code copied without review.
2. Pin every build input and record its upstream project, checksum, source URL,
   and SPDX license identifier.
3. Keep inventory and ordinary tests read-only. Storage-writing tests must use
   the central target-authorization API.
4. Treat hardware reports as sensitive. Tests must declare every field and
   evidence artifact they collect.
5. Add focused automated tests for behavior changes and test boot changes in a
   disposable VM.

## Certificate of origin

Commits must contain a `Signed-off-by` trailer certifying the Developer
Certificate of Origin 1.1. Use `git commit -s` locally.

## Review

Pull requests use `.github/pull_request_template.md`. Maintainer ownership,
security-sensitive review requirements, and release decisions are defined in
`GOVERNANCE.md`. Do not use automated output as approval: the responsible
reviewer verifies generated evidence and safety assumptions.

## Versioning

Git tags and product versions follow Semantic Versioning:

- `MAJOR`: incompatible profile, report-schema, plugin, or policy change.
- `MINOR`: backward-compatible feature or hardware-test addition.
- `PATCH`: backward-compatible fix, package rebuild, or security update.
- prereleases: `v0.1.0-alpha.1`, `v0.1.0-beta.1`, and `v0.1.0-rc.1`.

An unchanged feature set rebuilt from updated base packages receives a new
patch version. Reports also record the OS, package lock, and Git revision.

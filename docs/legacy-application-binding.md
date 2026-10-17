# Legacy image application binding

The Buildroot wrapper compiles a new `hhpt` from verified committed source for
each legacy image invocation. It uses the same bounded raw Git source verifier
as the [modern image build](modern-application-binding.md). An earlier binary,
an archive-only source export, or a linked Git worktree cannot substitute for
the required standalone Linux clone.

## Source and compiler inputs

The helper copies raw Git objects into a temporary store with no repository
configuration, hooks, filters, alternates, replacement objects, or lazy fetching.
It checks the commit/tree hashes and compares every tracked regular file with
its raw committed blob. Private/generated tracked paths, links, submodules,
and extra compilation or embedded inputs are rejected. The source scanner runs
only on the verified Gitless export. Separate publication scans must still
cover the original worktree, index, history, and credentials before transfer.

The legacy wrapper uses that export for `build-go.sh`, `BR2_EXTERNAL`, the
reviewed Buildroot host patch, the post-build script, and the SPDX writer.
`SOURCE_DATE_EPOCH` comes from the authenticated raw commit's bounded committer
timestamp; the wrapper does not query the original checkout's Git configuration.

The installed Go compiler must match `.go-version`. The exported build script
runs tests and compiles Linux/386 with `GO386=softfloat`, `CGO_ENABLED=0`, and the
committed product version. Compiler and module downloads remain disabled.
When an offline source cache is supplied, both Go testing/compilation and each
Make phase run under `unshare --net`. Namespace failure has no online fallback.
Non-development images require the reviewed cache and actual-host gate.

## Installed application evidence

The fresh `application-VERSION-legacy-x86` work directory contains:

- `application-source/`: the verified committed source export.
- `application-bin/linux_legacy-x86/hhpt`: this invocation's new executable.
- `application-inputs.json` and `application-binding.json`: source commit/tree,
  per-file hashes, source timestamp, requested target/version, Go version,
  executed policy digests, and binary filename, ELF target, size, and SHA-256.
- `application-overlay.sha256`: the post-build receipt for `/usr/bin/hhpt`.

The host record selects `buildroot-image` and `platform/buildroot/base.lock`.
It records an approved manifest digest only after an actual-host comparison;
development builds explicitly remain unqualified. Existing binaries, binding
records, or receipts cannot be reused as a new invocation's evidence.

After installing the executable, the post-build script calls the shared verifier
to hash the actual regular file through a pinned Linux descriptor. Parent links,
symlinks, hardlinks, special files, and changed bytes fail the check. Permissions
must remain exactly `0755`, without set-user-ID or set-group-ID bits. Metadata
is checked around the read; root ownership is not assumed before fakeroot.
The copied size and digest must match the completed application binding before an exclusive
new receipt is written. The wrapper checks source, compiled binary, receipt,
and installed target again after image Make, after legal-info, and after output
and SPDX creation. A final check failure makes the invocation fail even if
partial output files already exist.

Buildroot and Linux archive checksum commands must themselves exit successfully.
Printing the expected digest followed by a nonzero exit does not satisfy the
archive gate. The existing signature, pinned hash, source-cache, copy-only
legal-info helper, and pre/post host checks continue to apply.

## Validation and remaining gates

The focused fixtures exercise poisoned old binaries and ambient overrides,
requested legacy target/version and compiler isolation, malicious Git
configuration, source changes, wrong ELF targets, stale receipts, bad installed
files, late mutations, and checksum failure with matching stdout. The real
post-build script is exercised against disposable source and target fixtures.
Native Linux execution is required for descriptor, shell, and raw Git cases;
portable-only test runs do not validate those boundaries.

These records bind one invocation. They do not establish approval, a trusted
kernel, an immutable builder, complete source/license closure, or reproducible
output. Changes made and reverted between observations remain outside the
checks. The recorded version and CPU baseline describe the verified build
inputs; independent inspection must also verify the actual binary's embedded
product version and target properties. Go build metadata alone is not evidence
of the product version string.

Extract `/usr/bin/hhpt` independently from the finished ISO and compare its
bytes with the binding record. The post-build receipt does not replace this
check. A fresh offline image build, no-SSE/SSE2 boot validation, runtime safety
and report checks, complete notices/corresponding source, and applicable physical
hardware qualification remain separate release gates.

# Modern image application binding

The Alpine image wrapper compiles a new `hhpt` for the requested x86, x86_64,
or AArch64 target during each invocation. It does not consume an earlier
`build/bin` executable. The installed Go package must match `.go-version` for
non-development builds, and `build-go.sh` independently checks the selected
compiler version before testing or compilation. Automatic compiler and module
downloads remain disabled.

The [legacy image wrapper](legacy-application-binding.md) reuses this source
verifier with its own fixed Buildroot policy and installed-file checks. A change
to the shared helper changes its recorded policy digest; validate application
bindings again for the final source commit intended for an image build.

## Source boundary

Use a standalone Linux Git clone containing the exact committed source intended
for review. A source archive or linked Git worktree is not a supported substitute.
The build records the actual commit and tree IDs; that identity does not itself
establish review or approval. The source and its history must pass the separate
publication and provenance checks before transfer to a builder.

The binding helper copies bounded regular Git object files into a temporary
object store without repository configuration or hooks. Git reads only raw
commit and tree objects there, using a cleared environment and disabled lazy
fetching. Their raw object hashes are checked. Shallow history, grafts,
replacement references, alternates, unsupported object files, and linked
repository paths are rejected. Original repository hooks, filters, fsmonitor,
and user configuration are not executed to establish source identity.

Every tracked source file must be a regular file whose actual bytes match its
raw committed blob hash. Symlinks, submodules, private/generated tracked paths,
and extra compilation or embedded inputs under `cmd` and `internal` are rejected.
Additional root Go inputs must also be tracked. Only those verified tracked
bytes are copied into the invocation's new source directory; unrelated ignored
directories and existing binary outputs are not copied.

The controlled `build-go.sh` runs tests and compiles inside that new source
directory. With an offline package cache, compilation runs under `unshare --net`;
namespace failure stops the build without a fallback. Source bytes are compared
again when compilation completes and after image creation.
The source scanner runs on this verified Gitless export before the compiler;
it does not consult the original checkout's Git configuration. Publication still
requires the separate full worktree, index, history, and credential scans.
The image wrapper also disables lazy fetching and replacement objects for its
aports revision and timestamp queries. Use the complete authenticated aports
checkout; those query options do not establish its provenance or sandbox all
repository configuration.

## Invocation records

The fresh image work directory retains `application-inputs.json` and
`application-binding.json`. They bind:

- The committed source identity and each included file's size and SHA-256.
- The requested architecture, product version, and `.go-version`.
- The actual application build, image wrapper, inventory, cache, SBOM, overlay,
  and source-check policy digests.
- The new binary's filename, ELF target, size, SHA-256, and installed path
  `/usr/bin/hhpt`.
- The approved host manifest digest only after successful verification against
  the actual host; development builds explicitly record an unqualified host.

The overlay generator hashes the regular file actually copied to
`/usr/bin/hhpt`. It must match the invocation's application digest before the
generator writes `application-overlay.sha256`. That receipt must be new:
an existing file or link is rejected. The wrapper compares the receipt and
binary with the completed binding both before and after writing the SBOM.
The SBOM receives this same invocation-only binary.

These records are build evidence, not an approval, signature, or immutable
attestation. Keep them with the reviewed build evidence; do not treat their
presence as a completed release gate. A matching host manifest also does not
establish upstream trust, kernel integrity, or full builder qualification.
Changes made and reverted between observations remain outside these checks.

## Firmware exclusions

The modern image build omits the exact firmware entries in
[`platform/alpine/firmware-exclusions.json`](../platform/alpine/firmware-exclusions.json)
while their redistribution or corresponding-source evidence remains unresolved.
An upstream `Unknown` label is treated as missing evidence, not a finding that
redistribution is prohibited. Firmware with reviewed alternative or conditional
license grants is evaluated separately.

The pinned input has 41 excluded paths. Before either the initramfs or modloop
is created, the wrapper runs a source-bound filter on both temporary firmware
trees. It requires the expected bytes and paths, including 29 selected modloop
entries for x86/x86_64 or 26 for AArch64. Unexpected compression variants,
matching payloads under another name, changed inputs, unsafe filesystem nodes,
or a missing or inconsistent receipt stop the build. The wrapper patches a
fresh private copy of the reviewed `update-kernel`; it does not alter the
installed host script. The build retains `firmware-exclusions.json` with the
removed paths, hashes, policy and helper digests.

Some older network adapters, sound devices, cameras, USB serial adapters and
other devices depend on these files. Their firmware-dependent functions may
therefore be unavailable in these images. Hardware detection alone must not be
reported as successful device qualification. The exact path list is the
compatibility reference; the filter does not remove an entire vendor's firmware.
Independent inspection of the finished images must confirm the omissions and
remaining notices before publication. The exclusion receipt does not replace
that inspection or authorize distributing an unreviewed upstream firmware archive.

## Remaining image checks

An independent check must extract the application from the finished ISO and
compare its bytes with the binding record. The overlay receipt does not replace
that check. Fresh offline image production, target boot tests, runtime safety,
reports, notices, corresponding-source requirements, and applicable physical
hardware checks remain separate gates. No application binding record closes
those gates or grants release approval.

## Validation evidence

The native Alpine build-helper suite passed 166 tests with no skips, including
raw committed-source checks, ignored-input rejection, wrong-target and changed
binary checks, poisoned old binary isolation, compiler namespace failure,
and the real overlay generator's copied bytes and ownership. Overlay failures
include changed installed bytes, a substituted link or FIFO, missing context,
and existing receipt files or links. The host inventory's 42 cases are included
in that suite.
The fixtures also prove that the original-checkout Git query can execute a
configured fsmonitor hook while the isolated source helper does not, that an
exported-source scan failure prevents compilation, and that checksum-command
failure cannot pass by printing the expected digest.

Source and wrapper fixtures use small synthetic repositories and inert compiler
or image tools where appropriate; they do not produce qualified images. A real
fresh compilation from the reviewed Git commit, its independent version/target
inspection, and finished-ISO extraction remain separate validation work.

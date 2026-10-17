# Offline legacy source inputs

Non-development legacy images require a reviewed Buildroot source cache as
the ninth argument to `scripts/build/build-buildroot-image.sh`. Only canonical
`MAJOR.MINOR.PATCH-dev.NUMBER` builds may omit it and use live downloads; those
outputs remain unqualified development images. Supplying a cache always enables
the review and network-isolation requirements, including for development builds.

Prepare dependencies separately from image production. Preserve the original
download archive bytes, upstream signature/hash review, licenses, and the
configuration used to collect them. A usable cache contains the complete host
and target dependency closure for the selected Buildroot configuration. Copying
only the Linux archive is insufficient. The initial 29-archive
[source lock](../platform/buildroot/source-cache-lock.json) is approved as a
controlled build input. Its [review scope](build-input-baseline.md#legacy-source-cache)
does not establish a complete fresh image or release qualification.

## Prepare a candidate

The tool accepts a completed Buildroot download layout with one package
directory per source package and regular archive or patch files immediately
inside it:

```text
linux/linux-VERSION.tar.xz
busybox/busybox-VERSION.tar.bz2
PACKAGE/SOURCE.tar.gz
```

Raw VCS checkouts, temporary downloads, unrelated files, links, hardlinks,
special files, empty archives, and deeper directory trees are rejected. Package
and object names use portable ASCII letters, digits, dot, underscore, plus,
tilde, and hyphen. Supported objects are tar archives compressed with gzip,
bzip2, xz, zstd, lzip, or lzma; tgz, tbz2, txz, and zip archives; and patch/diff
files with optional gzip or xz compression. A package's `.lock` may be present
only as a regular zero-byte file. This coordination metadata is checked but
excluded from payload hashes and staging; Buildroot creates fresh locks.

The limits are 512 package directories, 4096 payload files, 4 GiB per file,
and 16 GiB in total. Operations require Linux and reject symlinks in parent
directories as well as cache entries. Use explicit paths without `..`.

Generate a review candidate in ignored build storage, using the reviewed source
checkout and a separately prepared cache:

```sh
mkdir -p build
env -i PATH=/usr/sbin:/usr/bin:/sbin:/bin LC_ALL=C TZ=UTC \
  /usr/bin/python3 -I -B scripts/build/buildroot_source_cache.py candidate \
  --repository . --cache /mnt/source-cache/legacy-x86 \
  --output build/legacy-source-cache.candidate.json
```

Candidate creation never approves a cache or writes the production lock. It
requires a new output file under `build/`. The JSON records relative names,
sizes, and SHA-256 digests, without recording absolute builder paths. It binds
the complete `platform/buildroot` policy tree, the Go version, the Go and legacy
build scripts, and the cache verifier itself. The cache must include the exact
Linux archive pinned by `platform/buildroot/base.lock`.

Review both source authenticity and the manifest's contents. Only after that
review may the approved file be committed as
`platform/buildroot/source-cache-lock.json`. Reusing a candidate as approval to
make a failed build proceed is not a release check. Any bound policy or payload
change requires a new review.

## Consume the approved inputs

Pass the existing six source/signature/public-key arguments, then an output
directory, a fresh work directory, and the source cache:

```sh
sh scripts/build/build-buildroot-image.sh \
  /mnt/inputs/buildroot-VERSION.tar.xz \
  /mnt/inputs/buildroot-VERSION.tar.xz.sign /mnt/inputs/buildroot-release.asc \
  /mnt/inputs/linux-VERSION.tar.xz \
  /mnt/inputs/linux-VERSION.tar.sign /mnt/inputs/linux-release.asc \
  build/images /mnt/builder/legacy-work /mnt/source-cache/legacy-x86
```

The wrapper selects the fixed approved lock itself. It verifies the cache and
requires `unshare --net` to succeed before signature checks, extraction, or
new work directories. The supplied Buildroot and Linux source signatures and
pins still apply. The cache tool copies verified payloads into a new downloads
directory without links or reuse, then checks both copies. The preserved input
cache is never modified by the wrapper.

Before Make, the wrapper applies the reviewed
`platform/buildroot/host-patches/0001-legal-info-copy-sources.patch` to the fresh,
authenticated Buildroot 2025.02.18 source tree. It requires exact helper hashes
before and after applying the patch with zero fuzz. This makes the upstream
legal-info helper create independent copies of source archives and external
patches, preserving the cache and repository policy's single-link requirement.
A different Buildroot version or helper requires an explicit patch and hash
review; there is no environment override. The complete host-patch directory is
covered by the source-cache policy binding.

The wrapper first exports verified committed application and Buildroot policy
bytes. It compiles the legacy application from that export and records the
actual installed binary through the [legacy application binding](legacy-application-binding.md).
Go testing/compilation, configuration, image compilation, and legal-info Make
commands each run in a new network namespace with no external network interfaces. Namespace failure aborts
the build; there is no online fallback. Missing source dependencies therefore
cannot be fetched from a live mirror. The wrapper checks both original and
staged payloads again after Make, before copying image artifacts to the output
directory. Only bounded, zero-byte `.lock` metadata may have appeared. Changed
or additional payloads fail the build. Failed work remains available for review.

The existing [builder host gate](builder-host.md) and clean execution environment
remain mandatory. Source-cache hashes do not establish upstream authenticity,
a trusted compiler, an immutable builder, complete license fulfillment, or
reproducible image output. A reviewed complete cache, a fresh offline build,
and the resulting ISO's boot/safety validation are still required release
evidence. Fixture tests exercise these gates with synthetic archives and inert
build commands; they do not qualify a real source closure or release image.

# Offline Alpine package inputs

The image builder accepts an optional sixth argument: a previously prepared,
reviewed package cache. A non-development `VERSION` requires that argument.
Development images may still use live repositories, with no release claim.

Each architecture has a separate cache directory containing only:

```text
main/ARCH/APKINDEX.tar.gz
main/ARCH/PACKAGE-VERSION.apk
community/ARCH/APKINDEX.tar.gz
community/ARCH/PACKAGE-VERSION.apk
```

Keep the official signed indexes and original package bytes. Populate the
complete kernel, firmware, bootloader, tool, and dependency closure in a
separate networked dependency-refresh job. Missing packages must fail the
offline build; it must never fetch a replacement from a live repository.
Do not include private keys, work roots, or source checkouts in this cache.

Use the [original-input assembler](alpine-cache-assembly.md) to collect preserved
archives into this layout and detect missing or inconsistent selections.
Assembly checks structure and index membership; trusted signature verification,
dependency closure, and approval remain separate steps.

Generate a review candidate under ignored `build/`:

```sh
python3 scripts/build/alpine_package_cache.py lock \
  --cache /mnt/cache/x86_64 --arch x86_64 \
  --base-lock platform/alpine/base.lock --lock build/x86_64.packages.json
```

Review upstream signatures, versions, licenses, and changes in that candidate.
After review, commit the approved JSON as
`platform/alpine/package-locks/x86_64.json` (or `x86.json`, `aarch64.json`).
The committed lock binds every package and index to its SHA-256 and size and
to the exact Alpine base lock. Cache generation alone is not release approval.

Run the existing image wrapper with a new work directory and the approved
cache. Upstream shell tools require paths composed of letters, digits, slash,
dot, underscore, and hyphen. This applies to the checkout, aports, signing key,
work/output directories, and cache; use absolute paths for external inputs:

```sh
sh scripts/build/build-alpine-image.sh x86_64 build/images \
  /mnt/builder/aports /run/builder/apk-signing.key /mnt/builder/clean-work \
  /mnt/cache/x86_64
```

The wrapper verifies the cache before and after image creation, refuses reused
apk roots, uses only local repositories, and runs both application compilation
and `mkimage` in new network namespaces with no external network interfaces.
Failure to create a namespace stops the build without an online retry.
Normal apk signature verification
still applies. This needs Linux with `unshare --net` permitted and Python 3.11
or newer. The apk wrapper also passes `--no-network` in offline mode.

Install builder tools during the separate networked builder preparation step.
The build requires `apk`, `fakeroot`, `update-kernel`, `mkinitfs`, `mksquashfs`,
`modinfo`, `depmod`, `openssl`, `xorrisofs`, `grub-mkimage`, `mformat`, `mcopy`,
`scanelf`, `lddtree`, `go`, and `python3`, plus the normal Alpine abuild, BusyBox,
archive, and shell tools.
Install the Go version pinned in `.go-version`; the builder must not download
a substitute compiler. The image invocation compiles a fresh application from
a standalone committed source checkout. Existing `build/bin` outputs are never
used. See [modern application binding](modern-application-binding.md) for the
source, binary, and overlay receipt checks and their qualification limits.
In particular, install the `mkinitfs`, `squashfs-tools`, and `kmod` packages
before disconnecting the builder. The wrapper starts a real fakeroot session,
which makes upstream `update-kernel` skip its host-package installation and
cleanup. An additional guard refuses apk mutations outside the build's roots.
The preinstalled host toolchain is a separate reviewed builder input; the
target package lock does not pin those host tools. Non-development image
builds also require the [builder host inventory check](builder-host.md).
No host manifests have been approved yet, and full builder qualification
remains outstanding.

The build-local `scanelf` launcher accepts only the four single-file ELF queries
used by `lddtree`. It removes fakeroot's preload and session identifier only for
that scanner child. Scanelf's own IPC namespace would otherwise separate it
from the fakeroot daemon and stall initramfs dependency discovery. Scanelf keeps
its namespace sandbox; image metadata operations keep their fakeroot session.
Other flags, write/fix operations, and multiple inputs are rejected. A native
Alpine regression compares the real queries and dependency listing under
fakeroot with ordinary scans, using bounded timeouts.

Each invocation starts in a cleared environment with a fixed system PATH,
then creates a fresh tool directory, empty HOME, temporary/cache directories,
and explicit abuild configuration. Source scanning, cache checks, signing-tool
preflight, and post-build steps share this controlled execution boundary.
The image subprocess receives an explicit environment,
including the supplied private/public signing-key paths; the wrapper verifies
that the two keys match. User mkimage plugins, user abuild configuration, and
ambient signing-key overrides are excluded. Development builds use fresh
section directories too, retaining old work and evidence rather than reusing
possibly stale package records.

These entrypoints require a Linux builder and fixed `/usr/bin/python3`;
ambient Python startup files and module paths are excluded. The controlled
execution policy and its remaining qualification limits are described in
[builder host inventory](builder-host.md#controlled-build-execution).

During image creation, the apk wrapper retains successful fetch selections and
the exact installed versions after target-root additions. The installed
database includes dependencies and conditional packages. This also captures
packages streamed directly into bootloader extraction, such as `syslinux`
and `grub-efi`. Capturing these archives is mandatory: failure stops the build.
The SPDX inventory and package list use these consumed inputs, not every
unused package in the supplied cache. SPDX binds the ISO checksum and relates
it to the consumed archives with `GENERATED_FROM`; archive checksums describe
the original package bytes, not a claim that every file in each archive ships
in the ISO. Original license declarations are preserved as comments.

The package-cache verifier has automated tests for modified indexes/packages,
extra or missing files, wrong architectures/bases, and malformed locks.
Additional fixture tests check bootloader recording, exact installed dependency
versions, rejected host mutations, missing captures, and incomplete SBOM input.
The first real offline image build and subsequent boot checks must pass before
the master checklist's cache-consumption milestone is marked complete.

The legacy builder separately emits a deterministic `.iso.spdx` from Buildroot
`legal-info/manifest.csv`. It binds the image digest and preserves original
license declarations as comments without inventing license conclusions. This
inventory does not replace complete notices or corresponding-source fulfillment.

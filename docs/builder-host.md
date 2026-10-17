# Builder host inventory

Non-development image builds require a reviewed manifest of the Alpine host
running the build. This is a first check on host files and installed packages;
it does not qualify a builder or establish the provenance of its contents.
There are no approved host manifests in the repository yet. Release builds
remain blocked until review and the remaining qualification work are complete.

## Prepare a review candidate

Prepare the builder separately from image production, using verified upstream
inputs. Then generate a candidate in ignored storage on that builder:

```sh
env -i PATH=/usr/sbin:/usr/bin:/sbin:/bin LC_ALL=C TZ=UTC \
  /usr/bin/python3 -I -B scripts/build/builder_host_manifest.py candidate \
  --profile alpine-image --base-lock platform/alpine/base.lock \
  --go-version .go-version --manifest build/alpine-image.host-candidate.json
```

For the legacy image use `--profile buildroot-image` and
`--base-lock platform/buildroot/base.lock`. The installed Alpine Go package
must match `.go-version` for both profiles. Modern images compile their application
from committed source during the image invocation, using that pinned compiler.
The [application binding record](modern-application-binding.md) identifies the
source and resulting binary; host qualification remains a separate review.

The inventory records the complete installed APK database digest, package
identities and dependency fields, fixed-path tool resolution, and the selected
runtime and public configuration trees. File digests, sizes, ownership,
permissions, symlink targets, and hardlink groups are compared. It also binds
the base lock, Go version policy, and inventory policy source.
The bounded profile rejects nonempty extended attributes, including file
capabilities and POSIX ACLs; attribute values are never collected. Metadata
inspection must be supported on the production Linux host. Wget and GnuPG's
fixed global configuration files and the x86, x86_64, and AArch64 musl loader
path files are observed even when absent, so later appearance changes the
inventory. Supported additional musl loader paths are inventoried too.

Candidate creation does not execute installed tools or source configuration.
It excludes user homes, work directories, private key storage, and host kernel
module/firmware trees. Unsupported special files, links escaping the covered
trees, and selected unsupported execution overrides fail the check. These
boundaries deliberately leave further builder qualification work outstanding.
Review candidates locally before publication: even a file inventory can reveal
unintended names or installed software.

After independent source, package, configuration, and privacy review, the
approved location is `platform/builder-hosts/PROFILE-HOSTARCH.json`, for example
`platform/builder-hosts/alpine-image-x86_64.json`. The architecture names the
builder host, not the target ISO. Never create an approved file merely by
renaming a candidate to make a failed release build proceed.

## Controlled build execution

The public Go, Alpine, and Buildroot build scripts start their build bodies in
a new shell with a cleared environment and the fixed system tool path
`/usr/sbin:/usr/bin:/sbin:/bin`. There is no caller flag to skip that boundary.
Source scanning and Python build helpers use isolated Python execution. Make,
compiler, shell, OpenSSL, Git, and Python overrides from the caller are removed
before the tools run. Image preflight checks use an inaccessible user home;
build stages use fresh HOME, temporary, configuration, and cache directories
under their work root. Existing work and evidence are retained.

The image scripts preserve their documented path arguments. Buildroot also
accepts `HHPT_JOBS` as a canonical integer from 1 through 64, defaulting to 2;
other build configuration comes from reviewed source and the selected host.
Source timestamps come from the pinned aports commit for Alpine images and
the source checkout commit for Buildroot. Caller timestamps are ignored.

`build-go.sh` selects one absolute Go executable from the incoming PATH, then
uses that same executable with cleared configuration. Its reported toolchain
version must equal `.go-version`; automatic toolchain switching, module
downloads, workspace discovery, and persisted Go settings are disabled. Tests
and compilation run in the script's source checkout regardless of the caller's
working directory. Relative output paths are relative to that checkout;
absolute output paths remain explicit. Each invocation receives fresh build
and module caches. Checksum sidecars contain only the artifact filename.

The five utility targets retain their CPU baselines: x86 uses SSE2, legacy x86
uses soft float, x86_64 and Windows x86_64 use AMD64 v1, and AArch64 uses ARMv8.0.
These shell entrypoints require a Linux build host with `/usr/bin/python3`.
CRLF-terminated version files are accepted, but embedded version lines are not.
Install the matching Go toolchain during reviewed builder preparation; a
different installed compiler must fail rather than download a replacement.

## Verification and limits

The image wrappers select the approved path themselves and verify the actual
host before build work and after image creation. Missing or changed inputs
block a non-development build. Production verification requires Linux and
does not accept an alternate root. `candidate --root` exists for isolated test
fixtures and cannot substitute for production verification.

Only canonical `MAJOR.MINOR.PATCH-dev.NUMBER` versions may use an unqualified
builder and retain that status. Embedded `-dev.` text in another prerelease
or in build metadata does not grant that exception.
Do not relabel their outputs as releases. The host manifest also does not
replace the [target package cache lock](package-cache.md).

Matching files do not prove a trusted kernel, immutable filesystem, complete
semantic configuration closure, reproducibility, or authentic upstream inputs.
Changes made and reverted between checks are outside this comparison. The
remaining work includes qualifying the full execution environment, pinning
and authenticating its preparation inputs, and validating fresh offline builds
and their resulting images. Cleared environments and matching compiler versions
do not authenticate tools or configuration on disk. The modern application
binding checks committed source, fresh compilation, and the bytes copied into
the overlay; independent extraction of the finished ISO and approval of its
source/compiler provenance remain required. The legacy wrapper requires a reviewed
[offline source cache](buildroot-source-cache.md) and network-isolated Make
phases for non-development builds. Its complete source closure and fresh
offline image still need qualification; seeding the Linux archive alone does
not provide those guarantees. The full builder-toolchain checklist item remains
open until that work is complete.

## Validation evidence

Native Alpine validation exercised 34 inventory tests, seven wrapper
integration tests, and the 28 existing build tests. The cases include indirect
library changes, missing and extra files, private-path and symlink boundaries,
FIFO rejection before data access, malformed manifests, version exceptions,
and failure propagation before and after build work. The wrapper fixtures use
inert build tools; they do not produce release images.

A candidate generated from an actual Alpine builder matched on verification,
while a changed manifest and a legacy host Go version differing from the
repository pin were rejected. The candidate and detailed lab evidence remain
local. These checks validate the gate's behavior; they do not approve that
builder or complete the fresh offline image-build milestone.

Additional copied-source fixtures exercise the real environment boundary with
inert tools. They cover inherited configuration overrides, compilation from a
foreign working directory, wrong installed Go versions, all five target
baselines, fresh caches, source-scan and test failures, and checksum path
privacy. These tests do not qualify the selected compiler or produce release
images.

The stricter inventory profile passed 42 native Alpine tests with no skips.
These include both-profile Go requirements, changed global configuration,
previously absent configuration appearing, added extended attributes and ACLs,
private link targets, and link replacement with a FIFO during metadata checks.
Passing these fixtures does not approve a candidate host manifest.

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
must match `.go-version` for that profile. The modern image profile consumes
an application binary built separately; its host inventory does not attest
to how that binary was produced.

The inventory records the complete installed APK database digest, package
identities and dependency fields, fixed-path tool resolution, and the selected
runtime and public configuration trees. File digests, sizes, ownership,
permissions, symlink targets, and hardlink groups are compared. It also binds
the base lock, Go version policy, and inventory policy source.

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
and their resulting images. In particular, Buildroot/Go compiler and Make
environment selection still needs qualification beyond this file inventory.
The full builder-toolchain checklist item remains open until that work is
complete.

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

# Initial alpha build inputs

`0.1.0-alpha.1` is the first qualification target. The committed inventories
approve these exact inputs for fresh offline builds. They do not approve an
image or authorize publication. All four completed ISOs, companion utilities,
security and license checks, boot evidence, and release approval are required
before a GitHub download release.

## Host baseline

The two [x86_64 host manifests](../platform/builder-hosts/README.md) cover the
Alpine and Buildroot image profiles. They bind 245 packages and 15,909 runtime
paths, including Go 1.27.2. Their provenance records the authenticated bootstrap
ISO, original signed indexes and APKs, exact source recipes, native inventory
tests, and a separate offline installation replay. Replay differences are
documented observations; the build gate accepts no such differences.

The baseline is a bounded comparison of host inputs. It does not establish
measured boot, immutable execution, an independently bootstrapped compiler,
or reproducible output. Host comparison must pass before and after each build.

## Modern target caches

Each lock preserves the original signed main/community indexes and APK bytes.
No index was regenerated and no package was substituted across architectures.

| Target | APKs | APK bytes | `linux-lts` | Committed lock |
| --- | ---: | ---: | --- | --- |
| x86_64 | 231 | 1,046,555,620 | 6.18.56-r0 | [x86_64.json](../platform/alpine/package-locks/x86_64.json) |
| x86 | 231 | 979,413,788 | 6.18.57-r0 | [x86.json](../platform/alpine/package-locks/x86.json) |
| AArch64 | 229 | 1,022,338,638 | 6.18.57-r0 | [aarch64.json](../platform/alpine/package-locks/aarch64.json) |

All 691 APKs passed full byte hashes, compressed control checksums, signed-index
membership, package metadata and payload checks, native signature verification,
and independent RSA verification. Public keys were checked against the
authenticated bootstrap ISO's `alpine-keys` package. Each fresh offline install
probe produced exactly its resolved package set, with package scripts and
commit hooks disabled. Those probes do not prove the image builder's complete
conditional dependency closure.

The frozen indexes select different kernel revisions for x86_64 and the other
targets; each architecture retains its own reviewed selection. All three
selected kernels contain gzip-compressed modules and require no Zstandard
module decompressor in the host baseline. Fresh image builds must use these
locked inputs and fail if anything is missing. See [package cache use](package-cache.md).

The aports revision remains pinned by `platform/alpine/base.lock`. Its upstream
tag and commit are unsigned. Authentication uses independently observed
official HTTPS refs, raw Git object verification, and the committed script
SHA-256 pins; it does not rely on a nonexistent Git signature.

## Legacy source cache

The [legacy lock](../platform/buildroot/source-cache-lock.json) binds 29 source
archives totaling 339,789,441 bytes and 21 build-policy files. Buildroot
2025.02.18 and Linux 6.12.112 retain their pinned source hashes and verified
upstream signatures. Archive hashes were checked against the authenticated
Buildroot definitions and project kernel pins. Review also checked 49 declared
license-file hashes and the three pinned Linux license-file paths.

The source-policy review includes the fresh application binding and actual
installed-file receipt. An offline prerequisite check found all seven required
Perl modules. The complete configuration, compiler build, image creation, and
`legal-info` stages must still succeed from fresh work directories. A source
inventory and license-file checks do not fulfill all distribution obligations.
See [legacy source cache use](buildroot-source-cache.md).

## Validation still required

The full native build fixture suite passed 178 tests with no skips. Separate
offline compilation from the committed source passed for all four application
targets, including independent inspection of embedded product versions and CPU
policies, the actual modern overlay copies, and the real legacy post-build
installed executable. Earlier binaries remained untouched and excluded.

These application checks used the preceding development version. The alpha
images must be built afresh from the alpha source commit, then independently
extracted to verify the shipped executable. Image boot, runtime safety, report
handling, dependency vulnerability review, complete notices and corresponding
source, and protected release publication remain open. VM evidence does not
complete physical-hardware qualification.

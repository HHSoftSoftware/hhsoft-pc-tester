# Assemble original Alpine package inputs

`scripts/build/assemble_alpine_cache.py` assembles an offline cache from original
signed APKINDEX archives and original APK files. It checks archive structure,
index membership, package metadata, control checksums, and payload hashes.
It does not download, extract, or execute packages, regenerate indexes, verify
signature trust, or establish dependency closure.

Run on Linux with Python 3.11 or newer. Supply the original `main` and
`community` indexes for one architecture: `x86_64`, `x86`, or `aarch64`. Supply
one or more flat directories of APK files; content-addressed filenames are
accepted. Every immediate directory entry must be a regular file. Unrelated
regular files without the `.apk` suffix are ignored; subdirectories, symlinks,
and special files are rejected. Input and output parent paths must not contain
symlinks. Symlinks and installation scripts inside package archives remain
unchanged archive bytes and are never extracted or executed.

Create the report's parent directory beforehand. Both the cache output and
report must be new paths. For example, from the source checkout:

```sh
env -i PATH=/usr/sbin:/usr/bin:/sbin:/bin LC_ALL=C TZ=UTC \
  /usr/bin/python3 -I -B scripts/build/assemble_alpine_cache.py \
  --arch x86_64 \
  --main-index /mnt/inputs/main/APKINDEX.tar.gz \
  --community-index /mnt/inputs/community/APKINDEX.tar.gz \
  --packages /mnt/inputs/packages \
  --output build/cache-x86_64-candidate \
  --report build/cache-x86_64-assembly.json
```

Repeat `--packages DIRECTORY` for additional flat input directories. Add
`--require NAME=VERSION` for each exact package identity in the intended input
set. Requirements do not resolve dependencies. An index may list many packages
that the build does not consume; their absence alone is not reported as a gap.

Repository membership comes from the supplied indexes, independently of input
filenames. Exact duplicate archives are deduplicated. Different bytes for one
name/version, ambiguous membership in both repositories, conflicting canonical
filenames, or mismatched metadata block assembly. Target-specific indexes may
contain packages whose internal architecture is `noarch`; each target cache
still uses its own indexed bytes.

Successful output contains unchanged original files at:

```text
main/ARCH/APKINDEX.tar.gz
main/ARCH/NAME-VERSION.apk
community/ARCH/APKINDEX.tar.gz
community/ARCH/NAME-VERSION.apk
```

The JSON report records canonical output paths, sizes, SHA-256 digests, package
identities, duplicate counts, missing required identities, and unindexed input
archives. It does not record source paths. Exit status determines the result:

| Exit | Result |
| --- | --- |
| `0` | All supplied APKs match an index and all explicit requirements are present; a fresh cache and report were written. |
| `2` | Required archives are missing or supplied APKs are unindexed; only an incomplete report is written. |
| `1` | Invalid, changed, unsafe, unsupported, or ambiguous input blocked assembly. A copy failure can leave this invocation's partial output and an empty report; use new paths for a retry. |

The supported format is signed APK v2: two gzip members for an index and three
for a package. The parser accepts timestamp-only PAX metadata and rejects
metadata path, size, link, and sparse overrides. Unknown formats fail closed.
Limits include 32 package directories, 10,000 entries per directory and
10,000 input APKs overall, 512 MiB per supplied
APK, 16 GiB of compressed package inputs, 2 GiB of expanded payload per APK,
and 32 GiB of total archive expansion. Each index is limited to 16 MiB
compressed, 64 MiB of index text, and 50,000 records. Larger unselected packages
listed in an index do not consume the supplied-archive size allowance.

Every report explicitly records `signature_verification: not_performed` and
`dependency_closure: not_established`. A successful assembly is a preparation
result. Independently verify the original indexes and packages with trusted
Alpine signing keys, review their provenance and intended versions, and prove
the image's complete dependency set with the offline build. Then follow the
[package-cache review and lock workflow](package-cache.md). The assembly report
is not an approved package lock, builder qualification, or release approval.

# Builder input baseline

These x86_64 manifests record the reviewed host inputs for the `alpine-image`
and `buildroot-image` profiles. Each binds 245 installed packages, 15,909
covered runtime paths, tool resolution, the APK database, and project policy.
The build wrappers compare the actual host strictly before and after a build.
This is an input baseline, not full builder qualification or release approval.

| Manifest | SHA-256 |
| --- | --- |
| [alpine-image-x86_64.json](alpine-image-x86_64.json) | `ec82f960563044597dd125ec2abc9b26e69df3c30219eadc3161285c7388f923` |
| [buildroot-image-x86_64.json](buildroot-image-x86_64.json) | `1ad1e578c5a7672ca1713995d60b77c91bf21cefd0b7f80d313776031cdaaa1c` |

Both require inventory policy SHA-256
`fe082782fb4d30fd44be1d63476f70a37a3b875842fcdee705e30ef86d0fcd00`
for `scripts/build/builder_host_manifest.py`. Preserve these manifest bytes;
a different policy or host requires a separately reviewed baseline.

[provenance.json](provenance.json) records the exact preparation inputs:

- Alpine extended 3.24.2 x86_64 ISO, its kernel and boot-file hashes, and the
  original APK verifier and public keys obtained from that ISO.
- The ISO's official detached signature and release public key, verified
  offline over the exact ISO bytes with primary fingerprint
  `0482D84022F52DF1C4E7CD43293ACD0907D9495A`.
- All 245 APK identities, sizes, SHA-256 hashes, signed-index membership,
  declared licenses, project URLs, and exact-revision source-recipe locations.
- The original signed v3.24 main/community indexes and the edge/community
  index authenticating only `go-1.27.2-r0.apk`. The other 244 packages come from
  v3.24. No edge repository was enabled during installation.
- Go 1.27.2 source, the exact Alpine recipe, and its three patches, including
  source SHA-512 values checked against the recipe. This does not establish an
  independently bootstrapped or reproduced compiler binary.

Reproduction starts from the pinned ISO and verifies the exact signed indexes
and APK bytes before installing the listed `name=version` package set offline.
The Go APK is an explicit input. The ten package upgrades from the ISO baseline
are listed in the provenance record. Upstream URLs are retrieval locations;
missing bytes or changed hashes must stop preparation, not select replacements.
Preserve original signed indexes instead of generating unsigned substitutes.

The signature check used a fresh keyring with key retrieval disabled. Its
timestamp is a signer assertion; offline revocation checks cover only the
pinned public key. The separately pinned aports Git tag and commit are unsigned
and are checked against official HTTPS refs, raw object hashes, and the
committed build-script digests.

Native validation passed all 42 inventory tests with zero skips. A separate
fresh-root installation of the same 245 packages passed its offline runtime
check and reported `go version go1.27.2 linux/amd64`. Package records and tool
resolution matched. Comparison of the covered runtime paths found exactly
three bookkeeping differences:

| Path | Baseline compared with replay |
| --- | --- |
| `/etc/apk/repositories` | The baseline has its optical repository entry; replay used a separate explicit repository list. |
| `/lib/apk/db/installed` | Only the `abuild` record for `var/cache/distfiles` differs: group 300 in the baseline, group 0 in replay. |
| `/lib/apk/db/lock` | The baseline has an empty mode-0600 lock file; replay has none. |

The provenance record preserves the exact file hashes, sizes, and metadata for
these differences. Signed package contents support the group-name and numeric
fallback explanation; exact group-resolution timing is inferred because its
diagnostic was unavailable. Neither APK database was normalized or modified.
These are replay observations, **not exceptions in the build gate**: builds
must match the complete manifest, including its exact database bytes. Nonempty
extended attributes are rejected.

The replay shared the running kernel. No measured boot, immutable execution,
or independently booted replay kernel is established. Changes reverted between
comparisons are outside the gate. Fresh source-bound offline images, independent
finished-image extraction, boot and safety checks, complete notices and
corresponding-source fulfillment, and release approval remain separate checks.

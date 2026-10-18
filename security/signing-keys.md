# Project signing keys

HHSoft Software uses separate OpenPGP keys for source release tags and lab
evidence. Both keys use Ed25519 and the project email `info.hhsoft@gmail.com`.
Only public keys are distributed here.

| Purpose | Public key | Full primary fingerprint |
| --- | --- | --- |
| Annotated release tags | [release-signing-key.asc](release-signing-key.asc) | `762C2C2E975050765F43DED57DB7FAF292F558BC` |
| Completed lab evidence | [lab-signing-key.asc](lab-signing-key.asc) | `C0A8D8D1E1002A0DEA6C64DA051891C4742EA26B` |

The keys were created on 2026-10-18 UTC and expire on 2028-10-17 UTC.
Inspect an exported public key before importing it:

```sh
gpg --show-keys --with-fingerprint security/release-signing-key.asc
gpg --show-keys --with-fingerprint security/lab-signing-key.asc
```

The release pipeline requires the corresponding full primary fingerprint in
`RELEASE_SIGNING_KEY_FINGERPRINT` or `LAB_SIGNING_KEY_FINGERPRINT`. It verifies
the signature against both that fingerprint and the committed public key.
The release key also needs registration with the HHSoftSoftware GitHub account
and a verified project email for GitHub's tag-signature check.

A valid signature identifies the signer and protects the signed bytes. Release
approval still requires the complete image, source, privacy and validation
checks in [publication readiness](../docs/publication-readiness.md). The public
keys alone do not enable publication or attest that a candidate passed review.

Keep private keys and recovery material outside source, images and Actions.
Review key replacement or expiry changes through a pull request, independently
verify the new full fingerprints, and update the repository variables together.

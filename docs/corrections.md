# Signed display corrections

`hhpt correction create`, `verify`, and `view` handle a separate signed operator
assertion about public System display metadata. The comparison shows original and
operator values in different columns. It preserves the original `.hhpcr`, component
IDs, HMACs, fingerprint, plan, test evidence, and original HTML/PDF views.

The full Windows amd64 and 386 suites each passed 12,300 tests/subtests with 18
retained skips, including all 148 initial correction cases. Five additional parser
budget cases passed separately on each architecture without production changes.
Formatting, vet, and Linux amd64/386/AArch64 application and updated correction-test
builds passed. Linux artifacts were compiled only; Linux runtime, installed-image,
physical-PC pilot, and release gates remain open.

## Supported values

One to four unique fields on the single System component may be supplied:

| Field | Meaning |
| --- | --- |
| `vendor` | Public system vendor name |
| `product_name` | Public system model name |
| `product_family` | Public product family |
| `product_version` | Public product version label |

No System, multiple Systems, a System ID reused by another component, and duplicate
supported attributes are rejected. An absent attribute is shown as **not observed**,
separately from an observed empty value. New values are nonempty, trimmed text of at
most 256 UTF-8 bytes, without control or format characters. Duplicate fields and
exact no-op changes are rejected. Values are not silently normalized.

Use only public display metadata and public signer labels. There is no operator
identity or free-text reason field. Serials, UUIDs, network addresses, component
IDs/types, HMACs, fingerprints, and test outcomes are not correction targets.

Existing inventory and linked-retest profiles keep their original readers.
Corrections require schema-1 public inventory, source session/System IDs of at most
256 UTF-8 bytes each, and at most 1,024 UTF-8 bytes in each supported original
attribute. Literal U+FFFD and valid Unicode pairs remain correctable. Unpaired JSON
surrogate escapes are rejected before decoding. Original controls and non-ASCII
characters, including in identifiers, appear as visible quoted escapes without
changing the original signed bytes.

## Create

Prepare `changes.json`:

```json
{
  "schema": "hhsoft.pc-tester.display-correction-request/v1",
  "changes": [
    {"field": "product_name", "value": "Service Model 17"},
    {"field": "product_version", "value": "Revision B"}
  ]
}
```

Use a reporting signing key provisioned for this purpose. Private-key files contain
exactly 64 raw Ed25519 bytes with consistent seed/public halves; public-key files
contain exactly 32 raw bytes. Protect private keys. The explicit public signer label
accepts 1-128 ASCII letters, digits, periods, underscores, colons, or hyphens.

```text
hhpt correction create -report original.hhpcr -source-public-key report-public.key -source-key-id report-key -changes changes.json -signing-key correction-private.key -key-id correction-key -output correction.json
```

Creation fully verifies the acquired original and binds its canonical bundle
SHA-256, signed manifest digest, session ID, and System ID. The addendum has a new
UUIDv4 and a signature domain distinct from report envelopes. It makes no timestamp
or ordering claim. Changes are sorted before signing.

Requests allow JSON whitespace and member reordering. Unknown, duplicate, missing,
and incorrectly spelled members are rejected. Requests are limited to 8 KiB and
canonical addenda to 16 KiB. Do not edit or pretty-print a signed addendum;
verification requires its exact canonical encoding.

## Verify and compare

Original-report and addendum trust are independent. Trusting one signer does not
authorize the other.

```text
hhpt correction verify -report original.hhpcr -source-public-key report-public.key -source-key-id report-key -addendum correction.json -addendum-public-key correction-public.key -addendum-key-id correction-key
hhpt correction view -report original.hhpcr -source-public-key report-public.key -source-key-id report-key -addendum correction.json -addendum-public-key correction-public.key -addendum-key-id correction-key -output comparison.html
```

`view` verifies both signatures and their exact relationship before rendering. Its
HTML is an unsigned derived comparison capped at 64 KiB. Retain the original and
signed JSON, and regenerate the view from that pair; detached HTML is not signature
evidence. Ordinary `hhpt verify` continues to verify reports and does not implicitly
apply addenda.

For an intentionally self-issued original, replace its public-key/key-id pair with
`-source-self-issued`. To explicitly accept the addendum's embedded public key,
replace its pair with `-addendum-self-issued`. Each opt-in is separate. A self-issued
signature establishes consistency with an embedded key, not independent signer
identity; output labels that reduced assurance. Mixing self-issued and external-key
flags for the same artifact is rejected.

Use `hhpt correction create -h`, `verify -h`, or `view -h` for syntax. Trust key IDs
retain the report command's surrounding-whitespace trimming. Input paths are used
as supplied; creation labels are not silently normalized.

## Output and remaining scope

Creation and viewing require `-output`; `-` selects caller-owned stdout. Files use
exclusive creation and do not overwrite an original or existing addendum/view.
Success requires complete writes and successful owned-file closure. There is no
rename transaction, filesystem-sync, power-loss durability, or rollback promise.
Failure may leave an incomplete new file; preserve it and choose a new destination
for a later attempt.

Inputs are closed before parsing. Observed acquisition/cleanup, cancellation,
verification, signing, and output failures prevent success. Correction diagnostics
are fixed and do not echo provider errors, keys, or request values. Cancellation is
checked at operation boundaries, without promising bounded interruption of every
synchronous verification or filesystem operation.

A correction is an operator assertion, not a new hardware observation or test. This
workflow handles one explicitly selected addendum. Complete history, latest-wins
ordering, supersession, revocation, asset identity, and chain of custody remain
separate work.

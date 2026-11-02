# Requirements-based validation

The master checklist records product claims. Tests must establish the intended
claim and its failure boundaries, not merely reproduce the current code's
output. A completed implementation item is not physical-hardware qualification.

At each feature milestone and before publication:

1. Compare the operator guide, safety rules, and checklist with the actual code
   paths. Inspect TODOs, stubs, ignored errors, default success values, and
   unsupported branches. An absence of TODO comments is not proof of completion.
2. State the intended invariant before choosing a fixture. Include the missing,
   contradictory, substituted, and zero-work cases that must not pass.
3. Exercise relevant entry points, including direct capture and finalization.
   A helper test alone cannot establish that every signing path calls it.
4. Test external tool behavior with the real tool when its argument semantics
   affect correctness. Keep these integration fixtures isolated and offline.
5. Record what ran, what was skipped, and what still needs VM or physical
   evidence. Check off the item only after its applicable validation passes.

## Bounded Exterior Inspection

### Current four-criterion implementation

Exterior definition 2 records enclosure integrity, visible hinge/mount condition,
port exterior condition and surface finish as four required fixed fields. Each
starts unobserved. A manual pass requires three explicit no-issue observations
and a no-issue hinge observation or an explicit no-hinge not-applicable answer.
Hidden or uninspected hinges remain unobserved. Any observed issue stays adverse.
No free-text condition, operator identity or automatic hardware pass is collected.

The real `inspect-exterior-condition` command emits `exterior-condition.json`
with fixed operator-reported/visual-only scope. One prompt lists all four
criteria, and one owned batch accepts four bounded LF/CRLF lines within one
shared 45-second deadline. Bare CR is incomplete; clean completed prefixes
survive a stop between lines. Prompt failure cannot admit observations, and
final owner cleanup must succeed before output or signing.

Current schema-3 plans select exterior definition 2. Quick v14, Standard v15
and Extended v16 keep fourteen/fifteen/fifteen workers. Actual observations
require their matching current journal; exact fixed error/aborted nonexecution
notes expose no conditions and retain their limited journal-free compatibility.
Malformed present evidence fails before fallback. All-profile signed positives,
independent malformed signed bundles, HTML/PDF views, no-view bundles, copied
accessors and current/history plan regressions are included in the source tests.

Corrected current host validation passed all 27 commands: 11,891 test/subtest
passes and 18 retained platform skips in the full Windows amd64 suite; 593
focused passes and no skips on Windows 386; formatting and vet; and 21 Linux
compile-only builds. The Linux outputs were three application and eighteen
test binaries across amd64, 386 and arm64. All seven postchecks passed. The
tested candidate contained 726 source files, including 710 Go files. This host
run did not execute Linux binaries or installed commands.

A preceding full-run attempt failed fixture regressions and remains
unqualified. Subsequent selected native input-owner and broker fixtures passed on Linux
amd64/386: 1,296 outcomes across 12 streams, 162 root executions and
1,134 declared parent intervals, with no failures or skips. Nine ownership
cases, 32 archive cases, six postchecks and cleanup also passed. One separate
synthetic standalone inspection CLI exercise passed. Installed CLI, full Linux
suite, ARM runtime and physical qualification remain open.
The older v1 results below and the later combined 580-outcome Linux cohort
retain their original source scope. That combined cohort used 714 source files,
698 Go files and v11/v12/v13 journals; it does not qualify inspection v2.
Installed commands/images, physical observations, hinge/port function, cooling
noise, battery condition, the ten-PC pilot and release approval remain open.

### Historical v1 implementation and qualification

The implemented check records a finite operator assertion about safely visible
exterior enclosure surfaces without moving or opening the device. It does not
verify physical truth or complete chassis coverage. D reports cracks, broken
or missing enclosure pieces, or deformation; N reports none of those defects
observed on inspected surfaces. I, empty, invalid or incomplete input cannot
establish a pass. A completed defect stays adverse after a clean stop.
Hard prompt/input errors retain error status.

The consumed path includes the standalone Linux console command, Quick-test
menu and coordinator, finalization, signed bundles without optional views,
HTML/PDF rendering and linked retesting/comparison. Plan schema 3 makes the
row applicable to an operator rather than a discovered component. The later
v1 touchpad stage used Quick v9/thirteen workers and Standard v10/fourteen,
with 22 catalog rows plus run integrity. Its modern Quick selection offered
thirteen checks and legacy x86 eleven. Direct capture retained its seven-check
subset. Those historical layouts retain their version-specific read rules;
compatibility does not admit stale plans for current execution.
Storage sampling remains inactive.

Observation time starts at prompt attempt and ends at the response or stop
decision, capped at the effective deadline including a shorter parent deadline.
Preparation and final owner cleanup are excluded. Decisions at or after the
deadline cannot pass, independent of delayed context notification. This is
bounded evidence, not a hard return-time guarantee. Queued hard input faults
survive cancellation; the actual final owner receipt must still succeed before
output or signing. Cleanup uncertainty retains exit 12.

The complete integrated source passed the full Windows host run: **5,942
test/subtest passes across 30 packages**, **17 test skips** and **one skipped
package**. Formatting, format checking, vet and Linux amd64/386/arm64 application
cross-builds passed, along with all eight postchecks. Earlier partial runs are
not used as qualification of this complete source.

The selected native Linux amd64/386 fixture cohort passed **4,580 named
outcomes across 16 suites**, with **836 root executions** and no failures or
skips. Each architecture passed exactly **2,290 required names and 418 roots**
across the command, exterior-inspection, plan, session-evidence, application,
bundle, rendering and comparison packages. These totals include retained
regressions; they are not counts of new inspection tests.

All 43 selected Linux-only names and four names skipped on Windows passed on
each native architecture. The real Linux broker and owner fixtures therefore
have native execution evidence. Nine ownership cases, 32 archive cases and all
six postchecks passed, with cleanup confirmed. Independent actual-evidence
review confirmed the exact named lifecycles and the source-declared group
ordering. Independent source review also closed the finite tuples, malformed
no-view signed bundles, real coordinator/finalizer composition and exact
parent-pair verification.

No installed-command or race scenarios ran in this cohort. It does not qualify
actual media access, kernel-close/fsync fault injection or output durability.

These tests use controlled assertions and fixtures. They do not establish
installed-command operation on new images, truthful or complete physical
inspection, hinge or port function, cooling noise, battery condition, output
durability or broad hardware coverage. The required ten-PC pilot, all four
bootable images, release safety checks and release approval remain open.
This bounded implementation does not close the broader inspection checklist.

## Power-Supply Inventory

The implemented Linux collector enumerates every validated supply entry and
assigns one-based, nonpersistent `power_supply:N` IDs. It exports no raw entry
name, serial or derived stable identifier. A missing class and a successfully
empty class both mean no reported rows, not proof of absent hardware. Every
enumerated entry retains a row even when all fields are unavailable.

Each field has a `<field>_state` attribute. The finite states are `reported`,
`unavailable`, `unsupported`, `malformed` and `out_of_range`; only `reported`
has a value. Exact documented `Unknown` labels are reported values.

| Public field | Admitted reported value |
| --- | --- |
| `supply_type` | `Unknown`, `Battery`, `UPS`, `Mains`, `USB`, `USB_DCP`, `USB_CDP`, `USB_ACA`, `USB_C`, `USB_PD`, `USB_PD_DRP`, `BrickID`, `Wireless` |
| `scope` | `Unknown`, `System`, `Device` |
| `present` | 0 or 1; unavailable does not imply 1 |
| `online` | 0 offline, 1 fixed supply online, 2 programmable supply online |
| `status` | `Unknown`, `Charging`, `Discharging`, `Not charging`, `Full` |
| `capacity_percent` | Integer from 0 through 100 |
| `cycle_count` | Integer from 1 through 2,147,483,647; zero is unavailable |
| `energy_full_uwh` | Signed 32-bit integer, micro-watt-hours |
| `energy_full_design_uwh` | Signed 32-bit integer, micro-watt-hours |
| `charge_full_uah` | Signed 32-bit integer, micro-amp-hours |
| `charge_full_design_uah` | Signed 32-bit integer, micro-amp-hours |

Successful complete scalar reads are parsed before whitespace normalization.
At most one terminal LF is removed. Enum text must be nonempty printable ASCII
without edge spaces; exact labels are reported, while every other clean label
is unsupported without being echoed. Numeric fallback, case variants and repeated
internal spaces therefore do not become accepted enum values. Empty text,
controls, CRLF and non-ASCII bytes are malformed. Integers use canonical decimal
syntax without `+`, leading zeroes or `-0`; syntax errors are malformed, and
signed-32-bit overflow or a field-domain violation is out of range. Negative,
zero and full-greater-than-design energy or charge observations are retained
within that integer range, without a health ratio or unit conversion.

Ordinary scalar open/read errors discard all returned bytes and become
unavailable. This does not distinguish missing attributes, unavailable driver
data, hotplug or I/O failure. Existing bounds, malformed-protocol, checked-close,
cancellation and directory-error handling remain mandatory. The scalar byte
limit remains 65,536; this adds no total collection deadline or guarantee that
a blocked kernel call returns promptly. Sysfs reads may trigger driver hardware
queries. The observations are separate samples, not an atomic power state.

The new power-specific volatility rule covers only the eleven values and their
eleven states. Existing shared exclusions and other types retain their prior
rules; unknown attributes do not gain a blanket exclusion. Power identity stays unavailable; ordinals
can shift and same-count replacement can be undetectable. No-supply payload bytes,
identified-component fingerprints and historical session contracts remain
unchanged. That qualification used 22 catalog rows and v7 with twelve workers;
storage sampling remains inactive.

**Qualification completed for the bounded software scope:** the complete
collector, plan and downstream composition passed 6,239 host test/subtest
outcomes across 30 packages, with 17 platform test skips and one skipped
package. Formatting checks, vet, Linux amd64/386/arm64 application builds,
all eight commands and all eight postchecks passed. All 30 changed-file roots
and 297 required names passed once.

Selected native Linux amd64/386 fixtures passed 3,100 named outcomes across
14 suites and 582 root executions. Each architecture passed all 1,550 names
and 291 roots, with no failures, skips, extra names or duplicates. The four
Linux-only descriptor, normal-link and FIFO roots and the collector fixture
skipped on the Windows host passed on both architectures. All nine ownership
cases, 32 archive cases and six postchecks passed, with confirmed cleanup.
These results qualify the selected software fixtures, not installed commands
or physical power hardware. Fixture tests must cover real collection through signing, no/both views,
linked verification and comparison, including private-name canaries and
correctly re-signed forged identities after valid prerequisites.

This inventory does not test battery health, wear, capacity accuracy, AC/battery
transitions or physical continuity. Unsupported-platform execution, installed
commands, rebuilt images, output durability, the ten-PC pilot and release
approval retain their separate gates.

## Firmware and TPM Inventory

The finite Linux producer reads three fixed EFI metadata roles and only
`tpm_version_major` for every validated TPM class entry. It adds no worker,
catalog row, profile, evidence schema or identity algorithm.

| Component and field | Admitted reported value |
| --- | --- |
| `firmware_security`: `uefi_platform_bits` | 32 or 64 bits |
| `firmware_security`: `secure_boot` | 0 or 1, preserved as the reported flag |
| `firmware_security`: `setup_mode_flag` | 0 or 1, independent of SecureBoot |
| `tpm`: `version_major` | 1 or 2, the exposed interface's major version |

Every field has a `<field>_state`: `reported`, `unavailable`, `malformed` or
`unsupported`. Only `reported` includes a value. Width and version use canonical
unsigned ASCII decimal, with at most one terminal LF. Empty input, leading zeroes
on a multi-digit value, signs, spaces, CRLF, controls and non-ASCII bytes are
malformed. Other canonical numeric tokens are unsupported and are not echoed.
There is no whitespace trimming or case folding before validation.

Each EFI flag must be exactly five bytes: a four-byte little-endian attribute
word equal to 6 followed by one byte equal to 0 or 1. A different length is
malformed; another attribute word or flag byte is unsupported. These are the
product's finite admission rules, not an assertion that every firmware exposes
these files. SecureBoot 1 is a reported firmware flag, not proof of an effective
trust chain. SetupMode 1 alone is not a complete firmware-mode classification;
the literal SecureBoot 1/SetupMode 1 pair is retained without reconciliation.

The `firmware_security:uefi` row appears if at least one EFI read completes,
including a completed malformed or unsupported value. All three unavailable
reads produce no row. Every enumerated TPM entry produces a row even if its
version is unavailable. Missing and empty enumeration both mean no reported
data, not physical absence. One-based `tpm:N` IDs follow validated sorted
enumeration and are nonpersistent; no raw entry name, serial, endorsement
identity, PCR, certificate/key contents or provider error string is exported.
Both types retain unavailable identity. Ordinals can shift and an
indistinguishable same-count replacement can remain undetectable.

Ordinary scalar open/read errors discard all returned bytes, including favorable
bytes returned with an error, and become unavailable. They do not distinguish
missing attributes, permission denial, hotplug or other ordinary read failures.
Existing malformed-protocol, 65,536-byte bound, checked-close, cancellation and
directory-error rules remain mandatory. This adds no total collection deadline
or guarantee that a blocked call returns promptly. Fields are separate samples,
not an atomic firmware state, and metadata collection issues no TPM commands.

All finite attributes/states and rows remain in the frozen configuration graph
and normal comparison. There is no power-style volatility exclusion for these
types. Changes require a new plan for future execution; existing finalization
retains a signed failed-integrity diagnostic with exact component changes.
Unavailable identities are not upgraded by matching ordinals. That inventory
increment preserved no-observation payload/view compatibility, existing
identified components, Quick v7, explicit Standard v8, historical v4/v5/v6 and
inactive storage sampling.

**Qualification completed for the bounded software scope:** the complete host
suite passed 6,948 test/subtest outcomes across 30 packages, with 17 platform
test skips and one skipped package. All 31 new roots and 216 required names
passed once, with run/pass membership and parent ordering checked. Formatting,
vet, Linux amd64/386/arm64 application builds, all eight commands and all eight
postchecks passed.

Selected native Linux amd64/386 fixtures passed 3,886 named outcomes across
14 suites and 736 root executions. Each architecture passed all 1,943 selected
names and 368 roots, with no failures or skips. Nine ownership cases, 32 archive
cases and six root postchecks passed, with confirmed cleanup.

These are software-fixture results, not physical firmware, TPM-health or
measured-boot evidence. The portable source subset does not establish execution
on unsupported operating systems. Broader firmware-lock, attestation, Secure
Boot image, physical-PC, installed-CLI, image, race/durability and release gates
remain open where documented.

## Memory Device Inventory

The finite Linux producer and its graph/report/comparison integration are
implemented and have passed host tests and selected native Linux amd64/386
software fixtures. The scope is three selected numeric observations from exposed
SMBIOS Type 17 records; it does not complete the broader memory requirement.

### Acquisition and row contract

The collector reads eligible entries under `/sys/firmware/dmi/entries/` through
the existing owned directory and scalar acquisition paths. It sorts cached names
lexically, accepts only the ASCII spelling `17-(0|[1-9][0-9]*)`, then opens only
the accepted entry's `raw` file. Nonmatching names are skipped without opening
their raw file. The suffix is neither converted to a fixed-width integer nor
exported. Each eligible entry receives a one-based, nonpersistent
`memory_device:N` ordinal. An absent, empty or ineligible-only registry adds no
rows and establishes no hardware absence.

Ordinary scalar errors discard all returned bytes, including favorable bytes
returned alongside an error, and produce a row with all three fields unavailable.
Existing ownership, protocol, bound, checked-close, directory and cancellation
failures retain their stronger policy: the collector returns no partial snapshot.
The existing 65,536-byte scalar bound is a product limit, not an SMBIOS ABI maximum.
The existing `memory:system` OS observation remains the RAM provider.

| Attribute | Finite interpretation |
| --- | --- |
| `capacity_bytes` | Decimal byte count, present only when reported. |
| `memory_type` | One admitted type label, present only when reported. |
| `form_factor` | One admitted form-factor label, present only when reported. |
| Each field's `_state` | `reported`, `unknown`, `unavailable`, `malformed` or `unsupported`. |
| `observation_source` | Always `smbios_type17` on an eligible row. |

`unknown` describes a firmware field with no known value; `unavailable`
describes an unobserved eligible entry. `malformed` describes failed structure;
`unsupported` describes a value outside the admitted encoding policy. Only
`reported` has a value. Missing, rejected and unknown capacity never becomes
zero. The source label identifies the input family without authenticating it.

### Whole-record framing

Parsing requires at least the four-byte header, Type 17, a formatted length from
21 through 255 bytes, and at least two bytes after that formatted area. The first
double NUL at or after the formatted boundary must end exactly at the end of the
raw file. Missing terminators, trailing bytes, concatenated records, wrong type,
short headers and incomplete formatted areas make all three fields malformed.
Opaque string bytes, including non-UTF-8 bytes, are allowed when this framing is
valid; they are never decoded or borrowed to complete a selected field. Handles,
string indexes and unselected fields are ignored. This is a bounded projection,
not a full SMBIOS conformance or authenticity validator.

Size is the little-endian 16-bit field at offset 12; Form Factor is byte 14;
Memory Type is byte 18. Extended Size, when selected and wholly present, is the
little-endian 32-bit field at offset 28.

### Capacity policy

| Size field | Capacity state and value |
| --- | --- |
| `0` | `reported`, `0` bytes: firmware's empty-entry observation. |
| `0x0001..0x7FFE` | `reported`, Size multiplied by 1,048,576 bytes. |
| `0x7FFF` | Select Extended Size according to the rules below. |
| `0x8000` | `unsupported` under the product's conservative policy. |
| `0x8001..0xFFFE` | `reported`, low 15 bits multiplied by 1,024 bytes. |
| `0xFFFF` | `unknown` with no value. |

For Size `0x7FFF`, a formatted length at most 28 means Extended Size is absent:
capacity is `unknown`. Lengths 29 through 31 contain only part of that field:
capacity is `malformed` while valid type and form-factor observations survive.
At length 32 or greater, bit 31 set in Extended Size is `unsupported`.
Values 0 through 32,766 are also `unsupported` under the product's conservative
policy; values 32,767 through 2,147,483,647 report that many MiB in bytes.
An extension is ignored when Size is not `0x7FFF`.

The base `0x8000` and sub-threshold extension choices are product policies, not
claims that every SMBIOS producer universally prohibits those encodings.
Multiplication widens to `uint64` first. The largest admitted capacity is
2,251,799,812,636,672 bytes; this boundary passed on native Linux 386.
A reported zero does not prove a physical slot is empty. Capacities are not
summed or substituted for usable OS RAM.

### Finite type and form-factor labels

Both enums map code 1 to reported `other` and code 2 to `unknown` without a
value. Every unlisted code, including future codes, is `unsupported` without
echoing the raw code. The following are the complete reported mappings; code
numbers are decimal.

| Form-factor code | Label | Form-factor code | Label |
| --- | --- | --- | --- |
| 1 | `other` | 3 | `simm` |
| 4 | `sip` | 5 | `chip` |
| 6 | `dip` | 7 | `zip` |
| 8 | `proprietary_card` | 9 | `dimm` |
| 10 | `tsop` | 11 | `row_of_chips` |
| 12 | `rimm` | 13 | `sodimm` |
| 14 | `srimm` | 15 | `fb_dimm` |
| 16 | `die` | 17 | `camm` |
| 18 | `cudimm` | 19 | `csodimm` |

| Memory-type code | Label | Memory-type code | Label |
| --- | --- | --- | --- |
| 1 | `other` | 3 | `dram` |
| 4 | `edram` | 5 | `vram` |
| 6 | `sram` | 7 | `ram` |
| 8 | `rom` | 9 | `flash` |
| 10 | `eeprom` | 11 | `feprom` |
| 12 | `eprom` | 13 | `cdram` |
| 14 | `3dram` | 15 | `sdram` |
| 16 | `sgram` | 17 | `rdram` |
| 18 | `ddr` | 19 | `ddr2` |
| 20 | `ddr2_fb_dimm` | 24 | `ddr3` |
| 25 | `fbd2` | 26 | `ddr4` |
| 27 | `lpddr` | 28 | `lpddr2` |
| 29 | `lpddr3` | 30 | `lpddr4` |
| 31 | `logical_nonvolatile_device` | 32 | `hbm` |
| 33 | `hbm2` | 34 | `ddr5` |
| 35 | `lpddr5` | 36 | `hbm3` |
| 37 | `mrdimm` | | |

`logical_nonvolatile_device` remains an enum observation; it does not imply
physical module topology or usable volatile RAM.

### Privacy, graph and consumer boundaries

Raw names, handles, indexes, opaque strings, serial/location data, unselected
fields and provider error text are excluded from the new observations. Identity
remains unavailable with existing algorithm/key-epoch bookkeeping and no new
source, value or confidence. Ordinals can shift and equal finite rows cannot
establish physical continuity, including indistinguishable same-count replacement.

All finite values, states and rows enter the ordinary configuration graph and
comparison; none is marked volatile. A changed configuration requires a new plan
for future execution. Finalization retains a signed failed-integrity diagnostic
with exact changed component references when the final graph differs. Existing
payload/signature/view rules and independent linked parent/child trust remain
in force. Signatures bind captured bytes, not firmware truth. This producer adds
no generic semantic validator for arbitrary inventory attributes or new strict
evidence schema. When no rows are added, old snapshot, graph, payload and view
bytes are preserved.

The addition reads no SPD or EDAC data, performs no device command, loads no
module, requests no extra privilege and adds no global-table fallback, worker,
capability, catalog row, dependency or identity algorithm. At that memory-device
increment, Quick v7 and Standard v8 retained their 12/13-worker schedules,
22 catalog rows and nine inactive functional families.

### Qualification and remaining requirements

The complete source defines 34 new test roots and 691 required test/subtest names;
the portable subset is 30 roots and 681 names. The host run separately confirmed
all 691 new required names passed. Source requirements and measured outcomes are
distinct.

| Closed software validation | Measured outcome |
| --- | --- |
| Full host suite | 7,639 named test/subtest passes and 30 package passes; 17 test skips and one package skip. |
| Selected native Linux amd64 and 386 suites | 5,268 named test/subtest passes across 14 package executions; no skips or failures. |

Each native architecture executed 2,634 required names across 402 test roots.
Exact names, completed lifecycles and child-before-parent completion were
reconciled independently, with no duplicate or extra native outcomes. This
includes actual 32-bit capacity arithmetic. Format checks, vet and Linux
amd64/386/arm64 application builds passed; cross-builds are compilation evidence,
not native execution or AArch64 qualification. Historical CPU and firmware
results remain evidence for those earlier changes.

The [parser and projection fixtures](../internal/inventory/memory_device_test.go),
[owned acquisition fixtures](../internal/inventory/memory_device_acquisition_test.go)
and [signed consumer fixtures](../cmd/hhpt/memory_device_report_test.go) define
the executable coverage. See
[the validation scope and remaining gates](test-strategy.md#memory-device-inventory-validation).

SPD, ECC deltas, topology and slot locations, speed/rate/voltage/rank,
independent physical capacity validation, RAM correctness beyond the bounded
memory workers, broader working sets and pre-boot Memtest86+ remain open. Image/sysfs
exposure and
permissions, physical-PC observations, installed-command/image behavior,
race/durability qualification, all four release images, the physical pilot and
required release approval remain separate gates. Actual 600-second Standard
workflow evidence belongs to the later
[Standard console qualification](#standard-console-launch), which is limited to
native VM runs with synthetic thermal observations. No memory-health,
attestation or physical-continuity conclusion follows from this inventory.

## Bounded Standard CPU Stress

Implementation is present across the standard coordinator, strict evidence,
finalization, signed bundles, canonical views and comparison. The earlier CPU
increment passed host, selected native and bounded real elapsed-window
qualification; later integrated-route gates remain below. At the touchpad
activation stage, Quick v9 was the default with thirteen workers. Explicit
`session -profile standard` required a matching frozen standard plan and used
fourteen workers with v10, with a 900-second default and maximum work budget.
That 22-row catalog had fourteen implemented rows and eight inactive families:
`audio.quick`, `camera.quick`, `input.touchscreen.quick`, `memory.stress`,
`network.quick`, `storage.quick`, `storage.destructive` and `usb.quick`.
Standard-only CPU stress did not activate those families or complete the
broader matrix. The current layouts are recorded in the
[Extended memory contract](#bounded-extended-memory-session).

The fixed request is a 600-second shared elapsed load window after all launched
workers have completed initial integer/SHA-256/scalar work and reached readiness.
The invocation-anchored 660-second watchdog includes preparation. Worker count
is bounded to 64. Admission requires strict real-time headroom and conservative
serialized millisecond headroom; a shorter parent deadline never becomes a
shorter passing request. A pass requires every worker to enter the window,
retain valid ordered accounting and complete further iterations plus at least
one twelve-vector scalar sweep during load. A completed scalar sweep followed
by a late recurrence must retain its checks without inventing an iteration.

Initial partial or unavailable accepted temperature coverage prevents load.
Later loss of accepted coverage stops work without a pass. Valid late heat is
retained independently of the monitor's first failure. Arithmetic mismatch
remains fail; observer faults remain error, coverage and incomplete work remain
inconclusive, and cancellation/deadline/thermal stops remain non-pass according
to finite precedence. Unconfirmed cleanup and invalid clock/counter/accounting
cannot produce signable evidence. Sample availability does not identify CPU
sensor coverage or detect every same-count sensor substitution.

Every dispatched Standard stress row, including historical v8 and v10, requires
strict persisted bytes, file and typed digests and a bound worker interval,
including valid no-load or non-pass results. Null source is limited to
undispatched rows. At the touchpad activation stage, Standard finalization
required the actual fresh v10 gate even when stress never started. Historical Quick contracts and exact old
supervisor notes remain readable, but opaque observed error/aborted objects
and re-signed no-view forgeries must fail semantic admission.

Independent acceptance covers readiness-or-terminal barrier liveness, nonzero
worker faults, fixed scalar-bit oracles, late work, stop precedence, partial
coverage and late heat, checked aggregate overflow, conservative deadline
rounding, source cloning, profile/plan mismatches, strict source/null relations,
legacy byte compatibility and identified-component comparison. Real coordinator,
finalizer, linked/unlinked signatures and canonical/no-view paths are required;
a hand-constructed verifier fixture alone does not establish that execution path.

The complete source passed the host suite: 6,732 test/subtest passes across
30 packages, with 17 test skips and one package skip. All 493 required named
outcomes across 69 selected roots passed. Selected native tests passed on Linux
amd64 and 386: 6,040 test/subtest outcomes across 18 package executions, covering
517 roots and 3,020 names per architecture, with no failures or skips.
All 15 names skipped on Windows and all 23 Linux-only names passed on both
architectures. Fifteen genuine subtest groups per architecture retained all 252
group-child orderings across the two runs. Nine owned-process cases, 32 archive
cases and six postchecks passed with cleanup confirmed.

A separate API-level qualification completed a real shared 600-second load
window on Linux amd64 and 386, using two workers and fixed synthetic temperature
observations of 35 degrees C. Both runs passed with cleanup confirmed. This
establishes elapsed-window completion and checked worker participation within
that fixture. CPU time, utilization, all-core saturation and physical thermal
behavior remain unqualified.

One additional test/root passed per architecture, with no failures or skips.
Each worker retained its
preparation accounting and completed additional load work and scalar checks.
The real-duration qualification also passed nine owned-process cases, 32 archive
cases and six postchecks with cleanup confirmed.
Race checks, installed CLI/image execution, representative physical CPUs and
sensors, the ten-PC pilot, durability and release approval remain separate gates.
The alpha candidate `b1a5fb9` and its required four-ISO release set are unchanged.
This result does not claim CPU utilization, scheduling continuity, complete
instruction coverage, performance, throttling, cooling qualification or future
hardware reliability.


## Standard Console Launch

This increment implements an explicit `S` console choice for standard. Automatic
start, Enter and `1` remain quick; modern `K` remains quick with deployment-key
media. Standard is offered only when `ui -launch-selection-output` names the new
handoff file and uses session-local identity. Any actual true exclusion blocks
standard until the operator restores the defaults; a refused choice preserves
the quick selection. Legacy quick JSON and no-output behavior remain supported.

The version-1 launch record has exactly `schema_version`, `profile`,
`architecture` and `excluded_tests`. It binds quick or standard to the requested
image architecture. Standard requires an explicit empty exclusion array; quick
retains its finite safe selection rules. The decoder rejects missing, null,
duplicate, aliased or unknown members, unsupported values and target mismatches.
The planner accepts this record through `-launch-selection-file`, using the
checked regular-file reader with a 16 KiB bound. Invalid launch selection stops
before key acquisition, inventory collection or plan output. The startup scripts
retain their existing earlier key/inventory work, so their downstream failure
requirement is no session, ready marker or save.

UI publication creates one exclusive file with mode 0600, checks the complete
write count and write/close errors, and closes every acquired owner exactly once.
It observes cancellation before creation and after creation, writing and closing;
observed cancellation prevents a launch status and preserves the created file.
The launch encoder's one final newline is retained. The UI rejects combining
`-launch-selection-output` with `-selection-output`; the planner rejects combining
`-launch-selection-file` with `-selection-file`. An explicitly empty new flag also
fails before waiting, creating a file or acquiring planner inputs. The old-only
empty flag retains its prior behavior.

Both startup scripts map only admitted UI statuses to literal profile and timing
choices, freeze the handoff and plan, and use matching plan/session profiles.
Quick retains five minutes and a 350-second outer supervisor policy; standard
uses 15 minutes and 950 seconds. These policies do not establish a universal
process deadline or independently confirmed cleanup. Unknown statuses and
invalid handoffs cannot fall back to quick. Failed session or final safety checks
prevent ready/save progression and retain available diagnostics.

The complete host suite passed 8,236 test/subtest outcomes across 30 packages,
with 18 test skips and one package skip. Across the two selected packages,
325 of 326 required names passed; the sole permitted Windows skip was
`TestUILaunchSelectionExistingLinkIsPreserved` when the owned symlink fixture
could not be created. The selected contract has 46 roots, no intermediate test
groups and 280 direct-parent relations. Independent recount and formatting
review preserved code tokens, literals and comments in all 12 candidate Go files.
AArch64 and FreeBSD compilation do not establish native execution.

Selected native Linux amd64 and 386 qualification passed all 652 named
test/subtest outcomes across 92 roots and four package executions. The independent
recount confirmed all 560 direct-parent relations and complete source-required
lifecycles, with no missing, extra, duplicate, skipped or failed outcomes.
All 18 startup-shell fixture methods passed using the real shell and owned
command stubs. The nine process-ownership cases, 32 archive cases and six final
host checks also passed. The shortened watchdog fixtures establish bounded
fixture behavior; they do not establish an actual 950-second wait or CPU load.
Relevant acceptance sources include the
[UI ownership tests](../cmd/hhpt/ui_launch_selection_test.go) and
[launch record tests](../internal/plan/launch_selection_test.go).

Actual native Linux amd64 and 386 VM runs exercised `S` input through a matching
frozen Standard plan, session and verified self-issued report. Each architecture
completed a cancellation smoke before its full run, using two CPU workers.
The measured session wall time and observed CPU load were recorded separately:

| Architecture | Run | Session elapsed (ms) | CPU load observed (ms) | CPU evidence |
| --- | --- | ---: | ---: | --- |
| amd64 | Cancellation smoke | 5,521 | 2,024 | Aborted |
| amd64 | Full | 711,388 | 600,000 | Pass |
| 386 | Cancellation smoke | 5,900 | 2,012 | Aborted |
| 386 | Full | 709,775 | 600,000 | Pass |

Both full sessions exceeded 600,000 ms and each retained exactly 600,000 ms of
CPU load evidence. The smoke records retained `operator_cancelled`; their short
load windows provide cancellation evidence only. Full-session journals recorded
`test_deadline`; this qualification covers the launch/report workflow and the
recorded CPU windows, with individual hardware outcomes retained separately.
All four calls passed CLI
verification of report contents and the self-issued signature, with source,
plan, ready-record and report bindings checked. This verification does not
establish operator identity or a second independent cryptographic implementation.
Owned process cleanup, unchanged inputs and fixture boundaries, archive closure
and all six final host checks passed.

The runs used read-only synthetic 35 C thermal observations and an empty hwmon
view. The 950-second Standard supervisor remains a policy limit; these successful
runs do not qualify an actual 950-second timeout event or a universal deadline
for kernel or descendant cleanup. The earlier CPU API elapsed-window runs retain
their original scope. Physical sensor/CPU validation, utilization or all-core
claims, installed boot images and all four rebuilt images, the ten-PC pilot,
complete-profile coverage, race/durability and release approval remain open.


## Memory-Capacity Report View

The shared HTML/PDF projection uses only the original signed inventory. It shows
"Sum of reported firmware entries" or "Reported-entry subtotal (partial)" when
valid reported capacities are available, alongside the OS-visible observation.
Capacity values use canonical unsigned decimal and checked uint64 arithmetic;
OS KiB conversion uses exactly 1,024 bytes per KiB. Neither an invalid value nor
an absent observation becomes zero. An explicitly reported empty entry remains
distinct from no firmware entries.

Populated, unpopulated, unknown, unavailable, unsupported, malformed and invalid
observations have distinct counts. A partial subtotal requires at least one
valid reported capacity, including an explicit zero; if none exists, the firmware
amount is unavailable. Invalid or ambiguous firmware observations and sum
overflow suppress the firmware aggregate. An invalid or ambiguous OS observation,
or KiB-conversion overflow, suppresses the OS amount. The valid other side stays
visible, but either invalid side prevents comparison.

An equal, OS-lower or OS-higher relationship requires complete eligible firmware
rows with at least one populated supported memory type, the same type across
populated rows, and a positive valid OS total. Mixed, nonvolatile, unknown or
unsupported types in populated rows do not qualify. An empty row's unknown type
does not itself block eligible populated rows. There is no tolerance threshold
or automatic hardware pass/fail result. The summary proves neither installed/
usable RAM, complete physical coverage nor health. Raw inventory, HMAC identities,
fingerprints and test outcomes remain unchanged; correction addenda do not alter
its source observations.

Current ordinary manifest v3 and linked v4 require exact
`view_profile=hhsoft.pc-tester.memory-capacity-view/v1` and at least one canonical
HTML/PDF view. The authenticated schema/profile selects complete canonical view
semantics; signed inventory and signed software version remain content inputs.
Future canonical-view changes require a new profile while original signed bytes
remain verifiable. HTML-only and PDF-only are supported, while mixed profiles
and invalid selectors fail without legacy fallback. Historical v1/v2 archives,
views and viewless support remain exact. A valid legacy-canonical re-signing does
not prove its age.

The complete candidate passed the full Go 1.27.2 Windows amd64 and 386 suites.
Each architecture recorded 12,642 test/subtest passes and 18 retained skips. All
337 new cases passed without skips: 79 projection, 222 bundle/profile, eight
correction-source and 28 command-consumer cases. Their exact names, 26 roots and
311 direct-parent completion intervals reconciled without missing, extra or
duplicate outcomes. Four pre-change ordinary/linked, externally trusted/self-issued
legacy artifacts and their member bytes remained exact.

Formatting and vet passed. The application and render, bundle, correction and
command test binaries compiled for Linux amd64, 386 and arm64: 15 compile-only
artifacts. These binaries were not executed. Linux execution and device behavior,
installed images, the physical-PC pilot and release qualification remain pending.
Earlier inventory and linked-report native evidence does not qualify this profile.

## Linked Retest Contract

The linked retest implementation is an opt-in report relationship, not a
hardware-history verdict. Current linked reports sign the independently verified
parent's manifest digest and exact canonical bundle SHA-256 in a v4 manifest
with the capacity view profile. Historical linked v2 and ordinary v1 remain
supported with their original bytes and envelope-v1 signing domain. A new linked
child has a distinct canonical UUIDv4. Product, ID and strict reference checks
precede view handling; historical v2 may be viewless, while current v4 requires
at least one canonical HTML/PDF view.

Review and validation cover correctly re-signed malformed metadata, two valid
parents with equal manifests but different signed envelopes, separate child
and parent trust, child-only unchecked references, one-hop behavior and
current-evidence isolation. The actual CLI verifier consumes checked owned
inputs for all four possible artifact/key roles. Real finalizer/session
composition must retain the consumed live gate, current failures, cleanup
exit12 and the clean-stop integrity context. Independent legacy archive and
render fixtures guard compatibility.

Historical linked-v2 validation on 2026-10-28 passed 4,804 test/subtest outcomes
across 29 packages, including all 56 selected roots; 17 tests were skipped
and one package contained no tests. Formatting, vet and Linux amd64/386/arm64
cross-builds passed, with all eight command and postcheck results successful.
A separate build produced eight test binaries for cmd, bundle, render and
report on Linux amd64/386; its 18 commands and nine postchecks passed.

Selected native execution of those eight binaries passed 698 test/subtest
outcomes and all 110 required root executions (55 per architecture), with
zero failures or skips and confirmed cleanup. Each architecture contributed
288 cmd, 54 bundle, six render and one report outcome. The cohort also passed
nine owned-resource cases, 32 archive cases and all six root postchecks.
These measurements used the isolated 491-file candidate; the prior committed
473-file baseline remained separate from the compiled source.

Implementation review, host tests, selected native software tests, installed
images and physical-PC evidence are separate. These historical linked-v2 results
do not qualify the current v3/v4 capacity profile. Correction history,
supersession, revocation, custody, durable storage and existing image/physical/
release work remain separate; the reference does not close them. This cohort did
not qualify an installed CLI, real report media, kernel-close faults,
blocked-kernel deadlines or fsync durability.

## Comparison Input Ownership

Comparison routes both report artifacts and applicable public keys through the
checked acquisition body. Regular-file metadata establishes eligibility before
readable acquisition; the readable descriptor must match that object. Each
input's metadata/readable owners close before verification of its captured
bytes. Input failures and cancellation observed before output dispatch suppress
comparison output. Input and untrusted-payload diagnostics are fixed; output
errors and destination ownership retain their existing behavior.

The global self-issued flag retains Boolean-value semantics, including explicit
false in trusted mode. After-key trust defaults to the before key/ID, with a
fresh acquisition even when the pathname repeats; complete overrides use their
separately supplied key. Historical generic v1 and linked v2 remain supported;
current v3/v4 artifacts must pass their complete profile verification before
comparison. HMAC key/epoch incompatibility and identity-ambiguity policy remain
unchanged. A signed retest link does not establish hardware continuity. The
following earlier command-ownership results retain their historical scope.

The 2026-10-28 isolated Windows run passed 4,875 test/subtest outcomes and 29
packages, including all 36 selected roots; 17 tests and one package were
skipped. Formatting, vet and Linux amd64/386/arm64 cross-builds passed, with all
eight commands and eight postchecks successful. Selected Linux amd64/386
execution of the comparison and retained command, bundle, render and report
cohort passed 938 test/subtest outcomes across eight suites and all 120 required
root executions (60 per architecture), with no failures or skips and confirmed
cleanup. Each architecture passed 469 required named outcomes: 408 command,
54 bundle, six render and one report. Nine owned-resource cases, 32 archive
cases and six root postchecks also passed. These measurements used the isolated
492-file source projection; the 491-file committed baseline remained distinct.

A cancellation observed in an already-entered destination callback can leave
an empty exclusively created file; failed output is not rolled back. This
increment does not establish deadlines for blocked OS calls, kernel-close fault
behavior, fsync durability, installed-CLI execution or physical-media outcomes.
Capture keys have separate qualification below. Remaining legacy key inputs
and persistence's live source handle remain separate ownership work. Existing
image, physical-PC and release gates remain open; prior cohort counts are not
new comparison evidence.

## Capture Input Ownership

Capture routes its fingerprint and raw Ed25519 signing keys through bounded
regular-file acquisition. Metadata establishes eligibility before readable
acquisition, and the readable handle must match that object. Checked closure
of both owners precedes key acceptance. Exact sizes remain 32 and 64 bytes;
public-key input remains 32 bytes. Length is validated before seed access,
and inconsistent private keys are rejected. Input diagnostics are fixed.
Hard input or cleanup faults remain failures when cancellation also occurs.

The command checks cancellation before each key, before session-ID randomness
and before collection. Its later worker, signing and output policies, including
worker cleanup exit 12, are unchanged. This is not a new live-session safety
gate or a deadline for blocked calls.

The 2026-10-28 isolated Windows run passed 5,004 test/subtest outcomes and
29 packages, with 17 test skips and one skipped package. All 28 changed-file
host roots and all 44 roots in the broader host contract passed. Formatting,
vet and Linux amd64/386/arm64 builds passed, with eight commands and eight
postchecks successful. The two unsupported-only candidate roots were not
executed in these Windows/Linux cohorts.

Selected native Linux amd64/386 execution passed 1,074 outcomes across eight
suites and all 100 required root executions (50 per architecture), with no
failures or skips and confirmed cleanup. Each architecture passed 537 required
named outcomes: 476 command, 54 bundle, six render and one report. Nine runtime
ownership cases, 32 archive cases and six root postchecks also passed. These
measurements used the isolated 494-file projection; its 492-file committed
baseline remained distinct.

At completion of the capture increment, six legacy raw-key calls remained: one
in finalization, one in deployment media discovery, one in report-target
discovery/authorization, two in persistence and one in save. The finalizer
increment below reduces this count to five. Persistence's live source handle
and other existing input loaders remain separate ownership work. These software fixtures do not qualify
installed-CLI execution, physical media, kernel-close faults, blocked-kernel
deadlines, secure key erasure or durability. Existing image, physical-PC and
release gates remain open.

## Finalizer Fingerprint Input Ownership

Finalization routes its fingerprint key through the existing
bounded regular-file reader with its exact 32-byte role. Metadata eligibility
precedes readable acquisition; matching readable identity and checked closure of
both owners precede byte acceptance. Missing or malformed contexts/readers and
observed cancellation fail safely. Earlier linked predecessor checks handle nil
and typed-nil contexts locally. A key error or wrong length takes precedence over
post-read cancellation, with fixed input diagnostics.

The key remains after existing argument, predecessor, outcome and evidence-directory
checks and before frozen-plan loading. An immutable verified-parent summary is
not reopened for the key read. The post-acquisition finalization policy, HMAC and
plan binding, current failure evidence, one-use live gate, self-issued signing
and output ownership are unchanged. Cleanly stopped sessions continue with their
fresh bounded integrity context; key failure does not redefine worker cleanup
exit 12.

The 2026-10-28 isolated Windows run passed 5,053 test/subtest outcomes and
29 packages, with 17 test skips and one skipped package. All 121 required
changed-file host roots passed. Formatting, vet and Linux amd64/386/arm64 builds
passed, with eight commands and eight postchecks successful. The unsupported-only
candidate root was not executed.

Selected Linux amd64 and 386 runs passed 1,934 test/subtest outcomes across eight
suites, with zero failures or skips and confirmed cleanup. Each architecture
passed all 128 selected roots and 967 required names: 906 command, 54 bundle,
one report and six render outcomes. This totals 256 root executions. All four
symlink cases skipped on Windows passed on each Linux architecture. The eight
explicit nested groups per architecture satisfied 144 child-start ordering
checks in total. Nine ownership cases, 32 archive cases and six root postchecks
also passed.

Five legacy raw-key calls remain: one in deployment media discovery, one in
report-target discovery/authorization, two in persistence and one in save.
Frozen-plan/evidence reader closure, persistence's live verified-report owner
and audit owners remain separate work. These fixture results do not qualify
unsupported OS acquisition, installed-CLI execution, physical media,
kernel-close faults, blocked-call deadlines, secure key erasure or durability.
Existing image, physical-PC and release gates remain open.

## Report-target Fingerprint Input Ownership

The isolated Windows host run passed 5,159 tests and 29 packages, with 17 test
skips and one skipped package. All eight commands and eight postchecks passed,
including formatting, the full test suite, vet and Linux build checks. All
106 required names from the new command tests passed on the host.

Selected Linux amd64 and 386 runs passed 244 named outcomes across four suites
and 44 root executions, with no failures, skips or extra named outcomes.
Each architecture passed 22 roots and 122 required names: 12 command roots with
106 names and ten retained report-target policy roots with 16 names. Nine of
those policy roots are Linux-only. The selection has no declared nested groups;
slash-containing child labels are single subtest names. Nine runtime ownership
cases, 32 archive cases and all six root postchecks passed; cleanup was confirmed.
These runs qualified an isolated source projection before public application.

The command retains its public entry point and supplies explicit required
input, discovery, authorization and destination functions to the tested body.
Usage checks remain first. Fingerprint acquisition uses the existing exact
32-byte role, with metadata eligibility before readable acquisition, matching
descriptor identity and checked closure of both owners before byte acceptance.
A reader error or malformed successful length takes precedence over the
post-read cancellation check. Input failures use fixed diagnostics and prevent
discovery, authorization and destination effects.

A non-pass Catalog remains serializable data and returns success when output
succeeds. Authorization errors retain their existing diagnostic and return
without output. Target eligibility, typed confirmation, HMAC policy, output
ownership and all persistence code remain unchanged. This increment adds no
post-consumer cancellation policy.

The command suite has ten portable roots and two Linux/Windows roots; all
106 source-required names passed on the Windows host and both Linux targets.
One separate root requires explicit unsupported input on other OSes and remains
unqualified because it was not executed there. Positive checks compare complete
literal catalog data and an independently fixed authorization HMAC. Failure
cases use the real owned-input core and exact acquired/closed ledgers; real
regular-file cases inject consumers rather than inspect host media.

After this implementation, four legacy raw-key calls remain: one in deployment
media discovery, two in persistence and one in save. The earlier capture and
finalizer counts describe their respective completed stages. Frozen-plan and
evidence readers, persistence's live verified-report owner and audit owners
remain separate work. Physical-media, image, physical-PC and release gates
remain open. These software fixtures do not establish installed-CLI execution,
kernel-close faults, blocked-call deadlines, secure key erasure or durability.

## Save Fingerprint Input Ownership

The save fingerprint-key boundary is implemented and qualified by the selected
software fixtures. The host run passed 5,240 test/subtest outcomes across 29
packages, with 17 test skips and one package skip outside the selected save set.
All 130 selected save names passed, including 81 names from the new tests and
49 retained names. All eight host commands and eight postchecks passed.

Native Linux `amd64` and `386` test-suite invocations passed 260 outcomes and
56 root executions: 130 names and 28 roots per architecture, with no failures or
skips. The selection contains 11 new supported roots and 17 retained save roots;
its slash-bearing labels are direct subtest names, with no intermediate groups.
Native fixture cleanup completed. Earlier capture, finalizer and report-target
results retain their original scope and counts.

Existing usage validation and audit initialization precede the new input gate.
It passes the original context and key path to the finite 32-byte owned reader,
checks successful length independently and accepts bytes only after checked
readable and metadata closure. Invalid contexts and a missing reader fail
locally after audit initialization. Fixed key-input diagnostics omit the path
and provider error. Input failure prevents report opening and all later
save/discovery/authorization/mount/persistence effects, without undoing the
already completed audit initialization.

Only nil bytes with an exact cancellation/deadline error equal to the context's
current error use the clean cancellation diagnostic. Hard, wrapped or joined
errors, malformed successful lengths and input cleanup faults retain failure
priority over concurrent cancellation. Successful closed capture is followed
by a context check. These stops retain save's ordinary failure exit status 1;
the original later report-source, interactive confirmation, retry, write-attempt,
unmount and receipt behavior is unchanged.

Consumer tests exercise the real owned-input core with injected owner faults,
exact closure ledgers and checks that no later input, menu or save consumer
runs. Static source-order review and paired valid-report/missing-report rejection
cases establish key-failure precedence; they do not directly count every
internal report open. Real regular-file size cases and non-regular-input dispatch
rejection run on the supported host and native platforms. The other-platform
rejection test was not selected and provides no runtime credit here.

The source change leaves three legacy raw-key calls: one in deployment media
discovery and two in persistence. Earlier remaining-call counts describe their
respective stages. Frozen-plan/evidence readers were outside that key-input
scope; see the later [small-document reader increment](#small-regular-file-input-closure).
Audit closure and the live verified-report owner remain separate work. These fixtures do not establish
unsupported-platform acquisition, installed-CLI execution, physical media,
kernel-close faults, blocked-call deadlines, secure key erasure, rollback or
durability. Existing image, physical-PC and release gates remain open.

## Persistence Key Input Ownership

The persistence fingerprint/public-key input change is implemented and qualified
by its own consumer fixtures and selected native runs. Host validation passed
5,374 test/subtest outcomes across 29 packages, with 17 test skips and one package
skip. All eight validation commands and eight postchecks passed. The selected
28 roots produced 222 named passes: 134 from the new consumer cases and 88 from
retained linked-report and persistence-receipt regressions.

Native Linux amd64 and 386 runs each passed all 222 required names under 28 roots.
Together they produced 444 passed test/subtest outcomes in two suites and 56 root
executions, with zero failures or skips. Ownership and archive rejection checks
passed, all six postchecks passed, and cleanup was confirmed. The other-OS
rejection root was not executed by this qualification.

Usage validation and the existing authorization-file read/decode precede the new
guard for nil or typed-nil context, missing reader, nil persister and stopped
context. `DecodeAuthorization` checks authorization shape, including its schema,
binding, lifetime and policy fields. MAC and current boot/device authorization
remain separate checks in persistence policy.

The original context/path and exact finite role reach each key read. Any returned
error or non-32-byte successful result rejects before post-read cancellation:
`read fingerprint key: input acquisition failed` for the first key and
`verify source report: input acquisition failed` for the trusted public key.
This includes pure context errors; persist does not adopt save's clean-reader-
cancellation exception. Local admission or cancellation after a valid closed
result uses `persist input acquisition failed`. These failures return 1. Fixed
diagnostics are limited to the new boundaries.

Fingerprint acceptance requires checked input closure before report opening.
Trusted public-key acquisition still follows report opening; self-issued mode
performs no public-key read. Same-path keys are acquired separately. Existing
Boolean-value trust grammar, trimmed key-ID lookup, v1/v2 current-bundle
verification, linked references, report naming, persister arguments, required
receipt flags and output behavior remain unchanged. Decoding authorization is
not evidence that its MAC or a physical target was validated.

The exercised [consumer cases](test-strategy.md#persistence-key-input-ownership-validation)
cover exact forwarding, owner closure, error/shape precedence, cancellation,
trust branches and downstream behavior. Source ordering and paired missing-report
precedence witnesses remain distinct from a counted report-open seam.
Ordinary-file fixtures require an injected media persister for successful command
paths; such fixtures do not establish actual media writes.

This implementation reduces the save-stage three legacy raw-key calls to one in
deployment discovery. Authorization-file and frozen-plan/evidence closure were
outside that key-input scope; see the later [small-document reader increment](#small-regular-file-input-closure).
Audit and the live verified-report owner remain separate work. Unsupported-OS
acquisition, installed-CLI execution, physical media, kernel-close faults,
blocked-call deadlines, secure erasure, rollback and durability receive no new
qualification. Existing image, physical-PC and release gates remain open.

## Deployment Discovery Key Input Ownership

The final legacy raw-key consumer has been migrated to the checked input reader,
and the unused helper has been removed. This increment has its own host and
selected native software-fixture evidence; preceding persistence results remain
historical evidence for that earlier scope.

Usage errors precede the audit-output attempt. A missing audit operation fails
locally; other required operations and nil, typed-nil or stopped context are
checked after a successful audit open. The reader receives the original context,
literal path and finite fingerprint32 role. Every returned error or successful
result of a length other than 32 rejects before post-read cancellation. The fixed
key diagnostic is `read media discovery key: input acquisition failed`; local
admission or cancellation after a valid closed result uses
`deployment-key input acquisition failed`. Both return 1. There is no save-style
clean-reader-cancellation exception.

Key-owner closure is required before activation availability, media selection,
confirmation input, token loading or activation writes. The existing audit owner
is already acquired and retains its existing closer. No token, mount or output
lifetime is newly qualified by closing the discovery-key handles.

The six explicit operations preserve the real production path while allowing
consumer fixtures to isolate audit, media and activation effects. Discovery uses
the accepted key, while the selected token supplies the deployment key and epoch.
Existing eligibility, confirmation grammar, token checks, fixture exclusions,
activation-record fields, downstream diagnostics and successful read-only mount
policy remain unchanged. Failed post-load paths retain their existing unmount
calls; this increment does not add confirmation of their success or audit closure.

The executed [consumer cases](test-strategy.md#deployment-discovery-key-input-ownership-validation)
cover independent successful token-record/activation oracles, exact forwarding,
real owned-reader failure ledgers, error/shape precedence and downstream policy. Real
ordinary-file key fixtures require injected media/activation effects; a public
wrapper or dispatcher rejection must stop before device access. Other-platform
rejection source does not itself establish runtime qualification.

Windows host validation passed 5,473 test/subtest outcomes across 29 packages,
with 17 test skips and one package skip. All eight commands and eight postchecks
passed. The selected 12 roots and 101 named outcomes passed, including 99 names
from the ten changed-file roots and two retained policy roots.

Native Linux amd64 and 386 test binaries passed 202 named outcomes in two suites
and 24 root executions: 101 names and 12 roots per architecture, with no failures
or skips. Each selection contains the same 99 changed names and two retained
policy names. Nine ownership cases, 32 archive cases and six root postchecks
passed, with cleanup confirmed. The other-OS-only rejection root was not executed.
These fixture results do not demonstrate actual token-media discovery, mounting
or activation-file writes.

No legacy `readRawKey` definition or caller remains in this implementation.
Authorization-file and frozen-plan/evidence closure were outside this discovery-key
scope; see the later [small-document reader increment](#small-regular-file-input-closure).
Audit, encoded-token, mount, activation-output and live report ownership remain
separate. No new
physical-media, installed-CLI, unsupported-OS, kernel-close, deadline, rollback,
secure-erasure or durability claim is made. Existing image, physical-PC and
release gates remain open.

## Inventory-plan Fingerprint Input Ownership

The `inventory` and `plan` fingerprint-key inputs now use checked regular-file
acquisition with an exact 32-byte result. These were two direct `os.ReadFile`
calls outside the completed legacy `readRawKey` helper migration described
above. Incorrect key lengths were already rejected downstream; the new behavior
adds bounded acquisition, identity/stability checks and confirmed owner closure
before collection consumes the key.

Both commands preserve the original context, literal input path and fingerprint
input role. Any read error or returned length other than 32 produces
`read fingerprint key: input acquisition failed` before post-read cancellation
is considered. Local dependency/context admission and cancellation after a valid
closed result use the command's fixed input-acquisition diagnostic. Key errors
do not disclose key bytes, paths or provider details. Nil or typed-nil contexts,
missing operations and observed cancellation stop later collection or output.
Cancellation remains cooperative; these checks do not preempt a blocked kernel
operation or prove a hard deadline for it.

Usage and plan-selection validation retain their existing precedence over key
acquisition. Inventory retains its existing positional-tail policy. Collection
errors retain their original diagnostics and precedence. Profiles, validated
selection contents, schemas, plan IDs and output destination policy are
unchanged. The [consumer and ownership fixtures](../cmd/hhpt/inventory_plan_fingerprint_input_test.go)
exercise acquired-owner failures, metadata/identity changes, bounded reads and
the terminal EOF probe, closure errors, cancellation/deadline observations and
downstream output failures. Platform-specific source additionally exercises
ordinary files and Windows path spellings, Linux links/FIFOs and descriptor
replacement, and rejection on unsupported operating systems. Source coverage
and execution evidence are distinct.

Windows host validation passed 7,943 test/subtest outcomes and 30 package
outcomes, with 17 test skips and one package skip. Focused and full-suite
selections each completed exactly 1,102 named lifecycles under 107 roots:
1,101 names and 106 roots passed; `TestPlanCommandRejectsSelectionSymlink` was
the sole permitted selected skip. All 304 names from the 20 new host roots
passed. Retained coverage contributed 797 passes and that one skip. The eight
actual intermediate groups and all 995 source-derived parent relationships
satisfied lifecycle ordering; slash-containing flat labels were not treated as
additional groups.

Five deterministic pre-change artifacts were captured before this source
change: default and filtered inventory, plus default, selected and standard
plans. Independent semantic expectations and 17 HMAC reference vectors supported
the baseline review. The unchanged probe then produced five candidate artifacts
with exactly the same original bytes, with all six named tests and one package
passing. This compares the candidate with a captured pre-change baseline, not
with a second candidate build. All 12 raw/formatted source files preserved
tokens, literals and comments; the full product source remained unchanged when
the temporary probe was added.

All 11 host commands and nine boundary postchecks completed successfully,
including vet, Linux amd64/386/arm64 application builds and a FreeBSD amd64 test
binary compilation. Linux arm64 and FreeBSD remain compilation-only evidence
for this increment.

Native Linux amd64 and 386 each passed exactly 1,071 named outcomes under 112
roots, including eight actual intermediate groups: 2,142 named passes in total.
Each required name ran and passed once, with no failures, skips, extra or
duplicate names. All 959 source-derived parent relationships per architecture
satisfied lifecycle ordering. The 22 Linux-only required names and the
host-skipped `TestPlanCommandRejectsSelectionSymlink` case passed on both
architectures. Native cleanup, input stability and VM boundary checks passed.

The scoped source review retained existing assertions and found no abandoned
key-reader helper, import or stub in the 12 changed files. This is not a claim
that every CLI input has checked ownership. Authorization-file and
frozen-plan/evidence closure were outside this key-input increment and are
covered by the later [small-document reader increment](#small-regular-file-input-closure).
Audit and live report-source lifetimes remain separate work.
Unsupported-OS runtime, installed-command/image behavior, kernel-call preemption,
race/durability, physical-PC qualification, the ten-PC beta pilot and all four
release-image gates remain open for this increment. The initial alpha retains
its separate release requirements.

## Small Regular-file Input Closure

The shared small-document reader returns bytes only after successful readable
handle closure. Linux also requires successful metadata-handle closure before
transferring the readable handle. Acquisition, metadata, read, length or cleanup
failure returns an error and no bytes. Fixed input/read/cleanup categories retain
combined read and cleanup failures without exposing provider diagnostics.

The existing size limits and document decoders remain in place at six production
call sites:

| Input | Consumer |
| --- | --- |
| Quick selection file | `plan -selection-file` |
| Persistence authorization | Report-target persistence |
| Frozen finalizer plan | Signed finalization |
| Optional evidence | Finalizer and session evidence loading |
| Frozen session plan | Session startup |
| Session journal | Journal loading and post-write readback |

Inputs must be nonempty regular files within the caller's limit. Reading is
bounded by that limit plus one byte and must match the admitted size. Every
acquired owner receives one close attempt, including owners returned with an
error; a failed close rejects the result. The planner's selection failure stops
key acquisition, collection and output. Its usage precedence remains intact.
The separate `-launch-selection-file` path retains the checked launch-record
reader and its quick/standard routing.

On Linux, metadata acquisition and readable reopening use the existing pinned
object operations, reject symbolic links and special files, and compare object
identity and size. The metadata owner closes before byte reading; the readable
owner closes before success. Other platforms retain the existing
`lstat`/open/identity/size policy. They do not gain Linux's pinned-reopen or
nonblocking-open guarantee. Neither policy freezes file contents or adds
post-read metadata checks or cancellation preemption.

Windows amd64 validation passed 8,303 test/subtest outcomes across 30 passing
packages, with 18 test skips and one package skip. The Windows 386 command
package passed 3,207 test/subtest outcomes, with six skips. Each architecture
passed all 67 selected names under eight roots and satisfied all 59 direct
parent relationships, with no selected failures, skips, missing, extra or
duplicate outcomes.

All 11 commands and seven boundary postchecks passed. This includes full
Windows amd64 tests and vet, actual Windows 386 command tests, and Linux
amd64/386/arm64 plus FreeBSD amd64 test-binary compilation. Compilation alone
gives no runtime qualification; arm64 and FreeBSD remain compilation-only for this
increment. All seven raw/formatted Go files preserved code
tokens, literals and comments; the formatted source projection contains 594
files, including 578 Go files.

Linux amd64 and 386 test binaries each passed 76 selected names under 11 roots:
152 named passes, 22 root executions and two package passes in total. All 65
direct parent relationships per architecture passed, with no failures, skips,
missing, extra or duplicate names. These software fixtures ran in a VM and do
not establish installed-CLI or physical-media behavior.

Nine runtime ownership fixtures and 32 archive fixtures passed, along with all
six boundary postchecks; cleanup was confirmed. The
[fixture coverage](test-strategy.md#small-regular-file-input-closure-validation)
distinguishes injected cleanup faults, real owned-file behavior and platform
coverage. Earlier key-input and Standard workflow results remain evidence for
their own source scopes.

This increment does not close audit, encoded-token, mount, activation-output or
live report-source ownership. It does not establish actual kernel-close failures,
`fsync`, durability, rollback, blocked-call deadlines or physical-media behavior.
Installed images, the four release-image checks, physical-PC qualification and
the beta pilot retain their separate gates.

## Current review boundaries

The initial requirements review found no implementation TODO/FIXME placeholders
in the application, internal packages, or scripts. It did identify behavioral
gaps despite existing passing tests:

| Intended behavior | Independent failure case |
| --- | --- |
| Safe storage rejects every writable block mount | Writable dm, md, and loop devices omitted from hardware inventory; mount aliases and escaped paths |
| USB readiness never passes unexamined or misidentified devices | Unauthorized/unknown-authorization peripherals; zero authorized devices; an unrelated matching character node; duplicate bus/device addresses; identity replacement; failed closure |
| Report saving writes only to the currently authorized destination | Replacement paths, expired authorization, upstream hub identities, mapped or unregistered sibling partitions, unsupported or ambiguous filesystem headers, and external ext4 journals; descriptor binding through the mount operation |
| Signed pass results contain sufficient consistent observations | Header-only results, zero work, missing safety fields, ambiguous JSON, and contradictory mount/traffic evidence |
| Display acceptance requires every applicable pattern and operator criterion to pass | One successful output must not hide another failed or incomplete pattern; partial operator findings and explicit failures must survive aggregation and finalization |
| Operator exclusions remain explicit in the frozen plan and signed report | Default selections changing plan bytes; excluded workers running; absent or missing-driver reasons being overwritten; omitted frozen test rows; stale evidence for an excluded worker |
| Guided keyboard claims cover six fixed controls on every applicable interface | One complete device hiding another incomplete device; initially held or stale actions; partial frames; disconnect/replacement; retained passes without final neutral/identity checks; hidden functional claims in execution notes |
| Frozen test deadlines bound complete work, and cancellation preserves evidence | Sequential subprocesses or multiple display cards exceeding a row's timeout; stopped workers or descendants surviving; cancelled finalization losing completed failures |
| Build inventories retain the exact selected package versions | Native APK resolver behavior differs from a mocked command runner |
| Source publication scans the objects actually transferred | Dangling outgoing commits, replacement refs, merge-only secrets, and deleted instruction backups |

The 2026-10-15 review passed the full Go test suite and `go vet` on Windows
(Go 1.27.2) and Alpine Linux (Go 1.26.9), plus application cross-builds for
Linux x86_64, x86, legacy x86, AArch64, and Windows x86_64. Linux executed all
19 build and 31 release Python tests, including native APK resolution and real
GPG signature/tamper checks. The 24 security tests passed across the two hosts:
Windows skipped the Linux symlink fixture; Linux skipped the two Gitleaks
integration cases, which ran with pinned Gitleaks on Windows. Workflow lint,
platform-lock checks, and the combined publication scan also passed. These
results cover this source review milestone, not subsequent changes or images.

On 2026-10-16, an x86_64 development image passed the real QEMU USB report-save
flow. Before authorization, after a wrong code, and after the next selection,
both QEMU write counters were zero and the whole disk image digest was unchanged.
Correct confirmation produced a 184,105-byte report; after unmount and guest
shutdown, mtools extracted it from a read-only partition copy. Its digest matched
the guest receipt and the independent verifier accepted its self-issued signature.
The MBR and bytes outside the selected partition remained unchanged. Graphics
and display remained inconclusive; no physical-device qualification is implied.

This development fixture reused independently copied, completed kernel/package
sections after fixing a nested fakeroot overlay failure. Its ISO SHA-256 was
`f0d33af03229fb0d3f8b798288d81b8c6b0ba1714e4772d1e766f9898fe63def`.
The same image also booted in the preserved VirtualBox BIOS and UEFI fixtures;
CPU, memory, storage safety, input readiness, and run integrity passed, with
zero writable block mounts and self-issued reports. Original VM media and
serial-log settings were restored afterward.
It is not a fresh offline release build or a published download. The new native
fakeroot regression verifies that the overlay inherits the outer session's fake
ownership without changing real file ownership. All 28 build and 10 lab Python
tests passed on Alpine; Windows skipped the three native build integrations.
The Go suite and vet passed on both hosts, all five application targets built
with Go 1.27.2, and a Go 1.27.2 Linux test executable also passed the save tests.
The builder's native Go suite used Go 1.26.9. CI runs the maintained harness
failure fixtures, including its embedded Python syntax check; full image boot
and report-save integration still run in the separate lab.

The 2026-10-25 safe quick selection milestone passed the Windows Go suite and
`go vet` with Go 1.27.2, plus all five application build targets. The suite
recorded 735 test/subtest pass events across 16 packages, zero failures, and
13 skips. Native Linux amd64 and 386 each recorded 83 planner and 109 CLI
test/subtest pass events with no skips. The actual generated modern startup
script and legacy startup script also passed their isolated native supervisor
fixtures without skips.

Native menu-to-plan-to-signed-report checks exercised all nine exclusions on
x86_64 and x86, and all seven supported exclusions on legacy x86. Each report in
this milestone retained all nine implemented quick-worker outcomes; legacy
graphics and display were not selectable.
Each target rejected three special-file selection inputs. The checks verified
signed report contents and signatures, including the frozen exclusion intent.
A comparison against the pre-selection implementation also confirmed unchanged
default plan bytes and ID. ISO rebuilds, physical beta qualification, and
release approval remain open for this milestone.

The subsequent 2026-10-25 report-coverage and worker-cleanup milestone passed
the full Windows Go suite and `go vet` with Go 1.27.2: 809 test/subtest pass
events across 16 packages, zero failures, and 13 skips. All five application
targets built. Native Linux amd64 and 386 each passed seven affected packages
with 630 test/subtest pass events and no skips. The 19 generated-modern and
legacy supervisor fixtures passed without skips, including terminal cleanup
failure before later work, finalization, or saving. Native Go 1.26.9 race testing
passed all 43 workerexec test/subtest cases without skips or race findings.

Real x86_64, x86, and legacy x86 menu-to-plan-to-finalize-to-verify workflows
produced signed reports containing all 21 frozen catalog rows plus run integrity,
with exact planned dispositions and exclusion intent. Failure tests cover
descendants retaining or closing output pipes, cancellation/exit races,
unconfirmed cleanup, diagnostic-write failures, and capture stopping before
later collectors or signing. Report tests reject ambiguous or contradictory
new coverage claims, including viewless bundles. Four historical HTML/PDF byte
digests captured from the preceding implementation remain unchanged. These
checks validate applications and fixtures; new ISO builds, whole-run deadlines,
physical qualification, and release approval remain open.

The 2026-10-25 quick-session milestone passed the full Windows Go suite and
`go vet`: 948 test/subtest passes across 16 packages, zero failures, and 13
platform skips. All five application targets built with Go 1.27.2. Native
Linux amd64 and 386 each passed 661 test/subtest cases across the four affected
packages without skips. The actual generated modern and legacy shell scripts
passed 82 isolated handoff scenarios across 12 integration methods, plus the
fixture syntax test. This includes a report-written-then-watchdog-stop case
that must not announce readiness or save. Worker orchestration is tested in the
Go coordinator rather than duplicated in the shell fixture.

Twelve real CLI sessions exercised all-off completion, a shared deadline,
Ctrl-C, and SIGTERM on each modern x64/x86 and legacy x86 application. Every
report retained all 22 results, exact frozen dispositions, signed stop reason
and worker states, and safe observations including the fresh pre-sign check.
Independent verification passed, and owned pseudo-terminal modes and descriptor
flags were restored. Native Go 1.26.9 race testing passed 120 session, journal,
finding-input, and signal test/subtest cases without skips or race findings.
The source audit found no abandoned implementation stubs or orphaned helpers.
Rebuilt ISO qualification, continuous monitoring, remaining functional adapters,
and the physical beta pilot remain open; cooperative cancellation does not
establish preemption of an uninterruptible kernel operation.

The subsequent guided-pointer milestone adds `input.pointer.quick` to the
implemented quick path. The Windows suite passed 1,094 test events across 17
packages, with 12 platform skips and no failures; vet and all five application
targets also passed. Native Linux amd64 and 386 suites passed 1,650 test events
across input, inventory, graphics, storage, safety, and CLI code, without skips.
Pointer/session race checks passed 241 test events without skips or race reports.
The historical counts above describe their original catalogs. Selection at the
pointer milestone had ten modern and eight legacy choices; the
existing 21 catalog rows plus run integrity still produce 22 results. Enabling
pointer changes the catalog digest and introduced fresh plans and a ten-outcome
`hhsoft.pc-tester.quick-session/v2` journal for that milestone. Historical
signed-report verification and rendering remain unchanged; direct capture continues to
omit guided pointer testing.

Portable and native fixtures independently cover relative `EV_REL` X/Y movement in both
directions and primary plus advertised right/middle button cycles on every
interface. Negative cases include one complete device hiding another
incomplete device, absolute/mixed interfaces, missing actions, lost or malformed
events, disconnect/replacement, cancellation/deadline races, contradictory
evidence, stale plans/journals, and privacy leakage. Initial queued events are
discarded, and a completed sequence cannot hide a subsequently queued loss or
disconnect. Capability checks use independently observed kernel bitmap width;
an ambiguous 32/64-bit interpretation cannot omit advertised buttons. Retained
per-device passes inside cancelled or partial results require the same hardware
binding as an aggregate pass. Reports retain bounded
action summaries without raw input events or typed text. The default observation
window is 30 seconds, the whole-command limit is 90 seconds, and the quick
session's five-minute budget remains independent and shared.

Fifteen real CLI scenarios passed on modern x64/x86 and legacy x86 applications:
twelve session cases covered all exclusions, deadlines, Ctrl-C, and SIGTERM;
three pointer-to-finalizer cases preserved incomplete observations from two
virtual interfaces in verified 22-row signed reports. The shell handoff suite
also passed. These exercise execution and honest non-pass reporting; they do
not establish successful physical pointer actions. Rebuilt-image qualification,
the physical pilot, continuous monitoring, and remaining functional adapters
remain open.

The periodic safety milestone implements bounded read-only mount/network
monitoring through quick-session execution, input cleanup, and final safety. It uses one
owned serialized observer, a one-second interval and observation deadline, a
2,500-millisecond maximum gap, and a two-second join allowance. Its first unsafe,
unknown, timed-out, or excessively delayed sample cancels active work and blocks
signing, even if a later sample is safe. A clean operator stop may still finalize
after the monitor joins and the independent pre-sign check passes.

Journal `hhsoft.pc-tester.quick-session/v3` adds the bounded coverage summary
while keeping the catalog, plan digest, and ten worker outcomes unchanged.
Leading and trailing gaps must remain bounded; a session before the first
one-second tick records zero samples rather than claiming observations it did
not make. Fresh v3 execution journals are required, while historical signed
verification remains unchanged. The boot guard increases to 350 seconds to
include the monitor join allowance.

For this milestone, the Windows suite passed 1,212 test events across 17 packages,
with 12 platform skips and no failures. Vet, all five application targets, and
cross-builds of 12 native test binaries passed. Native Linux amd64 and 386 passed
1,886 test events across six suites without skips. Race checks passed 321 test
events without skips or race reports. Nine compiler-supervision fixtures
confirmed child cleanup, and six independent environment and input checks
passed.

Fifteen real CLI scenarios passed: twelve quick sessions and three standalone
pointer-to-finalizer flows across modern x64/x86 and legacy x86 applications.
Each 2.5-second session deadline case retained two periodic observations.
The signed monitor evidence exactly matched `session-safety-monitor.json` in
the evidence directory, and reports retained all 22 results. These isolated VM
checks validate the application paths and bounded reporting claims; they do not
replace rebuilt-image or physical qualification. Historical milestone counts
above remain separate evidence.

Rebuilt bootable images, the physical pilot, remaining functional adapters,
broader beta qualification, and release gates remain open. Those mount/network
observations do not provide continuous coverage, thermal or heartbeat monitoring,
stress qualification, privilege isolation, or preemptive containment of a
blocked kernel operation. These results do not establish publication readiness.

The subsequent local thermal prerequisite is implemented in source. It introduced
`hhsoft.pc-tester.cpu/v2` and
`hhsoft.pc-tester.memory/v2`. The initial and subsequent observations have
one-second deadlines; subsequent samples start after a 250-millisecond wait
following completion. Results are constructed after a bounded two-second
observer join, including a final in-flight reading during normal completion.
Unconfirmed cleanup returns exit code 12 and prevents later work or signing,
even if diagnostic output fails. Known calculation and RAM mismatches remain
failures. Available, partial, and unavailable thermal coverage is explicit;
a computation pass does not establish thermal health when telemetry is absent
or incomplete.

Focused Windows command, finalizer, and session-journal regression tests passed
571 test events across 59 top-level tests, with no failures and four symlink
cases skipped. Controls cover output failures, both contradictory cleanup
claims, malformed/duplicate/old-version evidence, existing unreadable evidence,
impossible aggregate counts, retained upper-range heat, and preserved worker
failures. A missing evidence file retains the documented error/aborted fallback;
an existing invalid CPU/RAM file cannot use that fallback to bypass validation.
The subsequent full Windows suite passed 1,532 test events across 17 packages
with 21 platform skips and no failures. Vet and five application targets passed:
Linux amd64, x86 with SSE2, 386 soft-float, arm64, and Windows amd64. Seven test
suites also cross-compiled for both Linux amd64 and 386.

Native validation of this increment completed on 2026-10-26. The 14 suite runs
across Linux amd64 and 386 passed 2,690 test/subtest events with no skips. Race
checks for the quick-worker and command packages passed 1,125 events with no
skips. All 15 command-line workflows passed: 12 quick-session cases and three
pointer cases. Nine compiler and nine runtime process-ownership controls also
passed with confirmed fixture cleanup. Pass counts were independently checked
against the test logs. Historical signed-report verification and journal v3 are
unchanged. That milestone retained ten implemented workers and 21 catalog rows,
plus run integrity in the report.

This local guard is a prerequisite for the separate run-wide increment. It
does not monitor other workers or an all-off selection, implement heartbeat or
general hang supervision, or introduce privilege isolation. Fresh images,
physical sensor validation, the beta pilot, all four alpha ISOs, source/image
privacy review, and publication approval remain separate open gates.

The safemode acquisition increment retains 64-byte scalar and 4 MiB
mount-metadata limits, using positive requests of at most 4 KiB and one overflow
byte. Registry enumeration requests at most 32 entries, retains the 4,096-entry
limit and one overflow entry, and requires exact EOF. Names are validated,
cached once, checked for duplicates and sorted before later acquisition.
Short successful reads are progress; wrapped or joined EOF is not completion.

Linux uses root containment, final-component no-follow, metadata type proof and
matching metadata/readable-descriptor identity before accepting bytes. Every
acquired root, metadata and readable handle closes once, including a handle
returned with an open error. Failed acquisition or closure revokes safe credit.
Hard errors survive simultaneous cancellation; a previously observed unsafe
count retains failure precedence. Only complete, identity-bound values can
retain unsafe diagnostics after a later close or cancellation failure. Each
category remains independently observable unless cancellation stops new work.

The result schema, mount parser, catalog and session policy are unchanged.
Failure tests cover the actual owned fixture producer and periodic monitor;
CLI, session and finalizer cases compose its exact diagnostic result. A valid
self-issued signed control and exact rejection checks guard the finalizer
tests. This is compositional coverage, not a same-run kernel fault through
signing. Host validation passed 4,357 test/subtest outcomes across 29 packages.
All 50 selected roots and their 311 outcomes passed: 19 new roots with 133
children and 31 retained roots with 128 children. The full run retained 17
existing test skips and one empty package. Formatting, vet and Linux
amd64/386/arm64 cross-builds passed. Selected native Linux amd64/386 fixtures
passed 640 test/subtest outcomes across four suite invocations, including all
54 required roots per architecture (108 executions), with zero failures or
skips. Six final checks and cleanup confirmation passed. This native evidence
uses owned fixtures and does not establish physical or continuous safety.
Historical results below retain their original scope. Device data, network
traffic, physical hardware, rebuilt images and release qualification remain
outside this increment; bounded requests do not preempt blocked kernel calls
or bound aggregate allocation or continuous safety.

The subsequent shared periodic-engine extraction preserves the safemode API,
duration constants and v1 evidence. Six exact JSON cases were executed against
the preceding implementation before extraction; their literal bytes remain
unchanged. Focused engine, safemode and command integration checks passed 217
test/subtest events with no failures or skips. These include deadline and
concurrent-Stop behavior, independent immutable snapshots, raw-result policy
classification, assessment-panic cleanup, and completion timestamps captured
before policy evaluation. The full Windows suite then passed 1,549 test/subtest
events across 18 packages, with 21 Windows-specific skips and no failures. Vet
passed; 16 Linux test binaries across eight packages on amd64/386 and all five
application targets compiled successfully. The subsequent 16 native Linux
amd64/386 suite executions passed 2,724 test/subtest events with no skips.
Periodic, safemode and command race checks passed 1,105 events with no skips;
all 15 CLI scenarios passed. Nine compiler and nine runtime ownership cases
confirmed fixture cleanup without signaling foreign process groups. Root checks
reconciled the actual logs and archive against the frozen inputs, confirmed
cleanup, and passed all six postchecks. The extraction itself added no run-wide
thermal coverage or new catalog/report rows. Rebuilt-image and physical
qualification remain open.

The qualified run-wide increment implemented sampled thermal supervision in the
safe quick session and introduced `hhsoft.pc-tester.quick-session/v4` with embedded
`hhsoft.pc-tester.run-thermal/v1`. It retained ten worker outcomes and the existing
catalog. Initial thermal completion precedes execution and preparation, even
with all tests excluded. Serialized one-second sampling continues through all
workers, input cleanup and final inventory, with a one-second reading deadline
and a 2,500-millisecond maximum gap. A final observation is required after
inventory preparation. The local CPU/RAM guard retains its separate
250-millisecond wait between readings.

A one-use in-process final gate obtains pre-sign safety, requests the final
thermal observation, and synchronously joins both session monitors under one
common allowance of at most two seconds. Normal final observation and joins
fit within the 30-second preparation/finalization bound. An earlier error has
one common two-second cleanup allowance. Monitoring failure blocks signing;
heat cancels work while monitoring continues and remains reportable after clean
joins. Late heat determines the final signed stop reason and readiness marker,
without changing a valid worker failure or conflating heat with graph integrity.
The monitored boundary excludes subsequent report encoding, output and saving.

Journal v4 discloses available, partial or unavailable telemetry without
promoting absent readings to a thermal pass. Local CPU/RAM heat is retained even
when the global sampler has no sensors. Each local source binds its execution
interval, original file bytes and canonical full typed worker evidence. The live
finalizer rechecks both digests; the offline verifier recomputes the typed digest
and checks the original-file digest's format. Shared validators reject false
timelines, missing joins, oversized nested journals and inconsistent source
bindings. Historical signed evidence retains its previous interpretation.

Shared-evidence and command acceptance tests exercise the real
coordinator, thermal and safemode adapters, finalizer, signed-bundle verifier
and shared report consumer with synthetic observations. Cases include initial
heat and all-off runs; heat during every worker, input cleanup, final inventory
and pre-sign safety; cooling after a latched finding; retained local failures;
changed, cooler, disappeared or reformatted evidence; common monitor deadlines;
failure during cleanup; duplicate/missing gates; and typed readiness mismatches.

Full Windows qualification passed 1,754 test/subtest events across 22 packages,
with 21 platform skips and no failures. Vet, cross-builds of 24 Linux test binaries
and all five application targets passed. Native Linux amd64/386 qualification
then passed 3,032 events across 24 suite runs. The periodic, safemode, thermal and
command race suites passed 1,203 events. Native and race runs had no skips.
All 15 CLI cases passed: twelve session scenarios and three guided-pointer
scenarios across the modern and legacy x86 applications. Nine compiler ownership
and nine runtime ownership cases, 32 archive cases and 13 shell-supervisor cases
passed with confirmed cleanup. All six postchecks passed and all 301 input pins
remained unchanged. Verbose log counts and retained receipts were independently
reconciled.

The first native attempt failed because the harness launched a test binary
outside its package directory, so relative fixture paths did not resolve. That
failure evidence was preserved. A fresh attempt using the correct package
working directories passed without changes to product source.

All twelve CLI session cases reported `thermal_coverage=unavailable`, matching
the VM's lack of temperature telemetry. All-off sessions retained their required
initial and final observations; each deadline session retained four observations,
including two periodic samples. Injected heat exercises the real software
coordinator and finalizer, but does not establish physical cooling performance.
These native runs cover Linux amd64 and 386; AArch64 was cross-built only. Rebuilt
images, representative physical sensors, the beta pilot and release gates remain
open.

The guided-keyboard increment completed software qualification on 2026-10-26.
It activates definition version 2 of `input.keyboard.quick`, requires fresh
frozen plans, and introduces session v5
with eleven ordered workers. Keyboard runs between input readiness and pointer
testing. Selection offers eleven modern and nine legacy quick tests, subject
to applicability. The catalog remains 21 rows plus run integrity, producing 22
report results. These changes do not relabel any preceding v4 or earlier
validation results as v5 evidence.

The bounded check requires a press and release of left Shift, right Shift, Up,
Down, Left and Right on every applicable keyboard interface. It waits for a
neutral baseline and accepts complete event frames only. Every retained device
pass requires final idle, neutral and identity checks before its descriptor
closes, including when cancellation leaves other devices incomplete. One
interface cannot supply another's actions. Evidence records fixed completion
booleans, not typed text, raw input events or their sequence. The default window
is 30 seconds within a 90-second command limit and the remaining five-minute
session budget. Full layout, rollover, ghosting and other keys remain open.

The session's single console owner handles Ctrl-C and discards queued keyboard
input before later display findings. Ordinary console failures block success
with exit 1 after confirmed cleanup. Unconfirmed descriptor or console cleanup
forces exit 12, even after another failure or failed diagnostic output. A later
successful cleanup attempt cannot erase an earlier uncertain receipt.

Shared decoding binds functional evidence to v5 worker status and timing;
finalization additionally binds each retained device pass to the unchanged
frozen component and final inventory. Actual keyboard observations require the
current live finalization gate. Without a session journal, standalone
frozen-plan finalization accepts only exact nonexecution notes for `excluded`,
`not_present`, `unsupported` or `missing_driver` keyboards. Such notes contain
no functional observations. Original v4 journals retain their strict ten-worker
rules; v1-v3 evidence keeps its historical interpretation. Duplicate, escaped,
case-aliased or hidden modern claims cannot use a historical or viewless-bundle
bypass. Direct capture still omits guided keyboard and pointer workers.

Windows qualification with Go 1.27.2 passed 2,156 test/subtest events across 23
packages, with 21 platform-specific skips and no failures. Vet, 26 Linux test
binary cross-builds and all five application targets passed. Native Linux
amd64/386 qualification passed 3,784 events across 26 suite runs; five race
suites passed 1,461 events, with no skips. All 21 CLI scenarios passed: twelve
session, three pointer, three standalone keyboard and three keyboard-session
cases. Owned-process checks passed nine compiler and nine runtime cases with
confirmed teardown; all 32 archive and 13 supervisor checks passed.

The three standalone keyboard cases observed an interface but no completed
press/release pairs and correctly remained inconclusive at the observation
timeout. The three keyboard-session cases retained an error at the run deadline,
with no completed pairs. Their terminal state and descriptor flags were restored,
children were reaped and cleanup was confirmed. These expected outcomes validate
bounded behavior; they do not establish successful physical keyboard actions.

All fifteen session cases reported unavailable temperature telemetry. All-off
cases retained initial and final observations; run-deadline cases retained four
observations, including two periodic samples. No VM result establishes safe
physical temperatures.

Earlier unsuccessful native attempts remain recorded separately. The positive
heat fixture now waits for a second serialized safety callback, which proves
the first safe completion was accepted before heat is induced. Fake-clock
success cases wait for the exact next timer to be registered before advancing
time. Independent counterexamples demonstrate the timing assumptions; the
original native interleavings were not recorded, so their precise causes remain
unconfirmed. A separate negative requires zero samples at the first interval to
retain a sampling-gap failure despite confirmed cleanup. Production timing,
coverage, cancellation, cleanup and signing guards were not relaxed.

These results cover Linux amd64 and 386; AArch64 was cross-built only. Fresh
construction and qualification of all four beta images, physical multi-keyboard
and held-key/disconnect checks, and the pilot on at least ten varied PCs remain
open. They do not replace or expand the frozen initial alpha candidate's
separate source, image and release gates. Full layouts, rollover, ghosting and
other keyboard functions remain planned.

The subsequent keyboard acquisition increment preserves functional evidence
v1, definition 2, the six controls and session v5 policy. Registry reads retain
their 1 MiB limit and selected identity reads their 128-byte limit, with bounded
completion checks. Device type, number, containment and metadata identity must
be established before readable event access. Malformed providers, typed nils,
handles returned with errors, invalid progress and uncertain closure cannot
silently produce usable input.

Hard acquisition, protocol, read or identity faults survive later cancellation
through the runner and command consumers. A clean stopped-context read remains
interruption; an invalid count or an error joined with a hard fault does not.
Unconfirmed owned-descriptor or console cleanup retains exit 12 even when
diagnostic output also fails. Earlier completed rows keep a pass only after the
existing final idle, neutral and identity proof. A final zero-byte retry means
idle was not proved; it does not invent a hard fault. Bounded temporary metadata
reads for an already-selected interface may complete that proof after a stop,
with checked closure, no new event endpoint and no additional action credit.

The isolated Windows suite passed 4,471 test/subtest outcomes across 29 packages,
with 17 existing test skips and one empty package. All 86 selected host roots
and their 385 outcomes passed: 65 belong to eleven new roots and 54 children,
and 320 to retained coverage. Formatting, vet and application cross-builds for
Linux amd64, 386 and arm64 passed. The separate selected-test build produced
four test binaries and two test2json tools through fourteen commands and eight
successful postchecks.

Native Linux amd64/386 execution passed 1,196 test/subtest outcomes across four
suites: 456 input-worker outcomes and 142 command outcomes on each architecture.
All 119 selected roots passed on each architecture, for 238 root executions,
with no failures or skips. Nine runtime-ownership checks, 32 archive checks and
six root postchecks passed with cleanup confirmed. Independent recounts agree
with the closed suite logs. These acquisition runs add no standalone CLI or
race qualification to the historical results above.

Fixtures exercise actual owned adapters and fixture-root rejection through
command/session consumers. Injected owner and event faults remain distinct from
literal evidence-composition tests. Scheduled-plan and live-session finalizer
checks preserve signed error evidence and reject unbound or incomplete retained
passes. Self-issued test bundles do not qualify provisioned report keys. The
fixtures create no evdev nodes and collect no real keys. Physical input, full
layout/rollover coverage, persistent device identity, total or peak allocation,
blocked-kernel deadlines, rebuilt images and release approval remain open.

The subsequent keyboard query-phase and report-value correction passed host
and selected native Linux software qualification with Go 1.27.2. A key-state
query can remove queued key events from the same evdev client. The correction
retains the initial neutral/queue-flush boundary before fresh action credit,
then makes no active post-baseline key-state query. A keyboard with complete
actions stays in the read loop while another keyboard is unfinished, so its
later malformed or lost events are not hidden by tentative completion.

Event completion does not establish final neutrality. A surviving candidate
must pass the bounded terminal identity, idle-read, complete-state, neutral and
identity proof, followed by checked closure. Terminal bytes supply no new action
credit. Queued input, a retry, nonneutral controls or a failed proof can revoke
completion without restarting the observation window. Hard faults and cleanup
uncertainty retain precedence over cancellation; cancellation or another
device's failure cannot become an aggregate pass merely because one device
retains diagnostic completion.

`SYN_REPORT` values are ignored at both baseline and active frame boundaries.
Positive, negative and signed-limit values are compatibility cases, not action
evidence. `SYN_DROPPED`, unknown synchronization codes, malformed key transitions,
partial frames and existing bounds retain their rejection rules. The six-control
criteria, evidence schema, acquisition ownership and session policy are
unchanged.

The same two new test files were run against the predecessor on Windows amd64.
All 80 required names completed: 48 passed and exactly the predicted 25 child
tests and seven parent roots failed. Each failed child reported its expected
assertion. With the corrected source, both focused Windows amd64 and 386 suites
passed 345 test/subtest outcomes across 69 roots. Each included all 150 selected
keyboard names across 25 roots and 125 parent intervals, including all 80 new
regression names, with no failures, skips, missing names or duplicates.

The full Windows amd64 suite passed 8,952 test/subtest outcomes with 18 permitted
skips. Its 32 packages included 31 passing packages and one package with no test
files. All 1,360 source roots and the separate 720-name storage-plus-keyboard
selection completed, including 619 selected parent intervals, 23 fuzz seed
children and 13 parallel children. This is seed-corpus execution, not a fuzzing
campaign. Whole-module vet passed. All nine validation commands and seven
postchecks passed; formatting preserved code tokens, literals and comments.

Linux amd64 and 386 each passed all 150 selected names across 25 roots and
125 direct-parent intervals. Each package completed with no failures, skips,
missing names, duplicates or extra outcomes. Nine runtime ownership fixtures,
32 archive cases and all six root postchecks passed. Source and transfer
closure, archive contents, EOF and cleanup checks reconciled. The three Linux
compile targets produced test artifacts; AArch64 was compiled but not executed.

The earlier host, native, race and CLI results above retain their historical
scope. This qualification exercises the injected queue model and software
contracts; it does not reproduce a deployed-kernel race or qualify physical
keyboards. Rebuilt images and the existing physical-PC pilot/release gates
remain open.

The pointer wheel-direction extension passed Windows and native Linux amd64/386
software qualification on 2026-10-26. Definition 2 and functional evidence v2 require
both directions of every advertised vertical/horizontal axis on each supported
relative pointer. High-resolution streams are preferred; their coarse companions
are accepted when advertised but are not counted again. Net-zero packets do not
credit a direction, and malformed packets cannot partially satisfy criteria.

Shared validation preserves the exact historical v1 wire shape and scope. New
v2 evidence is bound to the advertised wheel bits in final inventory and the
frozen plan, including retained device passes in partial results. Standalone
pointer finalization remains supported without session monitoring. V5 sessions
also bind worker status and duration; v4 cannot claim pointer definition 2.
Raw hidden, duplicate, aliased and viewless claims select strict validation.
The full host suite passed 2,364 test events across 24 packages, with 17
platform test skips and four packages without host test files. Vet, 28 Linux
test-binary builds and five application builds passed against the same source.
Independent review reproduced and verified the correction of an interrupted-read
bug: an interrupted syscall now retries without establishing queue idle, so stale
baseline input and queued event loss cannot earn a pass through that boundary.

Native Linux amd64/386 passed 4,114 test/subtest events across 28 suite runs.
The input, periodic, safemode, thermal and command race suites passed 1,508
events. Native and race runs had no skips. All 24 CLI cases passed: twelve
session lifecycle cases, six keyboard cases, and six pointer cases. The pointer
cases covered three standalone reports and three pointer-only signed v5 sessions
across modern x64/x86 and legacy x86 applications. Nine compiler ownership,
nine runtime ownership, 32 archive and 13 supervisor checks passed with confirmed
cleanup. Returned verbose logs, source bindings and all six final environment
checks were reconciled independently.

The pointer CLI cases observed two virtual interfaces, one supported for relative
actions, without supplied movement, button or wheel actions. The interfaces
advertised two wheel axes in total and selected no high-resolution stream.
Standalone results remained inconclusive; pointer-only sessions retained
deadline errors and clean teardown. These are successful workflow checks, not
successful physical actions or high-resolution hardware qualification.
Session thermal evidence honestly retained unavailable telemetry. AArch64 was
cross-built only for this increment, not executed natively.

The subsequent guided-pointer acquisition increment retains pointer evidence v2,
historical v1 verification, definition 2 and the existing session policy. Registry
reads admit at most 1 MiB and selected sysfs identity values at most 128 raw bytes,
with overflow probes and exact EOF. Root containment, final-component no-follow
and metadata identity checks establish eligibility before opening an event node
for reading. The readable descriptor must then match that identity. Owned
metadata, event and root handles have checked closure, including a returned
handle accompanied by an open error.

Hard acquisition, read, protocol, identity and close errors survive later
cancellation or timeout. Complete frames from a positive-count hard-error read
may remain diagnostic when cancellation has not already been observed; partial
packets add no committed counters and the affected row cannot pass. An earlier,
independently verified device pass can survive another device's later fault.
Each retained pass requires complete criteria, an idle queue and final identity;
later positive bytes revoke that row's pass until those checks succeed again.
Uncertain closure prevents an overall pass. Consumers retain honest error
results and reject caller-pass promotion without adding a global cleanup exit
status or the separate storage/network safety-worker policy.

The shared keyboard helper now has unconditional cleanup checks matching its
previously reachable mode; the unused alternative is removed. Retained path
adapters test that shared behavior, while new tests exercise actual pointer
acquisition through injected owned operations. A regular-file registry failure
also reaches the real producer and command/session/finalizer consumers. Injected
close faults and signed-consumer checks remain compositional. No real input
event nodes are created or collected by these fixtures.

Implementation, static review and isolated host validation are complete. The
Windows suite records 4,406 passing test/subtest outcomes across 29 packages,
with 17 existing test skips and one empty package. All 75 selected host roots
and their 320 outcomes pass, including ten new portable roots and 39 children.
Formatting, vet and Linux amd64/386/arm64 cross-builds also pass. These results
are bound to the same 468-file formatted source. Selected native Linux
amd64/386 qualification passed 848 test/subtest outcomes across four suites:
294 input-suite and 130 command-suite outcomes on each architecture. All 97
required roots passed on each architecture, for 194 executions, including the
seven new and fifteen retained Linux roots. There were no failures or skips;
nine ownership controls, 32 archive controls, all six final checks and confirmed
cleanup passed. These test-binary runs add no standalone CLI or race validation.
Physical input, aggregate allocation, blocked-kernel deadlines, rebuilt-image
and release qualification remain open.

All four fresh beta images, physical wheel and sensor checks, and the pilot on
at least ten varied PCs remain open. The frozen initial alpha candidate and its
separate source, image and release-approval gates are unchanged.

Storage readiness now binds the expected canonical unsigned 32-bit major/minor
identity to both pathname metadata and the opened block descriptor. Registry and
mount parsing reject numeric aliases and malformed identities. Duplicate device
numbers fail across the complete registry, including excluded interfaces.
Read-only opens use `O_NOFOLLOW`, and a later registry recheck must still match.
Each acquired descriptor gets one close attempt; failed closure remains an error.
Cancellation at the helper or caller boundary revokes the current observation's
readable flag and credit while retaining earlier unsafe mount observations.

Focused Windows checks passed five test/subtest events without skips or failures.
Independent simulated registry, metadata and descriptor cases passed 71 events
without failures, including stable wrong nodes, swaps, symlink insertion,
duplicate identities, high device-number bits, close failures and cancellation.
These cases exercised production decisions with inert device operations; they
did not execute Linux kernel device operations. Linux storage vet and amd64/386
test-binary cross-builds also passed. Full Windows qualification then passed
2,369 test/subtest events across 25 packages, with 17 platform test skips,
three packages without host test files and no failures. Vet, 28 Linux test-binary
builds and five application builds passed against the same source.

Native Linux amd64/386 qualification passed 4,220 test/subtest events across
28 suite runs, including 67 storage events on each architecture. The five
existing input, periodic, safemode, thermal and command
race suites passed 1,508 events; native and race runs had no skips. All 24 CLI
cases passed with confirmed cleanup. Nine compiler ownership, nine runtime
ownership, 32 archive and 13 supervisor checks passed, and all six final
environment checks were reconciled. Storage identity fixtures used simulated
metadata and descriptors without accessing physical disks or reading media
contents. AArch64 was cross-built only; these results establish no physical
storage qualification.

A subsequent mount-policy increment shares one bounded parser between runtime
safety and storage readiness. Empty observations and malformed mount records
are errors. Every mounted nonzero device number must exist in the complete
registry before any node open. Unsupported major-zero filesystems, including
Btrfs and FUSE, fail closed; writable overlays are rejected while the existing
read-only overlay policy remains unchanged. Already parsed writable counts
survive later malformed input, missing or incomplete registries and cancellation.

This increment passed 109 focused Windows test/subtest events and 139 simulated
Linux-function events. Independent review added 25 events, passing all 164
simulated events without failures. The added cases cover record-order and map
iteration independence, unknown anonymous disk-backed filesystems, retained
unsafe counts after malformed tails or missing registries, cancellation, and
valid versus empty diskless observations. The original identity, no-follow,
close and caller-cancellation logic remains unchanged. Linux vet and four
amd64/386 package test-binary cross-builds passed. These checks use inert metadata
and descriptors, not native kernel device operations. Full Windows qualification
passed 2,406 test/subtest events across 25 packages, with 17 platform test skips,
three packages without host test files and no failures. Vet, 28 Linux test-binary
builds and five application builds passed against the same source.

Fresh native Linux amd64/386 qualification of this increment passed 4,364
test/subtest events across 28 suite runs, including 102 storage events per
architecture. Five existing race suites passed 1,545 events; storage is not a
separate race suite in this cohort. Neither group had skips. All 24 CLI cases,
nine compiler ownership, nine runtime ownership, 32 archive and 13 supervisor
checks passed, with confirmed cleanup and all six final environment checks
reconciled. Storage fixtures still use simulated metadata and descriptors:
native execution does not establish physical-disk or media-read behavior.
AArch64 was cross-built only. Rebuilt-image and physical checks remain open.

The storage-readiness v1 schema, catalog and session journal are unchanged, and
the adapter performs no media-content reads or writes. Agreement between these
metadata observations does not establish persistent physical identity or detect
every replacement that reuses a kernel number. Health telemetry, sampled/full
reads and the broader storage matrix remain planned. Rebuilt-image, physical
replacement/cancellation, pilot and frozen-alpha release gates remain open.

The shared parser's mount-identity follow-up rejects a snapshot that assigns
different filesystem types to one canonical zero-major device, such as `0:31`
reported as both `tmpfs` and `proc`. Same-type aliases may differ in path,
mount-source name and access options, and valid diskless observations remain
supported. Writable block counts already observed survive a later conflict.
The existing input bounds, runtime-safety v1 and storage-readiness v1 evidence,
catalog and session versions are unchanged.

Focused Windows regressions passed 1,733 test/subtest events across eight
packages, with five platform test skips and no failures. The full Windows suite
passed 2,791 events across 25 packages, with 17 platform test skips, three
packages without host test files and no failures. Formatting, vet, 28 Linux
test-binary builds and five application builds passed against the same source.

Native Linux amd64/386 validation passed 5,072 test/subtest events across 28
suites. Six race suites passed 1,930 events; both groups had no skips or failures.
All 39 CLI cases passed, with confirmed cleanup and all six final environment
checks reconciled. Mount/storage fixtures use simulated metadata and descriptors;
these results establish neither loop backing nor physical-device or media-read
correctness. AArch64 was cross-built only. The broader storage, rebuilt-image,
physical-device, pilot and release gates remain open.

The bounded registry increment replaces whole-directory enumeration with
positive bounded requests through actual EOF. The existing limit still counts
all 256 admitted entries before media filtering; a 257th entry rejects the view
without truncation. Short successful batches require another read. Partial
errors, no progress and malformed iterator responses fail. Every acquired
directory closes once, and successful views retain lexical order before identity
processing. Read/admission failures and failed closure take priority over
simultaneous cancellation; cancellation alone aborts, including at the empty-view
caller boundary. Earlier writable counts, readiness-v1 JSON, diskless views and
normal sysfs links are preserved. The catalog, session versions and media-content
I/O boundary are unchanged.

Focused Windows regressions passed 1,790 test/subtest events across eight
packages, with five platform test skips and no failures. The full Windows suite
passed 2,848 events across 25 packages, with 17 platform test skips, three
packages without host test files and no failures. Formatting, vet, 28 Linux
test-binary builds and five application builds passed against the same source.

Native Linux amd64/386 validation passed 5,256 test/subtest events across 28
suites. Six race suites passed 1,930 events; both groups had no skips or failures.
All 39 CLI cases passed, with confirmed cleanup and all six final environment
checks reconciled. Real directory operations use controlled temporary fixtures;
block metadata and descriptors remain simulated. These results establish no
physical-device or media-read qualification. AArch64 was cross-built only.

The scalar-admission increment rejects unavailable or invalid size metadata for
supported non-partition media instead of classifying it as empty. Size, read-only,
removable and state reads use a fixed 257-byte buffer and admit at most 256 raw
bytes after exact EOF and confirmed closure. The opened descriptor must be a
regular metadata file before reading; normal sysfs links remain supported.
Required capacity must parse as decimal uint64 and satisfy the existing byte
capacity limits. An observed numeric zero retains the empty classification.

Optional partition capacity, flags and state retain their existing unknown-value
behavior on observation failure. Successfully read state text and unusable-state
handling remain intact. Required-capacity errors, known size overflow and every
scalar close failure survive simultaneous cancellation. Cancellation alone aborts
before the current row or eligibility counters are credited, before continuing
past an excluded interface and before the no-media conclusion. Earlier writable
counts and unsafe metadata prefixes remain visible. Mount, registry and node
identity checks, readiness-v1 JSON, catalog/session versions and the boundary
against media-content I/O are unchanged.

Focused Windows regressions passed 1,852 test/subtest events across eight
packages, with five platform test skips and no failures. The full Windows suite
passed 2,910 events across 25 packages, with 17 platform test skips, three
packages without host test files and no failures. Formatting, vet, 28 Linux
test-binary builds and five application builds passed against the same source.

Native Linux amd64/386 validation passed 5,538 test/subtest events across 28
suites. Six race suites passed 1,930 events; both groups had no skips or failures.
All 39 CLI cases passed, with confirmed cleanup and all six final environment
checks reconciled. Scalar file operations use controlled temporary fixtures;
block metadata and descriptors remain simulated. These results establish no
physical-device or media-read qualification. AArch64 was cross-built only.
Broader inventory completeness/error reporting and acquisition bounds,
sampled-media reads, rebuilt images, physical-device checks, the pilot and
release gates remain open.

The inventory enumeration increment now reads production directories in
positive bounded batches through actual EOF without silently truncating results.
Short successful batches continue; partial errors, joined EOF/errors, no progress
and oversized responses fail. Every acquired directory closes before names are
validated and sorted. Failed closure is a hard error; malformed, duplicate and
nil entries cannot become a successful partial snapshot. Only a sole clean
open-time absence is optional, preserving missing interfaces and diskless views.

Collection context and failure state are separate for each call. Providers retain
their original slices; validated names are cached before use. Cancellation is
checked around acquisition and processing, including nonmatching entries, parsed
records, nested USB scans, a final-item break and snapshot canonicalization.
Directory errors and failed closure remain failures during cancellation. Optional
file observation errors retain their existing unknown-value policy. Legacy
PathSource adapters still use their declared aggregate ReadDir operation.

Inventory and plan reject failed collection before output; capture rejects it
before workers and signing, and finalize rejects failed final inventory before
report creation. Positive controls reach valid output or verified signed reports.
The catalog, component identifiers and existing report/session schemas are unchanged.

Focused Windows validation passed 1,900 test/subtest events across eight packages,
with six platform test skips and no failures. Formatting and focused vet passed;
all 29 new portable test functions passed. The full host cohort passed 3,047
test/subtest events across 25 packages, with 17 platform test skips, three packages
without tests and no failures. Host vet passed; 28 Linux test binaries across
amd64 and 386 and five application targets built successfully. Source,
documentation and host artifacts passed the privacy check.

Native validation passed 5,816 test/subtest events across 28 suites, 2,106 race-test
events across seven suites and all 39 CLI cases, with no native/race skips or
failures and confirmed cleanup. All 31 new test functions passed on both amd64
and 386, including the two Linux-only directory tests. The 28 inventory functions
also passed in the inventory race suite, and the three command-boundary functions
passed in the command race suite. Source, payload and native/host artifacts were
checked together; all 573 files in the artifact privacy check passed.

The next inventory increment bounds scalar acquisition to 64 KiB (65,536 raw
bytes) per value, with one additional probe byte. Coverage includes every
declared attribute and direct scalar role through 105 category/role cases,
including discriminator reads. The owned
reader uses bounded positive requests, proves a regular file before reading and
requires exact EOF. Short reads continue; malformed counts, no progress, unknown
file type and overflow stop collection. Overflow and protocol failures retain
precedence over a simultaneous observation error or cancellation. Every acquired
handle closes once; failed closure takes precedence over earlier outcomes.

Ordinary open/read errors caused by absence, permission denial or unsupported
operations discard partial bytes and remain unknown. A failed Stat call remains
a hard acquisition error. Wrapped or joined EOF does not prove completion.
Cancellation is checked before admission, after observation and after closure.
Decorators retain their declared ReadFile dispatch; their aggregate allocation
happens before result admission. The per-value budget imposes no device-count
cutoff and preserves missing-interface and diskless behavior.

DRM connector observations read status and enabled fields; controller and render
observations read device numbers and PCI fields. Connector device links can lead
to directories instead of PCI attribute leaves. Category-specific reads preserve
strict failures on declared attributes, with regression coverage for plan output,
cancellation and identity behavior. Linux symlink cases passed native validation.

Focused Windows validation passed 2,215 test/subtest events across eight packages,
with six platform test skips and no failures. Formatting and focused vet passed;
all 26 new portable test functions passed. The checks cover all 105 scalar category/role cases,
325 distinct devices, repeated/concurrent collection and the four command
boundaries. Full Collect command cases exercise overflow, nonregular values and
cancellation before output or report work. Cleanup coverage combines injected
reader close failures with command propagation controls; it does not induce an
operating-system Close failure inside a command. The isolated artifact privacy
check passed all 397 files.

Full host validation passed 3,362 test/subtest events across 25 packages, with
17 platform test skips, three packages without tests and no failures. Host vet
passed; 28 Linux test binaries and five application targets built successfully.
All 26 new portable functions, 105 scalar category/role cases and 12 full-collection
command cases passed in this cohort. The host artifact privacy check passed all
487 files with zero findings.

Native Linux qualification passed 6,468 test/subtest events across 28 suites,
2,432 race-test events across seven suites and all 39 CLI cases. There were
no native/race failures or skips, and cleanup was confirmed. All 31 scalar
and DRM test functions, including five Linux-only functions, passed on
amd64 and 386 and in the corresponding race suites. The native artifact
privacy check passed all 584 files with zero findings.

Primary proc acquisition now bounds memory records to 256 KiB, CPU records
to 16 MiB and input-device records to 8 MiB. The owned reader requires a
regular file, exact EOF and successful closure, with one extra probe byte.
Buffers grow from a small initial allocation; decorator dispatch is
preserved. Focused validation of this later increment passed 2,309
test/subtest events across eight packages, with six platform skips and
no failures. Formatting, vet and all 15 new portable functions passed,
including 36 real command cases and four composed closure cases. The
407-file privacy check passed with zero findings.

Full host validation of the proc increment passed 3,456 test/subtest events
across 25 packages, with 17 platform test skips, three packages without tests
and no failures. Formatting and vet passed; 28 Linux test binaries and five
application targets built successfully. All 15 new portable functions passed,
including the 36 real command cases and four composed closure cases. The host
privacy check passed all 493 files with zero findings.

Native Linux qualification of the proc increment passed 6,660 test/subtest
events across 28 suites, 2,528 race-test events across seven suites and all
39 CLI cases. All 17 new functions, including two Linux-only functions,
passed on both amd64 and 386 and in their corresponding race suites.
There were no native/race failures or skips; cleanup and all six root
postchecks passed. The native artifact privacy check passed all 590
files with zero findings.

The later aggregate directory-budget implementation shares a 65,536-record and
8 MiB name-byte budget across every directory read in one inventory collection.
Each collection starts with fresh counters. Exact-limit EOF remains valid;
exhaustion returns the fixed error
`inventory: directory acquisition budget exceeded` and no partial snapshot.
Owned-handle closure and declared PathSource overrides remain checked.

The [reader](../internal/inventory/directory.go) bounds collector-owned entry
retention and admitted name bytes. It does not bound allocations inside source
or entry callbacks, opaque entry payloads, total peak heap use, or blocked kernel
calls. The [new regressions](../internal/inventory/directory_budget_test.go)
contain 10 roots and 39 root/subtest names, including aggregate, reset, EOF and
cleanup failures. The earlier host/native counts above do not qualify this
increment.

Windows amd64 and 386 inventory runs each passed 1,550 test/subtest cases
with one existing platform skip. All 39 new names passed in both runs and
in the full Windows suite, which passed 10,380 cases with 18 platform skips,
32 passing packages and one package without tests. Vet and Linux amd64,
386 and AArch64 production builds passed. This provides host test and
cross-build evidence; native Linux runtime, rebuilt-image and physical-device
qualification remain open.

Cumulative scalar/proc acquisition shares 65,536 file attempts and
64 MiB of raw bytes across all scalar and primary-proc reads in one collection.
Missing, empty and repeated attempts count. Raw bytes remain charged when an
observation is discarded or a read returns bytes with an error; normalization
does not refund them. Every collection receives fresh counters.

The [owned reader](../internal/inventory/acquisition.go) may consume at most
one additional, unretained byte to detect overflow. Exhaustion returns
`inventory: file acquisition budget exceeded` and no partial snapshot, with no
later source work. Existing per-file caps, exact EOF, malformed-read checks,
close-error precedence and declared source overrides remain mandatory. Legacy
aggregate providers may already have allocated before their results are checked.

Windows amd64 and 386 inventory runs each passed 1,611 test/subtest cases
with one existing platform skip. All 61 new names passed in both runs and
in the full Windows suite, which passed 10,441 cases with 18 platform skips,
32 passing packages and one package without tests. Vet and Linux amd64,
386 and AArch64 production builds passed. Native Linux runtime, rebuilt-image
and physical-device qualification remain open.

The proc parser-budget increment shares 65,536 raw record blocks, 262,144
field-work units and 16 MiB of cumulative retained-text accounting across CPU,
memory and proc input-device parsing in one collection. CPU/input raw blocks
include empty and trailing blocks; an observed memory file counts as one
record. Missing files still count toward acquisition attempts, not parser
records. Field work counts visited lines, including malformed or empty lines,
input identity tokens and value words; repeated normalization visits count
again. Duplicate, overwritten and discarded work receives no refund.

Text admission precedes selected lowercase-key, normalized-value and map
allocations. Input values retain their original spelling for classification.
CPU aliases and first component projections are charged again, including
generated IDs. Record admission precedes parsed record maps. Counters reset
for each collection. Exhaustion returns
`inventory: primary observation parser budget exceeded` and no partial
snapshot. Below-limit duplicate, malformed/truncated and index behavior,
source overrides, acquisition errors, checked closure and cancellation retain
their existing semantics.

Complete inventory-package tests passed on Windows amd64 and 386. Each run
recorded 1,673 passes and one retained platform skip,
`TestLinuxCollectorReadsInventoryFixture`. All 48 new names across 14 roots
and 34 declared-parent relations passed without skips on each architecture.
The fixtures exercise exact limits and one-over failures, raw-block and field
floods below acquisition caps, distinct-field and duplicate growth, retained
copies, aggregate CPU/memory/input exhaustion, fresh-call ownership,
cancellation at iterator exits, complete 325-instance inventories and no-drive
systems.

All nine commands passed, including formatting/checks for five Go files, both
package suites, vet and Linux amd64/386/AArch64 inventory test-binary builds.
The three Linux artifacts were compiled, not executed. All seven final
postchecks passed. These runs cover the 704-file inventory slice; they did
not execute the later combined source.

The combined 714-file source, including 698 Go files, then passed all three
host commands. The full Windows amd64 suite recorded 11,689 passes and 18
retained platform skips across 11,707 names. The Windows 386 affected-family
suite passed all 290 names across 55 roots, 235 declared-parent intervals
and five packages. Vet and all seven postchecks passed. The Windows 386
result covers only those selected families. This combined run comprised
the two Windows commands and vet; it added no Linux compile or runtime
evidence. Earlier component builds retain their compile-only scope.

A subsequent run built and executed ten test binaries from that same combined
source, covering the selected command, app, inventory, plan and session-evidence
fixtures on Linux amd64 and 386. All 580 named outcomes passed across 110 roots
and 470 source-declared parent intervals, without failures or skips. All nine
ownership cases, 32 archive cases and six postchecks passed, with cleanup
confirmed. Exact test names and parent intervals were reconciled in every
stream. This qualifies the selected connected Extended and proc parser
fixtures; it does not establish a full elapsed session or installed CLI behavior.

Remaining parser expansion, aggregate/peak allocation, CPU time budgets and
optional field-error visibility remain open. These logical limits exclude
allocator overhead, raw source storage, later downstream copies and provider
allocations; they do not establish a whole-heap bound or kernel preemption.
Cancellation cannot interrupt a blocked source call. This inventory increment
does not activate catalog entries. Broader Linux runtime, rebuilt images,
physical devices, sampled-media reads, the physical pilot and release approval
remain separate gates.

The inactive storage result reader validates storage-sample/v1 metadata and
explicit v6 session claims through the existing signed-report verification,
HTML/PDF and comparison paths. It checks full node/result correspondence,
typed backing and fixture facts, fixed sample geometry and recomputed accepted
counters. Required integer masks, exact fields, nullable objects and bounded
arrays reject ambiguous or hidden claims. Historical v4/v5 wire handling remains
supported; that reader increment retained the v5 emitter with eleven workers.
Storage sampling remains inactive in the current workflow.

Accepted failures survive later stops. Unaccepted observations remain diagnostic
and add no accepted work; an unfinished eligible target cannot claim clean
completion. Every retained passed device must match final inventory identity and
geometry, including within an overall non-pass result. A vanished nonaccepted
device may remain diagnostic. Cleanup uncertainty blocks report acceptance.
Snapshot and typed-result hashes bind signed values; millisecond fields establish
feasible intervals without proving precise producer timing, kernel behavior or
physical-device continuity.

The earlier admission and observation model passed 140 focused Windows
test/subtest events across 30 test functions, with no skips. Formatting and vet
passed using fixtures; no media was accessed. That evidence applies only to the
model cohort. The integrated reader passed 3,776 Windows test/subtest events
across 27 packages, including all 67 candidate test functions, with 17 existing
test skips and three empty packages. Formatting, vet and Linux amd64/386/arm64
cross-builds passed. These are host fixture and build checks, without media I/O.
A subsequent native run of that reader source passed 1,582 test/subtest
events across seven suites on each of Linux amd64 and 386, including all
67 candidate roots per architecture. All 62 verify/compare CLI cases
passed, with no failures or skips. Nine runtime ownership cases,
32 archive cases, all six final checks and cleanup passed. The CLI
subset covers provisioned signing trust and bundles with no views or
both views. It does not qualify other CLI trust/view combinations or
the later output-completion fault cases. Production sampling, loop
backing/lifetime safeguards, all four rebuilt images, physical checks
and release approval remain open.

The later storage sampling/discovery increment remains an isolated component
candidate. It implements finite read schedules and worker reply validation,
parent accounting and result projection, bounded topology/mount/swap parsing,
owned metadata capture, a held metadata scope and canonical-fact classification.
The live producer-to-media-worker-to-report route is not yet implemented.

The earlier 27-file candidate passed 773 test/subtest outcomes across two packages on
each of Windows amd64 and 386. Independent reconciliation matched all 495
selected names under 68 roots and 427 direct parent relationships on each
target, with no failures, skips, missing/extra names or duplicate lifecycles.
Formatting preserved tokens, literals and comments in every candidate file;
vet, all 13 host commands and seven postchecks passed. Both packages compiled
for Linux amd64, 386 and arm64; arm64 remains compile-only. Selected native
component and owned-path suites passed on Linux amd64 and 386, with 505 names
under 70 roots and 435 direct parent relationships per architecture. The four
suite runs produced 1,010 named passes, 140 root executions and 870 parent
relationships, with no failures, skips, missing/extra names or duplicate
lifecycles. Nine ownership fixtures, 32 archive checks and all six final checks
passed; cleanup and transfer EOF were confirmed.

`TestLinuxStorageMetadataFeedsWholeFamilyExclusions`,
`TestStorageMetadataRejectsOwnerAliases`,
`TestStorageMetadataScopeRejectsFinalChanges` and
`TestStorageClassificationUSBPrecedence` cover exclusion propagation, unique
owner closure, retained final-verification faults and transport/optical
classification. Complete topology inputs reject overflow beyond 256 nodes or
1,024 edges. Unknown swap, incomplete relations and loops fail closed. The
result and range-copy tests prevent caller callbacks from changing admitted
facts or converting diagnostic observations into accepted work.

The successor connects canonical-ancestor acquisition to the held metadata
scope and existing classifier. It admits finite relative class and subsystem
links, retains directory owners, checks registry/canonical device numbers and
reobserves links and classification facts before the owning call succeeds.
Final drift, cancellation or failed API closure withholds the result. Returned
classification remains descriptive: original boot backing is still unknown.
Link-text limits apply after SDK Readlink returns; they do not bound its internal
allocation or make kernel calls preemptible.

The current 31-file candidate passed 848 test/subtest outcomes and all 119
package roots across two packages on each of Windows amd64 and 386. Independent
reconciliation matched exactly 570 selected names under 76 roots and 494 direct
parent relationships per target, with no failures, skips, missing/extra names
or duplicates. All 30 Go candidates preserved tokens, literals and comments
through formatting; the approved module bytes were unchanged. Focused vet,
all 13 commands and seven postchecks passed. Six Linux test binaries compiled
for amd64, 386 and arm64. The module declares Go 1.25.0 for the Root.Readlink
API. Validation used Go 1.27.2 and provides no execution qualification on
Go 1.25.

The additional full-module suite and vet passed on Windows amd64: 8,873
test/subtest passes and 18 allowed skips, with 31 package passes and one package
without tests. All 32 packages and 1,349 source-required roots were accounted
for; all observed lifecycles closed. The selected 570-name cohort, 23 fuzz seed
children and 13 parallel children matched their required lifecycles. This was
seed execution, not a fuzzing campaign.

The current selected Linux cohort then passed on both amd64 and 386. Each
architecture produced exactly 667 named outcomes under 91 roots and 576 direct
parent relationships across two packages, without failures, skips, missing or
extra names, or duplicate lifecycles. The four package runs totalled 1,334
passes. Nine ownership fixtures, 32 archive checks and six final checks passed;
cleanup and transfer EOF were confirmed. Linux arm64 remains compile-only.
These native runs qualify the selected owned-fixture cohort, not the Linux
full-module suite or any real-media sampler.

The scoped source audit found complete internal component bodies and their
fixture callers, with no implementation placeholder markers. It did not find
a live sampler entry. Descriptive acquisition is connected; authority to sample
media is still absent.
The remaining route needs an authentic protected early boot-source handoff,
continuous target/backing ownership and actual supervised worker/CLI composition.
Boot and kernel-owner proposals are design evidence only. Unknown original
boot backing cannot become an empty exclusion set, and equal snapshots cannot
substitute for continuous ownership. Discovery/backing completeness remains
ungranted and `storage.quick` remains inactive. These component and owned-fixture
checks do not qualify real or physical media, underlying kernel close failures,
hard preemption, adversarial namespaces, race, durability, all four rebuilt
images, the physical-PC pilot or release readiness.

CLI output completion requires successful writes and successful close of
each owned destination. Short writes, including a final newline, fail;
write and close errors remain discoverable together. Exclusive creation
uses mode 0600 and leaves existing destinations untouched. Caller-owned
stdout is never closed. Worker cleanup uncertainty retains exit status 12
even when output also fails, with both diagnostics preserved; the
finalizer success banner follows successful close. A failed output may
remain as a partial file. Close success alone proves neither fsync nor
durability. The isolated candidate passed 3,878 Windows test/subtest
events across 27 packages, all nine new functions and 93 child cases,
with 17 existing test skips and three empty packages. Formatting, vet
and Linux amd64/386/arm64 builds passed. Those host results alone did not
qualify native output faults.

The selected native output-completion cohort subsequently passed 204
test/subtest outcomes across two Linux amd64/386 suites. All nine roots
and 93 named children passed on each architecture, giving 18 root
executions, with no failures or skips. Nine runtime ownership cases,
32 archive checks and all six final checks passed with cleanup confirmed.
This evidence covers injected open, write, short-write, close and combined
failures, owned-handle completion and caller-owned stdout. Temporary-file
fixtures separately verify exclusive creation, Linux mode-0600 permissions
and retention of existing content or partial output. The command tests invoke
controlled command functions; no standalone installed-CLI scenario is qualified.
Injected Close errors do not establish kernel-close or fsync-fault behavior.
The existing durability, rebuilt-image, physical-PC and release qualification
gates remain open.

The audio readiness acquisition increment keeps metadata-only operation and
the then-current result schema, catalog and eleven-worker v5 session. Registry
enumeration uses positive batches of at most 32 entries, with a 4,096-entry
limit and one overflow witness. Names are read once, validated, cached and
sorted. Attribute reads admit at most 64 raw bytes with one overflow byte,
bounded requests and exact EOF. Invalid counts, no progress, partial errors,
unsafe entries cannot produce readiness credit. Unconfirmed closure cannot
yield an overall readiness pass; prior Ready rows remain diagnostic.

Root containment and ordinary contained sysfs links remain supported. Final
attribute components use no-follow metadata handles; the readable descriptor
must identify the same regular file. Audio device nodes are inspected through
metadata only. An absent registry remains no-device evidence, and
an absent node remains a missing-node diagnostic; joined errors, wrong-phase
errors and handles returned alongside an error do not gain that exception.
Every acquired root, directory and attribute handle receives one close attempt.
Close failure takes precedence over cancellation, while cancellation after
clean final closure revokes tentative success. Earlier complete rows and
counters remain diagnostic after a later fault, without acquiring later endpoint
metadata.

Audio errors retain the existing reportable-result policy: they do not by
themselves stop unrelated session workers or prohibit signing. Aborted audio
work follows the generic session stop handling. No global cleanup exit status
is added, and consumers cannot promote error or aborted evidence to a pass.
Injected operating-system close faults and command/session/finalizer cases
establish separate, compositional boundaries; they do not claim one induced
Close fault ran through every layer in the same invocation. Real producer
fixtures cover absence, missing nodes, attribute overflow, final-component
symlinks and cancellation.

The closed Windows host run passed 4,171 test/subtest events across 29 packages,
with 17 existing test skips and one empty package. All twelve new portable
functions and their 62 named children passed. Formatting, vet and whole-source
Linux amd64, 386 and arm64 builds passed, with all eight final checks true.
The actual formatted candidate contains 448 source files, against the prior
442-file baseline. These host and cross-build results remain distinct from
the selected native evidence below.

Selected native Linux amd64 and 386 qualification passed 438 test/subtest
events across four suites: 194 audio events and 25 command events on each
architecture. All 41 required functions per architecture passed, including
22 new functions and 19 retained Linux functions, for 82 required executions.
There were no failures or skips; retained device-node fixtures therefore have
explicit executed outcomes. The host's non-Linux cancellation implementation
does not supply this Linux credit. Nine ownership controls, 32 archive
controls and six final checks passed, with owned-process cleanup confirmed.
No standalone CLI scenarios or race run were included in this native cohort.
This evidence is bound to the same 448-source-file cohort and uses fixtures;
it does not qualify physical devices or playback/capture. Blocked-kernel
deadlines, channels, volume, jack behavior, rebuilt-image, physical-hardware
and release qualification remain separate open gates.

The input-readiness acquisition candidate preserves its existing evidence
schema, catalog and session policy. Registry acquisition admits at most 1 MiB
of raw data with one overflow byte, 4,096-byte requests, 65,536-byte lines,
32,768 lines and 2,048 nonempty records, including irrelevant records. The
existing 128 selected-interface limit remains fail closed. Selected device
identity reads admit 128 raw bytes with bounded requests and exact EOF.

The selected sysfs device number must be eligible for input events and match
the node; it is never calculated from the event index. Root containment,
contained sysfs links, final-component no-follow and metadata/readable-descriptor
identity checks precede acceptance. Readiness closes each event's owned handles
before granting readable credit. Activity uses the same bound descriptors,
rejects incomplete or faulty event reads, and exports aggregate counters only.
These checks do not prove persistent physical identity or interruptible kernel
operations.

Only a sole expected missing or permission error without an acquired handle
can be ordinary unavailability at a device boundary. Mandatory registry and
identity acquisition, malformed providers, substitutions and uncertain closure
remain hard errors. A hard error survives later cancellation; cancellation after
otherwise complete work prevents stale success. Earlier accepted rows remain
diagnostic. Input errors remain reportable, aborted work follows the existing
session stop policy, and consumers reject caller-pass promotion. No new global
cleanup exit status is introduced.

The closed Windows host run records 4,205 passing test/subtest events across
29 packages, with 17 existing test skips and one empty package. All ten new
portable roots and their 24 named children pass. Formatting, vet and complete
Linux amd64/386/arm64 cross-builds also pass; cross-builds do not execute Linux
tests.

Selected Linux amd64/386 execution records 364 passing test/subtest events
across four suites: 163 input and 19 command events on each architecture. All
35 required roots run on each architecture (70 executions), without failures
or skips. Nine ownership controls, 32 archive controls and six final checks
pass with confirmed cleanup. This is the selected fixture cohort, not a full
native run of every package or standalone CLI scenario.

Nominal ownership and activity cases use injected handles. Real producer
fixtures use regular files, links and missing paths, never input device nodes
or real input collection. Producer close faults and signed consumer checks
remain compositional. Physical input, broader guided keyboard/pointer coverage,
blocked-kernel deadlines, rebuilt-image and release gates remain open.

The network acquisition path adds bounded discovery while retaining
the existing result schema, then-current v5 emission and catalog. Registry reads
request at most 32 entries per call and enforce the existing 128-entry
limit with one overflow witness. Names are validated, cached and sorted;
ordinary sysfs directory links remain usable. Scalar bodies admit 256 raw
bytes, and driver-link and fallback metadata bodies admit 4,096 bytes,
each with a bounded overflow check. Scalar and fallback reads require exact
EOF, regular-descriptor proof and successful owned closure.

Optional absence requires a sole open-time missing-file error without an
acquired handle. Only carrier, speed and duplex may additionally treat a
sole first read returning zero bytes with `EINVAL` as unavailable. Open,
stat, partial-read, later-read, joined-error and close faults do not receive
that exception. Successfully acquired malformed optional values retain
their earlier unknown-value behavior. Close failure remains an error during
cancellation; cancellation after clean closure revokes current credit and
stops later acquisition. Earlier completed interface rows remain diagnostic
when a later interface fails.

Driver lookup uses a bounded link read, falling back to bounded metadata
only for an absent link or an invalid driver basename in a successfully
acquired target. NUL, overlong targets and acquisition faults remain hard
errors. The production syscall adapter handles the actual negative count
returned with a readlink error without weakening generic invalid-count
rejection. The frozen tests include real-syscall missing-link fallback,
missing-both cases, retained-prefix faults and consumer status checks.
Network errors and aborted results stop the session safety sequence before
later workers and signing. Standalone finalization must retain diagnostic
status and reject a caller's unsupported pass claim.

This increment has 17 new test functions, ten portable and seven Linux-only,
plus four retained Linux functions. The corrected isolated Windows run
passed 4,097 test/subtest events across 29 packages, with 17 existing test
skips and one empty package. All ten portable functions and their 87 named
children passed. The four standalone-finalizer status cases preserve signed
diagnostics; intended caller-pass rejection checks also passed within those
cases and the retained-prefix case. These fixtures use self-issued bundle
verification and remain compositional rather than provisioned-key evidence.

Formatting, vet, Linux amd64/386/arm64 whole-source builds and all eight
postchecks passed. A separate pinned local Go 1.27.2 build produced four
selected Linux test binaries and two test2json tools; all 14 commands and
seven postchecks passed. This binds the local SDK closure without claiming
upstream or release-toolchain qualification.

The matching native run passed 404 test/subtest events across four suites:
177 network events and 25 command events on each of Linux amd64 and 386.
All 21 required functions passed on each architecture, including the ten
portable, seven new Linux and four retained Linux functions, with zero
failures or skips. Missing-link fallback, missing-both and retained-prefix
read, close and close-plus-cancel cases passed on both architectures. Nine
owned-process cases, 32 archive cases and all six root postchecks passed
with cleanup confirmed. These suites use injected acquisition faults and
owned temporary fixtures; they do not qualify standalone CLI runs, races,
real network hardware or functional traffic.

Historical USB validation below is separate from this network increment.
Functional traffic, blocked-kernel deadlines, total/peak allocation,
persistent identity, rebuilt-image, physical-PC and release qualification
remain open.

USB readiness now binds each authorized device to its selected registry
device number, bus/device address and authorization. It rejects duplicate
observed device numbers or addresses before opening nodes, then checks direct
pathname, owned descriptor and final pathname identity. It rechecks the
registry association and requires successful close before readable credit.
A close failure remains an error even with cancellation; later USB opens
stop and earlier diagnostics remain. Ordinary unavailable or replaced
nodes remain inconclusive. The existing schema, then-current v5 emitter and
catalog are unchanged, with no USB global cleanup exit or new transfers.

The acquisition increment extends the same owned reader to every USB
readiness scalar: 256 raw bytes plus one overflow probe, exact EOF,
regular-file proof and one close attempt per acquired handle. This includes
vendor presence, initial bus/device/authorization and optional class,
speed and port observations. Class is read once and retains prior numeric
spellings. Vendor presence now requires a successful bounded read.
Only a sole open-time absence is optional; successfully read malformed
optional values remain unknown. Permission, read, protocol, type, overflow
and close failures remain errors, including during cancellation.

Registry enumeration requests at most 64 entries at a time, admitting at
most 2,048 raw entries with one extra overflow witness. The separate
512-device limit remains. Names are cached and validated without following
entry links; complete EOF and directory closure precede attribute reads.
Oversized or incomplete input cannot become a truncated readiness pass.

The isolated Windows run of this acquisition increment passed 4,000
test/subtest events across 28 packages, with 17 existing test skips and two
empty packages. All seven new portable functions and their 60 named child
cases passed. Formatting, vet, Linux amd64/386/arm64 whole-source builds
and all eight postchecks passed. A separate pinned local Go 1.27.2 build
produced four selected Linux test binaries and two test2json tools, with
all 14 commands and seven postchecks passing. This checks the local SDK
closure, not upstream or release-toolchain qualification.

The matching native run passed 640 test/subtest events across four suites:
290 USB events and 30 command events on each of Linux amd64 and 386.
All 44 required functions passed on each architecture, including the 18
acquisition functions and all 26 retained functions, with zero skips or
failures. Both mandatory-discovery tail cancellation cases passed on each
architecture. Nine owned-process cases, 32 archive cases and all six root
postchecks passed with cleanup confirmed. These tests use injected device
observations and owned temporary fixtures; they do not supply standalone
CLI, race, real USB-device or functional-transfer evidence.

Host validation of the preceding USB identity/closure correction passed
3,933 tests across 28
packages, with 17 existing test skips and two empty packages. All eight new
portable test functions passed, together with formatting, vet and Linux
amd64, 386 and arm64 whole-source builds. A separate pinned local Go 1.27.2
cross-test build produced four test binaries and two test2json tools for
amd64 and 386; all 14 build/metadata commands and seven postchecks passed.
This verifies the local SDK closure, not upstream or release-toolchain
qualification.

Selected native execution of that preceding correction passed 296 test/subtest
events across four suites: 126 USB events and 22 command events on each
of Linux amd64 and 386. All 26 required test functions passed on each
architecture, with zero skips or failures. Nine owned-process cases,
32 archive cases and six root postchecks passed with cleanup confirmed.
The cohort runs the selected USB and command tests with injected device
observations and owned temporary fixtures. It supplies no standalone CLI,
race, real USB-device or functional-transfer evidence. Rebuilt images,
physical USB ports and release approval require separate evidence. Total
acquisition/allocation, blocked-kernel deadlines and enduring physical
identity also remain open.

The next CPU increment implements twelve fixed binary64 addition, subtraction,
multiplication and division vectors in the bounded worker. CPU v3 evidence adds
ordered per-worker counts, exact mismatch bits and separate integer, SHA-256 and
scalar failure flags. That CPU definition-2 increment used the existing v5 journal and
22-row report. Historical CPU v2 hashes and definition 1 remain valid under their
original strict v4/v5 rules; that CPU increment left memory evidence unchanged.

Affected-package Windows regression tests passed 1,788 test/subtest events across
nine packages, with 16 platform test skips and no failures. Cases cover cadence,
late work, distinct failure workers, counter overflow, historical typed hashes,
version pairing, no-view verification and HTML/PDF summaries. Completed-result
fixtures with first, last and later-suite faults on a nonzero worker cross the
real session coordinator and finalizer, including operator cancellation and local
heat. Separate producer tests exercise the operation and worker-coordination
boundaries. Full host validation passed 2,543 test/subtest events across 25
packages, with 17 platform test skips and three packages without host tests.
Vet, 28 Linux test-binary builds and five application builds passed. The 28
native Linux amd64/386 suites passed 4,608 events; six race suites passed 1,745
events, with no native skips. All 30 CLI scenarios passed, including six new
standalone/session CPU capture and signed-finalization cases across amd64, x86
SSE2 and legacy x86 soft-float. Fresh beta images and physical CPU checks
remain open. The fixed suite does not establish wider instruction coverage or
stress/performance behavior; legacy soft-float validates software arithmetic,
not hardware FPU execution. The frozen initial alpha candidate and its release
gates are unchanged.

The subsequent RAM increment adds `0xaa` initialization, ascending verify/invert
to `0x55`, descending verify/invert to `0xaa`, and final ascending verification
after the three existing patterns. Each pass has ten phases and six logical
read and write sweeps. Definition 2/memory v3 records partial phases, uncredited
terminal work and explicit mismatch presence, including offset zero. Historical
memory v2 and nested safety preserve their canonical and typed hashes. The v5
journal keeps eleven workers and the full report has 22 rows; direct capture
keeps its seven-row subset.

Final focused Windows checks passed 2,082 test/subtest events across nine
packages, with sixteen platform test skips and no failures. Independent phase
and corruption oracles cover both directions, late mismatch, provisional
completion, partial/full uncredited chunks, overflow and malformed availability.
Real 4 MiB runs exercise capture and session/finalization; completed-result fault
fixtures exercise signed failure paths separately. An earlier focused attempt
retained a stale three-read-sweep expectation; its corrected assertion requires
all six sweeps without changing production guards.

Full Windows validation passed 2,755 test/subtest events across 25 packages,
with seventeen platform test skips, three packages without host tests and no
failures. Vet, 28 Linux test-binary cross-builds and all five application targets
passed. Inspection of amd64, x86 SSE2 and legacy x86 application builds confirmed
a separate non-inlined kernel with buffer loads, comparisons, stores and reverse
indexing. This does not establish physical DRAM traffic, cache bypass or actual
hardware-fault detection.

Native Linux validation passed 5,000 test/subtest events across 28 amd64/386
suites, with no skipped tests or failures. Six race suites passed 1,894 events
without skips or failures. All 39 CLI cases passed across amd64, x86 SSE2 and
legacy x86, including nine RAM cases for standalone execution and signed
finalization, memory-only sessions and direct capture. Modern standalone/session
cases completed 64 MiB/two passes; legacy cases completed 16 MiB/one pass.
Direct-capture cases completed 4 MiB/one pass. Cleanup was confirmed and thermal
coverage remains explicitly unavailable in these VMs.

The preceding native attempt is retained as failed after the command race
package exhausted its 120-second qualification budget. The successful fresh run
uses 180 seconds for that package and a 210-second supervisor limit; the other
race budgets and the product's 30-second RAM worker limit are unchanged.

AArch64 was cross-built only. All four rebuilt beta images, physical-memory
validation, the ten-PC pilot, broader RAM/SPD/ECC/stress coverage and Memtest86+
remain open. Frozen alpha release gates are unchanged.

Regression coverage for these cases belongs with the corresponding feature,
not with a blanket claim of complete product coverage. Linux mount/device
fixtures and real APK selection require the appropriate Linux environment;
Windows compilation alone does not execute them. Real signature verification
uses disposable keys and an isolated keyring.

Report mounting reads only the first 2 KiB of the authorized partition to
recognize an unambiguous `vfat`, `exfat`, or `ext4` header before requesting a
writable mount. Ext4 must use extents; any journal must be internal, and stored
mount-option overrides are rejected. This probe does not establish filesystem
integrity or media health. The kernel must still accept the explicit filesystem
type. The mount uses bound device/directory descriptors, repeated authorization
checks, and `nodev,nosuid,noexec`. Linux fixtures exercise pathname replacement
and authorization expiry without mounting real media. They do not establish
immunity to physical removal, device reuse, or other hardware hot-plug races.

Direct quick capture and worker finalization validate evidence before creating
signed results. Schema validation rejects missing required fields, duplicate or
unknown fields, and ambiguous field spelling. Semantic validation requires work
and observations sufficient for the claimed bounded check: for example, CPU
known answers, complete RAM operation counts, consistent readiness inventories,
and no contradictory storage writes or network traffic. Valid partial non-pass
observations remain reportable. A supervisor-recorded error or abort can produce
an explicit execution note when usable worker evidence is unavailable; it cannot
become a pass or discard a valid worker failure. CPU, memory, graphics, and
display failures cannot be replaced by a supervisor error or abort. A RAM byte
mismatch, including at offset zero, cannot be labeled inconclusive, error, or
aborted by either direct capture or finalization. Explicit renderer or pattern
failures also require an overall graphics failure. Matching failure and partial
results retain their original evidence in the signed report. These checks do
not upgrade readiness into physical qualification.
The standalone report verifier checks bundle integrity and signatures. Strict
supported session claims additionally require the shared journal and local-thermal
evidence checks described above; functional keyboard claims also require their shared
decoder and final-inventory bindings. Verification does not rerun hardware
tests or establish physical truth.

Both appliance startup scripts check mount/network safety before handing the
frozen plan to the quick-session coordinator and again before announcing a ready
report. The coordinator independently observes safety before work, before every
started worker, and after cleanup. The live final gate adds a fresh pre-sign
observation and binds the completed journal in signed run-integrity evidence.
Writable block mounts,
active non-loopback interfaces, contradictory details, or incomplete observations
block signing and the normal ready/save path while retaining RAM diagnostics.
Device numbers identify mapped and aliased mounts even without a storage test;
software interfaces are checked without a hardware readiness provider. Selecting
no quick tests still requires the safety observations and preserves them in the
signed session history. The extra shell checks remain separate operational
gates. Passing graph integrity establishes an unchanged observed graph, not
completed hardware-test coverage.

The coordinator enforces the shared run deadline and each frozen worker's
complete deadline. Graphics tools share one 30-second limit; display cards,
patterns, and findings share one 45-second limit. Ctrl-C and Linux SIGINT/SIGTERM
stop new work, preserve completed failures and exclusions, and record unfinished
scheduled tests. Console input must finish and restore its state before fresh,
bounded final safety and finalization contexts begin. Unsafe, interrupted, or
incomplete network/storage readiness cannot authorize signing. Unconfirmed
worker cleanup also blocks finalization. The terminal shell guard prevents
ready/save after any nonzero session exit, even if a report already exists.

The quick-session and pointer milestones above validate this cooperative
orchestration and its bounded input path. These snapshots and
deadlines do not establish continuous thermal/heartbeat monitoring, preemption
of an uninterruptible kernel operation, containment of escaped process groups,
or the still-planned privilege separation and broader profile workflows.

Display regressions cover a successful pattern alongside another output's
modeset failure, failed/error/aborted/inconclusive/skipped patterns in either
order, mixed operator findings, and an absent display. Passing observations for
one output cannot erase another output's failure or incomplete result. A display
pass requires every applicable pattern and all seven operator criteria for each
successfully patterned connector to pass. These fixtures do not replace real
multi-display testing or operator observation of pixels, color, and artifacts.

The hardware matrix in [test-strategy.md](test-strategy.md) includes future
requirements. Full SMART/media qualification, complete RAM tests, GPU stress,
physical ports, privilege separation, comprehensive watchdogs, Secure Boot,
and the broader profiles remain open where marked in
[the master checklist](../PROJECT_CHECKLIST.md). VM success must not be used
to close physical-hardware requirements.

## Inactive Touchpad Core and Linux Adapter

This section records the legacy single-contact touchpad core and Linux adapter
qualification obtained while `input.touchpad.quick` was inactive. Those results
covered components; they did not qualify the later inventory, command,
catalog/plan, session or signed-report integration. Current activation has
separate evidence below. Historical verification rules remain version-specific.

The core uses complete frames, bounded counters and fixed X/Y anchors within
each contact. Signed range arithmetic is widened before computing engineering
thresholds; these thresholds do not measure physical distance. Fresh motion
cannot be assembled across contacts or borrowed from a queried coordinate.
A supported device must supply the required directional motion and primary
button cycle. Initially held input supplies no fresh action credit.
Key-state queries occur only at the baseline and noncrediting terminal phases;
completed devices remain observed while another device is unfinished.
Undefined `SYN_REPORT` values preserve frame boundaries, while loss and invalid
transitions reject.

The Linux adapter checks the caller's event layout, ioctl return counts and
the complete returned bitmap extent. Signed axis descriptors are validated;
unreturned compatibility padding is not capability evidence. Unknown absolute
axes retain their codes as unsupported markers: their zero-filled fields are
unmeasured, not measured zero configurations. Unknown and multi-touch
capabilities cannot be discarded to manufacture a supported subset. Acquisition
checks metadata before readable event access and closes acquired owners in
reverse order. Terminal identity, idle and neutral proof cannot add actions;
failure revokes tentative completion. Valid fragmented input interrupted by
cancellation or a deadline remains incomplete, while observed EOF, malformed
input, event loss and failed closure retain their distinct error precedence.
Component results omit raw coordinates, event sequences, device names and
event timestamps. The implemented strict evidence, privacy projection and owned
finalization route is described under
[touchpad inventory and strict evidence](#touchpad-inventory-and-strict-evidence);
its later integration has separate validation evidence.

With Go 1.27.2, focused Windows amd64 and 386 each passed 541 test/subtest
outcomes across 95 roots. Both included all 196 selected names across 26 roots
and 170 direct-parent intervals, with no failures or skips. These runs exercise
the portable core and explicit non-Linux behavior, not the Linux adapter.
The full Windows amd64 suite passed 9,148 outcomes with 18 permitted test skips:
31 packages passed and one had no test files, covering 32 packages and 1,386
source roots. The exact 916-name selection, 23 fuzz seed children and 13
parallel children reconciled; seed execution is not a fuzzing campaign.
Whole-module vet passed and formatting preserved tokens, literals and comments.

Linux amd64 and 386 each passed the exact 395-name selection across 47 roots
and 348 direct-parent intervals, with no missing, extra, duplicate, skipped or
failed outcomes. Nine ownership fixtures and 32 archive cases also passed;
EOF and cleanup checks were confirmed. Linux amd64, 386 and AArch64 test
artifacts compiled; AArch64 was not executed. Recorded host and cross-build
commands and postchecks reconciled, but their outer process exit codes were
unavailable. Native execution closed successfully.

This qualifies software and injected owned fixtures only. It adds no real
evdev/uinput exercise, deployed-kernel ioctl or queue qualification, physical
touchpad result, new race run or Go 1.25 runtime matrix. The following
inventory/evidence increment has separate host evidence; it does not extend
the component's native results to those new paths. Edge coverage, tap,
multi-touch, scroll and gesture behavior, all four rebuilt images, the
physical-PC pilot and release approval remain separate gates. See
[component tests](test-strategy.md#inactive-touchpad-component-validation).

## Touchpad Inventory and Strict Evidence

Inventory now derives a finite `touchpad_candidate` marker from category and
ABS observations, using the adapter's candidate predicate. Checked sysfs/proc
MSC collection preserves bounded 32/64-bit bitmap meaning and distinguishes
unavailable data from a valid empty set. Neither the marker nor an unsupported
capability grants functional support.

The strict evidence package rejects ambiguous JSON and contradictory bounded
status, frame, action and cleanup claims. Inventory/plan binding requires
complete capability sets and consistent candidate, identity, scheduled-test
and original graph claims. Its caller must supply checked plan and inventory
inputs; self-consistency does not authenticate unseen kernel history.
Unknown-axis zero fields remain unmeasured unsupported markers. Functional
evidence adds no raw coordinates, model names, paths or event timestamps.

With Go 1.27.2, focused Windows amd64/386 suites each passed 2,332 outcomes with
one platform skip, including all 686 selected names across 58 roots and 628
direct-parent intervals. The full Windows amd64 suite passed 9,509 outcomes
with 18 permitted test skips across 33 packages and 1,409 roots; 32 packages
passed and one had no tests. Vet and all seven postchecks passed. Linux/native
qualification of this inventory/evidence increment remains pending.

The standalone command is also host-qualified with Go 1.27.2. Focused Windows
amd64/386 suites each passed 5,647 outcomes with seven permitted skips and all
794 selected names. The full Windows amd64 suite passed 9,617 outcomes with
18 permitted skips; vet and all seven postchecks passed. All 108 CLI names
across 16 roots and 92 direct-parent intervals passed in each focused run and
the full run. These are software command fixtures; Linux/native, installed
command and real-device qualification remain open for this increment.

The pre-activation v9/v10 session evidence and command/finalizer increment
passed Windows qualification. Typed-plan fixtures exercise fixed dispatch,
strict publication/status/duration matching and original-key HMAC reconstruction
over owned plan and inventory inputs. Evidence is reread and compared; cleanup
uncertainty retains priority. Tests reject success after cancellation at
entropy, clock and output callbacks while preserving output already written.
A supplied journal still requires the existing live safety/thermal owner.

With Go 1.27.2, focused Windows amd64/386 suites each passed 6,277 outcomes with
seven permitted skips; the full Windows amd64 suite passed 9,982 outcomes with
18 permitted skips. All 151 session names across 14 roots and 137 direct-parent
intervals, and all 214 command names across 19 roots and 195 intervals, passed
in each focused run and the full run. Vet and all seven postchecks passed.

The app/report consumers added before activation check functional evidence, final
candidate sets, finite disposition notes and matching v9/v10 session records.
They reject contradictory component-difference sets and a scheduled touchpad
row that the same integrity record explicitly excludes. Cleanup uncertainty
cannot become a successful observation. These are consistency checks; original
inventory ownership and HMAC reconstruction remain in the finalizer.

Signed bundles apply these checks even without HTML/PDF views. Unknown or
malformed touchpad definitions cannot bypass validation through the historical
opaque-report path; explicit definition-1 unsupported notes and ID-only
references retain their limited historical handling. HTML/PDF views use copied,
bounded data and distinguish unsupported markers from measured fields. The
public candidate projection excludes raw coordinates, model/name text, paths,
unique device IDs and event timestamps without rewriting the original inventory.
Inventory without an event interface remains reportable; functional evidence
still requires a matching event interface.

Go 1.27.2 focused Windows amd64/386 runs each passed 4,586 outcomes with six
permitted skips. The full Windows amd64 suite passed 10,242 with 18 permitted
skips; vet and all seven postchecks passed. All 260 app names, 25 roots and
235 direct-parent intervals passed in each of the three runs. Review
reconciled all 16 formatted candidates and confirmed 639 other source members
unchanged. Those measurements qualify the app increment before catalog
activation.

At touchpad activation, definition 2 used the exact derived
`touchpad_candidate=true` provider marker; category text alone granted no
worker authority. Quick v9 contained thirteen workers and Standard v10
fourteen, with touchpad once after pointer. Modern Quick selection had
thirteen choices and legacy x86 eleven.
Explicit exclusions remain Quick-only. Absent or unsupported candidates retain
their finite dispositions, and cleanup uncertainty prevents a pass.

Execution still requires `Plan.Decode` and validation against the current
catalog. Stale plans remain rejected even if rehashed. Historical v4-v8 signed
reports and journals retain their original interpretation; they are not a
fallback for current execution or live-owner finalization. At that stage,
Extended and Destructive profile names did not introduce session coordinators.
The later bounded Extended implementation is described below.

With Go 1.27.2, the connected activation fixtures passed focused Windows
amd64/386 runs: each had 4,838 passes and six permitted skips. The full amd64
suite had 10,341 passes and 18 permitted test skips. All 583 activation names
across 73 roots and 510 declared parent intervals passed in all three streams.
Vet and all seven postchecks passed. Strict formatting review covered all
50 Go candidates, including twelve with byte changes, with no normalization
exceptions; tokens, literals and comment anchors were preserved. The other
607 source members remained byte-exact.

These Windows fixtures add no Linux/native, real evdev, deployed-kernel,
installed-image or physical qualification for the later integrated route.
Broader touch/gesture coverage, the physical pilot, complete beta matrix and
release gates remain open. See
[tests](test-strategy.md#touchpad-inventory-and-evidence-host-validation).

## Bounded Extended Memory Session

Development source implements an explicitly selected Extended profile with a
current v16 journal and fifteen workers. It retains the current Quick sequence and
adds `memory.stress` immediately after `memory.quick`; successful scheduled
quick-memory checks are a prerequisite. CPU stress remains Standard-only.
Quick v14/fourteen workers and Standard v15/fifteen workers retain their
schedules, and Standard retains its 900-second work budget. The completed
Extended and combined records below belong to the preceding v11/v12/v13
source, before exterior definition 2; they do not qualify the new inspection.

The memory-stress definition is version 2 and Extended-only. The development
catalog has sixteen implemented rows and six inactive families:
`audio.quick`, `camera.quick`, `network.quick`, `storage.quick`,
`storage.destructive` and `usb.quick`. Readiness evidence does not activate
these remaining functional families or complete the broader profiles.

### Selection and budget

Both the frozen plan and `hhpt session -profile extended` must select Extended.
The default and maximum work budget are 2,700 seconds (`-timeout 45m`).
The console's explicit `E` selection requires an owned profile/architecture
launch record with no exclusions; automatic and ordinary Quick choices remain
Quick. Both startup supervisors use a separate 2,750-second outer policy.
The conservative 2,744-second budget ledger retains the existing preparation,
finalization and joined-cleanup rules. This is not a hard wall-clock deadline
or a guarantee that blocked kernel work or every descendant can be stopped.

The producer still requests 1,800 seconds of load within its original
1,860-second invocation watchdog. It reuses one 4-64 MiB allocation selected
against fresh available memory, at most one quarter of that observation.
Preparation, active-cycle completion, final read and monitor cleanup share
that watchdog. Short available time cannot turn a reduced load into a pass.
The bound covers the owned working set, not the entire heap, installed RAM,
physical traffic or DIMM health.

### Owned evidence and compatibility

The live finalizer separately binds the original evidence-file SHA-256 and
strict typed digest to the current frozen plan, hardware graph, result and v16
journal. It rereads owned evidence before signing. Dispatched work requires
matching evidence; unstarted work retains a finite note. Cleanup uncertainty
prevents signing and a ready marker. Mismatch facts survive cancellation and
heat, and local thermal-source presence must agree with the observed result.

Strict report, bundle, copied-accessor, comparison and HTML/PDF consumers reject
malformed, stripped or mixed memory/CPU claims and mismatched source, plan or
journal bindings. Canonical views preserve the complete bounded-memory scope
warning across PDF line wrapping. Exact non-scheduled definition-2 catalog
notes remain admissible in current Quick and Standard without acquiring
observed memory-stress authority. Historical v4-v13 wire layouts and typed-plan
bindings retain their original interpretation; current `Plan.Decode` still
rejects stale catalogs, even when rehashed.

The regression requirements include valid real-finalizer prerequisites,
different original serializations with independently bound file and typed
digests, missing dispatched evidence, cleanup nonpublication, local heat
consistency, forged non-scheduled notes and unchanged Quick/Standard authority.
See the [connected finalizer fixtures](../cmd/hhpt/extended_memory_finalizer_test.go),
[signed consumers](../cmd/hhpt/extended_memory_signed_test.go) and
[journal bindings](../internal/sessionevidence/memory_stress_test.go).

### Validation status

Corrected Extended host validation passed all 242 declared focused test/subtest
names on Windows amd64 and 386, including 41 roots and 201 source-declared
parent relations in each run. The full Windows suite closed all 11,659
outcomes: 11,641 passes and 18 retained platform skips, with 33 passing
packages and one package without tests. Exact names, declared parents,
lifecycle closure and the retained skip/fuzz/parallel policy were reconciled.

All 22 recorded commands completed, including formatting, vet, three Linux
application builds and twelve Linux test-binary builds across amd64, 386
and AArch64. The fifteen Linux artifacts were compiled, not executed.
All seven final postchecks passed. Both earlier failed attempts remain
failed evidence and supply no qualification.

Earlier standalone host and selected native memory fixtures keep their
original scope. These Windows fixtures qualify the connected session software
without establishing native startup behavior or a real elapsed load.

A separate standalone API qualification completed one real 1,800-second load
on each of Linux amd64 and 386. Its 703-file source overlay adds only the
elapsed-test fixture to the 702-file standalone baseline; it does not execute
the later Extended integration. The real allocator, byte kernel, availability
reader and thermal sampler used one 4 MiB working set with synthetic
availability and fixed 35-degree-Celsius inputs. Both roots passed the strict
decoder, independent cycle/typed-digest checks, final verification and cleanup.
All nine ownership cases, 32 archive cases and six postchecks passed. The
earlier failed elapsed attempt remains unqualified.

The three updated startup/fixture files were separately tested against that
702-file standalone baseline. Both shell syntax checks and all 22 mock-fixture
methods passed, including the exact 22 new subcase outcomes, with no failures
or skips. Ownership, archive and six postchecks passed with cleanup confirmed.
These checks used owned command stubs and executed neither installed
supervisors nor product workers.

The later combined source also passed the selected Linux amd64/386 session
and parser fixtures: 580 outcomes across ten package executions, including
all 110 roots and 470 declared-parent intervals, with no failures or skips.
Ownership, archive, six postchecks and cleanup passed. These fixture results
exercise the connected software without establishing an elapsed Extended load.

Installed startup supervisors, full-duration Extended execution,
installed CLI and image behavior, the full Linux suite, race/ARM qualification, physical memory and
thermal coverage, all four bootable images, the ten-PC pilot and release
approval remain separate gates. Broader working sets, SPD/ECC, Memtest86+ and
complete profile coverage remain planned work. See
[operator guidance](memory-stress.md) and the [checklist](../PROJECT_CHECKLIST.md).

# Memory stress diagnostic and Extended session

Development source implements `hhpt memory-stress`, a standalone JSON
diagnostic requesting a fixed 30-minute load over one bounded working set.
An explicitly selected Extended session now connects the same producer to a
frozen plan and strict signed evidence. A standalone diagnostic file alone
does not supply that session authority.

The `memory.stress` definition is version 2 and applies only to Extended.
The 22-row development catalog has sixteen implemented and six inactive rows.
Quick v14 has fourteen workers; Standard v15 has fifteen and retains its
900-second work budget. Extended v16 has fifteen workers: the Quick sequence
with memory stress immediately after quick memory. CPU stress stays
Standard-only. Automatic boot and ordinary Quick selection do not start
Extended.

## Run the diagnostic

```sh
hhpt memory-stress -output memory-stress.json
hhpt memory-stress -bytes 16777216 -output memory-stress-16m.json
```

Choose a new output path in a writable destination. Existing files are not
replaced. The default `-output -` writes to standard output. Check the exit code
and the saved result: completing memory work does not guarantee that output
was successfully written.

The production command uses Linux availability and thermal observations.
Missing or unusable observations prevent admission; there is no alternate-root
command-line option for substituting them. There are also no duration,
pass-count or thermal-limit overrides.

## Run an Extended session

Use the same existing 32-byte fingerprint key and explicit key epoch for
planning and execution. Use a fresh, writable evidence directory and a new
report path; retain the evidence until finalization completes.

```sh
hhpt plan -fingerprint-key fingerprint.key -key-epoch local-1 \
  -profile extended -output extended-plan.json
hhpt session -fingerprint-key fingerprint.key -key-epoch local-1 \
  -plan extended-plan.json -profile extended -timeout 45m \
  -evidence-dir extended-evidence -output extended-report.hhpcr
```

The matching key epoch is deliberate: the plan and session commands have
different defaults. The session checks the current catalog and matching
profile before dispatch. A stale or rehashed historical plan is not an
execution fallback.

The console offers `E` when it has an owned launch-selection output. Restore
Quick exclusions with `C`, then `A`, before choosing Extended. The launch
record binds profile, architecture and an empty exclusion list; both startup
supervisors carry that same profile into planning and execution. Automatic
start, Enter, `1` and the modern deployment-key choice remain Quick.

The default and maximum Extended session work budget are 45 minutes
(2,700 seconds). A smaller explicit timeout does not shorten the required
memory load into a pass. The startup supervisors use a separate 2,750-second
outer policy; do not pass that value as the session timeout. Finalization and
joined cleanup retain their separate checks. These policies cannot guarantee
a hard wall-clock deadline or termination of blocked kernel work.

Memory stress starts only after scheduled quick-memory checks pass. A
quick-memory failure, missing prerequisite, thermal stop or cancellation
prevents later work. The session uses the producer's default working-set
selection; it does not expose the standalone `-bytes` override.

## Working set and timing

The default `-bytes 0` selects the smaller of 64 MiB and one quarter of freshly
observed available memory, rounded down to a whole MiB. An explicit size must
be between 4 MiB and 64 MiB and fit within that same quarter-memory limit.
Availability is acquired once for this invocation, after initial thermal
admission. Unavailable, malformed or insufficient availability prevents load;
this command does not use the quick-memory worker's conservative fallback.

Preparation, load, final verification and cleanup share a 31-minute watchdog
measured from invocation. A parent deadline may reduce the available budget.
The diagnostic requires enough remaining time to start the full 30-minute
load and will not report a pass for a shortened window. These are cooperative
bounds, not a guarantee that a blocked kernel operation can be interrupted.

The load reuses one allocated buffer. It writes and verifies byte patterns,
address-dependent patterns and forward/reverse inversions. A cycle already
in progress when the load window ends may finish within the remaining
watchdog budget. No new cycle starts at or after that boundary. A separate
final read verifies the completed buffer without counting as another load
cycle. No terminal verification is credited when no cycle completed.

## Thermal observations and stop results

Load requires an initial available thermal observation below 90 degrees
Celsius. An observed temperature at or above that threshold stops work.
Partial or unavailable initial coverage prevents admission, and later loss
of coverage prevents a pass. Observer errors and unconfirmed cleanup also
prevent a pass.

These are available system sensor observations. They do not establish DIMM
temperature coverage or replace physical-PC qualification. The 4-64 MiB
working-set bound also does not bound the entire process heap or machine.

A memory mismatch remains a failure even when cancellation, a deadline or a
thermal stop is observed at the same time. If an availability reader or
monitor has not confirmed cleanup, the command returns the cleanup error
and emits no normal diagnostic. Cancellation cannot forcibly interrupt a
blocked kernel operation.

## Read the result

Check both `status` and `stop_reason`. A successfully written diagnostic can
report a failure, abort, unavailable coverage or another inconclusive result.

| Exit code | Meaning |
| --- | --- |
| `0` | A structurally valid diagnostic was written. Inspect its status. |
| `1` | Accounting, evidence validation or output failed. |
| `2` | Arguments, options, context or command dependencies were invalid. |
| `12` | Cleanup was not confirmed; no normal diagnostic was emitted. |

The `hhsoft.pc-tester.memory-stress/v1` result separates completed cycles,
cycles verified within the load window, logical reads/writes, uncredited work
and the final read. A full cycle has ten phases and accounts for six logical
reads and six writes per working-set byte; the terminal read is separate.
Those counters do not measure physical memory traffic, prove cache bypass,
identify a failing DIMM or establish whole-memory coverage.

The strict decoder rejects incomplete, contradictory or ambiguous JSON and
keeps memory-stress evidence separate from quick-memory evidence. Its typed
digest identifies the decoded result; it does not authenticate the original
file bytes or turn the diagnostic into signed-session authority.

For Extended, the real session finalizer binds both the original evidence-file
SHA-256 and the strict typed digest to the frozen plan, hardware graph,
worker result and current v16 journal. A dispatched worker needs its matching evidence;
an honest not-started note is not an observation. Unconfirmed cleanup prevents
signing and publication of the ready marker. Mismatches remain failures through
cancellation or heat, and local thermal evidence must agree with the journal.

Signed reports, HTML/PDF views, bundles without views, copied accessors and
comparisons reject malformed or mixed memory/CPU claims and mismatched bindings.
Views retain the bounded-working-set warning; they do not claim full-RAM,
DIMM, cache-bypass or physical-traffic coverage. Current Quick and Standard
can retain exact non-scheduled memory-stress catalog notes without gaining
observed stress authority. Historical v4-v13 reports and typed journals keep
their versioned interpretation, while live execution still requires a current
plan.

## Validation and remaining integration

The records below retain their original source scope. Extended and combined
results used v11/v12/v13 journals before the four-criterion exterior update.
The selected combined native cohort's 580 outcomes belong to that earlier
714-source/698-Go source. The corrected current v14/v15/v16 inspection source
passed Windows host validation and subsequently passed 1,296 selected Linux
fixture outcomes across 12 amd64/386 streams, with no skips or failures. One
separate synthetic standalone inspection CLI exercise also passed. Installed
CLI and physical inspection qualification remain open.

Implementation and source review are complete for this standalone slice.
All 19 host-validation commands passed. Focused tests on Windows amd64 and 386
passed all 196 named fixture cases on each architecture, with no skips. The
full Windows suite recorded 11,434 passes and 18 retained platform skips.
Formatting and format checks passed for all 13 Go files, and vet passed.
Three Linux application binaries and nine test binaries built for amd64, 386
and arm64; that host-validation step did not execute the Linux artifacts.

Subsequent selected native Linux amd64 and 386 runs executed six test binaries
for `cmd/hhpt`, `internal/quick` and `internal/quickevidence`. They passed all
392 named outcomes across 62 root executions, with all 330 declared-parent
intervals checked. No outcome failed, skipped or used pause/continue. All nine
process-ownership cases, 32 archive cases and six postchecks passed, and cleanup
was confirmed. This closes the selected native software-fixture gate.

Those controlled-clock fixtures retain their original scope. A later standalone
API test completed a real 1,800-second load on each of Linux amd64 and 386,
using the production allocator, memory kernel, availability reader and thermal
sampler with one 4 MiB working set and fixed synthetic input files. Both runs
passed the strict decoder, checked cycle accounting, final verification and
cleanup. Nine process-ownership cases, 32 archive cases and six postchecks
passed. The earlier failed elapsed attempt remains unqualified.

This closes the standalone elapsed-window fixture gate. It does not qualify
the installed command, a full Extended session, larger working sets or physical
memory and thermal behavior. Race checks, the full Linux suite and ARM runtime
remain open. Earlier quick-memory and CPU-stress results remain evidence for
those earlier implementations.

The connected Extended implementation and source review are complete.
Corrected Extended host validation passed 242 focused test/subtest cases on
each of Windows amd64 and 386. The full Windows suite passed 11,641 cases
with 18 platform skips, 33 passing packages and one package without tests.
Formatting and vet passed. Three application builds and twelve test-binary
builds passed across Linux amd64, 386 and AArch64; those artifacts were
compiled, not executed. These host fixtures cover the connected coordinator
and signed-consumer route without establishing a real elapsed load.

The updated startup scripts separately passed two shell syntax checks and all
22 mock-fixture methods, including 22 new subcases, with no failures or skips.
The run used the standalone source baseline plus the two startup scripts and
their fixture suite. It executed owned command stubs, not an installed
supervisor or product session. Ownership, archive and six postchecks passed
with cleanup confirmed.

The later combined Extended and proc parser source passed 580 selected
test/subtest outcomes on Linux amd64 and 386, with no failures or skips.
The ten package executions covered all 110 roots and 470 declared-parent
intervals; ownership, archive, six postchecks and cleanup passed. These fixtures
exercise connected session and signed-consumer paths without a real elapsed load.

The standalone and auxiliary results retain their earlier scopes. Full-duration
Extended execution, installed supervisors and CLI, the full Linux suite,
race/ARM runtime and installed image behavior remain separate validation gates.

Broader working sets, SPD/ECC, the separate Memtest86+ boot path, physical
memory and thermal behavior, all four bootable images, the ten-PC pilot and
release approval remain open. See the [project checklist](../PROJECT_CHECKLIST.md)
and [roadmap](roadmap.md) for implementation and qualification status.

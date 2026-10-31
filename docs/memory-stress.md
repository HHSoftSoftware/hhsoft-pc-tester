# Memory stress diagnostic

Development source implements `hhpt memory-stress`, a standalone JSON
diagnostic requesting a fixed 30-minute load over one bounded working set.
It is separate from `memory.quick` and is not included in automatic boot,
Quick or Standard. Its output does not supply signed-session test authority.

The `memory.stress` catalog row remains inactive. The 22-row catalog still has
fifteen implemented and seven inactive rows. Quick v11 has fourteen workers;
Standard v12 has fifteen and retains its 900-second session budget. An explicitly
selected longer profile and its signed integration remain separate work.

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

## Validation and remaining integration

Implementation and source review are complete for this standalone slice.
All 19 host-validation commands passed. Focused tests on Windows amd64 and 386
passed all 196 named fixture cases on each architecture, with no skips. The
full Windows suite recorded 11,434 passes and 18 retained platform skips.
Formatting and format checks passed for all 13 Go files, and vet passed.
Three Linux application binaries and nine test binaries built for amd64, 386
and arm64; these artifacts were not executed.

These host checks include controlled-clock timing fixtures and do not establish
a real 1,800-second load. Native Linux fixtures, race checks and an actual
1,800-second run remain pending. Earlier quick-memory and CPU-stress results
remain evidence for those earlier implementations.

Connecting `memory.stress` requires an explicitly selected longer profile
with suitable budgets, frozen-plan authorization, versioned journals and
strict signed report, view and comparison consumers. It must not silently
extend Quick or the current 900-second Standard session.

Broader working sets, SPD/ECC, the separate Memtest86+ boot path, physical
memory and thermal behavior, all four bootable images, the ten-PC pilot and
release approval remain open. See the [project checklist](../PROJECT_CHECKLIST.md)
and [roadmap](roadmap.md) for implementation and qualification status.

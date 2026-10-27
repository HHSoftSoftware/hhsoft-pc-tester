# Beta physical validation pilot

This run sheet supports the ten-PC pilot in the [roadmap](roadmap.md).
It records physical evidence; VM checks do not fill these rows. Pilot results
do not replace the [release gates](release-process.md) or the open functional
requirements in the [master checklist](../PROJECT_CHECKLIST.md).
The ten-PC gate remains open. Guided keyboard software qualification passed,
including native Linux amd64/386, race and CLI checks. These results do not fill
physical pilot rows. Use newly rebuilt and qualified beta images; the initial
alpha candidate retains its separate release gates.

## Candidate and fleet

Record the candidate version, source commit, image architecture, ISO SHA-256,
companion verifier version, and test date with each run. Use a local asset alias
such as `pilot-01`; keep serial numbers, customer names, and deployment keys out
of shared notes. Distinguish physical hardware from virtual devices in reports.

Choose at least ten different physical PCs across the supported configurations:
Intel and AMD CPUs; integrated and discrete Intel, AMD, and NVIDIA graphics;
laptops and desktops/workstations; BIOS and UEFI; SATA and NVMe; and varied input,
audio, network, and USB controllers. Record legacy x86 and AArch64 validation
separately where those images are applicable. An unavailable architecture remains
an open qualification item rather than inheriting another image's result.

| Asset alias | CPU / architecture | Graphics | Form factor | Boot / image | Storage and peripherals | Run / report reference |
| --- | --- | --- | --- | --- | --- | --- |
| pilot-01 | | | | | | |
| pilot-02 | | | | | | |
| pilot-03 | | | | | | |
| pilot-04 | | | | | | |
| pilot-05 | | | | | | |
| pilot-06 | | | | | | |
| pilot-07 | | | | | | |
| pilot-08 | | | | | | |
| pilot-09 | | | | | | |
| pilot-10 | | | | | | |

## Per-PC run sheet

Use verified, prepared boot media and the safe quick profile. Use a separate
removable device for report saving. This pilot does not authorize destructive
tests, repairs, internal-drive mounts, or disabling firmware safety controls.

| Check | Evidence to retain | Result / issue |
| --- | --- | --- |
| Boot the applicable image. | Firmware mode, image digest, visible boot failures, time to menu. | |
| Cancel automatic start using keyboard and pointing-device input. | Input types tested; whether cancellation reaches the menu. | |
| Exercise automatic safe start on a separate boot. | Countdown behavior and the selected frozen quick plan. | |
| Compare discovery with the installed hardware. | Missing or duplicated devices, diskless behavior where applicable, driver gaps. | |
| Run the applicable quick tests. | Each result, skipped/unsupported reason, operator findings, and elapsed time. | |
| Exercise guided keyboard actions on each interface, including multiple keyboards. | Press/release left Shift, right Shift, Up, Down, Left and Right on each keyboard. Verify one device cannot complete another, withheld actions stay incomplete, and no typed text or raw events enter evidence. | |
| Check keyboard baseline and interruption handling with a removable fixture. | Release an initially held control before starting fresh actions; leave a control held at completion; cancel or disconnect one keyboard while another completes. Retain partial observations and verify idle/neutral/identity failures do not pass. Keep boot and report media attached. | |
| Exercise guided relative-pointer actions on every applicable interface. | Left/right and up/down movement; primary and advertised right/middle button press/release cycles; both directions on every advertised wheel axis. Include no-wheel, vertical/horizontal and high-resolution devices; one device must not satisfy another. Absolute/mixed interfaces and incomplete actions must not pass. Do not retain raw events or typed text. | |
| Check thermal evidence across the quick session, including an all-off selection. | Initial observation, periodic coverage through workers and cleanup, final inventory and final safety/thermal observations, and confirmed monitor joins. Retain available/partial/unavailable coverage and plausible peaks; absent telemetry stays absent. Report encoding, output and saving follow this boundary. Do not force overheating or change fan controls. | |
| Exercise cancellation where the UI supports it. | Worker termination, retained evidence, and return to a usable interface. Record unavailable cancellation paths. | |
| Exercise a supported removable test peripheral's disconnect/reconnect. | Explicit device-change or partial-result handling. Keep boot and active report media attached. | |
| Save the report through destination selection and typed confirmation. | Save receipt, selected removable destination, and any recovery behavior. | |
| Verify the saved report independently. | Verifier result, artifact digest, and agreement between signed data and HTML/PDF views. | |
| Confirm safe-mode boundaries remained intact. | Reported mount/network safety observations and any unexpected changes or traffic. | |

Record boot-to-usable-results time and individual phase timings. Flag any run
that reaches 15 minutes for review; retain slow and failed runs in the evidence
set. A readiness check does not establish functional transfer, audio playback,
camera capture, RF performance, or sustained-load coverage. Use the exact
coverage reported by the candidate, and track missing beta functionality openly.
For pointer definition 2, use a newly rebuilt and qualified candidate; full
software qualification of the wheel increment is still in progress. Include
multiple pointers, withheld wheel directions and other actions,
lost/disconnected-device handling, and cancellation with a supported removable
fixture. Record unsupported absolute/mixed interfaces separately. VM and
synthetic event checks do not complete these physical rows.

For the guided-keyboard increment, use a candidate with keyboard definition 2
and session v5. Its eleven modern or nine legacy choices remain subject to
hardware applicability; reports still contain 22 results. Check the six fixed
controls only, within the default 30-second window, 90-second command limit and
remaining session budget. Missing actions do not establish a defective key.
Verify that queued arrow input does not answer a later display prompt and that
Ctrl-C has one console owner. Record ordinary console errors separately from
terminal exit 12 cleanup uncertainty; neither authorizes ready/save. Full layout,
rollover, ghosting and other keyboard functions remain outside this pilot subset.

Self-issued report verification checks integrity, not independent signer trust:

```text
hhpt verify -self-issued REPORT.hhpcr
```

## Pilot review

- Retain at least ten distinct-PC run records and independently verified reports.
- Map the tested firmware, architecture, graphics, controller, and peripheral
  combinations to the fleet table; list untested combinations explicitly.
- Triage hangs, crashes, unexpected writes or traffic, false passes, missing
  evidence, and failed recovery before calling the candidate beta-ready.
- Preserve the original failed run when retesting a fix, and bind the new run
  to the new candidate version and image digest.
- Review the under-15-minute objective, remaining functional adapters, and
  release checks together. Completing this form alone does not approve release.

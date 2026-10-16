# Open Decisions

Defaults let Phase 1 proceed. Change them before the named decision gate.

| Question | Working default | Decide by |
| --- | --- | --- |
| Oldest x86 CPU | Buildroot i586 and Go `softfloat` (Pentium MMX) | first legacy image |
| Destructive storage | separate signed image and profile | first write test |
| Upload service | local-only; no server dependency | beta remote pilot |
| Hardware identifier | local SHA-256 plus optional deployment HMAC | report schema v1 |
| Operator identity | optional signed local operator profile | remote pilot |
| PDF engine | CI/off-device conversion, not required at runtime | beta guide |
| UI renderer | TUI plus dedicated graphical tests first | alpha UX pilot |
| Secure Boot | optional and deferred; plain native UEFI remains required | after functional release |
| Documentation UI | square, dense, utilitarian custom static site | Pages prototype |

## Owner input that would change architecture

- Requiring pre-MMX PCs would need a separate non-Go inventory fallback and
  much older kernel/toolchain qualification.
- Requiring every report to be globally comparable requires a protected remote
  HMAC service and makes offline-only identity impossible.
- Requiring certified wipe or legally admissible audit records needs separate
  standards, operator controls, clock trust, retention, and independent review.
- Requiring one image to work on every ARM laptop is not feasible without
  device firmware and driver-specific qualification; AArch64 UEFI is the first
  bounded target.

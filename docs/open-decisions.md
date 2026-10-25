# Open Decisions

Working defaults and implemented choices are recorded below. Review unresolved
choices before the named decision gate.

| Question | Working default | Decide by |
| --- | --- | --- |
| Oldest x86 CPU | Buildroot i586 and Go `softfloat` (Pentium MMX) | first legacy image |
| Destructive storage | separate signed image and profile | first write test |
| Upload service | local-only; no server dependency | beta remote pilot |
| Hardware identifier | implemented deployment/session-keyed HMAC; controlled token for cross-session comparison | cross-epoch or cross-deployment comparison |
| Operator identity | optional signed local operator profile | remote pilot |
| PDF engine | implemented deterministic static report PDF at runtime; documentation manual remains separate | versioned documentation manual |
| UI renderer | TUI plus dedicated graphical tests first | alpha UX pilot |
| Secure Boot | optional and deferred; plain native UEFI remains required | after functional release |
| Documentation UI | square, dense, utilitarian custom static site | Pages prototype |

## Owner input that would change architecture

- Requiring pre-MMX PCs would need a separate non-Go inventory fallback and
  much older kernel/toolchain qualification.
- Cross-deployment comparison requires an explicit shared-trust and key-custody
  design. Current offline tokens support comparison within one deployment and
  key epoch; independent deployments remain unlinkable by default.
- Requiring certified wipe or legally admissible audit records needs separate
  standards, operator controls, clock trust, retention, and independent review.
- Requiring one image to work on every ARM laptop is not feasible without
  device firmware and driver-specific qualification; AArch64 UEFI is the first
  bounded target.

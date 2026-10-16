# Repository protections

The official public repository is `HHSoftSoftware/hhsoft-pc-tester`, owned by
the HHSoftSoftware account. The following configuration was verified during
initial source publication on 2026-10-15. Recheck it before a release.

- Secret scanning and push protection are enabled.
- `main` requires a pull request, resolved review conversations, and an
  up-to-date successful `source` check from GitHub Actions App 15368.
  These requirements apply to administrators. Force pushes and deletion are
  disabled.
- A separate rule requires CODEOWNERS approval. During the single-maintainer
  bootstrap, repository administrators can bypass only this review rule after
  the deliberate review required by `GOVERNANCE.md`. That exception does not
  bypass the separate PR, CI, or branch-integrity requirements. Remove it when
  independent maintainers can satisfy the required reviews.
- `v*` tag creation, updates, and deletion are restricted to the administrator
  role, currently the HHSoftSoftware owner. Adding another administrator would
  also grant this exception; review repository access before doing so.
- The `release` environment requires HHSoftSoftware approval and permits only
  protected branches. The publisher additionally requires `main` itself.
  Administrator bypass is disabled. Self-review remains permitted for the
  single-maintainer bootstrap and is not independent approval.
- Immutable releases are enabled. `RELEASES_ENABLED` remains `false`; signing
  keys, approved signer fingerprints, and the complete ISO evidence are still
  required before the first publication run.
- Issue-template labels are defined in [.github/labels.json](../.github/labels.json)
  and have been applied and read back from the official repository.

## Preserve commit identity during bootstrap merges

Local commits use `HHSoft Software <info.hhsoft@gmail.com>` for both author and
committer. Server-side merge or rebase operations cannot guarantee both exact
identities. Use an ordinary fast-forward of the exact reviewed PR head instead:

1. Require current remote `main` to be an ancestor of the PR head and review all
   incoming commits. Verify both identity fields and run the publication gate.
2. Require a successful `source` check from Actions App 15368 on that exact head.
   Dispatch source validation on the PR branch when necessary, and also review
   the PR's merge validation. Record the deliberate review after tests finish.
3. Recheck the PR head, remote `main`, and unresolved conversations. Push the
   reviewed head to `main` normally, without force or a server-side rewrite.
4. Verify the remote SHA, unchanged commit metadata, and the PR's merged state.

The administrator exception applies only to the separate CODEOWNERS review
rule. If GitHub rejects the update, investigate without weakening the mandatory
PR or CI protections. GitHub documents [local protected-branch merges](https://docs.github.com/en/repositories/configuring-branches-and-merges-in-your-repository/managing-protected-branches/about-protected-branches)
and the [cumulative application of protection rules](https://docs.github.com/en/repositories/configuring-branches-and-merges-in-your-repository/managing-rulesets/about-rulesets#about-rule-layering).

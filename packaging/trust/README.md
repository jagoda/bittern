# Trust anchors

Repository copies of public trust anchors. Private keys never live here.

## Beta CI test anchor

`beta-ci.pub` is the public half of the CI-only Ed25519 key generated on 2026-11-22 for the
`beta-draft` GitHub Actions environment. It signs test bundles only. It is a bench test anchor: it is
pinned only on bench devices and never in a release image or a release trust store, and it is never
the release anchor. Release manifests are signed offline under
[ADR 0029](../../docs/adr/0029-release-signing-and-key-custody.md). ADR 0032 records the CI
decision.

`beta-ci.pub` is the raw 32-byte Ed25519 public key. Its key id is
`e19d358aa7caaca5e5ddb26560227a285a88bc6f136621f3972e0ac054b5c9f3`, the lowercase SHA-256 of the
public key. The appliance trust store reads the key from `<key_id>.pub`, so a bench device needs the
file renamed to `e19d358aa7caaca5e5ddb26560227a285a88bc6f136621f3972e0ac054b5c9f3.pub`. The key id
is also the SHA-256 of this file. The first CI test key was retired on 2026-11-22 before any device
carried its anchor, so no re-seeding is required.

The private half is held only as the `BITTERN_CI_SIGNING_KEY` environment secret in
`beta-draft`, never at repository or organization scope. The separate `beta` environment is the
approval gate and holds no signing key. The environment stores it base64-encoded, and a workflow must
decode it to the 32 raw seed bytes before `bittern-release sign`. The publish workflow derives the
key id from the decoded seed and refuses to sign when it does not equal the manifest
`signing.key_id`. The beta preparation kit installs this anchor as
`/etc/bittern/trust/release-keys/<key_id>.pub`; nothing else installs it.

Public key (hex): `c4b17fe275cc4632b58c24a4cd09ee1da8d9fb904a5e1907e75601b882855285`

## Environment ref policy

The `beta` and `beta-draft` environments both allow the tag glob `app-beta-*` and the `main`
branch. Neither allows another tag or another branch. The `*` is the documented wildcard form. The
[deployments and environments reference](https://docs.github.com/en/actions/reference/workflows-and-actions/deployments-and-environments)
(accessed 2026-11-22) documents the `*` wildcard and refers further syntax to Ruby `File.fnmatch`;
it does not document character classes or ranges as supported. The exact tag format is therefore
validated by the publish workflow before it creates the draft, not by an undocumented glob feature.
The workflow accepts only `app-beta-` followed by exactly six lowercase hexadecimal characters, with
generation starting at `000001`, and rejects everything else.

[ADR 0033](../../docs/adr/0033-dispatch-driven-release-trigger.md) explains why both refs are
needed. A workflow run carries a single ref. A dispatch at `main` carries `refs/heads/main` for the
preflight, the signing job, and the approval job; the tag is created by the signing job after the
build and signature succeed. The environment policy is evaluated against the run's ref, not against
the tag created during the run or a workflow input. The tag policy remains for the tag-push
fallback. The tag no longer constrains a branch-triggered publish; the `beta` reviewer remains the
publication gate.

The environment policy and the ADR 0031 tag scheme must change together. A change to either one
without the other either blocks a valid tag at deploy time or permits a tag the ADR does not
define. The same rule applies to `main`: it is the only branch the release path uses.

## Threat model for the key

The environment gate is a ref gate, not an actor gate. `beta-draft` has two policies, the
`app-beta-*` tag and the `main` branch, and no required reviewer. A job that declares
`environment: beta-draft` is matched against the run's `GITHUB_REF`; a non-matching ref is refused
before the job starts, so the secret is never provided. A matching tag or `main` is admitted
without approval, by design, because the draft must exist before the `beta` approver can review it.

The permanent `main` policy does not widen the trust root. A tag push and a main-ref workflow both
require repository write access. The actors are unchanged; what changed is that the ref gate now
admits the normal release dispatch instead of keeping `main` away from the key. The controls that
still do work are the `beta` reviewer gate, which is the publication gate, SHA pinning of every
action, and the protected `main` ruleset, which forbids deletion, force push, and non-linear
history and has no bypass actors. The key remains a CI test key pinned only on bench devices, never
a release anchor.

| Threat | Control that stops it | Outcome |
| --- | --- | --- |
| Fork pull request (`pull_request` from a fork) | GitHub withholds repository and environment secrets from fork-triggered runs, and the run ref `refs/pull/N/merge` matches neither the tag nor `main` | No key |
| Same-repository pull request (`pull_request` from a branch) | Same-repository pull requests are not fork-restricted, and the run may change the workflow it runs because that workflow comes from the merge commit. The control is the ref: `refs/pull/N/merge` is neither a tag nor `main`, so the environment refuses the job | No key |
| Branch push other than `main` | The environment refuses the job before any step runs | No key |
| Main-ref run | `main` is admitted by `beta-draft`. Only a repository writer can dispatch the release workflow or add a main-triggered workflow; protected `main` and repository write access are the controls. The draft is invisible until `beta` approves | Key available to a writer-controlled job; no release-trust compromise because the key is a test anchor, and publication still needs the `beta` approval |
| `pull_request_target` | The run uses the base branch ref, `refs/heads/main`, which is now admitted. A fork pull request cannot change the base workflow, but a repository writer can add one. The event gets a write-capable token and repository secrets; a workflow that checks out and runs pull request code is a repository takeover | Key could be read by a writer-controlled workflow; the trust root stays repository write access |
| Unmatched tag | Only `app-beta-*` tags and `main` are admitted. The tag policy matches the tag name only, so `app-beta-probe` passes it; the exact tag grammar is enforced by the publish workflow's parser, before any build or secret use | No key unless the tag matches `app-beta-*` |
| Matching tag without approval | No reviewer on `beta-draft`, so the job runs and the key is available. The draft it signs stays invisible until it is published, which the workflow does only after the `beta` environment approves the publish job; a repository writer can also publish a draft by hand. The key is a CI test key pinned only on bench devices, never a release anchor | Key available; no release-trust compromise because the key is a test anchor |
| Direct change to the workflow on `main` | A main-ref run is now admitted, so a repository writer can push a workflow change and read the key from it. The same writer can also push a matching tag. Main has no required review and no required status check; the `main-protection` ruleset forbids deletion, force push, and non-linear history, and it has no bypass actors. The `beta` reviewer still gates publication, and SHA pinning keeps an action from changing under a reviewed commit | Key readable by a repository writer; no publication without the `beta` approval, and no release-trust compromise because the key is a test anchor |

The key is a CI test anchor. A writer who can read it can sign a bundle that only bench devices with
that anchor would trust; that is the same blast radius as a tag push. A release that a field device
trusts still needs the offline release key under [ADR 0029](../../docs/adr/0029-release-signing-and-key-custody.md).
The resolved [OQ-36](../../docs/open-questions.md#resolved-questions) row records the policy choice.

Do not add `refs/pull/*/merge`, any branch other than `main`, or any tag other than `app-beta-*` to
either environment's policy. The policy depends on those refs never matching. The probe runs and
the cleanup are recorded in the
[verification note](../../docs/notes/2026-11-22-beta-draft-secret-verification.md).

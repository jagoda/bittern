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

## Environment tag policy

The `beta` and `beta-draft` environments both allow the tag glob `app-beta-*`, which is the
documented `*` wildcard form. Neither environment allows a branch. The
[deployments and environments reference](https://docs.github.com/en/actions/reference/workflows-and-actions/deployments-and-environments)
(accessed 2026-11-22) documents the `*` wildcard and refers further syntax to Ruby `File.fnmatch`;
it does not document character classes or ranges as supported. The exact tag format is therefore
validated by the publish workflow before it creates the draft, not by an undocumented glob feature.
The workflow must accept only `app-beta-` followed by exactly six lowercase hexadecimal characters,
with generation starting at `000001`, and reject everything else.

The environment policy and the ADR 0031 tag scheme must change together. A change to either one
without the other either blocks a valid tag at deploy time or permits a tag the ADR does not
define.

A `workflow_dispatch` dry run runs at a branch ref, so the tag-only policy refuses the draft job
before it can read the key. The dry run on 2026-11-22 added a temporary `main` branch policy to
`beta-draft`, ran, deleted the policy, and verified that both environments list only
`tag app-beta-*` again. That is a named exception for a maintainer-run dry run, not the steady
state, and it must not be left in place. The procedure, the risk window, and the durable follow-up
are in
[the dry run note](../../docs/notes/2026-11-22-beta-dry-run-and-bench-rollout.md#the-environment-policy-exception-for-the-dry-run).

## Threat model for the key

The environment gate is a ref gate, not an actor gate. `beta-draft` has one protection rule, the
`app-beta-*` tag policy, and no required reviewer. A job that declares `environment: beta-draft` is
matched against the run's `GITHUB_REF`; a non-matching ref is refused before the job starts, so the
secret is never provided. A matching tag is admitted with no approval, by design, because the draft
must exist before the `beta` approver can review it.

That gate does not stop a repository writer. GitHub states that any user with write access has read
access to all secrets in the repository, and a writer can push a workflow with a matching tag, or
push a matching tag for a workflow already on main. The tag policy admits the tag, not the person.
The trust root is therefore write access to this repository, which today is the single admin
account, plus that account's own security. No tag protection rule exists, so tag creation is part
of that write access. The policy does keep branches, pull requests, forks, and main-ref runs away
from the key, and those are the cases below.

| Threat | Control that stops it | Outcome |
| --- | --- | --- |
| Fork pull request (`pull_request` from a fork) | GitHub withholds repository and environment secrets from fork-triggered runs, and the run ref `refs/pull/N/merge` does not match the tag policy | No key |
| Same-repository pull request (`pull_request` from a branch) | Same-repository pull requests are not fork-restricted, and the run may change the workflow it runs because that workflow comes from the merge commit. The control is the ref: `refs/pull/N/merge` is not a tag, so the environment refuses the job | No key |
| Branch push (`refs/heads/*`) | The environment refuses the job before any step runs. Verified on 2026-11-22 | No key |
| `pull_request_target` | The run uses the default branch workflow and the default branch ref, so the environment refuses the job, and a pull request cannot change that workflow. The event still gets a write-capable token and repository secrets; a workflow that checks out and runs pull request code is a repository takeover, not a way to widen the key's scope. No such workflow exists | No key from the environment; the trust root stays repository write access |
| Unmatched tag | The environment accepts only tags matching `app-beta-*`. The policy matches the tag name only, so `app-beta-probe` passes it; the exact tag grammar is enforced by the publish workflow's parser, before any build or secret use | No key unless the tag matches `app-beta-*` |
| Matching tag without approval | No reviewer on `beta-draft`, so the job runs and the key is available. Verified on 2026-11-22 with a dispatch at a matching tag. The draft it signs stays invisible until it is published, which the workflow does only after the `beta` environment approves the publish job; a repository writer can also publish a draft by hand. The key is a CI test key pinned only on bench devices, never a release anchor | Key available; no release-trust compromise because the key is a test anchor |
| Direct change to the workflow on main | A push or dispatch run at `refs/heads/main` is refused by the tag policy, so the changed workflow cannot read the key from a main-ref run. The same writer can also push a matching tag, and the tag run reads the key without approval. Main has no required review and no required status check; the `main-protection` ruleset forbids deletion, force push, and non-linear history, and it has no bypass actors | Key readable by a repository writer who also creates a matching tag |

A direct change to a workflow on main can read the key. The environment stops the main-ref run, but
the change can add a tag-triggered path, and whoever can push the change can also push the tag.
Nothing in the environment configuration prevents that. What protects main is the ruleset and
repository write access, not review, and the key's blast radius is limited by the key being a test
anchor. Adding a reviewer to `beta-draft` would put an approval in front of every draft and defeat
the reason the environment exists ([ADR 0032](../../docs/adr/0032-continuous-integration-and-build-pipeline.md));
it would not change the tag path, because the tag is what the environment admits.

Do not add `refs/pull/*/merge` or a branch pattern to either environment's policy. The current
policy depends on pull request and branch refs never matching. The probe runs and the cleanup are
recorded in the
[verification note](../../docs/notes/2026-11-22-beta-draft-secret-verification.md).

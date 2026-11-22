# Beta-draft key: documented gating and probe verification

- Date: 2026-11-22
- Status: verification note. A temporary probe confirmed that the `beta-draft` environment refuses
  branch and pull request refs before the job starts, and admits a matching `app-beta-*` tag with no
  approval. The probe was removed and the repository was returned to its prior state in the same
  session.
- Related: [trust anchors](../../packaging/trust/README.md),
  [beta draft environment](2026-11-22-beta-draft-environment.md),
  [ADR 0032](../adr/0032-continuous-integration-and-build-pipeline.md),
  [ADR 0029](../adr/0029-release-signing-and-key-custody.md),
  [ADR 0031](../adr/0031-update-channel-on-github-releases.md)

## Question

`BITTERN_CI_SIGNING_KEY` is an environment secret in `beta-draft`. That environment accepts only tag
deployments matching `app-beta-*` and has no required reviewer. Can a workflow triggered by anything
other than a matching tag read the secret?

## What GitHub documents

### Pull requests and secrets

> With the exception of `GITHUB_TOKEN`, secrets are not passed to the runner when a workflow is
> triggered from a forked repository.

[Using secrets in GitHub Actions](https://docs.github.com/en/actions/security-for-github-actions/security-guides/using-secrets-in-github-actions),
accessed 2026-11-22.

A pull request from a branch in the same repository is not a fork, so that withholding does not
apply. The documented trust boundary is write access:

> Any user with write access to your repository has read access to all secrets configured in your
> repository.

[Secure use reference](https://docs.github.com/en/actions/reference/security/secure-use), accessed
2026-11-22.

The difference for the `pull_request` event is put directly:

> The `pull_request` event (along with `pull_request_review` and `pull_request_review_comment`) is
> unusual: it runs the workflow file from the **merge commit of the pull request**. For a pull
> request opened from a fork, that commit is controlled by someone without write access to the base
> repository. To run untrusted workflow code safely, GitHub restricts these events to a read-only
> `GITHUB_TOKEN`, withholds access to other secrets, and applies fork approval policies to prevent
> compute abuse.

[Securely using pull_request_target](https://docs.github.com/en/actions/reference/security/securely-using-pull_request_target),
accessed 2026-11-22.

### Environment secrets and the deployment policy

> A job that references an environment must follow any protection rules for the environment before
> running or accessing the environment's secrets.

[Managing environments for deployment](https://docs.github.com/en/actions/how-tos/deploy/configure-and-manage-deployments/manage-environments),
accessed 2026-11-22.

> Secrets stored in an environment are only available to workflow jobs that reference the
> environment.

and

> Selected branches and tags: Only branches and tags that match your specified name patterns can
> deploy to the environment.
>
> The deployment branch or tag rule is matched against the `GITHUB_REF` of the workflow run.

[Deployments and environments](https://docs.github.com/en/actions/reference/workflows-and-actions/deployments-and-environments),
accessed 2026-11-22.

The same page also states that a job using an environment can access its secrets only after the
configured rules pass. The documentation states the requirement but does not name the failure mode
for a non-matching ref. The probe supplies it: the job is refused before it starts. It is not run
with an empty secret and it is not run normally.

### `pull_request_target`

> Workflows triggered by `pull_request_target` run with elevated trust: the job receives the base
> repository's `GITHUB_TOKEN` and access to repository and organization secrets.
>
> `pull_request_target` makes one critical and subtle change: the workflow, and any subsequent
> `actions/checkout` call that does not specify a `ref`, is taken from the base repository's default
> branch, not from the pull request. Because only trusted code from the default branch runs, it is
> safe to grant secrets and a read/write token.

[Securely using pull_request_target](https://docs.github.com/en/actions/reference/security/securely-using-pull_request_target),
accessed 2026-11-22.

> This event runs in the context of the default branch of the base repository, rather than in the
> context of the merge commit, as the `pull_request` event does.

[Events that trigger workflows](https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows),
accessed 2026-11-22. The same table gives `GITHUB_REF` as the default branch for
`pull_request_target`.

So a pull request cannot change the workflow that runs for `pull_request_target`; the workflow comes
from the default branch. For `pull_request` it can, because the workflow comes from the merge
commit. The environment policy, not the workflow content, is what keeps both away from the key:
the default branch ref and the merge ref are not tags.

### A declared environment with a non-matching ref

The environment secret is scoped to jobs that declare the environment, and those jobs must satisfy
the environment's rules before they run. A job can therefore declare `environment: beta-draft` on a
branch, but the ref policy refuses it. A job that does not declare the environment never receives
the environment secret at all, because environment secrets are only available to jobs that reference
the environment.

## Probe

The probe was a single temporary workflow, `.github/workflows/probe-env-secret.yml`, with
`workflow_dispatch` only, `permissions: contents: read`, and one job declaring
`environment: beta-draft`. Its only step read the secret into an environment variable and printed
the event name, `GITHUB_REF`, `GITHUB_REF_NAME`, whether the value was empty, and its length. It
never printed, hashed, encoded, or wrote the value, and it did not interact with `publish.yml`.

`workflow_dispatch` requires the workflow file on the default branch, so the probe file was added to
main for the test and removed by a revert commit in the same session. The temporary tag was
`app-beta-probe`: it matches the environment's `app-beta-*` glob but not the publish workflow's
exact generation grammar, so the publish workflow could not reach its draft job.

| Run | Event | Ref | Job | Secret empty | Length |
| --- | --- | --- | --- | --- | --- |
| `45056270929` | `workflow_dispatch` | `refs/heads/probe-env-secret` | Refused before any step | Job did not run | Job did not run |
| `45056326123` | `workflow_dispatch` | `refs/tags/app-beta-probe` | Ran, one step, success | `no` | `44` |

The branch run's job has no steps and the run annotation reads:

```text
Branch "probe-env-secret" is not allowed to deploy to beta-draft due to environment protection rules.
The deployment was rejected or didn't satisfy other protection rules.
```

The tag run's log contains only these result lines, with no value:

```text
event=workflow_dispatch
ref=refs/tags/app-beta-probe
ref_name=app-beta-probe
secret_empty=no
secret_length=44
```

A length of 44 is consistent with the base64 encoding of the 32-byte Ed25519 seed that the README
describes. The value itself was not printed, hashed, or stored.

Creating the tag through the REST API still produced a `push` event and started the publish
workflow once. `app-beta-probe` fails the repository tag parser (it is not six lowercase hex
characters), so the `validate tag` job failed at the parser, `checks`, `draft`, and `publish` were
skipped, and no draft or release was created. The publish run was deleted during cleanup.

## Cleanup

| Item | Result |
| --- | --- |
| Probe runs `45056270929`, `45056326123` | Deleted |
| Probe-triggered publish run `45056305827` | Deleted |
| Check run `45056257884` on the probe commit | Cancelled by the revert push, then deleted |
| Deployments `7983220492`, `7983213553` in `beta-draft` | Deleted, deployment list empty |
| Tag `app-beta-probe` | Deleted, `git ls-remote --tags` empty |
| Branch `probe-env-secret` | Deleted remotely, remote heads are `main` and `packer-and-publish` |
| Probe workflow on main | Removed by revert commit `419d77d`, `gh workflow list` shows `check`, `publish`, and `Dependency Graph` |
| Releases | None exist |
| Working tree | The revert commit restores the tree to `e425c71`; no probe file remains |

## Limit

The probe proves secret availability and ref gating only. It does not prove the signing, draft, and
approval path with a real generation tag. That path remains unproven until a real `app-beta-000001`
style tag runs.

## Triage

The durable control mapping is in [the trust anchors README](../../packaging/trust/README.md). The
finding is the trust-root statement: the environment policy is a ref gate, not an actor gate, and
repository write access plus the ability to create a matching tag is the real boundary. That is
documented, not deferred to an open question, because it follows from GitHub's documented write
access rule and from the environment configuration, not from an undecided choice.

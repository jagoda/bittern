# Beta draft environment and CI test key scope

- Date: 2026-11-22
- Status: implementation note and operational blocker. The `beta-draft` environment exists with the
  tag policy and no required reviewer. The CI test key has not been copied into it because GitHub
  secret values are write-only. The maintainer must re-upload the key before a tag run can sign.
- Related: [ADR 0032](../adr/0032-continuous-integration-and-build-pipeline.md),
  [ADR 0031](../adr/0031-update-channel-on-github-releases.md),
  [ADR 0029](../adr/0029-release-signing-and-key-custody.md),
  [beta launcher, platform, and draft note](2026-11-22-beta-launcher-platform-and-draft.md),
  [beta payload and publish note](2026-11-22-beta-payload-and-publish.md),
  [publish workflow](../../.github/workflows/publish.yml)

## Why there are two environments

A job that references an environment with required reviewers does not start until the approval. The
draft job creates the draft that the approver needs to inspect, so that job cannot be gated by the
approving environment. The CI test key also cannot be a repository secret: that would expose it to
every workflow in the repository and defeat the environment scope.

The key therefore lives in `beta-draft`, which has no required reviewer and is only reachable by a
tag-triggered run that matches the `app-beta-*` policy. The approval gate stays in `beta`, on the
publish job, which takes no build action.

## Environment readback

Read back with the environments API on 2026-11-22.

| Environment | Required reviewers | Admin bypass | Deployment tag policy |
| --- | --- | --- | --- |
| `beta` | `jagoda` | disabled | custom, `app-beta-*` tags only |
| `beta-draft` | none | disabled | custom, `app-beta-*` tags only |

`beta-draft` has a branch policy protection rule, `custom_branch_policies` true and
`protected_branches` false, with one tag policy named `app-beta-*`.

## Workflow permissions

| Job | Environment | Permissions | Key material |
| --- | --- | --- | --- |
| `tag` | none | `contents: read` | none |
| `checks` | none | reusable phase one, read-only | none |
| `draft` | `beta-draft` | `contents: write` | CI test key, decoded to a 0600 temporary file, removed by a trap and an `always()` step |
| `publish` | `beta` | `contents: write` | none |

No other job has `contents: write`, and no environment is required for the tag or check jobs.

## Secret state and the remaining command

The `beta` environment still lists `BITTERN_CI_SIGNING_KEY`. Its value cannot be read back through
the API or the CLI because GitHub secret values are write-only, so it cannot be copied
server-side. The `beta-draft` environment currently has no secrets. The value was not read, printed,
or moved by this change.

The maintainer must re-upload the same key to the new environment with this single command from the
repository checkout:

```sh
gh secret set BITTERN_CI_SIGNING_KEY --env beta-draft --repo jagoda/bittern
```

The command prompts for the secret value. Do not pass the value on the command line or write it to a
file. After the command, `gh secret list --env beta-draft` should list the secret by name; the value
stays unreadable.

The existing `beta` secret was not deleted. Whether to remove it after the new environment works is
the maintainer's decision, because the `beta` environment no longer needs the key for this workflow
but another runbook or manual step may still use it.

## Limit

The workflow has not run with the new environment. `beta-draft` has no key until the command above
runs, so a tag-triggered draft job would fail at the signing step. That is a deliberate fail-closed
state, not a successful signing path. No tag was created and no release was published.

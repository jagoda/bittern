# Beta draft environment and CI test key scope

- Date: 2026-11-22
- Status: implementation note. The `beta-draft` environment exists with the tag policy and no
  required reviewer. The first CI test key was retired and replaced by a new key in `beta-draft`.
  The `beta` environment holds no signing secret. No tag was created and no release was published.
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

## Key rotation

The first CI test key was generated in this container, uploaded to the `beta` environment, and its
local copy was shredded. Nobody held the value, and GitHub returns secret values as write-only, so
the key was rotated instead of re-uploaded. No device carried the first key's anchor, so replacing
it required no re-seeding.

The new key pair was generated in a private temporary directory with mode 0700. The seed file was
mode 0600 and the public file was mode 0644, both checked with `stat`. The seed was base64-encoded
and piped into `gh secret set --env beta-draft`; the value never appeared on a command line, in a
kept file, or in output. The stale `beta` secret was then deleted, and the private key and temporary
directory were shredded and removed.

New CI test key:

| Item | Value |
| --- | --- |
| Key id | `e19d358aa7caaca5e5ddb26560227a285a88bc6f136621f3972e0ac054b5c9f3` |
| Public key hex | `c4b17fe275cc4632b58c24a4cd09ee1da8d9fb904a5e1907e75601b882855285` |
| Public file | `packaging/trust/beta-ci.pub`, 32 raw bytes, mode 0644 |

`gh secret list --env beta` is empty. `gh secret list --env beta-draft` lists exactly one secret,
`BITTERN_CI_SIGNING_KEY`.

## Limit

The workflow has not run with the new key. A tag-triggered draft job has the secret available, but
the signing, draft, and approval path is not proven until a real tag run happens. No tag was created
and no release was published.

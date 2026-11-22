# Release call path dry run

- Date: 2026-11-22
- Status: fixed in this change. The release workflow now defaults to a dry run that calls the same
  reusable publish workflow as the publish path, with only the irreversible steps disabled. The
  call-path dry run is recorded below. No tag, draft, or release was created. The first publish
  dispatch and the `beta` approval remain unproven.
- Type: bug fix and verification note.
- Related: [ADR 0031](../adr/0031-update-channel-on-github-releases.md),
  [ADR 0032](../adr/0032-continuous-integration-and-build-pipeline.md),
  [ADR 0033](../adr/0033-dispatch-driven-release-trigger.md),
  [beta bench rollout](../runbooks/beta-bench-rollout.md),
  [principles](../principles.md),
  [release workflow](../../.github/workflows/release.yml),
  [publish workflow](../../.github/workflows/publish.yml),
  [dispatch release trigger note](2026-11-22-dispatch-release-trigger.md),
  [previous dry run note](2026-11-22-beta-dry-run-and-bench-rollout.md),
  [BIT-042](../plan/implementation-plan.md#bit-042-release-channel-and-ci-pipeline)

## Diagnosis

The maintainer's first dispatch of `release.yml` (run
[45083370630](https://github.com/jagoda/bittern/actions/runs/45083370630)) passed the release
preflight and failed in the called publish workflow at `validate tag`:

```text
build, tag, draft, publish / validate tag
workflow_dispatch is the dry-run path; push app-beta-000001 to publish
##[error]Process completed with exit code 1.
```

Inside a reusable workflow, `github.event_name` is the caller's event. The caller was a
`workflow_dispatch` of `release.yml`, so the called workflow saw `workflow_dispatch` even though it
was entered through `workflow_call`. The tag selection step took the direct-dispatch branch, saw
`dry_run=false` from the release call, and refused. The `checks`, `sign and stage draft`, and
`publish the verified draft` jobs were skipped because the tag job failed.

The previous green dry run, [45064681803](https://github.com/jagoda/bittern/actions/runs/45064681803),
dispatched `publish.yml` directly. It covered the direct-dispatch branch and did not cover the call
path. The branch that matters, `release.yml` calling `publish.yml` through `workflow_call`, had never
run.

## Conditions changed

| Location | Before | After |
| --- | --- | --- |
| `publish.yml` tag selection | `case "$EVENT_NAME" in push) ... workflow_dispatch) ... workflow_call) ...` chose the path from the event name | `if [ -z "$INPUT_TAG" ]` selects the push ref path, otherwise the input path. `dry_run` and `create_tag` are explicit inputs. The publish path is `dry_run=false` with `create_tag=true`; a tag input with `dry_run=false` and `create_tag=false` is refused. A direct dispatch never supplies `create_tag`, so it cannot publish. |
| `publish.yml` generation check | `if [ "$EVENT_NAME" = "workflow_call" ] && [ "$generation" != "$INPUT_GENERATION" ]` | `if [ -n "$INPUT_GENERATION" ] && [ "$parsed_generation" != "$INPUT_GENERATION" ]`. A supplied generation is compared; a push or direct dispatch has none. |
| `release.yml` release job | `needs.preflight.result == 'success' && inputs.confirm == needs.preflight.outputs.tag` | `needs.preflight.result == 'success' && (inputs.dry_run || inputs.confirm == needs.preflight.outputs.tag)` |
| `release.yml` called inputs | `create_tag: true`, `dry_run: false` | `create_tag: ${{ inputs.dry_run == false }}`, `dry_run: ${{ inputs.dry_run }}` |
| `publish.yml` publish job | `needs.tag.outputs.dry_run == 'false'` | Unchanged. It already used the normalized `dry_run` output, not the event name. |
| `publish.yml` dry-run and create-tag steps | `needs.tag.outputs.dry_run` and `needs.tag.outputs.create_tag` | Unchanged. They already used the normalized outputs. |

The push trigger still takes its tag from `github.ref` and parses it with `bittern-release tag`. The
called path takes the tag, generation, and commit from inputs. The selected commit is now an output
and is used for the manifest source check, the tag creation, and the release notes.

## Dry-run call path result

Run pending.

## Proof that nothing was created

Run pending.

## Finding

The previous dry run exercised only the direct-dispatch branch, not the call path. A dry run that
exercises only one branch proves nothing about the branch that matters. A test mode must run the same
conditions as the real mode with only the irreversible steps disabled.

The rule is recorded in [OP-1](../principles.md#op-1-a-repeated-manual-bridge-becomes-an-idempotent-script-whose-dry-run-covers-the-real-path),
which was revised rather than duplicated.

## Next step

Run `make release-preflight`, dispatch `release.yml` with `dry_run` true again on the new `main`
head, then dispatch with `dry_run` false and the computed tag in `confirm`. Approve `beta` and
record the tag creation, draft upload, run URLs, and release URL in
[the beta bench rollout](../runbooks/beta-bench-rollout.md).

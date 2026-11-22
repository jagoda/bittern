# Dispatch-driven release trigger

- Date: 2026-11-22
- Status: implementation note. [ADR 0033](../adr/0033-dispatch-driven-release-trigger.md) makes a
  manual dispatch of `release.yml` the normal beta release path and keeps the tag push as the
  fallback. The preflight, the release workflow, the reusable publish workflow, and the runbook are
  in place. Both environments now admit the `app-beta-*` tag and the `main` branch, and the live
  dispatch preflight passes. The first dispatch release remains unproven. No tag, draft, or release
  was created.
- Type: implementation note.
- Related: [ADR 0033](../adr/0033-dispatch-driven-release-trigger.md),
  [ADR 0031](../adr/0031-update-channel-on-github-releases.md),
  [ADR 0032](../adr/0032-continuous-integration-and-build-pipeline.md),
  [ADR 0029](../adr/0029-release-signing-and-key-custody.md),
  [beta bench rollout](../runbooks/beta-bench-rollout.md),
  [release preflight](../../packaging/release_preflight.py),
  [release workflow](../../.github/workflows/release.yml),
  [publish workflow](../../.github/workflows/publish.yml),
  [OQ-36](../open-questions.md),
  [trust README](../../packaging/trust/README.md),
  [BIT-042](../plan/implementation-plan.md#bit-042-release-channel-and-ci-pipeline)

## What landed

| Area | Change |
| --- | --- |
| Decision | ADR 0033 supersedes [ADR 0031](../adr/0031-update-channel-on-github-releases.md) point 3 for the trigger only. The tag is still the release identity and must equal the manifest generation. ADR 0031's status is `Accepted; tag-as-publish-signal decision superseded in part by ADR-0033`, the partial form in the ADR README. The decision text is unchanged. |
| Preflight | `packaging/release_preflight.py` and `make release-preflight`. The maintainer's local preflight verifies the exact `main` check run, the `BITTERN_CI_SIGNING_KEY` name in `beta-draft`, the tag and branch policies on both environments, the `beta` reviewer and admin bypass, the next generation, the tag absence, and the Go tag parser. The workflow's preflight job declares no environment and reads no key; the draft job's `beta-draft` gate is where the key is present and used. `--plan` prints the plan; `--tag-path` checks the tag-push fallback without the `main` branch requirement. It never creates a tag, draft, or release. |
| Tests | `tests/release/test_release_preflight.py`, wired into `make check`: no releases, a contiguous history, a gap, a draft, the other channel, a malformed published tag, hexadecimal versus decimal, a computed tag that already exists, and one test per precondition failure message. |
| Release workflow | `release.yml` is `workflow_dispatch` only with an optional `confirm` tag. It runs the preflight, then calls `publish.yml` with the computed tag, generation, and commit. The concurrency group is `bittern-release-beta`, `cancel-in-progress: false`. The workflow level is `contents: read` and `actions: read`; the release job raises `contents: write` for the called tag, draft, and publish jobs. |
| Publish workflow | `publish.yml` is now callable with `workflow_call`, keeps the `push: tags` and `workflow_dispatch` dry-run triggers, and validates the call's generation with the same Go parser. The draft job creates the tag after the signature verification when `create_tag` is true, checks the remote first, and then creates the draft. The publish job uses the tag output, not `github.ref_name`, and never receives the CI test key. |
| Runbook | [Beta bench rollout](../runbooks/beta-bench-rollout.md) uses `make release-preflight` instead of the inline precondition commands. Dispatch is the normal path; the tag push is the fallback. The two-release sequence and the expectation table are unchanged. |

## The environment ref decision

A `workflow_dispatch` at `main` carries `refs/heads/main`, not a tag. A workflow run carries one ref
for all its jobs, so the preflight, the signing job, and the approval job all run at `main`. The tag
is created by the signing job after the build and signature succeed; by the time the publish job
runs, the tag exists, but the run's ref is still `refs/heads/main`. The environment policy is
evaluated against the run's ref, not against a tag created during the run or a workflow input.

Both environments now admit the `app-beta-*` tag and the `main` branch, and no other ref. The
readback after the change:

```text
beta-draft: branch main, tag app-beta-*
beta:       branch main, tag app-beta-*
```

The policy change does not widen the trust root. A tag push and a main-ref workflow both require
repository write access; the actors are the same. The `beta` reviewer remains the publication gate,
SHA pinning keeps an action from changing under a reviewed commit, and the protected `main` ruleset
forbids deletion, force push, and non-linear history. The key remains a CI test key pinned only on
bench devices. The full control mapping is in [the trust README](../../packaging/trust/README.md).
[OQ-36](../open-questions.md) is resolved by ADR 0033.

The live dispatch preflight is green against the live repository after the policy change:

```text
$ make release-preflight RELEASE_PREFLIGHT_ARGS=--plan
preflight: plan (no tag, draft, or release will be created)
repository: jagoda/bittern
main head: cd83eaf1af47fd9aeea939427a9deeb0b9853623
check run: 45072280446 https://github.com/jagoda/bittern/actions/runs/45072280446 success
secret: BITTERN_CI_SIGNING_KEY present in beta-draft (name only)
environment beta-draft: tags app-beta-*; branches main
environment beta: tags app-beta-*; branches main
published beta: none
tag: app-beta-000001
commit: cd83eaf1af47fd9aeea939427a9deeb0b9853623
generation: 1
next: gh workflow run release.yml --ref main -f confirm=app-beta-000001
```

The tag-push fallback still passes its tag-path check and prints the tag command. The dispatch path
is now the normal one; the fallback remains for a release workflow outage or a bootstrap.

## Verification

- `make check` passes in the worktree, including the 33 preflight tests.
- `go vet ./...` and `gofmt -l` pass for the module; the vendor tree is excluded.
- `actionlint` passes for all three workflows.
- `gh workflow view` shows `release.yml` as `workflow_dispatch` only and `publish.yml` with the
  `push: tags app-beta-*` fallback, the dry-run dispatch, and `workflow_call`.
- The live dispatch preflight passes with the two environment policies above.
- A plan-only dispatch of `release.yml` at `main` (run
  [45074716505](https://github.com/jagoda/bittern/actions/runs/45074716505)) ran the no-environment
  preflight job, printed `preflight: ok`, and skipped the release job because the confirmation was
  empty. It created no tag, draft, or release.
- No tag, draft, or release exists after the change. The preflight and the plan-only dispatch
  created none.

## What remains unproven

- The first confirmed dispatch release: the tag creation, the draft upload, the `beta` approval,
  and the visible publish. The plan-only dispatch proved the no-environment preflight but built
  nothing.
- The tag-creation step's API path and the draft's five-asset check in the same run.
- The tag-push fallback with the real `beta` approval. It was already unproven before this change.
- The bench rollout itself. The kit install, the first update, the second update, and the rollback
  are unchanged.

## Next step

Run `make release-preflight`, dispatch `release.yml` at `main` with the printed tag, approve the
`beta` deployment, and record the release run and release URLs in
[the beta bench rollout](../runbooks/beta-bench-rollout.md). The first dispatch release proves the
tag creation, the draft upload, the approval, and the visible publish.

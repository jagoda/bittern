# Dispatch-driven release trigger

- Date: 2026-11-22
- Status: implementation note. [ADR 0033](../adr/0033-dispatch-driven-release-trigger.md) makes a
  manual dispatch of `release.yml` the normal beta release path and keeps the tag push as the
  fallback. The preflight, the release workflow, the reusable publish workflow, and the runbook are
  in place. The dispatch preflight refuses on the live repository because `beta-draft` and `beta`
  still admit only the `app-beta-*` tag; the `main` branch policy is the open decision. No tag,
  draft, or release was created.
- Type: implementation and blocker note.
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
| Preflight | `packaging/release_preflight.py` and `make release-preflight`. It verifies the exact `main` check run, the `BITTERN_CI_SIGNING_KEY` name in `beta-draft`, the tag and branch policies on both environments, the `beta` reviewer and admin bypass, the next generation, the tag absence, and the Go tag parser. `--plan` prints the plan; `--tag-path` checks the tag-push fallback without the `main` branch requirement. It never creates a tag, draft, or release. |
| Tests | `tests/release/test_release_preflight.py`, wired into `make check`: no releases, a contiguous history, a gap, a draft, the other channel, a malformed published tag, hexadecimal versus decimal, a computed tag that already exists, and one test per precondition failure message. |
| Release workflow | `release.yml` is `workflow_dispatch` only with an optional `confirm` tag. It runs the preflight, then calls `publish.yml` with the computed tag, generation, and commit. The concurrency group is `bittern-release-beta`, `cancel-in-progress: false`. The workflow level is `contents: read` and `actions: read`; the release job raises `contents: write` for the called tag, draft, and publish jobs. |
| Publish workflow | `publish.yml` is now callable with `workflow_call`, keeps the `push: tags` and `workflow_dispatch` dry-run triggers, and validates the call's generation with the same Go parser. The draft job creates the tag after the signature verification when `create_tag` is true, checks the remote first, and then creates the draft. The publish job uses the tag output, not `github.ref_name`, and never receives the CI test key. |
| Runbook | [Beta bench rollout](../runbooks/beta-bench-rollout.md) uses `make release-preflight` instead of the inline precondition commands. Dispatch is the normal path; the tag push is the fallback. The two-release sequence and the expectation table are unchanged. |

## The environment ref blocker

A `workflow_dispatch` at `main` carries `refs/heads/main`, not a tag. The preflight and draft jobs
need the `beta-draft` key, and the publish job needs the `beta` approval, so both environments need
a `main` branch policy in addition to `app-beta-*`. Both live environments currently admit only
`tag app-beta-*`; neither allows a branch.

This is the exact failure the dispatch preflight reports:

```text
$ make release-preflight RELEASE_PREFLIGHT_ARGS=--plan
preflight: fail (plan mode)
fail: [environment-dispatch-ref] the beta-draft environment does not admit the main branch; a workflow_dispatch at refs/heads/main is refused before the job starts. Add a main branch policy to beta-draft
fail: [environment-dispatch-ref] the beta environment does not admit the main branch; a workflow_dispatch at refs/heads/main is refused before the job starts. Add a main branch policy to beta
make: *** [Makefile:...: release-preflight] Error 1
```

The tag-push fallback is not blocked. The tag-path plan passes and prints the generation and the
next action:

```text
$ make release-preflight RELEASE_PREFLIGHT_ARGS="--plan --tag-path"
preflight: plan (no tag, draft, or release will be created)
repository: jagoda/bittern
main head: 0432516dabc604d0912893ae7f6fd30d51ba2dd7
check run: 45066613170 https://github.com/jagoda/bittern/actions/runs/45066613170 success
secret: BITTERN_CI_SIGNING_KEY present in beta-draft (name only)
environment beta-draft: tags app-beta-*; branches none
environment beta: tags app-beta-*; branches none
published beta: none
tag: app-beta-000001
commit: 0432516dabc604d0912893ae7f6fd30d51ba2dd7
generation: 1
next: git tag app-beta-000001 0432516dabc604d0912893ae7f6fd30d51ba2dd7 && git push origin app-beta-000001
```

The decision is [OQ-36](../open-questions.md). The dispatch path needs the permanent `main` policy
on `beta-draft` and `beta`; the dry-run path also uses it, so the temporary policy procedure can be
retired. The alternative is a branch-scoped dispatch environment with its own key, which keeps the
tag-only policy but duplicates the environment and the key scope. The policy is not applied in this
change because OQ-36's provisional answer says not to add the permanent `main` policy without a
decision. The maintainer applies it with:

```sh
for env in beta-draft beta; do
  gh api -X POST "repos/jagoda/bittern/environments/$env/deployment-branch-policies" \
    -f name=main -f type=branch
done
```

After the policy, `make release-preflight` should print `preflight: ok` and the dispatch next action.
The trust boundary does not move: the key is the CI test key pinned only on bench devices, the trust
root is repository write access, and `beta` still requires the maintainer's approval.

## Verification

- `make check` passes in the worktree, including the 33 new preflight tests.
- `go vet ./...` and `gofmt -l` pass for the module; the vendor tree is excluded.
- `gh workflow view` returns the new release workflow and the changed publish workflow after `main`
  is pushed.
- No tag, draft, or release exists after the change. The preflight did not create one, and neither
  workflow was dispatched.

## What remains unproven

- The first dispatch release: the preflight, the tag creation, the draft upload, the `beta` approval,
  and the visible publish. The live environment policy must be applied first.
- The tag-creation step's API path and the draft's five-asset check in the same run.
- The tag-push fallback with the real `beta` approval. It was already unproven before this change.
- The bench rollout itself. The kit install, the first update, the second update, and the rollback
  are unchanged.

## Next step

Apply the `main` branch policy to `beta-draft` and `beta`, run `make release-preflight` until it
prints the dispatch next action, dispatch `release.yml` at `main` with the printed tag, approve the
`beta` deployment, and record the release run and release URLs in
[the beta bench rollout](../runbooks/beta-bench-rollout.md). If the maintainer rejects the policy,
open a new decision for a branch-scoped dispatch environment and update ADR 0033 before the first
dispatch release.

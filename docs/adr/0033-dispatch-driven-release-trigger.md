# 0033. Dispatch-driven release trigger

*Publication is triggered by a manual dispatch that runs the preconditions, builds and signs, and
then creates the tag and the draft; a tag push remains the fallback path.*

- Status: Accepted
- Date: 2026-11-22
- Deciders: maintainer
- Related: [ADR 0029](./0029-release-signing-and-key-custody.md),
  [ADR 0031](./0031-update-channel-on-github-releases.md),
  [ADR 0032](./0032-continuous-integration-and-build-pipeline.md),
  [BIT-042](../plan/implementation-plan.md#bit-042-release-channel-and-ci-pipeline),
  [OQ-36](../open-questions.md), [beta bench rollout](../runbooks/beta-bench-rollout.md),
  [release preflight](../../packaging/release_preflight.py),
  [release workflow](../../.github/workflows/release.yml),
  [publish workflow](../../.github/workflows/publish.yml)

## Context

[ADR 0031](./0031-update-channel-on-github-releases.md) makes the tag the publish signal and maps
it one to one onto the application bundle generation. [ADR 0032](./0032-continuous-integration-and-build-pipeline.md)
puts the draft and publish path on a tag push: the tag exists first, the workflow builds and signs,
uploads the five assets to a draft, and publishes after the `beta` environment approval. The
`workflow_dispatch` path added on 2026-11-22 is a dry run only. It validates the tag, builds and
signs the same bytes, and creates no tag, draft, or release. The dry run proved the build, key
decode, signature, and digest path; no release has been published.

The tag-first order has two problems.

**A token-pushed tag does not start another workflow run.** GitHub suppresses workflow runs for
events created with the repository's `GITHUB_TOKEN`, with the exception of `workflow_dispatch` and
`repository_dispatch` (GitHub Docs, "Triggering a workflow", accessed 2026-11-22). A dispatch
cannot create the tag and then rely on the existing `push: tags` trigger to publish it. The dispatch
must run the build and publication itself, or use a second credential to push the tag. A second
credential is a new secret and a new actor for no benefit.

**A tag created before the build burns a generation when the build or signature fails.** ADR 0031
makes a published release immutable and says a tag is never moved or reused. A tag created first
exists even if the checks, build, or signature then fail, and the next attempt must skip that
generation. The generation is the device's anti-rollback counter, so a skipped generation is not
recoverable by retrying the same number.

The release event is one maintainer action. The preconditions that a maintainer used to run by hand
are in the [beta bench rollout](../runbooks/beta-bench-rollout.md): a green check run on the exact
`main` head, the `BITTERN_CI_SIGNING_KEY` name in `beta-draft`, the two environment policies, the
next free generation, and the Go tag rule. The runbook carried those commands inline. A repeated
procedure belongs in a checked-in script that fails with a specific reason, not in prose.

## Decision

**1. Publication is triggered by a manual dispatch of the release workflow.** The maintainer
dispatches `release.yml` at `main`, optionally confirming the tag that `make release-preflight`
printed. The workflow runs the preflight, builds the unsigned assets, signs the manifest in the
`beta-draft` environment, creates the tag at the checked commit, creates the draft and uploads the
five assets, and publishes the draft after the `beta` environment approval. The tag is created only
after the build and the signature succeed.

**2. The tag-push trigger remains as a fallback path.** The existing `publish.yml` `push: tags:
app-beta-*` trigger stays. It validates the pushed tag, runs the same checks, signs, creates the
draft against the existing tag, and publishes after `beta` approval. It does not compute the
generation, so the pushed tag must be the next generation. The fallback is for a release workflow
outage, a bootstrap, or a repair; it is not the normal procedure.

**3. The tag keeps its meaning as the release identity.** The tag is still the selector the device
filters on, and its generation must equal the signed manifest's `generation`. The tag is never a
trust input; the manifest integer remains authoritative. The device still reads the highest
published `app-beta-*` generation and rejects a tag that does not match the manifest.

This supersedes [ADR 0031](./0031-update-channel-on-github-releases.md) point 3 only for the
trigger. ADR 0031's sentence "the tag is the publish signal" no longer describes how publication
starts; it still describes what the tag is. ADR 0031 is marked `Accepted; tag-as-publish-signal
decision superseded in part by ADR-0033`, following the partial-supersession form in
[the ADR README](./README.md). The decision text of ADR 0031 is not edited.

**4. The preconditions are a checked-in script, not prose.** `make release-preflight` runs
`packaging/release_preflight.py`. The script:

- verifies the current `main` head has a completed, successful `check.yml` run on that exact
  commit, and refuses if the head moved after the run or during a release run;
- verifies `BITTERN_CI_SIGNING_KEY` exists in `beta-draft` by name only, never reading or printing
  a value;
- verifies both environments admit the `app-beta-*` tag, the `main` branch for the dispatch path,
  and no other ref; verifies `beta` requires the maintainer as reviewer and has
  `can_admins_bypass` off;
- computes the next generation as the highest published `app-beta-*` release generation plus one,
  ignoring drafts, other channels, and tags without a published release, and refuses if the
  computed tag already exists;
- validates the computed tag with `bittern-release tag`, the same Go parser the publish workflow
  uses, rather than a shell copy of the rule;
- prints the tag, the commit, the generation, and the exact next action, and exits non-zero with a
  specific reason per failure.

`--plan` performs every check and prints the result without changing anything. The script never
creates a tag, draft, or release in any mode.

**5. The dispatch ref needs the default branch admitted by both environments.** A
`workflow_dispatch` at `main` carries `refs/heads/main`, not a tag. The build and signature job uses
`beta-draft`, and the publish job uses `beta`. Both environments admit only the `app-beta-*` tag
today, so a dispatch at `main` is refused before the key is read. The dispatch path therefore
requires a `main` branch policy on both environments, in addition to the tag policy. The tag-push
fallback continues to use the tag policy.

This is a repository setting, not a code change. The preflight verifies it and refuses with
`environment-dispatch-ref` until it is applied. The alternative, a separate branch-scoped signing
environment with its own key, is rejected for now: it duplicates the environment and key scope for
the same CI test key, and the task keeps the key in `beta-draft` and the approval in `beta`.
[OQ-36](../open-questions.md) records the dry-run history and the policy decision. The trust
boundary does not move: the key is a CI test key pinned only on bench devices, the trust root
remains repository write access, and `beta` still requires the maintainer's approval.

**6. Concurrency, permissions, and key handling.** `release.yml` uses the `bittern-release-beta`
concurrency group with `cancel-in-progress: false`, so two runs cannot compute the same generation
while the first is waiting for approval. The workflow has `contents: read` and `actions: read` at
the workflow level. Only the draft and publish jobs have `contents: write`. The CI test key is held
by the `beta-draft` environment on the draft job, is removed by the existing `always()` cleanup
step, and is never passed to the publish job. Every action stays SHA-pinned.

**7. A failure before the tag creates no tag.** The preflight, the checks, the build, the asset
check, the signature, and the signature verification all run before the tag creation step. A
failure in any of them stops the run with no tag and no draft. The tag creation checks the remote
one more time so a concurrent release cannot reuse the generation. A failure after the tag, such as
a draft upload failure, leaves the tag and requires a new generation or a draft repair; the tag is
never moved.

## Consequences

### Positive

- A failed check, build, or signature no longer burns a generation, because the tag is created
  after the build and signature succeed.
- One dispatch starts the release, and the preconditions are executable, tested, and printed with
  the next action.
- The tag remains the release identity and the generation selector; the device path does not
  change.
- The tag-push fallback keeps a manual recovery path that uses the same build, signature, asset
  set, and approval gate.
- The preflight's generation arithmetic and failure messages are tested without a release or a
  network call.

### Negative / costs

- The dispatch path widens the environment ref gate from tags to tags plus `main`. The key is a CI
  test key, but the tag-only gate no longer covers the release workflow's main-ref run.
- The release workflow is another workflow to maintain, review, and keep SHA-pinned.
- The tag and the draft are not one atomic transaction. A draft upload failure after the tag leaves
  a tag without a visible release, and the next attempt must use a new generation.
- The fallback tag path can still burn a generation if the build or signature fails after the tag
  is pushed. That is why it is the fallback.
- The dispatch runs at `main`, so the checked commit is the dispatch commit. A push after the
  dispatch does not move the release: the tag points at the commit the preflight checked. The
  preflight refuses if the head moved before it ran, so the maintainer dispatches again rather
  than releasing a head without a green run.
- The `beta` approval remains one interactive step after the draft is complete.

### Follow-ups

- Apply the `main` branch policy to `beta-draft` and `beta` before the first dispatch release.
  Then run `make release-preflight`, dispatch `release.yml` with the computed tag, approve `beta`,
  and record the first dispatch publish in the beta bench rollout.
- Update [OQ-36](../open-questions.md) and [the trust README](../../packaging/trust/README.md) when
  the policy is applied. The dry-run temporary policy procedure is no longer needed once `main` is
  admitted permanently; the no-secret dry-run environment remains a separate option.
- Prove the tag creation, the draft upload, the `beta` approval, and the visible publish on the
  first dispatch. Record the run and release URLs in the runbook's record table.
- Decide whether the tag-push fallback stays after the dispatch path is proven. It is kept now
  because it is the only path that can publish when the release workflow is unavailable.
- Add the first published generation to the update-history and support-bundle surfaces if the
  release event needs to be visible there.

## Alternatives considered

- **Keep the tag push as the only trigger.** It is the current path and needs no new workflow, but a
  failed build burns a generation and the dispatch requirement is not met. Rejected.
- **Dispatch, then push the tag and let the tag workflow publish.** A tag pushed with the repository
  token does not start another workflow run. A personal access token or GitHub App token would start
  it, but it adds a credential and still creates the tag before the build. Rejected.
- **Create the tag before the build and move it on failure.** A release tag is the release identity
  and ADR 0031 forbids moving or reusing it. Moving it would also break the device's generation
  comparison. Rejected.
- **A separate release branch or tag namespace.** It adds a branch or a second tag grammar, while
  the device still filters on `app-beta-*` and the generation still has to be unique. It does not
  remove the environment ref problem. Rejected.
- **Build and sign locally, then push the tag from the maintainer's machine.** The tag is created
  before the CI build and signature, the build is not the CI build, and the local key path is the
  airgapped release key rather than the CI test key. Rejected.
- **A separate branch-scoped signing environment with its own key.** It keeps both environments
  tag-only, but duplicates the environment and key scope for the same CI test key. It remains the
  fallback option if the maintainer rejects the `main` policy; the dispatch path would then need a
  new environment and a key copy. Rejected for now in favour of the smaller policy change.

## References

- [ADR 0029: release signing and key custody](./0029-release-signing-and-key-custody.md)
- [ADR 0031: update channel on GitHub Releases](./0031-update-channel-on-github-releases.md)
- [ADR 0032: continuous integration and build pipeline](./0032-continuous-integration-and-build-pipeline.md)
- [BIT-042: release channel and CI pipeline](../plan/implementation-plan.md#bit-042-release-channel-and-ci-pipeline)
- [Open questions](../open-questions.md), including OQ-36
- [Beta bench rollout](../runbooks/beta-bench-rollout.md)
- [Release preflight](../../packaging/release_preflight.py)
- [Release workflow](../../.github/workflows/release.yml)
- [Publish workflow](../../.github/workflows/publish.yml)
- [Dispatch release trigger note](../notes/2026-11-22-dispatch-release-trigger.md)
- GitHub Docs, "Triggering a workflow", triggering a workflow from a workflow:
  <https://docs.github.com/en/actions/how-tos/writing-workflows/choosing-when-your-workflow-runs/triggering-a-workflow#triggering-a-workflow-from-a-workflow>
- GitHub Docs, "Deployments and environments":
  <https://docs.github.com/en/actions/reference/workflows-and-actions/deployments-and-environments>

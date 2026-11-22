# Publish dry run and the beta bench rollout runbook

- Date: 2026-11-22
- Status: verification and documentation note. The publish workflow's `workflow_dispatch` dry run ran
  green and created no tag, draft, or release. The two-release bench runbook is written and has not
  been executed. The tag-triggered publish with the real `beta` approval remains unproven until the
  maintainer pushes the first tag.
- Type: verification and documentation note.
- Related: [ADR 0031](../adr/0031-update-channel-on-github-releases.md),
  [ADR 0032](../adr/0032-continuous-integration-and-build-pipeline.md),
  [ADR 0029](../adr/0029-release-signing-and-key-custody.md),
  [beta bench rollout](../runbooks/beta-bench-rollout.md),
  [beta payload and publish](2026-11-22-beta-payload-and-publish.md),
  [beta kit wave three](2026-11-22-beta-kit-wave-three.md),
  [beta draft environment](2026-11-22-beta-draft-environment.md),
  [update channel and manual rollback](2026-11-22-update-channel-check-and-manual-rollback.md),
  [QEMU update scenario](2026-11-22-qemu-update-scenario.md),
  [publish workflow](../../.github/workflows/publish.yml),
  [release asset name check](../../tests/checks/release_asset_names.py),
  [BIT-009](../plan/implementation-plan.md#bit-009-app-bundle-update-transaction-with-rollback),
  [BIT-042](../plan/implementation-plan.md#bit-042-release-channel-and-ci-pipeline)

## What landed

| Area | Change |
| --- | --- |
| Dry run | `publish.yml` gains a `workflow_dispatch` trigger with a `tag` input (default `app-beta-000001`) and a `dry_run` boolean (default true). The tag job selects the tag from the event, refuses a dispatch at a tag ref, refuses `dry_run=false`, and validates the tag with `bittern-release tag`, the same Go parser a push uses. |
| Dry-run path | The reusable checks build the payload, manifest, kit, and checksums. The draft job decodes the `beta-draft` CI test key, signs the manifest, verifies the signature against `packaging/trust/beta-ci.pub`, prints the five asset names with their SHA-256 digests, the manifest generation, channel, and source commit, and confirms that no tag and no release exist. The create, upload, and publish steps are skipped. The key is removed by the existing trap and the `always()` step. |
| Publish path | At the time of this run, a tag push was the only publish path. It keeps the literal tag ref guard, the draft creation with exactly five assets, the recorded digests, and the `beta` environment approval. A dispatch with `dry_run=false` fails before any check runs. [ADR 0033](../adr/0033-dispatch-driven-release-trigger.md) later makes a manual dispatch of `release.yml` the normal path and keeps the tag push as the fallback; the dry-run behaviour in this note is unchanged. |
| Naming check | `tests/checks/release_asset_names.py` compares the publish workflow, ADR 0031, the device channel client, the packer, and the Makefile. It fails when the five names drift or when the kit or `SHA256SUMS` is treated as an update asset. It is part of `make check`. |
| Runbook | [Beta bench rollout](../runbooks/beta-bench-rollout.md) records the preconditions, the two-release sequence, the expected card, device, and channel states, the failure states, the evidence to collect, and the honest gaps. |

No ADR decision changed. The tag scheme, asset names, trust anchors, and publication gate are the
same. The dry run adds no release path.

## Dry-run result

The workflow was committed as `8ad23e3e7839804382ba8f4b507483075afa4a25` and pushed to `main`. The
dispatch ran at that commit:

```sh
gh workflow run publish.yml --ref main --field tag=app-beta-000001 --field dry_run=true
```

| Field | Value |
| --- | --- |
| Run | [45064681803](https://github.com/jagoda/bittern/actions/runs/45064681803) |
| Event | `workflow_dispatch` at `main` |
| Source commit | `8ad23e3e7839804382ba8f4b507483075afa4a25` |
| Conclusion | success |
| Tag validated | `app-beta-000001` |
| Manifest generation | 1 |
| Manifest channel | `beta` |

Jobs: `validate tag`, `checks and unsigned test build`, and `sign and stage draft` passed.
`publish the verified draft` was skipped. Inside the draft job, the create, confirm, and publish
steps were skipped; `Print the dry-run result` and `Confirm the dry run created no tag and no
release` ran. The step summary printed:

```text
### Publish dry run: app-beta-000001

No tag, draft, release, or upload was created.

- Manifest generation: 1
- Manifest channel: beta
- Manifest source commit: 8ad23e3e7839804382ba8f4b507483075afa4a25
- Generated at: 2026-11-22T20:11:07Z
```

| Asset | SHA-256 |
| --- | --- |
| `manifest.json` | `c1328892318748c6104db0f232a52d74bb83843cd34e33c08ebd9ec793f2f14d` |
| `manifest.json.sig` | `508e6e337d4d96d5675f9fda4167baed197d3686f5eef0950d96cdaa25f9e82c` |
| `bittern-app.tar.gz` | `10af7a43921720d84cfaf07abed96b0ce669af2fac386950b0ab24f2eb6f09b7` |
| `bittern-beta-kit.tar.gz` | `7d39e73f084952f204ee9e5b242f3a8d07547c7dd6604a4de717fb8b546c0e18` |
| `SHA256SUMS` | `5fa6bcb00f1c956fabdd438ef3bdd4f2072d49085b9de99803df193d95717455` |

The CI build's `created` timestamp is `2026-11-22T20:10:20Z`, so these bytes and digests are tied to
that run. The unsigned test artifact from the same run was downloaded after the dry run, and its
`manifest.json`, `bittern-app.tar.gz`, `bittern-beta-kit.tar.gz`, and `SHA256SUMS` digests match the
table. `manifest.json.sig` exists only in the signed draft job, so its digest is from the run log. A
local `make beta-payload` with the same tree and a different `PAYLOAD_CREATED` produces a different
manifest digest and a different signature.

The dry-run guard printed `confirmed: no tag and no release named app-beta-000001`. After the run:

```text
$ gh release list
$ git ls-remote --tags origin
$ gh api repos/jagoda/bittern/git/ref/tags/app-beta-000001
{"message":"Not Found","documentation_url":"https://docs.github.com/rest/git/refs#get-a-reference","status":"404"}
```

There is no release, no draft, and no tag.

### The environment policy exception for the dry run

`beta-draft` is tag-scoped to `app-beta-*`, and a `workflow_dispatch` run at `main` is a branch ref,
so the environment refuses the draft job before it can read the key. The dry run proves the secret
decode and the signature path only if it runs in that environment, so the run used a temporary
branch policy:

1. Add a `main` branch policy to `beta-draft`.
2. Dispatch the dry run at `main`.
3. Delete the temporary policy as soon as the run settles.
4. Verify that `beta-draft` and `beta` each list only `tag app-beta-*`.

```sh
# 1. Record the new policy id, then dispatch from another terminal.
gh api -X POST repos/jagoda/bittern/environments/beta-draft/deployment-branch-policies \
  -f name=main -f type=branch
gh api repos/jagoda/bittern/environments/beta-draft/deployment-branch-policies \
  --jq '.branch_policies[] | "\(.id) \(.type) \(.name)"'

# 3. Delete the id that is not the app-beta-* tag policy.
gh api -X DELETE \
  repos/jagoda/bittern/environments/beta-draft/deployment-branch-policies/<main-policy-id>

# 4. Both environments must list only the tag policy.
for env in beta beta-draft; do
  gh api "repos/jagoda/bittern/environments/$env/deployment-branch-policies" \
    --jq '.branch_policies[] | "\(.type) \(.name)"'
done
```

The temporary policy was created and deleted in this run; the verification above was taken after the
deletion. The window is bounded by the maintainer's session. It adds no new actor to the key: the
trust root is repository write access, and the same writer can already push a matching tag and get
the key. It does widen the ref set for the duration, so the policy must not be left in place. The
steady state is tag-only.

This is a named exception, not the normal procedure. The durable fix is a dry-run path that does not
need the key at a branch ref, or a decision to give a dry-run environment its own reviewed test key.
It is recorded as [OQ-36](../open-questions.md), with the temporary procedure as the provisional
default and the no-secret dry-run environment as the durable removal. The tag path needs no
exception, because the tag ref is what the environment admits.

### A check-workflow flake

The first check run on `8ad23e3` failed one UI test,
`TestWizardShowsFailedCameraCheckAfterApply`, with missing page text and a temporary-directory
cleanup error. The failed job was rerun unchanged and passed; the run then completed green. The test
and the code under test were not changed by this work. One occurrence is recorded here as a flake
with no code action. If it recurs, the test's temporary-directory lifecycle and its apply server are
the first place to look.

## Naming cross-check

`tests/checks/release_asset_names.py` reads five sources and fails closed:

| Source | What it contributes |
| --- | --- |
| `.github/workflows/publish.yml` | The `gh release create` asset arguments, both expected-set literals, and the release notes list. |
| `docs/adr/0031-update-channel-on-github-releases.md` | The two `Asset`/`Role` tables. |
| `agent/internal/update/channel.go` | The literal `findAsset` names plus `manifest.Payload.Filename`. |
| `agent/cmd/bittern-release/pack.go` | The payload name constant and the manifest path the packer writes. |
| `Makefile` | The payload, manifest, kit archive, and checksum outputs. |

It asserts the release set is exactly the three update assets plus the kit and checksum file, that
the device set is exactly the three update assets and a strict subset of the release set, that the
kit and `SHA256SUMS` do not appear in the device client, and that the packer and Makefile names
match. A renamed kit asset makes the check exit 1, so a drift in any one source fails.

Local output:

```text
release assets: SHA256SUMS, bittern-app.tar.gz, bittern-beta-kit.tar.gz, manifest.json, manifest.json.sig
device update assets: bittern-app.tar.gz, manifest.json, manifest.json.sig
ADR 0031, the publish workflow, the device client, the packer and the Makefile agree; the kit and SHA256SUMS are not update assets
```

The check is wired into `make check` before `version-check`.

## The runbook

[Beta bench rollout](../runbooks/beta-bench-rollout.md) is the maintainer checklist. It covers:

- Preconditions: `main` green, the `beta-draft` secret present, both environments tag-scoped to
  `app-beta-*`, `beta` requiring the maintainer as reviewer, no tag at the target generation, and a
  known Debian 13 bench box with management DHCP, DNS, and GitHub access.
- Publish generation 1, approve the `beta` environment, verify the published release.
- Download the kit, verify the published checksum before extracting or running, install, reboot, and
  open the UI from the console QR.
- Install generation 1 from the Updates card.
- Make a visible change, publish generation 2 at the checked commit, install it, and verify the
  change is present.
- Roll back to generation 1 and require the committed generation to stay at 2, so the next update
  must be newer than 2.
- A table of the card, device, and channel state at each point, plus the check-offline, rejected
  bundle, and failed health gate states.
- What to collect on failure: the publish run ID and URL, the release URL,
  `bittern update status`, `/var/lib/bittern/update/journal.json`, the history records, the
  `bittern-update.service` and `bitternd.service` journals, the check record, and the status
  document.
- The honest gaps: no camera and no Frigate database restore, no real-storage power cut, the CI test
  key rather than the release key, bundle-only manual rollback, unit changes not delivered by the
  bundle, the pending `appliance.yaml` migration not persisted, and no OS slot, image, HIL, or stable
  channel.

The runbook is not evidence until a maintainer runs it. Its first execution is the next rollout.

## Verification

- `make check` passes in the worktree, including the new release asset name check.
- `go vet ./...` and `gofmt` pass for the module; the vendor tree is excluded from the format check.
- `gh workflow view publish.yml --ref main --yaml` returns the updated workflow, so GitHub accepted
  the dispatch trigger and the new jobs.
- The check workflow run [45064216205](https://github.com/jagoda/bittern/actions/runs/45064216205) is
  green on `8ad23e3` after the failed job was rerun.
- The dry run [45064681803](https://github.com/jagoda/bittern/actions/runs/45064681803) is green and
  printed the digests above.
- No tag, draft, or release exists after the dry run.

## What remains unproven

- The tag-triggered publish path: admission of a real `app-beta-*` tag by the `beta-draft` policy,
  the draft creation and upload, the `beta` environment approval by the maintainer, the draft asset
  check, and the visible draft-to-published flip. The dry run skipped every one of those steps and
  the dispatch path never declares the `beta` environment.
- The bench rollout itself. No published release has been installed on a device, so the GitHub
  Releases channel client has not run against a real release from an appliance. The QEMU update
  scenario used a local fixture, not a published release.
- The production release key ceremony and the release trust anchor. The dry run used the CI test key
  and the bench anchor.
- The camera and Frigate database path, the real-storage power-cut matrix, unit changes through an
  update, and the pending `appliance.yaml` migration, as listed in the runbook.

# Beta bench rollout

- Date: 2026-11-22
- Status: maintainer checklist for the first two published beta generations. The release workflow
  dispatch is the normal path and the tag push is the fallback. The release workflow defaults to a
  dry-run call that exercises the called publish workflow and the signing step without creating a
  tag, draft, or release. Both `beta-draft` and `beta` admit the `main` branch and the
  `app-beta-*` tag, so the dispatch preflight passes. Neither publish path has been run against a
  published release. The `beta` approval remains unproven until the first release is approved
- Bench: Debian 13 (trixie) amd64, minimal, one management NIC on the house LAN, camera NIC
  unplugged, local console attached
- Payload: the published `bittern-beta-kit.tar.gz` and the `app-beta-000001` and `app-beta-000002`
  releases
- Related: [ADR 0031](../adr/0031-update-channel-on-github-releases.md),
  [ADR 0032](../adr/0032-continuous-integration-and-build-pipeline.md),
  [ADR 0033](../adr/0033-dispatch-driven-release-trigger.md),
  [ADR 0029](../adr/0029-release-signing-and-key-custody.md),
  [release workflow](../../.github/workflows/release.yml),
  [publish workflow](../../.github/workflows/publish.yml),
  [release preflight](../../packaging/release_preflight.py),
  [beta payload and publish note](../notes/2026-11-22-beta-payload-and-publish.md),
  [dry run and bench rollout note](../notes/2026-11-22-beta-dry-run-and-bench-rollout.md),
  [dispatch release trigger note](../notes/2026-11-22-dispatch-release-trigger.md),
  [release call path note](../notes/2026-11-22-release-call-path.md),
  [update channel and manual rollback note](../notes/2026-11-22-update-channel-check-and-manual-rollback.md),
  [beta kit README](../../packaging/README-beta-kit.md),
  [BIT-009](../plan/implementation-plan.md#bit-009-app-bundle-update-transaction-with-rollback),
  [BIT-042](../plan/implementation-plan.md#bit-042-release-channel-and-ci-pipeline)

This runbook answers one question: how does a maintainer take two published beta generations and
prove the dispatch-to-tag-to-draft-to-publish path, the kit install, the update card, the second
update, and the manual rollback on a bench box? It is the first field-like rollout. It is not the
hardware release gate and it does not exercise a camera.

The sequence uses two releases because a manual rollback needs a previous bundle. Generation 1 is the
first update from the kit baseline, which is generation 0. Generation 2 proves that a changed bundle
actually replaces generation 1. The rollback then proves that the running bundle moves back while
the committed generation stays forward.

## Preconditions

Run the release preflight before dispatching a release. Do not work around a failed precondition.

- [ ] The release preflight passes.

```sh
make release-preflight
```

Expected: every check passes, and the last lines print the computed tag, the commit, the generation,
and the next action. The preflight verifies the `check.yml` run on the exact `main` head, the
`BITTERN_CI_SIGNING_KEY` name in `beta-draft`, the `app-beta-*` tag policy on both environments, the
`beta` reviewer and admin bypass, the next generation, and the tag absence. It validates the tag with
a Python rule checked against the shared corpus at `tests/fixtures/release/tag-cases.json`, the same
corpus the Go parser tests use. The preflight needs only `python3` and `gh`; it does not build or
invoke the Go release tool. It creates no tag, draft, or release.

The dispatch runs at `main`, so both environments must admit `main` in addition to `app-beta-*`. The
preflight reports a missing branch policy as `environment-dispatch-ref`; fix the policy rather than
working around it. If any check fails, fix the named precondition and run the preflight again.

- [ ] For the tag-push fallback only, the tag-path preflight passes.

```sh
make release-preflight RELEASE_PREFLIGHT_ARGS=--tag-path
```

Expected: the same checks without the `main` branch requirement. The fallback tags the commit the
preflight prints; it does not compute a generation in CI.

- [ ] The bench box is a known Debian 13 amd64 machine with the management NIC on the house LAN.

The box has a minimal Debian 13 install, UEFI with Secure Boot off, UTC, a local administrator with
`sudo`, a local console, and no Bittern unit, package, configuration, or container. The camera NIC is
unplugged until first boot has proposed the roles. The management NIC has carrier, a DHCP lease, and
DNS. From the box, both of these resolve:

```sh
getent hosts deb.debian.org
getent hosts github.com
```

The installer needs Debian package access, and the update check and download need GitHub. If either
lookup fails, fix the management network before starting; the installer stops before `apt` rather
than guessing.

- [ ] The maintainer can open the run URL and approve the `beta` deployment. This is the designed
      human gate from ADR 0032 point 7, not a manual workaround.

Record the run as you go:

| Field | Value |
| --- | --- |
| Date and maintainer | |
| Bench box and management address | |
| `main` commit for generation 1 | |
| Generation 1 release run ID and URL | |
| Generation 1 release URL | |
| Visible change for generation 2 | |
| Generation 2 release run ID and URL | |
| Generation 2 release URL | |
| Rollback result and `bittern update status` path | |

## Dispatch generation 1

The release workflow computes the generation, builds and signs, creates the tag, stages the draft,
and waits for the `beta` approval. The same release job first runs the call path as a dry run with
`dry_run` true, which is the default. The dry run runs the preflight, the called `validate tag`,
`checks and unsigned test build`, and `sign and stage draft` jobs, and skips `publish the verified
draft`. It creates no tag, draft, or release. Only then dispatch with `dry_run` false to publish. Do
not create the tag by hand on this path.

```sh
cd /path/to/bittern
git switch main
git pull --ff-only
git status --short        # must be empty
make release-preflight    # prints app-beta-000001 and the next action
gh workflow run release.yml --ref main
sleep 5
run_id="$(gh run list --workflow release.yml --event workflow_dispatch --limit 1 \
  --json databaseId --jq '.[0].databaseId')"
test -n "$run_id"
gh run view "$run_id" --json jobs,url
```

Expected: the `release preflight`, `validate tag`, `checks and unsigned test build`, and
`sign and stage draft` jobs pass. The `sign and stage draft` job runs `Sign the manifest with the CI
test key` and `Verify the signature with the CI test anchor`. The `publish the verified draft` job is
skipped. No tag, draft, or release exists after the run.

```sh
git ls-remote --tags origin
gh release list
```

Expected: both commands print nothing. If either lists the computed tag or release, stop: the dry
run did not stay dry, and record the finding before any further dispatch.

Then dispatch the publish path with the tag the preflight printed:

```sh
gh workflow run release.yml --ref main -f dry_run=false -f confirm=app-beta-000001
sleep 5
run_id="$(gh run list --workflow release.yml --event workflow_dispatch --limit 1 \
  --json databaseId --jq '.[0].databaseId')"
test -n "$run_id"
gh run view "$run_id" --json url --jq .url
```

Watch the run:

```sh
gh run watch "$run_id"
```

Expected: the `release preflight`, `validate tag`, `checks and unsigned test build`, and
`sign and stage draft` jobs pass. The tag `app-beta-000001` is created inside the draft job after the
signature is verified, then the five assets are uploaded to the draft. The `publish the verified
draft` job waits at the `beta` environment. The draft exists and is invisible to devices.

Open the run URL, select the waiting `publish the verified draft` job, choose Review deployments,
select `beta`, and approve. The runbook cannot approve for you; the environment is the gate. A
rejected or cancelled approval leaves the draft in place and publishes nothing.

After approval:

```sh
gh run watch "$run_id" --exit-status
git ls-remote --tags origin app-beta-000001
gh release view app-beta-000001 --json isDraft,assets \
  --jq '{isDraft, assets: [.assets[].name]}'
```

Expected: the run is green, the tag exists at the `main` commit the preflight printed, `isDraft` is
false, and the asset names are exactly `manifest.json`, `manifest.json.sig`, `bittern-app.tar.gz`,
`bittern-beta-kit.tar.gz`, and `SHA256SUMS`.

The release URL is `https://github.com/jagoda/bittern/releases/tag/app-beta-000001`.

### Fallback: push the tag

Use the tag push only when the dispatch path cannot run, for example a release workflow outage or a
bootstrap. It does not compute the generation, so the tag must be the next one the preflight prints.

```sh
make release-preflight RELEASE_PREFLIGHT_ARGS=--tag-path
release_commit="$(git rev-parse HEAD)"
git tag app-beta-000001 "$release_commit"
git push origin app-beta-000001
sleep 5
run_id="$(gh run list --workflow publish.yml --event push --limit 1 \
  --json databaseId --jq '.[0].databaseId')"
test -n "$run_id"
gh run watch "$run_id"
```

The tag push runs `publish.yml` at the tagged commit. The `validate tag`, `make check`, `unsigned
test build`, and `sign and stage draft` jobs run, and the draft is created against the existing tag.
The `publish the verified draft` job waits at `beta`. Approve it and verify the release as above.
The fallback can burn a generation if the build or signature fails after the tag is pushed; that is
why it is not the normal path.

## Prepare the bench box from the kit

Download the kit and its published checksum from the same release. Do not pipe a download into an
interpreter or a root shell.

```sh
mkdir -p ~/bittern-bench
cd ~/bittern-bench
curl -fLO https://github.com/jagoda/bittern/releases/download/app-beta-000001/bittern-beta-kit.tar.gz
curl -fLO https://github.com/jagoda/bittern/releases/download/app-beta-000001/SHA256SUMS
```

Verify the tarball before extracting or running anything:

```sh
sha256sum -c SHA256SUMS
```

Expected: `bittern-beta-kit.tar.gz: OK`. If the download crossed an untrusted network, compare the
digest with the release page. The kit is unsigned, so the published checksum and release
immutability are its integrity check. The kit is not an update payload and must never be installed as
one.

Then extract, inspect the version stamp, and run the installer:

```sh
tar -xzf bittern-beta-kit.tar.gz
cd bittern-beta-kit
cat VERSION
sudo ./install.sh --dry-run
sudo ./install.sh
sudo reboot
```

Expected: `VERSION` has `baseline: true`, `channel: beta`, `generation: 0`, a
`0.0.0-factory-<short>` version, the full source commit, and the payload digest. The dry run changes
nothing. The installer bootstraps the management link, installs the Debian packages, writes the
units and `/usr/lib/bittern/platform.json`, installs the CI test anchor in
`/etc/bittern/trust/release-keys`, enables every unit it installs, and asks for the reboot. Keep the
camera NIC unplugged.

After the reboot, first boot writes the rendered configuration and prints the setup block on the
local console. That block contains the QR, the grouped token, and the short code. Do not put any of
them in the run record.

## Open the UI and install generation 1

Scan the console QR for `https://<lan0-address>/ui/setup#token=<token>`, or open
`https://<lan0-address>/ui/` from the house LAN and enter the token by hand. Accept the self-signed
certificate, set the administrator password once, and open `https://<lan0-address>/ui/status`.

Expected: the status page is ready. The Updates card reads "Factory version
`0.0.0-factory-<short>`" and "Bittern has not checked for an update since it started." The device is
at committed generation 0 with no active bundle.

On the Updates card, select Check for updates. Expected: "Update available", version
`0.0.0-beta.1-<short>`, and update number 1. Select Install update. The card shows "Installing update
`0.0.0-beta.1-<short>`" with the current step and a two-second refresh.

Wait for the card to read "Up to date" with the installed version. Then collect the evidence:

```sh
sudo bittern update status
sudo cat /var/lib/bittern/app/current
curl -fsS http://127.0.0.1:8080/api/version
```

Expected: `committed: generation 1 (0.0.0-beta.1-<short>), channel beta` and
`running: generation 1 (0.0.0-beta.1-<short>)`; the pointer file contains `1`; the version endpoint
reports `baseline: false` and the same version. The update journal
`/var/lib/bittern/update/journal.json` is absent and the newest history record is `committed`.

## Make a visible change for generation 2

The change must be visible in the browser and belong on `main`. The version string alone is not
enough, because the generation is in the tag and the bundle version, not in the tree. A real
user-visible change proves the new bundle is running. For a pure bench run, the smallest template
change is the footer sentence in `ui/templates/base.html`; record the exact diff.

```sh
cd /path/to/bittern
git switch main
git pull --ff-only
$EDITOR ui/templates/base.html
make check
git add ui/templates/base.html
git commit -m "docs: describe the bench generation 2 change"
git push origin main
```

Run the preflight on the new `main` head. It refuses while the check run for that exact commit is
still running, and it computes the next generation from the published releases. Run the same two
dispatches as generation 1: first the default dry run, then the publish dispatch with `dry_run`
false and the printed tag in `confirm`.

```sh
make release-preflight    # prints app-beta-000002 and the next action
gh workflow run release.yml --ref main
sleep 5
run_id="$(gh run list --workflow release.yml --event workflow_dispatch --limit 1 \
  --json databaseId --jq '.[0].databaseId')"
test -n "$run_id"
gh run view "$run_id" --json jobs,url
git ls-remote --tags origin app-beta-000002
gh release view app-beta-000002
```

Expected: the dry run is green through `sign and stage draft`, skips `publish the verified draft`,
and creates no tag or release. The last two commands must both fail with not found. Then publish:

```sh
gh workflow run release.yml --ref main -f dry_run=false -f confirm=app-beta-000002
sleep 5
run_id="$(gh run list --workflow release.yml --event workflow_dispatch --limit 1 \
  --json databaseId --jq '.[0].databaseId')"
test -n "$run_id"
gh run watch "$run_id"
```

Expected: the preflight prints `app-beta-000002`, and the run creates the tag after the signature and
stages the draft. The release workflow creates the tag; do not create it by hand. A tag-push fallback
uses `make release-preflight RELEASE_PREFLIGHT_ARGS=--tag-path` and the generation 1 fallback commands.

Approve the `beta` deployment as for generation 1, then verify the release:

```sh
gh run watch "$run_id" --exit-status
gh release view app-beta-000002 --json isDraft,assets \
  --jq '{isDraft, assets: [.assets[].name]}'
```

Expected: `isDraft` is false and the five asset names are unchanged. The release URL is
`https://github.com/jagoda/bittern/releases/tag/app-beta-000002`.

On the bench box, the visible change is still absent. The device is running generation 1, which
predates the change.

## Install generation 2 and roll back

On the Updates card, select Check for updates. Expected: "Update available", version
`0.0.0-beta.2-<short>`, and update number 2. Select Install update and wait for "Up to date".

Verify that the visible change is now present, then collect:

```sh
sudo bittern update status
sudo cat /var/lib/bittern/app/current
```

Expected: `committed: generation 2 (0.0.0-beta.2-<short>)` and
`running: generation 2 (0.0.0-beta.2-<short>)`; the pointer file contains `2`.

Roll back to generation 1 from the card. Select the rollback link, read the confirmation page, and
confirm. The page names the target and states that configuration and recordings are not reverted.
This is a manual rollback, not a failed-update rollback.

Expected: the card reads "Rolled back to version `0.0.0-beta.1-<short>`. The last installed version is
`0.0.0-beta.2-<short>`. The next update must be newer than version `0.0.0-beta.2-<short>`." The
visible change is gone. The rollback link is absent after the rollback because the running
generation is already the retained previous bundle; the card offers a rollback only while the
running and committed generations match.

Collect the evidence:

```sh
sudo bittern update status
sudo cat /var/lib/bittern/app/current
curl -fsS http://127.0.0.1:8080/api/version
```

Expected: `committed: generation 2 (0.0.0-beta.2-<short>)` and
`running: generation 1 (0.0.0-beta.1-<short>)`; the pointer file contains `1`; the version endpoint
reports the generation 1 version with `baseline: false`; the newest history record is
`rolled-back` with `restored_generation` 1.

The committed version must stay at 2. Do not edit `/var/lib/bittern/update/state.json`, the pointer,
or the history to make the card look clean. The next update must have a generation greater than 2,
so a republished generation 1 or 2 is not a valid next update and a new tag must be generation 3 or
later.

## What to expect

| Point | Updates card | Device | Channel |
| --- | --- | --- | --- |
| Before generation 1 is published | No card, the bench box is not prepared | No Bittern device yet | No published release |
| Generation 1 draft staged, not approved | No card, the bench box is not prepared | No Bittern device yet | Draft `app-beta-000001`, invisible to the check |
| Generation 1 published, before the check | Factory version, "Bittern has not checked for an update since it started" | Committed 0, running baseline | Published generation 1 is the highest |
| Check for generation 1 | "Update available", version 1, update number 1 | Committed 0, running baseline | Generation 1 |
| Installing generation 1 | "Installing update 1", step list, two-second refresh | Transaction from 0 to 1 | Generation 1 |
| Generation 1 committed | "Up to date", installed version 1 | Committed 1, running 1 | Generation 1 |
| Generation 2 draft staged | "Up to date", no candidate | Committed 1, running 1 | Draft generation 2, highest published is 1 |
| Generation 2 published, before the check | "Up to date", with a check time from before the publish | Committed 1, running 1 | Published generation 2 is the highest |
| Check for generation 2 | "Update available", version 2, update number 2 | Committed 1, running 1 | Generation 2 |
| Installing generation 2 | "Installing update 2", step list | Transaction from 1 to 2 | Generation 2 |
| Generation 2 committed | "Up to date", installed version 2 | Committed 2, running 2 | Generation 2 |
| Rolling back to generation 1 | "Rolling back to version 1", rollback steps | Pointer moves from 2 to 1, committed stays 2 | Generation 2 |
| Rolled back | "Rolled back to version 1", last installed 2, next update newer than 2 | Committed 2, running 1 | Generation 2, no newer release |

The card never reports "Up to date" for a check that could not reach GitHub. A failed check leaves
the device and the channel unchanged.

### Failure states

| Failure | Updates card | Device | Channel | What to do |
| --- | --- | --- | --- | --- |
| A check cannot reach GitHub | "Could not reach the update server", "Bittern cannot say whether an update is available" | Unchanged: committed and running generations stay as they were | Unchanged | Check the management link, DHCP lease, default route, and DNS, then check again. Do not start an install from a failed check |
| A bundle is rejected | "The update did not finish", "Stopped while" names `verify-manifest` or `verify-payload`, and the reason names the failing check | Unchanged: no live write, the previous generation keeps running, the journal is removed, and a `rejected` history record remains | Unchanged | Collect the journal, history, check record, and `bittern update status`. Compare the release digest and key id. Do not force the install |
| A health gate fails | "The update did not finish", "Stopped while" names `health`, and the alert says the previous version is running | Automatic rollback: the pointer returns to the previous generation, the failed candidate is retained, and a `rolled-back` history record names the failed step and the restored generation | Unchanged | Collect the unit journals, the journal, the history, and the status. Do not re-tag the same generation; a new tag must be a higher generation than every published tag |

A rate-limited check is a retry-later state, not a failure of the device or the channel. It never
reads as up to date.

## If something fails

Stop at the failed check and collect the evidence before changing anything. Do not retry the update,
re-tag a generation, or edit the device state to make the card look clean.

Publish-side evidence:

```sh
gh run list --workflow release.yml --limit 5
gh run list --workflow publish.yml --limit 5
gh run view <run-id> --log > /tmp/publish-<run-id>.log
gh run view <run-id> --json url,conclusion,event,headSha
gh release view <tag> --json isDraft,assets
```

The run ID and the release URL are the two identifiers the maintainer needs. The release URL is
`https://github.com/jagoda/bittern/releases/tag/<tag>`; the run URL is
`https://github.com/jagoda/bittern/actions/runs/<run-id>`.

Device-side evidence:

```sh
sudo bittern update status > /tmp/bittern-update-status.txt
sudo cat /var/lib/bittern/update/check.json > /tmp/bittern-update-check.json 2>/dev/null || true
sudo cat /var/lib/bittern/update/journal.json > /tmp/bittern-update-journal.json 2>/dev/null || true
sudo sh -c 'for f in /var/lib/bittern/update/history/*.json; do printf "== %s ==\n" "$f"; cat "$f"; done' \
  > /tmp/bittern-update-history.log
sudo journalctl -u bittern-update.service -b --no-pager > /tmp/bittern-update-unit.log
sudo journalctl -u bitternd.service -b --no-pager > /tmp/bittern-daemon.log
sudo curl -fsS http://127.0.0.1:8080/api/status > /tmp/bittern-status.json
```

The active journal path is `/var/lib/bittern/update/journal.json`; a completed rollback or rejection
removes it and leaves the record under `/var/lib/bittern/update/history/`. The check record is
`/var/lib/bittern/update/check.json`. The fixed helper unit is `bittern-update.service`; the daemon
is `bitternd.service`.

Never include `/etc/bittern/secrets.yaml`, a camera URL with credentials, the administrator password,
the setup token, the short code, or the setup URL fragment in a report or a support bundle. The status
document is already redacted; read the daemon log before sharing it.

## What this runbook does not cover

- A camera and the Frigate database backup or restore. The bench box has no camera, so `backup-data`
  records `not-configured` and the Frigate API health probe does not exercise a configured stack.
- A power cut on real storage. The per-step power-cut and resume matrix is covered by the slice one
  unit tests only.
- The release key. This rollout uses the CI test key and the CI test anchor. The production release
  key ceremony is offline and airgapped under ADR 0029 and is not exercised here.
- Configuration and data on manual rollback. Manual rollback reverts the running bundle only. It does
  not restore `appliance.yaml`, the Frigate config, the Frigate database, or recordings.
- Unit and packaged-file changes through an update. The bundle is a files-only payload under
  `/var/lib/bittern/app/bundles/<generation>/files`. Units, scripts, and templates come from the kit
  or the image, not from the update.
- The pending `appliance.yaml` migration. The update engine writes
  `/var/lib/bittern/update/<operation-id>/appliance.yaml` and removes it at the end of the
  transaction; nothing applies it to `/etc/bittern/appliance.yaml`. Do not publish a bundle that
  needs a schema migration until that path is wired and tested.
- The OS A/B slot, the bootloader, the image build, HIL, and the stable channel. Beta is the only
  live channel.
- The unsigned kit trust class. The kit runs as root and its only integrity check is the published
  checksum plus release immutability.

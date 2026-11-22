# Session checkpoint

- Date: 2026-11-22
- Status: checkpoint note. The update path, the publish workflow, the beta kit, the QEMU update
  scenario, the dry run, and the bench runbook exist. No tag, release, or draft exists. The beta
  bench rollout has not been run and is the next action.
- Type: session checkpoint.
- Related: [ADR 0031](../adr/0031-update-channel-on-github-releases.md),
  [ADR 0032](../adr/0032-continuous-integration-and-build-pipeline.md),
  [ADR 0029](../adr/0029-release-signing-and-key-custody.md),
  [beta bench rollout](../runbooks/beta-bench-rollout.md),
  [dry run and bench rollout note](2026-11-22-beta-dry-run-and-bench-rollout.md),
  [channel, CI, and publication note](2026-11-22-channel-ci-and-publication.md),
  [update transaction specification](2026-11-22-update-transaction-spec.md),
  [QEMU update scenario](2026-11-22-qemu-update-scenario.md),
  [beta payload and publish note](2026-11-22-beta-payload-and-publish.md),
  [BIT-009](../plan/implementation-plan.md#bit-009-app-bundle-update-transaction-with-rollback),
  [BIT-042](../plan/implementation-plan.md#bit-042-release-channel-and-ci-pipeline),
  [OQ-36](../open-questions.md)

## What this session changed

- Decided the GitHub Releases channel and CI in ADR 0031 and ADR 0032, answered OQ-34, and added
  OQ-35. CI does not hold the release key.
- Implemented the BIT-009 update transaction through slice two: the engine and its 82 named cases,
  the fixed helper unit, the request record, the agent handoff, the platform, Docker, systemd, and
  sqlite3 adapters, and the shared operation lock. Two hardening passes followed the audit.
- Added the beta channel check, manual rollback, and the status update card with the check, install,
  progress, result, and rollback states.
- Added the files-only bundle packer, the beta preparation kit, and the tag-triggered publish
  workflow with a draft-before-approval split and a `workflow_dispatch` dry run.
- Added the fixed activation launcher, so an installed bundle is the program the unit runs.
- Ran the QEMU update scenario under KVM: first bundle install, update, health-gate rollback, and
  three pre-live rejections.
- Ran the publish dry run green; it created no tag, draft, or release.
- Wrote the two-release beta bench rollout runbook with its expected states and failure states.

## What is proven

- The BIT-009 engine passes its 82 named cases, including all 22 install and 12 rollback power-cut
  points and the 24 rejection cases.
- The QEMU update scenario passes under KVM: the kit installs, generation 1 installs, generation 2
  updates, a failed health gate rolls back, and an invalid signature, a digest mismatch, and a
  downgrade are rejected before a live write.
- The dry run ran green at `8ad23e3` (run
  [45064681803](https://github.com/jagoda/bittern/actions/runs/45064681803)), decoded the
  `beta-draft` key, signed the manifest, verified it against `packaging/trust/beta-ci.pub`, printed
  the five asset digests, and created no tag, draft, or release.
- The check workflow is green on `main`, and the release asset name check passes in `make check`.
- The beta kit produces a device that accepts a beta bundle; the two QEMU workarounds are gone.

## What is not proven

The runbook's honest gaps, verbatim:

- A camera and the Frigate database backup or restore. The bench box has no camera, so `backup-data`
  records `not-configured` and the Frigate API health probe does not exercise a configured stack.
- A power cut on real storage. The per-step power-cut and resume matrix is covered by the slice one
  unit tests only.
- The release key. This rollout uses the CI test key and the CI test anchor. The production release
  key ceremony is offline and airgapped under ADR 0029 and is not exercised here.
- Configuration and data on manual rollback. Manual rollback reverts the running bundle only. It
  does not restore `appliance.yaml`, the Frigate config, the Frigate database, or recordings.
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

The tag-triggered publish with the real `beta` approval and the bench run itself are also unproven.
The runbook is not evidence until a maintainer runs it.

## Next action

Follow [the beta bench rollout](../runbooks/beta-bench-rollout.md) in order. Push
`app-beta-000001` from a green `main`, watch the draft job, approve the `beta` deployment, and
prepare the bench box from the published kit. Install generation 1, publish generation 2 with a
visible change, install it, and roll back to generation 1. Record what the Updates card shows at
each step, with the device and channel state, in the runbook's record table.

## Facts to keep

- The CI test key lives in the `beta-draft` environment as `BITTERN_CI_SIGNING_KEY`. GitHub secrets
  cannot be read back; if it is lost, rotate it and update `packaging/trust/beta-ci.pub` and the
  bench anchor together.
- The environment tag policy is the `app-beta-*` glob and the ADR 0031 tag scheme is the exact
  `app-beta-<6 hex>` rule. They live in different places and must change together; the workflow's
  Go parser is the exact check.
- A branch push does not run publish. Only an `app-beta-*` tag push runs
  `.github/workflows/publish.yml`; `workflow_dispatch` is the dry run and never creates a release.
- The local Go toolchain is at `/usr/local/go/bin` and is gone if the container is recreated. QEMU
  also has to be reinstalled; the commands are in the QEMU update scenario note.
- The pre-public history is on `archive/pre-public`. `main` starts at the import commit `060bc76`;
  do not treat that as the whole history.

## Conventions reply

- The conventions reply given to the platform-core project this session was delivered in that
  project's own session and is not copied here; it changes nothing in this repository.

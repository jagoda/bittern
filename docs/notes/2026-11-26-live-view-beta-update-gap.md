# Live view beta: the update does not apply the bundle's configuration

- Date: 2026-11-26
- Status: open finding. Generation 2 carries the live view and the Frigate 0.18.3 pin. The update
  transaction installs the bundle, but the generated Frigate configuration and compose file are
  written by an apply. The [live view beta test](../runbooks/live-view-beta-test.md) uses one camera
  save per direction as a temporary bridge.
- Related: [live view beta test](../runbooks/live-view-beta-test.md),
  [ADR 0034](../adr/0034-live-view-owned-by-bittern.md),
  [BIT-009](../plan/implementation-plan.md#bit-009-app-bundle-update-transaction-with-rollback),
  [BIT-023](../plan/implementation-plan.md#bit-023-live-view),
  [update transaction specification](2026-11-22-update-transaction-spec.md),
  [beta bench rollout](../runbooks/beta-bench-rollout.md),
  [0.18.3 pin](2026-11-26-frigate-0.18.3-pin.md).

## Finding

The update transaction in `agent/internal/update/engine.go` installs a files-only bundle, moves the
activation pointer, restarts `bitternd` and `bittern-frigate.service`, and runs its health gate. It
does not render or write `/var/lib/bittern/frigate/config.yml`,
`/var/lib/bittern/compose/compose.yaml` or the Frigate unit. Those files are written only by an apply
under `agent/internal/apply`.

The image step uses `render.FrigateImageReference()`, which is compiled into the process that runs
it. The fixed helper unit runs `/usr/bin/bittern update helper`, which is the kit baseline, not the
candidate bundle. On a device prepared from the `app-beta-000001` kit, that pin is 0.18.2. The
candidate bundle's 0.18.3 pin and its `go2rtc.streams` block therefore do not reach the running stack
at activation.

## Consequence

After installing generation 2, the served page is the generation 2 page, but the Frigate config and
compose file are generation 1's. The named `main` and `sub` go2rtc streams are absent, so the live
tiles fail to start. The recording service also stays on 0.18.2 until an apply writes the new
compose file.

The same holds in reverse. A manual rollback to generation 1 restores the bundle pointer and the
Frigate config snapshot, but not the compose file, so the running engine stays on the newer image
until an apply runs from the rolled-back bundle.

## Bridge used by the runbook

The camera edit form calls `AgentApplier.Apply` in the running daemon. Saving a camera unchanged
renders and applies the whole configuration with the bundle's renderer and pin. The runbook uses
that action once after the update and, because the rollback check expects the image to return to
0.18.2, once after the rollback.

This is a manual step. It is not a product procedure: an update should apply the new configuration,
or refuse a bundle whose rendered output has not been applied. It is recorded here so the runbook
can test the live view now and so the gap is not mistaken for a live view defect.

## What to build

- Apply the bundle's rendered configuration as part of activation, or add an explicit apply step
  after the pointer moves and before the Frigate start, with the image pull inside the transaction
  and the health gate after it. The pending `appliance.yaml` migration has the same gap.
- Make the rollback restore the compose file and the image reference, or run the same apply with the
  restored bundle.
- Add a test that installs a bundle with a changed renderer and pin and asserts the live
  configuration and the running image, not only the bundle generation.

Until that exists, the status page's "Recording software digest" reports the running daemon's pin,
not the container. Verify the container with
`docker inspect --format '{{.Config.Image}}' frigate`.

## No ADR conflict

[ADR 0034](../adr/0034-live-view-owned-by-bittern.md) requires the live view and the renderer block.
This note does not change that decision; it records that the update path does not deliver the
rendered block. BIT-009 owns the transaction. Destination: a BIT-009 follow-up, to be triaged by the
maintainer.

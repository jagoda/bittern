# QEMU adoption scenario failed on stale UI label assertions

- Date: 2026-11-18
- Status: fixed in the QEMU harness; the adoption and firstboot scenarios pass and `make check` is
  green.
- Type: harness defect, no product defect.
- Related: [camera adoption POC note](2026-11-15-camera-adoption.md),
  [ADR 0024](../adr/0024-onvif-discovery-levels.md),
  [camera matrix](camera-matrix.md), [QEMU harness README](../../tests/qemu/README.md).

## Symptom

`python3 tests/qemu/run.py adoption` failed in the `wizard` phase, before the apply, with:

```text
qemu-harness: error: camera discovery page does not show 'Model Virtual Camera' for the DHCP camera
```

Two earlier attempts in another worktree failed at the same assertion. The failure is in the
WS-Discovery path but it is not a discovery failure: the page lists the camera, its MAC, the
`ws-discovery` layer, and the `Online` state. Only the literal string with the `Model` prefix is
missing.

## Root cause

The fixture and the product are correct. The harness assertion kept a UI label that was removed in
commit `cfe24d2` ("style: simplify the camera form and prefill it from discovery", 2026-11-17).

| Piece | Location | What it does |
| --- | --- | --- |
| Fixture | `tests/qemu/run.py:4173-4181`, `CAMERA_DISCOVERY_RESPONDER` | Replies with the ONVIF name scope `onvif://www.onvif.org/name/Virtual%20Camera` and the hardware scope `onvif://www.onvif.org/hardware/QEMU`. |
| Discovery | `agent/internal/discovery/wsdiscovery.go:53-62`, called from `agent/internal/discovery/discovery.go:252-254` | Reads the `name`, `model`, or `hardware` scope into the candidate model. The fixture therefore reports `Virtual Camera` as the model. |
| Old UI | `ui/templates/camera.html` at `de29838` | Rendered the candidate as `Model {{.Model}}`, which produced the literal `Model Virtual Camera`. |
| New UI | `ui/templates/camera.html:45-47` after `cfe24d2` | Renders the vendor and model as plain values, so the visible text is `Virtual Camera`. |
| Stale assertion | `tests/qemu/run.py:4604` before this fix, in `AdoptionScenario.drive_wizard` | Still demanded `Model Virtual Camera`. |

The same commit changed the review page credential copy. Once the discovery assertion was fixed,
the run reached the review page and failed on the next stale harness string:

```text
qemu-harness: error: review page does not show 'Stored in /etc/bittern/secrets.yaml'
```

`cfe24d2` changed that copy to "Stored on the appliance when the configuration is applied, and never
shown again" (`ui/templates/review.html:45`) but did not update `tests/qemu/run.py`.

Both assertions date from `de29838` ("feat: adopt cameras with layered discovery and a managed
list", 2026-11-15), when the old template text matched them. `cfe24d2` is the commit that broke the
scenario. The recent Frigate enablement commits are unrelated: `b3b7174` does not touch
`tests/qemu/run.py`, `24eab9c` touches it but not these strings, and the failure occurs in the wizard
before any apply or Frigate start.

## Which side was wrong

The harness assertion was wrong, not the fixture and not discovery.

The fixture advertises a valid ONVIF name scope, and the product intentionally treats that scope as
the display model. The old assertion was coupled to a presentational label in the wizard template,
not to a discovery promise. The review assertion was coupled to copy that the UI change deliberately
rewrote. No product or fixture behaviour needed to change, and the fixture still advertises exactly
what the [camera adoption note](2026-11-15-camera-adoption.md) and
[ADR 0024](../adr/0024-onvif-discovery-levels.md) describe: the ONVIF name scope is display evidence,
and the manual address plus RTSP path remains the baseline.

## Why this will recur

Both failures came from one editorial commit, `cfe24d2`. The assertions were written against the
template prose that existed on 2026-11-15: `Model {{.Model}}` on the discovery card and the old
`Stored in /etc/bittern/secrets.yaml` credential copy. Neither string was a product contract, so the
next editorial pass will break another assertion in the same way unless the harness reads stable
elements and values.

[BIT-035](../plan/implementation-plan.md#bit-035-decouple-qemu-assertions-from-rendered-copy)
decouples the QEMU assertions from rendered copy. It is scoped to the harness and the template
attributes it needs, and the new engineering principle
[EN-2](../principles.md#en-2-tests-assert-structure-and-values-not-user-facing-copy) records the rule.

## Fix

`tests/qemu/run.py` now checks the model value the fixture actually publishes and the current review
copy:

- `Virtual Camera` instead of `Model Virtual Camera` in the WS-Discovery page check.
- `Stored on the appliance when the configuration is applied` instead of the removed file-path copy
  in the review page check.

The checks still prove the same things: the WS-Discovery layer answered with the fixture's ONVIF name
scope and the review page shows the credential and reservation summary. No fixture, discovery,
renderer, or UI file changed. The camera matrix does not need an update because the fixture is test
infrastructure, its advertised scopes are unchanged, and no new camera model or quirk was recorded.

## Verification

- Before the fix, `python3 tests/qemu/run.py adoption` with KVM failed in `wizard` after 12.58 s with
  the `Model Virtual Camera` message. The log is
  `build/qemu/logs/adoption-20261118-185656/`.
- After the first assertion repair, the same command reached the review page and failed on the stale
  secrets-path copy, confirming the second stale string. That log is
  `build/qemu/logs/adoption-20261118-190212/`.
- After both repairs, `python3 tests/qemu/run.py adoption` with KVM passed end to end. The `wizard`
  phase passed in 242.3 s including apply, the `camera-list` phase showed the adopted camera, its
  picture, recording and enabled state, the edit and explicit snapshot test, and the `camera-offline`
  phase showed the failed snapshot and degraded status. The log is
  `build/qemu/logs/adoption-20261118-190554/`.
- `make qemu-firstboot` passed with KVM. The post-apply reboot, unit, Frigate and status checks all
  passed. The log is `build/qemu/logs/firstboot-20261118-191445/`.
- `make check` passed: 20 schema cases, the Frigate bundle check, 15 Python tests, and every Go
  package.

## Ticket

None. This was a QEMU harness defect, not a product defect. No implementation-plan ticket, ADR, or
open question is needed, and no camera-matrix row changed.

# The multi-camera apply result: per-camera credentials, redaction and naming

- Date: 2026-11-21
- Status: implemented. The apply transaction's step order and generation model are unchanged.
- Scope: the one apply behind the multi-camera review: where each camera's sign-in is written, what
  the result page says about every camera, which camera an apply-time refusal belongs to, and what a
  restored transaction leaves behind.
- Related: [the step specification](2026-11-21-multi-camera-step-spec.md),
  [the wizard slice](2026-11-21-multi-camera-wizard-slice.md),
  [the camera step](../design/flows.md#the-camera-step),
  [the add-and-list pattern](../design/patterns.md#add-and-list),
  [BIT-029](../plan/implementation-plan.md#bit-029-multi-camera-inventory-and-add-from-listing),
  [BIT-036](../plan/implementation-plan.md#bit-036-first-run-and-flow-contradictions-from-the-flow-layer).

## What changed

- Each camera's sign-in is written under that camera's own key, from the one apply request that
  already carried one URL per camera: `cameras.<id>.rtsp_url` in `secrets.yaml`, and one
  `FRIGATE_CAMERA_<ID>_RTSP_URL` line in the generated camera env file. The transaction refuses a
  duplicate environment name, so two cameras cannot share one entry.
- The apply operation records the camera list it was started with, and the result page names every
  camera with its own picture-check outcome. A committed apply that left one camera without a
  picture says so in the lede and on that camera's row instead of reporting a clean success.
- The result states the transaction's real semantics. The picture check runs after the commit and
  never rolls back: a camera without a picture is committed-and-failing, and the page says the
  configuration stays applied. A rolled-back apply says nothing was changed for any camera. An apply
  that stopped without a completed restore says the appliance's state is unknown and points at
  status.
- A camera the appliance refuses at apply time is named by name and address in primary copy, and the
  review shows the refusal on that camera's own card until that camera changes or the next apply
  starts. The validator's path, rule and sentence stay in technical details.
- Every camera's sign-in is registered with the redactor before anything can fail: the complete RTSP
  URL and the username and password inside it, at the applier boundary, in the transaction's log
  values, and again where the operation record is written. A failure that quotes a stream is stored
  and rendered with `[REDACTED]` in place of every camera's credential.
- A run that does not commit restores `secrets.yaml` byte for byte, with its mode. A run that
  committed, a run that stopped with a journal to resume, and a run whose live steps are partly on
  the appliance keep the new sign-ins.
- The older success page names the cameras and says "recording service" instead of the upstream
  product name.

## What the secrets restore rule is

The wizard writes each camera's sign-in before the transaction starts, because the transaction's
first steps render and check the camera env file from the secret document. The write therefore sits
next to the transaction, not inside it, and the applier adds the one promise the note asked for: the
document is restored whenever the transaction did not commit.

| Outcome | Secrets document | Why |
| --- | --- | --- |
| Committed | Kept | The committed configuration references the new keys. |
| Stopped with a journal to resume (power loss) | Kept | The next apply resumes that generation and it must commit with its sign-ins. |
| Rolled back | Restored | The generation that references them was never committed. |
| Failed before any live step ran | Restored | Nothing was written, so nothing may keep a credential. |
| Failed with live steps and no completed restore | Kept | The new configuration may be partly live and still reading them; the result page sends the owner to the support path. |

## What remains

- `appliance.yaml` is still written before the transaction, so a rolled-back apply leaves the
  document listing the attempted cameras while the live generation and the restored secret document
  are the previous ones. The wizard's retry rewrites both from the session; a fresh session must add
  the cameras again. Moving the document write into the transaction is a transaction-shape change
  and is not made here. It belongs with [BIT-036](../plan/implementation-plan.md#bit-036-first-run-and-flow-contradictions-from-the-flow-layer).
- The camera step still has no per-camera check state (added, check failed) in its rows; the apply
  result is the first place a per-camera check is named. That row state is
  [BIT-029](../plan/implementation-plan.md#bit-029-multi-camera-inventory-and-add-from-listing)'s.
- The browser check cannot reach the committed-with-a-failing-camera result or the apply-time
  refusal: the preview apply always succeeds and its picture check always answers. Those two states
  are read from rendered pages at 360 and 1280 in this change, not from
  `build/ui-screenshots/`. A preview switch for a failing picture check is a test seam nobody has
  asked for; [BIT-036](../plan/implementation-plan.md#bit-036-first-run-and-flow-contradictions-from-the-flow-layer)
  owns the separate partial-result state, and the seam should land with it.
- The operation record's `cameras` list is not in the status contract. The status document keeps
  reporting cameras from `appliance.yaml`, which after a rollback still lists the attempted list, as
  above.

## Finding: the QEMU adoption scenario still drives the one-camera flow

`tests/qemu/run.py`, the `drive_wizard` of the adoption scenario (around lines 4980 to 5008), still
expects the camera step's add to redirect to `/ui/wizard/review`, the review to say "Stored on the
appliance when the configuration is applied", and the done page to say "Frigate returned a picture".
The multi-camera step changed all three, so the adoption scenario fails before it reaches the apply.
The firstboot scenario uses a `GET /ui/wizard/review` after its add and still matches.

Extending the scenario to two cameras with two sign-ins is not cheap on top of that repair: the
virtual camera fixture serves one mediamtx path with one credential, so it needs a second path and a
second camera record, plus the two set-of-credentials assertions the Go tests already make. What the
hardware run adds is the part Go cannot: the pinned Frigate container reading two different
`FRIGATE_CAMERA_*_RTSP_URL` values, both streams recording, and the real rollback leaving
`/etc/bittern/secrets.yaml` on the running appliance as it was. The harness repair and the second
fixture path are [BIT-004](../plan/implementation-plan.md#bit-004-qemu-two-nic-dev-harness-and-virtual-camera)'s
with [BIT-029](../plan/implementation-plan.md#bit-029-multi-camera-inventory-and-add-from-listing)
for the flow assertions.

## Evidence

- `TestTwoCamerasKeepTheirOwnCredentials` writes two cameras with different sign-ins and reads back
  two secrets entries, two distinct environment lines and a rendered configuration that references
  each camera's own variable.
- `TestApplyResultNamesEveryCamera` applies two cameras with one picture check failing and reads the
  result page and the operation record.
- `TestApplyValidationFailureNamesTheCamera` applies a camera on the house network, then checks that
  the result names it and that the review shows the refusal on its card and clears it after the
  camera is corrected.
- `TestMultiCameraCredentialsStayOutOfEveryResult` walks both cameras' usernames, passwords and
  complete URLs against the camera step, the review, the result page and the operation record.
- `TestRolledBackApplyLeavesTheSecretsDocumentAsItWas`, `TestCommittedApplyKeepsItsCameraSignIns` and
  `TestKeepCameraSecretsFollowsTheTransactionOutcome` cover the restore rule and its table.
- `TestStatusDocumentCarriesNoCameraCredentials` builds the status document from a two-camera
  configuration and the daemon's redactor and finds neither camera's credentials.
- `make check`, `make ui-test`, `make design-check`, `make unit-enablement-check` and
  `make ui-browser-check` pass with 0 failures and 0 warnings. The review and the result pages were
  read at 360 by 800 and 1280 by 800 with two cameras.

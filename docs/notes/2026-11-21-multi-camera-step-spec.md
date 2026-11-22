# The multi-camera add step: decision, session model, and risks

- Date: 2026-11-21
- Status: decision recorded and specified. The wizard-side slice landed on 2026-11-21: the step
  holds the pending list, the per-camera settings destination and the review lists every camera.
  The separate add destination and the per-camera check state remain; see
  [the slice note](2026-11-21-multi-camera-wizard-slice.md). The apply result landed on 2026-11-21
  and is recorded in [the apply-result note](2026-11-21-multi-camera-apply-result.md). The destinations, states and rules are
  owned by [BIT-029](../plan/implementation-plan.md#bit-029-multi-camera-inventory-and-add-from-listing),
  with the camera form's search and prefill owned by
  [BIT-028](../plan/implementation-plan.md#bit-028-camera-onboarding-ux).
- Scope: the first-run camera step (step 4), its three destinations, per-camera settings and
  sign-ins, what the change does to the wizard session, and what it means for apply and the secret
  store.
- Related: [the camera step](../design/flows.md#the-camera-step),
  [the add-and-list pattern](../design/patterns.md#add-and-list),
  [the copy glossary](../design/copy.md#glossary), [BIT-029](../plan/implementation-plan.md#bit-029-multi-camera-inventory-and-add-from-listing),
  [BIT-028](../plan/implementation-plan.md#bit-028-camera-onboarding-ux),
  [OQ-30](../open-questions.md#oq-30-is-bulk-camera-add-in-scope-for-v1-and-if-so-what-is-the-batch-flow),
  [ADR 0027](../adr/0027-flow-layer-above-patterns.md)

## What was decided

The maintainer decided the multi-camera add step on 2026-11-21. Step 4 is now a list of the cameras
already added at the top, discovery and manual entry to add more, one review at the end, and one
apply for all of them. The hard requirement is that every camera keeps its own settings, including
its own sign-in, port and path, because cameras on one appliance often have different usernames and
passwords.

The step is split into three destinations so that one destination still asks one question:

| Destination | Question it asks |
| --- | --- |
| `/ui/wizard/camera` | Are these the cameras for this setup, and is there another to add? |
| `/ui/wizard/camera/add` | What is this one camera, and what is its sign-in? |
| `/ui/wizard/camera/{ref}/settings` | What are this one camera's settings? |

The rules that follow from per-camera settings, stated as M1 to M6 in
[the flow document](../design/flows.md#rules-that-follow-from-per-camera-settings) and as A1 to A9
in [the pattern](../design/patterns.md#add-and-list), are:

- Advanced settings are per camera, not per step. Opening one camera's settings, including its
  advanced disclosure, renders and changes only that camera.
- A saved sign-in is never displayed. A blank field keeps the saved sign-in only while the address,
  port and path are unchanged; a changed endpoint asks for it again and says why.
- Removing a camera from the pending list is not the destructive remove. It needs no confirmation,
  it changes nothing on the appliance, and it says that the typed sign-in is dropped.
- A camera that fails validation does not block adding the next one. It is either not added, with
  the failure on the add destination, or already in the list with its check failure on its row; the
  add control and the review action stay available in both cases.
- The review lists every camera with its own values and its own sign-in state, and its single
  filled action applies all of them once.

Two smaller decisions are recorded rather than implied. A camera's saved identity is assigned when
it is added and does not change when its name changes, so a rename keeps its sign-in. And an add
that the camera answered with a refused sign-in is a validation failure, not a failed check: the
camera is not added until the sign-in is corrected.

What this decision does not settle: whether one search pass may adopt several selected cameras at
once. That stays in
[OQ-30](../open-questions.md#oq-30-is-bulk-camera-add-in-scope-for-v1-and-if-so-what-is-the-batch-flow),
with the evidence note that says which part is now closed. How the add destination lays out the
candidate list, the manual fallback and the disclosure stays
[BIT-028](../plan/implementation-plan.md#bit-028-camera-onboarding-ux)'s. The session lifetime stays
[OQ-33](../open-questions.md#oq-33-which-ui-states-must-be-stable-urls-and-which-are-transient)'s and
[BIT-036](../plan/implementation-plan.md#bit-036-first-run-and-flow-contradictions-from-the-flow-layer)'s.

## What it changes in the session model

Today the wizard session holds one camera and one complete RTSP URL, and the review and the apply
request are built from that single pair: the review renders one camera, and the apply request
carries one credential.

The target is a list. Each pending camera carries its own name, address, stream port and path,
hardware address, discovered source, check state, and its own sign-in. The review renders the whole
list, and the apply request carries one credential per camera. Nothing else in the flow changes
shape: the step count, the one apply, and the result page stay as they are.

Three consequences are worth naming.

- The list is session state, so it is transient under
  [F8](../design/flows.md#f8-a-linkable-state-has-a-stable-url-and-a-transient-state-is-not-offered-as-one).
  A reload keeps it; a different browser, a cleared cookie, a daemon restart or the one-hour expiry
  loses it. Today that loss is silent, and BIT-036 owns the message; BIT-029 has to render the
  cleared list inside it.
- The reference in the settings URL is scoped to the session, not to the appliance. It is not the
  camera's saved identity, it is never typed by the owner, and a reference the session does not
  hold renders the missing-camera state rather than another camera's values.
- The session holds one secret per camera instead of one. The store is in memory and the TTL is one
  hour, so the secrets live no longer than they do today, but there are more of them and they are
  discarded together.

## What it means for apply and secrets

One apply writes every camera. The apply request already carries the camera list and one RTSP URL
per camera; the change is that both have more than one entry. The secrets document keeps one entry
per camera under that camera's own `credentials_ref`, the renderer reads the value through that
reference, and the rendered recording configuration derives one environment variable per camera id.
Two cameras with one id would collide in that variable name, and the apply already refuses a
duplicate environment name rather than writing one camera's sign-in over another's.

The atomicity promise is unchanged and is now bigger: one transaction, one generation, one
rollback. A failed add writes nothing, because it never reaches apply. A failed apply restores the
previous generation and leaves the secret document as it was. The split that would hurt is a
credential written for one camera while the configuration generation does not commit, so the
credential write belongs inside the transaction, not beside it.

Nothing about the secret's visibility changes. The review says that a sign-in is saved for a camera,
never the username or the password, and the row never shows either. The blank-keeps-the-saved-secret
rule is the existing edit rule, applied per camera.

## Risks

1. **Credential fan-out in one session.** Several sign-ins in one in-memory session mean a larger
   secret footprint and a redaction test per camera, not one for the flow. Today the redaction
   tests cover one credential. BIT-029 must render, log and report none of them, and the support
   bundle and status paths must not learn about the list.
2. **Identity collision.** If two pending cameras share a saved identity, one sign-in can overwrite
   or shadow another's. The mitigation is the rule above, an identity per camera assigned at add and
   kept across a rename, plus a uniqueness check when the camera is added.
3. **A partial success that reads as success.** With four cameras, "Configuration applied" beside
   one dead camera is the current result shape. The result must name each camera's check, and a
   camera that failed must not be summarised away. This is BIT-029's, with the partial-result state
   still owned by BIT-036.
4. **The review becomes a wall.** Four cameras with all their values is a long page. Grouping per
   camera and keeping evidence behind a disclosure must not hide a value that will be written, and
   four widths have to be read before it is called reviewed.
5. **The session expires mid-list.** Four cameras on a slow install can cross the one-hour expiry,
   which loses the list and every typed sign-in. Nothing in this change shortens that; a warning or
   a longer lifetime is a decision for OQ-33 and BIT-036.
6. **A lost password on a validation error.** F4 keeps every value except a secret, so a failed add
   costs the sign-in for that camera. The page says so before the owner retries, and the field
   checks run before the credential check where they can.
7. **A lost password on a remove from the list.** Taking a camera out of the list is reversible in
   one sense and not in another: the camera can be added again, but the typed sign-in cannot be
   recovered. That is why the row states the loss instead of confirming the action.
8. **"Add" read as "record".** "Add camera" can be read as "make this camera record", when only the
   review applies anything. The copy rule and the "not applied yet" term in
   [the glossary](../design/copy.md#glossary) are the mitigation; the review stays the only commit.

One smaller finding, not a risk to the feature: the v0.8 changelog entry recorded a flow change as
`Class: flow`, which [ADR 0027](../adr/0027-flow-layer-above-patterns.md) does not allow, because it
records a flow change under the existing `pattern` class. This change uses `pattern`. The v0.8 entry
is left as written and is not corrected here.

## Next step

Implement the three destinations with BIT-029, add them to the browser-check manifest in the same
change ([OQ-29](../open-questions.md#oq-29-how-do-later-ui-tickets-extend-the-browser-check-route-list)),
read the four widths, and update the transition contract's current column when the handlers land, as
[ADR 0027](../adr/0027-flow-layer-above-patterns.md) amendment P3 requires. The interruption test
that makes F3 checkable is still unwritten, and it is the test that would exercise the session
expiry risk above.

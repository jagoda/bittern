# Flows

- Date: 2026-11-21; updated 2026-11-22
- Status: Accepted as design system v0.3 on 2026-11-21
  ([ADR 0027](../adr/0027-flow-layer-above-patterns.md)), with amendments P1 and P3; the acceptance
  is recorded as v0.6 in [CHANGELOG.md](CHANGELOG.md). The token, component and fact-row layer is
  implemented in `ui/static/bittern.css` and the templates; this flow layer is binding for routes,
  entries, exits, redirects and destinations, and descriptive for the wording of copy. The camera
  step's multi-camera destinations are specified in [the camera step](#the-camera-step) and
  recorded as v0.10.
- Applies to: every destination under `/ui`, every redirect a handler performs, and every flow
  change that touches more than one screen.
- Companion documents: [README.md](README.md), [patterns.md](patterns.md),
  [components.md](components.md), [professional-test.md](professional-test.md).

The design system governs screens. It did not govern the space between them: which destination
follows which, what an entry point must carry, what back and reload mean, where a failure lands,
and which states can be bookmarked. That gap produced real defects, not hypothetical ones: `Setup`
stays in the header after setup completes and offers the first-run wizard as an ordinary
destination, a failure re-renders a POST, and an expired wizard session lands on the camera step
with no message. This document owns that layer.

It is deliberately small. It does not add a component, a token, or a pattern. It names the
destinations that already exist in [routes.go](../../agent/internal/ui/routes.go), states the rules
between them, and records the transitions the handlers actually perform. Where the implemented flow
has no defined destination, it says so instead of inventing one.

## What this layer owns

A pattern owns one job and the screens it needs: its primary action, its steps, its states, and its
component composition. A flow owns the sequence around that job: the entry points, the exit, the
terminal destination, the back and reload behavior, the failure destination, and which states are
linkable.

| Layer | Decides | Does not decide |
| --- | --- | --- |
| Tokens | Colour, type, spacing, radius, motion, and target values | Anything about a route or a screen |
| Components | Anatomy, variants, states, and the accessible name of a fragment | A route, a flow, or a product noun |
| Patterns | One job: primary action, steps, states, and composition | The entry and exit around the job, and the transition between jobs |
| Flows | Destinations, entries, exits, transitions, resume, and linkability | A component's style, a pattern's state matrix, or a token value |

The one-way rule extends:

```
tokens -> components -> patterns -> flows
```

- A pattern may not decide a redirect, a destination, or a session lifetime. Those are flow rules.
- A flow may not restyle a component or change a pattern's state matrix. If a transition needs a new
  screen state, the pattern changes first.
- A flow is the only layer that knows a route, a redirect, a one-shot result, or a bookmark rule.
- A token never references a pattern or a flow.

This layer sits above [patterns.md](patterns.md), so the sentence there that makes a pattern the
only layer that knows about a flow now reads: a pattern knows the shape of one job and its steps,
not the route sequence or the transition between destinations.

## The information architecture map

The tree is the destination set, not the URL set. Redirects and POST handlers are transitions and
appear in [the transition contract](#the-transition-contract).

```
/ui
|-- login                 sign in
|-- setup                 first-run lock, before the administrator exists
|-- wizard                first run, transient
|   |-- network           step 2
|   |-- storage           step 3
|   |-- camera            step 4, the cameras for this setup
|   |   |-- add           add one camera
|   |   `-- settings      one camera's own settings
|   |-- review            step 5
|   |-- apply             running operation
|   |-- result            success, failure, or rollback
|   `-- done              step 6, legacy success page
|-- status                persistent, signed in
|   `-- updates/rollback   manual rollback confirmation
|-- cameras               persistent, signed in
|   |-- {id}/edit         object scoped, transient
|   `-- {id}/remove       object scoped, destructive
|-- live                  persistent, signed in
|   `-- {camera}          one camera, full width
`-- error                 any failure or unknown path
```

### Signed-out destinations

| Destination | Job | Entry points | Exits | In the header |
| --- | --- | --- | --- | --- |
| `/ui/login` | Prove the owner is the administrator and return them to the work they asked for. | Any signed-in route while signed out ([auth.go](../../agent/internal/ui/auth.go)); sign-out; session expiry; direct. | The validated `next` destination, else the device landing resolver (setup, live or status); the same destination on a failed attempt. | Signed out only |
| `/ui/setup` | Exchange the one-time setup token for an administrator password. | Any non-static `/ui` path while setup is required; direct. | `/ui/wizard` on success; the same destination on a failed attempt. | Setup required only |

### First-run destinations

All of these are transient. Their content depends on the server-side wizard session, so a URL
without that session is not the state it names.

| Destination | Job | Entry points | Exits | In the header |
| --- | --- | --- | --- | --- |
| `/ui/wizard` | Say what the first run will do and that nothing is written until Apply. | `Setup` in the header while first run is incomplete; the device landing while setup is incomplete; setup completion; review "Back to welcome"; error page "Open setup". | `/ui/wizard/network`; also `/ui/wizard` from cancel today. | Transient, header entry only while first run is incomplete |
| `/ui/wizard/network` | Show the two links and their carrier before the owner continues. | Welcome submit; storage "Back to network". | `/ui/wizard/storage`; welcome. | Transient |
| `/ui/wizard/storage` | Show the recording drive, its free space, and the reserve. | Network submit; camera "Back to storage". | `/ui/wizard/camera`; network. | Transient |
| `/ui/wizard/camera` | Show every camera added so far, each with its own settings, and add more before the one review. | Storage submit; an add that returns to the step; a settings save; a remove from the list; review "Change cameras". | Add camera; one camera's settings; review; storage. | Transient |
| `/ui/wizard/camera/add` | Add one camera: what the search found on cam0, a manual address, its own sign-in and stream values. | The step's "Add camera", in the empty state and in the list; the discovery POST redirect. | The step after "Add camera"; the step on back or cancel; the same destination on a validation failure. | Transient |
| `/ui/wizard/camera/{ref}/settings` | Change one camera's own name, address, stream port and path, sign-in and hardware address. | A step row's "Edit settings"; a review group's "Change settings". | The destination it was opened from, on save or back; the same destination on a validation failure; the step when the camera is no longer in the list. | Transient, object shaped |
| `/ui/wizard/review` | Show exactly what will be written for every camera, with each camera's own values, and take the one commit. | The camera step's "Review configuration"; a settings save from a review group; done when the session is not applied. | Apply; the camera step; welcome; the camera step when the list is empty. | Transient |
| `/ui/wizard/apply` | Show the apply that is running, its current step, elapsed time, and whether leaving is safe. | Apply start redirect; reload; reopened tab; the two-second fragment. | `/ui/wizard/result` on completion; status; review when no operation exists. | Transient, resumable from the persisted operation. |
| `/ui/wizard/result` | Say what happened, including the failing step, the rollback result, every camera and its picture check, and the next action. | Apply completion redirect; direct; reload. | Review; status; camera list. | Persistent until the next operation replaces it. |
| `/ui/wizard/done` | The older success page, retained for a direct visit and existing links. | Direct only; the apply flow now ends at `/ui/wizard/result`. | Camera list; status; setup today. | Transient, requires `session.Applied` |

### Signed-in destinations

| Destination | Job | Entry points | Exits | In the header |
| --- | --- | --- | --- | --- |
| `/ui/status` | Answer "is it working, and what do I do next" from the current status document. | Header; the device landing when no camera is configured; done; camera list; refresh link; the update card's 15 second fragment, or 2 seconds while an update or rollback runs. | Cameras; setup today; refresh; update-card fragment. | Persistent |
| `/ui/cameras` | Show every managed camera, its state, and the path to edit, test, or remove it. | Header; status "Manage cameras"; done "Manage cameras"; edit save; remove confirm; edit and remove cancel. | Camera edit; remove; snapshot action; status; setup today. | Persistent |
| `/ui/live` and `/ui/live/{camera}` | Show one camera live, or the latest snapshot when the stream cannot start, with a picker when more than one camera exists and one line of camera state. | Header when at least one camera is configured; the device landing when a camera is configured; live picker; a camera name from another live destination; direct. | Another camera; the main or sub profile; status; sign-in when the session ends. | Persistent |
| `/ui/status/updates/recovery` | Confirm one recovery action for a stuck update, name its effect, and state what is not touched. | Status update card's recovery action when a stuck state exists; direct. | Status on confirm or cancel; status when the state cleared. | Transient to the current status document. |
| `/ui/status/updates/rollback` | Confirm one manual rollback, name its target, and state that configuration and recordings are not reverted. | Status update card "Roll back" when a target exists; direct. | Status on confirm or cancel; status when no target exists. | Persistent while the target exists; the confirmation is transient to the current status document. |
| `/ui/cameras/{id}/edit` | Change one camera with its current values and one apply action. | Camera card "Edit". | Camera list on save or cancel; the same destination on a validation or apply failure. | Transient, object scoped |
| `/ui/cameras/{id}/remove` | Remove one camera with its exact effect stated before the commit. | Camera card "Remove". | Camera list on confirm or cancel; the same destination on an apply failure; the error page when the saved credential cannot be deleted. | Transient, object scoped |
| `error` | Say what failed and what to do next when no destination fits. | An unknown path; unreadable configuration; failed form token; session store failure; credential deletion failure. | Setup, and status; no return to the failing flow. | None |

The `Setup` header entry and the "Back to setup" links are recorded as implemented above. They
contradict the navigation model below; the defects are inventoried in
[the flow-layer defect note](../notes/2026-11-21-flow-layer-defects.md).

### Storage states on the status destination (target)

[ADR 0028](../adr/0028-missing-recording-drive-and-boot.md) makes storage a first-class status state
because a missing recording drive no longer blocks boot. The status destination must distinguish
four conditions before it can answer "what do I do next":

| State | What it means | Today |
| --- | --- | --- |
| Not attached | No eligible recording device is present. | Target |
| Attached, not mounted | A device is present, but `/var/lib/bittern/media` is not a mountpoint. | Target |
| Write test failed | The media path is a mountpoint, but the write test fails. | Target |
| Ready | The media path is a mountpoint and the write test passes. | Not distinguished; a present directory reads as ok |

In the target, the write test runs only after the mountpoint check, so a bare directory on the OS
disk is never written to and its free space is never reported as recording capacity. All four
distinctions are target; the current status section exposes a `present` boolean and a severity, not
these four conditions. The status state and its repair action are owned by
[BIT-006](../plan/implementation-plan.md#bit-006-bitternd-health-and-state-model), and the product
mount path by [BIT-012](../plan/implementation-plan.md#bit-012-storage-selection-and-preparation).

### The camera step

Step 4 is the first destination in the flow that holds more than one object. The step now holds a
list of pending cameras in the wizard session: the list at the top with each camera's name,
address, "Not applied yet" state, Edit settings and Remove from list, and the search, candidate
list and add form below. Adding another camera returns to the step and changes no existing row. The
step's filled "Review configuration" sits at the end of the step, after the add form, and continues
to the one review, which renders every camera and carries the list to the one apply. The per-camera settings destination is
`/ui/wizard/camera/{ref}/settings`, and a save returns to the step or to the review it was opened
from. This slice keeps the add form on the step; the separate add destination in the target below
remains with [BIT-028](../plan/implementation-plan.md#bit-028-camera-onboarding-ux) and
[BIT-029](../plan/implementation-plan.md#bit-029-multi-camera-inventory-and-add-from-listing). The
requirement that every camera keeps its own settings, including its own sign-in, port and path, is
recorded in BIT-029 and in
[the step's specification note](../notes/2026-11-21-multi-camera-step-spec.md).

The step has three surfaces. The step itself is the list and the add form; the settings destination
changes one camera that is already in the list; the review renders the whole list. Their names are
"Cameras", "Camera settings" and "Review configuration", and the review link that returns to the
step is "Change cameras". The step's single filled action continues to the one review, which
applies every camera in one transaction. `{ref}` is a reference the session holds for a camera in
the list; the owner never types it, and it is not the camera's saved identity.

#### The outcomes of an add

An add has three outcomes. Two of them create a row, and one changes nothing.

| Outcome | What the owner sees | The list |
| --- | --- | --- |
| Added | The step, with the new row, its facts and its own actions. | One more row; every other row unchanged. |
| Added, check failed | The step, with the row carrying the failure word, the reason and the two row actions. | One more row; the review action and the add control stay available. |
| Not added | The add form's destination, at 422, with the error summary and the field errors. | Unchanged. |

A failed check in the list covers a camera that did not answer the search or the probe, including a
manually entered camera that was never reached. A sign-in the camera refused is a validation
failure, not a failed check: the camera answered, so the sign-in is wrong, the camera is not added,
and the sign-in must be entered again. An unreachable camera never blocks an add, because the
manual path in [F5](#f5-no-flow-asks-for-a-value-the-appliance-can-discover) must keep working when
every probe fails. The picture check happens after apply and is reported per camera on the result
destination (M6).

#### States

| State | Where it renders | What the owner sees | Next action | Linkable |
| --- | --- | --- | --- | --- |
| No cameras yet | Step | The empty state: why the list is empty, that nothing is applied yet, and one action, "Add camera". | Add camera | No |
| One camera | Step | One row, the add control, and the filled "Review configuration". | Review, add, edit settings, remove from list | No |
| Several cameras | Step | One row per camera in the order they were added, the add control, and the filled "Review configuration". | Review, add, edit settings, remove from list | No |
| A camera that failed its check | Step, one row | The failure word, the reason, and the two row actions. The other rows and the review action are untouched. | Edit settings, remove from list, add another | No |
| Search empty | Step, add form | "Bittern found no camera on cam0", in words that do not claim the network is empty, with the manual fields and "Search again". | Enter the address by hand, search again | No |
| Search failed | Step, add form | An alert naming what failed: no answer on cam0, a search that ran a moment ago, or a search that could not start. The manual fields and "Search again" stay. | Enter the address by hand, search again | No |
| Add failed validation | Step, add form | The error summary, the field errors, every value kept except the sign-in, and a sentence saying the sign-in must be entered again. | Correct the field, add again | No |
| Settings failed validation | Settings | The error summary and the field errors for that camera only, with every value kept except the sign-in and a sentence saying it must be entered again. No other camera changes. | Correct the field, save again | No |
| A camera the appliance refused at apply | Review, that camera's card | The refusal names the camera, says nothing on the appliance has changed, and says what to correct. Every other camera's card is unchanged, and the apply action stays available. | Correct that camera, apply again | No |
| The camera is not in this list | Settings | It names the camera and says it is not in this setup's list, and that nothing was changed. | Open cameras | No |
| Session lost | Step, settings | A named restart state that says the pending list was cleared and that nothing was applied. | Add camera, Open status | No |

The missing-camera state is required by the object-shaped URL. The session-lost state is
[BIT-036](../plan/implementation-plan.md#bit-036-first-run-and-flow-contradictions-from-the-flow-layer)'s
message, with BIT-029 rendering the cleared list inside it. Today a new session is silent.

#### Transitions

| Action | Current (verified or inferred) | Target |
| --- | --- | --- |
| Add a camera | `POST /ui/wizard/camera` adds one camera to the list and 303s to `/ui/wizard/camera`; a validation failure re-renders the step at 422 with the list unchanged ([wizard.go](../../agent/internal/ui/wizard.go), `submitCamera`; [routes.go](../../agent/internal/ui/routes.go)). `verified`. | `POST /ui/wizard/camera/add` adds one camera to the list and 303s to `/ui/wizard/camera`; a validation failure is 422 on the add destination with the list unchanged. Owning ticket: BIT-029. |
| Search on cam0 | `POST /ui/wizard/camera/discover` 303s to the step, which renders the result ([wizard.go](../../agent/internal/ui/wizard.go), `refreshDiscovery`). `verified`. | The same POST 303s to `/ui/wizard/camera/add`, which renders the result. Owning tickets: BIT-028 for the search, BIT-029 for the destination. |
| Choose a discovered camera | A GET for the step with `selected=<address>` fills the name, address and hardware address from that candidate: with HTMX the identity section is swapped and a status line names the camera, and without HTMX the page re-renders with the same values ([wizard.go](../../agent/internal/ui/wizard.go), `showCamera`; [camera.html](../../ui/templates/camera.html), the candidate row and its fallback control). `verified`. | The same GET on `/ui/wizard/camera/add` fills only the camera being added, and the fallback control submits the same selection, so the add destination works without HTMX. Owning tickets: BIT-028 for the form, BIT-029 for the destination. |
| Save one camera's settings | `POST /ui/wizard/camera/{ref}/settings` saves that camera and 303s to the step, or to the review when it was opened from there; a failure is 422 on the settings destination with every other camera unchanged ([wizard.go](../../agent/internal/ui/wizard.go), `submitCameraSettings`). `verified`. | `POST /ui/wizard/camera/{ref}/settings` saves that camera and 303s to the destination it was opened from; a failure is 422 on that destination with every other camera unchanged. Owning ticket: BIT-029, with BIT-028 for the form. |
| Remove a camera from the pending list | `POST /ui/wizard/camera/{ref}/remove` removes the pending camera and 303s to the step with a notice, with no confirmation and no appliance change ([wizard.go](../../agent/internal/ui/wizard.go), `removePendingCamera`). `verified`. | `POST /ui/wizard/camera/{ref}/remove` removes the pending camera and 303s to the step, with no confirmation and no appliance change. Owning ticket: BIT-029. |
| Continue to review | `POST /ui/wizard/camera/continue` 303s to `/ui/wizard/review` when the list has at least one camera, and to the step with the empty state when it does not ([wizard.go](../../agent/internal/ui/wizard.go), `enterCamera`; [routes.go](../../agent/internal/ui/routes.go)). `verified`. | 303 to `/ui/wizard/review` when the list has at least one camera; otherwise the step re-renders with the empty state and the add action focused. Owning ticket: BIT-029. |
| Apply refused one camera | The operation records the refusal and 303s to `/ui/wizard/result`, which names the camera by name and address in its alert and says nothing on the appliance has changed; the review shows the refusal on that camera's card until that camera changes or the next apply starts ([applier.go](../../agent/internal/ui/applier.go), `newSetupValidationError`; [wizard.go](../../agent/internal/ui/wizard.go), `runApplyOperation`, `reviewData`; [operation_result.html](../../ui/templates/operation_result.html), [review.html](../../ui/templates/review.html)). `verified` by `TestApplyValidationFailureNamesTheCamera`. | The same. The review is the destination that corrects one camera, so it is where the refusal is fixed; the separate add destination remains BIT-029's. |
| Review, change cameras | Review's "Change cameras" links to `/ui/wizard/camera`, and each camera group's "Change settings" links to `/ui/wizard/camera/{ref}/settings?from=review` ([review.html](../../ui/templates/review.html), the camera group). `verified`. | "Change cameras" links to the step, and each camera group's "Change settings" links to that camera's settings destination. Owning ticket: BIT-029. |
| Settings back and save | Both return to the destination the settings was opened from: the step from a row, the review from a group. `verified`. | Both return to the destination the settings was opened from: the step from a row, the review from a group. Owning ticket: BIT-029. |

#### Rules that follow from per-camera settings

| ID | Rule | Check |
| --- | --- | --- |
| M1 | Every camera keeps its own settings. Name, address, stream port, path, optional sub stream path, sign-in and hardware address belong to the camera being added or edited. Opening one camera's settings renders only that camera's values and changes no other camera. | Q11 and Q15; a two-camera walk; the per-camera settings test in BIT-029. |
| M2 | One add or one save touches one camera. A failed add creates no row and changes no existing row, and the next add is available immediately. | Q15; the two-camera add test, including a failed first add. |
| M3 | A camera that fails validation does not block adding the next one, and a camera already in the list whose check failed does not block the review or the next add. A validation failure renders on the destination that holds the add form and creates no row; a check failure stays on the camera's own row with its reason and its two actions. | Q5 and Q15; the failed-add and failed-check-row tests. |
| M4 | Removing a camera from the pending list is not the destructive remove. It needs no confirmation, it changes nothing on the appliance, and it says that the sign-in typed for that camera is dropped. The label is "Remove from list", never "Remove camera". | Q9; [D6](patterns.md#destructive-action); the remove-from-list test. |
| M5 | A saved sign-in is never rendered. A blank sign-in field keeps the saved sign-in only while the address, port and path are unchanged; a changed endpoint asks for the sign-in again and says why. | Q10; [E6](patterns.md#edit-form) and [UX-10](../principles.md#ux-10-editing-keeps-unchanged-secrets-and-never-echoes-them); the redaction and reveal tests. |
| M6 | The review lists every camera with its own values, and its single filled action applies them all once. A failure names the camera it belongs to, and the result reports each camera's check separately. | Q1, Q10 and Q11; the two-camera apply tests in `multicam_test.go`, which read the result page, the review page and the operation record. |

This section binds the destinations, the entries, the exits and the one apply. The screen rules for
the same behaviour are A1 to A9 in [the add-and-list pattern](patterns.md#add-and-list).

#### Linkability

The step and the settings destination are transient, and so is the review they lead to: they render
from the wizard session, so a URL without that session is not the state it names. Two consequences
are specific to this step. A reload keeps the list and every camera's values, because they live in
the session, and the step's add form re-renders from the route and the live search, so a reload
there repeats no add. A reference the session does
not hold renders the missing-camera state rather than another camera's settings, so a pasted URL
never shows one camera's values under another camera's name.

## The navigation model

The header answers one question: where can this owner go from here, in this state? It is not a site
map, not a stepper, and not a set of shortcuts to a flow that has already finished.

| Rule | Statement |
| --- | --- |
| N1 | The header lists only destinations the current session can open and that have a job in the current state. |
| N2 | A header entry resolves in one request. It never redirects to sign-in or to another destination. |
| N3 | One name per destination, matching its title and its primary action. |
| N4 | The header is not the wizard stepper. The step count lives in the flow, not in the site header. |
| N5 | A flow that has completed is absent from the header, not disabled and not renamed. |

### What the header shows

| Session state | Header | Why |
| --- | --- | --- |
| Signed out | Brand | Every other route redirects to the gate, so no navigation entry is offered. |
| Setup required | Brand, Set up | `/ui/setup` is the only served destination until the administrator exists. |
| Signed in, setup incomplete | Brand, Setup, Cameras, Status, Sign out | The first run is the current job, and the camera list shows what has been entered. `Setup` points at the current step. Live is absent because no applied camera exists yet. |
| Signed in, complete, no camera | Brand, Cameras, Status, Sign out | There is no live view to open and no first-run work left. |
| Signed in, complete, at least one camera | Brand, Cameras, Live, Status, Sign out | Setup is gone permanently. Camera changes go through the list; reconfiguration is a reset. |

The current destination is marked with `aria-current="page"` on its header link, and the current
link gains weight and a stronger underline. The header is rendered only for an authenticated
session, so a signed-out visitor sees the brand and the gate.

A signed-out visitor can reach `/ui/login`, and `/ui/setup` while setup is required. Every other
`/ui` path redirects to sign-in with the requested path in `next`, so the header must not offer a
destination the visitor cannot open.

The implemented header follows the table: `Setup` is rendered only until the setup-complete
marker exists, `Live` only when `appliance.yaml` has at least one camera, and the current link
carries `aria-current="page"`. The login and setup gates render no navigation links. The
implementation is the resolver in `agent/internal/ui/landing.go`, the header in
[`base.html`](../../ui/templates/base.html), and the `ShowSetupNav` and `ShowLiveNav` values set in
`render` ([ui.go](../../agent/internal/ui/ui.go)).

### The `Setup` destination after the first run

`Setup` after the first run completes is a defect against N1 and N5, and against
[W1](patterns.md#first-run-wizard): a repeat owner gets direct edit, not the first-run wizard. The
implemented wizard has no configured check: `welcome` always renders step 1
([wizard.go](../../agent/internal/ui/wizard.go)), so the header no longer offers it after the
setup-complete marker. A direct visit remains possible in this build; it is not a normal
destination.

What replaces it:

| Need | Destination |
| --- | --- |
| Change a camera | Camera list, then camera edit ([W7](patterns.md#first-run-wizard) and [E9](patterns.md#edit-form)). |
| Add another camera | Camera list, once [BIT-029](../plan/implementation-plan.md#bit-029-multi-camera-inventory-and-add-from-listing) lands the add path. |
| Change the network, storage, or identity | Configuration reset ([ADR 0017](../adr/0017-reset-levels-and-decommissioning.md)), not the first-run wizard. |
| Recover the administrator password | Factory reset ([ADR 0017](../adr/0017-reset-levels-and-decommissioning.md)), not setup. |
| See the appliance | Status. |
| Reach the first-run wizard again | The header does not offer it after setup completes. The direct route remains for the current build. |

The navigation and the setup marker are implemented in this change. The wizard itself still has
no configured check, which remains [BIT-036](../plan/implementation-plan.md#bit-036-first-run-and-flow-contradictions-from-the-flow-layer).

## Flow rules

These rules, and the map and navigation model above them, are binding for routes, entries, exits,
redirects and destinations, together with the transition contract below. The wording of copy is
descriptive: a copy change does not require a change to this document, and a rule is not used to
litigate wording ([ADR 0027](../adr/0027-flow-layer-above-patterns.md), amendment P1).

Each rule names its cost. A rule without a cost is a slogan, and a slogan cannot be reviewed.

### F1 One decision per step

A destination asks for one decision. It has one filled button that carries that decision, or none
when it is read-only. A step does not combine two unrelated questions, and it does not ask the owner
to confirm a value that the next screen will confirm anyway.

Cost: more destinations and more taps. A combined form that would have saved a screen becomes two,
and the owner meets the step count more often.

Check: Q1 and Q11 in [the professional test](professional-test.md), and a count of filled buttons
per destination against [routes.go](../../agent/internal/ui/routes.go). A destination with two
unrelated decisions is a finding.

### F2 Every step is reversible or explicitly not

Before a commit, back returns to the previous step with its input. After a commit, the exit names
what changed and what cannot be undone. A destructive step states its effect before the button, as
[D1 to D7](patterns.md#destructive-action) require. "Cancel" returns to the object, not to the
start of the flow.

Cost: more copy, and one explicit sentence on steps that cannot be undone. Some flows gain a
confirmation destination that a reversible step would not need.

Check: Q12 and Q13, and the destructive-pattern rules for the commit. A back link that lands on a
different flow, or a cancel that restarts the flow, is a finding.

### F3 An interrupted flow can be resumed or restarted without losing work

Input the owner entered survives a reload, a closed tab, an expired session, and a daemon restart,
or the flow says that it restarted and what was cleared. Resume state has a named lifetime, and the
owner is told when it ends. A completed commit is never repeated by a resume.

Cost: server-side session state with an expiry, or state encoded in the URL, plus a cleanup path.
A transient flow has to say "this expired" instead of silently starting over.

Check: Q14, a reload at every step, and a resume after the session lifetime. The wizard interruption
test is not written; this is a recorded gap, not a pass.

### F4 A failure lands on the failing step with the input intact

A failure renders the destination that failed, not a generic page and not a new flow. Every value
survives except a secret; a secret is the stated exception, and the flow says which secret must be
entered again. One message names the field or step, the effect, and the next action, following
[UX-2](../principles.md#ux-2-a-failure-names-the-step-the-effect-and-the-next-action). The owner can
retry without retyping the non-secret input.

Cost: the failing destination must be able to render an error state, so a generic template cannot
carry every failure. Error copy grows, and a handler that used to render one page now has two
states.

Check: Q15 and Q7, the submit test for every form, and the failure test for every commit. A failure
that lands on the error page with no path back to the form is a finding.

### F5 No flow asks for a value the appliance can discover

Discovery fills the value, the screen shows where it came from, and manual entry is the fallback.
A default is not a discovered value. Do not ask for an address, port, path, MAC, vendor, model, or
identity that discovery already reported, following [UX-1](../principles.md#ux-1-discovery-before-credentials)
and [UX-3](../principles.md#ux-3-prefill-discovered-values-and-show-their-source).

Cost: discovery adds latency and a failure mode, so the flow needs a known, partial, and failed
state beside the manual path. The manual path cannot be removed.

Check: Q5 and the discovery states in [patterns.md](patterns.md#first-run-wizard), plus a template
read for a default that is presented as discovered. A flow that asks for a value the appliance
holds is a finding.

### F6 The owner always knows where they are and what happens next

Every destination has one name, used in the header, the title, and the action that leads to it.
A step shows its position and total. Every terminal action names its destination before it is taken,
and the destination the owner lands on matches that name. A screen with no next action is a dead
end, even when it has no failure.

Cost: longer headings and button labels, and one repeated step line on every wizard destination.
A short label such as "Continue" is not available.

Check: Q11, Q12, and Q9, and the route inventory against this document's map. A link label that
does not match the destination title is a finding.

### F7 No flow requires moving a secret between devices

A first-run secret reaches the device that runs the flow by QR or fragment, or it is entered once on
that device. No step asks the owner to copy a token, password, or key from one device to another.
A handoff that cannot be removed is named in the step, uses a one-time expiring value, and is
consumed when used, following [UX-7](../principles.md#ux-7-first-run-secrets-travel-by-qr-or-fragment-never-by-copy)
and [SEC-1](../principles.md#sec-1-a-secret-has-one-storage-location-and-a-redaction-test).

Cost: the QR and fragment code, the short-code fallback, and the expiry and rate-limit machinery.
The flow cannot fall back to "read the value on the console and type it on the phone" as the normal
path.

Check: Q16, the setup token tests named in [BIT-019](../plan/implementation-plan.md#bit-019-setup-token-qr-exchange),
and a template read for an instruction that moves a secret. A step that asks the owner to copy a
secret is a finding unless it names why no automatic path exists.

### F8 A linkable state has a stable URL, and a transient state is not offered as one

A destination that renders from the appliance state, or from its route, has a stable URL and is safe
to bookmark or share. A destination whose content depends on a server-side session, a one-shot
result, or a login handoff is transient: it says so, redirects with an explanation when its state is
gone, and does not present itself as a permanent destination. A new flow declares the class of every
state before the first template is written.

Cost: state that would be convenient in a session has to be encoded in the URL or dropped, and a
transient result needs a persistent home so the owner can still see what happened.

Check: Q14 and the [linkability](#linkability) table, plus a bookmark-and-reload pass on every
destination. A transient state that silently becomes a different screen is a finding.

## The transition contract

A terminal action commits something or ends the flow. Each row states two things: what the handlers
do today, and the target this document requires. The current column was checked against the named
handler and template for this change and is marked `verified` when that code states the destination,
or `inferred` when only browser behaviour or elapsed state would prove it. The target column names
the owning ticket when no handler serves it. A target is never presented as current behaviour
([ADR 0027](../adr/0027-flow-layer-above-patterns.md), amendment P3).

The header after every transition is rendered by `base.html`. Today it is the same three links plus
sign-out for an authenticated session; the target is the table in
[the navigation model](#the-navigation-model): `Setup` disappears after a successful apply, and the
signed-out header shows only sign-in.

| Terminal action | Current (verified or inferred) | Target |
| --- | --- | --- |
| Apply start | `POST /ui/wizard/apply` starts one persisted operation and 303s to `/ui/wizard/apply`; an operation that finishes within 100ms redirects straight to its terminal destination ([wizard.go](../../agent/internal/ui/wizard.go), `apply`; [routes.go](../../agent/internal/ui/routes.go)). A second start while one runs 303s to `/ui/wizard/apply?busy=1` and runs nothing. `verified` by `operation_test.go`. | The same destination. The operation section in the status contract remains [BIT-014](../plan/implementation-plan.md#bit-014-long-running-operation-progress). |
| Apply in progress | The operation page shows the current step, elapsed time, completed and remaining steps, and whether leaving is safe. A reload reads the same operation record; the live region polls every two seconds and the Technical details disclosure sits outside it ([operation.html](../../ui/templates/operation.html), [operation_panel.html](../../ui/templates/operation_panel.html)). `verified`. | The same destination, with the status page showing the same operation after [BIT-014](../plan/implementation-plan.md#bit-014-long-running-operation-progress) adds its status section. |
| Apply success | The operation 303s to `/ui/wizard/result`, titled "Configuration applied" with the steps, elapsed time, generation, every camera and its own picture check, and next-step links ([wizard.go](../../agent/internal/ui/wizard.go), `showApplyResult`; [operation_result.html](../../ui/templates/operation_result.html)). `verified`. | The same destination. The setup-complete marker and the header drop `Setup` once the configuration is applied (N5). |
| Apply, committed but a camera check failed | The same result destination. The lede says one camera has not sent a picture and that the configuration stays applied, and that camera's row carries the failure and its next action ([operation_result.html](../../ui/templates/operation_result.html); [wizard.go](../../agent/internal/ui/wizard.go), `runApplyOperation`). `verified` by `TestApplyResultNamesEveryCamera`. | The result distinguishes the committed apply from the failed camera check, and names each camera's check, so one failed camera among several is named rather than summarised. A separate partial-result state remains with [BIT-036](../plan/implementation-plan.md#bit-036-first-run-and-flow-contradictions-from-the-flow-layer). |
| Apply hard failure | The operation 303s to `/ui/wizard/result`, which names the failing step, the restored generation, the failure, and the next action. Every camera is listed with the outcome the transaction reached: "Not applied" after a rollback, and "Unknown" when no completed restore is recorded ([wizard.go](../../agent/internal/ui/wizard.go), `showApplyResult`; [operation_result.html](../../ui/templates/operation_result.html)). `verified`. | The same destination. A failed apply is never rendered from the POST, so a reload does not repeat it. |
| Update check | `POST /ui/status/updates/check` runs one forced channel check and 303s to `/ui/status`; the status card reports the check record, including a rate limit, an offline device, and a failed check, and never reports up to date for a failed check ([update_handlers.go](../../agent/internal/ui/update_handlers.go), `checkForUpdate`; [update.go](../../agent/internal/ui/update.go), `AgentUpdateService.Check`). `verified` by `TestUpdateCardCoversEveryState` and the update card action tests. | No change. The check record is the persistent result. |
| Update install start | `POST /ui/status/updates/install` starts the verified download in the background, writes the fixed request, starts the helper unit, and 303s to `/ui/status`; the card shows the preparation step and then the journal step until the operation records its history ([update_handlers.go](../../agent/internal/ui/update_handlers.go), `startUpdateInstall`; [update.go](../../agent/internal/ui/update.go), `AgentUpdateService.StartInstall`). `verified` by `TestUpdateMutationsArePostsAndRedirectToStatus` and `TestUpdatePreparingStatePollsTheStatusFragment`. | No change. The status card and the update history are the persistent result. |
| Update recovery | The status card links to `/ui/status/updates/recovery` when a stuck state is detected. The destination names the state, the effect, and what is not touched. `POST /ui/status/updates/recovery` re-detects under the operation lock, refuses a live transaction or a changed state, runs exactly one action, and 303s to `/ui/status`; the card reports recovered, started, or still stuck with the reason ([update-recovery.html](../../ui/templates/update-recovery.html); [update_handlers.go](../../agent/internal/ui/update_handlers.go), `showUpdateRecovery`, `startUpdateRecovery`; [recovery.go](../../agent/internal/update/recovery.go)). `verified` by the recovery state, refusal, quarantine and active-bundle tests. | No change. The detected state and the update history are the persistent record. |
| Manual rollback | The status card links to `/ui/status/updates/rollback`, which names the target and states that configuration and recordings are not reverted. `POST /ui/status/updates/rollback` writes the fixed manual-rollback request, starts the helper, and 303s to `/ui/status`; the card shows the rollback step and then the rolled-back state with the next-update rule ([update-rollback.html](../../ui/templates/update-rollback.html); [update_handlers.go](../../agent/internal/ui/update_handlers.go), `showUpdateRollback`, `startUpdateRollback`). `verified` by `TestUpdateCardRollbackConfirmationStatesTheLimitation` and `TestUpdateCardCoversEveryState`. | No change. A failed rollback names the step and keeps the committed version recorded. |
| Camera save, applied | 303 to `/ui/cameras?flash=edited` ([cameras.go](../../agent/internal/ui/cameras.go), `submitCameraEdit`); the list says the change was applied. `verified`. | No change. The result is transient under F8 and the changed row is the persistent record. |
| Camera save, applied but the snapshot failed | 303 to `/ui/cameras?flash=snapshot-failed`; the message reports only that the snapshot test failed ([cameras.go](../../agent/internal/ui/cameras.go), `submitCameraEdit`, `flashMessage`). `verified`. | The result states both that the change was applied and that the snapshot failed. Owning ticket: [BIT-029](../plan/implementation-plan.md#bit-029-multi-camera-inventory-and-add-from-listing). |
| Camera save failure | The edit form re-renders at 200 from the POST with the apply error; a validation failure is 422 on the same destination ([cameras.go](../../agent/internal/ui/cameras.go), `submitCameraEdit`, `renderCameraEdit`). `verified`; the reload repeating the apply is `inferred`. | The failing edit step is rendered from a GET so a reload does not repeat the apply, with the values intact. Owning ticket: [BIT-036](../plan/implementation-plan.md#bit-036-first-run-and-flow-contradictions-from-the-flow-layer), with [BIT-028](../plan/implementation-plan.md#bit-028-camera-onboarding-ux) for the form. |
| Camera remove success | 303 to `/ui/cameras?flash=removed` ([cameras.go](../../agent/internal/ui/cameras.go), `removeCamera`); the list says the camera and its saved credential were removed. `verified`. | No change to the destination. A Back step after the removal must not land on a removed object; [BIT-029](../plan/implementation-plan.md#bit-029-multi-camera-inventory-and-add-from-listing) owns it. |
| Camera remove, apply failure | The remove page re-renders at 200 with "The camera was not removed" ([cameras.go](../../agent/internal/ui/cameras.go), `removeCamera`). `verified`; the reload repeating the destructive commit is `inferred`. | The failing remove step is rendered from a GET so a reload does not repeat the commit. Owning ticket: [BIT-036](../plan/implementation-plan.md#bit-036-first-run-and-flow-contradictions-from-the-flow-layer), with [BIT-029](../plan/implementation-plan.md#bit-029-multi-camera-inventory-and-add-from-listing). |
| Camera removed, credential delete failed | The error page at 500, with a hand-edit instruction and a filled "Open setup" ([cameras.go](../../agent/internal/ui/cameras.go), `removeCamera`; [error.html](../../ui/templates/error.html)). `verified`. | A remove result that names the credential failure and offers an in-product retry, with an exit to the camera list. Owning ticket: [BIT-029](../plan/implementation-plan.md#bit-029-multi-camera-inventory-and-add-from-listing), with [BIT-010](../plan/implementation-plan.md#bit-010-factory-reset-and-support-bundle-skeleton) for the credential path. |
| Setup completion (administrator) | 303 to `/ui/wizard` ([auth.go](../../agent/internal/ui/auth.go), `setup`); welcome, step 1 of 6. `verified`. | The same destination on success. The header entry remains while the setup-complete marker is absent and drops after the apply (N1, N5); the replacement name is [OQ-32](../open-questions.md#oq-32-navigation-destination-after-first-run). |
| Login success | 303 to the validated `next`, else the device landing resolver: `/ui/wizard` before setup completes, `/ui/live` with a camera, `/ui/status` with none ([auth.go](../../agent/internal/ui/auth.go), `login`; [landing.go](../../agent/internal/ui/landing.go), `resolveLanding`). `verified` by `TestResolveLandingForEveryDeviceState` and the browser routing checks. | The same. The destination is stable and the sign-in handoff is transient, as F8 requires. |
| Logout | 303 to `/ui/login` ([auth.go](../../agent/internal/ui/auth.go), `logout`). The signed-out header renders only the brand; the navigation is not offered ([base.html](../../ui/templates/base.html)). `verified`. | The same. |
| Session expiry, GET | 303 to `/ui/login?next=<original URI>` ([auth.go](../../agent/internal/ui/auth.go), the unauthenticated case); sign-in returns to the original destination. `verified`. | No change. The `next` handoff returns the owner to the requested work. |
| Session expiry, HTMX fragment | The fragment request gets 401 with `HX-Redirect` to `/ui/login?next=<owning page>`, never a 303 followed by login HTML. The live region keeps its content and shows the reconnecting notice until the top-level navigation starts ([auth.go](../../agent/internal/ui/auth.go), `redirectToAuth`). `verified` by the fragment auth test. | No change. A missing or expired session always produces one top-level sign-in, not injected content. |
| Session expiry, POST | 303 to `/ui/login?next=/ui/status` ([auth.go](../../agent/internal/ui/auth.go), the unauthenticated case); the form input is gone. `verified` for the redirect; the lost input is `inferred` from the absence of any form state. | Sign-in returns to the interrupted form with its input, or the destination says the work was lost and where to restart. Owning ticket: [BIT-036](../plan/implementation-plan.md#bit-036-first-run-and-flow-contradictions-from-the-flow-layer). |
| Wizard session expiry | A silent new session ([wizard.go](../../agent/internal/ui/wizard.go), `getOrCreate`); `/ui/wizard/done` redirects to review, which redirects to camera. There is no message and no values. `verified`; the expiry after one idle hour is `inferred` because no test exercises it. | A named restart state that says what was cleared, including the pending camera list, and whether the apply happened. Owning ticket: [BIT-036](../plan/implementation-plan.md#bit-036-first-run-and-flow-contradictions-from-the-flow-layer), with [BIT-029](../plan/implementation-plan.md#bit-029-multi-camera-inventory-and-add-from-listing) for the list; the apply result is [BIT-014](../plan/implementation-plan.md#bit-014-long-running-operation-progress). |
| Wizard cancel | 303 to `/ui/wizard` after deleting the session ([wizard.go](../../agent/internal/ui/wizard.go), `cancel`); no template invokes the route. `verified` (latent). | Cancel leaves the flow for its entry or the object it came from, without restarting the wizard. Owning ticket: [BIT-036](../plan/implementation-plan.md#bit-036-first-run-and-flow-contradictions-from-the-flow-layer). |
| Unknown path or handler failure | The error page ([ui.go](../../agent/internal/ui/ui.go), `notFound`; [auth.go](../../agent/internal/ui/auth.go), `csrfFailure`; [cameras.go](../../agent/internal/ui/cameras.go), `renderError`), with a filled "Open setup" and an "Open status" link. `verified`. | An error destination whose filled exit belongs to the flow that failed, with no return to a flow that cannot recover. Owning ticket: [BIT-036](../plan/implementation-plan.md#bit-036-first-run-and-flow-contradictions-from-the-flow-layer), with [BIT-007](../plan/implementation-plan.md#bit-007-bittern-ui-first-run-wizard-skeleton) for the shared page. |

One state is still missing rather than wrong. A committed apply whose camera check failed has no
separate heading: the result page says "Configuration applied", its lede says the camera is not
recording, and the camera's row carries the failure. A separate partial-result state remains with
[BIT-036](../plan/implementation-plan.md#bit-036-first-run-and-flow-contradictions-from-the-flow-layer).
The operation destination now exists at `/ui/wizard/apply`; its status counterpart and the update,
reset and prewarm writers remain with
[BIT-014](../plan/implementation-plan.md#bit-014-long-running-operation-progress).

The navigation after every transition is unchanged today: the same three links, plus sign-out only
when the owner is authenticated. The target is the table in
[the navigation model](#the-navigation-model): `Setup` disappears after a successful apply, and the
signed-out header shows only sign-in.

## Linkability

A linkable state is one that a bookmark, a reload, or a pasted URL renders as the same thing it
rendered for the owner who copied it.

| State | URL | Linkable | Why |
| --- | --- | --- | --- |
| Status, current | `/ui/status` | Yes | Renders the current status document. `?state=` is a preview-only switch and is not a product state. |
| Camera list, current | `/ui/cameras` | Yes | Renders the current camera inventory. |
| Camera edit | `/ui/cameras/{id}/edit` | Yes, object scoped | Stable while the camera exists. After removal it names the missing camera and the next step. |
| Camera remove | `/ui/cameras/{id}/remove` | Yes, object scoped | Same, and the effect statement names the camera. |
| Update recovery confirmation | `/ui/status/updates/recovery` | Yes while a stuck state exists | Renders the detected state and one action; when nothing is stuck it says so and offers status, without inventing an action. |
| Update rollback confirmation | `/ui/status/updates/rollback` | Yes while a target exists | Renders the current update state; when no target exists it says so and offers status, without inventing a target. |
| Sign in | `/ui/login` | Yes | Renders the sign-in form. `?next=` is contextual and not required. |
| First-run setup | `/ui/setup` | Conditional | Only while setup is required. After completion a signed-out visit redirects to sign-in, and a signed-in visit follows the device landing resolver. |
| Wizard welcome to review | `/ui/wizard`, `/ui/wizard/network`, `/ui/wizard/storage`, `/ui/wizard/review` | No | The screen depends on the server-side wizard session. A bookmark without the cookie gets a fresh session, and review redirects to the camera step when the list is empty. |
| Camera step | `/ui/wizard/camera` | No | The rows come from the wizard session, so a URL without that session renders the empty step, not the owner's list. A reload in the same browser keeps the list. |
| Camera add | `/ui/wizard/camera/add` | No | The form and the search result re-render from the route and the live search, so a reload in the same browser repeats no add. The camera it adds lands in that browser's list. |
| Camera settings | `/ui/wizard/camera/{ref}/settings` | No, object shaped | The reference exists only in the wizard session. A reference the session does not hold renders the missing-camera state with the camera step as the exit. |
| Apply operation | `/ui/wizard/apply` | Yes | Reads the persisted operation record, so a reload, a reopened tab, or a second browser shows the same running operation or redirects to its result. Query `?busy=1` only adds the refusal message. |
| Apply result | `/ui/wizard/result` | Yes | Renders the persisted terminal result until the next operation replaces it. A running operation redirects back to the operation page. |
| Wizard done | `/ui/wizard/done` | No | The older success page. Requires `session.Applied`; otherwise it redirects to review and then to camera. The apply flow reaches the result page, which remains readable after the wizard session expires. |
| Camera result | `/ui/cameras?flash=edited`, `?flash=removed`, `?flash=snapshot-failed` | No | A one-shot result carried in the query. The next navigation loses it. |
| Sign-in handoff | `/ui/login?next=...` | No | Contextual to the request that was interrupted. |
| Error page | any failing route | No | It renders the failure of one request, not a destination. |

The rule for a new flow is F8: declare one of three classes before the first template.

| Class | Test | Requirement |
| --- | --- | --- |
| Persistent | Renders from appliance state or from the route alone | A stable URL, safe to bookmark and share, with no session dependency |
| Object scoped | Renders from an object id plus appliance state | A stable URL while the object exists, and a named missing-object state with the next step when it does not |
| Transient | Depends on a session, a one-shot result, or a login handoff | Never the only place a result is shown; redirects with an explanation when its state is gone |

A transient state is not a bug. A transient state that silently becomes a different screen, or that
is the only record of a result, is.

The class definitions are binding. Which state belongs to which class, and the consequence for
bookmarks, refresh and future HTMX swaps, is [OQ-33](../open-questions.md#oq-33-linkable-and-transient-states).
The table above is the current assignment; that question records the recommendation.

## What this document does not decide

- The operation destination and its states. [BIT-014](../plan/implementation-plan.md#bit-014-long-running-operation-progress)
  owns them, and the route is intentionally absent here.
- The resume lifetime of the wizard. It is one hour in
  [wizard.go](../../agent/internal/ui/wizard.go) (`sessionTTL`), the owner is not told, the pending
  camera list is cleared with it, and the value is provisional until the operation view and the
  interruption test exist.
- How the add destination lays out the candidate list, the manual fallback, the required stream
  fields and the disclosure. [BIT-028](../plan/implementation-plan.md#bit-028-camera-onboarding-ux)
  owns the form; this document owns the destinations around it.
- Whether a configured appliance can ever re-enter the wizard. [ADR 0017](../adr/0017-reset-levels-and-decommissioning.md)
  gives configuration reset as the reconfiguration path, so the answer here is no; the handler
  change is not part of this document.
- Whether the header shows the flow name or the current step label while the first run is
  incomplete. Either satisfies N1, and the choice belongs to the wizard ticket.
- Which destination replaces `Setup` in the header after the first run completes. [OQ-32](../open-questions.md#oq-32-navigation-destination-after-first-run)
  owns the decision; this document requires only that the completed flow is not offered (N5).
- Which states are stable URLs and which are transient. [OQ-33](../open-questions.md#oq-33-linkable-and-transient-states)
  owns the state-by-state assignment; F8 and the class table above are binding.
- The kiosk entry model. [OQ-25](../open-questions.md) is open, and the kiosk would inherit these
  destinations rather than fork them.

The implemented contradictions are inventoried with file and line in
[the flow-layer defect note](../notes/2026-11-21-flow-layer-defects.md) and owned by
[BIT-036](../plan/implementation-plan.md#bit-036-first-run-and-flow-contradictions-from-the-flow-layer).
They are not fixed in this change: the note is the evidence, and the ticket owns the fix.

Related: [README.md](README.md), [patterns.md](patterns.md), [components.md](components.md),
[professional-test.md](professional-test.md),
[the interaction research](../research/2026-11-21-ui-design-interaction.md),
[the precedents research](../research/2026-11-21-ui-design-precedents.md),
[principles](../principles.md), [ADR 0017](../adr/0017-reset-levels-and-decommissioning.md),
[ADR 0019](../adr/0019-ui-stack.md), [ADR 0028](../adr/0028-missing-recording-drive-and-boot.md).

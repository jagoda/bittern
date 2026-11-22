# Flow-layer defect inventory

- Date: 2026-11-21
- Status: open. Recorded while adding [the flow layer](../design/flows.md); no `ui/` or handler
  change is included.
- Applies to: every destination under `/ui`, the redirects in `agent/internal/ui`, and the
  navigation in `ui/templates/base.html`.
- Related: [flows](../design/flows.md), [patterns](../design/patterns.md),
  [the professional test](../design/professional-test.md), [routes.go](../../agent/internal/ui/routes.go),
  [wizard.go](../../agent/internal/ui/wizard.go), [cameras.go](../../agent/internal/ui/cameras.go),
  [auth.go](../../agent/internal/ui/auth.go), [base.html](../../ui/templates/base.html),
  [error.html](../../ui/templates/error.html).

The flow layer states the rules between screens. The implemented flows contradict several of them.
This note is the evidence: one row per contradiction, with the file and line, the rule, the owner
consequence, and the artifact that should own the fix. Each row is triaged to the ticket that owns
the flow or the screen; [BIT-036](../plan/implementation-plan.md#bit-036-first-run-and-flow-contradictions-from-the-flow-layer)
owns the fixes as one prioritized unit. No row is fixed in this change.

All findings are open. The severities use
[the professional test](../design/professional-test.md#severity).

## Navigation and exits

| Finding | Evidence | Impact | Rule | Owner | Destination |
| --- | --- | --- | --- | --- | --- |
| [Major] `Setup` stays in the header after setup completes and opens the first-run wizard as a normal destination. | `ui/templates/base.html:18`; `agent/internal/ui/wizard.go:143` (`welcome` has no configured check); repeated as "Back to setup" in `ui/templates/done.html:26`, `ui/templates/done.html:31`, `ui/templates/status.html:7`, `ui/templates/cameras.html:11`, `ui/templates/cameras.html:72`. | A repeat owner can replay the wizard and treat a first-run apply as an edit. The header says setup is unfinished when it is not. | N1, N5, W1, F6 | frontend, wizard | BIT-007 for the wizard guard; BIT-029 for the camera-list links |
| [Minor] The signed-out header offers `Setup`, `Cameras`, and `Status`, and every one of them bounces. | `ui/templates/base.html:18-20`; `agent/internal/ui/auth.go:29-36` (setup-required redirect), `agent/internal/ui/auth.go:65-71` (unauthenticated redirect), `agent/internal/ui/auth.go:72-74` (setup after sign-in goes to status). | A signed-out owner taps a destination and gets a sign-in form, or signs in and lands somewhere other than the link promised. | N2, F6 | frontend, auth | BIT-007 |
| [Major] The error page's filled exit is `Open setup` for every failure, including failures inside the camera flow. | `ui/templates/error.html:8-9`; `agent/internal/ui/auth.go:227-235` (form token); `agent/internal/ui/cameras.go:162-170` (credential delete); `agent/internal/ui/cameras.go:341-353` (camera form read); `agent/internal/ui/ui.go:240-246` (unknown path). | A camera-form failure sends the owner to the first-run wizard instead of the form that failed. A removed camera's not-found page offers setup. | F4, F6, Q12 | frontend | BIT-007 and the camera ticket |
| [Minor] The camera list's empty state and page links point at setup after setup is complete. | `ui/templates/cameras.html:11` (filled `Open setup` is the only empty-state action), `ui/templates/cameras.html:72`. | The only action offered for an empty list restarts the finished first-run flow instead of adding a camera. | L5, F6 | frontend, camera list | BIT-029 |

## Back and reload

| Finding | Evidence | Impact | Rule | Owner | Destination |
| --- | --- | --- | --- | --- | --- |
| [Major] An apply failure renders the review at 200 from the POST, so a reload re-submits the apply. | `agent/internal/ui/wizard.go:339-343`; `ui/templates/review.html:55-58`. | The owner cannot tell whether the first apply ran, and the browser asks to resubmit a commit that may already have committed or rolled back. | F3, F8, Q14 | wizard, apply | BIT-014 and BIT-007 |
| [Major] A camera edit apply failure renders the form at 200 from the POST, so a reload re-submits the apply. | `agent/internal/ui/cameras.go:94-96`; `ui/templates/camera.html:81`. | Reloading after a failed save repeats the apply, and the owner cannot tell whether the camera changed. | F3, F8, Q14 | camera edit | BIT-028 and BIT-029 |
| [Major] A camera remove apply failure renders the remove page at 200 from the POST, so a reload re-submits the removal. | `agent/internal/ui/cameras.go:152-161`; `ui/templates/camera-remove.html:21-26`. | A reload repeats a destructive commit, and a second submit after a slow apply can show a different result. | F3, Q14 | camera list | BIT-029 |
| [Minor] Browser Back after a successful removal lands on the remove page of a camera that no longer exists. | `agent/internal/ui/cameras.go:135-173` (success redirects to `?flash=removed`), `agent/internal/ui/cameras.go:176-191` (`loadCamera` 404); `ui/templates/error.html:8`. | The owner sees "Camera not found" and a filled `Open setup` instead of the list, and the removal result is gone. | F2, Q13 | camera list | BIT-029 |
| [Minor] A form-token failure loses the form and offers no return to it. | `agent/internal/ui/auth.go:227-235`; `ui/templates/error.html:8-9`. | The owner must use browser Back and re-enter the form; the page offers setup and status instead. | F4, Q15 | auth | BIT-007 |

## Resume and interruption

| Finding | Evidence | Impact | Rule | Owner | Destination |
| --- | --- | --- | --- | --- | --- |
| [Major] The wizard session expires after one hour with no message, and `/ui/wizard/done` cascades through review to the camera step. | `agent/internal/ui/wizard.go:23` (`sessionTTL`), `agent/internal/ui/wizard.go:729-755` (`getOrCreate` silently creates a new session), `agent/internal/ui/wizard.go:351-358` (`showDone` redirect), `agent/internal/ui/wizard.go:316-324` (`showReview` redirect). | An interrupted setup loses the camera and the applied result. The owner sees an empty camera step and cannot tell whether the apply happened. | F3, F4, Q14 | wizard | BIT-007 |
| [Major] A POST during an expired auth session loses the form and lands on status after sign-in. | `agent/internal/ui/auth.go:65-70` (`target = SafeNext("")` for a non-GET request). | The owner signs in and loses the edit or apply they were making. The input is gone and the destination is not the form. | F4, Q15 | auth | BIT-007 |
| [Major] The apply POST holds the request open and has no operation destination. | `ui/templates/review.html:55-58`; `agent/internal/ui/routes.go` has no operation route. | An apply that takes minutes has no page to return to. A reload loses progress and the owner cannot tell whether the appliance changed. | F3, O3, O4 | operation view | BIT-014 |

## Failure and input

| Finding | Evidence | Impact | Rule | Owner | Destination |
| --- | --- | --- | --- | --- | --- |
| [Minor] A failed setup attempt loses the token and both password fields, and the page does not say what must be re-entered. | `ui/templates/setup.html:18` (token), `ui/templates/setup.html:26` (password), `ui/templates/setup.html:30` (confirmation), none repopulated; `agent/internal/ui/auth.go:170-201` (error renders). | After a password mismatch or a weak password, the owner must find the token again. The short code may expire, and the page does not name the value to re-enter. | F4 (secret exception), Q15 | setup | BIT-019 and BIT-007 |
| [Major] A camera save with a failed snapshot reports only "The snapshot test failed", not that the change was applied. | `agent/internal/ui/cameras.go:100-104` (chooses `snapshot-failed`), `agent/internal/ui/cameras.go:317-326` (flash text); `ui/templates/cameras.html:5`. | The owner cannot tell whether the edit applied. The list reads like a standalone failed test. | F4, F6, Q12 | camera list | BIT-029 |
| [Major] A removed camera whose credential cannot be deleted lands on the error page with a hand-edit secret instruction. | `agent/internal/ui/cameras.go:162-170`; `ui/templates/error.html:8`. | The owner is told to edit `/etc/bittern/secrets.yaml` by hand, with no in-product path, and the exit is setup. The raw path also fails Q10. | F4, F7, SEC-1, Q16 | camera list, secrets | BIT-010 and BIT-029 |
| [Major] The done page says "Setup complete" beside a failed camera snapshot. | `ui/templates/done.html:3` (`h1`), `ui/templates/done.html:13-16` (failed snapshot alert). | A partial success is stated as complete, so the owner cannot tell what is running and what failed. | F4, F6, W9 | wizard | BIT-007 and BIT-014 |
| [Minor] `/ui/wizard/cancel` deletes the session and redirects back into the wizard. | `agent/internal/ui/wizard.go:368-371`; the `wizard-cancel` route in `agent/internal/ui/routes.go` has no template action. | Cancelling restarts the flow and loses the wizard input instead of leaving it. No current template invokes the route, so this is latent. | F2 | wizard | BIT-007 |
| [Minor] The not-found copy is written for the setup flow only. | `agent/internal/ui/ui.go:240-246` ("That page is not part of the setup flow."). | After setup, an unknown path says the page is not part of setup, which is not the owner's model. | F6, Q10, Q11 | wizard, shared error page | BIT-007 |

## What this note does not do

It does not fix any row, and it does not decide the per-row ticket. [BIT-036](../plan/implementation-plan.md#bit-036-first-run-and-flow-contradictions-from-the-flow-layer)
owns the fixes; each row is triaged to the ticket that owns the flow or the screen. The
flow rules and the transition contract live in [flows.md](../design/flows.md); the review questions
that should catch the next instance are Q11 to Q16 in
[the professional test](../design/professional-test.md).

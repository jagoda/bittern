# The professional test

- Date: 2026-11-18; updated 2026-11-21
- Status: Accepted. The design check and four-width browser check are in place, and the test applies
  to the implemented token layer and templates.
- Applies to: every UI change, every visual review, every flow change that touches more than one
  screen, and every agent that touches a template or `ui/static/bittern.css`.
- Companion documents: [README.md](README.md), [tokens.md](tokens.md),
  [components.md](components.md), [patterns.md](patterns.md), [flows.md](flows.md).

This is the sixteen-question test. Q1 to Q10 come from
[the precedents research](../research/2026-11-21-ui-design-precedents.md), rewritten for Bittern;
Q11 to Q16 come from [the flow layer](flows.md). Every answer is evidence, not opinion. A "no" or
an "I do not know" is a finding. The test is run by the implementing agent before human review,
then by the reviewer on the same evidence. It does not replace the BIT-020 browser check; it starts
after the check is green.

Q1 to Q10 are answered for a screen and its states. Q11 to Q16 are answered for a transition and the
destination it lands on. A screen-only change may record the flow questions as `n/a` with the
reason; a change to an entry, an exit, a redirect, or a result answers all sixteen.

## Severity

One finding gets one severity and one fix. Severity is about the owner's task, not about how much
work the fix needs.

| Severity | Definition | Merge rule |
| --- | --- | --- |
| Blocker | The owner cannot complete the job, or a core flow fails WCAG 2.2 AA or the security rules. | Stop. Fix before merge. |
| Major | The owner can complete the job but is misled, cannot tell the state, or the core flow is unreliable. | Fix before merge, or record an approved exception with a removal ticket. |
| Minor | Friction or inconsistency with a workaround, or a local rule is broken without changing the task. | Fix in the same change or open a ticket with the evidence. |
| Nit | Polish with no task impact. | Optional. Never blocks a merge. |

A finding is not a severity label on a preference. If the rule, the evidence, or the consequence is
missing, it is not a finding. The precedents research calls the three-part test rule, evidence,
consequence. Use it.

## Finding format

Every finding names the question number, the rule, and the evidence. The shortest acceptable form
is:

```
[severity] Qn [rule] at [route, viewport, state]: [evidence]. The owner [consequence]. Fix: [smallest change].
```

Example:

```
[Major] Q7 visible label and linked error at /ui/cameras/{id}/edit, 390px, empty port:
the page says "The camera change was not applied" at the top, the port field is not marked,
and the message does not name the field. The owner has to hunt for the field and cannot tell
whether the port is optional. Fix: put "Enter a port between 1 and 65535" next to the port
field and link the summary entry to it.
```

The finding goes in the change description or a dated note under [docs/notes](../notes/) when it
comes from a run. The review record below is the short form.

## Review record

Copy this block into the change or the review note. Use `pass`, `fail`, or `n/a` for each question,
and add the evidence next to the answer. A question that does not apply gets a one-line reason.

```
Page:
Route:
Template:
Widths: 360 / 390 / 1280 / 1920
State: empty / one / many / loading / partial / error / success / degraded
Flow: entry -> destination -> exit, or n/a
Q1: pass | fail | n/a, evidence
Q2: pass | fail | n/a, first three fixations and screenshot
Q3: pass | fail | n/a, off-scale values and CSS lines
Q4: pass | fail | n/a, wrap, overflow, line length, fact rows
Q5: pass | fail | n/a, states checked and what each said
Q6: pass | fail | n/a, tab order and focus evidence
Q7: pass | fail | n/a, error strings and focus result
Q8: pass | fail | n/a, visible label, accessible name, role and target sizes
Q9: pass | fail | n/a, strings and CSS transforms
Q10: pass | fail | n/a, paths, identifiers, status codes, service names
Q11: pass | fail | n/a, entry paths and what the destination needs
Q12: pass | fail | n/a, exit labels and where they land
Q13: pass | fail | n/a, browser Back and the back link
Q14: pass | fail | n/a, method, status, and reload result
Q15: pass | fail | n/a, failing step, kept values, next action
Q16: pass | fail | n/a, handoff, lifetime, value moved
Findings:
- [severity] Qn [rule] at [route, viewport, state]: evidence. Consequence. Fix.
```

## Q1: what is the page's one job, and where is the one filled button that does it?

**Look at.** The route, the `h1`, the template, and the buttons. Say the job in one sentence without
using the word "page". Count the filled buttons. A read-only screen has zero filled buttons and its
primary action is the first text link that moves the owner to the next real task.

**Pass.** The job is one sentence. There is exactly one filled button, or zero on a read-only
screen. The filled button is the job, not a secondary action. The primary action is reachable
without scrolling past unrelated evidence.

**Fail example.** [`ui/templates/cameras.html`](../../ui/templates/cameras.html) renders a filled
`Edit` button in every camera card. With four cameras there are four filled `Edit` buttons; in the
empty state the only action is a filled `Open setup`. The owner cannot tell whether the page's job
is to add, edit, or test. Fix: make `Add camera` the page-level primary action and make each
card's `Edit` a secondary button or a link.

**Record.** `Q1: fail`, the route and state, the number of filled buttons, and the template lines.

## Q2: squint, what are the first three things you see, in order, and is that the intended order?

**Look at.** A screenshot at 360, 1280, and 1920, blurred by 5 to 10 pixels if possible. Name the
first three things that survive the blur. Compare them with the order the owner needs. Count the
loud elements: a loud element is a filled button, a large title, a coloured panel, or a badge that
competes with the title.

**Pass.** The first three fixations are the page name, the overall state or object, and the next
action or decision. There are at most two loud elements per region. A decorative icon or a
navigation item does not win.

**Fail example.** [`ui/templates/status_panel.html`](../../ui/templates/status_panel.html) renders
nine equal cards with badges. At 1280 the eye lands on the first card, not on the overall state
sentence, and no card is ranked by what needs a decision. Fix: one summary with the overall state
and the first repair action, then sections ordered by action, with evidence behind a disclosure.

**Record.** `Q2: fail`, the first three fixations in order, the screenshot path, and the intended
order.

## Q3: name every spacing, radius, shadow, and type size. Which ones are outside the token set?

**Look at.** The template classes, the computed styles, and `ui/static/bittern.css`. Use a CSS grep
for `padding`, `margin`, `gap`, `border-radius`, `box-shadow`, `font-size`, and `font-weight`.
Compare each value with [tokens.md](tokens.md).

**Pass.** Every value is a token. There are one or two rectangular radii plus the pill, no shadow,
two weights, and four type sizes. Sibling gaps are equal and section gaps are larger. No `clamp()`
grows type with the viewport.

**Fail example.** [`ui/static/bittern.css`](../../ui/static/bittern.css) defines one radius and one
spacing token, then uses roughly 25 spacing values, `0.35rem`, `0.4rem`, and `0.45rem` radii,
`clamp()` type sizes, and `font-weight: 700`. The preview notice alone uses a `0.35rem` radius and
a `0.25rem` border. Fix: replace each literal with the mapped token in [tokens.md](tokens.md), or
add a token deliberately in the same change.

**Record.** `Q3: fail`, the off-scale values with CSS line numbers, and the token that replaces
each one.

## Q4: at 360, 390, 1280, and 1920, what wraps, truncates, or runs past 90 characters per line?

**Look at.** Full-page screenshots at 360, 390, 1280, and 1920. Check the page for horizontal
scroll, clipped text, a control outside the viewport, a table that scrolls the page instead of its
own container, and a fact row whose label and value sit at opposite edges of a wide card. Measure
line length in characters.

**Pass.** No page-level horizontal scroll. Body copy is 45 to 90 characters. Hints are at most
60 characters. Long names and addresses wrap inside their container. A fact row keeps the label and
value together within a bounded pair. A wide page uses its width for a second column instead of
leaving half the viewport empty.

**Fail example.**
[BIT-033](../plan/implementation-plan.md#bit-033-visual-quality-pass-across-phone-and-desktop)
records fact rows stretching a label and value to opposite ends of a 1088 pixel card, and the status
page staying a single narrow column at 1920 by 1080. Fix: bound the fact pair and let the status
grid use two columns where the content allows it.

**Record.** `Q4: fail`, the screenshot path, the measured line length, and the element that
overflowed or stretched.

## Q5: what shows in every state?

**Look at.** The state matrix in [patterns.md](patterns.md). Force each reachable state with
`--ui-mock` or a test fixture. Check empty, one item, many items, loading at 0, 2, and 30 seconds,
partial discovery, authentication required, unreachable, validation error, apply running, apply
failed, apply succeeded but health failed, stale status, and JavaScript disabled.

**Pass.** Every state says what is happening, what is missing or changed, and what the owner can do
next. Empty is not "No data available". Unknown is not zero. Partial names the known and unknown
parts. A stale status says when it was built. A partial success does not say "complete".

**Fail example.** [`ui/templates/done.html`](../../ui/templates/done.html) says "Setup complete"
while a camera snapshot alert below says the camera has not delivered a picture. The status page
also says "This section has not reported yet" for a section that may be not applicable. Fix: say
"Configuration applied; camera check failed" and separate never-reported from not-applicable.

**Record.** `Q5: fail`, the state, the exact string, and the missing next action. A state that was
not checked is recorded as `n/a` with the reason.

## Q6: tab through: is focus visible on every control, in order, and never behind a sticky header?

**Look at.** Keyboard only, from the skip link to the last control. Check focus order, the 3px ring
at 3px offset, focus on disclosure summaries, table scroll regions, and the primary action. Check
that the sticky or fixed header does not cover the focused control at 360 and 1280.

**Pass.** Every keyboard-operable control has a visible focus ring. Order matches the reading order.
No control is hidden behind a header or footer. A scrollable table can receive focus and has a
name. `outline: none` appears only on a non-interactive skip target.

**Fail example.** A `.table-wrap` scrolls horizontally but has no `tabindex`, `role="region"`, or
accessible name, so a keyboard user cannot reach the hidden columns. Fix: make the wrapper a named,
focusable region, as the interaction research requires.

**Record.** `Q6: fail`, the tab sequence, the element that cannot be reached, and a focus
screenshot.

## Q7: does every error name the field, say what is wrong, and state the fix in text?

**Look at.** Submit every form empty, then with one invalid value per field. Read the summary and
the field message. Check that values are preserved, that the summary links to the field, that focus
moves to the summary or the field, and that the same words appear in both places. Check that the
message says what happened and how to fix it, without "invalid", "oops", or a raw status code.

**Pass.** The summary and the field agree. The field is identified by its visible label. The fix is
a concrete instruction. Values survive. Focus moves to the summary or the first field in error. The
page does not validate while the owner is still typing.

**Fail example.** [`ui/templates/camera.html`](../../ui/templates/camera.html) renders an error
summary as a list of `Label: Message`, but the entries are not links and focus is not moved. The
required RTSP port and path sit inside the "Advanced settings" disclosure, whose hint says
"Optional". The owner has to hunt for the failing field and is told a required field is optional.
Fix: link and focus the summary, move required stream fields out of the optional group, and repeat
the same message at the field.

**Record.** `Q7: fail`, the field, the summary string, the field string, and whether focus moved.

## Q8: does every label match the accessible name, and does every target meet its role's minimum?

**Look at.** The DOM and the rendered page. For every input, compare the visible label, the
accessible name, and the placeholder. Then measure the hit area of every link, button, summary,
radio, and select at 360, and sort each control by role: navigation, an action row, a whole-row
target, or everything else. Check the skip link and the header. A link inside a sentence is the
only link role that may stay below 44, at a 24 by 24 hard floor; a standalone link is a navigation
or action target. WCAG 2.2 AA SC 2.5.8 exempts inline text, and this system keeps the floor anyway.

**Pass.** Every input has a visible label above it. The accessible name contains the visible label.
The placeholder is only a format example. Navigation links, action rows, and whole-row targets
measure at least 44 by 44 CSS pixels (`--touch-target`). Every other interactive control measures
at least 24 by 24 (`--touch-target-min`), including a link inside a sentence. No control is below
24 by 24. Hit area counts, visual size does not. Icon-only controls have a name.

**Fail example.** [`ui/templates/setup.html`](../../ui/templates/setup.html) uses `visually-hidden`
labels for the setup token and the administrator password. The accessible name exists, but a
sighted owner sees only a hint and no persistent field name. Fix: show a visible label and keep the
hint for the format. A second example is a header navigation link whose text line is 32 pixels high
with no 44 pixel row around it: it is a navigation target, so it fails Q8 even though the text is
legible. Fix: give the link the row's 44 pixel height and padding.

**Record.** `Q8: fail`, the field, the visible label, the accessible name, the role, the measured
hit area, and the token the role requires. Use `n/a` for a screen with no inputs.

## Q9: read aloud: sentence case, verbs on buttons, one noun per object across screens?

**Look at.** Read the copy top to bottom, then read only the buttons and links. Check every heading,
label, button, table header, and status word. Look for Title Case, ALL CAPS, synonyms for the same
object, and a button that says "OK", "Submit", "Yes", or "Continue" when the next screen does
something specific.

**Pass.** Sentence case everywhere except proper nouns. Every button starts with a verb and names
the result. One noun per object across screens. A status word is the same word in the badge, the
sentence, and the evidence.

**Fail example.** The camera list says `Remove` while the confirmation says `Remove camera`. The
review page says `Configuration file` and `Apply`, the status page says `Render and validation`,
and the camera page says `Camera change`. The owner has to decide whether these are the same
things. Fix: sentence case, one verb for the transaction, and one noun per object.

**Record.** `Q9: fail`, the strings, and the synonym list.

## Q10: does owner copy contain system nouns?

**Look at.** Primary copy only: headings, ledes, labels, hints, button labels, alerts, and empty
states. Search for `/`, `cam0`, `lan0`, `appliance.yaml`, `cam0`, `401`, `500`, `render`, `apply`,
and vendor service names. Then check that the raw value is available in a disclosure, an evidence
list, or the support bundle.

**Pass.** Primary copy uses the owner's words. A path, identifier, HTTP status, or service name
appears only in evidence, a disclosure, or a support bundle. A value the owner must type, such as
an address or a port, is allowed when it is the answer to the field.

**Fail example.** [`ui/templates/review.html`](../../ui/templates/review.html) prints
`{{.Plan.ConfigPath}}` under "Configuration file".
[`ui/templates/status_panel.html`](../../ui/templates/status_panel.html) prints a raw path under
"Path". The setup page explains that the token is in a root-only file. Fix:
say "Settings file" in primary copy and put the path in evidence; keep the raw path only where the
owner needs it to act.

**Record.** `Q10: fail`, the string, the template line, and where the raw value should live.

## Q11: how does the owner get here, and does the destination have what it needs when they do?

**Look at.** Every entry point in [the information architecture map](flows.md#the-information-architecture-map):
the header, a link, a redirect, a bookmark, and browser Back. Read what the handler does when the
state the destination needs is missing, and count the redirects between the label and the screen.

**Pass.** Every entry matches an intent the destination serves. The destination either has the state
it needs or says what is missing and how to get it. The label matches the destination title, and no
entry silently cascades through more than one redirect.

**Fail example.** `/ui/wizard/done` requires `session.Applied`
([wizard.go](../../agent/internal/ui/wizard.go), `showDone`). When the one-hour wizard session
expires, `showDone` redirects to review, and `showReview` redirects to the camera step
([wizard.go](../../agent/internal/ui/wizard.go), `showReview`). The owner asked for the result of
an apply and landed on an empty camera form with no message. Fix: render a "This setup session
expired" state with "Start setup again" and "Open status", and do not cascade through two
redirects.

**Record.** `Q11: fail`, the entry path, the redirect chain, and the first screen without context.

## Q12: does every exit do what its label says, including the exit after a result?

**Look at.** The primary action's destination, every secondary link, the header, and browser Back.
Compare each label with the destination title. Read the result destination: does it offer the next
real task and a way back to the object, or does it exit the flow?

**Pass.** Every exit names its destination. The primary action lands where its copy said. A result
offers the next real task and the object it changed. No exit leaves the flow for a page with no way
back, and the header is not the only exit.

**Fail example.** [`error.html`](../../ui/templates/error.html) makes "Open setup" the filled exit
for every failure, including a failed form token on a camera form and a failed credential deletion
after a camera removal ([auth.go](../../agent/internal/ui/auth.go), `csrfFailure`;
[cameras.go](../../agent/internal/ui/cameras.go), `removeCamera`). The owner is sent to the
first-run wizard instead of the form or the camera list. Fix: the error template names the failing
flow and offers "Back to the camera form" or "Open cameras".

**Record.** `Q12: fail`, the route, the exit label, the destination, and what the owner expected.

## Q13: what does browser Back do here, and does the back link agree?

**Look at.** The POST to redirect to GET history, the previous entry in the history, and every
"Back to ..." link. Check whether the previous screen still exists after the action, and whether the
input and the result survive the return.

**Pass.** Back never resubmits a mutation. Back from a result returns to the object or the list with
the result still visible. A back link names the previous step and keeps its input. An object that
was removed gets a named "already removed" state with a link to the list, not a generic error.

**Fail example.** After a successful camera removal, the list is at
`/ui/cameras?flash=removed`. Back returns to `/ui/cameras/{id}/remove`, and `loadCamera` now renders
"Camera not found" with a filled "Open setup" ([cameras.go](../../agent/internal/ui/cameras.go),
`removeCamera` and `loadCamera`; [error.html](../../ui/templates/error.html)). The owner cannot see
the result and is offered the wrong flow. Fix: render "This camera is already removed" with a link
to the camera list when the remove destination has no object.

**Record.** `Q13: fail`, the history step, the resulting screen, and whether the input and result
survived.

## Q14: what happens if the owner reloads at this point?

**Look at.** The response method and status, the redirect, where the state lives, and any one-shot
result in the query. Reload the form, the result, and the failure state.

**Pass.** Every mutation is POST, then redirect, then GET. A reload of a result shows the same
result. A reload of a form keeps its values and does not repeat the change. A transient state that
has expired says so or redirects with an explanation. A one-shot result has a persistent home.

**Fail example.** An apply failure renders the review at 200 from the POST
([wizard.go](../../agent/internal/ui/wizard.go), `apply`), and a camera edit apply failure renders
the form at 200 from the POST ([cameras.go](../../agent/internal/ui/cameras.go),
`submitCameraEdit`). A reload re-submits the apply, and the browser asks to confirm a commit that
may already have run. Fix: redirect to a GET result state that renders the error, or use the
operation destination [BIT-014](../plan/implementation-plan.md#bit-014-long-running-operation-progress)
owns.

**Record.** `Q14: fail`, the method, the status, where the state lives, and what the reload did.

## Q15: when the step fails, where does the owner land, what is kept, and what is the next action?

**Look at.** Every error return in the handler, the template it renders, the values it passes, and
the action it offers. Walk the failure with the form half filled. Treat a secret as the stated
exception and check that the page says which secret must be re-entered.

**Pass.** The failure lands on the failing step with the values intact, except a secret. One message
names the field or step, the effect, and the next action. The owner can retry without retyping the
non-secret input. A secret is named as needing re-entry. No failure lands on a generic page that
offers a different flow.

**Fail example.** An expired session on a form POST redirects to `/ui/login?next=/ui/status`
([auth.go](../../agent/internal/ui/auth.go), the unauthenticated case), so the camera or wizard
input is gone and sign-in lands on status. The setup page also loses the token after a password
mismatch and does not say it must be re-entered ([setup.html](../../ui/templates/setup.html), the
token field). Fix: keep the failing destination in `next`, and say "Enter the setup token again"
in the setup error.

**Record.** `Q15: fail`, the failing step, the kept and lost values, the message, and the next
action.

## Q16: does any step move a secret between devices, and is that handoff named and expiring?

**Look at.** The setup token and its QR or fragment, the short fallback code, and every instruction
to read a value on one device and enter it on another. Read the terminal steps too: a flow that
sends the owner to a shell to edit a secret is a handoff even when no second device is named.

**Pass.** A first-run secret reaches the device that runs the flow by QR or fragment, or it is
entered once on that device. A fallback is short, one-time, and expiring. No step asks the owner to
copy a password, key, or token between devices. A handoff that cannot be removed is named in the
step.

**Fail example.** No between-device secret copy exists in the current setup, and the QR path is the
model ([BIT-019](../plan/implementation-plan.md#bit-019-setup-token-qr-exchange)). The nearest
failure is the credential-deletion error after a camera removal, which sends the owner to edit
`/etc/bittern/secrets.yaml` by hand ([cameras.go](../../agent/internal/ui/cameras.go),
`removeCamera`). That is a secret operation with no in-product path. Fix: delete the credential
through the agent, or state the exact key and why no automatic path exists; never ask the owner to
read the secret.

**Record.** `Q16: pass` or `fail`, the handoff, its lifetime, and the value moved. Use `n/a` when
no handoff exists, with the reason.

## What is not a finding

The precedents research separates wrong, taste, and unknown. Use the same test:

1. Name the rule: a WCAG criterion, a token, a pattern rule, a platform convention, or a Bittern
   principle.
2. Name the evidence: route, viewport, state, screenshot, measurement, or the exact string.
3. Name the consequence for the owner: what they cannot tell, find, or recover from.

If all three hold, it is a finding. If only the rule holds, it is a principle to discuss. If none
hold, it is taste. Say so and do not spend review time on it.

A rule with no possible check is marked advisory in the design documents. An advisory rule cannot
produce a blocker or a major finding on its own. It can support a finding that already has a
checkable rule behind it.

## The screen review order

1. Read the route, title, and primary action. Answer Q1.
2. Blur the screenshot and answer Q2.
3. Read the CSS and answer Q3.
4. Check the widths and answer Q4.
5. Walk the state matrix and answer Q5.
6. Tab through and answer Q6.
7. Submit the forms and answer Q7.
8. Inspect labels and targets and answer Q8.
9. Read the copy and answer Q9.
10. Search the owner vocabulary and answer Q10.

The order matters because a missing state or a wrong job is a bigger finding than a radius. Do not
start with polish. The precedents research says the same: states before style.

## The flow review order

Run this on a change to an entry, an exit, a redirect, a result, or a resume. It is also the order
to walk a transition that a screen change affects.

1. Entry: list every path in, and read what the destination does without its state (Q11).
2. Exit: follow every action and link, including the result, and compare the label with the landing
   screen (Q12).
3. Back: use browser Back from the result and from the form, then follow every back link (Q13).
4. Reload: reload the form, the failure, and the result, and watch the method and status (Q14).
5. Failure: force each failure, including an expired session, and check the kept values, the
   message, and the next action (Q15).
6. Handoff: walk the first-run secret and any instruction that moves a value between devices or to
   a terminal (Q16).

A flow that cannot answer one of the six is not reviewed. A transition with no defined destination
is a finding against [F8](flows.md#f8-a-linkable-state-has-a-stable-url-and-a-transient-state-is-not-offered-as-one),
not a note to self.

Related: [README.md](README.md), [tokens.md](tokens.md), [components.md](components.md),
[patterns.md](patterns.md), [flows.md](flows.md),
[the precedents research](../research/2026-11-21-ui-design-precedents.md),
[the foundations research](../research/2026-11-21-ui-design-foundations.md).

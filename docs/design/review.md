# Authoring and review

- Date: 2026-11-21
- Status: Accepted. The review procedure applies to the implemented token layer, the templates and
  the browser check.
- Applies to: every change to `ui/templates/`, `ui/static/`, a browser-facing flow, or a
  browser-check fixture, and every UI review.
- Companion documents: [README.md](README.md), [audience.md](audience.md), [copy.md](copy.md),
  [copy-inventory.md](copy-inventory.md), [tokens.md](tokens.md),
  [components.md](components.md), [patterns.md](patterns.md), [flows.md](flows.md),
  [professional-test.md](professional-test.md), [evolution.md](evolution.md),
  [CHANGELOG.md](CHANGELOG.md).

This is the protocol for the agent or person writing a UI change and for the reviewer reading it.
Both work from the same evidence: the rendered screens, the screenshots, and the rules. The
professional test in [professional-test.md](professional-test.md) owns the questions, the review
record, and the severity definitions; this file owns the order of work and the pass conditions.

One rule applies to both roles: the agent or person who wrote a change does not approve it. A
one-maintainer project cannot always buy that independence, so it is required when a second reader
exists and named as an accepted risk when none does.

## Authoring sequence

Work in this order. Each step is cheaper than the one after it, and a failure at step 1 is not
fixed by a screenshot at step 6.

1. **Read the pattern and the flow.** Find the screen in [patterns.md](patterns.md) and read its
   job, primary action, steps, states, and rules. Read the destination, its entries, its exits, and
   its transition in [flows.md](flows.md). Read the component entries in
   [components.md](components.md) for every component the screen will use, including the do and
   don't lines. Read [audience.md](audience.md) and [copy.md](copy.md) before writing an
   owner-facing string, and use [copy-inventory.md](copy-inventory.md) when the change is the
   rewrite. If no pattern covers the screen, stop: that is a new pattern, and
   [evolution.md](evolution.md) sets the evidence bar before any markup.
2. **Name the job and the one action.** Write the page's job in one sentence without the word
   "page". Decide the one filled button, or that the screen is read-only and has none. Decide the
   states before the layout: empty, one, many, loading, partial, error, success, degraded, stale.
3. **Use tokens.** Write CSS only with the tokens in [tokens.md](tokens.md). No hex value or raw
   pixel value outside the token block, no off-scale spacing, no `clamp()`, no shadow, no
   transition that is not a state change. A value that the token set does not have is a token
   change, not a literal.
4. **Compose existing components.** Use the anatomy, variants, and states from
   [components.md](components.md). Do not restyle a component from a pattern, do not nest a card
   in a card, and do not add a variant that belongs in the component layer. A one-off exception
   needs a reason, a scope, an expiry, and a removal ticket.
5. **Run the browser check.** `make ui-browser-check` must pass before anything is read. Provision
   it once from [tests/browser/README.md](../../tests/browser/README.md): Node, the pinned
   Playwright package, and Chromium. It is opt-in, needs network access to install, and is never
   part of `make check`. If the check cannot run, say so in the change and do not call the change
   reviewed.
6. **Read the screenshots.** Open every changed screen at 360, 390, 1280, and 1920 in
   `build/ui-screenshots/`, in colour, and read the `-expanded` copy for every screen with a closed
   disclosure. Read the 360 and 1280 images again in greyscale when the change touches a state
   colour, a badge, or a boundary. This is the step that catches what the check cannot:
   [UX-12](../principles.md#ux-12-a-passing-geometric-check-is-not-proof-a-screen-looks-right) and
   [UX-13](../principles.md#ux-13-read-the-full-page-screenshots-before-human-review).
7. **Apply the professional test.** Answer Q1 to Q16 from
   [professional-test.md](professional-test.md) for each changed screen and state, and Q11 to Q16
   for each changed transition, with the evidence beside the answer. Write each failure as a
   finding, not as a note to self.
8. **Record and land the checks.** Put the review record and the findings in the change
   description. Fix every blocker and major finding or record the approved exception with its
   removal ticket. Land the check the change created, and the changelog entry when the change is
   one of the four classes in [evolution.md](evolution.md). Run `make check` for the offline
   checks.

Commands, in order:

```sh
make ui-browser-check   # geometry floor; opt-in, needs Node and Playwright
make check              # offline: links, schema, style, design, fixtures, packaging, Go tests
```

`make design-check` enforces the mechanical stylesheet rules: no colour literal or raw `px` or
`rem` value outside `:root`, no `text-transform: uppercase`, and no `!important`. It runs inside
`make check`. The richer design rules still need the browser check and the screenshot read. If a
rule is violated twice, [evolution.md](evolution.md) makes the missing check the work.

## Review sequence

Review in this order. A missing state or a wrong job is a bigger finding than a radius, and the
order makes that explicit.

1. **Gestalt.** Look at the 360 and 1280 screenshots without reading. Name the first three things
   that survive a squint, and compare them with the order the owner needs. This is Q2.
2. **Hierarchy.** Name the page's job, the one primary action, and the loud elements per region.
   Check the heading order, the section boundaries, and whether spacing or a border is doing the
   grouping. This is Q1, Q2, and Q9.
3. **States.** Walk the state matrix for the pattern. Force each state with `--ui-mock` or a
   fixture, and read the sentence, the next action, and the absence of a false success. This is Q5
   and Q7.
4. **Widths.** Read the four images again for the review, not only during authoring: 360 and 390
   for wrapping, target size, and line length (360 is the supported minimum and a floor that must
   pass, and both phone widths are acceptance widths); 1280 for layout and section grouping; 1920
   for use of width, fact pairs, and reading measure. Confirm no page-level horizontal scroll, no
   clipped text, no control outside the viewport, and a table that scrolls inside its own named
   region. This is Q4 and Q8.
5. **Keyboard.** Tab from the skip link to the last control at 360 and 1280. Check focus
   visibility, order, disclosure summaries, the table scroll region, the primary action, and that
   a sticky header or footer never covers the focused control. This is Q6.
6. **Copy.** Read the screen aloud. Check sentence case, verbs on buttons, one noun per object, a
   state word that matches its badge and its evidence, and no path, status code, or service name
   in primary copy. Read the string against the glossary and the sentence rules in
   [copy.md](copy.md), including the disclosure boundary and the component rule. This is Q9 and
   Q10.
7. **Flow.** Walk the transition: entry, exit, back, reload, failure, and handoff. Read the labels
   against the destinations, and confirm that a transient state says so and a result has a
   persistent home. This is Q11 to Q16 and the flow rules in [flows.md](flows.md).
8. **The rules that apply to the screen.** Name the pattern rule IDs and the component checks that
   the change touches, for example W5 and W6 for the review step, E2 and E4 for the camera form,
   L5 for the empty state. A rule that is broken is a finding with the rule ID. A rule that is
   wrong is an [evolution.md](evolution.md) change, not a review comment that the next reader will
   never see.

A reviewer who has time for only one step does step 3. A screen with a missing state is not saved
by good spacing.

## Severity

One finding gets one severity and one fix. Severity is about the owner's task, not the size of the
fix. The definitions are the professional test's; they are repeated here so a review can be read
without opening a second file.

| Severity | Definition | Merge rule |
| --- | --- | --- |
| Blocker | The owner cannot complete the job, or a core flow fails WCAG 2.2 AA or the security rules. | Stop. Fix before merge. |
| Major | The owner can complete the job but is misled, cannot tell the state, or the core flow is unreliable. | Fix before merge, or record an approved exception with a removal ticket. |
| Minor | Friction or inconsistency with a workaround, or a local rule is broken without changing the task. | Fix in the same change or open a ticket with the evidence. |
| Nit | Polish with no task impact. | Optional. Never blocks a merge. |

A defect that the browser check reports as a failure is not automatically a blocker. It is a
finding, and the severity follows the owner's consequence: a control that cannot be focused on the
only path to add a camera is a blocker, and a warning about a 40 pixel link inside a sentence is a
minor.

## What a finding contains

Every finding has four parts: the question and rule, the evidence, the owner consequence, and the
smallest fix. The question number is not optional: it is how a later reader knows which
professional-test question the finding answers.

```
[severity] Qn [rule ID] at [route, viewport, state]: [evidence]. The owner [consequence]. Fix: [smallest change].
```

| Part | What it is | Bad example | Good example |
| --- | --- | --- | --- |
| Question and rule | The professional-test question and the rule ID that the change breaks, or "advisory" when no checkable rule exists | "this feels cramped" | `Q6 E8` |
| Evidence | Route, viewport, state, screenshot name, exact string, CSS line, or measurement | "the form is confusing" | `at /ui/cameras/{id}/edit, 390px, port empty` |
| Consequence | What the owner cannot tell, find, do, or recover from | "bad UX" | "the owner has to hunt for the field and cannot tell whether the port is optional" |
| Fix | The smallest change that removes the consequence | "redesign the form" | "link the summary entry to the port field and move required streams out of the optional disclosure" |

Worked example:

```
[Major] Q7 E2 at /ui/cameras/{id}/edit, 390px, empty port: the summary says "The camera
change was not applied", the port field is not marked, and the required port sits inside
"Advanced settings" whose hint says "Optional". The owner is told a required field is
optional and has to hunt for it. Fix: move the required stream fields out of the disclosure,
mark them required, and link the summary entry to the field.
```

A finding is not a severity label on a preference. If the rule, the evidence, or the consequence
is missing, it is not a finding: it is taste or a principle to discuss. Say which, and do not
spend review time on it.

Where findings go:

- In the change description, as the review record's `Findings` list.
- In a dated note under [docs/notes](../notes/) when the finding comes from a run, a hardware
  session, or a review that outlives the change.
- In the ticket that owns the screen when the finding is deferred, with the same evidence.

They do not go into a second tracker.

## Review record

Copy this into the change. The sixteen-question record in
[professional-test.md](professional-test.md) is the canonical per-screen form; this is the short
version the review sequence produces.

```
Screens:
Routes:
Widths read: 360 / 390 / 1280 / 1920, colour; greyscale for state changes
Expanded disclosures read: yes | no | n/a
States walked:
Pattern and flow rules applied: W__ S__ L__ E__ O__ D__ R__ F__
Component checks: template read / CSS grep / keyboard / submit / reduced motion
Browser check: pass | fail, command and date
Gestalt:
Hierarchy:
States verdict:
Widths verdict:
Keyboard verdict:
Copy verdict:
Flow verdict:
Findings:
- [severity] Qn [rule] at [route, viewport, state]: evidence. Consequence. Fix.
Deferred:
- finding -> ticket or debt row, with the reason
```

A review record with an empty `Findings` line is a claim that the screen passed. That claim is
allowed only after the screenshots were read at all four widths and the states were walked.

## The browser check is the floor

A passing `make ui-browser-check` proves geometry at four fixed viewports: no page-level
horizontal scroll, every visible control can be scrolled into the viewport and focused, no control
is covered at its centre, and every control meets its role's target: 44 by 44 CSS pixels for
navigation, action rows, and whole-row targets, and 24 by 24 everywhere else, including a link
inside a sentence. It is a floor, not a sign-off.
[UX-12](../principles.md#ux-12-a-passing-geometric-check-is-not-proof-a-screen-looks-right) records
why: the check reported 0 failures at 360 and 390 while the maintainer saw poor wrapping and
sizing on a phone.
[UX-13](../principles.md#ux-13-read-the-full-page-screenshots-before-human-review) records who
reads the images: the agent or person implementing the change, before human review.

What the check cannot see, from [ADR 0025](../adr/0025-browser-check-for-the-responsive-ui.md) and
[tests/browser/README.md](../../tests/browser/README.md):

- Firefox, WebKit, real touch input, device pixel ratio, dynamic viewport units, and browser zoom.
- The self-signed TLS interstitial, and whether a physical camera can scan the setup QR.
- Hierarchy, density, line length, wrapping quality, and whether the first fixations match the
  owner's order.
- Any state that the harness did not reach, any disclosure that did not open, and any content
  behind an interaction the harness does not perform.
- Anything about copy, error wording, one noun per object, or a raw path in primary copy.

So the pass condition for a UI change is both:

1. `make ui-browser-check` passes, and
2. the author has read the full-page screenshots at 360, 390, 1280, and 1920, including every
   `-expanded` image, and the professional test's answers are recorded.

A green check with unread screenshots is a failed review, not a passed one. A check failure with a
recorded severity, the owner consequence, and a ticket or a fix is a reviewable change.

## Widths

360 CSS pixels is the supported minimum and a floor that must pass. Both 360 and 390 are
acceptance widths: a screen is accepted only when it holds at both, so a green run at 390 does not
cover 360 and a green run at 360 does not cover 390.

The check measures four viewports, and every one is an acceptance width:

| Name | CSS pixels | Role |
| --- | --- | --- |
| phone-narrow | 360 by 800 | The supported minimum and a floor that must pass; also an acceptance width |
| phone | 390 by 844 | A phone acceptance width |
| desktop | 1280 by 800 | A desktop acceptance width |
| desktop-wide | 1920 by 1080 | A desktop acceptance width |

The phone pair exists because the layout breaks at 38rem, 44rem, and 60rem, so neither 360 nor 390
is a midpoint sample and one width does not prove the other. The desktop pair exists because a wide
screen has to use its width, not only avoid overflow. A width below 360 is not supported and is not
measured.

## Screenshots

The per-run images are working artifacts. `make ui-browser-check` writes one full-page image per
screen and width to the gitignored `build/ui-screenshots/`, named
`<screen>-<width>x<height>.png`, plus an `-expanded` copy for each screen with a closed disclosure.
At the current manifest that is 68 default images from 17 screens and 13 routes at four widths,
before the expanded copies ([tests/browser/README.md](../../tests/browser/README.md)). They stay
uncommitted, they go away with the build directory, and a change does not add a binary to the
repository for them.

The durable record is a curated set, attached to the gate result or the release, not committed per
change:

- A gate result, such as a hardware run or a phase gate, carries the set for the screens and states
  the gate read, at the four widths, in colour, plus the greyscale and `-expanded` copies the
  review used.
- A release attaches the set that shows the released screens and states to the release record.

The curated set is what a later reader opens; the per-run directory is what the author reads before
human review. A review record names the images it read, so the curated set can be found from the
change.

Retention: the curated set stays with its gate result or release for the life of the record, and
the per-run images are disposable and are not restored.

## When to stop

A change is good enough to merge when all of these are true:

- No blocker or major finding is open, and every minor finding is fixed or has a ticket with the
  evidence.
- The browser check passes and every changed screen was read at 360, 390, 1280, and 1920,
  including the expanded disclosure state.
- The change follows an existing pattern and uses only tokens and existing components, or it
  carries the class evidence that [evolution.md](evolution.md) requires.
- The states the pattern names are reachable and each one says what is happening and what to do
  next. A state that could not be reached is recorded as `n/a` with the reason.
- Every changed transition was walked with Q11 to Q16, including browser Back, a reload of the
  result, and one forced failure.
- The offline checks pass: `make check`.
- The changelog has an entry if the change is a token, component, pattern, or breaking change.

Defer to a ticket instead of fixing in the same change when:

| Situation | Why it is not this change's fix |
| --- | --- |
| The fix needs a new token, variant, or pattern | It is a class change with its own evidence bar, and mixing it into a ticket fix hides the decision. |
| The defect predates the change and is not on the changed flow | The change did not cause it; fixing it widens the diff and the review without a ticket that owns the screen. |
| The fix belongs to a flow another ticket owns, such as the operation view or camera onboarding | That ticket owns the pattern, the states, and the tests; a local patch would be undone by it. |
| The fix is a restyle of a working screen with no rule behind it | That is taste or a redesign, and the inventory comes before the redesign. |
| The evidence is one occurrence with no rule and no owner consequence | Fix the instance where it happened; promote it only when the finding repeats. |
| The fix needs a decision that is still open | Record the [open question](../open-questions.md); a provisional value is allowed only when the change names it and the ticket that revisits it. |

A deferred finding is never closed by silence. It lands in the change's `Deferred` list, in the
ticket that owns it, and, when it is a standing gap, in the design debt table in
[evolution.md](evolution.md) with the trigger that would make it worth fixing.

Related: [README.md](README.md), [tokens.md](tokens.md), [components.md](components.md),
[patterns.md](patterns.md), [flows.md](flows.md), [professional-test.md](professional-test.md),
[evolution.md](evolution.md), [CHANGELOG.md](CHANGELOG.md),
[ADR 0025](../adr/0025-browser-check-for-the-responsive-ui.md),
[ADR 0026](../adr/0026-design-system-and-ui-review.md),
[tests/browser/README.md](../../tests/browser/README.md).

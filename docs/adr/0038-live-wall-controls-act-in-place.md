# 0038. Live wall controls act in place, per pane, without an exit button

*The wall keeps every player alive while its controls change the view, each pane owns its controls
in an overlay on that pane, empty panes are placeholders rather than forms, full screen is requested
directly and leaves by Escape or a named touch swipe, and the stream control is an explicit selector
with a current-stream line.*

- Status: Accepted
- Date: 2026-11-27
- Deciders: maintainer
- Related: [ADR 0019](./0019-ui-stack.md), [ADR 0022](./0022-community-functionality-ux-wins.md),
  [ADR 0025](./0025-browser-check-for-the-responsive-ui.md),
  [ADR 0026](./0026-design-system-and-ui-review.md),
  [ADR 0034](./0034-live-view-owned-by-bittern.md),
  [ADR 0037](./0037-nvr-grid-per-pane-quality-and-full-screen.md),
  [live view pattern](../design/patterns.md#live-view),
  [live player island](../design/components.md#live-player-island),
  [wall controls note](../notes/2026-11-27-wall-controls-redesign.md),
  [BIT-023](../plan/implementation-plan.md#bit-023-live-view)

## Context

[ADR 0037](./0037-nvr-grid-per-pane-quality-and-full-screen.md) built the NVR wall: six layouts, no
per-pane chrome, per-pane quality, capacity demotion and full screen of the layout. The first field
run on real hardware found five mismatches between the decision and the way an owner uses a wall:

- The full screen control called `requestFullscreen()` and swallowed the rejection, so a browser
  that refused the request left the owner with no change and no sentence.
- A layout choice submitted the layout form and navigated. The page reloaded, every player was
  replaced, and full screen ended. The quality control acted in place, but its form still invited a
  submit.
- The selected pane's controls lived in one wall overlay. The layout picker, camera chooser and
  quality control were separated from the pane they changed, and the overlay covered panes other
  than the selected one.
- An empty pane rendered a camera select and a submit button. Sixteen panes with two cameras showed
  fourteen forms, and each placeholder read as a card rather than an empty slot.
- The full screen exit was a button in the overlay. It was the only named exit, and it hid the
  browser's own exit paths behind a control that could cover a pane.

The constraints from [ADR 0034](./0034-live-view-owned-by-bittern.md) and the isolation, media,
authentication and capacity decisions in
[ADR 0037](./0037-nvr-grid-per-pane-quality-and-full-screen.md) are unchanged. This decision replaces
only the control model, the layout change mechanism, the placeholder shape and the full-screen exit
paths.

## Decision

**Every wall control acts in place, controls for a pane live in that pane's overlay, an empty pane
is a placeholder that opens the camera chooser, full screen is entered from a direct click on the
wall and left with Escape or a named touch swipe, and the stream control names the quality that is
playing.** The sections below are the parts of that decision.

### 1. The wall keeps sixteen pane slots

The server renders all sixteen pane slots for `/ui/live`, whether the current layout shows one, four,
six, eight, nine or sixteen. A slot beyond the current layout carries `hidden` and is not rendered,
not counted for capacity, not in the arrow-key geometry, and not a tab stop. The server resolves the
assignment and the per-pane quality for every slot, so a layout change does not ask it to invent a
pane.

A layout change updates `data-layout` on the wall, shows and hides the slots, keeps the selected pane
when it is still visible, and persists the choice with an authenticated inline `POST /ui/live/layout`
that returns `204`. The page does not navigate and does not reload. A player in a pane that stays
visible keeps its island, its media connection and its state. A pane that becomes hidden stops its
media connection; it starts again if the layout shows it later. A camera assignment acts in place in
the same way: the chosen camera's island moves to the target pane or is cloned from the camera
template, the target pane becomes occupied, and the pane it left becomes a placeholder.

The cookie keeps its name, path, flags and one-year lifetime from ADR 0037. A layout change writes the
layout and preserves the assignment and quality of every slot, including slots that are hidden.

### 2. Pane controls live on their pane

Every occupied pane has its own overlay. The overlay holds that pane's current-stream line, its
stream selector and its camera chooser. No quality, camera or pane action sits in the wall overlay.
The wall overlay holds only the layout picker, the full screen control and the wall-level state and
hints.

A pane overlay appears for the pane under the pointer or the pane with keyboard focus. It hides with
the wall's three-second idle rule. While hidden it is `inert` with `pointer-events: none`,
`visibility: hidden` and zero opacity, so it cannot take focus or intercept a click meant for the
video. The pane content (the camera name and a failure sentence) is separate from the control overlay
and fades with the same idle state; a failed pane keeps its name and sentence visible.

A pointer press selects the pane and reveals its overlay. A tap on an empty pane selects it and opens
its camera chooser. A pointer or keyboard focus never reveals a control for another pane.

### 3. The layout picker is an icon radio group

The layout picker has seven native radio choices: automatic and the six layouts. Each choice is one
inline SVG that draws the arrangement the layout actually uses, including the primary-plus-right-and-
bottom shape for six and eight. The icon is `aria-hidden`; the accessible name is the layout name in
a visually hidden span. The group is a native radio group, so arrow keys move the selection and the
choice applies in place.

The selected state is visible without colour alone: the native radio is checked and the selected
label carries a thicker border and a stronger weight. The no-JavaScript path keeps seven submit
buttons with `aria-pressed` and the same icons. A submit from either path is captured and applied in
place when JavaScript is available; without JavaScript the form is a real submit and a page load.

### 4. Empty panes are placeholders

A layout larger than the assignment shows an empty pane as a distinct placeholder: the unknown
surface, one centered plus icon and the words "Choose camera". It has no select, no submit button and
no form visible at rest. The whole pane is the target: a pointer press or a keyboard Enter or Space
selects the pane and opens its camera chooser in that pane's overlay. The pane has no player, opens
no media session, and is not a capacity pane. At sixteen panes with two cameras, fourteen
placeholders read as empty slots, not as fourteen forms.

### 5. Full screen is requested directly and left by named gestures

The full screen control calls `requestFullscreen()` on the wall element synchronously inside its
click handler, so user activation is intact. If the browser has no Fullscreen API or rejects the
request, the wall shows a plain-language notice that says the browser did not allow full screen and
names the browser's own full-screen control. The notice is visible; the rejection is never swallowed.

There is no exit-full-screen button. The exit paths are:

- Escape, including while a pane overlay or one of its controls has focus.
- A swipe down of at least 120 CSS pixels on a touch device, with the horizontal movement no larger
  than the vertical movement.
- The browser's own full-screen exit gesture or control.

Entering full screen shows a brief hint in the wall overlay: "Full screen. Press Escape, or swipe
down at least 120 pixels on a touch screen, to exit." The hint is the discoverable statement of the
gesture. A layout change inside full screen stays in full screen and keeps the current layout fitted
to the screen.

### 6. The stream control is an explicit selector

On `/ui/live`, an occupied multi-stream pane has a labelled stream selector with the options Main
stream, Sub stream and Snapshots. The selected option is the quality the pane is playing, and the
pane overlay prints the same choice as "Current stream: ...". A capacity demotion that shows
snapshots says so in the same line while the selector shows Snapshots. Changing the selector closes
and reopens one media connection; it does not navigate, reload, or replace the island.

A camera with one stream shows a disabled selector with one option, "One stream", and the reason in
the pane overlay: this camera does not report a second stream, so main and sub use the same picture.
The control does not offer main and sub choices that do nothing. A pane assigned a camera after first
paint updates its selector and current-stream line from the same data.

## Consequences

### Positive

- A layout change is instant and does not interrupt the streams that remain on screen. The wall
  behaves like an NVR instead of a form that navigates.
- The control for a pane is where the owner points or focuses. The wall overlay no longer carries a
  camera chooser or quality control for a pane that is somewhere else.
- Empty slots are legible at sixteen and do not turn the wall into a form.
- A refused full screen is a sentence, and the exit paths work on a keyboard and on a touch screen
  without a button covering a pane.
- The selector removes the guess about which quality is playing and makes the one-stream case
  honest.

### Negative / costs

- The server renders sixteen pane slots rather than the visible count. The extra markup is hidden,
  holds no player in an empty slot, and stops hidden players, but it is more DOM than the old page.
- A layout change starts players for newly visible occupied panes and stops players for hidden panes.
  The wall must keep the capacity model and the selection consistent with the visible set.
- The in-place camera assignment needs one camera template per enabled camera and one move path. A
  failure to save shows a notice and leaves the screen in the changed state until reload.
- The pane overlay is a second overlay state machine beside the wall overlay. Its focus, inert and
  idle rules have to be tested at phone and desktop widths.

### Superseded clauses in ADR 0037

| Superseded clause | Replacement |
| --- | --- |
| Section 2: an empty pane shows one "Choose camera" control; it is not chrome around a video. | Section 4 here: a distinct placeholder with no visible form, and the camera chooser in the pane overlay. |
| Section 6: a layout change is a form submission and a page load, and the browser leaves full screen on that navigation. | Section 1 here: the layout changes in place, the page does not navigate, and full screen stays. |
| Section 6: the exit paths include the overlay's "Exit full screen" control. | Section 5 here: Escape, a swipe down of at least 120 pixels, or the browser's own exit. |
| Section 7: one wall overlay carries the selected pane's name, quality, camera picker and layout picker. | Section 2 here: pane controls live on their pane; the wall overlay carries only the layout picker, full screen and wall state. |
| Section 7: the layout picker is a form of buttons and radio choices labelled with counts. | Section 3 here: one arrangement icon per layout, a native radio group, and in-place application. |
| Section 7: Tab reveals the overlay and moves focus to the selected pane's quality control; the exit control is a tab stop. | Section 2 and section 5 here: focus reveals the pane overlay, Tab moves within it, and there is no exit control. |
| ADR 0034 section 4: a layout change is a form submission and a page load. | Section 1 here: the layout changes in place and the page does not navigate. |

The capacity, snapshot interval, cookie format, media proxy, authentication boundary, player
isolation, muted video and keyboard geometry from ADR 0037 are unchanged.

## Alternatives considered

- **Keep the layout form and reload.** Rejected by the maintainer's field report: the players stop,
  full screen ends, and the owner loses the wall for a control that should be immediate. The player
  isolation rule is about a poll replacing a player, not about refusing a deliberate layout change.
- **Rebuild the wall from a fragment on layout change.** Rejected: it would replace player islands
  for panes that stay visible. Sixteen slots with visibility is smaller and keeps the islands.
- **Keep one wall overlay and move it under the pointer.** Rejected: a wall overlay still holds a
  control for a pane other than the one it covers, and the camera chooser for an empty pane cannot be
  on the pane. Per-pane overlays are the direct model.
- **Show every empty pane's camera chooser.** Rejected: fourteen forms at sixteen panes is the
  defect the field run reported. A placeholder plus an on-demand overlay is one control per pane
  when it is needed.
- **Keep the exit button and add Escape and a swipe.** Rejected: the brief removes the button. The
  hint and the browser's own exit make the gestures discoverable, and Escape stays available with an
  overlay open.
- **Use numbers with a selected colour for the layout picker.** Rejected: numbers do not show the
  arrangement, and colour alone is not a selected state. One arrangement icon per layout plus the
  native radio is readable in greyscale.
- **Leave the one-stream camera with disabled main and sub choices.** Rejected: a disabled choice
  still tells the owner a choice exists. One option and the reason is the honest control.

## References

- [ADR 0025: Browser check for the responsive UI](./0025-browser-check-for-the-responsive-ui.md)
- [ADR 0026: Design system and UI review](./0026-design-system-and-ui-review.md)
- [ADR 0034: Live view owned by Bittern](./0034-live-view-owned-by-bittern.md)
- [ADR 0037: The NVR grid, per-pane stream quality, and full screen](./0037-nvr-grid-per-pane-quality-and-full-screen.md)
- [Live view pattern](../design/patterns.md#live-view)
- [Live player island](../design/components.md#live-player-island)
- [Wall controls redesign note](../notes/2026-11-27-wall-controls-redesign.md)
- MDN, Fullscreen API (accessed 2026-11-27):
  <https://developer.mozilla.org/en-US/docs/Web/API/Fullscreen_API>

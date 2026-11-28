# 0039. Compact pictorial pane controls, click dismissal and in-place status actions

*Compact glyph controls replace the text stack in a pane, a click on a video dismisses the shown
overlays, and update check and install act inside the status card.*

- Status: Accepted
- Date: 2026-11-28
- Deciders: maintainer
- Related: [ADR 0019](./0019-ui-stack.md), [ADR 0025](./0025-browser-check-for-the-responsive-ui.md),
  [ADR 0026](./0026-design-system-and-ui-review.md),
  [ADR 0037](./0037-nvr-grid-per-pane-quality-and-full-screen.md),
  [ADR 0038](./0038-live-wall-controls-act-in-place.md),
  [live view pattern](../design/patterns.md#live-view),
  [live player island](../design/components.md#live-player-island),
  [status page pattern](../design/patterns.md#status-page),
  [field note](../notes/2026-11-28-compact-wall-controls.md)

## Context

The second field run on real hardware found that the per-pane overlay still read as a form: a
current-stream sentence, a text select, a hint and a camera select sat on top of the picture. It
also found that a click on the video did not dismiss the controls, and that installing an update from
the status page submitted a form and reloaded the page even though the card already had a live
progress region.

[ADR 0037](./0037-nvr-grid-per-pane-quality-and-full-screen.md) fixes the wall geometry, capacity and
player isolation. [ADR 0038](./0038-live-wall-controls-act-in-place.md) fixes per-pane ownership,
in-place layout changes and the named full-screen exits. The media, authentication, cookie and
capacity clauses are unchanged. This decision replaces the pane control anatomy and adds the two
missing in-place behaviours.

## Decision

**A pane overlay is a compact pictorial control surface, a click that starts on a shown video
dismisses the overlays without touching playback, and status update check and install answer inside
the update card.** The sections below are the parts of that decision.

### 1. The pane overlay is glyphs, not a form

An occupied multi-stream pane shows three native radio choices in one row: main stream, sub stream
and snapshots. Each is a 24 by 24 CSS pixel label whose visible content is one inline SVG glyph; the
accessible name is the stream name in visually hidden text. A small camera glyph covers the native
camera select, and its accessible name is "Camera for <name>". The current-stream line is one short
visible word, "Main", "Sub", "Still" or "One stream", with the hidden full sentence kept for assistive
technology.

The selected choice is marked by the checked native radio, a thicker border and a check glyph, so the
state does not depend on colour. The pane's current-stream word repeats the choice. A one-stream
camera shows one glyph and the reason, not three choices that do nothing.

The shown pane overlay takes at most 30 percent of a sixteen-pane cell at 360 by 800 and at 1280 by
800. The browser check measures the overlay rectangle against the pane rectangle at both widths. The
no-JavaScript path keeps a real form and a visible submit button; the script hides the submit and
applies each choice in place.

### 2. A click on a video dismisses the shown overlays

When an overlay is already shown, a click on a pane's video closes that pane's overlay and the wall
overlay. A click elsewhere on the wall closes the wall overlay. Clicking an empty pane opens its
camera chooser and does not dismiss it. A click that starts with the overlays hidden is a reveal, not
a dismissal, so a tap on the video still brings the controls up.

Dismissal only changes the overlay state. It does not navigate, reload, pause, play, mute or replace
the player, and the URL is unchanged. Escape keeps its existing behaviour: it leaves full screen, or
hides the overlays and returns focus to the selected pane.

### 3. Update check and install act in place

The update card's check and install forms are HTMX forms whose target is the whole update card and
whose swap is `outerHTML`. The handler answers an HTMX request with the card and no redirect, so the
preparing or downloading step and the progress region appear where the action stood. The URL does
not change and the page does not reload. A browser without JavaScript still posts the same form and
receives the existing redirect to status.

The audit of the other forms found no second in-place action that the card model already supports.
Test picture, discovery, camera add, save and remove, the wizard steps, login, setup, sign out and
the manual rollback confirmation either navigate to a different destination or deliberately render a
new page state. They stay ordinary submits. Live layout, quality and camera controls already act in
place under ADR 0038.

## Consequences

### Positive

- A pane's controls take one short row, so the picture keeps the pane at sixteen panes on a phone
  and on a desktop.
- The stream choice is legible as a picture, and the check, border and current word make the selected
  quality readable in greyscale.
- A click on the picture gets the controls out of the way without interrupting the stream.
- Installing an update no longer throws the owner back to the top of the status page or loses the
  card's progress.

### Negative / costs

- The camera glyph chooser relies on the native select menu, so the list is the platform's list
  rather than a styled one.
- The one-stream reason is hidden visually on the scripted live page and remains in the accessibility
  tree; the no-JavaScript form still shows it.
- The compact targets are 24 by 24, the hard floor for non-action controls. The browser check reports
  them as advisories below 44, not failures.
- The status install response rebuilds the card rather than only its live region. The card is the
  unit the owner sees, and the player and status disclosures are outside it.

## Alternatives considered

- **Keep the text select and shrink it.** Rejected: a native select with three words still reads as a
  form, and the option names do not fit a sixteen-pane phone cell.
- **Use one cycling button for main, sub and snapshot.** Rejected: the owner cannot see the other two
  choices, and a cycle is hard to reverse on touch.
- **Use colour for the selected glyph.** Rejected: the selected state must survive greyscale and the
  project forbids colour-only states.
- **Dismiss only on the video and leave the wall overlay up.** Rejected: the wall overlay is a second
  surface over the same picture, and a click on the wall is the natural way to clear it.
- **Reload the status page after install.** Rejected: the card already polls progress and the reload
  loses the owner's place, focus and the visible update context.

## References

- [ADR 0025: Browser check for the responsive UI](./0025-browser-check-for-the-responsive-ui.md)
- [ADR 0026: Design system and UI review](./0026-design-system-and-ui-review.md)
- [ADR 0037: The NVR grid, per-pane quality and full screen](./0037-nvr-grid-per-pane-quality-and-full-screen.md)
- [ADR 0038: Live wall controls act in place, per pane](./0038-live-wall-controls-act-in-place.md)
- [Live view pattern](../design/patterns.md#live-view)
- [Live player island](../design/components.md#live-player-island)
- [Design tokens](../design/tokens.md)
- [Wall compact controls note](../notes/2026-11-28-compact-wall-controls.md)
- [BIT-023](../plan/implementation-plan.md#bit-023-live-view)

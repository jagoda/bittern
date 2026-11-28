# Compact wall controls, click dismissal and in-place update actions

- Date: 2026-11-28
- Scope: [ADR 0039](../adr/0039-compact-pane-controls-and-in-place-status-actions.md),
  [ADR 0038](../adr/0038-live-wall-controls-act-in-place.md), `ui/`, `tests/browser/`

## What changed

Three field findings from the real-hardware wall run are fixed together:

- A pane overlay is now one current-stream word, three 24 by 24 stream glyphs and one 24 by 24
  camera glyph. The glyphs carry the accessible names Main stream, Sub stream and Snapshots, and the
  camera select carries "Camera for <name>". The selected choice has the checked radio, a thicker
  border and a check glyph. The visible current word is Main, Sub, Still or One stream; the hidden
  full sentence stays in the accessibility tree.
- A click that starts on a shown video dismisses that pane's overlay and the wall overlay. A click
  elsewhere on the wall dismisses the wall overlay. A click that starts with the overlays hidden
  still reveals them, and an empty pane opens its camera chooser instead of dismissing it.
- Check for updates and Install update answer with the whole update card on an HTMX request. The URL
  does not change, the page does not reload, and the preparing or downloading step and progress
  region appear in the card. Without JavaScript the same forms redirect to status as before.

## Area budget

The browser check asserts that the shown pane overlay is at most 30 percent of a sixteen-pane cell
at 360 by 800 and at 1280 by 800. The measured values were 26.05 percent at 360 (50 by 81.25 of
174.75 by 89.25 CSS pixels) and 26.49 percent at 1280 (50 by 311.25 of 184 by 319.25). The old text
select, hint and submit stack was capped at half the pane and usually reached the cap.

## Form audit

Converted to in-place HTMX with a card response:

| Control | Before | After |
| --- | --- | --- |
| Install update on status | POST, 303, full reload | HTMX POST, whole update card in place, no navigation |
| Check for updates on status | POST, 303, full reload | HTMX POST, whole update card in place, no navigation |

Left as ordinary submits because they navigate:

| Control | Destination or reason |
| --- | --- |
| Test picture on the camera list | Navigates to `/ui/cameras` with the test result and picture flash |
| Camera add, save, discovery and remove | Navigates to the camera list, edit form or discovery result |
| Wizard welcome, network, storage, camera continue, review apply | Each is a step to another page or the operation page |
| Login, setup and sign out | Authentication destinations, never a same-page action |
| Manual rollback confirmation | Navigates to status so the rollback progress is on its owning card |

Live layout, quality and camera controls already act in place under ADR 0038 and are unchanged in
their navigation behaviour.

## Screenshots read

The browser check wrote and the review read the shown and hidden wall overlay for the six layouts at
360 by 800, 390 by 844, 1280 by 800 and 1920 by 1080, the compact sixteen-pane overlay at 360 and
1280, the video-click dismissal, and the status install card at all four widths. The six layouts keep
the one pixel gap and the full-bleed wall. The compact pane overlay stays at the bottom of its pane,
the stream glyphs and check are legible in greyscale, and the hidden state is a clean picture. The
status install images show the updating badge, "Preparing the update", "Downloading the update" and
the card in place with no reload.

## Unfinished and next

- The wall-level overlay still takes the top of the wall on a phone. It is a separate surface from
  the pane overlay and the new click dismissal clears it, but a later pass could collapse the preview
  notice and camera state at narrow widths.
- The compact camera chooser uses the platform select menu. A future chooser could be a popover with
  the camera names, but the native menu is the smallest accessible control today.
- The 24 by 24 controls are the hard floor for non-action controls. The browser check reports them
  as advisory warnings below 44, not failures.
- BIT-027 still collects the real ONVIF profiles, so most field cameras keep the one-stream glyph
  until that lands.

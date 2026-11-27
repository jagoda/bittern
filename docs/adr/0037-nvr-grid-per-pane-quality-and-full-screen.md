# 0037. The NVR grid, per-pane stream quality, and full screen

*The live grid offers six layouts built from one area rule, fills the screen without per-pane chrome,
lets each pane choose main, sub or snapshot, plays only as many live panes as the device sustains,
and treats full screen as the chosen layout on the whole screen with an overlay for controls.*

- Status: Accepted; controls, layout change and full-screen exit superseded in part by [0038](./0038-live-wall-controls-act-in-place.md)
- Date: 2026-11-27
- Deciders: maintainer
- Related: [ADR 0005](./0005-dual-nic-isolated-camera-network.md),
  [ADR 0016](./0016-bittern-owns-frigate-config.md),
  [ADR 0019](./0019-ui-stack.md),
  [ADR 0022](./0022-community-functionality-ux-wins.md),
  [ADR 0024](./0024-onvif-discovery-levels.md),
  [ADR 0025](./0025-browser-check-for-the-responsive-ui.md),
  [ADR 0026](./0026-design-system-and-ui-review.md),
  [ADR 0034](./0034-live-view-owned-by-bittern.md),
  [ADR 0035](./0035-appliance-as-time-authority.md),
  [ADR 0036](./0036-update-bundles-deliver-subsystem-changes.md),
  [architecture](../architecture.md), [open questions](../open-questions.md),
  [implementation plan](../plan/implementation-plan.md), [schema README](../../schema/README.md),
  [renderer](../../agent/internal/render/frigate.go), [ONVIF client](../../agent/internal/onvif/client.go),
  [UI README](../../ui/README.md), [live view pattern](../design/patterns.md#live-view),
  [live player island](../design/components.md#live-player-island),
  [BIT-023](../plan/implementation-plan.md#bit-023-live-view),
  [BIT-024](../plan/implementation-plan.md#bit-024-event-list-and-review),
  [BIT-025](../plan/implementation-plan.md#bit-025-playback-and-recording-timeline),
  [BIT-027](../plan/implementation-plan.md#bit-027-robust-onvif-profiles-ptz-and-field-interoperability)

## Context

The live view ships as a grid. The preset set is one, two, four and nine panes
(`agent/internal/ui/live_layout.go:28`), the session is capped at nine tiles (`:24`), the proxy hard
stops at nine media connections (`agent/internal/ui/live_proxy.go:24`), and the player applies a one,
four or nine ceiling by viewport width (`ui/static/live-player.js:26-30`, `:63-68`). Each pane is a
card: the player carries a border, a radius, sixteen pixels of padding and a surface
(`ui/static/bittern.css:1344-1350`), the media box carries its own border and radius (`:1352-1359`),
and the grid gap is sixteen pixels (`:1479-1487`). Each tile renders a heading, a state badge, a
status region, a retry action, full-screen and full-width controls and a camera picker
(`ui/templates/live.html:25-46`). The layout picker is a disclosure above the grid (`:94-99`). Full
screen is requested on one player root, switches that tile to the main profile, and leaves every
other tile out (`ui/static/live-player.js:407-424`, `:149-186`).

The schematic has one stream per camera. A camera stores `rtsp_port` and `rtsp_path`
(`schema/appliance.schema.json:225-236`, `agent/internal/config/types.go:73-104`) and no profiles,
and the document version is 2 (`schema/appliance.schema.json:18-21`). The renderer emits a `main` and
a `sub` go2rtc key for every camera, but both point at that one path
(`agent/internal/render/frigate.go:545-552`). The ONVIF client is a Device service client: clock,
capabilities, NTP and manual time (`agent/internal/onvif/client.go:136-244`). It has no Media service,
so it cannot enumerate profiles or stream URIs today, and the device capability read at `:175`
(`GetServiceCapabilities`) returns no service endpoints, so there is no Media address to call.

The design documents described the same card grid: the live pattern fixed presets at one, two, four or
nine, and the live player island put the heading, status, controls and picker in the tile
(`docs/design/patterns.md`, LV9 and LV12, and `docs/design/components.md`, before this change). This
ADR updates both documents in the same change.

The product job has moved past that shape. The owner wants a wall that looks like an NVR: six or
eight panes with one larger view, sixteen panes on a large screen, each pane at the quality the device
can sustain, and a full-screen mode that keeps the wall. The card chrome that made a one-camera page
readable takes a large share of a small pane at sixteen, and the nine-connection ceiling was chosen
before any measurement, so a capable desktop is held back while an overloaded phone is not detected.

The constraints from [ADR 0034](./0034-live-view-owned-by-bittern.md) still hold and this decision
does not reopen them:

- The live page is a Bittern page on the authenticated 443 listener, using go2rtc MSE with HLS
  fallback through `bitternd`. WebRTC, 8555, 1984, 5000, 8554 and 8971 stay off `lan0` and the
  tailnet. The browser receives no camera address, credential, stream name or upstream URL.
- The player is a stable node outside every HTMX swap region, and a poll never replaces it.
- Snapshots come from Frigate's loopback `latest.jpg`; media responses are authenticated and
  `no-store`.
- The UI stack is server-rendered Go templates with HTMX, works without JavaScript, and every page
  is checked at 360, 390, 1280 and 1920 CSS pixels under
  [ADR 0025](./0025-browser-check-for-the-responsive-ui.md) and
  [ADR 0026](./0026-design-system-and-ui-review.md).
- Cameras live on `cam0`; the appliance does not forward, NAT or proxy camera traffic from `lan0`.

The upstream facts that decide the details are in the table below. The Frigate facts were verified
against the pinned 0.18.2 tree on 2026-11-26 and the same pages were re-read against the 0.18.3 pin on
2026-11-27; the pin is 0.18.3 per [the pin note](../notes/2026-11-26-frigate-0.18.3-pin.md). The 0.18.3
release notes name no live view regression and the pages read the same.

| Fact | Source (accessed 2026-11-27) |
| --- | --- |
| Frigate maps a friendly live name to a go2rtc stream key under `cameras.<name>.live.streams`, the first entry is the default, and the choice is per device in browser local storage. Streams are named keys under `go2rtc.streams`; there are no default `main` and `sub` names. | <https://github.com/blakeblackshear/frigate/blob/v0.18.3/docs/docs/configuration/live.md> |
| Frigate's restream example gives the main stream the `record` role and the sub stream the `detect` role, and Bittern's detection roles are not changed by this decision. | <https://github.com/blakeblackshear/frigate/blob/v0.18.3/docs/docs/configuration/restream.md> |
| ONVIF Media defines `GetProfiles`, `GetStreamUri` and `GetSnapshotUri`, each addressed by an opaque profile token, and `GetProfiles` returns the profiles the device is configured with. | <https://www.onvif.org/ver10/media/wsdl/media.wsdl> |
| `HTMLVideoElement.getVideoPlaybackQuality()` returns a `VideoPlaybackQuality` whose `droppedVideoFrames` and `totalVideoFrames` counters are cumulative since the video element was created, so a window rate is a delta of two reads. | <https://developer.mozilla.org/en-US/docs/Web/API/HTMLVideoElement/getVideoPlaybackQuality>, <https://developer.mozilla.org/en-US/docs/Web/API/VideoPlaybackQuality> |
| The Fullscreen API requires transient user activation to enter, exposes `document.fullscreenElement`, and the browser exits on its own exit key, usually Escape. | <https://developer.mozilla.org/en-US/docs/Web/API/Fullscreen_API>, <https://developer.mozilla.org/en-US/docs/Web/API/Fullscreen_API/Guide> |
| `grid-template-areas` names a rectangular area over a grid of cells, and every repeated name must form a rectangle. | <https://developer.mozilla.org/en-US/docs/Web/CSS/grid-template-areas> |
| The Page Visibility API reports `document.hidden` and fires `visibilitychange`, which is the correct hook to stop work in a background tab. | <https://developer.mozilla.org/en-US/docs/Web/API/Page_Visibility_API> |

## Decision

**The live grid is a full-bleed layout of one, four, six, eight, nine or sixteen panes, built from one
area rule, with no per-pane chrome; each pane chooses main, sub or snapshot, sub is the automatic
choice in any multi-pane layout and main in the single-pane layout, a class prior plus a dropped-frame
signal sets how many automatic panes stay live, panes beyond that capacity show snapshots, and full
screen is the current layout on the whole screen with an overlay for controls.** The sections below
are the parts of that one decision.

### 1. Six layouts, one area rule

The layout set is `1`, `4`, `6`, `8`, `9` and `16`. The value `auto` is not a layout: it is the
absence of a stored choice and resolves to the smallest layout in the set that holds every enabled
camera, capped at 16. A two- or three-camera auto layout is therefore the four-pane layout with empty
panes, and a five-camera auto layout is six with one empty pane; the empty pane shows its Choose
camera control. More than sixteen enabled cameras cannot be shown at once; the page says how many are
not in the wall and the owner chooses which ones to assign. A stored value outside the set clamps down
to the largest layout at or below it (`2` becomes `1`, `3` becomes `1`, `5` becomes `4`, `7` becomes
`6`, `10` to `15` become `9`), and an unreadable value becomes `auto`. The viewport no longer chooses
the layout. It chooses the live capacity, and the capacity only lowers the number of live panes
(section 4).

Every layout is one function over a grid of areas, not six hand-written layouts:

- Let `c` be the square grid side. For `1`, `4`, `9` and `16`, `c` is the square root and every pane
  is one cell.
- For `6` and `8`, `c` is half the pane count (`3` and `4`). Pane 1 is the rectangle of the first
  `c - 1` rows and the first `c - 1` columns. The next `c - 1` panes are the cells of the last
  column below the primary, top to bottom. The last `c` panes are the cells of the last row, left to
  right. The pane count is `1 + (c - 1) + c`, which is `2c`, so the rule produces exactly six or
  eight panes.

Pane order is reading order: the primary first, then the right column, then the bottom row. The
areas for six, on a three by three cell grid:

```text
+-------+-------+-------+
|               |       |
|       P1      |  P2   |
|               |       |
|               +-------+
|               |       |
|               |  P3   |
|               |       |
+-------+-------+-------+
|  P4   |  P5   |  P6   |
+-------+-------+-------+
```

The areas for eight, on a four by four cell grid:

```text
+-------+-------+-------+-------+
|                       |       |
|          P1           |  P2   |
|                       |       |
|                       +-------+
|                       |       |
|                       |  P3   |
|                       +-------+
|                       |       |
|                       |  P4   |
+-------+-------+-------+-------+
|  P5   |  P6   |  P7   |  P8   |
+-------+-------+-------+-------+
```

The primary takes four of nine cells in six (`44.4%`) and nine of sixteen in eight (`56.25%`). Every
secondary takes one cell (`11.1%` and `6.25%`). The primary is the largest area, not a different
stream: it follows the same automatic quality as the layout until the owner changes that pane.

The server resolves the pane count and the camera assignment, and renders a `grid-template-areas`
value and one area class per pane. The areas are data derived from the count. A new count is a new
row in the function, not a new template (`docs/design/patterns.md:250`, LV9, is updated to this rule).
The layout cookie keeps its name and flags. It stores the layout, the camera IDs and one quality
value per pane (section 3); it still stores no address, credential, stream name or owner name, and
keeps the `Path=/ui/live`, `Secure`, `HttpOnly`, `SameSite=Strict` and one-year settings
([ADR 0034](./0034-live-view-owned-by-bittern.md), section 4; `agent/internal/ui/live_layout.go:20-21`,
`:166-173`).

### 2. The grid fills the view, with no per-pane chrome

The grid fills the whole content region: from below the page header to the bottom of the viewport, on
both axes, with no page scroll on the grid route. The page keeps its one `h1` and the skip link. No
lede, notice, card, border, padding, toolbar or control row sits between the header and the grid or
between panes; the only separation is the one pixel gap described next. The layout picker and every
other control live in the overlay (sections 7 and 9).

Panes touch except for a one CSS pixel gap, the existing `--border-width` token. The gap shows the
page background. A pane has no border, no radius, no padding, no surface and no card. The video and a
snapshot are contained inside the pane and letterboxed with `object-fit: contain`, which is the
current behaviour (`ui/static/bittern.css:1367`, `:1374`); the picture is never cropped to fill.

The pane is a `figure`-like box that holds the video, the snapshot and the overlay. The overlay is a
sibling of the player island, inside the pane and above the video. A pane with no camera assigned
shows one control, "Choose camera", centered; it is not chrome around a video because there is no
video. A pane in a named failure state keeps its camera name and one sentence on the snapshot until
the stream recovers or the owner changes the pane; that is content, not a card. Every other pane at
rest is pure video.

### 3. Per-pane quality: sub in a multi-pane layout, main in the single-pane layout

The automatic quality of a live pane is:

| Layout | Automatic quality |
| --- | --- |
| `1` | Main stream |
| `4`, `6`, `8`, `9`, `16` | Sub stream |

Every pane has a three-way quality control: main, sub, snapshot. The control is in the pane's overlay
for the selected pane, names the quality that is playing, and changes the stream at runtime by
closing the current media connection and opening the same authenticated proxy route with the other
profile. The page does not reload and the player island is not swapped. The pane shows a short
"Switching to main stream", "Switching to sub stream" or "Showing snapshots" state and resumes.

A manual choice is stored per pane and per device in the layout cookie, in the same order as the
camera IDs, and is rendered by the server on the next request. It wins over automation without
exception:

- A manual `main` or `sub` pane is never demoted to snapshot by the capacity rules.
- A manual `snapshot` pane is never promoted to a live stream.
- A manual quality is never switched to the other live quality by automation.
- Automatic panes fill the remaining capacity around manual live panes. The live count can therefore
  exceed the class prior when the owner forces it; the overlay says the wall is above the tested
  capacity and does not refuse the choice.

When a camera has one profile, or the schema has no profiles yet, the control is absent rather than
offering a false choice and the pane says "one stream" in the same place. The fallback rules from
[ADR 0034](./0034-live-view-owned-by-bittern.md) section 5 still apply: if a requested profile is
unavailable the player uses the other one and labels the fallback.

### 4. Capacity is a class prior plus a dropped-frame signal

Capacity is the number of automatic panes allowed to hold a live media connection in the session. It
starts at the class prior, never rises above it, and is lowered and restored by the rules below.
Manual live panes are outside the capacity count and are always admitted.

| Device class | Viewport width | Capacity prior | Layouts that play fully |
| --- | --- | --- | --- |
| Phone | below 44rem (704 CSS pixels) | 4 | 1 and 4 |
| Tablet and small laptop | 44rem to below 80rem (704 to 1279 CSS pixels) | 8 | 1, 4, 6 and 8 |
| Desktop | 80rem (1280 CSS pixels) and above | 16 | all six |

The widths and the three classes are the ones in [ADR 0034](./0034-live-view-owned-by-bittern.md)
section 7. The numbers change from 1, 4 and 9 to 4, 8 and 16. The proxy hard maximum becomes 16,
because 16 is the largest legal layout (`agent/internal/ui/live_proxy.go:24` changes with
BIT-023). The proxy enforces its own maximum; the page enforces the prior first.

The signal is the browser's own drop counter. Every two seconds, for each live pane, the player reads
`video.getVideoPlaybackQuality()` and keeps the last six samples, which cover ten seconds. For a
window it computes `d = dropped(t) - dropped(t - 10s)`, `n = total(t) - total(t - 10s)` and
`rate = d / n` when `n` is positive. A pane is **hot** when its rate is above 2 percent in the current
window and in the previous window, which is twenty seconds of evidence. `totalVideoFrames` includes
dropped frames, so the denominator is the total the browser created. If the API is absent, automatic
demotion is disabled, the prior holds, and the proxy maximum still applies. A hot manual pane counts
as evidence that the device is overloaded even though it is never the demotion target.

Demotion and promotion are precise enough to test:

1. **Step down before a pane dies.** A hot automatic pane on the main stream switches to the sub
   stream first and resets its windows. This can only happen in the one-pane layout, because every
   multi-pane layout starts on sub. It stays live and keeps its capacity slot, and the overlay says
   "Sub stream, reduced after dropped frames".
2. **Demote the least visible automatic pane.** When at least one live pane is hot, the session
   demotes one automatic live pane to snapshot: the pane with the smallest rendered area from
   `getBoundingClientRect()`, with ties broken by reading order from the last pane. The primary pane
   is the largest area, so it is demoted last. Capacity decreases by one. One demotion happens per
   twenty-second window and the evidence resets after each demotion, so a wall steps down one pane at
   a time.
3. **Never demote a manual pane.** Manual main and sub panes are skipped when the target is chosen. If
   only manual live panes remain, no demotion happens and the page keeps their choice.
4. **The last automatic pane.** If the only automatic live pane stays hot after the step-down and
   another twenty seconds, it is demoted to snapshot and the pane says the device cannot play this
   camera live. A manual pane in that position is left alone.
5. **Promote slowly.** Promotion is allowed only when 300 seconds have passed since the last demotion
   and every live pane, automatic or manual, has a rate below 0.5 percent across a 60-second window.
   Then one capacity-demoted automatic snapshot pane is promoted to the layout's automatic quality:
   the most visible one by rendered area, ties broken by reading order from the first pane. One
   promotion per 60 seconds, never above the prior. A manual snapshot is never promoted.
6. **Hysteresis.** The demotion threshold (2 percent) is four times the promotion threshold
   (0.5 percent); demotion needs 20 seconds of evidence, promotion needs 60 seconds of evidence plus
   300 seconds of quiet, and each direction moves at most one pane per interval. A wall cannot
   oscillate faster than one demotion per 20 seconds or one promotion per 60 seconds, and the state
   after a sequence of windows is deterministic.
7. **Viewport changes.** When the viewport crosses down into a smaller class, the capacity clamp is
   immediate: automatic panes are demoted in the same least-visible order until the automatic live
   count fits the new prior or no automatic pane remains, and the evidence resets. When it crosses up,
   the cap rises to the new prior but no pane starts until the promotion conditions are met, so
   dragging a window does not open a burst of decoders.
8. **Visibility and reload.** Sampling stops while `document.hidden` is true and the windows reset
   when the page becomes visible. A layout change or a page reload resets the capacity to the prior
   and the evidence to empty. The capacity is session state and is not stored.

The page exposes the class prior, the current live count and the current capacity as data attributes
for the browser check (`data-device-prior`, `data-live-count`, `data-live-capacity`).

### 5. Panes beyond capacity show snapshots on an interval

A pane that is not live, because capacity is lower than the layout, shows the latest JPEG from the
existing authenticated snapshot route (`/ui/live/media/<camera>/<profile>/snapshot.jpg`, backed by
Frigate's loopback `latest.jpg`). It refreshes every 15 seconds while the tab is visible. The
interval is the one the page already uses for camera state (`ui/templates/live.html:76`, `:124`), so
the wall has one cadence rather than a second timer family. A still is a presence check, not motion:
four refreshes a minute is enough to see whether a person or a vehicle is still there, and a shorter
interval multiplies requests without adding information. Refreshing stops while `document.hidden` is
true and resumes with one immediate fetch when the tab becomes visible.

The cost is bounded and small. A substream JPEG is assumed to be 60 KB. Twelve snapshot panes at one
fetch per 15 seconds is `12 * 60 KB / 15 s = 48 KB/s`, which is `384 kbit/s` and `0.8` requests per
second at the daemon. Sixteen snapshot panes is `64 KB/s`, `512 kbit/s` and `1.07` requests per
second. That is about 4.8 percent of the 8 Mbit/s a sixteen-pane live wall costs (see the arithmetic
in the consequences), and it does not grow with the live connections. A snapshot pane that fails to
fetch
keeps its last image, names the failure in the overlay, and does not retry faster than the interval.
An offline camera stops refreshing until the owner retries.

### 6. Full screen is the layout, not one pane

Full screen requests `requestFullscreen()` on the grid container from a control in the overlay, and
the current layout fills the whole screen in the same area arrangement. Entering full screen never
changes the layout, never switches a pane to main, and never hides the other panes. A wall of six
stays six; a wall of sixteen stays sixteen.

The full-screen element has no page header, no title, no toolbar, no border and no padding. It is
`100vw` by `100vh`, margin and padding are zero, the layout's `grid-template-areas` and the one pixel
gap are unchanged, and the page behind does not scroll. The only background is the colour of the one
pixel gap between panes. The overlay is the only control surface, and it is inside the panes
(section 7).

The exit paths are the browser's own, which the browser always offers: Escape, the browser exit
gesture, and the overlay's "Exit full screen" control. Leaving full screen leaves the page, the
layout, the selected pane, the quality choices and every live connection in place. The browser
requires a user gesture to enter full screen, so Bittern never re-enters automatically after a
reload.

A layout change is a form submission and a page load, as it is in
[ADR 0034](./0034-live-view-owned-by-bittern.md) section 4. The browser leaves full screen on that
navigation. The chosen layout is stored before the redirect, so the page returns in the layout the
owner picked; the owner enters full screen again with one press. Bittern does not rebuild players in
place to fake a layout change inside full screen, because that would tear down and recreate media
connections and break the player isolation rule.

### 7. Controls in full screen are overlays, hidden after three seconds

The overlay contains the layout picker (six options plus automatic) on the grid route, the selected
pane's camera name, its three-way quality control, and "Exit full screen". It is shown on pointer
movement, a key press, a tap or focus entering one of its controls. It hides after 3000 ms with no such event and no focus
inside it. The period is three seconds: long enough to move between the quality control and the
layout picker without it vanishing, short enough that a parked pointer leaves the video unobstructed.
The overlay is shown for three seconds when full screen is entered.

The hidden state is `data-live-overlay="hidden"` on the overlay element, with `opacity: 0`,
`pointer-events: none`, `visibility: hidden` and the `inert` attribute. `inert` removes the controls
from the focus order and the accessibility tree, so a hidden overlay cannot take focus and cannot
intercept a click meant for the video. The video and the pane below it receive that click.

The keyboard rules are part of the overlay contract:

- Tab while the overlay is hidden reveals it and moves focus to the selected pane's quality control,
  before the browser's default Tab move. Shift+Tab does the same with focus on the exit control.
- Tab while the overlay is visible follows DOM order: quality control, layout group, exit control,
  then back to the pane wall. Shift+Tab reverses.
- Focus inside the overlay suspends hiding. When focus leaves, the three-second timer starts again.
- Escape exits full screen, visible overlay or not. Outside full screen, Escape returns focus to the
  selected pane and hides the overlay.
- A tap on the video when the overlay is hidden shows the overlay and selects that pane. It does not
  pause, play or mute anything; live video is muted and has no transport controls in v1.

The overlay is not a hover-only action and does not violate the design system rule against menus that
appear only on `:hover` (`docs/design/components.md:683`, `:702-703`): it is revealed by any pointer
movement, a key press or a tap, it is reachable by keyboard, and it is never the only path to a
required control.

Without JavaScript the overlay is visible by default, because only the script sets the hidden state.
The layout picker is a real form with six submit buttons and the quality control is a real form with
three submit buttons, so both work on the no-JavaScript path with a page load.

### 8. Camera names are content

Every pane carries a small camera name in its overlay layer, top left, inset by `--space-8`. The name
is text on a short translucent scrim the length of the text, not a bar, badge or card; it fades with
the overlay and takes no space from the video. It is allowed to fade because it is content, not
chrome: it identifies the pane when the controls are up and it never becomes a header above the
video. An operator must be able to tell similar scenes apart, so the name is present in every layout,
including the single-pane layout and full screen.

A pane in a named failure state keeps its name and one sentence persistently, with no scrim beyond the
text, until it recovers or the owner changes the pane. This keeps the failure state visible without a
status card (`docs/design/patterns.md:246`, LV5, still applies).

### 9. Keyboard, focus and the selected pane

The pane wall is one tab stop with a roving tabindex. The first Tab into the wall focuses the first
pane, or the primary pane in six and eight. Arrow keys move focus to the nearest pane in that
direction by the centre points of the rendered areas, Home goes to the first pane and End to the last.
Each pane is a labelled group whose accessible name is the camera name; its video has the label "Live
video from <name>".

The selected pane is the focused pane. There is exactly one. A pointer press on a pane selects it
and moves focus to it, and the arrow keys move the selection. It is shown in three ways that do not
add a border between panes:

- The one focus treatment is drawn inset inside the pane, by the ring width, so the three-pixel
  `--focus-ring` stays inside the pane and never reads as a tile border.
- The overlay's selected-pane group prints the camera name and its current quality beside the
  quality control.
- The pane's state changes are announced in a polite live region inside the pane's overlay, so sixteen
  panes do not announce at once.

The focus ring is the existing token (`docs/design/tokens.md:129-132`). If the four-width check finds
it below 3:1 against the test video, the fix is a token-level change recorded in
`docs/design/tokens.md`, not a per-pane border or a second focus treatment.

A layout is chosen by keyboard with the native radio group in the overlay: Tab reaches the group,
arrow keys move the selection, and the change submits the layout form. Without JavaScript the same six
options are submit buttons with `aria-pressed`, so the choice does not depend on the radio behaviour.
The selected option is the stored layout, and `auto` is a seventh option that resolves at render time.
Every control meets the target sizes in [OQ-26](../open-questions.md#oq-26).

### 10. Scope and supersession

This decision supersedes these parts of [ADR 0034](./0034-live-view-owned-by-bittern.md), and only
these parts:

| Superseded clause | Replacement |
| --- | --- |
| Section 4: the preset values `auto`, `1`, `2`, `4` and `9`, the cap at nine, the viewport-driven column count, and the clamp of a stored preset to the device ceiling. | Sections 1 and 4 here: six layouts, auto from the inventory, the stored layout is kept, and capacity demotes panes to snapshots. |
| Section 4: the cookie contains only the preset and the camera IDs. | Section 3 here: the same cookie also carries one quality value per pane. |
| Section 4 and section 5 as implemented, and the design pattern's tile rules: the per-pane heading, badge, status block, controls and picker as tile chrome. | Sections 2, 7 and 8 here: the grid fills the view, controls and the name are overlays. |
| Section 5: grid tiles use sub, the single-camera and fullscreen views use main, and entering fullscreen switches one tile to main. | Sections 3 and 6 here: sub in every multi-pane layout, main in the single-pane layout, per-pane manual quality, and full screen keeps the layout and the pane qualities. |
| Section 7: the class table of one, four and nine active tiles, and the hard maximum of nine media connections. | Section 4 here: priors of four, eight and sixteen with runtime demotion and a hard maximum of sixteen. |

The player isolation rule, the MSE and HLS transports, the snapshot fallback, the authentication and
`no-store` boundary, the CSP, the muted player, the no-WebRTC rule and the no-new-listener rule are
unchanged. PTZ ([BIT-027](../plan/implementation-plan.md#bit-027-robust-onvif-profiles-ptz-and-field-interoperability)),
audio ([OQ-21](../open-questions.md)), event review ([BIT-024](../plan/implementation-plan.md#bit-024-event-list-and-review)),
recorded playback ([BIT-025](../plan/implementation-plan.md#bit-025-playback-and-recording-timeline))
and the kiosk ([OQ-25](../open-questions.md)) remain out of scope.

## Consequences

### Schema: each camera carries enumerated main and sub profiles

Schema version 3 adds an optional `profiles` object to a camera:

```yaml
cameras:
  - id: driveway
    name: Driveway
    address: 10.77.0.21
    rtsp_port: 554
    rtsp_path: /Streaming/Channels/101
    profiles:
      main:
        profile_token: Profile_1
        rtsp_path: /Streaming/Channels/101
        snapshot_path: /onvif-http/snapshot?Profile_1
        width: 1920
        height: 1080
        codec: h264
      sub:
        profile_token: Profile_2
        rtsp_path: /Streaming/Channels/102
        width: 640
        height: 360
        codec: h264
```

The rules are:

- `profiles` is optional. `main` and `sub` are each optional. An absent `profiles` object, or an
  absent `sub`, means the single configured path is the only stream and both names resolve to it.
- A profile stores the ONVIF profile token, the path and any query the camera requires, and the
  reported width, height and codec. It never stores a full URI, a host, a user, a password or a
  credential in a query. The existing `credentials_ref` and the renderer's environment placeholder
  supply the credential (`agent/internal/render/frigate.go:567-574`). The schema rejects anything that
  is not a path beginning with `/`, rejects `://` and `@`, and caps every string. The camera-level
  `rtsp_path` remains the manual or fallback path, and a profile path takes precedence when present.
- `codec` is informational for the UI and the live support check. H.264 remains the only supported
  live codec ([ADR 0034](./0034-live-view-owned-by-bittern.md) section 2); an H.265-only camera keeps
  the named unsupported state and the snapshot.
- The resolution and codec are what the camera reports, not what Bittern assumes. A camera that does
  not report them stores the token and path and leaves the fields empty.
- The mapping into `main` and `sub` is a Bittern choice over the advertised profiles, recorded with
  the evidence: `main` is the advertised H.264 profile with the largest reported pixel count and
  `sub` is the advertised H.264 profile with the smallest pixel count that is strictly smaller than
  `main`. If no profile is strictly smaller, `sub` is absent and the camera has one stream. Profile
  order and token names are never used to infer quality, which is the rule
  [BIT-027](../plan/implementation-plan.md#bit-027-robust-onvif-profiles-ptz-and-field-interoperability)
  already carries.

The collection happens at adoption through the ONVIF Media service: `GetProfiles` for the tokens and
the attached video encoder configuration, `GetStreamUri` for each profile's RTSP path, and
`GetSnapshotUri` where the camera supports it. The current client is Device-only
(`agent/internal/onvif/client.go:136-244`); this extends it with the Media endpoint from
`GetCapabilities` and the three operations, behind the same endpoint validation, timeout and
operation budget. A camera without ONVIF, without a Media service, or with an unreadable profile keeps
the manual RTSP path and adopts normally, as
[ADR 0024](./0024-onvif-discovery-levels.md) requires. A URI returned by the camera is reduced to its
path and query and validated against the camera's own address and port; a URI with userinfo, another
host or another scheme is discarded and recorded as evidence.

The migration is version 2 to version 3. It is deterministic, performs no I/O, does not mutate its
input, and changes nothing except `schema_version`: a version 2 camera has no `profiles` key, so the
version 3 document has none either and the single-stream degradation is the migrated state. The
fixture pair lives under `schema/migrations/v2_to_v3/` and the Go migration mirrors it, following the
v1 to v2 pattern ([migrations README](../../schema/migrations/README.md)). Existing devices reach
version 3 through the update transaction's migration step and its persisted pending record
([ADR 0036](./0036-update-bundles-deliver-subsystem-changes.md)), not through a manual edit. The
migration has a golden test for a document with a camera, a document with camera credentials and a
document with no cameras; a version 3 document is unchanged. Profiles are collected at adoption, not
by the migration.

### Renderer: both streams per camera

The renderer emits `go2rtc.streams.<id>_main` and `go2rtc.streams.<id>_sub` from the two paths, and
maps both friendly names in `cameras.<id>.live.streams`. Without profiles it keeps today's output:
both keys point at the one configured path (`agent/internal/render/frigate.go:545-552`), so the page,
the proxy and the player behave exactly as they do now.

The renderer does not change the Frigate input roles. One input keeps `detect` and `record` on the
main path (the camera-level path until a main profile exists), because moving detection to the sub
stream changes detection and recording behaviour and needs its own measured decision. The upstream
restream example does put `detect` on the sub stream, and the follow-up below records that as the
next candidate change, not a silent part of this one. The goldens gain a profile camera and a
no-profile camera, and the `cameras.<name>.live.streams` order keeps the main entry first, matching
the current output and Frigate's documented default.

### Degradation until profiles exist

Until BIT-027 collects real profiles, every camera has no `profiles` object. The schema migration is a
version bump, the renderer still emits one path under both keys, the live page says "one stream", the
per-pane quality control is absent, and the single-pane route behaves as it does today. The new
layout, chrome, capacity and full-screen rules work without profiles because they only need the
layout, the camera inventory and the existing snapshot route. This is the explicit degradation: no
camera is rejected, no page fails, and a device that updates before BIT-027 sees the new wall with
the same one-stream player.

### Decode and bandwidth arithmetic for the capacity numbers

The prior is a planning estimate, not a measurement. It assumes a 640 by 360, 15 frame per second
H.264 substream at 0.5 Mbit/s and a 1920 by 1080, 15 frame per second H.264 main stream at 4 Mbit/s.
The pixel rate per pane is width times height times frames per second:

| Stream | Pixels per frame | Pixel rate at 15 fps | Assumed bitrate |
| --- | --- | --- | --- |
| Sub, 640 by 360 | 230,400 | 3,456,000 px/s (3.46 Mpx/s) | 0.5 Mbit/s |
| Main, 1920 by 1080 | 2,073,600 | 31,104,000 px/s (31.1 Mpx/s) | 4 Mbit/s |

One main stream is exactly nine times a sub stream in pixels (`2,073,600 / 230,400 = 9`). The class
priors then cost:

| Class | Prior | Pixel rate | Bandwidth |
| --- | --- | --- | --- |
| Phone | 4 | `4 * 3.46 = 13.8 Mpx/s` | `4 * 0.5 = 2 Mbit/s` |
| Tablet and small laptop | 8 | `8 * 3.46 = 27.6 Mpx/s` | `8 * 0.5 = 4 Mbit/s` |
| Desktop | 16 | `16 * 3.46 = 55.3 Mpx/s` | `16 * 0.5 = 8 Mbit/s` |

For scale, one 4K frame at 60 frames per second is `3840 * 2160 * 60 = 497.7 Mpx/s`. The desktop
prior is 11.1 percent of that decode rate; the phone prior is 2.8 percent. One main pane at 31.1
Mpx/s is 6.25 percent of the same 4K60 rate, and a sixteen-pane main wall would be
`16 * 31.1 = 497.7 Mpx/s`, the whole 4K60 budget, at `16 * 4 = 64 Mbit/s`. That is why main is the
single-pane default and sub is the multi-pane default, not a matter of taste. Eight Mbit/s is 0.8
percent of a 1 Gbit/s LAN, but 40 percent of a 20 Mbit/s remote uplink, so the default data flow
stays local and the remote path keeps its own decision.

The pixel rate alone says a modern phone can decode far more than four substreams. The prior is not a
decode limit; it is the number of panes at which the per-pane browser pipeline (one WebSocket, one
MediaSource, one SourceBuffer, one compositing layer) plus the phone's thermal budget is still a
guarantee rather than a hope. The runtime signal lowers the count when the device disagrees, and the
manual control lets the owner exceed it with a warning. The measured capacity of the bench devices is
[OQ-44](../open-questions.md#oq-44); the priors are revised when that evidence exists.

### Review and playback surfaces

[BIT-024](../plan/implementation-plan.md#bit-024-event-list-and-review) and
[BIT-025](../plan/implementation-plan.md#bit-025-playback-and-recording-timeline) do not adopt the
pane model. The work here adds no grid, capacity, per-pane quality or overlay to review or playback:

- Event review keeps its list, filters, thumbnails and one detail player. Thumbnails stay the snapshot
  and event JPEGs the Frigate loopback API already returns. There is no live capacity in a list.
- Recorded playback is one recorded stream in one player. Frigate records the input it is given, so
  there is no main and sub to toggle in playback. The schema's profile tokens let a later surface
  name the stream that was recorded, and a future decision could record continuous on sub and alerts
  on main, but that is not this decision.
- The pane quality control is a live-only control. It must not appear on review or playback, because
  a recorded file has no live profile to switch.
- Review and playback inherit the schema and renderer changes without depending on them: an old
  document migrates, a camera without profiles still records and plays.

### Positive

- The owner gets the wall an NVR is expected to have: six and eight with one larger view, sixteen on a
  large screen, and one, four and nine as the equal-area cases. The area rule makes the six layouts
  consistent and one testable function instead of six templates.
- Removing the card chrome gives the video the area the cards were using and makes sixteen panes
  legible at 1920 and above. The one pixel gap keeps panes distinct without a border.
- Per-pane quality means a phone grid does not pay for main streams by default, a single camera is
  sharp by default, and the owner can override either choice per pane.
- Capacity is evidence-based: a capable device is not held to a guessed ceiling, and a struggling
  device loses panes one at a time with visible hysteresis instead of stuttering or collapsing.
- Snapshot panes cost about 0.4 to 0.5 Mbit/s at a full demoted wall, so an overloaded device still
  shows every camera.
- Full screen keeps the wall the owner built, which is the point of a wall, and the overlay keeps the
  controls reachable without a permanent toolbar.
- The keyboard model, the inset focus ring and the text name in the overlay give an operator a way to
  identify and control a pane without a border.

### Negative / costs

- Sixteen live panes is more than the old nine. The prior and the dropped-frame signal bound it, but a
  low-end laptop that reports no drop counter will keep the prior and may struggle. The signal is only
  as good as the browser's counter ([OQ-46](../open-questions.md#oq-46)).
- The overlay is a new state machine: three timers, focus management, `inert`, and full-screen events.
  A bug can hide the only control or intercept a click. The browser check must cover the hidden,
  visible, focused, full-screen and no-JavaScript states.
- Removing per-pane chrome removes the always-visible status and retry. Failures now appear in the
  overlay or as the persistent failure sentence on the snapshot, which is less discoverable than a
  badge on every tile.
- The layout cookie grows by one quality value per pane. It stays a few hundred bytes, but it is
  owner state in a cookie and is lost in private browsing or on a cookie clear.
- A layout change is still a page load and reconnects every player; inside full screen it also leaves
  full screen. The owner enters full screen again. This is the cost of keeping the player isolation
  rule instead of rebuilding players in place.
- Snapshot panes are stills. Motion between refreshes is missed, and a demoted pane is not evidence
  that nothing happened.
- The schema, renderer, ONVIF and migration work spans BIT-023 and BIT-027. Until BIT-027 lands, the
  feature is the one-stream degradation and the quality control is absent.
- The design documents now describe a wall that the code does not implement yet. The rules are the
  review target until BIT-023 lands, which is the same status the live pattern already carries.

### Follow-ups

- [BIT-023](../plan/implementation-plan.md#bit-023-live-view) implements the six layouts and the area
  function, the full-bleed wall and the chrome removal, the per-pane quality control and cookie
  field, the class priors and the demotion and promotion state machine, the snapshot interval, full
  screen of the layout, the overlay and its keyboard model, the focus ring and the selected-pane
  text. It adds the schema version 3 fields with absent profiles, the v2 to v3 migration and its
  golden tests, and the renderer's two-key output on the one path. It updates the design pattern,
  component and browser check, and the QEMU live phase at 1920 by 1080.
- [BIT-027](../plan/implementation-plan.md#bit-027-robust-onvif-profiles-ptz-and-field-interoperability)
  adds the ONVIF Media client, collects the main and sub profiles at adoption, maps them by reported
  resolution, and fills the schema fields. It records the evidence for every camera in
  [the camera matrix](../notes/camera-matrix.md).
- Measure the real capacity on the bench boxes and record it in a dated note: four, eight and sixteen
  substreams, the dropped-frame counter, `document.hidden` behaviour, CPU, memory and heat. Revise
  the priors or the thresholds with the numbers ([OQ-44](../open-questions.md#oq-44),
  [OQ-46](../open-questions.md#oq-46)).
- Verify the bench camera's ONVIF Media profiles, including whether it exposes a sub stream at all
  ([OQ-45](../open-questions.md#oq-45)).
- Re-check the Frigate restream roles after profiles exist. Moving `detect` to the sub stream is the
  next measured candidate, with a golden and a QEMU run, not a drive-by change.
- Add the browser cases: each layout at four widths, a sixteen-pane wall with sixteen stubbed
  streams, the hidden overlay click-through, Tab focus, Escape, full screen of six and eight, a
  demotion and a promotion with synthetic counters, and the snapshot interval with a fake timer.
- Update the QEMU harness to serve a second profile through the virtual camera or a Media fixture, so
  the two-stream path is exercised before BIT-027 reaches real hardware.

Named tests the implementation owes: `TestLayoutAreasForEveryCount`,
`TestLayoutSixAreaIsThePrimaryPlusRightAndBottom`, `TestLayoutEightAreaIsThePrimaryPlusRightAndBottom`,
`TestAutoLayoutResolvesFromTheInventory`, `TestStoredTwoClampsToOne`,
`TestCapacityDemotesLeastVisibleAutomaticPane`, `TestCapacityNeverDemotesManualPane`,
`TestCapacityStepDownBeforeDemotion`, `TestCapacityPromotesOnePaneAfterQuietWindow`,
`TestCapacityHysteresisDoesNotOscillate`, `TestCapacityClampsOnViewportDrop`,
`TestSnapshotPanesRefreshOnTheInterval`, `TestOverlayHiddenIsInertAndClickThrough`,
`TestTabRevealsOverlayWithFocus`, `TestFullScreenKeepsTheLayout`,
`TestLayoutCookieCarriesPerPaneQuality`, `TestSchemaV2ToV3AddsNoProfiles`,
`TestRendererWritesTwoStreamsFromProfiles`, `TestRendererWritesOnePathWithoutProfiles`.

## Alternatives considered

- **Fixed ceilings per device without runtime demotion.** Rejected: a fixed number is a guess that
  never learns. A capable desktop is held below its hardware and a struggling phone or laptop is not
  detected until the owner complains. The dropped-frame signal is the only evidence the browser can
  supply, and demotion plus promotion with hysteresis is what makes it safe to use.
- **Single-pane full screen.** Rejected: it hides the wall the owner built, forces the isolated pane
  to main, and makes the full-screen layout different from the layout on the page. A wall is the
  reason the layout exists. Full screen fits the layout.
- **Cropping to fill instead of letterboxing.** Rejected: cropping cuts the edges of the frame, which
  is where people and vehicles enter, and camera aspect ratios vary. `object-fit: contain` is already
  the behaviour and the pane reserves the box before the first frame.
- **Hiding camera names entirely.** Rejected: six or sixteen similar scenes cannot be told apart by
  position alone, and an operator needs to know which camera is which before acting. The name is
  content, so it lives in the overlay and fades with it instead of becoming a card header.
- **Keeping the current card chrome around panes.** Rejected: at sixteen panes the border, radius,
  padding, heading, badge, status block and picker take a large share of each pane's height, and the
  wall stops being a wall. The chrome also contradicts the full-bleed default and the no-toolbar full
  screen.
- **Six hand-written layouts.** Rejected: six templates drift, the six and eight relation would be
  written twice, and a new count would be a new template. One function over a grid of areas is
  smaller, testable and the source of the CSS.
- **Let the viewport choose the layout, as ADR 0034 section 4 does.** Rejected: the stored layout and
  the full-screen layout would not be the layout the owner picked, and a desktop wall would shrink
  on a phone without the owner seeing the choice. The viewport now chooses capacity, not layout.
- **Store per-pane quality in browser local storage.** Rejected: it cannot be server-rendered, it
  splits the owner's layout state across two stores, and it breaks the no-JavaScript path. The
  existing per-device cookie already carries the layout and is the one place for the quality choice.
- **No capacity limit at all.** Rejected: sixteen mains on a phone is 64 Mbit/s and 497.7 Mpx/s, and
  the tab will stall. The prior is a guarantee and the proxy maximum is a hard stop.
- **Always main in the grid.** Rejected: a sixteen-pane main wall is the entire 4K60 decode budget and
  64 Mbit/s, and the tiles are too small to show the extra detail. Sub is the default, and the owner
  can raise a pane.
- **A per-pane hover menu for quality.** Rejected: the design system forbids hover-only actions, a
  hover menu cannot work on touch, and it would put a control in every pane. The overlay is revealed
  by movement, key or tap and holds one contextual quality control for the selected pane.

## References

- [ADR-0005: Dual-NIC isolated camera network](./0005-dual-nic-isolated-camera-network.md)
- [ADR-0016: Bittern owns the control surface and the Frigate config](./0016-bittern-owns-frigate-config.md)
- [ADR-0019: UI stack and TLS](./0019-ui-stack.md)
- [ADR-0022: Community functionality, UX wins](./0022-community-functionality-ux-wins.md)
- [ADR-0024: ONVIF discovery levels and field interoperability](./0024-onvif-discovery-levels.md)
- [ADR-0025: Browser check for the responsive UI](./0025-browser-check-for-the-responsive-ui.md)
- [ADR-0026: Design system and UI review](./0026-design-system-and-ui-review.md)
- [ADR-0034: Live view owned by Bittern](./0034-live-view-owned-by-bittern.md)
- [ADR-0035: The appliance is the time authority for the camera network](./0035-appliance-as-time-authority.md)
- [ADR-0036: Update bundles deliver subsystem changes through the fixed helper](./0036-update-bundles-deliver-subsystem-changes.md)
- [Architecture](../architecture.md)
- [Live view pattern](../design/patterns.md#live-view)
- [Live player island](../design/components.md#live-player-island)
- [Design tokens](../design/tokens.md)
- [Open questions](../open-questions.md): OQ-21, OQ-25, OQ-26, OQ-44, OQ-45, OQ-46
- [Implementation plan](../plan/implementation-plan.md): BIT-023, BIT-024, BIT-025, BIT-027
- [Schema README](../../schema/README.md) and [migrations](../../schema/migrations/README.md)
- [Camera matrix](../notes/camera-matrix.md)
- [Frigate 0.18.3 pin note](../notes/2026-11-26-frigate-0.18.3-pin.md)
- [Live view upstream verification, 2026-11-26](../notes/2026-11-26-live-view-upstream-verification.md)
- [Live view sizing, 2026-11-26](../notes/2026-11-26-live-view-sizing.md)
- [Full-width shell and landing, 2026-11-26](../notes/2026-11-26-full-width-shell-and-landing.md)
- [QEMU harness](../../tests/qemu/README.md)
- Frigate 0.18.3 live view documentation (accessed 2026-11-27):
  <https://github.com/blakeblackshear/frigate/blob/v0.18.3/docs/docs/configuration/live.md>
- Frigate 0.18.3 restream documentation (accessed 2026-11-27):
  <https://github.com/blakeblackshear/frigate/blob/v0.18.3/docs/docs/configuration/restream.md>
- ONVIF Media WSDL (accessed 2026-11-27): <https://www.onvif.org/ver10/media/wsdl/media.wsdl>
- ONVIF Core Specification 26.06 (accessed 2026-11-27):
  <https://www.onvif.org/specs/core/ONVIF-Core-Specification.pdf>
- MDN, `HTMLVideoElement.getVideoPlaybackQuality()` (accessed 2026-11-27):
  <https://developer.mozilla.org/en-US/docs/Web/API/HTMLVideoElement/getVideoPlaybackQuality>
- MDN, `VideoPlaybackQuality` (accessed 2026-11-27):
  <https://developer.mozilla.org/en-US/docs/Web/API/VideoPlaybackQuality>
- MDN, Fullscreen API and guide (accessed 2026-11-27):
  <https://developer.mozilla.org/en-US/docs/Web/API/Fullscreen_API>,
  <https://developer.mozilla.org/en-US/docs/Web/API/Fullscreen_API/Guide>
- MDN, `grid-template-areas` (accessed 2026-11-27):
  <https://developer.mozilla.org/en-US/docs/Web/CSS/grid-template-areas>
- MDN, Page Visibility API (accessed 2026-11-27):
  <https://developer.mozilla.org/en-US/docs/Web/API/Page_Visibility_API>

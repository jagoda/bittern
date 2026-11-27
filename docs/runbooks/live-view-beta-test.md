# Live view beta test

- Date: 2026-11-26
- Status: maintainer checklist for the generation 3 live view beta. Not run yet. The live view is
  verified on the QEMU fixture; this is the first run on a real camera, a real phone, and the
  published beta channel with the sizing, full-width shell and landing changes.
- Bench: Debian 13 amd64, one management NIC on the house LAN, the camera NIC on the camera network,
  one to four cameras, a local console, and a phone on the house LAN.
- Payload: the published `app-beta-000003` release and the device's installed generation 1
  (`app-beta-000001`). Generation 2 introduced the live view and the Frigate 0.18.3 pin. Generation
  3 adds the live view sizing fix, the full-width shell and the state-aware landing.
- Related: [ADR 0034](../adr/0034-live-view-owned-by-bittern.md),
  [BIT-023](../plan/implementation-plan.md#bit-023-live-view),
  [live view slice 1a](../notes/2026-11-26-live-view-slice-1a.md),
  [live view slice 1b](../notes/2026-11-26-live-view-slice-1b.md),
  [live view sizing](../notes/2026-11-26-live-view-sizing.md),
  [full-width shell and landing](../notes/2026-11-26-full-width-shell-and-landing.md),
  [beta bench rollout](beta-bench-rollout.md),
  [update card](../notes/2026-11-22-update-card-ui.md),
  [0.18.3 pin](../notes/2026-11-26-frigate-0.18.3-pin.md),
  [update apply gap](../notes/2026-11-26-live-view-beta-update-gap.md),
  [UI design skill](../../.pi/skills/ui-design/SKILL.md),
  [UX-14](../principles.md#ux-14-live-updates-must-not-disturb-state-the-user-owns),
  [security advisory](https://github.com/blakeblackshear/frigate/security/advisories/GHSA-7822-v38f-8p44)

This runbook answers one question: can the owner watch the cameras live on the published beta
channel, from a phone and a desktop, and recover from a failed camera without reloading the
appliance? It starts where the [beta bench rollout](beta-bench-rollout.md) leaves the device: on
generation 1, signed in, with at least one camera in the configuration.

The live view MVP is the grid at `/ui/live`, the single-camera page at `/ui/live/{camera}`, the
authenticated MSE and HLS proxy, the snapshot fallback, and the per-device layout with the 1, 4 and
9 device ceilings. Generation 2 introduced it and the Frigate 0.18.3 pin. Generation 3 makes the
player fit the space it is given, fills the page shell to the device width, and resolves the root
and post-sign-in landing from setup state and the configured camera count.

## Preconditions

Run the checks in order and do not work around a failed one.

- [ ] The device is on generation 1, prepared from the `app-beta-000001` kit, and updated through the
      beta bench rollout. That kit's baseline pins 0.18.2, which the rollback check depends on. Do
      not rebuild the device from the generation 3 kit for this run.

```sh
sudo bittern update status
sudo cat /var/lib/bittern/app/current
curl -fsS http://127.0.0.1:8080/api/version
```

Expected: `committed: generation 1 (0.0.0-beta.1-<short>), channel beta` and
`running: generation 1 (0.0.0-beta.1-<short>)`; the pointer file contains `1`; the version endpoint
reports `baseline: false` and the same version. If the pointer contains `2` or `3`, the device is
not on the generation 1 baseline. Roll it back or restart from the beta bench rollout.

- [ ] The maintainer is signed in at `https://<lan0-address>/ui/status` and the Updates card shows
      generation 1 as the installed version.

- [ ] At least one camera is configured and its latest picture is visible on the Cameras page. Two
      cameras are needed for the tile assignment check, and four make the four-tile grid a full
      grid. With one camera, checks 2 and 4 are limited to empty tiles and the assignment rule
      cannot be observed; record that as not covered.

- [ ] Record the stream resolution of each camera before the live checks. Use a 1080p camera for the
      fit check and a 4K camera as well when one is available. If the bench has no 4K camera, run
      the 1080p case and record 4K as not covered; do not claim it from the browser stub.

- [ ] The camera network is connected and the camera is reachable on `cam0`. The live view uses the
      appliance's camera network, not the house LAN.

- [ ] The device can reach GitHub and GHCR. The update check and download need GitHub; the Frigate
      image pull needs GHCR.

```sh
getent hosts github.com
getent hosts ghcr.io
```

Expected: both print an address. If either fails, fix the management link before starting. An image
pull that cannot reach GHCR stops the apply and the live view will not start.

- [ ] The install channel is beta.

```sh
sudo cat /etc/bittern/install-channel
```

Expected: `beta`.

- [ ] A desktop browser at 1280 by 800, the same desktop at 1920 by 1080 or in a device mode at
      1920 by 1080, and a phone at 390 by 844 are on the house LAN and can open the appliance
      certificate. Accept the certificate once on each device.

- [ ] The generation 3 release is published and approved. The beta bench rollout records the release
      URL and the approval. The live view test starts after the release is public.

Record the run as you go:

| Field | Value |
| --- | --- |
| Date and maintainer | |
| Bench box and management address | |
| Generation 1 version | |
| Generation 3 version and release URL | |
| Generation 3 release run URL | |
| Generation 3 publish run URL | |
| Apply result and running image digest | |
| Startup time, one camera, desktop | |
| Startup time, one camera, phone | |
| 1080p fit and full-screen result | |
| 4K fit and full-screen result | |
| Full-width shell result, status and live | |
| Landing result, all cases | |
| Status, wizard and login empty and error result | |
| Offline camera result | |
| Camera network loss result | |
| Rollback result and running image digest | |

## What generation 3 changes

| Area | Generation 1 | Generation 3 |
| --- | --- | --- |
| Live view | No page | `/ui/live` grid, `/ui/live/{camera}`, MSE and HLS through `bitternd`, snapshot fallback, per-device layout, and a player sized from the space it is given on both axes |
| Full screen | None | A player control that contains the video, keeps the controls and status reachable, and leaves through the control, Escape or Back |
| Page shell | 68rem centered maximum | Full device width with the layout gutter; prose, hints and controls keep their measures; the status grid and live wall use the extra room |
| Landing | Fixed destination | `/ui/wizard` before setup completes, `/ui/live` with at least one camera, `/ui/status` with none; the next visit follows the current state |
| Frigate image | 0.18.2 | 0.18.3, which fixes CVE-2026-123007 |
| Renderer | No `go2rtc` block | `go2rtc.streams` and `cameras.<name>.live.streams` for every camera |
| Device ceilings | None | 1 on a phone, 4 on a tablet, 9 on a desktop, 9 hard media sessions |
| Update transaction | Bundle files only; an apply after the update | Candidate renderer, generated configuration and pin applied inside the transaction before commit; the saved-settings migration persisted after commit |

Generation 2 introduced the live view, the `go2rtc` renderer and the 0.18.3 pin. Generation 3 adds
the sizing fix, the full-width shell and the state-aware landing. It also lands the update
transaction's configuration apply and migration persist from
[ADR 0036](../adr/0036-update-bundles-deliver-subsystem-changes.md). A device on generation 1
receives all of it in one update.

The card install now runs the candidate renderer, native checks, atomic configuration write and
image pull before it commits. The manual camera-save bridge after the update is gone. The manual
rollback still moves only the bundle pointer and does not restore the generated configuration, so
one camera apply after the rollback returns the compose file and image to 0.18.2.

## Install generation 3

### Before

The Updates card shows the installed generation 1 version. It reads "Up to date" if no check has
run since the release, or "Update available" for generation 3 if one has. Select Check for updates
to make the card current.

Expected: "Update available", "Version `0.0.0-beta.3-<short>` is available. The appliance is running
version `0.0.0-beta.1-<short>`.", update number 3, and an Install update button. The release is
public and the check is not rate limited.

If the card offers a different version, stop. The tag is computed from the published releases, and a
different generation means the channel or the device is not in the state this runbook expects.

### During

Select Install update.

Expected: "Installing update `0.0.0-beta.3-<short>`", the current step, "N of M steps completed", and
a two-second refresh. The existing steps keep their order. The configuration apply runs after
"preparing the recording software", and the saved-settings persist runs after "checking the
appliance". The card labels both new steps "updating the appliance" because they do not have
separate labels yet.

The image step is "preparing the recording software". It checks the helper baseline image and starts
a pull when it is absent. On this bench the 0.18.2 image is already present, so this step is usually
quick. The candidate apply pulls 0.18.3 before its Frigate check, so the longer wait is after this
step. The step budget is 60 minutes. Leave the page open. Do not start a second install, do not
reboot, and do not run a camera apply during the update.

The update restarts `bitternd`. The card can show "Bittern is restarting. Reconnecting when it is
ready.", then the sign-in page. Sign in again. The session ending is expected and not a failed
update.

### After

Expected: the card reads "Up to date", the installed version is `0.0.0-beta.3-<short>`, and the card
offers "Roll back to version `0.0.0-beta.1-<short>`". Collect:

```sh
sudo bittern update status
sudo cat /var/lib/bittern/app/current
curl -fsS http://127.0.0.1:8080/api/version
```

Expected: `committed: generation 3 (0.0.0-beta.3-<short>)` and
`running: generation 3 (0.0.0-beta.3-<short>)`; the pointer file contains `3`; the version endpoint
reports `baseline: false` and the generation 3 version. The update journal is absent and the newest
history record is `committed`.

On the status page, open Technical details. "Recording software digest" names the 0.18.3 digest
from the new bundle. Because the update applied the configuration, this is also the image the
container should be running, not only the pin the daemon expects.

### Verify the applied configuration

The update applies the candidate configuration before it commits. Do not run a camera apply after
the update. Verify the running image:

```sh
sudo docker inspect --format '{{.Config.Image}}' frigate
```

Expected: `ghcr.io/blakeblackshear/frigate:0.18.3@sha256:d1a3e0008557bb3f3314d442ecd4ba68e9b16ab7872d64612649f7a6e5156635`.

On the status page, Technical details should show the 0.18.3 recording software digest, a recent
"Last written", a non-zero file count, and a healthy recording service state. The update history
should record the configuration apply as applied and the migration as none when the schema did not
change.

If the update failed at the configuration apply, the transaction rolls back the bundle and the
configuration together. The card says the update did not finish, and the status page names the
apply failure. Collect `sudo journalctl -u bittern-update.service -n 100 --no-pager` and
`sudo bittern update status`, fix the named cause, then check and install again. Do not edit the
compose file by hand. If a committed update still runs 0.18.2, stop and record it as a regression of
the apply-on-update step instead of applying by hand.

## Live view checks

Open `https://<lan0-address>/ui/live` for the checks below. Use the same browser at 1280 by 800
until the phone check. Record the observed time and the exact sentence for every failure.

### 1. One camera, full width and fit

At 1280 by 800, open Live. If the grid opens, open Layout and choose One camera. The single tile
spans the page and the player fills the space left by the heading, controls and status. The
single-camera page at `/ui/live/<camera-id>` is the same view with the main profile; the camera id is
in the address bar of the camera's edit page. With one camera, Live opens on that camera directly.

Repeat the view at 1920 by 1080. The tile uses the extra width instead of stopping at the old 68rem
shell, and the player still fits the available height. The page has no horizontal or vertical
document scroll, and the footer is absent on the live grid page.

Start timing when the page starts loading or when you select the preset. Stop when the badge reads
"Live" and the picture moves. Record the seconds.

Expected: "Connecting" briefly, then "Live". First frame within about 5 seconds on the house LAN,
which is ADR 0034's provisional p95 for MSE. Up to 10 seconds is the HLS budget. The QEMU fixture
reached playing in 2.9 seconds, so a real camera on the same LAN should be in that range.

A failure looks like 10 seconds with no picture, a badge stuck on "Connecting", "Offline", "Not
supported", a still picture with no explanation, or a badge that says "Live" over a frozen image.
Check a clock in the scene or walk in front of the camera.

### 2. The grid with four tiles

Open Layout and choose Four cameras.

Expected: four tiles, each with a camera name, a profile label and a "Live" badge. At 1280 by 800
there is no ceiling note, because the desktop ceiling is nine. With fewer than four cameras, the
spare tiles read "Empty tile" and "No camera is assigned to this tile" and offer a camera picker.

A failure looks like a tile that stays on "Connecting", a page error, horizontal scrolling, a
ceiling note at desktop width, or a configured camera missing from the grid.

### 3. Nine tiles and back

Choose Nine cameras, wait for the page to load, then choose Four cameras and wait again.

Expected: the four cameras stay in the first four tiles across both changes. Under Nine cameras the
five spare tiles are empty pickers, and the layout summary reads "Layout: Nine cameras". Under Four
cameras the empty tiles are gone and the summary reads "Layout: Four cameras". The assignment
survives a page reload.

A failure looks like a lost or duplicated camera, a camera moving to another tile on its own, an
empty tile that shows a broken player instead of the picker, or a preset that does not stick after a
reload.

### 4. A different camera in a tile

This check needs an empty tile and a camera that is not in the grid. Choose Nine cameras so there
are empty tiles. Clear one occupied tile (Change camera, No camera, Show in this tile); that camera
is now unassigned. Put it in an empty tile (Change camera, the camera, Show in this tile). The point
is the rule, not the tile numbers: a tile replacement must not disturb the other tiles.

Expected: only the chosen tile changes. It shows "Switching" and then "Live" with the new camera
name. The other playing tiles keep their pictures, badges and media connections, and the page does
not reload.

A failure looks like another tile stopping, reconnecting or losing its picture, the whole page
reloading, the chosen tile not changing, or the new assignment disappearing after a reload.

### 5. A poll during playback with a card open

Open the Layout disclosure, or a tile's Change camera picker, and leave it open. Watch the video for
at least 20 seconds so the 15-second camera-state poll fires in every tile. Keep the pointer and
the keyboard focus where they are.

Expected: every player stays "Live", the open disclosure stays open, the selected preset and the
tile count do not change, focus does not jump, and the page does not scroll. This is the UX-14 rule
from ADR 0034: a live update may change server-owned data, but it must not discard state the owner
controls.

A failure looks like a closing disclosure, a restarted player, a reset layout, a focus jump, a
scroll jump, or a video that stops while the badge stays "Live".

### 6. One tile full width and back

On the four-tile grid, select Open full width on tile 1. Wait for "Live". The tile spans the grid,
switches to the main profile and fits the viewport without page scrolling or overflow. Select Back
to grid.

Expected: tile 1 spans the grid and switches to the main profile; the other three tiles keep
playing. Back to grid returns tile 1 to its place on the sub profile, and the other three are still
playing. The page does not reload, and focus moves to the matching control.

A failure looks like the other tiles stopping or reconnecting, a page reload, a tile that does not
return to the grid, the main stream failing while the sub stream worked, or focus dropping to the
document.

### 7. Phone width and the ceiling note

Set the browser to 390 by 844, by using the phone or a desktop device mode, and reload the Four
cameras layout.

Expected: one tile is "Live" and the other three read "Not started on this screen" with their own
"Start this camera" and "Open full width" actions. One sentence says "This screen plays one camera
at a time, so only the first tile is live. Open a tile to watch it full width." There is no
horizontal scroll and the layout disclosure is reachable. Starting an idle tile stops the current
live tile in bounded order, so exactly one media connection is active.

A failure looks like more than one tile playing, no ceiling note, error badges on the idle tiles,
horizontal overflow, controls off screen, or two media connections after starting a second tile.

### 8. Player fit and browser full screen at 1080p and 4K

This check uses the native stream size, not the tile size. Use a 1080p camera first. If a 4K camera
is available, repeat the whole check with it and record both results. If no 4K camera is available,
record 4K as not covered.

On the four-tile grid, open the 1080p camera full width and wait for "Live". Confirm the media box
fits the available width and height and the picture is contained, not shown at its native size. At
1280 by 800 and at 1920 by 1080, the document has no horizontal or vertical overflow: the video,
heading, status and controls all stay inside the viewport.

Select Full screen. Expected: the current layout fills the screen with no header or toolbar, the
video is contained with no cropping or overflow, the page underneath does not scroll, the layout
picker and full screen control stay reachable in the wall overlay, and the selected pane's stream
selector and camera chooser open on that pane. The entry hint names Escape and a swipe down of at
least 120 pixels on a touch device. Leave with Escape; enter again and leave with the swipe on touch.
There is no exit button. Each exit returns to the same wall, selection and player state.

Repeat with the 4K camera if one is available. The stream size must not change the geometry: a 4K
stream fits the same box as a 1080p stream, and the page does not grow to the native resolution.

A failure looks like a picture that runs past the card or viewport, a vertical or horizontal
scrollbar on the grid or in full screen, a video that is cropped instead of contained, controls or
status pushed off screen, a full-screen request that fails with no visible sentence, Escape or the
swipe leaving the page locked or scrolled, or a 4K stream that sizes the page differently from a
1080p stream. A browser without the Fullscreen API hides or
rejects the control; record that and use Open full width for the geometry check.

### 9. Full-width shell on status and live

Open `/ui/status` and `/ui/live` at 1280 by 800, then at 1920 by 1080, and finally at 390 by 844.
Look at the left and right edges of the header and the main content, not only the middle of the
page.

Expected: the header and main content share the same full-width shell and stop only at the layout
gutter, 16 pixels at phone width and 24 pixels at 1280 and 1920. At 1920 the status card grid and the
live wall use the extra room; they do not stay at a centered 68rem column. Paragraphs and ledes stop
at the 65ch reading measure, hints at 60ch, and form controls at 32rem, so text does not run the
full width of a wide monitor. Long paths and digests wrap, and a comparison table scrolls inside
its `.table-wrap` instead of widening the page. At phone width the header navigation wraps below
the brand and nothing is cut off.

A failure looks like a narrow centered column at 1920, a header that is narrower than the page, a
paragraph or hint that runs the full width, a form control stretched across the monitor, a table
that widens the page, horizontal scrolling, or text pressed against the viewport edge.

### 10. Landing rules

Use the root `/ui` and the sign-in result, not a bookmarked page. The rule is: setup before the
device is initialized, live when it is initialized and has a camera, status when it is initialized
and has none. A requested local `/ui` target still wins over the default.

| Case | How to reach it | Expected |
| --- | --- | --- |
| Not set up | A fresh device before the first-run apply, or the QEMU firstboot fixture. Do not delete or edit the setup marker on the bench to simulate this. | Signed out, `/ui` goes to `/ui/setup` while the administrator password is absent. After the password exists but before the first apply succeeds, `/ui` and sign-in go to `/ui/wizard`, not live or status. No default landing skips setup. |
| Set up with cameras | The bench after the update and apply. Visit `/ui` signed out, then sign in. | Sign-in opens `/ui/live`. The header marks Live as the current section, offers Live and omits Setup. A direct `/ui/login?next=/ui/status` still returns to status after sign-in. |
| Set up with no cameras | On Cameras, remove the last camera. Then visit `/ui` and sign out and in again. | The next visit lands on `/ui/status`, not a stale live page. The header omits Live and marks Status. A direct `/ui/live` says "No cameras to watch" and offers Add camera. |
| Last camera removed | Start on a set-up device with one camera and the live page open. Remove that camera on Cameras, then visit `/ui` or sign out and in. | The next visit lands on `/ui/status`. The removed camera does not keep the live landing, and Live disappears from the header. A reload of the old live URL shows the no-camera state, not a broken player. |

The no-camera case is created by the last-camera removal, so run them together. Restore the camera
at the end of the check if the rest of the run needs it; removing and re-adding a camera is a normal
configuration apply, not a hand edit.

A failure looks like a signed-in root visit that stays on live with no camera, a no-camera device
that lands on live, a half-finished setup that lands on status, a sign-in that ignores the resolver,
or a last-camera removal that leaves the next visit on the old live page.

### 11. Empty and error states on status, wizard and login

Widening the shell can break pages that were not part of the live view work, especially when their
content is empty or an error is present. Check each page at 390 by 844 and 1280 by 800, and check
status at 1920 by 1080 as well.

| Page | State to reach | What to look at |
| --- | --- | --- |
| Status | A set-up device with no cameras, and an unknown or failed section if one is already present. Do not stop a service only to create an error on the bench. | The page fills the width, cards stack at phone width and use two columns at 1280 and above, the update card alert wraps instead of forcing a scrollbar, and long error text and tables stay inside their cards. Prose keeps its measure, and the page has no horizontal scroll. |
| Wizard | `/ui/wizard` and the camera step with an empty camera list. Open the step, do not select Apply on the bench. | The stepper and cards fit both widths, the empty camera list offers Add camera without a broken row, hints and fields keep their measures, and the next action is visible without horizontal scrolling. A validation error, if you reach one safely, stays inside its field and does not widen the page. |
| Login | The normal sign-in page and one failed sign-in with a wrong password. | The card and fields stay readable at both widths, the "Sign-in failed" alert wraps above the form, the primary action stays visible, and the error does not stretch the page or push the form off screen. |

A failure looks like an empty state with no next action, an error alert that widens the page, a
field or table cut off at phone width, a form that stretches to the full width of a 1920 monitor,
or a page that scrolls horizontally only when the empty or error state is visible.

## The two failure modes worth naming

### One camera offline

With four tiles playing, power off one camera, or unplug its cable from the camera network. Wait up
to a minute.

Expected: that tile reads "Offline" and its sentence names the camera and says it may be offline or
the recording service may be stopped, with Try again and Open cameras as the next actions. The other
three tiles stay "Live" and keep their pictures. Restore the camera and select Try again on the
failed tile; it returns to "Live" without a page reload.

A failure looks like the whole grid stopping, the failed tile spinning or staying on "Connecting",
one camera's failure changing another tile's state, or Try again doing nothing after the camera is
back.

### Losing the camera network

With four tiles playing, unplug the camera network cable at the appliance (`cam0`), not the
management link. Wait and watch one tile.

Expected: the tile shows "Stalled", then "Reconnecting to <Camera>" with "Trying again in 1 second.
Attempt 1 of 3.", then attempts 2 and 3 with their delays. Within about a minute each tile either
falls back to HLS or shows the snapshot with "Offline". There is no endless spinner and no page
reload loop. Reconnect `cam0`, wait for the camera to come back, and select Try again; the tile
returns to "Live".

If the management link is pulled, the browser loses the appliance itself and the page cannot show a
player state. That is a network failure, not this bounded reconnect, and it is not the check.

A failure looks like a tile that stays on "Connecting" or "Stalled" forever, a reload loop, a
reconnect count beyond 3, or a "Live" badge over a frozen picture.

## Roll back

The rollback is a manual rollback of the running bundle, not a failed-update rollback. The card
offers it only while the running and committed generations match. It moves the bundle pointer and
leaves the generated configuration and the recordings as they are, so the update history should show
a manual rollback with no new configuration apply.

On the Updates card, select "Roll back to version `0.0.0-beta.1-<short>`". Read the confirmation page:
it names the target and says the configuration, the cameras, and the recordings stay as they are.
Select "Roll back update".

Expected during: "Rolling back to version `0.0.0-beta.1-<short>`", the rollback steps, and the
two-second refresh. The daemon restarts and the session ends; sign in again.

Expected after: "Rolled back to version `0.0.0-beta.1-<short>`", the summary "You rolled back from
the status page. Configuration and recordings were not reverted.", "Last installed version
`0.0.0-beta.3-<short>`", and "The next update must be newer than version
`0.0.0-beta.3-<short>`." The rollback link is gone. Collect:

```sh
sudo bittern update status
sudo cat /var/lib/bittern/app/current
curl -fsS http://127.0.0.1:8080/api/version
```

Expected: `committed: generation 3 (0.0.0-beta.3-<short>)` and
`running: generation 1 (0.0.0-beta.1-<short>)`; the pointer file contains `1`; the version endpoint
reports the generation 1 version with `baseline: false`; the newest history record is `rolled-back`
with `restored_generation` 1. The committed version stays at 3. Do not edit
`/var/lib/bittern/update/state.json`, the pointer, or the history to make the card look clean. A
republished generation 1, 2 or 3 is not a valid next update; the next tag must be generation 4 or
later.

### Return the image to 0.18.2

The rollback restarts the recording service with the compose file the generation 3 update applied,
so the container is still on 0.18.3 until an apply from the rolled-back bundle runs. Select Apply
camera change on a camera without changing a value. This is the one remaining manual bridge in this
run: the image step returns the compose file and the running container to 0.18.2. Verify:

```sh
sudo docker inspect --format '{{.Config.Image}}' frigate
```

Expected: `ghcr.io/blakeblackshear/frigate:0.18.2@sha256:fd60f99ffd4eb6c42360ed4e14953e270586f239309ab3983e55b2e97ffac09a`.

This is the security consequence of the rollback. 0.18.2 is vulnerable to CVE-2026-123007
(GHSA-7822-v38f-8p44): an unauthenticated nginx HTTP/2 remote code execution as root, CVSS 9.8,
affecting 0.18.0 to 0.18.2 and fixed in 0.18.3. Running the 0.18.2 image reintroduces that exposure
until a generation newer than 3 is installed with 0.18.3; the next tag must be generation 4 or later
and must keep the 0.18.3 pin. Do this on the bench appliance on the house LAN, not on a device
exposed beyond it. If you stop before the post-rollback apply, the bundle is rolled back but the
engine stays on 0.18.3; record which state you left.

## If something fails

Stop at the failed check and collect the evidence before changing anything. Do not retry the update,
re-tag a generation, or edit the device state to make the card look clean. The release run page and
the release URL are part of the record.

Collect:

- The browser console. Open the developer tools, Console tab, reproduce the failure, and save the
  console log. On the Network tab, record the failing request status and the WebSocket close code.
  Do not copy cookies, the Authorization header, or a media URL with a token.
- The Updates card facts and the status page's Technical details. Copy the installed, available and
  last installed versions, the update number, the last check result, the operation result, and the
  failing step. On the live page, record the camera, the tile number, the viewport width, the badge,
  and the exact sentence.
- The update state:

```sh
sudo bittern update status
```

- The daemon and recording service logs:

```sh
sudo journalctl -u bitternd -n 100 --no-pager
sudo journalctl -u bittern-frigate -n 100 --no-pager
```

- For an image or apply failure, add:

```sh
sudo journalctl -u bittern-update.service -n 100 --no-pager
sudo journalctl -u bittern-image-prewarm.service -n 100 --no-pager
sudo docker inspect --format '{{.Config.Image}}' frigate
```

- The run URLs: the generation 3 release run, the called publish run, and the release URL. The
  release URL is `https://github.com/jagoda/bittern/releases/tag/app-beta-000003`.

Never include `/etc/bittern/secrets.yaml`, a camera URL with credentials, the administrator
password, the setup token, the short code, or the setup URL fragment in a report or a support
bundle. Read the daemon log before sharing it.

## What this build does not have

| Missing | What you will see | Owner |
| --- | --- | --- |
| PTZ | No pad, no movement control, no preset list. A camera without verified PTZ has no disabled control either. | BIT-027, OQ-39 |
| Audio | The player is muted and has no audio control. A stream with audio is played with the audio track ignored. | OQ-21 |
| Playback and timeline | No route and no control. The live view is live only. | BIT-025 |
| Event review | No event list, no clips, no review page. | BIT-024 |
| Camera configuration | The live page is read-only. Add, edit and disable cameras on the Cameras pages; there are no camera settings in live. | Out of scope, ADR 0034 |
| Sub-streams | `main` and `sub` resolve to the one configured RTSP path, which is the main stream's path, so every tile pulls the camera's full stream. The profile switch is a label, not a second stream and not a bandwidth saving. | BIT-027 |
| Real-vendor interoperability | The QEMU fixture serves two streams from one virtual camera guest. There is no second vendor, no real ONVIF profile, and no hardware decode or phone HLS evidence beyond the browser check. | BIT-027 |
| Kiosk mode | No full-screen kiosk. | OQ-25 |
| Frigate UI proxy | No embedded or proxied Frigate UI; the support path remains loopback. | OQ-38 |
| Account-level layout | The layout is a per-device cookie. It is lost in private browsing, after a cookie clear, or on a new device. | ADR 0034 |
| A real 4K camera | The browser check stubs 3840x2160 and the QEMU fixture serves 1920x1080. If the bench has no 4K camera, the 4K fit and full-screen case is not covered in the field. | BIT-023 |
| Manual rollback configuration | A manual rollback moves the bundle pointer but does not restore the generated configuration, so the container stays on the newer image until an apply from the rolled-back bundle. This runbook uses one camera apply as a temporary bridge. | BIT-009, ADR 0036 |
| Update step labels | The new configuration apply and saved-settings persist show the generic "updating the appliance" label. | BIT-009 |

## What this runbook does not cover

- The publish, approval, and release verification path. Use the
  [beta bench rollout](beta-bench-rollout.md) for the release workflow and the environment gate.
- A second camera vendor, ONVIF profiles, PTZ movement, audio, playback, events, or camera
  configuration.
- Nine simultaneous real streams. The desktop ceiling is nine, but this run uses at most four
  cameras. The nine-tile decode load, CPU, memory and LAN bitrate are not measured here.
- A real 4K camera when the bench does not have one. The browser check covers 3840x2160, but that is
  a stub, not a decode or bandwidth measurement.
- Long-run stability. The checks are minutes long, not hours.
- A power cut during the update. The per-step matrix is covered by the update engine tests, not by
  this run.
- The production release key. The beta channel uses the CI test key and the CI test anchor.
- The OS image, A/B slots, bootloader, and hardware release gate.
- A real 0.18.2 to 0.18.3 database migration. The release-note review found no schema change, but
  only a real upgrade and this rollback exercise the database path. Record any error rather than
  assuming it cannot happen.

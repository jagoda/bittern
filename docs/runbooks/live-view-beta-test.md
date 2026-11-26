# Live view beta test

- Date: 2026-11-26
- Status: maintainer checklist for the first live view beta generation. Not run yet. The live view is
  verified on the QEMU fixture; this is the first run on a real camera, a real phone, and the
  published beta channel.
- Bench: Debian 13 amd64, one management NIC on the house LAN, the camera NIC on the camera network,
  one to four cameras, a local console, and a phone on the house LAN.
- Payload: the published `app-beta-000002` release and the device's installed generation 1
  (`app-beta-000001`). Generation 2 carries the live view and the Frigate 0.18.3 pin.
- Related: [ADR 0034](../adr/0034-live-view-owned-by-bittern.md),
  [BIT-023](../plan/implementation-plan.md#bit-023-live-view),
  [live view slice 1a](../notes/2026-11-26-live-view-slice-1a.md),
  [live view slice 1b](../notes/2026-11-26-live-view-slice-1b.md),
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
9 device ceilings. Generation 2 is also the first published bundle that carries the Frigate 0.18.3
pin, which fixes a critical security issue.

## Preconditions

Run the checks in order and do not work around a failed one.

- [ ] The device is on generation 1, prepared from the `app-beta-000001` kit, and updated through the
      beta bench rollout. That kit's baseline pins 0.18.2, which the rollback check depends on. Do
      not rebuild the device from the generation 2 kit for this run.

```sh
sudo bittern update status
sudo cat /var/lib/bittern/app/current
curl -fsS http://127.0.0.1:8080/api/version
```

Expected: `committed: generation 1 (0.0.0-beta.1-<short>), channel beta` and
`running: generation 1 (0.0.0-beta.1-<short>)`; the pointer file contains `1`; the version endpoint
reports `baseline: false` and the same version. If the pointer contains `2`, the device is already on
generation 2. Roll it back first, or restart from the beta bench rollout.

- [ ] The maintainer is signed in at `https://<lan0-address>/ui/status` and the Updates card reads
      "Up to date".

- [ ] At least one camera is configured and its latest picture is visible on the Cameras page. Two
      cameras are needed for the tile assignment check, and four make the four-tile grid a full
      grid. With one camera, checks 2 and 4 are limited to empty tiles and the assignment rule
      cannot be observed; record that as not covered.

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

- [ ] A desktop browser at 1280 by 800 and a phone at 390 by 844 are on the house LAN and can open
      the appliance certificate. Accept the certificate once on each device.

- [ ] The generation 2 release is published and approved. The beta bench rollout records the release
      URL and the approval. The live view test starts after the release is public.

Record the run as you go:

| Field | Value |
| --- | --- |
| Date and maintainer | |
| Bench box and management address | |
| Generation 1 version | |
| Generation 2 version and release URL | |
| Generation 2 release run URL | |
| Generation 2 publish run URL | |
| Apply result and running image digest | |
| Startup time, one camera, desktop | |
| Startup time, one camera, phone | |
| Offline camera result | |
| Camera network loss result | |
| Rollback result and running image digest | |

## What generation 2 changes

| Area | Generation 1 | Generation 2 |
| --- | --- | --- |
| Live view | No page | `/ui/live` grid, `/ui/live/{camera}`, MSE and HLS through `bitternd`, snapshot fallback, per-device layout |
| Frigate image | 0.18.2 | 0.18.3, which fixes CVE-2026-123007 |
| Renderer | No `go2rtc` block | `go2rtc.streams` and `cameras.<name>.live.streams` for every camera |
| Device ceilings | None | 1 on a phone, 4 on a tablet, 9 on a desktop, 9 hard media sessions |

The update transaction installs the bundle and restarts the appliance. The generated Frigate
configuration and compose file are written by an apply, not by the update. Generation 2 therefore
has two parts: the card install, then one apply from the new bundle. The bundle carries the 0.18.3
pin; the apply writes it to the compose file and pulls the image. Until the apply runs, the
container keeps 0.18.2 even though the card reports generation 2. This is a known gap in the update
path, recorded in [the update apply gap note](../notes/2026-11-26-live-view-beta-update-gap.md). The
camera save below is a temporary bridge, not a product procedure.

## Install generation 2

### Before

The Updates card reads "Up to date" and shows the installed generation 1 version. Select Check for
updates.

Expected: "Update available", "Version `0.0.0-beta.2-<short>` is available. The appliance is running
version `0.0.0-beta.1-<short>`.", update number 2, and an Install update button. The release is
public and the check is not rate limited.

If the card offers a different version, stop. The tag is computed from the published releases, and a
different generation means the channel or the device is not in the state this runbook expects.

### During

Select Install update.

Expected: "Installing update `0.0.0-beta.2-<short>`", the current step, "N of M steps completed", and
a two-second refresh. The steps read in this order: checking the appliance, checking the update,
preparing the update, saving the current settings, stopping the recording service, switching to the
new version, preparing the recording software, starting the appliance, starting the recording
service, checking the appliance, finishing the update.

The image step is "preparing the recording software". It checks the pinned image and starts a pull
when the image is absent. On this bench the 0.18.2 image is already present, so this step is usually
quick. The 0.18.3 pull happens in the apply below, and that is where the longer wait is. The step
budget is 60 minutes. Leave the page open. Do not start a second install, do not reboot, and do not
run an apply at the same time.

The update restarts `bitternd`. The card can show "Bittern is restarting. Reconnecting when it is
ready.", then the sign-in page. Sign in again. The session ending is expected and not a failed
update.

### After

Expected: the card reads "Up to date", the installed version is `0.0.0-beta.2-<short>`, and the card
offers "Roll back to version `0.0.0-beta.1-<short>`". Collect:

```sh
sudo bittern update status
sudo cat /var/lib/bittern/app/current
curl -fsS http://127.0.0.1:8080/api/version
```

Expected: `committed: generation 2 (0.0.0-beta.2-<short>)` and
`running: generation 2 (0.0.0-beta.2-<short>)`; the pointer file contains `2`; the version endpoint
reports `baseline: false` and the generation 2 version. The update journal is absent and the newest
history record is `committed`.

On the status page, open Technical details. "Recording software digest" now names the 0.18.3 digest
from the new bundle. That is the pin the daemon expects, not the image the container is running.

### Apply the new bundle

Open Cameras, open any camera, and select Save camera without changing a value. This runs an apply
with the generation 2 daemon. It renders the new `go2rtc` block, writes the compose file with the
0.18.3 image, pulls the image, and restarts the recording service.

The page can take minutes and has no step list. Leave it open. Do not select Save again and do not
start another apply. The apply keeps running if the browser gives up; reload the status page and
check Technical details. To follow the pull, run `sudo journalctl -u bitternd -f` in a terminal
before selecting Apply; the apply logs the image step and any download failure.

The apply's image step has a 60 minute budget and may take noticeably longer than the previous
update. If it is slow, leave it. If it fails, the apply stops at that step and writes nothing live:
the old configuration and the old container keep running, and the live view will not start until the
image is present. The camera page says the configuration was not applied, and the status page's
Technical details names the recording software download error. Check `getent hosts ghcr.io`, the
free space on the appliance drive, and `sudo journalctl -u bitternd -n 100 --no-pager`, then apply
again. Do not edit the compose file by hand.

Verify the running image:

```sh
sudo docker inspect --format '{{.Config.Image}}' frigate
```

Expected: `ghcr.io/blakeblackshear/frigate:0.18.3@sha256:d1a3e0008557bb3f3314d442ecd4ba68e9b16ab7872d64612649f7a6e5156635`.

On the status page, Technical details should show the 0.18.3 recording software digest, a recent
"Last written", a non-zero file count, and a healthy recording service state.

## Live view checks

Open `https://<lan0-address>/ui/live` for the checks below. Use the same browser at 1280 by 800
until the phone check. Record the observed time and the exact sentence for every failure.

### 1. One camera, full width

Open Live. If the grid opens, open Layout and choose One camera. The single tile spans the page. The
single-camera page at `/ui/live/<camera-id>` is the same view with the main profile; the camera id is
in the address bar of the camera's edit page. With one camera, Live opens on that camera directly.

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

This check needs a camera that is not already in the grid. With five or more cameras, use Nine
cameras and put the spare camera in tile 5. With exactly four cameras, first clear tile 4 (Change
camera, No camera, Show in this tile), then put that camera in tile 5 under Nine cameras. The point
is the rule, not the tile number: a tile replacement must not disturb the other tiles.

Expected: only the chosen tile changes. It shows "Switching" and then "Live" with the new camera
name. Tiles 1 to 3 (and tile 4 when it was not the one cleared) keep their pictures, badges and
media connections. The page does not reload.

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

On the four-tile grid, select Open full width on tile 1. Wait for "Live", then select Back to grid.

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

## The two failure modes worth naming

### One camera offline

With four tiles playing, power off one camera, or unplug its cable from the camera network. Wait up
to 30 seconds.

Expected: that tile reads "Offline" with "<Camera> is offline" and the sentence "Bittern could not
start the live stream after 3 attempts. The camera may be offline, or the recording service may be
stopped. Try again, or open cameras." The other three tiles stay "Live" and keep their pictures.
Restore the camera and select Try again on the failed tile; it returns to "Live" without a page
reload.

A failure looks like the whole grid stopping, the failed tile spinning or staying on "Connecting",
one camera's failure changing another tile's state, or Try again doing nothing after the camera is
back.

### Losing the camera network

With four tiles playing, unplug the camera network cable at the appliance (`cam0`), not the
management link. Wait and watch one tile.

Expected: the tile shows "Stalled", then "Reconnecting to <Camera>" with "Trying again in 1 second.
Attempt 1 of 3.", then attempts 2 and 3 with their delays. Within about 30 seconds each tile either
falls back to HLS or shows the snapshot with "Offline". There is no endless spinner and no page
reload loop. Reconnect `cam0`, wait for the camera to come back, and select Try again; the tile
returns to "Live".

If the management link is pulled, the browser loses the appliance itself and the page cannot show a
player state. That is a network failure, not this bounded reconnect, and it is not the check.

A failure looks like a tile that stays on "Connecting" or "Stalled" forever, a reload loop, a
reconnect count beyond 3, or a "Live" badge over a frozen picture.

## Roll back

The rollback is a manual rollback of the running bundle, not a failed-update rollback. The card
offers it only while the running and committed generations match.

On the Updates card, select "Roll back to version `0.0.0-beta.1-<short>`". Read the confirmation page:
it names the target and says the configuration, the cameras, and the recordings stay as they are.
Select "Roll back update".

Expected during: "Rolling back to version `0.0.0-beta.1-<short>`", the rollback steps, and the
two-second refresh. The daemon restarts and the session ends; sign in again.

Expected after: "Rolled back to version `0.0.0-beta.1-<short>`", the summary "You rolled back from
the status page. Configuration and recordings were not reverted.", "Last installed version
`0.0.0-beta.2-<short>`", and "The next update must be newer than version
`0.0.0-beta.2-<short>`." The rollback link is gone. Collect:

```sh
sudo bittern update status
sudo cat /var/lib/bittern/app/current
curl -fsS http://127.0.0.1:8080/api/version
```

Expected: `committed: generation 2 (0.0.0-beta.2-<short>)` and
`running: generation 1 (0.0.0-beta.1-<short>)`; the pointer file contains `1`; the version endpoint
reports the generation 1 version with `baseline: false`; the newest history record is `rolled-back`
with `restored_generation` 1. The committed version stays at 2. Do not edit
`/var/lib/bittern/update/state.json`, the pointer, or the history to make the card look clean. A
republished generation 1 or 2 is not a valid next update; the next tag must be generation 3 or later.

### Return the image to 0.18.2

The rollback restarts the recording service with the compose file the generation 2 apply wrote, so
the container is still on 0.18.3 until the rolled-back bundle applies. Save a camera unchanged
again, as in the apply step. The image step returns the compose file and the running container to
0.18.2. Verify:

```sh
sudo docker inspect --format '{{.Config.Image}}' frigate
```

Expected: `ghcr.io/blakeblackshear/frigate:0.18.2@sha256:fd60f99ffd4eb6c42360ed4e14953e270586f239309ab3983e55b2e97ffac09a`.

This is the security consequence of the rollback. 0.18.2 is vulnerable to CVE-2026-123007
(GHSA-7822-v38f-8p44): an unauthenticated nginx HTTP/2 remote code execution as root, CVSS 9.8,
affecting 0.18.0 to 0.18.2 and fixed in 0.18.3. Running the 0.18.2 image reintroduces that exposure
until generation 3 carries 0.18.3 again. Do this on the bench appliance on the house LAN, not on a
device exposed beyond it. If you stop before the post-rollback apply, the bundle is rolled back but
the engine stays on 0.18.3; record which state you left.

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

- The run URLs: the generation 2 release run, the called publish run, and the release URL. The
  release URL is `https://github.com/jagoda/bittern/releases/tag/app-beta-000002`.

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
| Camera configuration | The live page is read-only. Add, edit and disable cameras on the Cameras pages; there are no camera settings in live. | BIT-023 scope |
| Sub-streams | `main` and `sub` resolve to the one configured RTSP path, so every tile pulls the camera's full stream. The profile switch is a label, not a second stream and not a bandwidth saving. | BIT-027 |
| Real-vendor interoperability | The QEMU fixture serves two streams from one virtual camera guest. There is no second vendor, no real ONVIF profile, and no hardware decode or phone HLS evidence beyond the browser check. | BIT-027, camera matrix |
| Kiosk mode | No full-screen kiosk. | OQ-25 |
| Frigate UI proxy | No embedded or proxied Frigate UI; the support path remains loopback. | OQ-38 |
| Account-level layout | The layout is a per-device cookie. It is lost in private browsing, after a cookie clear, or on a new device. | ADR 0034 |
| A wide 1920 wall | The page shell caps the grid at 68rem, so the tiles keep their 1280 width at 1920. | OQ-40 |
| An apply after update | The update installs the bundle but does not write the generated configuration. This runbook uses the camera save as a temporary bridge. | [update apply gap](../notes/2026-11-26-live-view-beta-update-gap.md) |

## What this runbook does not cover

- The publish, approval, and release verification path. Use the
  [beta bench rollout](beta-bench-rollout.md) for the release workflow and the environment gate.
- A second camera vendor, ONVIF profiles, PTZ movement, audio, playback, events, or camera
  configuration.
- Nine simultaneous real streams. The desktop ceiling is nine, but this run uses at most four
  cameras. The nine-tile decode load, CPU, memory and LAN bitrate are not measured here.
- Long-run stability. The checks are minutes long, not hours.
- A power cut during the update. The per-step matrix is covered by the update engine tests, not by
  this run.
- The production release key. The beta channel uses the CI test key and the CI test anchor.
- The OS image, A/B slots, bootloader, and hardware release gate.
- A real 0.18.2 to 0.18.3 database migration. The release-note review found no schema change, but
  only a real upgrade and this rollback exercise the database path. Record any error rather than
  assuming it cannot happen.

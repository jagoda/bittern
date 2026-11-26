# Live view slice 1a: one camera through the authenticated proxy

- Date: 2026-11-26
- Status: implemented and verified on the QEMU harness. The grid, the layout cookie and the saved
  layout clamp remain open in [BIT-023](../plan/implementation-plan.md#bit-023-live-view).
- Scope: the single-camera page, the authenticated MSE and HLS proxy, the snapshot fallback, the
  renderer stream block, the player state machine and the 1/4/9 ceiling structure.
- Related: [ADR 0034](../adr/0034-live-view-owned-by-bittern.md),
  [ADR 0016](../adr/0016-bittern-owns-frigate-config.md),
  [UX-14](../principles.md#ux-14-live-updates-must-not-disturb-state-the-user-owns),
  [upstream verification](2026-11-26-live-view-upstream-verification.md),
  [UI README](../../ui/README.md), [QEMU harness](../../tests/qemu/README.md).

## What changed

| Area | Change |
| --- | --- |
| Routes | `/ui/live` and `/ui/live/{camera}` show one enabled camera full width, with a picker when more than one exists. `/ui/live/{camera}/status` is the sibling poll. |
| Media proxy | `/ui/live/media/{camera}/{profile}/` serves the fixed `ws`, `stream.m3u8`, `hls/playlist.m3u8`, `hls/init.mp4`, `hls/segment.m4s`, `hls/segment.ts` and `snapshot.jpg` endpoints. The server resolves the camera and profile; the browser cannot supply an upstream URL, host, port, path or `src`. |
| Auth | Navigation gets the login redirect. An HTMX fragment gets `HX-Redirect` to its parent page. Media gets `401` with no HTML. A WebSocket revalidates the session every 30 seconds. |
| Renderer | Emits `go2rtc.streams` and `cameras.<name>.live.streams`, with `main` and `sub` keys for each camera. Both keys point at the single configured RTSP path until BIT-027. |
| Player | `ui/static/live-player.js` owns the video element, the MSE `SourceBuffer` and the WebSocket. States are connecting, playing, switching, stalled, reconnecting, offline, unsupported and session ended. Recovery is three visible retries, then HLS, then the snapshot. |
| Isolation | The player sits outside every `hx-*` region. A template test walks the rendered DOM, and the controller refuses to mount if `closest()` finds an HTMX ancestor. The sibling camera-state line polls every 15 seconds. |
| Ceiling | The player exposes `data-device-ceiling` as 1 below 44rem, 4 below 80rem and 9 at 80rem or above, and `data-active-tiles` for the active count. The proxy keeps the hard nine-connection maximum per session. |
| CSP | `media-src` is `'self' blob:`. Chrome MSE needs a same-origin object URL; no external media source is allowed. This is the smallest addition to the ADR's self-only media policy. |

## Evidence

- `make check` passes, including the renderer goldens, the live proxy tests and the template guard.
- `go vet` and `gofmt` are clean.
- `make ui-browser-check` passes 144 route-width screens from 20 routes at 360, 390, 1280 and 1920,
  plus 12 live-player state and ceiling checks. The four-width live screenshots are under
  `build/ui-screenshots/`.
- `python3 tests/qemu/run.py firstboot` passes on the rebased 0.18.3 pin with the live phase. It
  signs in, checks the page for a leaked stream name or camera address, fetches the authenticated
  HLS master, playlist and a 1504-byte segment, and a host Playwright player reaches playing at
  640x360 in 2.9 seconds. Screenshot:
  `build/qemu/logs/firstboot-20261126-130254/live-view.png`.
- The QEMU run also proves the firewall and the existing no-8555 checks still hold.

## Findings

| ID | Finding | Destination |
| --- | --- | --- |
| F1 | The HTTP request logger's `responseRecorder` hid `http.Hijacker`, so the real daemon returned 500 for the WebSocket upgrade even though handler tests passed. The recorder now forwards `Hijack` and `Flush`. | This change; regression test in `agent/cmd/bitternd/main_test.go`. |
| F2 | Chrome rejects a `MediaSource` assigned to `srcObject` and needs an object URL, which the original self-only `media-src` blocked. | This change; CSP `media-src 'self' blob:` and the browser state walk. |
| F3 | Hijacking leaves the HTTP read and write deadlines on the connection. The proxy clears both deadlines so a live view can outlast the request timeout. | This change. |
| F4 | The checked-in QEMU firstboot path had drifted from the current installer and apply flow: the payload omitted the trust anchor, the pre-apply service start left an orphan chronyd, the fixture had no recording disk, and the wizard expected the old done destination. | This change, test harness only. |

## Cut from this slice

The grid, the per-device layout cookie, the saved-layout clamp, PTZ, audio, event review, playback
and camera configuration are not in this change. The player has the profile switch and the ceiling
structure the grid needs, but the grid itself is still BIT-023.

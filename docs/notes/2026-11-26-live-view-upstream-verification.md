# Live view upstream verification

- Date: 2026-11-26
- Status: verified against the pinned Frigate release and its bundled go2rtc. No product code, UI,
  schema, renderer or golden changed. The decision this evidence supports is
  [ADR 0034](../adr/0034-live-view-owned-by-bittern.md).
- Scope: the live view facts that [BIT-023](../plan/implementation-plan.md#bit-023-live-view) depends
  on, plus the repository state that constrains the design. All upstream pages and source files were
  accessed on 2026-11-26 unless a row says otherwise.
- Related: [ADR 0016](../adr/0016-bittern-owns-frigate-config.md),
  [ADR 0019](../adr/0019-ui-stack.md), [ADR 0022](../adr/0022-community-functionality-ux-wins.md),
  [OQ-23](../open-questions.md), [OQ-38](../open-questions.md), [OQ-39](../open-questions.md),
  [Frigate assessment](../research/frigate-assessment.md), [upstream register](../upstream.md),
  [media surfaces](2026-11-14-media-surfaces.md),
  [live refresh state loss](2026-11-26-live-refresh-state-loss.md),
  [QEMU harness](../../tests/qemu/README.md).

The pinned release was checked as a release, an OCI image, and a tagged documentation tree, not
from memory and not from the current documentation site. The documentation site renders the latest
release, which was 0.18.3 on the access date, so the quotes below come from the `v0.18.2` tag in the
Frigate repository and from the `v1.9.14` tag in the go2rtc repository. The current site was used
only to locate the corresponding pages. This distinction matters: a claim about 0.18.2 is quoted
from 0.18.2, and the 0.18.3 release is treated separately.

## Summary

| Item | Result |
| --- | --- |
| Pinned release | 0.18.2, published 2026-10-15, digest `sha256:fd60f99ffd4eb6c42360ed4e14953e270586f239309ab3983e55b2e97ffac09a`. The digest in `defaults/frigate.yaml` matches the GHCR image index on the access date. |
| Later release | 0.18.3, published 2026-11-19. It is a security release for an unauthenticated nginx HTTP/2 RCE as root (CVE-2026-123007). The pin is one patch behind it. |
| Embedded streaming | Frigate bundles go2rtc 1.9.14. go2rtc's HTTP API and web player default to port 1984. |
| Live transport | go2rtc offers WebRTC, MSE, HLS, MP4 and MJPEG. Frigate's live view defaults to MSE when go2rtc is configured, uses jsmpeg without go2rtc, and reserves WebRTC for cases that need it. |
| Camera stream names | There are no default `main` and `sub` names. Streams are keys under `go2rtc.streams`; `cameras.<name>.live.streams` maps a friendly name to one of those keys. The first entry is the default for the All Cameras dashboard. |
| PTZ | Frigate's PTZ control is ONVIF. The documented movement commands are MQTT payloads on `frigate/<camera_name>/ptz`; the HTTP API exposes `GET /api/<camera_name>/ptz/info` for capability and presets, not movement. |
| Auth | `auth.enabled` defaults to true and an admin password is generated and printed in the logs on startup. The internal port 5000 is unauthenticated admin by design; 8971 authenticates users unless auth is explicitly disabled. |
| Snapshots | `GET /api/<camera_name>/latest.<extension>` returns the latest frame and falls back to preview frames when the camera is offline. Event snapshots are `GET /api/events/<event_id>/snapshot.jpg`. |
| Browser playback | MSE is the best proxied transport for this appliance. WebRTC has the lowest latency but needs 8555 and cannot pass an HTTP proxy. HLS is the fallback for iPhone Safari before 17.1, with the worst latency of the three. |
| Repository boundary | The firewall goldens allow 443 on `lan0` only and drop 1984, 5000, 8554, 8555 and 8971 on `lan0` and `cam0`. The Frigate container publishes no ports and uses host networking. No existing code proxies a camera stream or a Frigate endpoint. |

## 1. Release and image pin

| Claim | Evidence and quote | Source |
| --- | --- | --- |
| 0.18.2 is the pinned release. | `defaults/frigate.yaml` sets `tag: 0.18.2`, `digest: sha256:fd60f99f...`, `verified: 2026-11-14`. | [defaults/frigate.yaml](../../defaults/frigate.yaml) |
| 0.18.2 was published on 2026-10-15. | The release page is titled "Release 0.18.2" and its body begins "This is a maintenance release for Frigate 0.18 that includes some security fixes." The API reports `published_at: 2026-10-15T22:41:47Z`. | <https://github.com/blakeblackshear/frigate/releases/tag/v0.18.2> |
| The pinned digest is the current GHCR index digest for 0.18.2. | The OCI manifest request for `ghcr.io/blakeblackshear/frigate:0.18.2` returned `docker-content-digest: sha256:fd60f99ffd4eb6c42360ed4e14953e270586f239309ab3983e55b2e97ffac09a` and `content-type: application/vnd.oci.image.index.v1+json`. | <https://ghcr.io/v2/blakeblackshear/frigate/manifests/0.18.2> |
| 0.18.3 is later and is a security release. | The release body says: "Security fixes - NGINX2Root (CVE-2026-123007): bundled nginx 1.27.4 with HTTP/2 enabled allows unauthenticated RCE as root". It was published on 2026-11-19. | <https://github.com/blakeblackshear/frigate/releases/tag/v0.18.3> |

**Triage.** This is a release-gate finding, not a live view design fact. The advisory gate in
[AGENTS.md](../../AGENTS.md) and [ADR 0012](../adr/0012-testing-and-release-gates.md) blocks a release
on a high or critical advisory against the pin. The pin must move to 0.18.3, or a later release that
contains the fix, in a separate bundle change with the digest, renderer and golden updates and the
usual verification. No live view claim below depends on the 0.18.2 to 0.18.3 difference; the design
must be true for the version that ships.

## 2. Live streams, go2rtc and ports

| Claim | Evidence and quote | Source |
| --- | --- | --- |
| Frigate's live view uses bundled go2rtc. | "The highest quality and fluency of the Live view requires the bundled `go2rtc` to be [configured]." | <https://github.com/blakeblackshear/frigate/blob/v0.18.2/docs/docs/configuration/live.md> |
| Frigate uses go2rtc for restream and MSE/WebRTC. | "Frigate uses [go2rtc](https://github.com/AlexxIT/go2rtc/tree/v1.9.14) to provide its restream and MSE/WebRTC capabilities." | <https://github.com/blakeblackshear/frigate/blob/v0.18.2/docs/docs/configuration/restream.md> |
| The bundled go2rtc version is 1.9.14. | The Dockerfile adds `https://github.com/AlexxIT/go2rtc/releases/download/v1.9.14/go2rtc_linux_${TARGETARCH}`. The system docs say "Frigate currently includes go2rtc v1.9.14". | <https://github.com/blakeblackshear/frigate/blob/v0.18.2/docker/main/Dockerfile> and <https://github.com/blakeblackshear/frigate/blob/v0.18.2/docs/docs/configuration/advanced/system.md> |
| go2rtc's HTTP API default is `http://localhost:1984/`. | "The HTTP API is the main part for interacting with the application. Default address: `http://localhost:1984/`." The module config shows `listen: ":1984" # default ":1984"`. | <https://github.com/AlexxIT/go2rtc/blob/v1.9.14/README.md> |
| The go2rtc web player is served on the same port. | "Open web interface: `http://localhost:1984/`." The API docs list `www/stream.html` as the universal viewer and `www/webrtc.html` as the WebRTC viewer. | <https://github.com/AlexxIT/go2rtc/blob/v1.9.14/README.md> and <https://github.com/AlexxIT/go2rtc/blob/v1.9.14/api/README.md> |
| The go2rtc API is reachable from the appliance host when Frigate uses host networking. | Frigate's live view docs say of `network: host`: "in this mode you don't need to forward any ports. The services inside of the Frigate container will have full access to the network interfaces of your host machine as if they were running natively and not in a container." Frigate's troubleshooting docs say "If using Docker, forward port `1984` before accessing the web interface", which is unnecessary in host mode. | <https://github.com/blakeblackshear/frigate/blob/v0.18.2/docs/docs/configuration/live.md> and <https://github.com/blakeblackshear/frigate/blob/v0.18.2/docs/docs/troubleshooting/go2rtc.md> |
| The go2rtc API does not authenticate localhost requests. | "go2rtc passes requests from localhost and from Unix sockets without HTTP authorisation, even if you have it configured! It is your responsibility to set up secure external access to the API." | <https://github.com/AlexxIT/go2rtc/blob/v1.9.14/README.md> |
| The documented go2rtc live endpoints are `/api/ws`, `/api/stream.m3u8`, `/api/stream.mp4` and `/api/frame.mp4`. | The WebSocket API endpoint is `/api/ws` with `src` required. HLS examples are `http://192.168.1.123:1984/api/stream.m3u8?src=camera1` and `...&mp4`. MP4 examples are `/api/stream.mp4?src=camera1` and `/api/frame.mp4?src=camera1`. | <https://github.com/AlexxIT/go2rtc/blob/v1.9.14/api/README.md> and <https://github.com/AlexxIT/go2rtc/blob/v1.9.14/README.md> |
| Frigate documents `/api/go2rtc/streams` for stream info. | "You can access the go2rtc stream info at `/api/go2rtc/streams` which can be helpful to debug as well as provide useful information about your camera streams." | <https://github.com/blakeblackshear/frigate/blob/v0.18.2/docs/docs/configuration/restream.md> |
| Frigate's own UI paths for MSE and WebRTC are not documented as a public API. | The nginx config proxies `/live/mse/api/ws`, `/live/webrtc/api/ws` and `/live/webrtc/webrtc.html` to go2rtc behind Frigate's auth request. These paths are implementation detail, not a documented integration contract. | <https://github.com/blakeblackshear/frigate/blob/v0.18.2/docker/main/rootfs/usr/local/nginx/conf/nginx.conf> |

**Reachability conclusion.** On the appliance, `127.0.0.1:1984` reaches go2rtc and its web player
because the container is on the host network and go2rtc binds all interfaces by default. A client on
`lan0` or `cam0` does not reach it: the rendered nftables rules drop 1984 on both interfaces, and
[defaults/frigate.yaml](../../defaults/frigate.yaml) does not configure a go2rtc listener. The same
is true of Frigate's 5000 and 8971 listeners: the config binds both to `127.0.0.1`, and the firewall
drops both on `lan0` and `cam0`.

**Consequence.** The go2rtc API is an unauthenticated local admin surface for every camera stream.
Any local process can read or publish streams on 1984. The live view proxy must therefore be the only
browser path, must never accept an upstream URL from the browser, and must not expose the go2rtc web
player. This is the same reasoning as [ADR 0016](../adr/0016-bittern-owns-frigate-config.md) for
Frigate's 5000 API.

## 3. Per-camera stream profiles

| Claim | Evidence and quote | Source |
| --- | --- | --- |
| Streams are declared as named keys under `go2rtc.streams`. | The examples use `go2rtc: streams: test_cam: ... test_cam_sub: ... test_cam_another_sub: ...`. | <https://github.com/blakeblackshear/frigate/blob/v0.18.2/docs/docs/configuration/live.md> |
| There are no default `main` and `sub` names. | The names in the documentation are author-chosen examples (`test_cam`, `test_cam_sub`, `test_cam_another_sub`). The only documented default is the first entry in the list. | <https://github.com/blakeblackshear/frigate/blob/v0.18.2/docs/docs/configuration/live.md> |
| A camera maps friendly names to go2rtc stream names under `cameras.<name>.live.streams`. | "Configure a 'friendly name' for your stream followed by the go2rtc stream name. Using Frigate's internal version of go2rtc is required to use this feature. You cannot specify paths in the streams configuration, only go2rtc stream names." The YAML example is `live: streams: Main Stream: test_cam` and `Sub Stream: test_cam_sub`. | <https://github.com/blakeblackshear/frigate/blob/v0.18.2/docs/docs/configuration/live.md> |
| The first entry is the default for the All Cameras dashboard. | "Frigate's default dashboard ('All Cameras') will always use the first entry you've defined in streams when playing live streams from your cameras." | <https://github.com/blakeblackshear/frigate/blob/v0.18.2/docs/docs/configuration/live.md> |
| A client selects a stream from a dropdown, and the choice is per device. | "Setting the streams list will populate a dropdown in the UI's Live view that allows you to choose between the streams. This stream setting is _per device_ and is saved in your browser's local storage." | <https://github.com/blakeblackshear/frigate/blob/v0.18.2/docs/docs/configuration/live.md> |
| Camera inputs declare roles such as `detect` and `record`. | The restream example shows two inputs: `main` with `roles: [record]` and `sub` with `roles: [detect]`, both pointing at `rtsp://127.0.0.1:8554/<stream>`. | <https://github.com/blakeblackshear/frigate/blob/v0.18.2/docs/docs/configuration/restream.md> |

**Consequence.** Bittern cannot assume a camera has streams named `main` and `sub`. The renderer must
own the names and emit both the `go2rtc.streams` entries and the camera's live mapping. The schema
does not currently carry a second RTSP path; that is a schema change with a migration, or a later
BIT-027 profile negotiation. Until then, both names can resolve to the single configured path.

## 4. PTZ

| Claim | Evidence and quote | Source |
| --- | --- | --- |
| Frigate's PTZ protocol is ONVIF. | "Not every PTZ supports ONVIF, which is the standard protocol Frigate uses to communicate with your camera." | <https://github.com/blakeblackshear/frigate/blob/v0.18.2/docs/docs/configuration/cameras.md> |
| The ONVIF connection is configured per camera, separately from the stream input. | `cameras.<name>.onvif` takes `host`, `port`, `user`, `password`, and optional `profile`. The docs say "Configure the ONVIF connection for your camera to enable PTZ controls." | <https://github.com/blakeblackshear/frigate/blob/v0.18.2/docs/docs/configuration/cameras.md> |
| Continuous movement, zoom, presets and stop are MQTT commands. | The `frigate/<camera_name>/ptz` table lists `preset_<preset_name>`, `MOVE_<dir>` with possible values UP, DOWN, LEFT, RIGHT, `ZOOM_<dir>` with IN, OUT, and `STOP`. | <https://github.com/blakeblackshear/frigate/blob/v0.18.2/docs/docs/integrations/mqtt.md> |
| The HTTP API exposes PTZ info, not movement. | `GET /api/<camera_name>/ptz/info` is described as "Access: Authenticated user with access to the referenced camera." The OpenAPI paths contain no PTZ move, stop, zoom or preset endpoint. | <https://github.com/blakeblackshear/frigate/blob/v0.18.2/docs/static/frigate-api.yaml> |
| go2rtc's ONVIF source is for RTSP and snapshot discovery, not PTZ control. | The source list says "onvif - get camera RTSP link and snapshot link using ONVIF protocol". The go2rtc API docs contain no PTZ endpoint. | <https://github.com/AlexxIT/go2rtc/blob/v1.9.14/README.md> and <https://github.com/AlexxIT/go2rtc/blob/v1.9.14/api/README.md> |
| PTZ behind go2rtc is not documented as a guarantee. | The ONVIF client uses the camera's configured `onvif.host`, not the ffmpeg input or go2rtc stream. Nothing in the documentation says PTZ depends on a direct camera stream. It also does not promise that every camera behind a restream accepts ONVIF commands. | <https://github.com/blakeblackshear/frigate/blob/v0.18.2/docs/docs/configuration/cameras.md> |

**Consequence.** Frigate's documented PTZ movement path is MQTT, and product config sets
`mqtt.enabled: false`. The HTTP surface returns capability and preset data only, and go2rtc has no
PTZ API. A PTZ control surface cannot be shipped on the current transport. BIT-027 must choose a
command path, and a real PTZ camera must verify move, stop, zoom, presets and stop-on-loss before the
control appears. The choice is [OQ-39](../open-questions.md).

## 5. Snapshots, camera list and auth

| Claim | Evidence and quote | Source |
| --- | --- | --- |
| The latest frame endpoint is `GET /api/<camera_name>/latest.<extension>`. | "Returns the latest frame from the specified camera in the requested format (jpg, png, webp). Falls back to preview frames if the camera is offline." | <https://github.com/blakeblackshear/frigate/blob/v0.18.2/docs/static/frigate-api.yaml> |
| The event snapshot endpoint is `GET /api/events/<event_id>/snapshot.jpg`. | The snapshot docs list `/api/events/<id>/snapshot.jpg` and `/api/events/<id>/snapshot-clean.webp`. | <https://github.com/blakeblackshear/frigate/blob/v0.18.2/docs/docs/configuration/snapshots.md> |
| The camera list is available from `GET /api/config` and `GET /api/stats`. | Both are "Access: Any authenticated user." `/api/config` returns the effective configuration, including `cameras`; `/api/stats` returns the camera statistics. | <https://github.com/blakeblackshear/frigate/blob/v0.18.2/docs/static/frigate-api.yaml> |
| The internal port 5000 is unauthenticated admin. | "The `remote-role` value is overridden to admin-level access. This design ensures unauthenticated internal use within a trusted network." | <https://github.com/blakeblackshear/frigate/blob/v0.18.2/docs/docs/configuration/authentication.md> |
| The authenticated port 8971 requires a user unless auth is disabled. | The ports table calls 8971 "Authenticated UI and API" and 5000 "Internal unauthenticated UI and API access". With `auth.enabled: false`, the auth endpoint applies the proxy header map and returns success; the default role is `viewer`. | <https://github.com/blakeblackshear/frigate/blob/v0.18.2/docs/docs/configuration/authentication.md> and <https://github.com/blakeblackshear/frigate/blob/v0.18.2/frigate/api/auth.py> |
| Auth is enabled by default and a generated admin password is printed at startup. | `AuthConfig.enabled` defaults to `True`. The docs say "On startup, an admin user and password are generated and printed in the logs." | <https://github.com/blakeblackshear/frigate/blob/v0.18.2/frigate/config/auth.py> and <https://github.com/blakeblackshear/frigate/blob/v0.18.2/docs/docs/configuration/authentication.md> |
| Snapshots and the camera list need no login on 5000, and need no login on 8971 when auth is disabled. | On 5000 the auth check is bypassed before the enabled check. On 8971 with auth disabled, the request is accepted without a JWT and the role is resolved from proxy headers or the default. | <https://github.com/blakeblackshear/frigate/blob/v0.18.2/frigate/api/auth.py> |

**Consequence.** The security boundary for these endpoints is the loopback bind plus the host
firewall, not Frigate authentication. Any local process can fetch a snapshot or the camera list from
5000 as admin. The browser must never be given a 5000 URL. Bittern should read its camera inventory
from `appliance.yaml`, not from `/api/config`, because Bittern is the single writer
([ADR 0016](../adr/0016-bittern-owns-frigate-config.md)); `/api/config` is a diagnostic endpoint, not
an inventory source.

## 6. Browser playback

| Claim | Evidence and quote | Source |
| --- | --- | --- |
| go2rtc offers WebRTC, MSE, HLS, MP4 and MJPEG. | The module list includes WebRTC, MSE/MP4, HomeKit, HLS and MJPEG. The API docs list stream technology autoselection `mode=webrtc,webrtc/tcp,mse,hls,mp4,mjpeg`. | <https://github.com/AlexxIT/go2rtc/blob/v1.9.14/README.md> and <https://github.com/AlexxIT/go2rtc/blob/v1.9.14/api/README.md> |
| WebRTC has the lowest latency; MSE is medium; HLS is bad. | The compatibility table's latency row is best, medium, bad, bad for WebRTC, MSE, HTTP and HLS. | <https://github.com/AlexxIT/go2rtc/blob/v1.9.14/README.md> |
| Frigate defaults to MSE when go2rtc is configured. | The live view table says MSE "is Frigate's default when go2rtc is configured." The FAQ says "Frigate prefers MSE ... MSE runs over TCP, so every frame arrives and is decoded in order ... near-real-time playback". | <https://github.com/blakeblackshear/frigate/blob/v0.18.2/docs/docs/configuration/live.md> |
| WebRTC requires extra configuration and port 8555. | "WebRTC works by creating a TCP or UDP connection on port `8555`." The docs require candidates and, for external access, forwarding 8555 for TCP and UDP. | <https://github.com/blakeblackshear/frigate/blob/v0.18.2/docs/docs/configuration/live.md> |
| WebRTC cannot pass through an HTTP reverse proxy. | "It can't pass media data through your Nginx or Cloudflare or Nabu Casa HTTP TCP connection!" | <https://github.com/AlexxIT/go2rtc/blob/v1.9.14/README.md> |
| HLS is the worst technology for real-time streaming. | "HLS is the worst technology for real-time streaming. It can only be useful on devices that do not support more modern technology, like WebRTC, MSE/MP4." | <https://github.com/AlexxIT/go2rtc/blob/v1.9.14/README.md> |
| MSE requires iOS 17.1 or newer on iPhone. | The Frigate table says MSE "iPhone requires iOS 17.1+". The go2rtc table shows iPhone Safari 17.1+ with MSE and older iPhone Safari with HLS only. | <https://github.com/blakeblackshear/frigate/blob/v0.18.2/docs/docs/configuration/live.md> and <https://github.com/AlexxIT/go2rtc/blob/v1.9.14/README.md> |
| Older iPhone Safari has no MSE and only HLS. | "old iPhone firmwares don't support MSE technology because it competes with the HTTP Live Streaming (HLS) technology, invented by Apple" and "HLS is the worst technology for live streaming, it still exists only because of iPhones". | <https://github.com/AlexxIT/go2rtc/blob/v1.9.14/README.md> |
| Firefox MSE is H.264 only. | The Frigate table says "Firefox is h.264 only". | <https://github.com/blakeblackshear/frigate/blob/v0.18.2/docs/docs/configuration/live.md> |
| Frigate falls back to jsmpeg without go2rtc. | The table says jsmpeg "is Frigate's default without go2rtc configured" and is capped at the detect frame rate and 720p. | <https://github.com/blakeblackshear/frigate/blob/v0.18.2/docs/docs/configuration/live.md> |

**Consequence.** MSE is the only transport that combines near-real-time latency, reliable in-order
delivery, no STUN, no extra port, and an HTTP/WebSocket path through `bitternd`. HLS is the fallback
for older iPhone Safari. WebRTC is rejected for v1 because it requires 8555 and a direct peer
connection that cannot be carried by the authenticated 443 proxy.

## 7. Repository inspection

| Question | Finding | Evidence |
| --- | --- | --- |
| Which ports do the renderer and firewall goldens allow? | `lan0` allows TCP 443 for the Bittern UI, UDP 68 for DHCP, and IPv6 neighbour discovery. When Tailscale is enabled, `tailscale0` also allows TCP 443. `cam0` allows UDP 67 and UDP 123 only. TCP 1984, 5000, 8554, 8555 and 8971 are dropped on `lan0` and `cam0`; UDP 8555 is also dropped. Forwarding is default deny with no NAT. | [nftables template](../../templates/nftables/bittern.nft.tmpl) and [four-cameras golden](../../tests/render/four-cameras/expected/nftables/bittern.nft) |
| Does the Frigate container publish a port? | No. The Compose service has `network_mode: host` and no `ports:` block. The rendered config binds Frigate's internal and external listeners to `127.0.0.1:5000` and `127.0.0.1:8971`. go2rtc's 1984, 8554 and 8555 are not configured, so they use upstream defaults and rely on the host firewall. | [compose golden](../../tests/render/four-cameras/expected/compose/compose.yaml), [Frigate config golden](../../tests/render/four-cameras/expected/frigate/config.yml), [defaults](../../defaults/frigate.yaml) |
| What does the QEMU virtual camera serve? | MediaMTX 1.21.3 with RTSP on 8554, path `/cam0`, fed by a 640x360, 10 fps H.264 `testsrc` pattern. The fixture disables the MediaMTX API, metrics, pprof, playback, RTMP, HLS, WebRTC and SRT. It has no ONVIF, no HTTP endpoint and no PTZ. It is a network and RTSP path fixture, not a camera compatibility test. | [QEMU README](../../tests/qemu/README.md), [camera cloud-init](../../tests/qemu/cloud-init/camera-user-data.yaml), [QEMU config](../../tests/qemu/config.json) |
| Does any existing code proxy a camera or a Frigate endpoint? | No. There is no reverse proxy, no WebSocket upgrade, no HLS handler and no go2rtc call in `agent/`. `FrigateSnapshotChecker` fetches `http://127.0.0.1:5000/api/<camera_id>/latest.jpg` and `GET /ui/cameras/{id}/picture` re-serves that JPEG under the administrator session. That is a server-side fetch of one image, not a proxy. | [snapshot.go](../../agent/internal/ui/snapshot.go), [routes.go](../../agent/internal/ui/routes.go), `grep` for `httputil`, `ReverseProxy`, `Hijack`, `m3u8`, `go2rtc` in `agent/` |
| Is there a live view route or player today? | No. The route inventory ends at status, updates and the camera list. There is no `/ui/live`, no `<video>` element and no player asset. | [routes.go](../../agent/internal/ui/routes.go), [ui templates](../../ui/templates) |
| Does the renderer emit go2rtc configuration? | No. The current camera config uses one direct RTSP URL per camera with `detect` and `record` roles. There is no `go2rtc:` block and no `cameras.<name>.live.streams` mapping. | [frigate.go](../../agent/internal/render/frigate.go), [config golden](../../tests/render/four-cameras/expected/frigate/config.yml) |
| Does the schema carry a second stream? | No. A camera has one `rtsp_port` and one `rtsp_path`, plus a credentials reference. There is no main/sub field. | [schema](../../schema/appliance.schema.json), [types.go](../../agent/internal/config/types.go) |
| Is the live refresh finding relevant? | Yes. The status and operation polls replace whole regions with `innerHTML`. A video element inside such a region would be destroyed on every poll, losing playback, fullscreen, focus and selection. | [live refresh state loss](2026-11-26-live-refresh-state-loss.md), [UX-14](../principles.md#ux-14-live-updates-must-not-disturb-state-the-user-owns) |

The nftables template comment still says the live-video path is `OQ-03`. That is stale: OQ-03 split
the live-video half into OQ-23, and ADR 0034 resolves it. BIT-023 updates the comment and the goldens
when it touches the firewall assertion. No product file was changed in this verification.

## 8. What could not be confirmed

| Fact | Status | Why it matters |
| --- | --- | --- |
| That a specific PTZ camera accepts ONVIF commands while its stream is consumed through go2rtc. | Not confirmed by documentation or hardware. The ONVIF client uses a separate configured address, which suggests it can work, but no page promises it and no camera has been tested. | BIT-027 must prove it. The PTZ control stays unbuilt until then. |
| The real startup and steady-state latency of MSE through a `bitternd` WebSocket proxy. | Not measured. The documentation gives relative latency, not a number for this topology. | BIT-023 measures it on the QEMU fixture and the first hardware run. The ADR sets a provisional budget and requires the measurement. |
| The behavior of every camera's main and sub profiles under ONVIF. | Not confirmed. The schema has one RTSP path today, and the camera matrix does not yet carry profile evidence. | BIT-027 collects real profiles. Until then, main and sub resolve to the same path. |
| That the QEMU virtual camera can exercise MSE or HLS. | Not confirmed. The fixture is RTSP only; Frigate and go2rtc must restream it. The harness has host forwards for 11984 and 18556, but the firewall test expects them dropped. | BIT-023 needs a fixture path that proves the proxy and player, not just RTSP reachability. |
| The exact 0.18.2 behavior of every `/live/...` nginx path. | The paths are in the source and are used by the Frigate UI, but they are not documented as a stable API. | Do not depend on them. Use go2rtc's documented API. |
| A documented Frigate HTTP endpoint for PTZ movement. | None found in the v0.18.2 OpenAPI. | The PTZ command path is OQ-39. |
| That 0.18.3 contains no live view regression. | Not reviewed beyond the release notes. | The pin bump is a separate bundle change with the full test suite. |

## 9. Triage

| Finding | Destination |
| --- | --- |
| Live view can be owned by Bittern and served on 443 using go2rtc MSE/HLS and the Frigate snapshot API, with no 8555 and no embedded Frigate UI. | [ADR 0034](../adr/0034-live-view-owned-by-bittern.md) |
| The pinned 0.18.2 is behind the 0.18.3 security release for CVE-2026-123007. | Release gate. Bump the pin in a separate bundle change before any release. The live view ADR does not change the pin. |
| Frigate's documented PTZ movement path is MQTT, and product MQTT is disabled. | [OQ-39](../open-questions.md); BIT-027. |
| A gated proxy of Frigate's own UI for the long tail would reopen the control-surface and single-writer questions. | [OQ-38](../open-questions.md); no default. |
| The nftables template comment names OQ-03 for live video. | BIT-023 updates it with the golden. |
| The schema has no main/sub stream fields and the renderer emits no go2rtc block. | BIT-023 adds the renderer block and the minimal schema migration; BIT-027 adds real profile negotiation. |
| The QEMU camera is RTSP only. | BIT-023 adds a media fixture or proves the go2rtc restream path; it does not treat the fixture as a camera compatibility test. |

## Sources

All accessed 2026-11-26.

- Frigate 0.18.2 release: <https://github.com/blakeblackshear/frigate/releases/tag/v0.18.2>
- Frigate 0.18.3 release: <https://github.com/blakeblackshear/frigate/releases/tag/v0.18.3>
- Frigate 0.18.2 live view docs:
  <https://github.com/blakeblackshear/frigate/blob/v0.18.2/docs/docs/configuration/live.md>
- Frigate 0.18.2 restream docs:
  <https://github.com/blakeblackshear/frigate/blob/v0.18.2/docs/docs/configuration/restream.md>
- Frigate 0.18.2 go2rtc docs:
  <https://github.com/blakeblackshear/frigate/blob/v0.18.2/docs/docs/configuration/go2rtc.md>
- Frigate 0.18.2 cameras and PTZ docs:
  <https://github.com/blakeblackshear/frigate/blob/v0.18.2/docs/docs/configuration/cameras.md>
- Frigate 0.18.2 authentication docs:
  <https://github.com/blakeblackshear/frigate/blob/v0.18.2/docs/docs/configuration/authentication.md>
- Frigate 0.18.2 snapshots docs:
  <https://github.com/blakeblackshear/frigate/blob/v0.18.2/docs/docs/configuration/snapshots.md>
- Frigate 0.18.2 MQTT docs:
  <https://github.com/blakeblackshear/frigate/blob/v0.18.2/docs/docs/integrations/mqtt.md>
- Frigate 0.18.2 API schema:
  <https://github.com/blakeblackshear/frigate/blob/v0.18.2/docs/static/frigate-api.yaml>
- Frigate 0.18.2 troubleshooting, go2rtc:
  <https://github.com/blakeblackshear/frigate/blob/v0.18.2/docs/docs/troubleshooting/go2rtc.md>
- Frigate 0.18.2 nginx config:
  <https://github.com/blakeblackshear/frigate/blob/v0.18.2/docker/main/rootfs/usr/local/nginx/conf/nginx.conf>
- Frigate 0.18.2 auth config model:
  <https://github.com/blakeblackshear/frigate/blob/v0.18.2/frigate/config/auth.py>
- Frigate 0.18.2 auth API:
  <https://github.com/blakeblackshear/frigate/blob/v0.18.2/frigate/api/auth.py>
- Frigate 0.18.2 Dockerfile, go2rtc pin:
  <https://github.com/blakeblackshear/frigate/blob/v0.18.2/docker/main/Dockerfile>
- go2rtc 1.9.14 README: <https://github.com/AlexxIT/go2rtc/blob/v1.9.14/README.md>
- go2rtc 1.9.14 API docs: <https://github.com/AlexxIT/go2rtc/blob/v1.9.14/api/README.md>
- GHCR image index for 0.18.2: <https://ghcr.io/v2/blakeblackshear/frigate/manifests/0.18.2>
- Frigate documentation site (renders the latest release, used to locate pages only):
  <https://docs.frigate.video/>

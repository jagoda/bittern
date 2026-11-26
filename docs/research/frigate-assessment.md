# Frigate assessment

Assessed 2026-11-14. Re-verify before relying on versions, ports, flags or UI behavior. Every Frigate
figure below came from that assessment session and was not re-verified while writing this document, so
treat versions, defaults and UI details as claims to check before they become fixtures or code.

Re-checked for live view on 2026-11-26 against the 0.18.2 release and its bundled go2rtc
1.9.14. The quoted facts, URLs and access dates are in
[the live view verification note](../notes/2026-11-26-live-view-upstream-verification.md). The pin
moved to 0.18.3 on 2026-11-26; the release-note review and digest are in
[the 0.18.3 pin note](../notes/2026-11-26-frigate-0.18.3-pin.md). Two statements below are now
stale: 8971 binds loopback, not the LAN, under
[ADR 0016](../adr/0016-bittern-owns-frigate-config.md), and live video is decided by
[ADR 0034](../adr/0034-live-view-owned-by-bittern.md), not by the 0.18.2 WebRTC recommendation.
The rest of the assessment remains dated 2026-11-14.

Verdict: adopt Frigate as the NVR engine, do not fork it, pin the image by digest, and gate every
Bittern release on upstream advisories and on config-migration tests for the pinned minor.

| Question | Answer |
| --- | --- |
| Use Frigate as the engine? | Yes, see [ADR 0003](../adr/0003-frigate-as-nvr-engine.md). |
| Fork or vendor the source? | No. Use the public API, UI and config as shipped. |
| Which version? | Latest stable at build time, pinned by digest, never `latest`. v1 floor is 0.18.3, the current stable release that fixes CVE-2026-123007. |
| Exposed to the LAN? | Only 8971, authenticated and TLS. 5000 and 1984 stay on loopback. |

## Provenance and project health

- Origin: started by Blake Blackshear, public since January 2019. A single-maintainer-origin project
  that grew into a small core team around one strong lead.
- Scale (approximate): 37.5k stars, 3.8k forks, about 430 contributor entries. Contributor entries
  count anyone who ever landed a commit and overstate the active maintainer set.
- Core maintainers: Blake Blackshear, NickM-27, hawkeye217. Bus factor is the central project risk,
  since one or two people carry review, releases and security response.
- Funding: Frigate+ subscriptions at 50 USD per year fund development. No foundation, no company with
  an SLA, and no support contract Bittern could buy.
- Namespace: repository and images sit under a personal GitHub and GHCR namespace
  (`blakeblackshear/frigate`), which is a supply-chain and continuity risk, not a naming detail.
- Cadence: 0.17.0 on 2026-02-27, 0.17.2 on 2026-06-28, 0.18.0 on 2026-09-12, 0.18.2 on 2026-10-15,
  0.18.3 on 2026-11-19. Roughly two feature releases a year with patches between, and only the
  latest minor gets fixes.
  Sitting on an old pin quietly transfers upstream security risk onto us and the owner.

## Capabilities relevant to Bittern

- Detection and recording: object detection, zones, masks, retention, alerts versus detections,
  snapshot and clip review. This is the value we resell and the reason not to build our own pipeline.
- Streaming: go2rtc provides restream, WebRTC, MSE and HLS. WebRTC makes live view feel immediate, and
  restreaming reduces camera load.
- 0.17 added the Add Camera Wizard; 0.18 completed the settings UI, and most settings apply without a
  restart. `config.yml` remains the source of truth, and config validation exists as a CLI and an HTTP
  endpoint so Bittern can check a rendered or UI-edited config before restarting the container.
- Onboarding gap: the wizard does no ONVIF LAN discovery, and upstream closed the request as not
  planned (issue #21914). The user must already know the camera's address. Bittern therefore owns
  discovery: ONVIF WS-Discovery where the camera answers it and unicast ONVIF and RTSP probing from
  `cam0` as the reliable path, then DHCP reservations after adoption. A camera at a static vendor
  fallback address such as `192.168.1.64` uses the manual URL fallback.

## Security posture

Frigate assumes a trusted LAN and an operator who understands its ports. A non-technical owner
provides neither, so each exposure becomes a control or an accepted risk with an owner.

| Exposure | Reality (2026-11-14) | Bittern control | Owner |
| --- | --- | --- | --- |
| 8971/tcp | Authenticated UI and API over TLS | Only ingress allowed, from LAN and tailnet | network |
| 5000/tcp | Unauthenticated admin by design | Bind 127.0.0.1, never publish, firewall drop | runtime |
| 1984/tcp | go2rtc, unauthenticated | Loopback only, firewall drop, absent from templates | runtime |
| Container user | Runs as root | Accepted risk; minimal mounts, no Docker socket | runtime |
| Admin password | Printed in logs at start | Redact in log views and support bundles | agent |
| Auth/RBAC bugs | 2026 cluster, viewer-to-root RCE fixed in 0.18.2 | Pin floor 0.18.3, no viewer accounts | release |
| NGINX2Root | CVE-2026-123007, unauthenticated HTTP/2 RCE as root, fixed in 0.18.3 | Pin floor 0.18.3, loopback listeners, firewall drop | release |
| Advisories | Published upstream, none recorded as actively exploited here | Advisory gate before every release | release |

Frigate runs as root in its container because device access and the legacy port push it there. We
shrink the blast radius with a minimal root filesystem, no Docker socket, no host mounts beyond
config, media and devices, and a host firewall that does not trust the container. Sidecar services are
attack surface too, as the go2rtc exec RCE showed, so the whole container is untrusted from the host.
The 2026 cluster matters more than its individual bugs: auth paths in a fast-moving UI get less
scrutiny than streaming code, and an auth proxy in front of 5000 would hide that, not fix it.

## Privacy posture

| Feature | Upstream default | Data leaving the appliance | Bittern default |
| --- | --- | --- | --- |
| Anonymous analytics | Off, opt-in | Daily JSON; no names, labels, IPs or credentials; kept 60 days | Off |
| Version check | On, calls `api.github.com` | Version and source IP to GitHub | Explicit choice (OQ-12), default off |
| Model downloads | One-time at start | Model requests to upstream hosts | Prefetch where licenses allow |
| STUN for WebRTC | On, `stun.l.google.com` | Public IP and timing to Google | Only for remote WebRTC; LAN and tailnet first |
| Push notifications | Off until configured | Subscription metadata to browser push services | Off; document the trade-off |
| Frigate+ | Off | Snapshots and video, kept indefinitely unless erased, stored in the US | Not supported by default |
| GenAI descriptions | Off | Thumbnails to OpenAI, Gemini or Azure; local Ollama possible | Off; local-only documented, not v1 |
| Face recognition, LPR | Off | Local biometric data, stays on device | Off; warn before enabling |

Version check is the default that surprises people: it phones GitHub, and it is disabled with
`telemetry.version_check: false`. Set it explicitly and show the consequence in the UI rather than
inheriting whatever upstream ships. Local-first is not offline, so the appliance also needs a visible
"what leaves this box" page and one place to switch each channel off.

## Licensing and trademark

- Frigate source is MIT: embedding, modifying and redistributing the container is allowed with
  attribution and the license text. go2rtc is MIT as well.
- Frigate, Frigate NVR and Frigate+ are trademarks of Frigate, Inc. The policy requires forks and
  derived products to rename and remove the logo. Bittern complies: independent name, no logo,
  independent-project disclaimer.
- Frigate+ models are licensed only for cameras the licensee owns or operates, and the terms prohibit
  customer-site deployment and hardware bundling without a commercial license. Handing an appliance
  to a relative or client is not clearly "own or operate", so Frigate+ stays out of the default
  image; see OQ-07.
- Default bundled detection models are Apache-2.0. Verify each model before redistributing it in our
  image and keep [THIRD_PARTY_NOTICES.md](../../THIRD_PARTY_NOTICES.md) current per release.
- ffmpeg builds shipped with Frigate are GPL, so distribution means honouring source-offer
  obligations for those binaries. That does not make Bittern's code GPL, but the combination needs a
  license review before the first release.
- Bittern's own license is MIT as of 2026-11-14
  ([ADR 0013](../adr/0013-branding-and-licensing-constraints.md)). The MIT grant covers Bittern's code
  and documentation, not the Bittern name or logo.

## Operational fit

- Image: `ghcr.io/blakeblackshear/frigate`, pinned by tag plus digest, mirrored to a registry Bittern
  controls if we can, with an SBOM per release.
- Networking: host networking is required for mDNS, ONVIF discovery, WebRTC and DHCP-dependent camera
  behaviour, and it removes Docker NAT from the picture. The host firewall, not Docker, is therefore
  the boundary, and Docker address pools must avoid LAN, camera and Tailscale ranges.
- Hardware: `/dev/dri` for VAAPI/QSV decode and detect, `/dev/bus/usb` for a Coral USB, `group_add`
  with the numeric Debian `video` (44) and `render` (992) GIDs, `shm_size` sized for camera count
  and resolution.
- Storage: recordings and snapshots on the data disk, SQLite state on fast local storage, config in a
  small backed-up directory. Retention belongs in the UI, because disk exhaustion is the likeliest
  field failure.
- Upgrades: container replacement plus config and database migration at start. Frigate migrates its
  own database, so our transaction backs up config and database, then verifies health afterwards.
- HA add-on prior art: the community add-on runs under the Supervisor with config at
  `/addon_configs/ccab4aaf_frigate/config.yml` and needs protection mode off (Full Access) for device
  access. Useful for config paths and pitfalls, not a substrate: HAOS gives no control over two NICs,
  firewall, update timing or first run, and the owner must learn Home Assistant. See OQ-08.

## Implications for Bittern

1. Advisory gate: block releases with an open high or critical advisory against the pinned version,
   and record any exception.
2. Pin 0.18.3 or later by digest, mirror images, and treat tracking the latest minor as a release gate.
3. Never publish 5000 or 1984. Template `listen.internal: 127.0.0.1:5000` and
   `listen.external: <lan-ip>:8971`, and drop both at the host firewall.
4. Allow only 8971, plus 8555 where WebRTC needs it, from LAN and tailnet; default-deny elsewhere.
5. Decide version check explicitly (OQ-12), default off unless updates need it, and show it in the UI.
6. Disable analytics, Frigate+, GenAI and push in default templates; each is opt-in with a written
   data-flow note.
7. Back up `config.yml` before UI writes, mirror camera inventory, and detect drift rather than
   overwriting either side.
8. Maintain a camera compatibility matrix covering gateway requirements, NTP option 42, ONVIF auth,
   discovery layer and RTSP path under OQ-10 and the evidence rules in
   [ADR 0024](../adr/0024-onvif-discovery-levels.md) and [the camera matrix](../notes/camera-matrix.md).
9. Implement layered ONVIF discovery on `cam0` (BIT-008), with WS-Discovery where it works, unicast
   ONVIF and RTSP probing as the reliable path, and manual address plus RTSP credentials as the
   always-available baseline, because upstream does not.
10. Test config and database migration for every Frigate minor in QEMU before shipping a bundle.
11. Keep Frigate+ out of the default image and out of hardware bundles without a commercial license.
12. Redact the admin password from logs and support bundles, and rotate it at first boot.

## Alternatives considered

Detail and rejection reasons are in [options-considered.md](options-considered.md).

| Option | One-line verdict |
| --- | --- |
| Frigate | Chosen: best detection, streaming and UI for the effort. |
| Scrypted NVR | Paid per camera, hostile to a give-away appliance. |
| ZoneMinder | Mature but dated and CPU-heavy. |
| Shinobi | Small team, slow cadence, uncertain future. |
| Viseron, Moonfire, OpenNVR | Toolkit, no AI, and new plus AGPL. |
| Build our own | Years of work to reach Frigate's streaming quality. |

## Sources

Upstream pages relied on, with the caveat that specific URLs were not re-checked while writing:

- Documentation: <https://docs.frigate.video>; project site: <https://frigate.video>
- Repository, releases, security policy and advisories: <https://github.com/blakeblackshear/frigate>
  (see the releases, `security/policy` and `security/advisories` pages)
- ONVIF discovery issue, closed as not planned:
  <https://github.com/blakeblackshear/frigate/issues/21914>
- HA add-on repository: <https://github.com/blakeblackshear/frigate-hass-addons>
- Frigate docs, camera setup, configuration reference, hardware acceleration and storage pages
  (specific URLs not confirmed here).
- Frigate+ terms of service and model license (URL not confirmed), and the Frigate trademark policy,
  linked from the project site and repository.

All figures, dates and defaults are as recorded on 2026-11-14 and were not re-verified here; where a
public URL could not be confirmed, the source is described rather than invented.

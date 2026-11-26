# Upstream register

This is the living register for local prototypes, upstream promotion, and adoption. Prototyping
locally is the default; promotion is a judgement call, and an accepted upstream change must replace
the local implementation. Add or update a row when a prototype starts, when promotion is proposed,
and when the upstream version is adopted. An upstream issue or PR link is optional; empty cells are
valid. The policy is [ADR 0020](adr/0020-local-prototypes-upstream-destination.md).

Track each prototype as a TODO in this repository. The TODO records the prototype, why it exists,
whether promotion is intended, and the condition to promote or retire it. Record the TODO in the
change that adds the prototype, and point the row at it. Keep the table short and review it at every
phase gate.

Last reviewed: 2026-11-26. The Frigate and go2rtc pins were checked against
`defaults/frigate.yaml` and the 0.18.3 image on that date. The security release findings are in
[the 0.18.3 pin note](notes/2026-11-26-frigate-0.18.3-pin.md), and the live view facts are in
[the live view verification note](notes/2026-11-26-live-view-upstream-verification.md).
The HTMX pin was last checked on 2026-11-21.

| Project | What Bittern uses | Local prototype or workaround | Promotion status | Upstream link (optional) | Adoption status | Revisit | Owner |
| --- | --- | --- | --- | --- | --- | --- | --- |
| Frigate | Pinned upstream image, generated `config.yml`, Compose project, HTTP API and UI. Live view uses go2rtc media and the snapshot API through `bitternd` ([ADR 0034](adr/0034-live-view-owned-by-bittern.md)). | None in product; ONVIF discovery is a Bittern feature (BIT-008), not a Frigate patch. | No Bittern proposal; upstream declined generic ONVIF discovery as not planned. | [#21914](https://github.com/blakeblackshear/frigate/issues/21914) (closed as not planned). | No local prototype; pinned 0.18.3, digest verified 2026-11-26. 0.18.3 is the current stable security release published 2026-11-19 and fixes CVE-2026-123007. | If upstream reverses the ONVIF decision, a supported ONVIF API appears, or a later pin changes live view behavior. | runtime |
| go2rtc | RTSP restream, MSE and HLS inside the Frigate container; WebRTC is unused in v1. | None recorded. | No prototype. | | No local prototype; consumed through Frigate 0.18.3 with go2rtc 1.9.14; MSE and HLS proxied through `bitternd` per ADR 0034. | When hardware testing finds a generic streaming issue or the MSE/HLS proxy needs an upstream fix. | runtime |
| HTMX | Vendored 2.0.11 for partial UI updates; every flow works without JavaScript. | None recorded. | No prototype. | | No local prototype; consumed at 2.0.11, vendored copy checked 2026-11-21. | When a UI bug affects other HTMX users. | frontend |
| mergerfs | Planned pool over ext4 members for recordings ([ADR 0018](adr/0018-recording-capacity-pool.md)); not implemented yet. | None yet; pool assembly is a BIT-011 follow-up. | No prototype. | | No local prototype; not implemented yet. | When pool assembly or free-space behavior exposes a generic issue. | image |
| Debian | Debian 13 base, mkosi image, systemd, Docker and Debian packages. | None recorded. | No prototype. | | No local prototype; consumed at Debian 13. | When a packaging gap affects other Debian users. | image |
| Coral driver | USB Coral detector through Frigate; `/dev/bus/usb` passed to the container. | None recorded. | No prototype. | | No local prototype; consumed via Frigate. | When hardware testing finds a generic driver issue. | image |
| Hailo driver | Hailo-8 detector through Frigate; `/dev/hailo0` passed to the container. | None recorded. | No prototype. | | No local prototype; consumed via Frigate. | When hardware testing finds a generic driver issue. | image |

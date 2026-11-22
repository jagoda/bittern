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

Last reviewed: 2026-11-21. The Frigate and HTMX repository pins were checked against
`defaults/frigate.yaml` and `ui/static/htmx-2.0.11.min.js` on that date; upstream release notes were
not re-checked.

| Project | What Bittern uses | Local prototype or workaround | Promotion status | Upstream link (optional) | Adoption status | Revisit | Owner |
| --- | --- | --- | --- | --- | --- | --- | --- |
| Frigate | Pinned upstream image, generated `config.yml`, Compose project, HTTP API and UI. | None in product; ONVIF discovery is a Bittern feature (BIT-008), not a Frigate patch. | No Bittern proposal; upstream declined generic ONVIF discovery as not planned. | [#21914](https://github.com/blakeblackshear/frigate/issues/21914) (closed as not planned). | No local prototype; pinned 0.18.2, repository pin checked 2026-11-21. | If upstream reverses the decision or a supported ONVIF API appears. | runtime |
| go2rtc | RTSP restream and WebRTC or HLS inside the Frigate container. | None recorded. | No prototype. | | No local prototype; consumed through Frigate. | When hardware testing finds a generic streaming issue. | runtime |
| HTMX | Vendored 2.0.11 for partial UI updates; every flow works without JavaScript. | None recorded. | No prototype. | | No local prototype; consumed at 2.0.11, vendored copy checked 2026-11-21. | When a UI bug affects other HTMX users. | frontend |
| mergerfs | Planned pool over ext4 members for recordings ([ADR 0018](adr/0018-recording-capacity-pool.md)); not implemented yet. | None yet; pool assembly is a BIT-011 follow-up. | No prototype. | | No local prototype; not implemented yet. | When pool assembly or free-space behavior exposes a generic issue. | image |
| Debian | Debian 13 base, mkosi image, systemd, Docker and Debian packages. | None recorded. | No prototype. | | No local prototype; consumed at Debian 13. | When a packaging gap affects other Debian users. | image |
| Coral driver | USB Coral detector through Frigate; `/dev/bus/usb` passed to the container. | None recorded. | No prototype. | | No local prototype; consumed via Frigate. | When hardware testing finds a generic driver issue. | image |
| Hailo driver | Hailo-8 detector through Frigate; `/dev/hailo0` passed to the container. | None recorded. | No prototype. | | No local prototype; consumed via Frigate. | When hardware testing finds a generic driver issue. | image |

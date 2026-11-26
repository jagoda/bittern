# Media surfaces: live view, playback and detection review

- Date: 2026-11-14
- Updated: 2026-11-26. [OQ-23](../open-questions.md) is resolved by
  [ADR 0034](../adr/0034-live-view-owned-by-bittern.md): live view is a Bittern page on the
  authenticated 443 surface, using go2rtc MSE with HLS fallback through `bitternd`, and no WebRTC
  or 8555 exposure. The proposed shape below is historical. The next step is BIT-023.
- Related: [ADR 0016](../adr/0016-bittern-owns-frigate-config.md),
  [ADR 0019](../adr/0019-ui-stack.md), [OQ-23](../open-questions.md),
  [implementation plan](../plan/implementation-plan.md), [status contract](../status.md),
  [Frigate renderer](2026-11-14-frigate-renderer.md)

This note records the state of the browser media surfaces. It is not an ADR, a ticket or a plan
change. The detail stays deferred until after the first hardware test.

## What is true now

Detection and recording are Frigate's job and remain unmodified. Bittern selects the detector and
devices through `hardware` and renders the config. Detection review is the Frigate event database,
not a Bittern feature.

No browser surface exists for live view, playback, PTZ or detection review. The wizard and status
page from BIT-007 are the only pages. M2 uses a snapshot, not live video.

[ADR 0016](../adr/0016-bittern-owns-frigate-config.md) makes the Bittern UI the only browser
surface, so these views must be built rather than linked. Frigate's UI stays loopback-only and is a
support path.

## Proposed shape

The working proposal is an authenticated reverse proxy in `bitternd` in front of Frigate's loopback
API. It would serve snapshots, event JSON, thumbnails, clips and recording segments, so the browser
keeps one origin and one authentication boundary.

For live video, [OQ-23](../open-questions.md) decides between direct WebRTC on 8555 with go2rtc
authentication and HLS proxied through `bitternd`. The proxy above covers the HLS path; direct
WebRTC would add a separate listener and network path.

## Recommended v1 scope

Events, clips and snapshots come first. Continuous playback with a scrubbing timeline is a second
step, and PTZ and audio are later.

The timeline is a project of its own: retention-aware segment indexing, seeking, stitching across
recording boundaries, and time-zone handling. The MVP needs review and live view, not continuous
playback, so the first surface should not wait for it.

## Costs and risks

- Proxying video through `bitternd` adds bandwidth and latency, and puts load on the daemon.
- Direct WebRTC on 8555 adds a second network surface and go2rtc authentication work.
- The exact recording and HLS endpoints of the pinned 0.18.2 image must be verified against that
  image, not assumed from older documentation or the current UI.

## What is deferred

No ADR, no ticket and no OQ beyond [OQ-23](../open-questions.md). The detail waits for the first
hardware test and evidence from the pinned API.

## Next concrete step

After M2, run a short spike against the pinned Frigate API to confirm the endpoints, then make the
OQ-23 decision and open a review-surface ticket.

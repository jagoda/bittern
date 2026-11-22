# 0024. ONVIF discovery levels and field interoperability

*ONVIF is an optimization, never a requirement: discovery is layered, every layer falls back to the
next, and the manual address plus RTSP credentials path always works.*

- Status: Accepted
- Date: 2026-11-15
- Deciders: maintainer
- Related: [ADR-0003](./0003-frigate-as-nvr-engine.md),
  [ADR-0005](./0005-dual-nic-isolated-camera-network.md),
  [ADR-0010](./0010-security-and-privacy-posture.md),
  [ADR-0016](./0016-bittern-owns-frigate-config.md),
  [ADR-0019](./0019-ui-stack.md),
  [ADR-0020](./0020-local-prototypes-upstream-destination.md),
  [ADR-0022](./0022-community-functionality-ux-wins.md),
  [ADR-0023](./0023-camera-native-ui-proxy.md),
  [open questions](../open-questions.md), [Frigate assessment](../research/frigate-assessment.md),
  [implementation plan](../plan/implementation-plan.md), [camera matrix](../notes/camera-matrix.md)

## Context

Cameras are the appliance's input, and owners buy whatever models are available. ONVIF is the
standard intended to make those cameras interchangeable, but its field implementations vary widely.
Two cameras that both claim ONVIF can differ in discovery behavior, SOAP version, authentication,
profile support, media URI shape, response timing, and how much of the protocol they actually
implement. A camera may have ONVIF disabled, expose only IPv4, answer discovery but not media, or
return a partial response after a long delay.

Frigate's upstream wizard does no ONVIF LAN discovery, and the request was closed as not planned.
Bittern therefore owns discovery and adoption ([ADR-0016](./0016-bittern-owns-frigate-config.md)),
using community protocols and implementations rather than a private protocol
([ADR-0022](./0022-community-functionality-ux-wins.md)). The camera segment is isolated on `cam0`
with no route to `lan0`, the WAN, or the tailnet
([ADR-0005](./0005-dual-nic-isolated-camera-network.md)). Camera endpoints and camera-supplied
addresses are untrusted, and camera credentials are secrets.

BIT-008 already requires ONVIF discovery, unicast probing, and a manual URL fallback. Without an
explicit support philosophy, every field quirk becomes a separate debate, a camera that fails
discovery looks unsupported, and a workaround can quietly become a support claim. The product needs
one rule for what discovery promises, what it does not, and how a failure is presented.

## Decision

**1. ONVIF is an optimization, never a requirement.**

The baseline adoption path is a manual camera address plus RTSP credentials. It must work when every
discovery and probe layer fails, when the camera has ONVIF disabled, and when the owner already knows
the stream URL. The manual path uses the same validation, optional RTSP probe, and atomic adoption
transaction as a discovered camera. No camera model is supported only through ONVIF.

**2. Discovery is layered, and each layer degrades to the next.**

The layers are evidence sources, not a certification ladder. The owner can enter the manual path at
any point, and a failure at one layer is shown in the UI rather than treated as a dead end.

| Layer | Evidence source | What it can contribute |
| --- | --- | --- |
| Baseline | Manual address and RTSP credentials from the owner | The adoption path that always works |
| 1 | dnsmasq leases and the ARP or neighbour table on `cam0` | Address, MAC, lease hostname and vendor OUI |
| 2 | ONVIF WS-Discovery on `cam0`, with unicast probing where multicast is filtered or disabled | Device endpoint, XAddrs, scopes, device type |
| 3 | ONVIF Device and Media services where the camera implements them | Model, firmware, profiles, media and snapshot URIs, authentication requirements |
| 4 | RTSP `OPTIONS` and `DESCRIBE` against the candidate endpoint | Reachability, authentication response, stream and codec evidence |
| Fallback | Manual form for address, port, path and credentials | Adoption when discovery or probing is incomplete or wrong |

A layer is skipped when the camera does not answer it, not treated as a product failure. The wizard
shows which layer produced each value and why a layer produced nothing. Partial results are useful:
an address without a media URI still gives the owner a starting point, and an RTSP URI without a
profile still proves the stream path. Discovery is read-only: only adoption writes the camera entry,
its reservation, and its secret, and that write is atomic.

**3. Version and vendor variance is expected and bounded.**

The implementation tolerates, at minimum: WS-Discovery variants and filtered multicast, SOAP 1.1
and 1.2 differences, WS-Security UsernameToken variants, HTTP Basic or Digest authentication, no
authentication, multiple media profiles, IPv4-only devices, cameras with ONVIF disabled, and slow or
partial responses. It does not assume that every service, profile, or optional field exists.

Every layer has a bounded timeout and an overall wizard budget. Probes have per-device concurrency
limits and work on a bounded set of candidates from discovery, the lease table, or the owner. A slow
camera cannot block the wizard, repeated discovery is rate limited, and results stream in as they
arrive. Cancellation stops outstanding probes.

**4. The owner sees the evidence and controls the substitution.**

The discovery list shows the address, MAC, model and firmware when reported, the layer that produced
each value, the profile and RTSP path when available, whether authentication is required, and the
reason for an unavailable field. A failed probe result is evidence, not a hidden retry.

The owner can correct the address, port, path, and credentials before adoption. Bittern does not
silently replace an owner-entered address or stream path with an ONVIF-derived value the owner did
not see. Adoption remains one atomic write of the camera entry, its DHCP reservation, and its secret;
a failed or abandoned adoption changes nothing.

**5. A tested device matrix under OQ-10 is the support statement.**

The matrix records, for each tested camera: model, firmware, discovery method, authentication,
profile, RTSP path, and the evidence or test that proves the row. The columns and rules are in
[the camera matrix](../notes/camera-matrix.md). Field quirks become rows and tests, not folklore. A
quirk that needs a code, template, or renderer change gets a test or a named follow-up in the same
change.

OQ-10 remains open. The matrix records what has been tested; it does not certify a model, promise
future firmware behavior, or replace the manual fallback. The product states the tested matrix and
the fallback instead of claiming ONVIF conformance.

**6. Discovery stays on `cam0` and cannot become an SSRF proxy.**

All discovery, probing, and ONVIF traffic uses `cam0` only. No discovery packet, media request, or
camera-supplied endpoint goes to `lan0`, the WAN, the tailnet, loopback, or a link-local address. A
discovered endpoint is validated against the camera segment and the allowed scheme and port before
Bittern connects to it. Device-supplied redirects, XAddrs, and media URIs cannot move the request
off `cam0`, and Bittern never exposes a generic URL fetch or proxy.

Discovery probes are unauthenticated. Credentials are used only for the selected camera's ONVIF or
RTSP check after the owner enters them, and only against the address the owner sees. Credentials and
complete stream URLs never enter discovery records, status, logs, support bundles, or probes sent to
other devices. Redaction is part of the tests, not a later cleanup.

**7. Use a community ONVIF implementation or the standard protocol.**

Bittern uses a maintained ONVIF implementation or the standard protocol through
[ADR-0022](./0022-community-functionality-ux-wins.md). There is no private discovery protocol and no
fork of an ONVIF library. A library choice needs the usual lifecycle, license, security, pinning,
[THIRD_PARTY_NOTICES.md](../../THIRD_PARTY_NOTICES.md), and SBOM review before it enters the product
path. If upstream Frigate later adds discovery, the local layer is removed and the upstream path is
adopted.

## Consequences

### Positive

- A camera with broken, partial, or disabled ONVIF remains adoptable through the manual path, so the
  product does not turn a protocol failure into an unsupported device.
- Layered evidence makes failures diagnosable. The owner and support can see whether the camera
  answered discovery, device services, RTSP, or none of them.
- The same rules cover vendor variance and keep field knowledge in a tested matrix instead of in
  issue comments and support memory.
- The `cam0` endpoint validation and credential rules contain the SSRF and secret-leak risks that
  discovery adds.
- No certification program is required. The tested matrix plus the manual fallback is an honest
  support statement.

### Negative / costs

- Multiple layers mean more code, fixtures, timeouts, and failure paths than a single ONVIF client.
- ONVIF evidence is often incomplete. Some cameras will never report a model, profile, or media URI,
  and the matrix will show gaps rather than a clean list.
- The manual path still asks the owner for an address and credentials, so a camera that cannot be
  discovered is not zero-touch.
- Bounded probes and partial results add UI complexity: the wizard must explain layers, missing
  fields, and failures without overwhelming a non-technical owner.
- The matrix grows with every firmware variation and needs a test or a field record to stay honest.
- A community library can change behavior or be abandoned, so the pin and the fallback need periodic
  review.
- Endpoint validation, timeout handling, and redaction are security-sensitive and need tests on
  every change.

### Follow-ups

- BIT-008 implements the levels, evidence, manual baseline, endpoint validation, bounded probes, and
  the QEMU virtual camera cases.
- Maintain [the camera matrix](../notes/camera-matrix.md) under [OQ-10](../open-questions.md), seed
  it with the first field camera and the QEMU protocol fixtures, and add every field quirk as a row
  and a test.
- Add protocol fixtures for WS-Discovery variants, SOAP 1.1 and 1.2, WS-Security and HTTP auth,
  partial responses, and a camera with ONVIF disabled.
- Add a packet capture acceptance check that proves no discovery or probe traffic leaves `cam0`, and
  security tests for endpoint validation, redirect refusal, and credential redaction.
- Add hardware-in-the-loop coverage for each tested matrix row; use the matrix to state support, not
  a conformance claim.
- Update the wizard and status copy to name the discovery layer and the fallback at phone and
  desktop widths.
- Revisit the library or protocol choice when ONVIF changes or upstream Frigate adds discovery, and
  remove the local layer when the upstream path is adopted.

## Alternatives considered

- **Require ONVIF or certify cameras.** Rejected: field implementations vary, certification adds cost
  and liability, and it would exclude cameras that work perfectly over RTSP.
- **Manual address and RTSP only.** Rejected: it is a reliable baseline, but it gives up the guided
  discovery the product needs for a non-technical owner and leaves the camera's network address as
  the first hurdle.
- **A single WS-Discovery implementation with no fallbacks.** Rejected: multicast is often filtered,
  some cameras disable ONVIF, and a discovery failure would look like an unsupported camera.
- **Trust ONVIF-derived values without showing them.** Rejected: a wrong media URI or profile would
  be adopted silently, and the owner could not tell whether the camera or the appliance was wrong.
- **Probe the whole network or follow any discovered endpoint.** Rejected: it breaks the `cam0`
  isolation, risks an SSRF path to the appliance or LAN, and can scan networks the owner did not
  select.
- **Build a private ONVIF stack tuned to each camera.** Rejected by
  [ADR-0022](./0022-community-functionality-ux-wins.md): it duplicates the protocol, carries a
  maintenance and security burden, and turns field quirks into permanent private code.
- **Claim ONVIF conformance from a few successful cameras.** Rejected: it overstates the evidence.
  The tested matrix and the manual fallback are the honest statement.

## References

- [ADR-0003: Frigate as NVR engine](./0003-frigate-as-nvr-engine.md)
- [ADR-0005: Dual-NIC isolated camera network](./0005-dual-nic-isolated-camera-network.md)
- [ADR-0010: Security and privacy posture](./0010-security-and-privacy-posture.md)
- [ADR-0016: Bittern owns the control surface and the Frigate
  config](./0016-bittern-owns-frigate-config.md)
- [ADR-0019: UI stack and TLS](./0019-ui-stack.md)
- [ADR-0020: Local prototypes, upstream destination](./0020-local-prototypes-upstream-destination.md)
- [ADR-0022: Community functionality, UX wins](./0022-community-functionality-ux-wins.md)
- [ADR-0023: Camera native UI through an authenticated proxy](./0023-camera-native-ui-proxy.md)
- [Open questions](../open-questions.md), including OQ-10
- [Frigate assessment](../research/frigate-assessment.md)
- [Implementation plan](../plan/implementation-plan.md),
  [BIT-008](../plan/implementation-plan.md#bit-008-camera-discovery-and-adoption-on-cam0)
- [Camera matrix](../notes/camera-matrix.md)
- [Extension points for device I/O and discovery](../notes/2026-11-14-extension-points.md)
- ONVIF specifications: <https://www.onvif.org/profiles/specifications/>
- ONVIF discovery: <https://www.onvif.org/specs/core/ONVIF-Core-Specification.pdf>
- Frigate ONVIF discovery issue, closed as not planned:
  <https://github.com/blakeblackshear/frigate/issues/21914>
- RTSP: <https://www.rfc-editor.org/rfc/rfc2326>

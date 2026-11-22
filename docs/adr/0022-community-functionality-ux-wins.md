# 0022. Community functionality, UX wins

*Prefer the existing community component, protocol, or package; when its user experience conflicts
with the product, UX wins through the smallest Bittern layer, never a fork.*

- Status: Accepted
- Date: 2026-11-15
- Deciders: maintainer
- Related: [ADR-0003](./0003-frigate-as-nvr-engine.md),
  [ADR-0013](./0013-branding-and-licensing-constraints.md),
  [ADR-0016](./0016-bittern-owns-frigate-config.md),
  [ADR-0019](./0019-ui-stack.md),
  [ADR-0020](./0020-local-prototypes-upstream-destination.md),
  [upstream register](../upstream.md), [vision](../vision.md), [architecture](../architecture.md),
  [implementation plan](../plan/implementation-plan.md), [UI README](../../ui/README.md),
  [Frigate assessment](../research/frigate-assessment.md)

## Context

Bittern is a thin appliance layer around many projects it does not own. Frigate does detection,
recording, review and streaming; Debian, systemd and Docker provide the OS, service management and
container runtime; HTMX provides partial page updates; ONVIF, WS-Discovery and RTSP provide camera
discovery and streaming; mergerfs joins the recording drives; and Debian packages such as qrencode
provide small utilities. A future kiosk, if approved, would add a browser engine and a display
session. The current state and pins are in [the Frigate assessment](../research/frigate-assessment.md)
(dated 2026-11-14) and [the upstream register](../upstream.md).

Each community project has users beyond Bittern, its own maintainers, release cadence, security
process and license. Reimplementing what they already do would duplicate that work, add a second
maintenance and security burden, and slow the appliance down. Bittern is pre-implementation and
cannot carry a private copy of every subsystem it touches.

Using a community component as shipped has the opposite failure. Its user experience was designed
for a different user. Frigate's UI suits someone who understands NVR configuration; Bittern's owner
is a non-technical person who needs first run, camera adoption, status, updates and recovery without
a shell or a config file. Generic protocols have no product UI at all, and Debian or systemd errors
are accurate but not written for the owner. Accepting that UX because the code exists would make the
appliance harder to hand over than building a small product layer.

The project needs one rule that covers both failures: when to reuse, when to configure, when to wrap,
and what happens when the community option works but the experience does not.
[ADR-0020](./0020-local-prototypes-upstream-destination.md) already decides how a local prototype
becomes upstream work; this ADR decides whether to use the community component and how a UX conflict
is resolved. It does not reopen the no-fork decisions in
[ADR-0003](./0003-frigate-as-nvr-engine.md) and
[ADR-0020](./0020-local-prototypes-upstream-destination.md), or the ownership boundary in
[ADR-0016](./0016-bittern-owns-frigate-config.md).

The first applications are the open tickets. Camera adoption is specified to use ONVIF discovery and
RTSP probes instead of a private discovery protocol, with the wizard and adoption transaction
supplying the product UX
([BIT-008](../plan/implementation-plan.md#bit-008-camera-discovery-and-adoption-on-cam0)). The setup
token QR is specified to use the Debian qrencode package instead of a custom encoder, with Bittern
owning the console block, URL fragment, short code and no-copy flow
([BIT-019](../plan/implementation-plan.md#bit-019-setup-token-qr-exchange)). Both are examples of the
rule, not exceptions to it.

## Decision

**1. Prefer the existing community component, protocol, or package.**

Use a maintained upstream project, a Debian package, or a standard protocol before writing new
Bittern code. Work through the options in order:

| Step | What Bittern does | Example |
| --- | --- | --- |
| Use as-is | Consume the documented API, config, protocol or package without modification. | The pinned Frigate image, its config format and its API. |
| Configure | Set the supported options and generated configuration for the appliance. | systemd units, networkd, Docker Compose, Frigate `config.yml`. |
| Wrap | Add the smallest product-specific layer that supplies the missing UX or boundary. | The Bittern wizard around ONVIF discovery, or the setup-token QR console block around qrencode. |
| Improve upstream | Promote a generic improvement through [ADR-0020](./0020-local-prototypes-upstream-destination.md). | A candidate Frigate, go2rtc or Debian fix that other users need. |
| Replace | Write a custom component only when the community option cannot meet the requirement. This is the last resort and needs its own ADR. | A new subsystem with no usable community option. |

The wrapper may handle product vocabulary, navigation, evidence, validation, error recovery and
accessibility. It must not reimplement the domain work that the community component already does.
Prefer a stable API or protocol over parsing human-readable output.

**2. UX wins a conflict with the community option.**

If the community option meets the functional requirement but its UX is poor, confusing, unsafe,
inaccessible, or inconsistent with the product, UX wins. Build the smallest product-specific layer
that fixes the UX, keep the upstream component doing the hard work, record the gap, and consider
upstreaming the improvement under [ADR-0020](./0020-local-prototypes-upstream-destination.md).

A UX conflict is not permission to fork. The layer is product-specific code in Bittern's repository,
using the documented upstream boundary. It is reviewed and tested like any other product code. When
upstream later fixes the same gap, delete the local layer and adopt the upstream version, as ADR 0020
requires.

The UX gate is part of the decision. The owner must be able to complete the affected task from the
product UI at phone and desktop widths, without a shell, a config edit, or copying a secret between
devices, unless no reasonable alternative exists. Accessibility, the no-JavaScript fallback where
[ADR-0019](./0019-ui-stack.md) requires it, and clear recovery from failure are acceptance criteria,
not a later polish pass.

**3. A fork is unacceptable; a custom replacement is the last resort.**

Do not fork, patch, vendor a modified copy of, or maintain a private source tree for a community
component. Do not duplicate its functionality behind a Bittern interface. This restates
[ADR-0003](./0003-frigate-as-nvr-engine.md) and [ADR-0020](./0020-local-prototypes-upstream-destination.md).

A custom replacement is allowed only when the community option cannot meet the requirement even
with configuration and a wrapper. It needs its own Accepted ADR before implementation. That ADR must
show what was tried, why the community option and the wrapper are insufficient, the license and
security update path, the maintenance owner, and the tests that will prove the replacement.

**4. Weigh the whole lifecycle, not only development speed.**

Every choice considers maintenance burden, license compatibility and obligations, security update
path, upstream activity and lifecycle, API or protocol stability, testability, resource footprint,
and the owner's UX. A component that is quick to add but unmaintained, incompatible with Bittern's
license policy, or impossible to update safely is not the cheaper option.

Pin versions and container digests, record licenses in [THIRD_PARTY_NOTICES.md](../../THIRD_PARTY_NOTICES.md),
keep upstream notices intact, and produce the SBOM required by
[ADR-0012](./0012-testing-and-release-gates.md) and
[ADR-0013](./0013-branding-and-licensing-constraints.md). A dependency with an unclear security
response or a lifecycle that cannot be tracked does not enter the product path.

**5. Bittern owns the product UX; Frigate owns the video work.**

Frigate is used unmodified and does the NVR work: capture, detection, recording, review and
streaming. Bittern owns the appliance control surface: first run, camera adoption, generated
configuration, status, updates, logs, support and reset
([ADR-0016](./0016-bittern-owns-frigate-config.md)). Frigate's UI remains a deliberate support and
diagnostic path, not the product surface. Bittern does not embed, restyle or fork it, and it does not
copy its screens.

The same boundary applies to every community component. The upstream project supplies the engine,
the protocol or the utility; Bittern supplies the non-technical path around it. A product screen that
merely exposes an upstream screen unchanged is not automatically acceptable. The owner's flow, not
the upstream project's information architecture, decides the UX.

**6. Examples in this project.**

| Area | Used from the community | Bittern's product-specific layer |
| --- | --- | --- |
| NVR engine | Unmodified Frigate image, its config format, API and go2rtc. | Renderer, camera inventory, wizard, status and the only browser control surface. |
| OS and services | Debian 13, systemd, networkd, Docker and Debian packages. | First-boot unit, generated units and network config, apply and rollback, owner-facing status and recovery. No custom init, network manager or service supervisor. |
| UI partial updates | HTMX 2.0.11, vendored and pinned. | Go `html/template` pages, product navigation, accessibility and the no-JavaScript fallback. No SPA, client-side router or custom update library. |
| Camera discovery | ONVIF, WS-Discovery and RTSP. | Bounded discovery and probing on `cam0`, evidence and credential collection, and the atomic adoption transaction. No private discovery protocol. |
| Setup token QR | Debian qrencode, planned in BIT-019. | Console formatting, fragment handling, short-code fallback, expiry and the no-copy setup flow. No custom QR encoder. |
| Future kiosk | A real browser engine and a full-screen session such as cage or greetd, if approved. | The same Bittern templates, kiosk principal, shell behavior and recovery path. No second UI or custom renderer. The kiosk remains deferred and is not decided here. |

The Frigate UI contrast is deliberate. Frigate's UI is the upstream surface for video and NVR
settings; it assumes Frigate concepts and a user who can reason about camera configuration. Bittern's
UI is for the person who flashes the appliance and hands it over. Bittern therefore owns the UX while
Frigate does the video work. When Frigate needs a new capability, Bittern extends its schema,
renderer and templates, or promotes a generic change upstream; it does not fork Frigate's frontend.

## Consequences

### Positive

- Bittern writes and maintains less code, so there are fewer bugs and fewer security fixes to carry.
- Security response, compatibility work and broad testing stay with the upstream project that already
  has the expertise and the user base.
- The product layer stays focused on the appliance-specific work: onboarding, isolation, updates,
  recovery, handoff and clear UX for a non-technical owner.
- The ordered use, configure, wrap, improve, replace path gives reviewers a concrete test and makes
  custom code a deliberate exception rather than the default.
- Upstream improvements reach other users, and adopting them removes a local wrapper instead of
  creating a permanent private copy.
- Standard protocols and Debian packages reduce camera-specific and platform-specific custom code.
- The no-fork rule keeps Frigate, Debian, HTMX and other components upgradeable and traceable.

### Negative / costs

- Integration is still real work. Configuring and wrapping a community component can take longer
  than the first estimate, especially when its API is incomplete or its behavior varies by version.
- A wrapper can grow into a second implementation if it is not reviewed against the smallest-layer
  rule. It needs an owner, tests and a removal path.
- Upstream UX gaps remain until they are fixed or promoted. Bittern may carry a product-specific
  layer for a long time, and upstream may reject the generic improvement.
- Upstream releases can change behavior or break the wrapper. Every bump needs a pin update, contract
  tests and a migration check, which adds work to the update path.
- An upstream project can slow down, change direction or be abandoned. A custom replacement is then
  expensive, and the lifecycle review has to catch that risk before it becomes urgent.
- Every dependency adds license review, notices, SBOM entries and a security update obligation.
  GPL components such as the bundled ffmpeg builds need their own compliance work under ADR 0013.
- "UX wins" is a judgement. Without owner testing, phone and desktop checks, accessibility review and
  failure-path testing, it can become a developer preference or a reason to add unnecessary code.
- Parsing logs or other human-readable upstream output is brittle. Using it as an integration point
  creates failures that are hard to detect before a release.

### Follow-ups

- Use this ADR as a review question for every new component or dependency: what community option was
  tried, why configuration or a wrapper was not enough, and what the maintenance and security path
  is.
- Record every new component in [THIRD_PARTY_NOTICES.md](../../THIRD_PARTY_NOTICES.md), pin its
  version or digest, and add it to the SBOM and the relevant update tests.
- Record each UX gap and its wrapper in [the upstream register](../upstream.md), with the promotion
  intent, the upstream link when one exists, the adoption status and the condition to remove the
  wrapper. Follow the prototype tracking rules in
  [ADR-0020](./0020-local-prototypes-upstream-destination.md).
- Apply the rule to BIT-008 and BIT-019 as they are implemented. Keep the ONVIF discovery
  orchestration as Bittern product work because upstream declined the generic discovery request, and
  add qrencode to the notices when it enters the image.
- Pass the UX gate for every affected flow: phone and desktop widths, keyboard and accessibility,
  no-JavaScript fallback where required, and no secret copy between devices.
- Test at the integration boundary: golden configuration and validation for generated upstream
  config, protocol fixtures for discovery, QR decode checks, and upgrade tests for pinned components.
  Do not re-test upstream internals, but do test the contract Bittern depends on.
- When upstream accepts and releases a fix, delete the local wrapper, update the pin, tests and
  documentation, and record the adoption as ADR 0020 requires.
- Revisit the kiosk example only when OQ-25 and the kiosk ADR are taken; this ADR does not decide the
  kiosk, live video (OQ-23), camera support (OQ-10) or Frigate+ (OQ-07).

## Alternatives considered

- **Build the functionality in Bittern by default.** Rejected: it duplicates mature upstream work,
  adds a second maintenance and security burden, and slows the appliance toward a platform.
- **Use the community option exactly as shipped, including its UI.** Rejected: Frigate's UI and
  generic system tools are not the product UX, and an owner who must understand them cannot complete
  first run, adoption or recovery. It also conflicts with the control-surface decision in ADR 0016.
- **Fork or patch the community component when its UX is poor.** Rejected: a fork inherits the
  maintenance and security burden, violates ADR 0003 and ADR 0020, and creates trademark and support
  problems. A wrapper is the smaller and reversible option.
- **Vendor the source so it can be patched locally.** Rejected: it is a fork in practice, and the
  divergence stays hidden until the next upgrade.
- **Wrap without tracking or upstreaming.** Rejected: that is how a temporary gap becomes a silent
  permanent copy. ADR 0020 requires a recorded promote-or-retire condition.
- **Replace a community component whenever its UX is imperfect.** Rejected: a custom replacement is
  the last resort. A small layer around a working upstream component is usually cheaper, safer and
  easier to remove.
- **Choose by development speed alone.** Rejected: quick integration can hide an unmaintained
  dependency, a license problem or a security path that Bittern cannot support.
- **Decide each case informally.** Rejected: the same debate returns with every component, and the
  result depends on who reviews it. This ADR makes reuse the default and custom code the exception.
- **Do nothing and keep the current mix.** Rejected: the project already depends on community
  components, so leaving the policy unstated invites both accidental reinvention and uncritical
  adoption.

## References

- [ADR-0003: Frigate as NVR engine](./0003-frigate-as-nvr-engine.md)
- [ADR-0013: Branding constraints and MIT license](./0013-branding-and-licensing-constraints.md)
- [ADR-0016: Bittern owns the control surface and the Frigate config](./0016-bittern-owns-frigate-config.md)
- [ADR-0019: UI stack and TLS](./0019-ui-stack.md)
- [ADR-0020: Local prototypes, upstream destination](./0020-local-prototypes-upstream-destination.md)
- [Upstream register](../upstream.md)
- [Vision](../vision.md)
- [Architecture](../architecture.md)
- [Implementation plan](../plan/implementation-plan.md)
- [UI README](../../ui/README.md)
- [Frigate assessment](../research/frigate-assessment.md)
- [Setup token QR note](../notes/2026-11-15-setup-token-qr.md)
- [Kiosk mode note](../notes/2026-11-15-kiosk-mode.md)
- [BIT-008 camera discovery and adoption](../plan/implementation-plan.md#bit-008-camera-discovery-and-adoption-on-cam0)
- [BIT-019 setup token QR exchange](../plan/implementation-plan.md#bit-019-setup-token-qr-exchange)
- Frigate documentation: <https://docs.frigate.video/>
- ONVIF specifications: <https://www.onvif.org/profiles/specifications/>
- HTMX documentation: <https://htmx.org/docs/>
- qrencode: <https://fukuchi.org/works/qrencode/>
- Debian packages: <https://packages.debian.org/>

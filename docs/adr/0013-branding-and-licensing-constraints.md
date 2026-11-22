# 0013. Branding constraints and MIT license

- Status: Accepted
- Date: 2026-11-14
- Decided: 2026-11-14 (license)
- Deciders: project owner
- Related: [ADR-0003](./0003-frigate-as-nvr-engine.md),
  [ADR-0011](./0011-monorepo-layout-and-naming.md),
  [Frigate assessment](../research/frigate-assessment.md), [question register](../open-questions.md),
  [TRADEMARK.md](../../TRADEMARK.md), [THIRD_PARTY_NOTICES.md](../../THIRD_PARTY_NOTICES.md)

## Context

Frigate's source code is MIT-licensed, which permits reuse with attribution. The names Frigate,
Frigate NVR, and Frigate+ are trademarks of Frigate, Inc. and are not covered by the code license.
The trademark policy requires forks and products to rename and remove the logo. Bittern is an
independent project and must not imply endorsement or official status.

The license decision was deferred during the initial bootstrap and made later the same day.

Licensing varies by artifact:

- Frigate code is MIT.
- Frigate+ models are licensed only for cameras the user owns or operates. The terms prohibit
  deploying at customers' sites or bundling with hardware without a commercial license.
- The default bundled models are Apache-2.0.
- The ffmpeg builds used by Frigate are GPL.
- go2rtc is MIT.

## Decision

**Bittern's own license**

- Bittern's original code and documentation are licensed under the MIT License. The
  [LICENSE](../../LICENSE) file is the canonical text.
- Apache-2.0 is not used for Bittern's own work. MIT is short, well understood, permissive, and
  matches Frigate's own license.
- There is no contributor license agreement. Contributions are accepted inbound under MIT.
- A Developer Certificate of Origin is optional and is not adopted now.
- The MIT License covers code and documentation only. The Bittern name and logo are not licensed
  under MIT; see [TRADEMARK.md](../../TRADEMARK.md).
- Third-party components keep their own licenses and are listed in
  [THIRD_PARTY_NOTICES.md](../../THIRD_PARTY_NOTICES.md).

**Branding**

- The product is Bittern, an independent project. Do not use Frigate, Frigate NVR, or Frigate+ in
  product, binary, service, package, or domain names.
- Do not use the Frigate logo or visual identity. Bittern needs its own mark or none.
- Descriptive references are allowed: "Bittern is an appliance for Frigate NVR", "works with
  Frigate". The first prominent mention includes a disclaimer that Bittern is not affiliated with
  or endorsed by Frigate, Inc.
- The disclaimer appears in the README, the UI About page, and release notes.

**Frigate+**

- Do not bundle, preload, or enable Frigate+ models or services.
- A user may connect their own Frigate+ subscription under Frigate's terms. Bittern does not
  redistribute Frigate+ artifacts.
- Selling hardware with Frigate+ models preloaded requires a commercial license from Frigate, Inc.
  That is out of scope and needs a separate recorded decision.

**Third-party licenses**

- Keep upstream copyright and license notices intact.
- Maintain [THIRD_PARTY_NOTICES.md](../../THIRD_PARTY_NOTICES.md) and generate an SBOM for every
  release, as required by [ADR-0012](./0012-testing-and-release-gates.md).
- Review GPL obligations for bundled ffmpeg binaries before distributing an image, and comply with
  applicable source-offer requirements.
- Assume Apache-2.0 obligations for default models and MIT obligations for go2rtc.

## Consequences

### Positive

- MIT is simple and permissive, and it is compatible with Frigate's MIT license.
- Contributors face low friction: no CLA, and inbound contributions are under the same license.
- A future commercial user can build on Bittern without copyleft obligations.
- Clear separation from Frigate, Inc. reduces trademark and endorsement risk.
- The project avoids redistributing artifacts it has no right to redistribute.
- Release artifacts carry traceable third-party notices and an SBOM.

### Negative / costs

- MIT has no explicit patent grant. This is an accepted trade-off.
- MIT offers no copyleft protection against proprietary forks.
- The Bittern name and logo still need their own policy, registration, and clearance.
- Every name, logo, domain, and package must pass branding review before release.
- The project cannot sell hardware with Frigate+ models preloaded.
- GPL compliance for ffmpeg requires source-offer work and legal review.
- The disclaimer and notices add friction to the UI and documentation.

### Follow-ups

- Perform trademark and domain clearance for Bittern before launch (OQ-14).
- Keep `LICENSE`, `TRADEMARK.md`, and `THIRD_PARTY_NOTICES.md` current with the project.
- Wire SBOM generation into the release process.
- Obtain legal review of GPL ffmpeg distribution and of Frigate+ terms for any integration.
- Add branding review to the release checklist.
- Document contribution terms if the project opens to external contributors.

## Alternatives considered

- **Apache-2.0.** Rejected: its explicit patent grant is not a priority for this project, and its
  notice and patent-termination terms add complexity. MIT matches Frigate and is simpler.
- **GPL or another copyleft license.** Rejected: it would impose source obligations on downstream
  appliance builders and work against a permissive appliance layer.
- **A source-available or custom license.** Rejected: it is not open source, and it blocks reuse
  and contributions.
- **Dual licensing or a commercial license.** Rejected for now: there is no commercial product or
  support model that justifies it. A superseding ADR can revisit this.
- **A CLA or DCO.** A CLA is rejected as contributor friction with no legal entity that needs it.
  A DCO is optional and not adopted now.
- **Keep the Frigate name and logo for continuity.** Rejected: violates the trademark policy and
  implies official status.
- **Bundle Frigate+ models for a better out-of-box experience.** Rejected: requires a commercial
  license and conflicts with the give-away model.

## References

- [LICENSE](../../LICENSE)
- [TRADEMARK.md](../../TRADEMARK.md)
- [THIRD_PARTY_NOTICES.md](../../THIRD_PARTY_NOTICES.md)
- Frigate trademark policy: <https://github.com/blakeblackshear/frigate/blob/dev/TRADEMARK.md>
- Frigate license: <https://github.com/blakeblackshear/frigate/blob/dev/LICENSE>
- Frigate+ terms: <https://plus.frigate.video/terms-and-conditions>
- [Frigate assessment](../research/frigate-assessment.md)
- [Question register](../open-questions.md)

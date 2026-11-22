# UI design system evolution and governance: research

- Date: 2026-11-21. Status: research only; no `ui/` change or final process. Scope: one
  maintainer, agent implementation, no design team.
- Method: read the project loop, principles, ADRs, and Level 2 run; fetched primary
  governance pages on 2026-11-21. Re-check external pages before relying on a rule.

The system is 1,052 lines of CSS in `ui/static/bittern.css`, 14 templates, Go templates
with HTMX, no package, no design tool, and no design team ([UI README](../../ui/README.md)).
The agent can implement and verify; it cannot supply user context or accountability.

## 1. Governance models

The taxonomy is solitary, centralised, federated, and hybrid. Curtis described the first
three in 2015 and later argued that federated-only systems do not work: successful systems
always have a central team and add federated contribution gradually. zeroheight and NN/g
ask the same governance question: who decides, who reviews, and what happens when a team
needs something that does not exist.

| Model | What it is | Works when | Failure modes |
| --- | --- | --- | --- |
| Solitary | One team builds a system for its own product. | One product dominates; owner is close to use. | Single point of failure; blind spots; other needs feel like a distraction. |
| Centralised | A dedicated team owns and distributes the system. | Many products need consistency; central team funded. | Queue and bottleneck; distance from product; "tenuous" position. |
| Federated | Designers from product teams decide together. | Many engaged designers have time and coordination. | Divided loyalties; lots of cooks; slow decisions; no owner. |
| Hybrid | A central core plus federated contributors. | Clear rules and enough central capacity. | Bureaucracy; unclear decision rights; central team remains the constraint. |

Sources: [Curtis 2015](https://medium.com/eightshapes-llc/team-models-for-scaling-a-design-system-2cf9d03be6a0), [Curtis 2024](https://medium.com/@nathanacurtis/the-fallacy-of-federated-design-systems-23b9a9a05542), [zeroheight](https://help.zeroheight.com/hc/en-us/articles/36474270188699-Design-system-governance-models-and-which-is-right-for-your-organization).

Federated governance is not available here: there is no design team, and an agent is not
an accountable designer. The realistic family is a solitary or centralised owner with
contribution lanes. Three options: solo owner with agent implementation (fast, risks
self-review and bus factor); central owner plus one named reviewer (independent, can
stall); or bounded contribution lanes (scales, risks bureaucracy). Fit is conditional: the
first option matches today's one-maintainer reality, the second is the smallest step that
adds independence, and the third only pays off if contributors appear. The trade-off is speed
and coherence against independence and resilience. A one-person project cannot buy
independence with a committee; it can name a reviewer or accept the risk. The agent can
propose, implement, and verify, but should not approve the pattern it wrote.

## 2. Contribution and change classes

Mature systems sort by size and impact. Grafana uses fix, small enhancement, large
enhancement, and new. Nord uses light, medium, and heavy, with a kick-off for medium and heavy.
Curtis describes a scale from fix to new feature; cross-component work is run centrally.
Atlassian accepts fixes and small enhancements, not major enhancements or new components.
Carbon uses draft, preview candidate, preview, and stable, with a component checklist.

| Class | Evidence floor | Who decides | Artifact |
| --- | --- | --- | --- |
| Copy or doc fix (wrong label, stale example) | Source of truth; affected screen. | Agent applies; maintainer spot-checks. | Commit or docs. |
| Token or value tweak (colour, spacing, type) | Contrast check; affected routes; before and after at both widths. | Maintainer. | Commit and changelog. |
| Component variant or state (new button state, field error, card variant) | Two real uses or one blocking flow; accessibility; screenshots; test. | Maintainer. | Ticket and UX checklist. |
| New pattern or component (new flow, shared component) | Repeated need on two or more screens or flows, or user research; no fit; accessibility, docs, tests. | Maintainer; open question or ADR if architectural. | Ticket, then ADR if needed. |
| Breaking change (rename or remove a class, token, or contract) | Usage inventory; migration path; deprecation notice; before and after; test updates. | Maintainer; ADR if contract; second reviewer if available. | ADR and changelog. |
| Exception or one-off (page-specific override) | Reason, scope, expiry, removal ticket. | Maintainer. | Ticket or comment. |

Avoid unbounded growth with usefulness, uniqueness, reuse, and a definition of done.
GOV.UK requires evidence of usefulness to many teams and no duplication; Carbon requires
every checklist category before "done"; Cusick says start with HTML and add tokens only
when needed. For Bittern the unit is two real screens or flows; a variant used once is an
exception, not a system asset. Avoid rubber-stamping with explicit evidence, a written
rationale, and a review that can say no. Low-risk classes can use automated checks and a
spot-check; new patterns and breaking changes need an independent read.

Sources: [Grafana](https://grafana.com/developers/saga/contributing), [Nord](https://nordhealth.design/resources/contributing), [Curtis contributions](https://medium.com/eightshapes-llc/defining-design-system-contributions-eb48e00e8898), [Atlassian](https://atlassian.design/contribution), [Carbon PDLC](https://www.carbondesignsystem.com/getting-started/contributing/product-development-lifecycle), [GOV.UK criteria](https://design-system.service.gov.uk/community/contribution-criteria).

## 3. Versioning and deprecation

SemVer deprecates in a minor and removes in a major, with at least one minor containing
the deprecation. Keep a Changelog groups Added, Changed, Deprecated, Removed, Fixed, and
Security. Atlassian has early access, beta, GA, intent to deprecate, and deprecated, and
will not remove a GA feature without an announced period. Spectrum marks yellow then red
and removes at the next major. Carbon published v10 end-of-life and migration guides.
GOV.UK v6.0.0 (February 2026) removed deprecated APIs as a breaking change. Salesforce
SLDS 2 replaced tokens with styling hooks and shipped a linter with auto-fixes. Polaris
versions web components by stable channel or pinned release.

Bittern has no package and one consumer, so package machinery would be disproportionate;
three levels are enough:

- Internal refactor: atomic, no deprecation; update templates and tests; changelog "Changed".
- Shared CSS or template contract: keep the old class or property for one app release,
  mark "Deprecated", then remove; a grep or template test proves no caller remains.
- User-visible flow or term: release note plus one release of overlap; no console warnings.

SemVer is optional while there is no package; a date-stamped changelog and app release
number are enough. A migration guide can be a changelog entry with before and after.
Codemods are only worth it for a rename with many call sites; at 1,052 lines, an inventory
search plus a template-to-class test is cheaper.

Sources: [SemVer](https://semver.org/), [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), [Atlassian release phases](https://atlassian.design/release-phases), [Spectrum deprecation](https://opensource.adobe.com/spectrum-web-components/deprecation), [GOV.UK what's new](https://design-system.service.gov.uk/community/whats-new).

## 4. Evidence-driven evolution

GOV.UK's strongest test is usability in user research with a representative sample,
including people with disabilities. Usefulness needs evidence from many teams or services;
uniqueness needs evidence a replacement is better. Carbon prioritises by business impact
and reuse, identifies five to eight stakeholder teams, and tracks insertions. Material's
M3 Expressive cites 46 studies and 18,000 participants. NN/g treats governance, support,
and measured outcomes as maturity, not component count. Accessible.org tracks recurrence,
severity, and remediation velocity, and warns that automated checkers see only part.

| Signal | Weak, do not change the pattern | Defensible, fix or investigate | Strong, change the pattern |
| --- | --- | --- | --- |
| One loud opinion | One comment, no reproduction. | Same issue in two independent reviews or notes, with reproduction. | Representative research, or severe accessibility failure. |
| One defect | One occurrence, implementation slip. | Same class on two or more screens or releases, shared cause. | Repeated across three or more releases, or blocks a core task. |
| Support load | One question. | Same question twice, or a documented gap. | Users cannot complete the task unaided. |
| Review comment | One reviewer preference. | Same comment on two or more changes, tied to a principle. | A checklist item is repeatedly missed and causes a defect. |
| Accessibility | An automated warning. | Manual WCAG 2.2 AA failure on a core flow. | Systematic failure across components, or legal or safety risk. |

Separate fixing the instance from changing the pattern. One severe accessibility failure
can justify an immediate fix, not a new principle. Require a reproduction, a second
independent occurrence or research, and a written rationale. The project's loop defines
recurrence as the same defect in two or more notes, tickets, surfaces, runs, or incidents,
and allows one severe finding to become a principle when it generalises. That is more
proportionate than "many teams" or five to eight stakeholder teams.

Sources: [GOV.UK criteria](https://design-system.service.gov.uk/community/contribution-criteria), [Carbon PDLC](https://www.carbondesignsystem.com/getting-started/contributing/product-development-lifecycle), [M3](https://m3.material.io/blog/building-with-m3-expressive), [NN/g](https://www.nngroup.com/articles/design-system-maturity), [Accessible.org](https://accessible.org/measure-accessibility-performance-over-time), [findings loop](../notes/2026-11-15-findings-and-principles-process.md).

## 5. Small-project failure modes

| Failure mode | What it looks like | Counter-practice |
| --- | --- | --- |
| Bureaucracy | Review board, monthly working group, multi-team roadmap for one person. | Match review to change class; no committee unless a second decision-maker exists. |
| Stagnation | System is "done"; patterns age; nobody owns evolution. | Review principles after runs and deliveries; deprecate unused assets. |
| Drift | One-off spacing, colours, cards; product stops matching the system. | One source of truth, a class inventory test, screenshot review, logged exceptions. |
| Diverging from product | System optimises for elegance while real flows work around it. | Stay close to real runs; contribute to current work, not an abstract backlog. |
| Documentation nobody reads | Separate docs site or long guidance, stale or disconnected from code. | Docs next to code, short, examples, link check; delete unused pages. |
| Built ahead of need | Tokens, components, tooling for hypothetical products. | Start with HTML and CSS; add a pattern after two real uses; skip packages and Storybook. |

NN/g's minimum team is an interaction designer, a visual designer, and a developer, plus
part-time research, architecture, and writing support. MadeCurious argues the opposite for
a solo team: a small component library, a handful of tokens, and a short honest document.
Amy Hupe's Babylon account warns against copying a mature contribution model too early: a
reactive process would have pulled a small team away from building the foundations, so
contribution was limited to current work. Cusick's "do less" makes the same point.

What keeps it small: one stylesheet, no build step, one UX checklist, one changelog, no
package, no design tool, and no abstraction until a pattern repeats.

Sources: [NN/g 101](https://www.nngroup.com/articles/design-systems-101), [MadeCurious](https://madecurious.com/articles/design-systems-for-solo-designers), [Hupe](https://amyhupe.co.uk/articles/enabling-contribution-in-the-early-stages-of-the-babylon-design-system), [Cusick](https://joshcusick.substack.com/p/design-systems-should-do-less), [Hurley](https://www.johnhurley.design/writing/design-system-governance), [Pennylane](https://medium.com/@ericascolaro/from-documentation-nobody-reads-to-a-living-design-system-189daa9fb69a).

## 6. Measuring it

Track a few counts that change a decision, not a dashboard. Recurrence metrics are the
best candidates because they map to existing notes and principles.

| Metric | What it tells | How to keep it honest |
| --- | --- | --- |
| Finding recurrence | Whether a principle or test prevents repeat defects. | Count the same class in two or more notes; link to the principle. |
| Review-comment recurrence | Whether a checklist item is ignored or ineffective. | Tag by principle or class; ignore reviewer style preferences. |
| Defect class recurrence per release | Whether a class is systemic. | Fixed small taxonomy; counts, not percentages with tiny samples. |
| Accessibility failures by severity and recurrence | User harm and returning issues. | Manual audit plus tests; automated checks are incomplete. |

Vanity metrics: component count, token count, adoption percentage, page views, Figma
insertions, number of contributions, and a self-assessed maturity score. They measure
activity or compliance, not task success or recurring defects.

Sources: [Accessible.org](https://accessible.org/measure-accessibility-performance-over-time), [adoption red herring](https://medium.com/@disco_lu/design-system-adoption-is-a-red-herring-6c6b5a504f43), [Figma metrics](https://www.figma.com/blog/design-systems-104-making-metrics-matter).

## 7. Reusing the findings loop and the ADR process

No second governance system is needed. The loop already captures evidence, types it,
triages it, checks recurrence, distils a principle, and requires a check or test. A design
system change is a finding with a change class, not a separate proposal track.

- Findings note or ticket: use a note for a run or review that produces a defect or
  question; use a ticket for a fix, variant, or bounded pattern change. A routine token
  tweak needs neither.
- ADR: only for a decision that affects more than one component, is expensive to reverse,
  changes a contract, or chooses between credible alternatives. A colour value or button
  state does not need one.
- Principle or changelog: a recurring finding becomes a principle with a check; a release
  records changed, deprecated, and removed UI.

Options that reuse the loop: add `change class` and `surface` to a finding's fields, or
leave them implicit and use the change-class table as a triage guide. The evidence does not
support a charter, council, backlog, or maturity programme. The
[ADR process](../adr/README.md) already says when a decision needs a record, and the
[findings loop](../notes/2026-11-15-findings-and-principles-process.md) already says how
evidence becomes a rule.

## 8. What not to copy

- GOV.UK, Carbon, NN/g, and Atlassian scale practices: working groups, stakeholder teams, minimum roles, and employee-only contribution assume staff a solo project lacks.
- Polaris, Salesforce, and Spectrum transition machinery: vendor version switches, SLDS 2 token migration, and changesets assume a published library and release cadence Bittern lacks. Polaris React was deprecated when web components shipped on 2025-10-01.
- Material-scale research and adoption dashboards: 46 studies and 18,000 participants are not a proportionate evidence bar; component counts measure activity, not task success.
- Federated-only governance: Curtis's later correction is direct; it does not work without central capacity, and a project with no design team should not pretend.

## 9. Sources (accessed 2026-11-21 unless dated)

- GOV.UK: [criteria](https://design-system.service.gov.uk/community/contribution-criteria), [develop](https://design-system.service.gov.uk/community/develop-a-component-or-pattern), [propose](https://design-system.service.gov.uk/community/propose-a-component-or-pattern), [community](https://design-system.service.gov.uk/community), [what's new](https://design-system.service.gov.uk/community/whats-new), [prioritisation 2022-09-07](https://designnotes.blog.gov.uk/2022/09/07/how-we-prioritise-additions-to-the-gov-uk-design-system).
- Carbon: [contributing, updated 2026-09-01](https://www.carbondesignsystem.com/getting-started/contributing/overview), [PDLC](https://www.carbondesignsystem.com/getting-started/contributing/product-development-lifecycle), [component checklist](https://www.carbondesignsystem.com/getting-started/contributing/component-checklist), [v10 deprecation](https://medium.com/carbondesign/moving-forward-on-deprecating-carbon-components-and-carbon-components-react-4f2f0c3d8448).
- Atlassian: [release phases](https://atlassian.design/release-phases), [contribution](https://atlassian.design/contribution), [contact](https://atlassian.design/contact-us).
- Adobe, Shopify, Salesforce, Material: [Spectrum deprecation](https://opensource.adobe.com/spectrum-web-components/deprecation), [Polaris versioning](https://shopify.dev/docs/api/app-home/latest/web-components/versioning), [Polaris React archive](https://github.com/Shopify/polaris-react-archive), [SLDS comparison](https://developer.salesforce.com/docs/platform/lwc/guide/create-components-css-slds1-slds2.html), [SLDS 2 transition](https://www.salesforce.com/blog/data-cloud-slds-2-uplift), [M3 Expressive](https://m3.material.io/blog/building-with-m3-expressive).
- NN/g: [Design Systems 101](https://www.nngroup.com/articles/design-systems-101), [maturity](https://www.nngroup.com/articles/design-system-maturity).
- Curtis: [team models 2015](https://medium.com/eightshapes-llc/team-models-for-scaling-a-design-system-2cf9d03be6a0), [federated fallacy 2024](https://medium.com/@nathanacurtis/the-fallacy-of-federated-design-systems-23b9a9a05542), [contributions](https://medium.com/eightshapes-llc/defining-design-system-contributions-eb48e00e8898), [component QA](https://medium.com/eightshapes-llc/component-qa-in-design-systems-b18cb4decb9c).
- Practitioners: [Hupe Babylon](https://amyhupe.co.uk/articles/enabling-contribution-in-the-early-stages-of-the-babylon-design-system), [Hupe humans](https://amyhupe.co.uk/articles/design-systems-for-humans), [Grafana](https://grafana.com/developers/saga/contributing), [Nord](https://nordhealth.design/resources/contributing), [Cusick](https://joshcusick.substack.com/p/design-systems-should-do-less), [Hurley](https://www.johnhurley.design/writing/design-system-governance), [MadeCurious](https://madecurious.com/articles/design-systems-for-solo-designers), [one framework](https://www.designsystemscollective.com/design-system-governance-one-framework-every-component-37cb8bd4a84d), [zeroheight](https://help.zeroheight.com/hc/en-us/articles/36474270188699-Design-system-governance-models-and-which-is-right-for-your-organization).
- Standards and metrics: [SemVer](https://semver.org/), [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), [adoption red herring](https://medium.com/@disco_lu/design-system-adoption-is-a-red-herring-6c6b5a504f43), [adoption review](https://uxdesign.cc/design-systems-adoption-metrics-over-the-past-5-years-b389308d6663), [Accessible.org 2026-04-23](https://accessible.org/measure-accessibility-performance-over-time), [Pennylane](https://medium.com/@ericascolaro/from-documentation-nobody-reads-to-a-living-design-system-189daa9fb69a), [Figma metrics](https://www.figma.com/blog/design-systems-104-making-metrics-matter).

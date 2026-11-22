# Agent instructions

Bittern is an open-source appliance layer around Frigate NVR: a reproducible Debian OS image, a
management agent, and a local web UI for a two-NIC x86 camera appliance. Frigate is used unmodified,
in Docker, and is not forked. Status: pre-implementation. The repository contains design
documentation and component README stubs only, so most tasks will be planning, documentation, or the
first skeleton code rather than feature work.

## Read before you change anything

Read these in order, and read the ADRs relevant to your task in full:

1. [docs/vision.md](docs/vision.md): the problem, the users, and the v1 boundary.
2. [docs/architecture.md](docs/architecture.md): layers, network design, components, and flows.
3. [docs/adr/README.md](docs/adr/README.md) and the ADRs it lists. Licensing and branding
   constraints are in ADR 0013 and [TRADEMARK.md](TRADEMARK.md).
4. [docs/open-questions.md](docs/open-questions.md): questions that are deliberately undecided.
5. [docs/plan/implementation-plan.md](docs/plan/implementation-plan.md): what is being built now.
6. [docs/research/frigate-assessment.md](docs/research/frigate-assessment.md) when your task relies
   on Frigate behavior.
7. [docs/design/README.md](docs/design/README.md) and the `ui-design` skill at
   [.pi/skills/ui-design/SKILL.md](.pi/skills/ui-design/SKILL.md) before any change to `ui/`, a
   template, or a browser-facing flow.

Do not start implementation that contradicts an Accepted ADR. If the task requires contradicting
one, stop, explain the conflict, and propose a new ADR that supersedes it. Do not quietly work
around a decision.

## Repository map

| Path | Role |
| --- | --- |
| `image/` | `bittern-os`: mkosi-built Debian image, partitions, A/B slots, first boot, signing |
| `agent/` | `bitternd` daemon and `bittern` CLI: config, renderer, apply, onboarding, updates, reset |
| `ui/` | `bittern-ui`: first-run wizard, onboarding, status, updates, logs, support bundle, reset |
| `schema/` | JSON Schema for `appliance.yaml`, schema versions, migrations, examples |
| `templates/` | Renderer inputs for networkd, nftables, dnsmasq, chrony, compose, Frigate fragments |
| `defaults/` | Product defaults: addresses, pools, ports, timeouts, channel URLs, pinned versions |
| `tests/` | Golden renders, QEMU two-NIC harness, virtual cameras, update and power-cut tests, HIL |
| `fleet/` | Optional GitOps bundles for maintainer-operated devices; not part of v1 |
| `docs/` | Vision, architecture, ADRs, research, open questions, plan, dated notes |
| `LICENSE` | MIT license for Bittern's own code and documentation |
| `TRADEMARK.md` | Bittern name and logo policy, plus Frigate trademark attribution |
| `THIRD_PARTY_NOTICES.md` | Licenses for bundled and separately distributed components |

## Hard rules

- No secrets in git. On a device, secrets live in `/etc/bittern/secrets.yaml` with mode 0600. Fleet
  secrets use SOPS with age. Never commit a rendered file that contains a secret, and never print a
  secret in logs or test output.
- Do not rename the product to include "Frigate". Do not use the Frigate logo or imply affiliation
  with Frigate, Inc. See ADR 0013.
- Do not vendor, patch, or fork Frigate. Bittern consumes the upstream container image and its
  documented API and config formats.
- Prototype locally by default, promote generic work upstream when useful, and adopt the upstream
  version when accepted. See
  [ADR 0020](docs/adr/0020-local-prototypes-upstream-destination.md).
- Pin the Frigate container image by digest, not by a floating tag.
- Never hand-edit generated files. Render them from `appliance.yaml`, the templates, and the
  defaults, then validate and apply through the agent pipeline.
- Keep `appliance.yaml` schema-versioned. Every schema change needs a migration and a migration
  test; the UI and CLI must be able to read older versions.
- Bittern's own code and documentation are MIT (see `LICENSE`). Do not relicense them, and do not
  add or replace a `LICENSE` file, or add or change license headers or declarations, without an
  accepted ADR. Third-party components and their licenses are listed in `THIRD_PARTY_NOTICES.md`;
  update that file when a dependency is added or its license changes. Branding and trademark
  constraints are in ADR 0013 and `TRADEMARK.md`.
- Prefer the existing community component, protocol, or package over custom code. Do not fork, patch,
  or duplicate it. When its UX conflicts with the product, UX wins through the smallest
  product-specific layer; record the gap and upstream it when generic. See
  [ADR 0022](docs/adr/0022-community-functionality-ux-wins.md).
- Documentation is part of the deliverable. Update the affected docs, ADRs, and open questions in the
  same change as the behavior change.
- All web interfaces are responsive and mobile friendly. Every UI change is checked at phone and
  desktop widths. A setup or control flow that requires copying a secret between devices is a
  defect unless no reasonable alternative exists.
- Every finding from a run, review, incident, report, or research task is captured in a dated note
  under [docs/notes](docs/notes/), triaged to a ticket, open question, ADR, research note, or
  documented no action, and distilled into [principles](docs/principles.md) when it recurs. The loop
  is in [the findings and principles process](docs/notes/2026-11-15-findings-and-principles-process.md).
  Every UI change applies the [UX review checklist](docs/principles.md#ux-review-checklist); a
  repeated finding without a principle or test is a process failure.
- Do not add CI, dependencies, or release machinery that the implementation plan does not call for.
  A new significant choice needs an ADR, not a drive-by commit.

## Automation first

- Default to automatic detection and configuration: management and camera NICs by carrier and role,
  DHCP, DNS, recording disks, cameras and containers. Do not ask the operator for a value the script
  can discover.
- A procedure that will run more than once ships as a checked-in idempotent script, not as a
  sequence of commands in a runbook. The runbook states preconditions, invokes the script, and
  verifies the result.
- Every script supports a plan or dry run, is safe to run twice, refuses mounts, symlinks and files
  whose ownership it cannot establish, verifies the end state, and fails instead of claiming
  success. Destructive actions need explicit confirmation.
- A manual step or human input is not an acceptable design. It is allowed only when no reasonable
  automatic path exists, and then it is named in the runbook with why, the smallest possible input,
  and the ticket that removes it. Do not present a manual step as the normal procedure.
- Keep the automation clean and simple: the smallest script or flow that meets the requirement, no
  clever generality, the detection path tested end to end, and one script rather than a sequence of
  operator commands.
- If a script cannot make progress automatically, it fails with a specific message and the exact
  missing fact, instead of telling the operator to configure it by hand.
- The [POC cleanup script](packaging/cleanup.sh) and the
  [clean-install runbook](docs/runbooks/gk41-clean-install.md) are the model: the runbook calls the
  script, and the script carries the removal, the stock conffile restore, the safety checks, and
  the clean-state verification.
- A new multi-step feature includes its automation and a test in the same change, not a runbook
  first and a script later.

## Decision records

The process is defined in [docs/adr/README.md](docs/adr/README.md). In short: copy
`docs/adr/0000-template.md`, take the next free number, keep the status `Proposed` until reviewed,
and set it to `Accepted` with a date when the decision is made. Never edit the decision text of an
Accepted ADR. To change a decision, write a new ADR that supersedes it, and mark the old one
`Superseded` with a link to the replacement.

An ADR records one decision, its context, the alternatives, and the consequences. It is not a design
document and not a task list.

## Frigate facts and verification

Frigate facts in `docs/research/frigate-assessment.md` are dated 2026-11-14. Before relying on a
version number, flag, port, config key, or UI behavior, re-check the upstream release notes,
documentation, and security advisories. Re-date anything you assert, and update the research doc or
open questions rather than leaving a stale claim in a new document. Version numbers and defaults
move quickly, and this project pins them deliberately.

## Testing expectations once code exists

- Config changes need golden render tests: input `appliance.yaml`, expected generated files.
- Rendered output needs validation, including `nft -c`, `dnsmasq --test`, and the Frigate config
  check, before it is applied.
- Updates need tests for success, health failure, rollback, and power loss during the transaction.
- No change is merged without a way to verify it. If a behavior cannot be tested yet, say so in the
  change and add the missing test to the implementation plan.

## Documentation style

Markdown with ATX headings. Sentence-case headings. Relative links only. Date-stamp factual claims
and link to the research doc for Frigate claims. Soft wrap near 100 columns. No emoji and no em
dashes. Prefer tables and short lists over long prose. Keep component READMEs aligned with
`docs/architecture.md` rather than restating the whole design.

## Handoff notes

When a session changes direction, leaves work half done, or discovers something the next agent must
know, leave a short dated note in `docs/notes/` using `YYYY-MM-DD-topic.md`. State what changed, what
is unfinished, and the next concrete step. Do not paste transcripts.

## Undecided work

Anything undecided lives in [docs/open-questions.md](docs/open-questions.md), never silently chosen.
If you need an answer to proceed, record the question and the options, then ask or pick the smallest
reversible default and mark it provisional. The open questions file is the single place where the
project admits uncertainty.

# Design system evolution

- Date: 2026-11-21
- Status: Accepted. The change classes apply to the implemented token layer, components and design
  check.
- Applies to: every change to the token set, a component, a pattern, or a shared UI rule, and to
  every design retro.
- Companion documents: [README.md](README.md), [tokens.md](tokens.md),
  [components.md](components.md), [patterns.md](patterns.md),
  [professional-test.md](professional-test.md), [review.md](review.md),
  [CHANGELOG.md](CHANGELOG.md).

This file says how the design system changes. It is sized for one maintainer with agent help: no
charter, no council, no separate backlog, and no maturity programme. The evidence behind the
stance, including the small-project failure modes it avoids, is in
[the evolution research](../research/2026-11-21-ui-design-evolution.md). Where this file and the
research disagree, this file wins for Bittern and the research keeps the sourced comparison.

The system is small on purpose: one `:root` block, one stylesheet, one component inventory, one
pattern set, one review protocol, one changelog. A rule that cannot be checked by reading a
template, taking a screenshot, running a grep, or running a test is marked advisory or removed. It
does not get a process to enforce it.

## The two governing rules

Both rules already exist in
[the findings and principles process](../notes/2026-11-15-findings-and-principles-process.md) and
[the engineering process](../engineering/README.md). The design system reuses them and does not
restate them as a second system.

| Rule | What counts | What it produces |
| --- | --- | --- |
| A finding seen twice becomes a rule | The same defect in two or more notes, tickets, surfaces, runs, or incidents, or one severe finding whose review shows the mechanism generalizes. | A rule row in [components.md](components.md) or [patterns.md](patterns.md), or a principle in [principles.md](../principles.md) when it applies beyond the UI. |
| A rule violated twice becomes a check | A recorded rule is broken again after the change that recorded it. The second violation is evidence about the check, not about the second screen. | The smallest check that fails on the real defect: a browser-check assertion, an offline script in `make check`, a Go test, or a named manual step with the ticket that automates it. |

Two limits keep the rules honest:

- A single occurrence is fixed where it happened, with its regression test or screenshot, and is
  not promoted. One severe accessibility failure may justify an immediate fix; it does not justify
  a new principle unless the review shows it generalizes.
- A rule with no possible check is marked advisory in the design document. An advisory rule cannot
  produce a blocker or a major finding on its own. It can support a finding that already has a
  checkable rule behind it.

Where the second violation is counted: in the rule row's check column, or in the dated note that
recorded it. There is no counter file. If the same rule fails twice and the check column still says
"template read", the missing artifact is the check.

## Change classes

Four classes. Each has an evidence bar a reviewer can apply without taste, one decider, and one
artifact. The research lists six rows; the other two are not classes here: a copy fix is a ticket,
and a one-off exception is recorded as a finding with a reason, scope, expiry, and removal ticket.

| Class | What it covers | Evidence bar | Who decides | Artifact that records it |
| --- | --- | --- | --- | --- |
| Token value | A colour, spacing, type, radius, border, motion, or touch value in [tokens.md](tokens.md). A name change is not a value change; see the breaking class. | The measured contrast against the floors in [tokens.md](tokens.md); every affected route listed; before and after at 360 and 1280, and at 1920 when the page uses width; a greyscale read when a state colour moves. | Maintainer for a new value or a new token. An agent may land a value that is already written in `tokens.md` without asking. | The value in `tokens.md` plus a `Changed` or `Added` entry in [CHANGELOG.md](CHANGELOG.md). No ADR. |
| Component variant | A new or changed variant, state, or anatomy detail on an existing component in [components.md](components.md): a destructive button, a field error, a camera card variant. | Two real uses, or one blocking flow where no existing variant works; keyboard and contrast evidence; both uses shown at 360 and 1280; the variant, its states, and its tokens written into the component entry. | Maintainer, on a proposal from the agent that found the need. The agent that implements the variant does not approve it in the same change. | The component entry in `components.md`, a ticket for the use sites that do not land with the variant, and a `Changed` or `Added` entry in the changelog. |
| New pattern | A new flow, or a composition of existing components into a job that no pattern in [patterns.md](patterns.md) covers: an operation view, a multi-camera add, a reset flow. A new destination or transition is a flow change in [flows.md](flows.md). | A repeated need on two or more screens or flows, or one blocking flow that the existing patterns cannot express; the job, one primary action, the steps, the state matrix, and rules with checks; accessibility; screenshots at every reviewed width. | Maintainer. An ADR as well when the pattern changes a contract with the agent, the route set, or a release promise. | A rules table in `patterns.md` with rule IDs, or the destination and transition in `flows.md`, a ticket, an `Added` entry in the changelog, and an ADR when it is architectural. |
| Breaking change | Rename or remove a token, class, component, pattern, rule, or route, or change a contract that templates and tests depend on. | A usage inventory over `ui/templates/`, `ui/static/bittern.css`, `agent/`, `tests/`, and `docs/`; a replacement named; every current use migrated in the same change; before and after screenshots; the affected tests updated. | Maintainer, plus a second read by an agent or person who did not write the change when one is available. An ADR when it changes a contract or is expensive to reverse. | `Deprecated` and `Removed` entries in the changelog with dates and the replacement, the updated docs, and an ADR when the change is a contract. |

Class rules that keep the table usable:

- A variant used once is an exception, not a system asset. It needs a reason, a scope, an expiry,
  and a removal ticket, and it is recorded as a finding.
- A token value change that also changes the role, such as renaming `--color-primary` to
  `--color-action`, is a breaking change even when the value stays.
- A new component is a new pattern for this purpose: it carries the same evidence bar and the same
  artifacts. The component inventory grows from real screens, never from a guess about future ones.
- A class that needs a decision, not a fix, stops at the ADR process. A class whose answer is not
  settled goes to [open questions](../open-questions.md).

### Evidence that is not enough

| Signal | Why it does not open a class | What to do instead |
| --- | --- | --- |
| One reviewer preference, or one agent opinion | It has no rule, no reproduction, and no owner consequence. | Say it is taste and spend no review time on it. |
| One defect on one screen | The mechanism may be that screen, not the system. | Fix it with its test or screenshot in the ticket that owns the screen. |
| An automated checker warning | It sees only part of accessibility and layout. | Confirm it manually before it becomes evidence. |
| A component count, a token count, or an adoption percentage | It measures activity, not whether the owner finishes the job. | Use finding recurrence and the defect classes instead. |

## How evidence enters

There is one evidence loop and one tracker. A design change is a finding with a class, not a
separate proposal track.

1. **Capture.** A run, review, incident, or research task that produces a UI finding writes a dated
   note under [docs/notes](../notes/) in the shape from
   [the findings process](../notes/2026-11-15-findings-and-principles-process.md): type `UX`, the
   finding, the evidence, the impact, the owner, the destination, and the status. The evidence is
   the smallest thing that proves it: a route, viewport, state, screenshot name, exact string, CSS
   line, or measurement. No transcript and no secret.
2. **Triage.** The finding goes to the artifact that owns it: a ticket for a fix, a component or
   pattern entry for a class change, an open question for an undecided answer, an ADR for a
   decision, a research note for a dated external fact, or a documented no action. The note stays
   the evidence; this file is not a second queue.
3. **Decide the class.** Use the table above. A finding that fits no class is a ticket fix or an
   exception, and both are cheaper than a system change.
4. **Carry the check.** Each class change lands with the check its row names, or with the manual
   step and the ticket that automates it. A class change with no check is incomplete, not
   provisional.

### Ticket fix or design-system change

The same defect can be either. These four questions decide it, in order:

| Question | Ticket fix | Design-system change |
| --- | --- | --- |
| Where does the fix live? | One template's markup or copy, one route handler, or one screenshot-only layout tweak. | A token, a shared class in `ui/static/bittern.css`, or a rule in `components.md` or `patterns.md`. |
| Would a second screen or flow need it? | No, and the next screen would not copy it. | Yes, or the kiosk would inherit it through the same templates. |
| Does it duplicate a value or a fragment that already exists? | No. | Yes, and the duplicate is the defect. |
| Does it add something a future screen must remember? | No. | Yes, so it needs a rule row with a check, even if the fix itself is small. |

A "no" to all four is a ticket fix. The ticket still carries the UI review checklist and the
widths. A "yes" to any one is a class change, and the class table decides the evidence bar.

| Finding | Destination |
| --- | --- |
| Copy, label, ordering, or a single-screen layout on one route | The ticket that owns the screen, with the two-width spot check |
| Token value or a missing token | [tokens.md](tokens.md) and a changelog entry |
| Component variant, state, or anatomy | [components.md](components.md), a ticket, and a changelog entry |
| A flow, or a composition that no pattern covers | [flows.md](flows.md) when it is a destination, transition, or navigation rule; [patterns.md](patterns.md) when it is a screen pattern; a ticket, a changelog entry, and an ADR when architectural |
| A broken rule that a check can catch | A check in the gate that owns the boundary, plus the rule row's check column updated |
| A rule that recurs beyond the UI | A principle in [principles.md](../principles.md), with the evidence and the check |
| An undecided answer | [open questions](../open-questions.md), never a silent default |
| A decision that affects more than one component or is expensive to reverse | [An ADR](../adr/README.md) |
| Nothing worth doing yet | The note, with the no-action reason and the trigger that would reopen it |

## Deprecation

Deprecation is not the same as the `Retire` class in [components.md](components.md). `Retire` means
new usage is forbidden now, and the component may still exist for a legitimate variant. Deprecation
means a current name, class, component, or pattern is on a dated path to removal.

Three paths, from [the evolution research](../research/2026-11-21-ui-design-evolution.md):

| Path | When it applies here | Overlap |
| --- | --- | --- |
| Internal refactor | The whole change lands in one commit and the project is the only consumer, as with the token rename in [tokens.md](tokens.md). | None. Update every use, the docs, and the tests atomically. |
| Shared CSS or template contract | A template, the kiosk, or a release in the field still carries the old name after the change lands. | One app release: keep the old name, mark it deprecated in the changelog, then remove when the inventory is empty. |
| User-visible flow or term | The owner sees the old word in a release note, a support bundle, or a saved page. | One release with both terms named in the release note. No console warning. |

The procedure:

1. Record the retirement before the migration: a `Deprecated` entry in
   [CHANGELOG.md](CHANGELOG.md) with the date, the item, the replacement, and the removal date. Add
   the item to the `Retire` list in `components.md` when new usage must stop immediately.
2. Name the replacement in the same entry. A deprecation with no replacement is a removal, and it
   needs the breaking-class evidence bar.
3. Inventory every use: `rg` over `ui/templates/`, `ui/static/bittern.css`, `agent/`, `tests/`, and
   `docs/`. The inventory is the evidence, not an impression.
4. Migrate every current use in the same change when there is no overlap path. When there is one,
   migrate every use in this repository now and let only a field release or the kiosk keep the old
   name.
5. Prove the inventory is empty before removal: the grep output in the change, or a test that fails
   on a new caller. A test is the durable form.
6. Remove the item, the deprecated entry stays in the changelog, and add a `Removed` entry. Update
   the rule row, the component entry, the pattern section, and the token mapping in the same change.
7. Update the design debt table below. A retired item that is still in use is debt, not a decision.

Do not keep a compatibility alias for a single consumer. [tokens.md](tokens.md) says the same for
the token rename: an alias adds a second name without a second consumer. An alias is allowed only
under the shared-CSS path, where a real second consumer exists, and it is removed at the stated
date.

## Design debt

Debt here is a known gap between the design documents and the screens, or between a rule and its
check. It is recorded once, with the owner and the trigger that would make it worth fixing, so a
reviewer does not rediscover it and a later agent does not fix it by accident.

A row is worth fixing when its trigger fires, not when it is noticed. Fixing a row without the
trigger usually means a drive-by restyle or a class change smuggled into a ticket.

| Item | Why it exists | Owner | Trigger that makes it worth fixing |
| --- | --- | --- | --- |
| Nested containers: `.card` around bordered candidate rows, `.disclosure` around bordered fieldsets | A container was added per screen as the flow grew, and no single container rule existed | frontend, [BIT-033](../plan/implementation-plan.md#bit-033-visual-quality-pass-across-phone-and-desktop) | The next change to the camera or candidate template, or a third boundary on one screen |
| Duplicate label and legend: legend "Camera" with label "Camera name"; a visually hidden label on setup | Fieldsets were added for grouping after the labels were written | frontend, [BIT-028](../plan/implementation-plan.md#bit-028-camera-onboarding-ux) | The next edit-form change, or a second Q9 finding on the same pattern |
| `.facts` stacks every pair below 38rem | The phone-first rule was applied to every row, including short ones | frontend, BIT-033 | Reading a 1280 or 1920 screenshot of a fact list, or a comparison task that needs a table |
| Raw path or identifier in primary copy: `review.html` config path, status "Path" | The value was printed where it was available, not where the owner needs it | frontend, BIT-033 | The next change to review or status, or a second Q10 finding |
| A filled `Edit` on every camera card, and a filled `Open setup` as the only empty-state action | Card actions were styled once for the first card and never revisited | frontend, [BIT-029](../plan/implementation-plan.md#bit-029-multi-camera-inventory-and-add-from-listing) | The next camera-list change, or the empty-state fix that lands with it |
| Removing a camera uses the primary button style | The destructive variant does not exist in the stylesheet yet | frontend | The variant lands in `components.md`; the next remove-page change |
| "Advanced settings" disclosure contains the required RTSP port and path | The form grew and required fields were moved to the bottom | [BIT-028](../plan/implementation-plan.md#bit-028-camera-onboarding-ux) | The next camera-form change, or the disclosure component work |
| The status badge has no shape beyond the pill, so state is word plus tint | `.state-badge` predates the word plus shape plus colour rule | frontend | The token implementation chunk, or the first failed greyscale read |
| The apply button shows a spinner and "Applying..." with no step or elapsed time | The operation view in [BIT-014](../plan/implementation-plan.md#bit-014-long-running-operation-progress) is not built | BIT-014 | BIT-014 lands, or a second Q5 finding on a long operation |
| A flash is the only record of a successful edit or remove | Flash messages came with the existing session handling | frontend | The next result-page change, or the persistent-result rule failing again |
| Action green and ok green are both greens, 1.16:1 apart | The brand action colour was chosen before the state palette existed | maintainer | Reopened by the v0.13 green-action restore (2026-11-21); the next token change that separates action and ok, or a recorded decision to accept the proximity |
| The control border is 1.72:1 on white | One border token served decoration and control boundaries | maintainer | The token implementation chunk |
| The stepper hides every step except the current one at phone width and builds "Step N" in CSS | Earlier phone layout predates the stepper pattern | frontend, BIT-033 | The next wizard change |
| Every fieldset has a border | Browser default plus a border rule that was never scoped | frontend | The field component work, or a second nested-container finding |

A row leaves the table when the fix lands with its evidence, or when the no-action reason is
recorded in the note. Do not delete a row because the trigger has not fired.

## Cadence

There is one design retro, and it is an agenda item in a review that already happens. It runs
after each hardware run and after each phase boundary in
[the implementation plan](../plan/implementation-plan.md) (P0 to P4). It does not run on a
calendar and it does not produce a roadmap.

**Time.** About 30 minutes. If it takes longer, the input is too broad: use the findings since the
last retro, not the whole repository.

**Inputs.**

- The dated notes since the last retro, filtered to type `UX`.
- The open and mitigated rows in [defect classes](../engineering/defect-classes.md) that have a UI
  mechanism.
- The last changelog entries and the design debt table above.
- The screenshots read for the changed screens, and the review records for changes since the last
  retro.

**Questions.**

1. Which findings repeated? Each repeat becomes a rule candidate, with the note links as evidence.
2. Which recorded rules were broken? Each second violation becomes a check candidate.
3. Which debt rows fired their trigger? Fix them in the ticket that owns the screen, or record why
   not.
4. Which provisional choice is now decided or rejected? Update the document and the changelog.
5. Which rule is advisory and should stay advisory, or be removed? Delete documentation that no
   longer changes a decision.

**Output.** Every triaged item leaves as exactly one of these, and nothing else:

| Output | Where it lands |
| --- | --- |
| Token | The value in [tokens.md](tokens.md) and a changelog entry |
| Component | The entry in [components.md](components.md), a ticket, and a changelog entry |
| Pattern | The section in [patterns.md](patterns.md), a ticket, a changelog entry, and an ADR when architectural |
| Principle | [principles.md](../principles.md), with the evidence, the do and don't, and the check |
| Check | The gate that owns the boundary, plus the rule row's check column |
| Documented no action | The dated note, with the reason and the trigger that would reopen it |

The retro record is a short section in the dated note for the run or the phase, not a new file
per retro: date, inputs, the findings that repeated, the output rows, and the debt rows that
changed. A retro that produces no change still writes the no-action reason, because an empty retro
looks the same as a skipped one.

## What not to copy from larger organisations

The research's section 8 is the direct source. Each row names the practice, why it does not fit,
and what Bittern does instead.

| Borrowed practice | Why it does not fit | Local substitute |
| --- | --- | --- |
| Governance board, working group, or scheduled design council | There is no second decision-maker, and a committee of one is a delay with extra steps | The maintainer decides; the agent proposes, implements, and verifies; a second read happens when a person is available |
| Federated contribution from product teams | There are no product teams, and Curtis's later correction says federated-only systems do not work without central capacity | One owner, contribution through findings and tickets |
| Stakeholder teams, minimum roles, and a maturity score | NN/g's minimum team is three roles plus part-time support; Bittern has one maintainer and an agent | The artifact set in this file, reviewed at the retro |
| Published package, SemVer machinery, changesets, codemods | There is no package, no second consumer, and no published API | A date-stamped changelog, an app release number, and a grep inventory |
| Storybook, a docs site, or a design tool as the source of truth | The docs would drift from the stylesheet and the templates, which are the real system | Docs beside the code, one stylesheet, one Makefile, link and style checks |
| Component and token counts, adoption percentage, page views | They measure activity or compliance, not whether the owner finishes the job | Finding recurrence and defect classes per release |
| Material-scale research and an 18,000-participant evidence bar | Disproportionate for a one-maintainer appliance | A hardware run, a phone in daylight, and a reproduced finding |
| Multi-stage release phases with early access and beta channels | The system has no external consumers and one app release channel | Two states: current, and deprecated with a removal date |
| A migration guide as a separate document | It would be a second place to keep current | A changelog entry with before and after, and the inventory in the change |

What is worth copying, in one line each:

- Keep the system close to current work, not to an abstract backlog.
- Sort changes by class, and give the small classes a cheap path.
- Require evidence that names the rule, the evidence, and the owner consequence.
- Deprecate with a date and a replacement instead of removing silently.
- Track recurrence, not adoption.

Related: [README.md](README.md), [review.md](review.md), [CHANGELOG.md](CHANGELOG.md),
[the evolution research](../research/2026-11-21-ui-design-evolution.md),
[findings and principles](../notes/2026-11-15-findings-and-principles-process.md),
[engineering quality](../engineering/README.md),
[defect classes](../engineering/defect-classes.md),
[ADR 0026](../adr/0026-design-system-and-ui-review.md).

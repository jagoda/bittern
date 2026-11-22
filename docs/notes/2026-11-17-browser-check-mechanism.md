# Browser check mechanism for the responsive UI

- Date: 2026-11-17
- Status: Accepted ADR, harness landed 2026-11-17
- Related: [ADR 0025](../adr/0025-browser-check-for-the-responsive-ui.md),
  [BIT-020](../plan/implementation-plan.md#bit-020-responsive-and-mobile-friendly-web-ui),
  [open questions](../open-questions.md), [principles](../principles.md),
  [UI README](../../ui/README.md), [implementation plan](../plan/implementation-plan.md),
  [first browser-check run](2026-11-17-browser-check-first-run.md)

This note records the recon finding that forced a decision and the provisional defaults that let
BIT-020 continue while the open questions are answered. The decision itself is
[ADR 0025](../adr/0025-browser-check-for-the-responsive-ui.md); this note does not restate it.

## Why the decision exists

BIT-020 asks for a repeatable browser check that loads every route at phone and desktop widths,
fails on horizontal overflow or an unreachable control, and saves a screenshot per screen. Until
2026-11-17 the repository had no mechanism, no route list, and no place for the check to run. The
ticket's acceptance, the UX-8 principle, and the "UI change without both widths is not accepted"
rule all depended on a check nobody had defined.

Recon facts from 2026-11-17, in this container:

| Fact | Value |
| --- | --- |
| Node | 24.22.3 |
| npm | 11.19.1 |
| `chromium`, `chrome`, `firefox` | not installed |
| Playwright, Puppeteer | not installed |
| Check entry point | `make check`, documented as offline in the Makefile and README |
| Product browser engine | none; the appliance image has no browser |

The consequence is blunt: no layout outcome in the ticket can be measured in this repository today.
Horizontal overflow, clipped text, overlapping controls, an unreachable action, and QR scannability
are paint results. HTML and CSS assertions can show a rule exists and cannot show a card keeps its
button on screen at 390 CSS pixels.

The constraints the decision had to respect are in the ADR: the offline `make check` promise, no
Node in the product build ([ADR 0019](../adr/0019-ui-stack.md)), no dependency without a reason, and
[ADR 0022](../adr/0022-community-functionality-ux-wins.md)'s preference for community components
over custom code.

## What it blocks

- **BIT-020 chunk B.** The route check, the overflow and reachability assertions, and the
  screenshot-per-screen output cannot be written without a mechanism and a place to run it. The
  responsive layout work in chunk A does not wait.
- **The UI mux refactor and BIT-019 do not wait.** Neither changes the mechanism question, and both
  are parallel work in the same tree.
- **Later UI tickets.** BIT-022, BIT-023, BIT-024, BIT-025, BIT-030 and BIT-031 cite the BIT-020
  checks in their acceptance, so an undefined check would cascade. [OQ-29](../open-questions.md)
  records how those tickets extend the route list once it exists.
- **Release confidence.** The UI surface is the owner's only path; without the check, the phone
  acceptance is a reviewer's opinion.

## Provisional defaults

Two values were blocking chunk B. Both are provisional, both are open questions, and both are
recorded here so the check can be written now without pretending the question is settled.

- **Touch-target floor ([OQ-26](../open-questions.md)):** fail below 24 by 24 CSS pixels, warn below
  44 by 44. The 24 by 24 floor is the WCAG 2.2 AA SC 2.5.8 minimum, so it is the smallest
  defensible reading of "touch-friendly"; the 44 warning list is the evidence needed to answer
  the question.
- **Phone check width ([OQ-27](../open-questions.md)):** 390 by 844 CSS pixels. It matches the
  BIT-020 acceptance sentence and the UX-8 principle; 360 stays the design target in the scope
  line.

Changing either is one constant or one width in the check. The 24 pixel floor is not a design
standard and the 390 width is not a statement that 360 is out of scope. They exist so the check has
a pass and fail condition. Neither may be cited as the answer to the open question.

## What is still undecided

- [OQ-26](../open-questions.md): the touch-target minimum.
- [OQ-27](../open-questions.md): the 360 CSS pixel scope line versus the 390 acceptance width.
- [OQ-28](../open-questions.md): whether screenshots are committed, gitignored, or only linked. No
  screenshot is committed while this is open.
- [OQ-29](../open-questions.md): how later UI tickets extend the route list.

The ADR also leaves out the screenshot policy and the touch-target number on purpose. It fixes the
mechanism, the offline boundary, the pinning rule, and the manual prerequisite, and nothing else.

## Acceptance

[ADR 0025](../adr/0025-browser-check-for-the-responsive-ui.md) was accepted on 2026-11-17. All
development runs in a container, so the harness must run fully headless. There,
`npx playwright install --with-deps chromium` needs root or preinstalled system libraries, and the
container filesystem is ephemeral, so the browser is provisioned again after the container is
recreated.

## Next step

The harness is implemented in [tests/browser](../../tests/browser/README.md), with the route
manifest, pinned package files, prerequisite README, and `make ui-browser-check` target. The first
run and the layout findings are in
[the first browser-check note](2026-11-17-browser-check-first-run.md). Run the check again after the
BIT-020 layout work lands.

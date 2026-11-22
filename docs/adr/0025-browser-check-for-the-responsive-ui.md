# 0025. Browser check for the responsive UI

*The responsive UI check is an opt-in Playwright harness under `tests/browser/`, run by its own make
target and never by the offline `make check`.*

- Status: Accepted
- Date: 2026-11-17
- Deciders: maintainer, frontend
- Related: [ADR-0012](./0012-testing-and-release-gates.md),
  [ADR-0015](./0015-agent-language.md), [ADR-0019](./0019-ui-stack.md),
  [ADR-0022](./0022-community-functionality-ux-wins.md),
  [OQ-26](../open-questions.md), [OQ-27](../open-questions.md),
  [OQ-28](../open-questions.md), [OQ-29](../open-questions.md),
  [BIT-020](../plan/implementation-plan.md#bit-020-responsive-and-mobile-friendly-web-ui),
  [UI README](../../ui/README.md), [recon note](../notes/2026-11-17-browser-check-mechanism.md)

## Context

BIT-020 requires a repeatable browser check: every route at phone width, around 390 by 844, and at
desktop width, at least 1280 by 800, failing on horizontal page overflow or a control outside the
viewport, with a screenshot per screen. The ticket's acceptance criteria and the UX-8 principle in
[principles](../principles.md) stand or fall on that check. There is no mechanism for it yet.

A recon pass on 2026-11-17 established the starting position. The development container has Node
24.22.3 and npm 11.19.1, but no `chromium`, `chrome` or `firefox` binary and no Playwright or
Puppeteer. Nothing in the repository starts a browser today, and the appliance does not ship one:
no browser engine is in the image or payload scope. The check therefore has no home.

Three existing constraints bound the answer:

- **The offline promise.** The [Makefile](../../Makefile) says "Checks run offline", and the
  [README](../../README.md) says a clean clone runs `make check` with Python, PyYAML, jsonschema,
  make and Go. Go modules are vendored and `go.sum` is committed
  ([ADR 0015](./0015-agent-language.md)). A browser download cannot join that path without breaking
  it.
- **No product Node toolchain.** [ADR 0019](./0019-ui-stack.md) rejects Node in the product build:
  server-rendered templates, no SPA build, no asset pipeline outside `make`, no runtime asset
  directory. Any test-only Node dependency must stay off the build and off the appliance.
- **Dependencies need a reason.** [AGENTS.md](../../AGENTS.md) allows no dependency or CI that the
  implementation plan does not call for, and requires an ADR for a new significant choice. BIT-020
  calls for the check, so the dependency is in scope; this ADR is the record it needs.
  [ADR 0022](./0022-community-functionality-ux-wins.md) prefers a maintained community component
  over custom code.

Without a real layout engine, none of the observable failures in the ticket can be measured.
Horizontal overflow, clipped cells, overlapping controls, an unreachable action, and QR scannability
at phone width are layout and paint outcomes. Parsing the rendered HTML or the stylesheet can show
that a rule exists; it cannot show that a card at 390 CSS pixels keeps its button on screen. A check
without a browser would pass while the defect remained, which is a worse outcome than no check.

The repository has no CI. Checks run on a developer machine, in the Go test suite, on the QEMU rig,
or on hardware. Whatever is decided, nothing runs the browser check automatically.

## Decision

**1. The check is a real browser driven by Playwright, and it lives in `tests/browser/`.**

A Node package under `tests/browser/` holds the harness, its `package.json`, and its committed
`package-lock.json`. Playwright (`@playwright/test`) drives Chromium. The harness starts `bitternd`
with `--ui --ui-mock --ui-insecure-http` on a loopback `--ui-listen` port, with a temporary auth
state, walks a checked-in route manifest, and for each route and width records the document scroll
width, the bounding boxes of interactive controls and focus targets, and one screenshot. It fails
on horizontal page overflow, on a control whose box is outside the viewport, and on a control it
cannot focus. It is not a visual diff tool and does not compare pixels between runs.

**2. It is a separate, opt-in make target, never part of `make check`.**

BIT-020 adds `make ui-browser-check`, and that target is not a dependency of `check`, `test`,
`schema`, `style-check`, `link-check`, or `ui-test`. `make check` keeps its current inputs and its
current promise: a clean clone with Python, make and the vendored Go modules passes it offline,
unchanged. The browser check is required before a UI change that touches templates or styles is
accepted, which is the BIT-020 acceptance rule. This ADR does not add a release gate to
[ADR 0012](./0012-testing-and-release-gates.md); if the release checklist should run the check, that
change goes through the ADR 0012 process.

**3. Playwright and Chromium are pinned as one pair, and the browser is a documented manual
prerequisite.**

The harness deviates from "vendor everything" deliberately and locally: `node_modules` is not
committed, and the check is not reproducible from a bare offline clone. A developer gets the
harness once:

```sh
cd tests/browser
npm ci
npx playwright install --with-deps chromium
```

`npm ci` installs exactly the versions in `package-lock.json`, and `package.json` pins Playwright
exactly, with no range. Playwright pins the Chromium revision it downloads, so the driver version
selects the browser version and there is no second number to keep in step. The harness prints the
Playwright version and the Chromium revision in its output, and `tests/browser/README.md` records
the current pair. Versions change together, in one change, with the recorded revision and any
screenshot evidence updated in the same commit. There is no `playwright@latest`, no auto-update, and
no download at run time: if Node, the package, or the browser is missing, the check fails with the
exact missing fact and the command that installs it.

**4. Test toolchain, not product toolchain.**

The Node dependency is test-only. It never builds, bundles, or serves `bittern-ui`; it is not in the
app bundle, the image, or the device SBOM, it is not needed at runtime, and `make assets` does not
touch it. [ADR 0019](./0019-ui-stack.md)'s no-Node rule governs the shipped product build and the
asset pipeline, and it still holds. Playwright is recorded in
[THIRD_PARTY_NOTICES.md](../../THIRD_PARTY_NOTICES.md) as a test-only dependency when the harness
lands.

**5. The check covers Chromium at two widths, and nothing else.**

The harness asserts what the ticket names and what one engine can observe: page-level overflow,
controls outside the viewport, element overflow that hides text, and a screenshot per route and
width. It does not cover Firefox or WebKit, real touch input, device pixel ratio, dynamic viewport
units, browser zoom, the self-signed TLS interstitial, or whether a physical camera can scan the
QR. Anything in that list is a manual review item or a later change, never a hidden pass.

**Explicitly out of scope.** This ADR does not decide screenshot retention
([OQ-28](../open-questions.md)), the touch-target floor ([OQ-26](../open-questions.md)), whether
the layout must also hold at 360 CSS pixels ([OQ-27](../open-questions.md)), or how later tickets
extend the route list ([OQ-29](../open-questions.md)). It adds no CI, no pixel-diff visual
regression, no second browser engine, and no browser to the appliance image or the app bundle.

**Provisional until [OQ-26](../open-questions.md) and [OQ-27](../open-questions.md) are answered.**
The check measures the phone viewport at **390 by 844 CSS pixels** and the desktop viewport at
**1280 by 800**, matching the ticket's acceptance sentence. It fails a control smaller than **24 by
24 CSS pixels**, the WCAG 2.2 AA SC 2.5.8 minimum, and reports controls below 44 by 44 as warnings
without failing. The phone width and the control floor are placeholders for the open questions,
chosen as the smallest reversible commitment that makes the ticket testable; the warning list is
the evidence for OQ-26. Neither value is a design standard until its question is answered.

## Consequences

### Positive

- The ticket's acceptance becomes falsifiable. Overflow, an out-of-viewport control, and the
  screenshots are observed in a real layout engine, not inferred from HTML.
- The offline promise survives. `make check` gains no browser, no Node, and no network.
- One pinned pair (Playwright and its Chromium) is a smaller version surface than a hand-written
  driver plus a distro browser that moves independently.
- The harness is ordinary community tooling, not a private protocol client that Bittern would own
  and maintain ([ADR 0022](./0022-community-functionality-ux-wins.md)).
- The same route manifest and widths can later run in a QEMU guest or CI without rewriting the
  assertions.

### Negative / costs

- **A browser on the developer machine is a manual prerequisite.** It is a few hundred megabytes on
  disk, `npm ci` and the browser download need network once, and `--with-deps` needs root or
  preinstalled system libraries. A contributor without that setup cannot run the check.
- **Development runs in a container, so the harness must run fully headless.** The browser has no
  display there, and `npx playwright install --with-deps chromium` needs root or preinstalled system
  libraries. The container filesystem is ephemeral, so the browser must be provisioned again after
  the container is recreated.
- **The harness is not reproducible from a bare offline clone.** That is the opposite of the Go
  vendoring rule ([ADR 0015](./0015-agent-language.md)) and is accepted only because the harness is
  test-only and opt-in.
- **Nothing runs it automatically.** With no CI, the check runs when a developer or reviewer runs
  it. A UI change can pass review without it unless the reviewer asks, which is why BIT-020
  acceptance names the check.
- **Two versions drift in practice.** A developer who skips `npm ci`, or uses a global Playwright,
  gets a different Chromium. Printing the revision makes that visible; it does not prevent it.
- **It is slow and heavy compared with `make check`.** A browser start, a server, and a screenshot
  per route and width make it unsuitable as a pre-commit hook.
- **The npm and browser downloads add supply-chain and license-review work** that the vendored Go
  tree does not have.
- **One engine is not the web.** Chromium at two fixed viewports will not catch a Safari,
  Firefox, or real-phone defect.
- **A green check is not a usable screen.** The harness asserts geometry; the screenshot is evidence
  for the human, and a passing run can still look wrong.

### Follow-ups

- BIT-020 adds `tests/browser/` with the harness, the route manifest, the pinned package files, a
  README with the prerequisite commands and the current versions, and the `ui-browser-check` target.
- Preinstalling the browser in the development container image would remove the per-container
  provision step. That is a separate change, not part of this ADR.
- Add Playwright to [THIRD_PARTY_NOTICES.md](../../THIRD_PARTY_NOTICES.md) as test-only, with its
  license and the Chromium license noted as not distributed.
- Answer [OQ-26](../open-questions.md) (touch-target floor), [OQ-27](../open-questions.md) (360 or
  390), [OQ-28](../open-questions.md) (screenshot retention), and [OQ-29](../open-questions.md)
  (route coverage for later tickets), then replace the provisional numbers here and in the recon
  note.
- Prove the QR is scannable at phone width by decoding the screenshot, or record why decoding is not
  part of the check. A decoder belongs to the same pinned test toolchain if it is added.
- Revisit the runner when CI exists ([ADR 0021](./0021-security-updates.md)): the target should then
  run in a container with the browser preinstalled, and the manual prerequisite becomes a CI image
  step.
- Revisit when OQ-25 (kiosk) adds a browser engine to the image; the check may then also run in a
  QEMU guest.

## Alternatives considered

- **Puppeteer instead of Playwright.** Rejected: Puppeteer drives Chromium well but brings no test
  runner, no viewport projects, and no trace or screenshot helpers, so the harness would add a
  runner and an assertion library beside it. Playwright is one dependency that brings the driver,
  the runner, the browser revision, and screenshots, and it can add Firefox and WebKit later without
  a second tool.
- **Driver-free headless Chromium invoked directly.** Rejected: `chromium --headless --screenshot`
  renders a page, but checking overflow or reachability needs scripted evaluation inside the page,
  which means a DevTools Protocol client (WebSocket, target sessions, JSON messages) that Bittern
  would own. That is the custom code ADR 0022 tells us to avoid, it tracks a moving protocol, and
  the distro browser version drifts with the base image instead of with a lockfile.
- **Static assertions only, no browser.** Rejected: it cannot observe overflow, clipping, overlap,
  reachability, or QR scannability, so it would turn an unfalsifiable ticket into a green check that
  proves nothing. It is a useful complement in the Go template tests, which already parse the
  rendered HTML, but not a substitute.
- **Committing golden HTML and diffing it.** Rejected as the browser check: a DOM snapshot detects a
  markup change, not a layout failure, and it has no viewport, no CSS cascade, and no screenshot.
  The repository already has golden render tests for the renderer; a second snapshot layer for the
  UI would add churn without adding layout evidence.
- **A hosted service (BrowserStack, LambdaTest, Percy, or a hosted browser job).** Rejected for now:
  it adds an account, a bill, credentials, and an external data path for a local-first appliance
  whose UI is reachable only on a LAN behind a self-signed certificate. There is no CI to run it in.
  The reason to revisit is a self-hosted runner, not a vendor.
- **A browser in the appliance image or the QEMU guest.** Rejected: it grows the shipped image and
  its attack surface for a development-time check, and the appliance is deliberately browser-free.
  The only reason to put a browser engine on the device is the deferred kiosk question (OQ-25).
- **Manual review only, no repeatable check.** Rejected: the ticket asks for a repeatable check, the
  current defects came from manual review missing narrow widths, and a reviewer cannot measure
  `scrollWidth` by eye consistently.

## References

- [ADR 0012: Testing and release gates](./0012-testing-and-release-gates.md)
- [ADR 0015: Agent language](./0015-agent-language.md)
- [ADR 0019: UI stack and TLS](./0019-ui-stack.md)
- [ADR 0021: Managed security updates and release automation](./0021-security-updates.md)
- [ADR 0022: Community functionality, UX wins](./0022-community-functionality-ux-wins.md)
- [Implementation plan](../plan/implementation-plan.md), BIT-020
- [UI README](../../ui/README.md)
- [Principles](../principles.md), UX-8 and the UX review checklist
- [Recon note: browser check mechanism](../notes/2026-11-17-browser-check-mechanism.md)
- [Open questions](../open-questions.md)
- Playwright: <https://playwright.dev/docs/intro>
- Playwright browsers and versions: <https://playwright.dev/docs/browsers>
- WCAG 2.2 SC 2.5.8 Target Size (Minimum):
  <https://www.w3.org/WAI/WCAG22/Understanding/target-size-minimum.html>

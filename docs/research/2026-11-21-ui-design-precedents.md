# UI design precedents, amateur tells and review technique

- Date: 2026-11-21. Status: research note, no product decision
- Scope: Bittern's five screens, the signals that make an interface read as unfinished, the rules that
  make copy read as professional, and how a designer reviews a screen
- Related: [principles](../principles.md), [UI README](../../ui/README.md),
  [ADR 0019](../adr/0019-ui-stack.md), [ADR 0025](../adr/0025-browser-check-for-the-responsive-ui.md)

Bittern's five screens are the first-run network step, the nine-section status page, the camera list,
the camera edit form, and the review and apply step. This note examines how well-regarded technical
products handle them, then states rules a reviewer can apply without taste arguments. UI behavior was
checked on 2026-11-21 and changes quickly; re-check before relying on a detail.

## 1. Precedents

| Product | Copy this decision | Do not copy this | Source |
| --- | --- | --- | --- |
| Home Assistant | Discovery is a first-class onboarding state: a found-device list with one Add action and a complete "All set!" outcome. Repairs is a counted list of named, fixable issues, not a metric wall | A four-level vocabulary (integration, device, entity, area) that needs a concepts page before the first task, and a second YAML path for the same job | HA onboarding, Repairs, concepts |
| UniFi Network | The device list states lifecycle in words: Pending Adoption, Online, Offline, Waiting Replacement. Adoption is one action from the row or property panel | Recovery from a failed Layer 3 adoption is documented as SSH and `set-inform`, so the browser surface stops where the diagnosis starts. State names describe the controller's process, not the owner's situation | UniFi adoption, ZTP |
| Pi-hole | One dashboard answers "is it working": total and blocked queries, activity, upstream servers, top domains. The query log is the evidence behind the numbers | Expert mode exposes every daemon option, including raw keys, and the README marks section after section "[Expert]". "Every setting, one place" becomes a config file with web styling | pi-hole/web, Pi-hole |
| Synology DSM | First storage setup is a wizard with a recommended default and a plain explanation of the trade-off. Long work has a progress surface, not a frozen page | Progress is split across four taskbar icons by subsystem (Upload Queue, Storage Manager, Task Manager, Notifications) and appears only while a task runs. The owner must know which icon owns the work | DSM desktop help, DSM guide |
| TrueNAS | A network change that can cut access is provisional: Test Changes starts a 60-second window, the old session can Revert Changes, Save Changes makes it permanent. The new state must prove reachable before commit | The test is manual (open a second window and log in again) and the window is fixed. The agent can check the new address, DNS and UI from its side; do not offload the health check to the owner | TrueNAS interfaces |
| Grafana | "No data" is a distinct state and "No value" a separate formatting choice, so a panel author can say what an empty field means | The default empty panel says only "No data"; diagnosis lives in the query inspector, a tool for dashboard authors. An owner-facing empty state must name the condition, scope and next action | Grafana panel and standard options |
| Frigate | Config edits accumulate as pending changes. "Review pending changes" lists scope (global or camera), field and new value before Save All. Restart-required fields carry an inline marker and the save offers one restart action | Save & Restart can show a restarting spinner while validation has failed and lose the edit (discussion 17131), and the raw YAML editor remains an equal path. Validate before promising progress | Frigate config, discussion 17131 |
| Tailscale | Policy changes can be previewed and tested before save. The visual editor generates a live JSON preview, and tests validate expected access before commit | Tests and policy are authored in JSON; the visual editor is a second view of an expert file. A UI that exposes its storage format has not finished the abstraction | Tailscale editor, ACLs |

### What to take to each Bittern screen

| Screen | Best precedent | Rule to adopt |
| --- | --- | --- |
| First-run network | TrueNAS test and revert; Home Assistant discovery | Show discovered links and prefill them, one primary action, and treat the new address as provisional until the agent confirms it |
| Status with many sections | Home Assistant Repairs; NN/g status trackers | One overall state sentence, then issues ordered by action; evidence behind a disclosure |
| Camera list | UniFi devices; Tailscale machines | One row per device, one state word, the deciding fact (address, last seen), one action |
| Edit one camera | Frigate pending changes; Tailscale preview | Field-level changes, a review before apply, and no secret ever echoed back |
| Long-running apply | Frigate restart markers; Synology progress; TrueNAS revert | Named steps, elapsed time, resumable, one operation at a time, truthful failure |

## 2. Amateur tells

Each item substitutes a local decision for a system.

| Tell | Why it reads unfinished | Professional alternative | Source |
| --- | --- | --- | --- |
| Mixed radii and shadows | Each value claims a different physical layer; three radii and four shadows look assembled from different kits | One shape scale and two elevations: page and overlay. Depth only for real layering | Material shape and elevation |
| Boxes inside boxes | Nested borders repeat the same grouping signal, so every boundary competes and the inner box reads as a dialog inside a card | Spacing and alignment first, one container level; nest only when the inner object is independently actionable | NN/g visual hierarchy |
| Arbitrary spacing | Gaps encode relationships; 13px beside 18px tells the eye the wrong story and breaks the rhythm | One 4/8px scale, equal gaps for siblings, a larger gap between groups | Material spacing |
| Label duplication | A "Network" heading, a "Network" label and a "Network settings" button spend three lines on one idea, and none says which value | Heading names the object, label names the property, hint names the constraint | GOV.UK UI writing; Apple writing |
| Inconsistent capitalisation | Sentence case, Title Case and ALL CAPS read as three voices, and caps blocks slow reading | Sentence case everywhere except proper nouns; publish the exceptions | GOV.UK; Material; Apple |
| Icon salad | Most icons are not universal; an icon per heading and per row adds noise and invites guessing | Text labels; an icon only with a label and a learned meaning; one family and size | NN/g icon usability |
| Centred paragraphs | Every line starts somewhere else, so long copy is slow to read and looks like a poster | Left-align body copy; centre only a short empty-state line; keep 45-90 characters | Butterick |
| Placeholder as label | The label vanishes while typing and after an error, has low contrast, and can be mistaken for a value | Persistent label above the field; placeholder only for a format example | NN/g placeholders; WCAG 2.5.3 |
| Every state coloured red | When updating, empty and failed are all red, red stops meaning "act now"; colour alone also fails 1.4.1 | Severity roles (neutral, info, warning, error, success), each with a word and icon; red only for blocking errors | NN/g errors; WCAG 1.4.1 |
| A modal for everything | Modals interrupt, trap focus and hide the context the decision needs; a flow of modals feels like interruptions, not steps | Inline edit or disclosure for routine work, a page for complex work, a modal only for a short decision or destructive confirmation | NN/g dialogs |
| Progress that is not progress | An indeterminate spinner for a 90-second apply, or a bar that restarts, teaches the owner to distrust status | Named steps with states, elapsed time, resumable; loop for 2-9s, percent for 10s+ | NN/g progress; status trackers |
| Raw identifiers in owner copy | `cam0`, `/etc/bittern/secrets.yaml`, `401` and `degraded` are the system's nouns; they make the UI read as a debug view and force translation | Role names and a plain sentence; raw value in evidence or the support bundle | GOV.UK; Apple; NN/g status trackers |
| Nine equal cards | Identical weight gives no entry point, and a destructive action looks like a readout | One summary and one primary action, then sections ordered by what needs a decision | NN/g visual hierarchy; GOV.UK buttons |

## 3. Content design

**Sentence case, one voice.** Use sentence case for headings, labels, buttons and table headers.
Capitalise proper nouns only. Material applies this to all product text, including titles, navigation
and buttons; GOV.UK and Apple say the same. A caps block is not emphasis, it is a different typeface
pretending to be one.

**Put the verb on the button.** A button names the action and result: "Save and continue", "Add
camera", "Apply settings", "Remove camera". Not "OK", "Submit", "Yes", or "Continue" when the next
screen does something specific. Apple says labels are almost always verbs; GOV.UK's button component
lists the result in sentence case; Material requires one line that is not truncated. One primary
action per screen; two filled buttons means no primary action.

**Error message anatomy.** Three parts: what happened, why it matters (only if it helps), what to do.
In a dialog the title is the problem, the body is cause and fix, the button is the action. Next to a
field, the message names the field and the fix. GOV.UK requires the message to explain what went
wrong and how to fix it; Microsoft uses the same anatomy. Do not blame, do not open with "oops", do
not show a code without a sentence. WCAG 3.3.1 requires the item in error to be identified in text;
3.3.3 requires a suggestion when one is known.

Example: "The camera did not answer at 192.168.20.31. Check that it is switched on and connected to
the camera network." The raw cause ("connection timed out") belongs in the evidence disclosure.

**Name one thing one way, in the owner's words.** Pick one verb for the transaction. If the product
says "Apply", do not also say Save, Commit, Deploy and Write. If it says "camera", do not say device,
unit, endpoint and source. A synonym reads as a different thing and makes the owner wonder which one
is destructive. Then remove the system's nouns: replace `cam0` with "camera network", `lan0` with
"home network", `appliance.yaml` with "settings", and `401` with "the camera needs a sign-in". Jargon
is allowed only when the owner must type it, such as an address or a port; keep the raw value in a
disclosure or the support bundle. GOV.UK, Apple and NN/g all say to drop the internal noun.

**Make the hint earn its line.** Keep a hint only if it changes the answer (a constraint, format or
unit), explains a non-obvious consequence, or names the source of a prefilled value ("Found on the
camera network"). Delete it if it restates the label. GOV.UK says to add help text only when research
shows it is needed; Apple says to add an explanation only when the label is not enough.

**Say what a number is, and is not.** Frigate's storage page is the model: the figure is recordings,
not free disk space, and it says why it will not match `df`. Bittern's status cards should say what
each count covers and excludes: "3 cameras configured, 3 responding"; "recording disk: 62% used by
recordings, 18% free". Avoid an unqualified "degraded": name the failed check.

## 4. Review technique

### The order to look

1. Read the route, title and primary action. Say the page's job in one sentence. If you cannot, that
   is the first finding.
2. Squint or blur the screenshot by 5-10px. Note the first three things that survive, and compare
   that order with the intended order.
3. Look at phone width first (360 and 390) for priority, wrapping and target size; then desktop
   (1280 and wider) for measure, empty space, alignment edges and density.
4. Read the copy top to bottom, then read only the buttons and links. Do they still tell the story?
5. Tab through it, then use it keyboard-only. Check focus order, visibility, and what a sticky header
   covers.
6. Walk the state matrix before judging polish. A missing state is a bigger finding than a radius.

### Hierarchy and rhythm in a minute

- Count type sizes and weights: more than four or five sizes, or three weights, means no scale.
- Count spacing values: sibling gaps equal, section gaps larger, no more than six to eight values.
- Count radii and shadows: one or two radii, one overlay elevation.
- Trace the first three fixations; if a decorative icon or a nav item wins, the hierarchy is wrong.
- Measure line length: 45-90 characters; a full-width paragraph at 1280px is often 150.
- Count left alignment edges; more than three or four means grouping is accidental.
- Compare the primary button with everything else; two loud things means no primary action.

### Review states, not the happy path

For every screen, capture: empty, one item, many items (20+), a long name and a non-ASCII name,
loading at 0, 2 and 30 seconds, partial discovery, authentication required, unreachable, validation
error, apply running, apply failed, apply succeeded but health failed, expired session, JavaScript
disabled, 360px, 200% zoom, keyboard only, and reduced motion.

The camera flow has more states than the status page and is the likely place to find an undesigned
one: a camera found but needing a login, one that answers ONVIF but not RTSP, one removed while an
apply was running.

### Wrong, taste, or unknown

Apply three tests to every observation:

1. Name the rule: a WCAG criterion, a design-system token, a platform convention, or a Bittern
   principle.
2. Name the evidence: route, viewport, state, screenshot, measurement, or the exact string.
3. Name the consequence for the owner: what they cannot tell, find, or recover from.

If all three hold, it is a finding. If only the rule holds, it is a principle to discuss. If none
hold, it is taste: say so and do not spend review time on it. NN/g makes the same distinction: a
usability problem needs an objective explanation, not a personal preference, and a finding carries a
severity and a recommendation.

Template: `[severity] [rule] at [route, viewport, state]: [evidence]. The owner [consequence]. Fix:
[smallest change].`

Example: `[Major] 3.3.1 and 3.3.3, /ui/cameras/{id}/edit at 390px, empty port: the page says "The
camera change was not applied" at the top, the port field is not marked, and the message does not
name the field. Fix: put "Enter a port between 1 and 65535" next to the port field.`

Severity: Nielsen's 0-4 scale (0 not a problem, 1 cosmetic, 2 minor, 3 major, 4 catastrophe), or
High/Medium/Low with frequency, impact and persistence. One finding gets one severity and one fix.

### WCAG 2.2 checks to name

| Criterion | Level | What to check on the screen |
| --- | --- | --- |
| 1.3.1 Info and Relationships | A | Headings in order, labels tied to inputs, fieldset and legend for radio groups, table headers |
| 1.4.1 Use of Color | A | No state, error or selection conveyed by colour alone; a word or icon carries it too |
| 1.4.3 Contrast (Minimum) | AA | 4.5:1 body text, 3:1 large text; check hints, muted metadata and disabled text |
| 1.4.11 Non-text Contrast | AA | 3:1 for input borders, focus ring, icons and status dots |
| 2.2.1 Timing Adjustable | A | Any time limit can be extended, or is essential; check the 10-minute setup code |
| 2.3.3 Animation from Interactions | AAA | Honour `prefers-reduced-motion`; the level is AAA, but Bittern should adopt it |
| 2.4.7 Focus Visible | AA | Every keyboard-operable control has an obvious focus indicator; no `outline: none` |
| 2.4.11 Focus Not Obscured (Minimum) | AA | A sticky header or footer does not cover the focused control |
| 2.5.3 Label in Name | A | The accessible name contains the visible label; icon-only buttons have a name |
| 2.5.8 Target Size (Minimum) | AA | 24 by 24 CSS pixels, or the spacing exception; Bittern's floor is 24 with a 44 target |
| 3.3.1 Error Identification | A | The item in error is identified and the error is described in text |
| 3.3.3 Error Suggestion | AA | A correction is suggested unless it would compromise security |

2.3.3 is AAA, not AA, but Bittern should adopt it; pair it with 2.2.2 Pause, Stop, Hide (A).

## The professional test

Ten questions whose answers are evidence, not opinion. A "no" or "I do not know" is a finding.

1. What is this page's one job, and where is the one filled button that does it?
2. Squint: what are the first three things you see, in order, and is that the intended order?
3. Name every spacing, radius, shadow and type size; which ones are outside the token set?
4. At 360px and 1280px, what wraps, truncates, or runs past 90 characters per line?
5. What shows before data, while loading, when unreachable, when auth fails, when apply fails, when
   it succeeds?
6. Tab through: is focus visible on every control, in order, and never behind a sticky header?
7. Does every error name the field, say what is wrong, and state the fix in text?
8. Does every label match the accessible name, and is every target at least 24 by 24 px?
9. Read aloud: sentence case, verbs on buttons, one noun per object across screens?
10. Does owner copy contain a path, identifier, HTTP status or service name? Where is the evidence?

## Mechanical rules for a review

| Rule | Check |
| --- | --- |
| One primary action | Exactly one filled button; the rest are secondary or links |
| Sentence case | No Title Case, no caps blocks, except proper nouns |
| Verbs on buttons | Every label starts with an action; no OK, Submit or Yes |
| One spacing scale | Sibling gaps equal; section gaps larger; no off-scale values |
| One shape scale | One or two radii; two elevations at most; no shadow without a layer |
| No nested containers | A card inside a card only when the inner object is independently actionable |
| Readable measure | Body copy left-aligned, 45-90 characters, no centred paragraphs |
| Visible labels | Label above the field; placeholder is never the label |
| Error anatomy | What happened, why, what to do, next to the field and in the summary |
| Status semantics | Word, icon and colour; red only for blocking errors |
| Honest progress | Named steps, elapsed time, 250ms delay, resumable, no fake percentage |
| Owner vocabulary | No path, identifier, HTTP status or service name in primary copy |
| Targets and focus | 24 by 24 minimum, 44 preferred, visible focus on everything |
| States covered | Empty, loading, partial, error, success, phone, keyboard, reduced motion |
| Findings | Rule, evidence, consequence, fix and severity in every one |

## Where Bittern is most exposed

1. **Boxes inside boxes on the status page.** Nine cards, inner state lists and per-item badges all
   draw a boundary. The page needs one overall sentence and a ranked issue list, evidence behind a
   disclosure, before it needs another border.
2. **Colour as the state system.** The overall state, card badges and discovery layer badges all use
   colour. If updating, empty and failed share red, the owner learns to ignore red, and colour alone
   fails 1.4.1.
3. **Implementation nouns in owner copy.** `cam0`, `appliance.yaml`, "render checks" and "apply" are
   the system's words and force the owner to translate.
## Sources

| Group | Full URLs |
| --- | --- |
| Home Assistant | https://www.home-assistant.io/getting-started/onboarding ; https://www.home-assistant.io/integrations/repairs ; https://www.home-assistant.io/getting-started/concepts-terminology/ |
| UniFi | https://help.ui.com/hc/en-us/articles/360012622613-UniFi-Device-Adoption ; https://help.ui.com/hc/en-us/articles/23954985107351-UniFi-Zero-Touch-Provisioning-ZTP-and-Device-Replacement |
| Pi-hole | https://github.com/pi-hole/web/blob/master/README.md ; https://pi-hole.net |
| Synology | https://kb.synology.com/en-uk/DSM/help ; https://kb.synology.com/en-br/UG/Syno_UsersGuide_NAServer_7_4/2 |
| TrueNAS | https://www.truenas.com/docs/scale/25.04/scaletutorials/network/interfaces |
| Grafana | https://grafana.com/docs/grafana/latest/visualizations/panels-visualizations/configure-panel-options ; https://grafana.com/docs/grafana/latest/visualizations/panels-visualizations/configure-standard-options |
| Frigate | https://docs.frigate.video/configuration/config ; https://github.com/blakeblackshear/frigate/discussions/17131 |
| Tailscale | https://tailscale.com/docs/features/visual-editor ; https://tailscale.com/docs/features/access-control/acls |
| GOV.UK | https://www.gov.uk/service-manual/design/writing-for-user-interfaces ; https://design-system.service.gov.uk/components/button/ ; https://design-system.service.gov.uk/components/error-message/ ; https://design-system.service.gov.uk/components/error-summary/ |
| Apple | https://developer.apple.com/design/human-interface-guidelines/writing ; https://developer.apple.com/design/human-interface-guidelines/layout |
| Material | https://m3.material.io/foundations/content-design/style-guide/ux-writing-best-practices ; https://m3.material.io/components/buttons/guidelines ; https://m3.material.io/styles/spacing ; https://m3.material.io/styles/shape ; https://m3.material.io/styles/elevation |
| Microsoft and Butterick | https://learn.microsoft.com/en-us/dynamics365/business-central/dev-itpro/developer/devenv-error-handling-guidelines ; https://practicaltypography.com/line-length.html ; https://practicaltypography.com/summary-of-key-rules.html |
| NN/g | https://www.nngroup.com/articles/error-message-guidelines ; https://www.nngroup.com/articles/visual-hierarchy-ux-definition ; https://www.nngroup.com/articles/form-design-placeholders ; https://www.nngroup.com/articles/icon-usability ; https://www.nngroup.com/articles/modal-nonmodal-dialog ; https://www.nngroup.com/articles/status-tracker-progress-update ; https://www.nngroup.com/articles/progress-indicators ; https://www.nngroup.com/articles/ux-expert-reviews ; https://www.nngroup.com/articles/how-to-rate-the-severity-of-usability-problems ; https://www.nngroup.com/articles/ten-usability-heuristics |
| WCAG 2.2 | https://www.w3.org/TR/WCAG22/ ; https://www.w3.org/WAI/WCAG22/Understanding/target-size-minimum.html ; https://www.w3.org/WAI/WCAG22/Understanding/focus-visible.html ; https://www.w3.org/WAI/WCAG22/Understanding/error-identification.html ; https://www.w3.org/WAI/WCAG22/Understanding/error-suggestion.html ; https://www.w3.org/WAI/WCAG22/Understanding/label-in-name.html ; https://www.w3.org/WAI/WCAG22/Understanding/animation-from-interactions.html |

# Review: document truth and internal consistency

- Date: 2026-11-21
- Status: review note; read-only audit; no fix applied, no plan or ticket edit
- Scope: the implementation plan's current state and queue, every BIT ticket, the open questions,
  the session brief index, the two GK41 runbooks, dates and links, at HEAD `b1d4c4e`.
- Method: read each claim against the tree, `git log`, and the cited notes; ran
  `make link-check style-check`; did not rerun the browser check or QEMU. Items marked inferred are
  not proved by a run or a note.
- Related: [implementation plan](../plan/implementation-plan.md),
  [open questions](../open-questions.md),
  [backlog grooming](2026-11-15-backlog-grooming.md),
  [Level 2 run](2026-11-17-level2-hardware-run.md),
  [clean-install runbook](../runbooks/gk41-clean-install.md),
  [POC runbook](../runbooks/gk41-poc.md).

## Summary

26 findings: 4 blocker, 14 major, 8 minor. The common cause is that the prose still describes the
2026-11-18 tree. It does not record the 2026-11-21 design system, browser-check expansion and
storage script, the 2026-11-18 release-gate fixes, or the failed 2026-11-17 Level 2 run.

**Order assessment.** The dependency order holds for the cold pull and the silent waits: BIT-009
gates BIT-013, BIT-013 gates BIT-014, and the Level 2 note itself recommends finishing the gate
before BIT-009 and BIT-013. The one strain is storage. The field evidence calls manual prep the
named exception, and the Level 2 note's recommendation puts BIT-012 before BIT-028, while the queue
puts BIT-012 at item 14, after BIT-029. The stopgap reduces the pain but does not remove the ticket.
I do not propose a reorder; the queue should at least name the stopgap and the narrowed exception at
item 3.

## Findings

| ID | Area | Finding | Evidence | Severity | Destination |
| --- | --- | --- | --- | --- | --- |
| F1 | Plan current state | The Exists line says "ADRs 0001 to 0024, with ADR 0021 proposed" and the list omits ADRs 0025 to 0027, the design system, the browser check and `packaging/storage-setup.sh`; the wizard is still called a skeleton. The inventory understates the tree and a reader will not find the current design or storage work. | `docs/plan/implementation-plan.md:37-42`; `docs/adr/README.md:74-76`; `Makefile:70,181`; `packaging/storage-setup.sh:1-22`; commits `c71d798` to `b1d4c4e` | blocker | plan current-state update |
| F2 | Plan current state | "Missing ... live verification of the wired Frigate env file and unit lifecycle in QEMU" is false for the unit lifecycle. The 2026-11-18 note records the post-apply reboot, all units enabled, the active set active, Frigate ok and status ready in the firstboot, stock-name and clean-network scenarios; the plan itself claims the same at 361-369. The env file may still be unverified. | `docs/plan/implementation-plan.md:43-46` versus `:361-369`; `docs/notes/2026-11-18-frigate-not-after-reboot.md:157-180` | blocker | plan current-state update |
| F3 | Plan current state | "The remaining UX findings are in the camera wizard UX findings note" is no longer the whole list. The 2026-11-17 Level 2 run added BIT-033 and UX-11/UX-12; following the old pointer misses the current UI backlog. | `docs/plan/implementation-plan.md:23-25`; `docs/notes/2026-11-17-level2-hardware-run.md:108-157`; `docs/plan/implementation-plan.md:778-783` | major | plan current-state update |
| F4 | Plan current state | "Level 2 ... is still unproven" is literally true but hides that Level 2 ran on 2026-11-17, failed the reboot checks, and now has two fixes awaiting a rerun. The history and the next action are missing. | `docs/plan/implementation-plan.md:49-51`; `docs/notes/2026-11-17-level2-hardware-run.md:1-8,40-56,248-256` | major | plan current-state update |
| F5 | Plan current state | "The QEMU firstboot scenario is green as of 2026-11-14" is a stale date. The scenario changed on 2026-11-18 (baseline units disabled, post-apply reboot) and was re-verified then. | `docs/plan/implementation-plan.md:21`; `docs/notes/2026-11-18-frigate-not-after-reboot.md:157-180` | minor | plan current-state update |
| F6 | Near-term queue and BIT-020 | Item 2 and the BIT-020 status report 51 of 51 route-width screens at three widths and 171 warnings. The harness now declares four widths and 68 screens, writes 68 screenshots, and the 2026-11-17 width commit records 228 warnings. BIT-033 already says four widths, so the plan contradicts itself. | `docs/plan/implementation-plan.md:143-145,720-727,782-783`; `tests/browser/README.md:80-82`; `tests/browser/check.mjs:23-26`; `git show 4867ec5` (commit message, not a current rerun) | blocker | queue and BIT-020 status update |
| F7 | Near-term queue | Item 3 is the right next gate, but it does not say the 2026-11-17 run failed on networkd and Frigate, that both are fixed, or that the payload must be rebuilt before the rerun. A reader can rerun the old payload and repeat the failure (inferred consequence). | `docs/plan/implementation-plan.md:149-153`; `docs/notes/2026-11-17-level2-hardware-run.md:40-56,248-253`; `docs/notes/2026-11-18-networkd-disabled-after-reboot.md:102-116` | major | queue update |
| F8 | Near-term queue | Items 3 and 14 still describe storage as the named manual exception and a "literal no-manual-step" blocker, with no mention of `storage-setup.sh`. `install.sh` now mounts a labelled ext4 drive without a person, so the head and BIT-012 item understate what is automatic. | `docs/plan/implementation-plan.md:152,192-195,386-393`; `packaging/install.sh:201-232,1004-1005`; `docs/runbooks/gk41-clean-install.md:123-131` | major | queue update |
| F9 | Runbook, clean install | The runbook says a prepared ext4 drive labelled `bittern-media` is mounted without a manual step and also that "a literal no-manual-steps Level 2 is not achievable until BIT-012 lands". The blanket claim is false for the labelled-ext4 case and contradicts the runbook's own step 5. | `docs/runbooks/gk41-clean-install.md:123-131,133-139,736-745,750-755` | blocker | runbook update |
| F10 | Runbook, clean install | "The drive must already be mounted there before the camera apply" still reads as an operator precondition. `install.sh` runs `storage-setup.sh` before first boot, so the operator no longer has to perform the mount for a labelled ext4 drive. | `docs/runbooks/gk41-clean-install.md:117-121`; `packaging/install.sh:1004-1005` | major | runbook update |
| F11 | Runbook, clean install | The Level 2 pass criterion says "the only non-automatic input was the Debian install plus the one named temporary exception" and then adds camera credentials as owner input. The sentence undercounts the non-automatic input it allows. | `docs/runbooks/gk41-clean-install.md:750-756` | minor | runbook update |
| F12 | Tickets | BIT-016, BIT-017 and BIT-018 (and BIT-001 to BIT-007 and BIT-011) are done or landed, but none has a Status line. BIT-016/017/018 passed in Level 1, cite the cleanup, bootstrap and hardware-ledger notes, and their scripts and tests exist, yet the plan presents them as ordinary open tickets. The done inference for the older tickets comes from the current state and the plan truth pass, not from a ticket status. | `docs/plan/implementation-plan.md:240-334,536-671,1446-1456` (headings only); `docs/notes/2026-11-15-gk41-hardware-run.md:106-117`; `packaging/cleanup.sh`; `tests/cleanup/test_cleanup.py` (inferred for BIT-001 to BIT-007 and BIT-011) | major | ticket status update |
| F13 | Tickets | BIT-035 says "Depends on BIT-004, BIT-008 (landed)". BIT-008 is explicitly open: only the POC slice landed and the remaining ONVIF evidence is a BIT-028 prerequisite. | `docs/plan/implementation-plan.md:906` versus `:25-28,1457-1487` | major | ticket dependency update |
| F14 | Tickets | BIT-031's "Depends on ... BIT-010" names the whole ticket, which stays open until the reset levels land, while the queue and the ticket body say only the support-bundle slice is required. The literal dependency is stricter than the stated plan. | `docs/plan/implementation-plan.md:1738-1740,164-169,173-176` | minor | ticket dependency update |
| F15 | Tickets | BIT-019 and BIT-020 status paragraphs do not link the notes that prove them. BIT-019 has no note link; BIT-020 links ADR 0025 but not the browser-check run notes. | `docs/plan/implementation-plan.md:673-680,720-730`; `docs/notes/2026-11-17-setup-token-qr-implementation.md`; `docs/notes/2026-11-17-browser-check-first-run.md` | minor | ticket status update |
| F16 | Tickets | BIT-033, BIT-034, BIT-035 and BIT-036 are unqueued and have no owner. BIT-033's status also predates this session's design-system work: tokens, fact rows, camera surfaces and the destructive variant landed, but the ticket still reads as new and unstarted (inferred from the commits, not from a review record). | `docs/plan/implementation-plan.md:778-784,836-838,876-878,908-911`; commits `0510df9`, `ec0fc52`, `c36d19b`, `1cb788c` | major | ticket and queue update |
| F17 | Tickets | BIT-008 has a "POC slice" paragraph but no Status label, and it says the adoption scenario is green on 2026-11-15 without citing the 2026-11-18 fixture repair that fixed the scenario. The full-ticket boundary is stated; the record is not labelled. | `docs/plan/implementation-plan.md:1457-1487`; `docs/notes/2026-11-18-qemu-adoption-fixture.md:92-108` | minor | ticket status update |
| F18 | Open questions | OQ-28 still says "Decide before BIT-020 lands". BIT-020 landed on 2026-11-17 and the harness has written gitignored `build/ui-screenshots/` since then, so the deadline is past and the de facto answer is the gitignored-artifact option. The question is still open and unrecorded. | `docs/open-questions.md:78`; `docs/plan/implementation-plan.md:734-736`; `ui/README.md:196`; `.gitignore:2` | major | OQ update or decision note |
| F19 | Open questions | OQ-26's next step still says "Measure the current screens from that warning list, then decide". The measurement is done (171 warnings at three widths, 228 at four) and the CSS already uses 24 and 44, so only the decision remains. | `docs/open-questions.md:76,90-99`; `docs/notes/2026-11-17-touch-target-floor.md`; `docs/design/tokens.md:138-140` | minor | OQ update |
| F20 | Session briefs and plan prompts | The session brief index's "Next work" and the plan's suggested prompts still start with BIT-019 and BIT-020, both landed. The actual head is queue item 3, the Level 2 rerun. | `docs/plan/session-briefs/README.md:34-38`; `docs/plan/implementation-plan.md:1813-1824,149-153` | major | session brief and prompt update |
| F21 | CHANGELOG | v0.1 and v0 are dated 2026-11-18 while both say "Recorded: 2026-11-21", and the file's own rule says the version date is the merge date. v0.1 is also placed before v0.2, against "newest version first". | `docs/design/CHANGELOG.md:4,57-61,75-79,88-92,150` | major | changelog update |
| F22 | CHANGELOG | v0 is still "Proposed for review" while ADR 0026, the design README and the CHANGELOG header say the system is Accepted. v0.3 is correctly Proposed under ADR 0027; v0 is the reverse case. | `docs/design/CHANGELOG.md:4,38,91`; `docs/design/README.md:4-5`; `docs/adr/0026-design-system-and-ui-review.md:6,164`; `docs/adr/0027-flow-layer-above-patterns.md:6` | major | changelog update |
| F23 | Design docs | The design README, CHANGELOG and document headers still say "Documentation only; no `ui/` change" although the tokens, component variants and retire rules are now implemented in `ui/static/bittern.css` and the templates. | `docs/design/README.md:4-5`; `docs/design/CHANGELOG.md:4`; `docs/design/tokens.md:3-4`; `docs/design/components.md:3-4`; commits `0510df9`, `ec0fc52`, `c36d19b`, `1cb788c` | major | design doc update |
| F24 | Engineering | Field parity's Level 2 row still says the recording drive is "prepared by hand as the named BIT-012 exception". The runbook and `install.sh` now mount a labelled ext4 drive automatically; the row overstates the manual baseline. | `docs/engineering/field-parity.md:33`; `docs/runbooks/gk41-clean-install.md:123-131` | major | field-parity update |
| F25 | Claims that will age | The POC runbook's "currently" statements have no date: the first apply "currently downloads" Frigate and the wizard "currently manages one camera". Both are true today but will be wrong after BIT-013 and BIT-029, and the prewarm section presents a manual `docker pull` as a normal step. | `docs/runbooks/gk41-poc.md:230-245,614` | minor | note or ticket update |
| F26 | Claims that will age | Version claims outside the dated Frigate assessment are not dated: the UI README pins HTMX 2.0.11 with a checksum, and the upstream register pins Frigate 0.18.2, without a "checked" date. | `ui/README.md:209-214`; `docs/upstream.md:18-20` | minor | doc update |

## Checked and accurate

- OQ-27, OQ-29 and OQ-30 statuses are accurate: OQ-27 is open with both widths measured, OQ-29 is
  provisional with the manifest drift test, and OQ-30 stays open for measured sequential-add time.
  None of them is answered by a note while still listed open.
- OQ-28 is the only question that is effectively answered in practice but not recorded. OQ-26 and
  OQ-27 have measurement data but the remaining choice is policy, so I did not treat them as
  answered.
- The POC runbook's manual `mkfs` and fstab fallback is correctly labelled "Use this only when
  detection cannot identify a valid drive, and record why". No normal-path manual storage sequence
  remains in either runbook.
- No `2026-11-18-ui-design-*.md` link survives after the rename to 2026-11-21. `make link-check` and
  `make style-check` pass at HEAD.
- ADR 0021 and ADR 0027 are correctly Proposed. The only reverse case is CHANGELOG v0 (F22).
- The other open tickets (BIT-009, BIT-010, BIT-012 to BIT-015, BIT-021 to BIT-032) have dependency
  lines that match the queue except BIT-031 (F14). They are not done and need no status.

## Top five

1. **F1, the current-state inventory.** It says the ADR set stops at 0024 and omits the design
   system, the browser check and the storage script. This is the first thing a new agent reads.
2. **F2, the false "missing" unit-lifecycle claim.** The same bullet says QEMU has not verified the
   unit lifecycle, while the 2026-11-18 note and the plan's own BIT-011 bullet say it has.
3. **F6, the stale BIT-020 numbers.** The queue and ticket still publish 51 screens and 171 warnings
   from a three-width run; the harness is four widths and 68 screens.
4. **F9, the Level 2 storage contradiction.** The release-gate runbook says a labelled ext4 drive is
   automatic and that no-manual-step Level 2 is impossible until BIT-012, in the same section.
5. **F7, the queue head without the failed run.** Item 3 is the right gate but does not say the
   2026-11-17 run failed, what was fixed, or that the payload must be rebuilt before the rerun.

The next tier is F12 (three landed tickets with no status), F16 (four unqueued tickets with no
owner, one partly landed) and F23 (the design docs still claim no `ui/` change).

# Table legibility at phone width

- Date: 2026-11-21
- Status: fixed. `make ui-browser-check` reports 0 failures and 0 warnings.
- Applies to: `ui/static/bittern.css`, `ui/templates/status_panel.html`,
  `ui/templates/network.html`, and every table on the status and wizard network pages.
- Related: [design conformance F13](2026-11-21-review-design-conformance.md#findings),
  [table and table wrapper](../design/components.md#table-and-table-wrapper),
  [stacked record list](../design/components.md#stacked-record-list),
  [tokens](../design/tokens.md), [changelog v0.16](../design/CHANGELOG.md).

## Finding

`status-ready-360x800.png` showed `genera ted`, `carries the genera ted header`, and `na tive
checks` in the configuration file checks table. `overflow-wrap: anywhere` on every `th` and `td`
broke ordinary words because the three columns were compressed into the 294 pixel content width of
the card at 360. The same wrapper had no `tabindex`, role, or accessible name, so once a table
overflowed a keyboard user could not scroll it (F13, latent at the measured widths before this
change).

Keeping the words whole exposed the next defect: at 360 the table scrolled, but the `Message` column
was clipped at the card edge (`Mes` and a cut sentence) and nothing told the reader the table
scrolled. A horizontally scrollable table is allowed by the design system, but a clipped third
column reads as broken on a phone.

## Fix

| Layer | Rule |
| --- | --- |
| Words | `th` and `td` use `overflow-wrap: normal`. A cell whose whole value is a path, address, UUID, or digest gets `.break-token`, the only rule that permits a break inside a value. |
| Phone | Below 44rem the same rows render as a `.table-stack` description list: the row subject and its state on the first line, the message, address, or problem detail beneath, and `Problem`, `Cable`, and `Address` labels in text where the value would otherwise be ambiguous. The table and its named focusable region are hidden with `display: none`, so no table scrolls at phone width and the region leaves the tab order and the accessibility tree. |
| Wide | At 44rem and above the table and its scroll region stay as they were, with `.table-wide` and `--measure-table`. The caption is a visible `.table-caption` outside both versions so it stays visible and never scrolls away. |

The phone layout is a template description list, not a CSS-only stack. Stacking the table through
CSS would require `display: block` or `flex` on table elements, which the design system forbids and
which can drop table semantics. The inactive version is `display: none`, so only one version is
exposed to assistive technology at a time.

Tables affected: configuration problems, configuration file checks, status network connections,
and the wizard network connections. The wizard review and camera pages contain no table.

## Measurements at 360 by 800

| Screen | Before | After |
| --- | --- | --- |
| Status, ready | 3809 | 3655 |
| Status, failed | 3314 | 3232 |
| Status, degraded | 3857 | 3703 |
| Status, updating | 3833 | 3679 |
| Wizard network | 1183 | 1158 |
| Wizard review | 2585 | 2585 |
| Cameras | 830 | 830 |

Status ready at 1280 by 800 is 2436 before and after; the table is unchanged above the breakpoint.
The status page grows by about 70 pixels at phone width because each record gets a state line and a
full-width message. No column is clipped, every ordinary word fits, and `native-checkers` stays
whole. The page itself stays at `scrollWidth` 360.

## Verification

- `make ui-browser-check`: 0 failures, 0 warnings at 360x800, 390x844, 1280x800, and 1920x1080.
- `make design-check`, `make ui-test`, and `make check` pass; the routes test asserts every table
  wrapper is a named, focusable region.
- The 360 screenshots for `status-ready`, `status-failed`, `wizard-network`, `wizard-review`, and
  `cameras` were read, and `status-ready` was read again at 1280 by 800. The table scroll region is
  hidden at 360 and focusable with the 3px ring at 1280.

# Token regression: primary and focus colours

- Date: 2026-11-21
- Status: fixed in design-system v0.13. The blue-tint follow-ups below are open and were not
  changed.
- Applies to: `ui/static/bittern.css`, [tokens](../design/tokens.md),
  [the changelog](../design/CHANGELOG.md), and every filled primary action and focus ring.
- Related: commit `0510df9`, [tokens](../design/tokens.md),
  [the changelog](../design/CHANGELOG.md), [evolution](../design/evolution.md).

## Regression

The token refactor in `0510df9` was declared a visual no-op but changed three token values to the
link blue. It was not recorded as a token change, which is the case the change classes exist for.

| Finding | Before | After | Impact |
| --- | --- | --- | --- |
| Primary action changed | `--color-action: #0b5d4b` | `#0b4f8a` | A filled primary button read as a link, and green/blue no longer distinguished action from link |
| Primary hover changed | `--color-action-hover: #084a3c` | `#083b68` | Hover followed the same shift |
| Focus changed | `--color-focus: #0b5d4b` | `#0b4f8a` | The focus ring changed with the action |

The regression was found by reading `build/ui-screenshots/wizard-review-360x800.png`, where the
Apply button was blue. v0.13 restores the three values, keeps the token names, and leaves links at
`#0b4f8a`. The measured pairs pass: `--color-on-action` on `--color-action` 7.83:1,
`--color-on-action` on `--color-action-hover` 10.22:1, `--color-focus` on `--color-page` 7.18:1,
and `--color-focus` on `--color-surface` 7.83:1.

## Surfaces that depended on the blue action

These pairings were consistent while action was blue. With the green action restored they are
mismatched. They are recorded here rather than changed in the restore.

| Surface | Current pairing | History |
| --- | --- | --- |
| `.button.secondary:hover` | `--color-notice-surface` blue tint behind a `--color-action` green border and label | The pairing predates the token layer; it was green on blue before too |
| `.button.quiet:hover` | `--color-notice-surface` blue tint behind a `--color-action-hover` green label | The quiet variant is newer; the mismatch is newly visible |
| `.candidate-list li:has(input:checked)` | `--color-notice-surface` blue tint inside a `--color-action` green border | The pairing predates the token layer; it was green on blue before too |

Owner: frontend. Destination: the next component or token change that separates the selection and
hover tint from the notice role, or a recorded decision that the pairing is intentional.

# Full-width shell and state-aware landing

- Date: 2026-11-26
- Status: implemented. The page shell fills the device width, and the root and post-sign-in landing
  are resolved from device state behind the correct gate.
- Related: [OQ-40](../open-questions.md#oq-40),
  [ADR 0027](../adr/0027-flow-layer-above-patterns.md),
  [flows.md](../design/flows.md#the-navigation-model),
  [tokens.md](../design/tokens.md#space),
  [components.md](../design/components.md#page-shell-and-header),
  [the design-system changelog](../design/CHANGELOG.md),
  [the live view sizing note](2026-11-26-live-view-sizing.md).

## Request

The maintainer asked for two changes after the live sizing work:

1. The default Bittern view should fill the full device width, because this is an NVR dashboard
   rather than a typical web application.
2. Landing should be sensible and always behind authentication: setup before the device is
   initialized, the live view when a camera exists, and status when none does.

## Full-width shell

The shell rule is one shared rule in `ui/static/bittern.css`:

- `.wrap` is `width: 100%` with `padding-inline: var(--layout-gutter)`, and
  `--layout-gutter-wide` at 80rem and above. The `--layout-page-max: 68rem` token is removed.
- Prose opts into `--measure-prose` (65ch) through the base `p` rule and `.lede`. Help text uses
  `--measure-hint` (60ch) through `.hint`.
- Fields keep `--measure-control` (32rem) through `.field input`, `.field select` and
  `.field textarea`; the live tile select also caps at `--measure-control`.
- Tables stay inside `.table-wrap`, which is capped at 100% and scrolls internally. The status card
  grid and the live grid use the extra room.
- The header uses the same full-width shell. It is navigation chrome for a dashboard, not prose, so
  a separate header maximum would detach it from the content below.

The design-system change is a token value change plus a breaking removal: the page maximum is gone,
and the later screen that needs a narrow column must set its own inline measure. `tokens.md`,
`components.md`, the changelog, `ui/README.md`, the live pattern rules and OQ-40 record it.

## Landing resolver and gate

The rule lives in `resolveLanding` in `agent/internal/ui/landing.go`:

| Initialized | Configured cameras | Landing |
| --- | --- | --- |
| No | any | `/ui/wizard` |
| Yes | 0 | `/ui/status` |
| Yes | 1 or more | `/ui/live` |

Initialized means the setup flow completed, not merely that an administrator password exists. The
explicit marker is `/var/lib/bittern/setup-complete.json`, mode 0600, written only after a successful
first-run apply. A device that completed setup before the marker existed is migrated once from a
successful `operation.json`; a failed or half-finished setup has neither and keeps landing on setup.
The password is set before review and apply, so it cannot be the initialized signal.

The gate before authentication is the setup token while the administrator password is absent,
because a fresh box has no password and the token is the only credential that can create one. Once
the password exists, the gate is sign-in; an unfinished setup then lands on `/ui/wizard` through the
resolver. The root routes `/ui` and `/ui/` and the post-sign-in redirect both call the resolver.
`SafeNextOr` accepts only local `/ui` paths, rejects absolute URLs, protocol-relative paths, `.` and
`..` segments, backslashes and paths outside `/ui`, and falls back to the resolver. A valid `next`
target wins over the default.

The navigation marks the current section with `aria-current="page"`. It renders only for an
authenticated session, omits `Setup` when the marker exists, and omits `Live` when no camera is
configured. Cameras and Status remain because both have useful empty and recovery states.

## Tests and evidence

- `TestResolveLandingForEveryDeviceState` covers uninitialized, initialized with zero, one and
  several cameras.
- `TestLandingFollowsTheConfigurationAtRequestTime` adds the first camera and removes the last, and
  asserts the next root visit changes landing rather than a reload of the old page.
- `TestSafeNextRejectsEveryEscapeAndKeepsLocalTargets` covers absolute URLs, protocol-relative paths,
  dot segments, backslashes, schemes and paths outside `/ui`.
- `tests/browser/routing.mjs` runs all three device states at 360, 390, 1280 and 1920 on fresh
  daemons, asserts the gate first and the landing after authentication, checks that a requested
  target wins, and checks that removing the last camera changes the next visit's landing.
- `tests/browser/assertions.mjs` asserts the shell fills the viewport and that prose stays inside
  its measure at 1280 and 1920 for every route.
- `make ui-browser-check` passes 152 route-width screens, 27 live checks and 14 routing checks with
  0 failures and 0 warnings.

Screenshots read at all four widths: `live-<width>.png`, `live-nine-tiles-<width>.png`,
`status-ready-<width>.png`, `wizard-<width>.png`, `login-<width>.png`, and the
`landing-<state>-<width>.png` set. The screenshots show the full-width status grid and live wall, the
full-width header, prose still bounded, form controls still bounded, and the login and wizard pages
unchanged in reading width.

## Deliberately not changed

- The tablet and desktop device ceilings stay at four and nine. The change does not alter tile
  counts, stream profiles or the proxy's hard maximum.
- The first-run wizard still has no configured check; a direct `/ui/wizard` visit remains possible
  after setup. [BIT-036](../plan/implementation-plan.md#bit-036-first-run-and-flow-contradictions-from-the-flow-layer)
  still owns that flow contradiction.
- The header still shows `Setup` during an unfinished first run. OQ-32 still owns its eventual
  replacement.
- No new route, dependency, build step or ADR is added. The design change is a token removal plus
  component, pattern and flow rule updates.

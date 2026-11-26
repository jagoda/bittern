# Live refresh resets owner state and swaps the sign-in page into the panel

- Date: 2026-11-26
- Status: diagnosed on the code at `0db2b56`. BIT-044 and BIT-046 are implemented on 2026-11-26
  with tests and browser evidence. BIT-045 remains blocked on OQ-37.
- Type: bug, UX, and auth.
- Evidence: maintainer report from a bench box running generation 1 and then updating from the
  status card. The report has three points: expanded cards collapse on every live refresh, the
  sign-in screen appeared inside the page after the update installed, and a manual reload restored
  a usable sign-in page. The maintainer wants to keep the live updates without the unrelated state
  changes.
- Related: [status patterns](../design/patterns.md#status-page),
  [flow rule F3](../design/flows.md#f3-an-interrupted-flow-can-be-resumed-or-restarted-without-losing-work),
  [status and auth contract](../status.md),
  [BIT-044](../plan/implementation-plan.md#bit-044-preserve-owner-state-across-live-refreshes),
  [BIT-045](../plan/implementation-plan.md#bit-045-keep-the-administrator-session-across-a-daemon-restart),
  [BIT-046](../plan/implementation-plan.md#bit-046-keep-sign-in-out-of-live-fragments),
  [OQ-37](../open-questions.md#register),
  [UX-14](../principles.md#ux-14-live-updates-must-not-disturb-state-the-user-owns).

## Findings

| ID | Type | Finding | Evidence | Impact | Destination | Status |
| --- | --- | --- | --- | --- | --- | --- |
| F1 | bug, UX | The status and operation polls replace the whole panel every 15 seconds or 2 seconds. Open disclosures close, focus and selection inside the panel are lost, and table scroll resets. | [status.html](../../ui/templates/status.html):4, [status_panel.html](../../ui/templates/status_panel.html):214,241, [operation.html](../../ui/templates/operation.html):6, [operation_panel.html](../../ui/templates/operation_panel.html):36, [ui.go](../../agent/internal/ui/ui.go):313-320,356-360 | The owner loses the disclosure, focus, and reading position on every refresh. The page claims to preserve them in [patterns S6](../design/patterns.md#status-page). | BIT-044, UX-14 | open |
| F2 | bug, auth | Administrator sessions are an in-memory map. The update stops and restarts `bitternd`, so every session is gone after the update and the browser cookie names no stored session. | [auth.go](../../agent/internal/auth/auth.go):93,129,323-356, [main.go](../../agent/cmd/bitternd/main.go):40, [update engine](../../agent/internal/update/engine.go):340,347,751-752 | The owner is signed out by an update. The next poll takes the unauthenticated path. | OQ-37, BIT-045 | open |
| F3 | bug, auth | A fragment request with no or expired session gets a 303 to `/ui/login`, which the browser follows inside the XHR. The login route returns a 200 full page, and HTMX swaps it into `#status-panel`. Nothing checks `HX-Request` or sends `HX-Redirect`. | [auth.go](../../agent/internal/ui/auth.go):23,65-71, [routes.go](../../agent/internal/ui/routes.go):205-206, [ui.go](../../agent/internal/ui/ui.go):313-320, [login.html](../../ui/templates/login.html):1, [status.html](../../ui/templates/status.html):4 | The sign-in form appears mid-page. A manual reload is a top-level navigation to `/ui/status`, which redirects to a full login page. | BIT-046 | open |

F2 and F3 interact. The restart is what removes the session, so the poll reaches the unauthenticated
branch. The fragment response is what turns that into an in-page login form instead of a normal
sign-in page. Either fix alone reduces the problem: F2 lets the session survive, and F3 makes a lost
session fail safely. Both are needed for an expired session and for the no-cookie case.

## Poll attributes

There are two polling regions. The update card has no poll of its own. It is part of the status
fragment through `{{template "update-card" .}}`, so the status poll replaces it too.

| File | Element | `hx-get` | `hx-trigger` | `hx-target` | `hx-swap` | Region replaced |
| --- | --- | --- | --- | --- | --- | --- |
| [status.html](../../ui/templates/status.html):4 | `div#status-panel` | `{{.StatusFragmentURL}}`, rendered as `/ui/status/fragment` plus the preview query | `{{.StatusRefresh}}`, which is `every 15s`, or `every 2s` while an update or rollback is current or active | `this` | `innerHTML` | Every child of `#status-panel`: the `status-grid`, every card, the update card at [status_panel.html](../../ui/templates/status_panel.html):214, the Technical details disclosure at :241, and the table scroll regions at :32, :83 and :179. |
| [operation.html](../../ui/templates/operation.html):6 | `div#operation-panel` | `{{.OperationFragmentURL}}`, rendered as `/ui/wizard/apply/fragment` | `every 2s` | `this` | `innerHTML` | Every child of `#operation-panel`, including the operation cards and the Technical details disclosure at [operation_panel.html](../../ui/templates/operation_panel.html):36. |
| [update_card.html](../../ui/templates/update_card.html):1 | `section.card` | none | none | none | none | No poll. The card is nested inside the status fragment and is replaced by the status poll. Its actions are ordinary POST forms. |
| [camera.html](../../ui/templates/camera.html):214 | candidate radio | `{{$.CameraAction}}` | `change` | `#camera-identity` | `innerHTML` | Not a poll. Included because it is the only other `hx-get` in the templates. |

No application template uses `hx-preserve`. The only preservation is HTMX's default behavior of
restoring focus to an element with the same `id` after a swap; the disclosure summary and most
controls in the polled regions have no `id`.

## Fragment response by session state

The route table declares the status fragment as `Auth: true` and status 200 at
`routes.go:205-206`, but the auth middleware runs first and can replace that response. The
handler at `ui.go:313-320` runs only after the session check passes.

| Session state | Immediate response | Browser-visible result | HTMX action |
| --- | --- | --- | --- |
| Valid, unexpired session | 200 `status-panel` fragment | The fragment is swapped into `#status-panel`. | Normal swap. |
| No cookie, setup complete | 303 to `/ui/login?next=<fragment URI>` | The XHR follows the redirect. `/ui/login` creates a guest session and returns 200 login HTML. | Swaps the login body into `#status-panel`. |
| Expired session | The session is deleted at `auth.go:358-360`; then the same 303 and 200 login page as no cookie. | Same mid-page login form. | Same swap. |
| Setup still required | 303 to `/ui/setup` | The XHR follows the redirect and receives 200 setup HTML. | Swaps the setup page into the panel. |
| Daemon stopped or restarting | No response, or a connection error. | The last page stays visible; the poll retries later. | No swap on the failed request. |
| Daemon restarted, old cookie | The new process has an empty session map, so this becomes the no cookie case. | The login page appears at the next successful poll. | Swaps the login body into `#status-panel`. |

## Root cause A: the whole region is replaced

`status.html:4` targets `this` with `innerHTML`. The server renders the fragment with
`showStatusFragment` at `ui.go:313-320`, which executes the `status-panel` template. That template
starts with the whole `.status-grid` at `status_panel.html:2` and ends with the Technical details
`details` element at `status_panel.html:241`. The fragment therefore replaces every child of
`#status-panel`, not just the changed values.

The `details` element has no `open` attribute in the template. `open` is a DOM property that the
browser owns, and the server does not receive it on a poll. A fresh fragment always renders it
closed. The same pattern exists on the apply page: `operation.html:6` replaces all children of
`#operation-panel`, and `operation_panel.html:36` renders its Technical details disclosure closed.

The cause is a whole-region replacement that drops client state. A missing preserve attribute is a
contributing fact, not the root cause: adding `hx-preserve` would require stable ids and would only
cover the nodes it marks, while the current swap replaces every node in the region.

This also contradicts [pattern S6](../design/patterns.md#status-page), which says the refresh does
not move focus or steal scroll, and
[flow F3](../design/flows.md#f3-an-interrupted-flow-can-be-resumed-or-restarted-without-losing-work),
which says owner input survives a reload, an expired session, and a daemon restart.

## Root cause B: the session does not survive the daemon restart

The session store is `sessions map[string]*Session` at `auth.go:93`. `Open` creates an empty map at
`auth.go:129`, and `createSessionLocked` inserts a new session only in memory at `auth.go:323-340`.
`Session` looks up the cookie value in that map at `auth.go:344-356`. There is no signing key at all:
the cookie value is an opaque random session id set by `SetCookie` at `auth.go:392-402`. The id is
generated with `crypto/rand` and is not signed or encrypted.

The only persistent auth state is `/var/lib/bittern/auth.json` (`main.go:40`). `loadState` reads
only the password record at `auth.go:158-182`, and `saveState` writes only the password record at
`auth.go:241,453-460`. Nothing writes session ids, CSRF tokens, expiry, or a signing key under
`/var/lib/bittern`. The setup token convenience file is removed after setup and is not a session.

The update stops `bitternd.service` before activation (`engine.go:340`), then activates and migrates
(`engine.go:341-342`), optionally ensures the Frigate image (`engine.go:345`), and restarts the agent
at `engine.go:347` through `RestartUnit` at `engine.go:751-752`. The health gate at `engine.go:352`
probes the restarted unit; it does not restart it again. The unit also has `Restart=on-failure` at
[bitternd.service](../../agent/packaging/bitternd.service):23-24, but the update uses the explicit
stop and restart path.

While the daemon is down, the browser poll gets a connection error and HTMX does not swap, so the
old page stays visible. When the daemon returns, its session map is new and empty. The old cookie
names no session. `authenticate` reads `h.auth.Session(r)`, gets nil, and takes the
`!authenticated` branch at `ui/auth.go:65-71`.

## Root cause C: the login page is injected into the fragment

The status fragment route is marked `Auth: true` at `routes.go:205-206`, so the auth middleware runs
before `showStatusFragment`. With no session and setup complete, the middleware at `ui/auth.go:65-71`
builds `next` from `r.URL.RequestURI()` for a GET and returns a 303 to
`/ui/login?next=<fragment URL>`. It does not inspect `HX-Request` and does not send `HX-Redirect`.

The browser's XHR follows the 303 transparently. The login route at `ui/auth.go:47-64` sees no
session, creates a guest session, sets the cookie, and renders `login.html` with a 200. HTMX sees a
successful 200 response and swaps the body of that full page into `#status-panel` using
`innerHTML`. That is the sign-in form in the middle of the status page. If setup were still required,
the same mechanism would inject the setup page instead.

A manual reload works because it is a top-level navigation. `/ui/status` returns a 303 to
`/ui/login?next=/ui/status`, and the browser renders the login page as a full document. The
injected form's `next` value is the fragment URL, so signing in through the mid-page form would
return to the raw fragment, not to the status page. BIT-046 covers the fragment contract and the
parent-page `next` value.

## State lost on each poll

| State | Lost? | Why |
| --- | --- | --- |
| Disclosure open or closed | Yes | Each fragment renders `details` without `open`; the DOM node is replaced. This affects the status Technical details and the operation Technical details. |
| Focus | Yes, if inside the polled region | The focused node is removed. HTMX can restore focus only by matching a stable `id`; the summary and most controls have none. Focus outside the region, such as the header, survives. |
| Window scroll position | Not explicitly reset, but the reading position moves | The browser keeps `scrollY`, but a closing disclosure or a changed update card changes the content height above and below the viewport. The anchor the owner was reading moves. |
| Table scroll position | Yes | `.table-wrap` regions at `status_panel.html:32,83,179` are replaced, so their `scrollLeft` and `scrollTop` return to zero. Their focus is also lost. |
| Text selection | Yes, if inside the region | The selected DOM nodes are replaced. |
| Unsaved form input | None today in these regions | The update card has only hidden CSRF fields and buttons; the operation panel has no form. A text field added inside either region would lose its value on the next poll. |
| Announced live region | Repeated | `#operation-panel` has `aria-live="polite"`, so replacing the whole panel can re-announce unchanged content every 2 seconds. |
| Progress element and transient UI | Re-rendered | The progress element and any CSS state restart from the server values. |

## Update sequence seen by the browser

1. The owner starts the install from the status card. The POST redirects to status, and the page
   polls every 2 seconds while `h.updates.Progress().Active` or `Sections.Updates.CurrentOperation`
   is set (`ui.go:356-360`).
2. The helper stops `bitternd.service` at `engine.go:340`. Polls fail with a network error. HTMX
   leaves the last content in place and emits an error event; it does not swap a failed request.
3. Activation, migration, and the optional image ensure run while the UI is down. The image step can
   take up to 60 minutes (`engine.go:345`), so the outage can be long.
4. `bitternd.service` restarts at `engine.go:347`. The new process has an empty session map.
5. The next poll reaches the unauthenticated branch, follows the 303 to the login page, receives a
   200 login document, and swaps it into `#status-panel`.
6. The owner reloads. The top-level `/ui/status` request redirects to `/ui/login` and renders the
   login page correctly. Signing in after the reload works.

## Tests and hooks

| Check | What it covers | Why it did not catch this |
| --- | --- | --- |
| [ui_test.go](../../agent/internal/ui/ui_test.go):1202-1216 `TestStatusFragmentIsASinglePanel` | The fragment is not a full page and carries the status grid. | It never loads the page, waits for a poll, or checks disclosure, focus, or scroll state. |
| [update_test.go](../../agent/internal/ui/update_test.go):280-293 `TestUpdatePreparingStatePollsTheStatusFragment` | The status page has `hx-trigger="every 2s"` during an update. | It asserts the attribute only. It never waits for a poll. |
| [routes_test.go](../../agent/internal/ui/routes_test.go):233 | The status fragment route returns 200 with mock data. | The test server has no auth store, so the fragment never crosses the auth boundary. |
| [auth_test.go](../../agent/internal/ui/auth_test.go):167-205 `TestSessionCookieFlagsExpiryAndLogout` | Cookie flags, expiry, and logout. | It never reopens the store, so it cannot catch session loss on restart. |
| [auth_test.go](../../agent/internal/auth/auth_test.go):138-143 | The password survives a store reopen. | It only checks that setup is not required again. It does not check a session. |
| [tests/browser/routes.json](../../tests/browser/routes.json):95-177 | Status and update-card screens at four widths. | Every entry is a single page load. No entry waits for a fragment poll. |
| [check.mjs](../../tests/browser/check.mjs):109-148 `captureExpandedDisclosures` | Opens closed disclosures for the expanded screenshot. | It runs after the screenshot and assertions, for review only, and never waits for a poll. |
| [assertions.mjs](../../tests/browser/assertions.mjs):130-307 | Overflow, focusability, and scroll into view. | It measures one loaded page. It did not compare focus or scroll before and after a swap. |
| [auth_test.go](../../agent/internal/ui/auth_test.go) `TestFragmentAuthFailureUsesHXRedirectAndNeverReturnsLoginHTML` | A missing or expired session on a fragment gets 401 and `HX-Redirect`, with no login body; a normal navigation still gets 303. | New with BIT-046. |
| [ui_test.go](../../agent/internal/ui/ui_test.go) `TestStatusPagePollsOnlyTheUpdateCard` | The status panel does not poll; the update card does; the fragment has no disclosure or table. | New with BIT-044. |
| [ui_test.go](../../agent/internal/ui/ui_test.go) `TestOperationFragmentPollsOnlyTheLiveRegion` | The operation panel does not poll; the live region does; Technical details is outside it. | New with BIT-044. |
| [tests/browser/routes.json](../../tests/browser/routes.json) `live-refresh` | Two real polls keep the open disclosure, focused summary, table scroll, and table wrapper; an aborted fragment shows the reconnecting notice without clobbering the card. | New with BIT-044 and BIT-046. |

The pre-fix suite had no test for poll state preservation. The new tests assert the behaviour, not
the markup, and the browser check fails on the old whole-panel swap.

## Destination

- BIT-044 is implemented: the status page polls only the update card, the operation page polls only
  its live region, and the browser check proves the disclosure, focus, table scroll, and table
  wrapper survive two real polls.
- BIT-045 remains blocked on OQ-37. The session store is still lost on a daemon restart.
- BIT-046 is implemented: HTMX auth failures get a non-2xx response and `HX-Redirect`, and the
  fragment never returns login HTML.
- UX-14 records the principle candidate: live updates must not disturb state the user owns.

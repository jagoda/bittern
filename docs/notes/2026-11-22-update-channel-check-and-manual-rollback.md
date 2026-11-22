# Update channel check and manual rollback

- Date: 2026-11-22
- Status: design delta for BIT-009 slice four, written before the implementation. It records the
  on-demand GitHub Releases check, the manual rollback semantics, the agent contract the UI will
  call, and the status fields. It decides no open question and does not accept ADR 0029.
- Related: [BIT-009](../plan/implementation-plan.md#bit-009-app-bundle-update-transaction-with-rollback),
  [BIT-042](../plan/implementation-plan.md#bit-042-release-channel-and-ci-pipeline),
  [ADR 0008](../adr/0008-updates-rollback-and-reset.md),
  [ADR 0029](../adr/0029-release-signing-and-key-custody.md),
  [ADR 0031](../adr/0031-update-channel-on-github-releases.md),
  [ADR 0032](../adr/0032-continuous-integration-and-build-pipeline.md),
  [update transaction specification](2026-11-22-update-transaction-spec.md),
  [update engine slice two](2026-11-22-update-engine-slice-two.md),
  [status contract](../status.md), [open questions](../open-questions.md),
  [defaults/release.yaml](../../defaults/release.yaml).

This note is the design delta for the next BIT-009 slice. It adds a channel check that only reports a
fact, a manual rollback that moves the running bundle without lowering the committed generation, the
agent commands the UI will call, and the status fields those commands and the UI read. The
transaction order, verification order, automatic rollback order, and `/var/lib/bittern` write
boundary are unchanged.

## 1. Channel check

The check answers one question: is there a verified application bundle in this device's channel with
a generation higher than the committed generation? It never installs, never writes a request, never
takes the transaction lock, and never changes the pointer or committed state.

### Endpoint and candidate selection

| Item | Decision |
| --- | --- |
| Endpoint | `GET https://api.github.com/repos/jagoda/bittern/releases?per_page=100` |
| Pagination | Follow `Link: rel="next"` up to three pages. A response that still has a next page after the cap is `channel.pagination`, not "up to date". |
| Namespace | Releases whose tag starts with `app-beta-` or `app-stable-` for the device channel from `appliance.yaml`. The other channel and every other tag are ignored. |
| Generation | The suffix is exactly six lowercase hexadecimal characters per ADR 0031. Tag and manifest generation must be equal. A tag in the device channel namespace that does not parse is `channel.tag`, not a silent skip. |
| Published | `draft: true` is ignored. A release with no `published_at` is ignored. A published release marked `prerelease` is still a published release and is considered. |
| Candidate | The highest parsed generation strictly greater than `state.json`'s committed generation. A draft or an out-of-order publish cannot move the device backwards. |
| Assets | The exact names `manifest.json` and `manifest.json.sig` from the selected release's `assets`, addressed by `browser_download_url`. The kit, `SHA256SUMS`, source archives, and every other asset are ignored by name. |
| Payload | The check does not fetch `bittern-app.tar.gz`. Install fetches it and verifies `payload.size` and `payload.sha256` before the transaction stages it. |

If the highest candidate fails any check below, the check fails. It does not fall back to a lower
release and it does not report "up to date". A channel with no release above the committed
generation is up to date.

### Fetch and verification order

The tag is a selector and is never a trust input. The check performs these steps in order:

1. Read `appliance.yaml` for the channel, `state.json` for the committed generation, the running
   schema version, and the platform generation through the same root-owned metadata provider the
   transaction uses.
2. Fetch the release list over TLS with bounded time and body size.
3. Select the candidate and fetch `manifest.json` and `manifest.json.sig` with the manifest bounded
   at 1 MiB and the signature at 4 KiB.
4. Parse the canonical manifest and verify the detached Ed25519 signature through
   `internal/release` with the pinned image and rotation trust stores.
5. Require `kind` `app-bundle`, `product` `bittern`, the device channel, and a tag generation equal
   to the signed manifest generation.
6. Require the manifest generation to be greater than the committed generation and the manifest
   compatibility fields to cover the running architecture, platform generation, schema, and updater
   version.
7. Only after every step passes, write a check record with `result: update-available` and the
   verified version, generation, tag, manifest digest, key id, and source commit.

A check record is written for every attempt, including failures, so the status contract can report
the last check time and result. A failure record never carries an available version or generation.
The reason is redacted and never includes a secret or a raw response body.

### Caching and request budget

The check record lives at `/var/lib/bittern/update/check.json`, mode 0644, root-owned, schema 1. It
contains no secret and no raw GitHub response. The record is a status fact, not a trust input; every
install repeats the manifest and signature verification.

| Rule | Value |
| --- | --- |
| Minimum interval | 15 minutes between network checks. A check inside the interval returns the cached record with `cached: true` and makes no request. |
| Force | `bittern update check --force` bypasses the interval. It is a root CLI action, not an automatic path. |
| Conditional requests | None. ADR 0031 records that an unauthenticated `304 Not Modified` still consumes the hourly budget, so local caching is the control. |
| API calls per network check | One request per release-list page, normally one. Release asset downloads are GitHub download traffic and do not consume the REST API hourly budget. |
| Budget | 15 minutes allows at most four checks per device per hour. A NAT with 15 devices that check every 15 minutes consumes the 60 request hourly budget. |
| Automatic checks | Not implemented. The `check_interval` default is not wired to a timer. Periodic checking remains open under [OQ-12](../open-questions.md). |

The status document and the UI never trigger a check. Only an explicit command or an explicit future
UI action calls the channel client.

### Failure and offline behavior

| Result | Meaning | Status and next action |
| --- | --- | --- |
| `update-available` | A verified candidate is above the committed generation | Show the version and generation, with an explicit install action |
| `up-to-date` | The channel answered and no published release is above the committed generation | Show the last check time; install is disabled |
| `rate-limited` | HTTP 403 or 429 with an exhausted limit or `Retry-After` | Show retry later and the reset time when GitHub supplied one. Never show "up to date" |
| `offline` | DNS, connection, TLS, or timeout failure | Show that the appliance could not reach the channel and that the last known state is unchanged. Never show "up to date" |
| `failed` | 404, a non-200 response, malformed JSON, a missing asset, a tag or manifest mismatch, a bad signature, a channel or compatibility rejection | Show the failing check and the next action. Never show "up to date" |

A failed check leaves the committed state, pointer, request, journal, units, and history unchanged.
It only replaces the check record. A check never installs anything.

## 2. Manual rollback

Manual rollback is an explicit owner action after a committed update. It is not the automatic
rollback of a failed transaction. Automatic rollback still restores the bundle and data captured by
the failed transaction.

### Preconditions and target

| Precondition | Failure |
| --- | --- |
| No active journal and the shared operation lock is available | `rollback.busy`; the active operation is untouched and no history record is written |
| `state.json` exists and names a committed generation `G` greater than 0 | `rollback.no-committed` |
| The activation pointer currently names `G` | `rollback.not-running`; a second rollback is refused, not replayed |
| A retained bundle `T` exists with `0 < T < G` | `rollback.no-target` |
| The retained bundle `T` and the committed bundle `G` both verify against their signed manifests and file digests | `rollback.previous` or `rollback.committed`; no live change |

The target is the highest retained generation below `G`. The prune rule keeps the committed bundle
and the immediately previous committed bundle, so normally exactly one target exists. Generations
may be skipped: a device that moved from 3 to 10 rolls back to 3, not to 9.

### Semantics

| Fact | After a successful manual rollback |
| --- | --- |
| Committed generation | Remains `G` in `state.json`. The anti-rollback anchor does not move backwards. |
| Committed version | Remains the version of `G`. |
| Running generation | Moves to `T` through the activation pointer. |
| Running version | The version in `T`'s signed manifest. |
| Units | `bitternd.service` restarts from the fixed launcher, then `bittern-frigate.service` starts when Frigate is configured. |
| Health | The same bounded bundle-integrity, agent-unit, and Frigate-API gate runs against `T`. |
| Configuration and data | Not restored. The current `appliance.yaml`, rendered configuration, and Frigate database stay in place. ADR 0008 requires backward-compatible migrations because an older bundle may run on newer data. |
| Status | Reports running generation `T` and committed generation `G`, and the rollback target fact becomes false until a newer release is retained. |
| History | Writes one `app-bundle-rollback` record with the reason, the running generation before the rollback (`G`), the restored generation (`T`), and the result. |
| Next update | Must have a generation greater than `G`. The existing release verifier already enforces this. |

The operation is journalled with phase `manual-rollback` and is resumable after a power cut. The
pointer only moves after both bundles verify, and the journal records the next step before every
action. The helper unit and request record are reused; the helper still takes no arguments.

### Failure cases

| Case | Behavior |
| --- | --- |
| No retained previous bundle | Reject before any live change. The pointer, committed state, units, configuration, and data are unchanged. A `rejected` history record carries the reason. |
| The previous bundle does not verify | Reject before any live change. Running stays on `G`. A `failed` history record names `rollback.previous`. |
| The previous bundle verifies but fails to start or fails the health gate | The engine writes the pointer back to `G`, restarts the `G` units, and runs the health gate against `G`. It records a `failed` rollback with `restored_generation: G`. Running stays or returns to `G`. |
| Restoring `G` also fails | The journal is kept, the result is `state-unknown`, and status names the failing step and the recovery action. The engine never claims `T` is running. |
| A rollback is requested during an update | The shared lock or the active journal refuses it with `rollback.busy`. The update is untouched and no rollback history is written. |

## 3. Agent contract

The UI is a later change. This slice fixes the commands and status fields it will call; it does not
add a browser route or a template.

| Action | Command | Returns | Authentication and CSRF |
| --- | --- | --- | --- |
| Check | `bittern update check [--force]` | Text summary and the check record. Exit 0 for `update-available` or `up-to-date`; non-zero for `rate-limited`, `offline`, or `failed` | Root CLI. A future UI `POST /ui/updates/check` requires the administrator session and CSRF token. It never runs as an unauthenticated loopback request |
| Install the available release | `bittern update install [--no-follow]` | Queues a local-file request for the verified downloaded candidate and starts `bittern-update.service`; with follow, prints the journal progress and history result and exits non-zero unless committed | Root CLI. A future UI `POST /ui/updates/install` requires the administrator session and CSRF token |
| Install a local bundle | `bittern update install SOURCE [--no-follow]` | The existing local-file handoff, unchanged | Root CLI. Not exposed by the UI |
| Roll back | `bittern update rollback --reason TEXT [--no-follow]` | Queues a `manual-rollback` request and starts the helper; with follow, prints progress and history and exits non-zero unless rolled back | Root CLI. A future UI `POST /ui/updates/rollback` requires the administrator session, a CSRF token, and the same typed reason |
| Read state | `GET /api/status`, `bittern update status` | The status document `updates` section and the CLI summary | `/api/status` stays loopback and read-only. The UI reads it through the authenticated `/ui/status` page |

The future browser routes are not implemented here. When they are added they call the same agent
functions, not a shell and not a command assembled from form values. The only long-running work is
the fixed helper unit. The request record is the only input, and it still has no path, flag,
environment variable, or argument supplied by the caller.

Progress is the transaction journal. Install and rollback write the fixed request, start the unit,
and follow `journal.json` until the unit exits. `bittern update status` and the status document report
the last operation from the newest history record, so a browser reload reconnects to the same
operation. The check is synchronous and short; it reports its result when it returns.

## 4. Status contract fields

The `updates` section keeps `state`, `current_slot`, `current_generation`, and `base`, and adds the
fields below. `current_generation` becomes the generation named by the activation pointer, which is
the running generation; `committed_generation` is the anti-rollback anchor from `state.json`. They
are equal except after a manual rollback. All new fields are additive to status schema 1.

| Field | Type | Meaning |
| --- | --- | --- |
| `current_version` | string or null | Version of the bundle named by the activation pointer |
| `current_generation` | integer or null | Generation named by the activation pointer, kept from the existing contract |
| `running_generation` | integer or null | Explicit name for the same pointer value |
| `committed_generation` | integer or null | Committed anti-rollback generation from `state.json` |
| `committed_version` | string or null | Version committed in `state.json` |
| `channel` | string or null | Device channel from `appliance.yaml`, or the last committed channel when the config cannot be read |
| `last_check.checked_at` | string or null | UTC RFC 3339 time of the last check attempt |
| `last_check.result` | string | `never`, `update-available`, `up-to-date`, `rate-limited`, `offline`, or `failed` |
| `last_check.reason` | string | Redacted explanation for a non-success result, empty on success |
| `last_check.cached` | boolean | True when the check was answered from the local record |
| `available_version` | string or null | Verified candidate version, only for `update-available` |
| `available_generation` | integer or null | Verified candidate generation, only for `update-available` |
| `rollback.available` | boolean | A retained previous bundle exists below the committed generation |
| `rollback.generation` | integer or null | Retained target generation |
| `rollback.version` | string or null | Retained target version |
| `last_operation.operation_id` | string or null | Newest history record |
| `last_operation.kind` | string or null | `app-bundle-update` or `app-bundle-rollback` |
| `last_operation.result` | string or null | `committed`, `rolled-back`, `rejected`, `failed`, or `state-unknown` |
| `last_operation.failed_step` | string | Failing step when the operation failed |
| `last_operation.reason` | string | Redacted failure or rollback reason |
| `last_operation.finished_at` | string or null | UTC RFC 3339 completion time |
| `last_operation.from_generation` | integer or null | Generation before the operation |
| `last_operation.to_generation` | integer or null | Candidate or restored generation |
| `last_operation.restored_generation` | integer or null | Generation restored by a rollback |

The status probe is read-only. It reads `state.json`, the activation pointer, the running bundle
manifest, the retained bundle directories, `check.json`, and the newest history record. It never
calls GitHub and never takes the transaction lock.

## Open questions and limits

- Automatic periodic checking, its default interval, and whether checks are on by default remain
  open under [OQ-12](../open-questions.md). This slice is on-demand only and does not wire
  `defaults/release.yaml`'s `check_interval` to a timer.
- One check reads at most three release-list pages. A channel that outgrows that needs a signed
  pointer or a different discovery mechanism, which ADR 0031 lists as a deferred trigger.
- The check does not fetch or verify the payload. Install fetches it and the transaction verifies
  its size, digest, file set, paths, and modes before promotion.
- Manual rollback does not restore configuration or data. A future change that needs that must
  specify its backup source and prove the older bundle can read the newer data.
- There is no browser route in this slice, so the UI field and route names in section 3 are the
  contract for the next UI change, not shipped routes.

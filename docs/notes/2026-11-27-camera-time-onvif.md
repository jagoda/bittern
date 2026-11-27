# Camera time: ONVIF detection, synchronisation, and status

- Date: 2026-11-27
- Status: landed, camera half of [ADR 0035](../adr/0035-appliance-as-time-authority.md). No schema
  change and no migration. Unit delivery and the manifest v2 field set remain out of scope.
- Related: [ADR 0035](../adr/0035-appliance-as-time-authority.md),
  [ADR 0016](../adr/0016-bittern-owns-frigate-config.md),
  [appliance time note](2026-11-27-appliance-time.md),
  [time sync recon](2026-11-26-time-sync-recon.md), [status contract](../status.md),
  [camera matrix](camera-matrix.md), [OQ-41](../open-questions.md), [OQ-42](../open-questions.md),
  [OQ-43](../open-questions.md)

## What changed

`agent/internal/onvif` is a small SOAP over HTTP Device service client with no new Go dependency. It
reads a camera clock without authentication, reads Device service capabilities with WS-Security
UsernameToken digest authentication, sets the NTP list and mode, and pushes a manual UTC time with a
fixed-offset POSIX zone. Every operation has a five second bound, the whole camera sync has a fifteen
second bound, redirects are refused, and every connection re-validates the endpoint against the
camera's own IPv4 address.

`agent/internal/cameratime` owns per-camera detection, the 15 minute schedule, the retry bound, and
the cache. `bitternd` runs it in the daemon context and wakes it after adoption or a camera edit.
Adoption only marks the cameras due; it never waits for a camera that refuses or is offline.

The status contract gains a `time` object on each camera item. The camera list shows the state and
the plain-language next step, and keeps the mechanism in technical details. The appliance time
section is unchanged.

## Endpoint rule

The camera schema has only `address`, `rtsp_port`, and `rtsp_path`. It has no ONVIF endpoint field.
The client therefore derives the Device service from the stored camera address:

| Value | Rule |
| --- | --- |
| Host | The camera's `address` IPv4 literal, never a hostname and never `cam0` |
| Scheme and port | `http` on the scheme default (80), then `https` on the scheme default (443) |
| Path | `/onvif/device_service` |
| RTSP port and path | Ignored. They describe the stream, not the ONVIF service |
| User information, fragments, redirects | Refused before connecting |

A camera whose ONVIF service answers on a different address or a non-standard port cannot be reached
through the stored schema. That is a detected `unreachable` state, not a crash and not a failed
adoption. Adding an ONVIF endpoint field would be a schema change with a migration and is not done
in this slice.

A self-signed camera certificate is not trusted. The client uses normal TLS verification and reports
the certificate trust failure as `unreachable`; verification is not disabled globally or per camera.
A camera with a self-signed certificate needs normal HTTPS interception or a future explicit trust
path before it can use HTTPS ONVIF.

## Credentials and authentication

The ONVIF account is the camera's existing `credentials_ref`. The client first uses the username and
password inside the RTSP URL, because most cameras use one account for stream and ONVIF. A separate
ONVIF account can be stored under the same reference as `onvif_username` and `onvif_password`;
`LoadCredentials` reads that mapping before falling back to the RTSP values. The camera form has an
optional "Camera time sign-in" section for that separate account; it is written under the same
reference and never creates a second secret store. No credential, complete URL, or endpoint with
user information is written to status, logs, or a test. The first authenticated
request uses the timestamp from the unauthenticated clock read, so a camera that is hours off can
still be fixed. When that read is unavailable, the request uses the appliance clock and a rejection
is reported as `refused`, as OQ-42 requires.

## Capability detection and order

Detection is per camera and is never inferred from a model, vendor, or streaming profile:

1. Unauthenticated `GetSystemDateAndTime`.
2. Authenticated `GetServiceCapabilities`, reading the `Network.NTP` maximum server count.
3. If NTP is advertised, `SetNTP(FromDHCP=false, NTPManual=[cam0 address])`, then
   `SetSystemDateAndTime(NTP, DaylightSavings=<current>, TimeZone=<current if returned>)`.
4. A pushed `SetSystemDateAndTime(Manual, DaylightSavings=false, TimeZone=<POSIX offset in force>,
   UTCDateTime=<now UTC>)` is also tried once during detection, so the cached result distinguishes a
   camera that accepts NTP only, a pushed time only, both, or neither.
5. If the push changed the mode and NTP had worked, NTP is restored before the result is recorded.
6. The result is verified by reading the camera again. A camera just switched to NTP mode can report
   `never-synchronised` until it polls; that is not a failure.

The detected mechanism and capability booleans are cached in `camera-health.json`, not in
`appliance.yaml`. They are re-detected after an adoption or edit, after a camera address changes, and
when a cached mechanism fails. A periodic check is read-only when the camera already agrees within
two seconds and the pushed zone has not changed.

## Schedule and retry bounds

| Bound | Value | Source |
| --- | --- | --- |
| Check interval | 15 minutes | ADR 0035 section 5; corrects a DST offset within one interval |
| Per ONVIF operation | 5 seconds | ADR 0035 section 5 |
| Per camera sync | 15 seconds | ADR 0035 section 5 |
| Parallel cameras | 4 | ADR 0035 section 5 |
| Passes at once | 1 | ADR 0035 section 5 |
| Operations per client | 16 | Implementation bound; the full detection path is shorter |
| Consecutive failures before hourly | 3 | ADR 0035 section 5 |
| Long-failure interval | 6 hours after 24 hours of continuous failure | ADR 0035 section 5 |

A failed camera is not retried inside a pass. A success resets the schedule. A camera that reports
`never-synchronised` waits for the next 15 minute cycle instead of counting a failure. The runner
loads the schedule from the health cache, so a daemon restart does not create a burst.

## Status vocabulary

| State | Meaning | Owner action |
| --- | --- | --- |
| `ok` | The camera clock agrees within two seconds, or was just set and verified | None |
| `never-synchronised` | No successful set or confirmation yet | None yet; the next check will try |
| `drifted` | The camera differs by more than two seconds | None if the next check corrects it; otherwise check the camera |
| `refused` | The camera rejected the sign-in or the time operation | Set the time on the camera, or check its sign-in |
| `unreachable` | The camera did not answer the time request | Check power and connection |

The two second threshold comes from ADR 0035 section 5. It is used both for writing and for
reporting `drifted`, so the state and the correction rule cannot disagree. The mechanism names NTP
and ONVIF appear only in technical details, never in the primary copy.

## Tests

| Test | Assertion |
| --- | --- |
| `TestDeriveEndpointsUsesTheStoredCameraAddress` | Only the camera address and `/onvif/device_service` matter |
| `TestValidateEndpointRefusesUnsafeTargets` | Other hosts, hostnames, user information, fragments, and non-HTTP schemes are refused |
| `TestClockReadsWithoutAuthenticationAndParsesUTC` | The PRE_AUTH read sends no security header and parses UTC, mode, zone, and DST |
| `TestSecurityHeaderUsesDigestAndCameraTimestamp` | The digest is SHA-1 over nonce, created, and password; `Created` uses the camera clock |
| `TestAuthenticationFailureIsClassified` | HTTP 401 is an authentication failure |
| `TestSOAPFaultIsRefusedWithoutLeakingTheSignIn` | A fault is refused and the error carries no credential |
| `TestTimeoutIsUnreachable` | An operation bound produces `unreachable` |
| `TestRedirectIsRefused` | A redirect never opens a second target |
| `TestMalformedResponseIsUnreachable` | Invalid XML is a detected failure |
| `TestSelfSignedCertificateRemainsUnverified` | The TLS limitation is explicit and verification is not weakened |
| `TestSyncDetectsNTPOnlyCamera` | NTP is configured, push is rejected, mechanism is NTP |
| `TestSyncDetectsPushOnlyCamera` | A camera with no NTP capability accepts a pushed time |
| `TestSyncDetectsCameraThatAcceptsBoth` | Both booleans are recorded and NTP remains preferred |
| `TestSyncReportsCameraThatRefusesTimeOperations` | Neither mechanism is recorded and the state is `refused` |
| `TestSyncReportsAuthenticationFailure` | A rejected account is `refused` with no credential in the detail |
| `TestSyncReportsTimeout` | A slow camera is `unreachable` |
| `TestSyncReportsOfflineCamera` | A closed port is `unreachable` |
| `TestSyncVerifiesPushWhenTheCameraReportsAnErrorAfterAccepting` | The read-back proves the set and records the error note |
| `TestPushAtDaylightSavingBoundarySendsTheOffsetInForce` | Europe/Berlin sends `CET-1` before and `CEST-2` after the spring change |
| `TestCachedCameraDoesNotWriteWhenItIsAlreadyCorrect` | One read and no write on an already correct camera |
| `TestBackoffSchedule` | 15 minutes, then hourly after three failures, then six-hourly after 24 hours |
| `TestRunnerDoesNotRetryInsideAPassAndUsesTheBackoff` | No second pass inside the interval, bounded requests, persisted backoff |
| `TestRunnerCompletesWithEveryCameraTimeClass` | NTP, push, refused, timeout, and offline cameras all finish one pass and cache a state |
| `TestLoadCredentialsUsesTheRTSPSignInFirst` | The existing sign-in is reused |
| `TestLoadCredentialsPrefersTheOptionalONVIFAccount` | A separate ONVIF account under the same reference wins |
| `TestAgentApplierWritesOptionalONVIFAccountUnderTheSameReference` | The camera form account is written under the RTSP reference and reloads as the ONVIF sign-in |
| `TestCameraTimeSectionForEveryState` | Every camera state maps to the status contract |
| `TestUpdateCameraHealthPreservesTheSnapshotResult` | Time and snapshot results do not overwrite each other |
| `TestCameraTimeCopyForEveryState` | The owner copy and next step for every state, with no mechanism names |
| `TestWizardRequestsCameraTimeAfterApplyWithoutFailingAdoption` | Adoption marks the camera due and still commits |

## Unverified without a real camera

- OQ-41: no product camera is in the [camera matrix](camera-matrix.md). The implemented client is
  tested against a fake endpoint; whether a given model accepts NTP, a pushed time, both, or neither
  is still unmeasured.
- OQ-42: the accepted skew window for authenticated requests is not measured. The implementation uses
  the camera's own unauthenticated clock for the first request, with the appliance clock as fallback.
- OQ-43: whether cameras accept a fixed-offset POSIX string and ignore the DST flag is not measured.
  The push sends the offset in force and re-pushes when it changes.
- Whether a camera in NTP mode polls the appliance within one 15 minute interval, and what it does
  when the appliance is unreachable, is vendor behaviour and remains unmeasured.
- DHCP option 42 acceptance is not measured per model.
- A camera whose ONVIF service uses a non-standard port, a different address from its RTSP endpoint,
  or HTTP Basic/Digest instead of WS-Security cannot be reached by this slice and is reported as a
  capability failure.
- A self-signed HTTPS camera certificate is not trusted; the client records `unreachable`.
- The QEMU harness has a virtual RTSP camera but no ONVIF time endpoint, so no QEMU phase exercises
  this fake. The fake endpoint is confined to the Go tests.

## Verification

`make check`, `go vet ./...`, and `gofmt -l` pass for this change. The full test set includes the
status goldens and the browser-facing camera list. A hardware-in-the-loop run against the Axis
reference device in BIT-027 remains the next concrete step for OQ-41 to OQ-43.

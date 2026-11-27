# ONVIF compatibility strategy

- Date: 2026-11-27
- Status: proposed, research only. No product code, schema or template changes in this change.
- Related: [ONVIF compatibility assessment](../research/onvif-compatibility.md),
  [ONVIF compatibility matrix](../engineering/onvif-compatibility.md),
  [ADR 0035](../adr/0035-appliance-as-time-authority.md),
  [ADR 0024](../adr/0024-onvif-discovery-levels.md),
  [BIT-027](../plan/implementation-plan.md#bit-027-robust-onvif-profiles-ptz-and-field-interoperability),
  [camera schema](../../schema/README.md#cameras), [field parity](../engineering/field-parity.md),
  [OQ-47](../open-questions.md), [OQ-48](../open-questions.md), [OQ-49](../open-questions.md),
  [OQ-50](../open-questions.md)

## Summary

The compatibility strategy is a staged probe that never blocks adoption and never runs at render
time. It reads the device service, discovers services from the device rather than from a vendor
list, chooses Media2 when it is advertised and Media1 when it is not, enumerates profiles with their
encoder attributes, classifies them into a high and a low stream by a deterministic rule, and stores
the resolved render inputs in `appliance.yaml`.

The strategy adds four things the current time client does not have: HTTP Digest authentication,
an optional ONVIF endpoint and port, Media1 and Media2 profile handling, and a per-camera capability
record. It keeps the current client's safety rules: endpoint validation on `cam0`, no redirects, no
credentials in discovery, bounded operations, and a camera that refuses ONVIF still adopted.

This note is a proposal for BIT-027. The verified facts, with the specification sections and source
URLs, are in [the assessment](../research/onvif-compatibility.md). Every `S` reference below resolves
to that document's source table.

## Findings and destinations

| Finding | Destination |
| --- | --- |
| Media2 is not in the `GetCapabilities` media element; a client must read `GetServices` to find it | [BIT-027](../plan/implementation-plan.md#bit-027-robust-onvif-profiles-ptz-and-field-interoperability) |
| `GetProfiles`, `GetStreamUri` and `GetSnapshotUri` are `READ_MEDIA`, so profile enumeration cannot assume anonymous access | [BIT-027](../plan/implementation-plan.md#bit-027-robust-onvif-profiles-ptz-and-field-interoperability) |
| The current client has no HTTP Digest, no media service and no ONVIF port or endpoint field | [BIT-027](../plan/implementation-plan.md#bit-027-robust-onvif-profiles-ptz-and-field-interoperability) |
| The device service path is fixed, but ports and service `XAddr` values vary, and a non-standard port needs a schema field | [BIT-027](../plan/implementation-plan.md#bit-027-robust-onvif-profiles-ptz-and-field-interoperability) |
| Media1 and Media2 precedence and token equivalence are undefined | [OQ-47](../open-questions.md) |
| The HTTP `Date` header as a clock bootstrap is documented HTTP behavior but not measured on a camera | [OQ-48](../open-questions.md) |
| The per-camera authentication method and the effect of an auth-mode setting are unmeasured | [OQ-49](../open-questions.md) |
| The high and low profile ranking is a provisional product policy | [OQ-50](../open-questions.md) |
| The bench camera's advertised profiles and sub stream are unmeasured | [OQ-45](../open-questions.md) |
| Vendor quirks need a register keyed by model and firmware with evidence and a test | [the compatibility matrix](../engineering/onvif-compatibility.md#quirk-register) |
| PTZ is a set of conditional capabilities, not a boolean | [BIT-027](../plan/implementation-plan.md#bit-027-robust-onvif-profiles-ptz-and-field-interoperability) |

## What is verified before the strategy starts

| Fact | Evidence | Effect on the strategy |
| --- | --- | --- |
| Media1 and Media2 are separate services with separate namespaces, profile types and stream request shapes | [S2] 1, 5.1.2, 5.4.1; [S3] 4.1, 5.2.2, 5.15.1 | A client must detect and choose one; it cannot assume one request shape works for both |
| `GetCapabilities` returns a Media `XAddr` but not a Media2 address; `GetServices` returns both namespaces | [S1] 8.1.2.2, 8.1.2.4, Annex A | Read `GetServices`; fall back to `GetCapabilities` for Media1 only |
| A Media2 profile has no configuration information unless `GetProfiles` is given a `Type` | [S2] 5.1.2 | Always request `Type=All`, then fetch per-profile configurations if the response is incomplete |
| `GetProfiles`, `GetStreamUri` and `GetSnapshotUri` are `READ_MEDIA`, which the default policy does not grant to Anonymous | [S1] Table 7; [S3] 5.2.2, 5.15.1, 5.16.1; [S2] 5.1.2, 5.4.1, 5.5.1 | Capability discovery may be unauthenticated; profile enumeration may not be |
| The device service path is fixed to `/onvif/device_service`; service addresses and ports vary | [S1] 5.1.1; [S13] 5.1.4; [S17]; [S19]; [S26] | Fix the path, make the port an explicit field, read other service addresses from capabilities |
| HTTP Digest is the Core baseline; WS-UsernameToken is the legacy exception; Profile T mandates HTTP Digest and Profile S mandates WS-UsernameToken | [S1] 5.9.1; [S11] 7.1.3; [S12] 7.1.3 | Prefer HTTP Digest when advertised, and keep WS-UsernameToken for legacy cameras |
| WS-UsernameToken requires both nonce and timestamp; a stale timestamp can be rejected | [S1] 5.9.5; [S14] 3.1 | Timestamp from the camera clock and treat a timestamp rejection as a distinct failure |
| `GetSystemDateAndTime` and `SetSystemDateAndTime` are required; NTP operations are conditional on the `Network.NTP` capability | [S1] 8.2.6, 8.2.7, 8.3.6, 8.3.7; [S12] 7.5.3, 8.8.3 | Read time before authenticating; use NTP where advertised, push where not |
| PTZ ContinuousMove and Stop are mandatory for a PTZ-capable device; presets, zoom, home, absolute and relative moves are conditional | [S4] 5.1.1, 5.3.1, 5.3.2, 5.3.3, 5.3.5, 5.4, 5.7 | Detect the node, spaces, preset count and status capabilities before showing a control |

## Detection order

The pipeline below is the order, not a menu. A stage runs only if the previous stage produced enough
to continue. Every stage has a bound and records its result. The current time sync bounds stay as
ADR 0035 defines them; the full profile probe gets its own bound, proposed at 30 seconds per camera,
because a camera with many profiles can exceed the 15 second time-sync budget.

### Step 0: acquire an address

- Manual address is always available and is the baseline. The owner enters the camera IPv4 address
  on `cam0`.
- WS-Discovery is an optional shortcut. It is unauthenticated. The `XAddrs` value is a hint, not an
  authority: validate it against the camera's stored IPv4 address, the `cam0` subnet, `http` or
  `https`, no user information, no fragment and no redirect. A discovered host that differs from
  the stored address is refused or held for owner confirmation.
- The ONVIF port is separate from the RTSP port. The camera form needs an optional ONVIF port, or
  the discovered `XAddr` must supply it. Reolink documents 8000 as its ONVIF default; Hikvision's
  8000 is a device port and its ONVIF default is HTTP 80. [S17]; [S19]; [S26].
- Never scan ports. A scan is slow, noisy, and can trip a camera login lock. Try the documented
  defaults only: HTTP 80 at `/onvif/device_service`, then HTTPS 443 at the same path. A non-standard
  port comes from the field or from discovery.
- TLS verification is normal. A self-signed camera certificate is recorded as `tls-untrusted`; it is
  not disabled per camera and it is not a reason to weaken verification globally.

### Step 1: device service, clock and security capabilities

- Try `GetSystemDateAndTime` without authentication. It is `PRE_AUTH` and returns the camera clock
  needed for a WS-UsernameToken `Created`. [S1] 8.3.6.
- Try `GetServiceCapabilities` without authentication. It is `PRE_AUTH` and returns the Device
  service flags: `Network/NTP`, `Security/UsernameToken` and `Security/HttpDigest`. [S1] 8.1.2.3,
  Table 9. This tells the client which authentication schemes and whether NTP are advertised before
  it sends a credential.
- Try `GetCapabilities` and `GetServices` without authentication. Both are `PRE_AUTH`. [S1]
  8.1.2.2, 8.1.2.4. Record the service namespaces, `XAddr` values, versions and capabilities.
- If `GetServices` is absent or malformed, use `GetCapabilities/Media/XAddr` as the media endpoint
  and try Media1 operations. Record `services-incomplete`. A Media2-only camera that omits
  `GetServices` is non-conformant and may have to fall back to manual RTSP.
- If a camera rejects a `PRE_AUTH` operation anonymously, that is non-conformant. Retry once with
  the auth ladder below and record the result. Do not assume the rejection.

### Step 2: service inventory

- Record which of Device, Media1, Media2, PTZ, Events and Imaging answer.
- A service is present only if its namespace appears in `GetServices` or its `XAddr` appears in
  `GetCapabilities`. A model name, a vendor list or a streaming profile is not evidence.
- Read `GetDeviceInformation` after authentication. It is `READ_SYSTEM`, so it is not available
  before auth. [S1] 8.3.1, Table 7. Record manufacturer, model, firmware and hardware ID for quirk
  matching and display. If it fails, continue without quirk matching.
- PTZ and Imaging are optional. The event service is required by Core 5.1.2, but an absent or
  broken event source is still a capability gap for this product, not an adoption failure. [S1]
  5.1.2.

### Step 3: media service

- If the `http://www.onvif.org/ver20/media/wsdl` namespace is present, choose Media2.
- Else if `http://www.onvif.org/ver10/media/wsdl` is present, choose Media1.
- If both are present, choose Media2 as authoritative, record both, and do not mix their profile
  tokens. If Media2 later fails to produce a usable profile or stream URI, fall back to Media1 once
  and record `media2-failed`.
- If neither is present, record `no-media-service` and use the manual RTSP floor.
- Media2 is the forward path because Profile T uses it and ONVIF is ending Profile S support in
  favour of Profile T. [S12] 7.2.3, 7.9.3; [S21]. Media1 remains required for the installed base and is
  still a current service specification. [S3].

### Step 4: profiles and streams

- Media2: call `GetProfiles` with `Type=All`. If the response has profiles but no configurations,
  call `GetVideoEncoderConfigurations` and `GetVideoSourceConfigurations` with each profile token.
  [S2] 5.1.2, 5.3.2.
- Media1: call `GetProfiles`. The `tt:Profile` contains the `VideoEncoderConfiguration` and
  `VideoSourceConfiguration` inline when present. [S3] 5.2.2; [S9] `Profile`.
- For each profile with a video source and a video encoder, call `GetStreamUri`. Try the transports
  in this order:
  - Media1: `RTP-Unicast` with `RTSP` (RTP over RTSP/TCP), then `UDP`, then `HTTP`. [S3] 5.15.1.
  - Media2: `RTSP` (RTP over RTSP/TCP), then `RtspUnicast` (RTP over UDP), then `RtspOverHttp`.
    If the camera advertises `SecureRTSPStreaming`, try `RtspsUnicast` as well. [S2] 5.4.1, 5.14.
  - Accept an `rtsps://` URI when the camera returns one, even if the request used RTSP, and
    validate its scheme and host. Do not construct a stream path from the RTSP port and path if the
    camera returned a URI.
- Call `GetSnapshotUri` for the high profile, then the low profile, then the first profile that
  answers. Fetch one image with HTTP GET through the validated endpoint, using the camera credential
  when it challenges, and check the JPEG magic. The image is always JPEG regardless of the profile
  encoding. [S3] 5.16.1; [S2] 5.5.1.
- A profile without a video encoder, without resolution, or without a working stream URI is
  `ineligible`; record the reason. A virtual or multitrack profile is ineligible for this rule
  because it does not map to one source and encoder. [S2] 5.1.6.

### Step 5: classification

The exact rule is in the next section. It produces at most one high profile and one low profile per
camera.

### Step 6: persist and select

- Store the chosen service, endpoint, profile attributes, stream URIs and snapshot URI in
  `appliance.yaml` (schema version 3 proposal below).
- Store the full probe catalogue, auth method, clock offset, failure records and evidence in the
  derived camera catalogue or health record, not in `appliance.yaml`.
- Set `stream_source` to `onvif` when a high stream URI was verified, or `manual` when it was not.
  The low stream is optional; if absent, Frigate uses the high stream for both record and detect.
- The renderer reads the stored selection and never opens a socket. A missing render input is a
  validation error, not a prompt to probe.

### Step 7: PTZ, later

PTZ is research for BIT-027, not this change. The detection order is: service namespace or
`PTZ/XAddr`, then a `PTZConfiguration` in the selected profile, then `GetNodes/GetNode` for spaces,
preset count and home support, then `GetConfigurationOptions` for the timeout range, then
`GetServiceCapabilities` for `MoveStatus` and `StatusPosition`. Only the controls the node actually
advertises are shown. Continuous move is always paired with Stop and an explicit timeout.

## Classification rule

A profile is **eligible** when all of these are true:

1. It came from the selected media service.
2. It has a video source configuration and a video encoder configuration with a token.
3. Its encoding normalizes to `H264` or `H265`. `H.264`, `H.265` and `HEVC` normalize to those
   values. JPEG and MPEG4 profiles are recorded but not ranked.
4. Its width and height are positive.
5. `GetStreamUri` returned a URI that passed endpoint validation, and an RTSP `DESCRIBE` or
   `OPTIONS` on that URI succeeded.
6. It is not a virtual or multitrack profile.
7. Its encoder token has not already been seen. When two profiles share an encoder token, keep the
   first by profile token and record the duplicate. [S9] `ConfigurationEntity`; [S2] 5.2.1.

For each eligible profile, compute:

- `area = width * height`
- `bitrate = RateControl/BitrateLimit` when present and greater than zero, else
  `RateControl/AverageBitRate` when present and greater than zero, else `0` (unknown)
- `fps = RateControl/FrameRateLimit` when present and greater than zero, else `0` (unknown)
- `encoding` as `H264` or `H265`
- `token` as the profile token

Sort the eligible profiles in ascending order by:

1. `area`
2. `bitrate`
3. `fps`
4. `token`, compared as a Unicode string, only as a stable tie-break

Then:

- **No eligible profile:** no high and no low. Set `stream_source=manual` and record
  `no-eligible-profile`.
- **One eligible profile:** `high` is that profile; `low` is absent.
- **Two or more:** `high` is the last profile in the order; `low` is the first profile in the order
  that is not the high profile and whose stream URI differs from the high stream URI. If no such
  profile exists, `low` is absent.

The rule uses resolution first, then the reported bitrate limit, then the reported frame rate limit.
It never uses the profile name or the token semantics. The token is only the final tie-break that
makes the result deterministic. A missing bitrate or frame rate is treated as `0`, which ranks below
any reported positive value; it is recorded as `unknown`, not as a claim that the stream is low
quality.

| Example profiles | High | Low | Why |
| --- | --- | --- | --- |
| 2688x1520, 20 fps, 6144 kbps; 1280x720, 15 fps, 2048 kbps; 640x360, 10 fps, 256 kbps | 2688x1520 | 640x360 | Largest area wins high; smallest area wins low |
| 1920x1080, 25 fps, 4096 kbps; 1920x1080, 25 fps, 2048 kbps | 4096 kbps | 2048 kbps | Equal area, bitrate breaks the tie |
| 3840x2160 H265, 20 fps, 8192 kbps; 1920x1080 H264, 20 fps, 4096 kbps | 3840x2160 H265 | 1920x1080 H264 | Encoding is a gate and is recorded, not a ranking key |
| One 1920x1080 profile | 1920x1080 | none | There is no second distinct stream |
| JPEG only | none | none | No eligible H264 or H265 profile; manual RTSP |

The specification is silent on profile order and on token meaning. [S3] 5.2.2; [S2] 5.1.2; [S10]
`ReferenceToken`; [S9] `Profile`. The rule is a client policy, not a quoted mandate. It is
provisional until OQ-50 is answered.

## Authentication

### Fallback order

The client uses the advertised security capabilities to order the attempts, not a fixed vendor
assumption. The order is:

| Step | Method | When it is tried | What it proves |
| --- | --- | --- | --- |
| 0 | No authentication | Only for `PRE_AUTH` operations: `GetCapabilities`, `GetServices`, `GetServiceCapabilities`, `GetSystemDateAndTime` | The camera answers; no credential is exposed |
| 1 | HTTP Digest with the RTSP account | `Security/HttpDigest` is advertised, or the capabilities are absent. The first request may be unauthenticated to obtain the challenge, then retried with the digest header | The account works and the clock is irrelevant |
| 2 | WS-UsernameToken digest with the RTSP account, timestamped from the camera clock | `Security/UsernameToken` is advertised, or HTTP Digest was not available | The account works with the legacy ONVIF scheme |
| 3 | HTTP Digest with the optional ONVIF account | The RTSP account was rejected and an ONVIF account is configured | A separate ONVIF account works |
| 4 | WS-UsernameToken digest with the optional ONVIF account, camera-clock timestamp | The RTSP account was rejected and an ONVIF account is configured | A separate ONVIF account works with the legacy scheme |
| 5 | WS-UsernameToken `PasswordText` over HTTPS | Only with an explicit per-camera opt-in and a quirk record | A camera that only accepts text works, without exposing the password on HTTP |
| 6 | No authenticated path | All of the above fail | The camera is adopted through manual RTSP and the ONVIF failure is reported |

Rules:

- HTTP Digest is first when advertised because it is the Core baseline, Profile T mandates it, and
  it has no timestamp, so it survives a wrong camera clock. [S1] 5.9.1; [S12] 7.1.3; [S15] 3.
- WS-UsernameToken is still required for Profile S and legacy cameras. It must carry a nonce and a
  `Created` timestamp; the digest is SHA-1 over nonce, created and password. [S1] 5.9.5; [S14] 3.1.
- `PasswordText` is never sent over HTTP. Over HTTPS it is still a last resort because the password
  reaches the SOAP layer; the OASIS profile recommends a secure transport or an encrypted token, not
  plaintext in a header. [S14] 3.1. The opt-in and the quirk record make the exposure a decision,
  not a default.
- A credential rejection is not a scheme negotiation. If the camera rejects the credential for the
  current account, do not try the same account again with another scheme in the same pass. Try the
  other account once, then stop and back off. This protects against login locks. The ladder above is
  for scheme negotiation and capability failures, not for a password that the camera has already
  rejected.
- A `401` to an unauthenticated request with a `WWW-Authenticate` challenge is a scheme
  negotiation. Follow the challenge; do not count it as a password failure. A `401` after an
  `Authorization` header was sent is a credential rejection.
- A SOAP `NotAuthorized` or a repeated `401` after a credential was sent is a credential rejection,
  unless the camera clock offset and an HTTP Digest or camera-clock retry show that the credential is
  good and the timestamp was the problem. In that case record `timestamp-rejected`; otherwise record
  `auth-rejected` and stop the ladder for that account.
- Cap the pass at four credential-bearing digest attempts (two accounts by two schemes). An
  unauthenticated request that receives a `WWW-Authenticate` challenge is not a credential attempt.
  The `PasswordText` opt-in is a separate explicit action, not a fifth automatic attempt. A failed
  pass backs off with the same schedule as the time sync: 15 minutes, then hourly after three
  failures, then six-hourly after 24 hours. A success resets the schedule.
- Never send a credential to WS-Discovery, to an unselected camera, or to an endpoint that failed
  validation.

### What is remembered per camera

| Field | Example | Why |
| --- | --- | --- |
| `advertised_methods` | `[http-digest, wsse-digest]` | Avoids retrying a scheme the camera says it does not support |
| `working_method` | `http-digest` | Tried first on the next pass |
| `account` | `rtsp` or `onvif` | The account that worked, not the credential |
| `clock_source` | `camera`, `http-date` or `appliance` | Explains how the first timestamp was built |
| `digest_algorithm` and `qop` | `MD5`, `auth` | Replays the HTTP Digest challenge parameters without storing the nonce |
| `last_success` and `failure_count` | timestamp, integer | Drives the backoff and the status |
| `backoff_until` | timestamp | Prevents a retry storm and a login lock |
| `camera_clock_offset` | `-247 s` | Status and diagnosis; not used to set the appliance clock |

No password, nonce, digest, `Authorization` header or complete URL with user information is
stored. The ONVIF account, when one is needed, lives under the camera's existing `credentials_ref`
in `/etc/bittern/secrets.yaml`, never in `appliance.yaml`. [ADR 0035](../adr/0035-appliance-as-time-authority.md).

## Failure model

Every stage records a result. A failure is evidence, not an adoption verdict.

| Field | Content | Redaction rule |
| --- | --- | --- |
| `camera_id` | Stable camera identifier | Not a secret |
| `stage` | `address`, `endpoint`, `clock`, `capabilities`, `services`, `media`, `profiles`, `stream`, `snapshot`, `classification`, `ptz` | None |
| `operation` | The ONVIF operation that failed | None |
| `result` | `ok`, `unsupported`, `auth-required`, `auth-rejected`, `timestamp-rejected`, `refused`, `timeout`, `malformed`, `tls-untrusted`, `unreachable`, `clock-unavailable`, `no-profile`, `no-stream`, `no-snapshot`, `no-ptz` | None |
| `http_status` | HTTP status when there was one | None |
| `soap_code` and `soap_subcode` | The SOAP fault values when there were any | None |
| `camera_clock_offset` | Seconds between the camera and the appliance at the failed attempt | Status and diagnosis only |
| `duration_ms` | Time spent | None |
| `attempt` | Attempt number in this pass | None |
| `fallback` | The fallback tried, or `none` | None |
| `safe_detail` | One sentence naming the missing fact | No credential, no nonce, no complete URL with user information, no query token |
| `at` and `next_retry_at` | Timestamps | None |
| `quirk_ids` | Quirks applied to this camera | None |
| `evidence_ref` | Test, fixture, capture or field-run reference | No serial number, MAC address or site identifier |

The fallbacks are fixed:

| Failed stage | Fallback | Adoption |
| --- | --- | --- |
| Address validation | None; the address must be a valid IPv4 literal on `cam0` | Refused before a camera is created |
| Endpoint unreachable | Manual RTSP floor | Adopted |
| Clock unavailable | HTTP `Date`, then HTTP Digest, then manual RTSP | Adopted |
| Capabilities malformed | `GetCapabilities/Media/XAddr` as Media1, else manual RTSP | Adopted |
| Media service absent | Manual RTSP floor | Adopted |
| Profiles empty or unreadable | Manual RTSP floor | Adopted |
| Stream URI fails for every profile | Manual RTSP floor | Adopted |
| Snapshot URI fails | Stream only; the snapshot path is reported missing | Adopted |
| Authentication fails | Manual RTSP floor; the ONVIF account can be added later | Adopted |
| PTZ fails | Stream and snapshot remain; PTZ is reported unsupported | Adopted |

Rules:

- **No ONVIF failure blocks adoption.** Adoption commits the camera, its reservation and its
  credential as it does today. The probe runs after the commit and cannot roll it back. This follows
  ADR 0024's rule that an optional service is a degradation, and ADR 0035's rule for time.
- **A failed probe does not erase a good catalogue.** Keep the last successful catalogue and mark
  it `stale`. A transient timeout must not break a working stream. Replace the catalogue atomically
  on the next success.
- **Invalidate on identity change.** An address change invalidates stream URIs. A credential
  change invalidates the auth method but not the stream URIs. A firmware change invalidates quirk
  matches and triggers a profile re-probe.
- **No retry inside a pass.** One attempt per method and account. A failed camera does not delay
  the other cameras. The backoff is the same as the time sync.
- **A camera that is never probed has no ONVIF block.** It uses the manual RTSP path. The absence
  of a probe is not a negative claim about the camera.

## Clock skew

The first authenticated request must carry a `Created` timestamp near the camera's clock. The flow
is:

1. Read `GetSystemDateAndTime` without authentication. It is `PRE_AUTH`. Use `UTCDateTime`, not
   `LocalDateTime`, for `Created`. [S1] 8.3.6.
2. If the read is refused, read the HTTP `Date` header from the same response. RFC 9110 requires an
   origin server with a clock to send `Date` in 2xx, 3xx and 4xx responses. [S16] 6.6.1. Use it only
   for the first `Created`; its one-second resolution is enough for authentication.
3. If neither is available, use the appliance clock once and record `clock_source=appliance`.
4. If WS-UsernameToken is rejected as a timestamp failure and HTTP Digest is available, retry with
   HTTP Digest. It has no timestamp, so the skew no longer matters. If it succeeds, read the camera
   time with the authenticated session and, if the camera permits, set it.
5. If the camera only accepts WS-UsernameToken, refuses the pre-auth time read and the HTTP `Date`,
   and also rejects the appliance-clock attempt as a timestamp failure, the camera is unreachable
   for authenticated ONVIF until its clock is fixed. Record `clock-unavailable`, keep the camera
   adopted through manual RTSP, and show the owner action to set the clock in the camera's own UI.
   This is the honest limit.
6. Never set the appliance clock to match a camera. The appliance is the time authority.
   [ADR 0035](../adr/0035-appliance-as-time-authority.md). Never disable TLS verification or replay
   protection to work around a clock.
7. Never retry the clock ladder in a loop. A camera with a dead clock battery will keep failing; the
   status stays `refused` or `clock-unavailable` with an action, and the backoff applies.

The documented freshness guideline is five minutes, and it is a recommendation, not a guaranteed
window. [S14] 3.1. ONVIF Core 26.06 does not define a numeric window. The exact vendor window is OQ-42.
The strategy does not assume five minutes; it assumes only that the camera's own clock is the best
available timestamp.

## What the schema must store

The renderer must never probe. The schema therefore stores the resolved render inputs, not just the
camera address and the profile token. The proposal is schema version 3 with an optional `onvif`
block per camera and a lossless migration from version 2: existing cameras get no `onvif` block and
continue to render from the manual RTSP path.

| Field | Required | Why the renderer needs it | Secret or validation rule |
| --- | --- | --- | --- |
| `onvif.endpoint` | Yes when `onvif` is present | The device service URL, including a non-standard port and scheme | No user information, no fragment; host must equal the camera `address`; scheme `http` or `https`; port 1 to 65535 |
| `onvif.media_service` | Yes when a media service was selected | Records whether the selected service is `media1` or `media2` | Enum |
| `onvif.stream_source` | Yes | Chooses the ONVIF stream or the manual RTSP path | Enum `onvif`, `manual` |
| `onvif.profiles.high.token` | Yes when `stream_source=onvif` | The profile token that produced the high stream | 1 to 64 characters |
| `onvif.profiles.high.encoding` | Yes when `stream_source=onvif` | The codec for validation and display | `H264` or `H265` |
| `onvif.profiles.high.width`, `.height` | Yes when `stream_source=onvif` | The resolution for validation and display | Positive integers |
| `onvif.profiles.high.frame_rate_limit` | No | The reported limit, for display and the classification record | Number; absent means unknown |
| `onvif.profiles.high.bitrate_limit` | No | The reported limit, for display and the classification record | Integer kbps; absent means unknown |
| `onvif.profiles.high.stream_uri` | Yes when `stream_source=onvif` | The exact URI the camera returned | No user information; scheme `rtsp` or `rtsps`; host must equal the camera `address`; no fragment; no query value that is a credential |
| `onvif.profiles.low` | No | The low stream; absent means use high for detect and record | Same rules as high |
| `onvif.snapshot.profile_token` and `.uri` | No | The snapshot path without exposing the camera credential to the browser | No user information; scheme `http` or `https`; host must equal the camera `address` |
| `onvif.ptz.enabled` and `.profile_token` | No | The PTZ surface for BIT-027 | Boolean and a token; detailed capabilities stay in the catalogue |
| `onvif.probe.at`, `.source`, `.quirk_ids` | Yes when `onvif` is present | Audit and reproducibility | `source` is `manual` or `discovery`; no site identifier |

What does **not** go in `appliance.yaml`:

- The auth method, the clock source, the camera clock offset, the full profile catalogue, the
  service capabilities, and the failure records. These are derived state and live in the camera
  catalogue or health cache, consistent with ADR 0035's rule for the detected time mechanism.
- Passwords, nonces, digests, `Authorization` headers, complete URLs with user information, serial
  numbers, MAC addresses and site identifiers.
- A credential embedded in a stream URI. If a camera returns a URI with user information, strip it
  and reconstruct the user information from `credentials_ref` at apply time through the same
  generated environment-file path as the RTSP URL, never through a rendered `config.yml`. If the URI
  carries a query token that cannot be proven non-secret, store only the path and record a quirk.

The renderer rule:

- Render is a pure function of `appliance.yaml`, the templates, the defaults and the secret
  substitution. It opens no socket and has no HTTP client.
- If `stream_source=onvif` and a required URI is missing, validation fails with the camera id and
  the missing field. It does not probe.
- If the stored URI is stale, the apply health check fails; the agent re-probes on the next health
  cycle and re-renders. Render itself stays offline.
- A golden render test uses a stored `onvif` block and would fail if the renderer tried to connect.

## Vendor quirks

A quirk is a documented adjustment for a vendor, model and firmware range, with evidence and a
test. It is not a hidden timeout, a silent retry, or an unrecorded `if vendor == ...` branch.

The register lives in [the compatibility matrix](../engineering/onvif-compatibility.md#quirk-register)
with these columns:

| Column | Content |
| --- | --- |
| `quirk_id` | Stable identifier, for example `Q-0001` |
| `vendor` | Device-reported manufacturer |
| `model` | Model pattern or exact model; never a serial number |
| `firmware` | Exact version or a tested range |
| `symptom` | What the camera does differently |
| `detection` | The capability, response or failure that identifies the quirk |
| `adjustment` | The smallest change: endpoint, scheme, path, SOAP version, auth order, media service, `Type`, transport or timeout |
| `scope` | The operations affected |
| `evidence` | Vendor document URL and access date, HIL run, packet capture or fixture |
| `test` | The fixture or test that proves the adjustment |
| `owner` and `status` | Role, and `proposed`, `active`, `retired` or `rejected` |
| `review_after` | Date or firmware event for re-verification |

Rules:

- No quirk without evidence. A forum post can open an open question; it cannot activate a quirk.
- No quirk without a test or fixture. A quirk that cannot be tested stays `proposed` and is not
  applied.
- The adjustment is data, not scattered code. The agent loads one quirk table and applies a named
  adjustment at the probe stage. The applied quirk ids are recorded with the catalogue and the
  failure record.
- A quirk is keyed by manufacturer, model and firmware, not by IP address, serial number or site.
- A firmware change invalidates the match and re-probes.
- A quirk is retired when the vendor fixes it or when the firmware range falls out of support. The
  review date makes that visible.
- Protocol rules are not vendor quirks. Media2 `Type=All`, Media1 `StreamSetup`, Media2 protocol
  names, and the fixed device service path are implemented as protocol behaviour for every camera.
  Reolink's port 8000 is a documented default handled by `onvif.endpoint`, not a quirk. Hikvision's
  ONVIF enablement is an owner setup step, not a client adjustment.
- Candidate quirks with no evidence are listed in the matrix as candidates only: an alternate device
  service path, SOAP 1.1-only, `PasswordText`-only, a snapshot endpoint that needs separate auth,
  and PTZ that advertises presets but returns none. None is active.

## Top ten vendor behaviours

The table states what the client should expect, whether the documentation verifies it, and how the
strategy survives it. "Folklore" means a widely repeated claim that the sources found do not
establish; it is not implemented as a default.

| # | Behaviour to expect | Status | Evidence | How the strategy survives it |
| --- | --- | --- | --- | --- |
| 1 | A camera may speak Media1 only, Media2 only, or both | Verified | [S2] 1; [S3] 4.1; [S11] 7.8; [S12] 7.9.3 | Read `GetServices`, prefer Media2, fall back to Media1 once, never mix profile tokens |
| 2 | A Media2 camera may return profile shells without configurations unless `Type` is sent | Verified | [S2] 5.1.2 | Always send `Type=All`; if incomplete, fetch `GetVideoEncoderConfigurations` and `GetVideoSourceConfigurations` per profile |
| 3 | `GetStreamUri` is shaped differently in Media1 and Media2, and transport support varies | Verified | [S3] 5.15.1; [S2] 5.4.1 | Build the request per service; try RTSP/TCP, then UDP, then HTTP; store the URI the camera returned |
| 4 | ONVIF may be on a non-standard port and a different `XAddr` from the device service | Verified for Reolink 8000 and for the Hikvision port split; the general XAddr rule is verified | [S17]; [S19]; [S26]; [S1] 8.1.2.2, 8.1.2.4 | Add an ONVIF endpoint or port field; use discovery `XAddr`; validate the host; never scan |
| 5 | A camera may use a device service path other than `/onvif/device_service` | Folklore; no vendor document found on 2026-11-27. The fixed path is verified | [S1] 5.1.1; [S13] 5.1.4 | Use the fixed path; allow an alternate only through a documented quirk with evidence; no path wordlist |
| 6 | A camera may accept only WS-UsernameToken, only HTTP Digest, or both | Verified | [S1] 5.9.1; [S11] 7.1.3; [S12] 7.1.3 | Read `Security/UsernameToken` and `Security/HttpDigest`; prefer HTTP Digest; keep the WS-UsernameToken ladder; remember the method |
| 7 | A camera may reject an authenticated request whose timestamp is far from its clock | Verified that timestamps are checked; the exact window is folklore | [S1] 5.9.5; [S14] 3.1 | Read the camera clock first; fall back to the HTTP `Date` header; use HTTP Digest as the skew escape; record a rejection |
| 8 | ONVIF may need enabling, and the ONVIF account may differ from the web or RTSP account | Verified for Hikvision enablement and separate accounts, and for Dahua's separate ONVIF user; assumed for other vendors | [S18]; [S20]; [S26] | Treat auth failure as a capability gap; offer an optional ONVIF account under the same `credentials_ref`; never block adoption |
| 9 | Profile tokens, names and order may not mean main or sub, and profiles may share an encoder | Verified for token and name meaning; the specification is silent on order | [S10] `ReferenceToken`; [S9] `Profile`; [S2] 5.2.1 | Classify by resolution, bitrate, frame rate and encoding; dedupe by encoder token; use the token only as a final tie-break |
| 10 | PTZ may be partial: continuous move and stop are mandatory, but presets, zoom, home and absolute or relative moves are conditional | Verified | [S4] 5.1.1, 5.3.1, 5.3.2, 5.3.3, 5.3.5, 5.4, 5.7, 5.10; [S12] 7.22 | Detect the node, spaces, preset count and status capabilities; show only the supported controls; always pair a move with Stop and a timeout |

The folklore items are deliberate. A client that assumes a different device service path, a SOAP 1.1
binding, a `PasswordText` requirement, or presets on every PTZ camera will add code that no evidence
supports. Each becomes a quirk candidate, not a default.

## What this proposal does not decide

- **OQ-45:** whether the bench camera exposes a sub stream and which Media profiles it advertises. The
  matrix row stays unknown until the field run records the tokens, resolutions and codecs.
- **OQ-47:** whether Media2 is authoritative when both services are advertised, and whether their
  profile tokens or configurations ever agree. The proposal chooses Media2 primary with a Media1
  fallback; the specification is silent.
- **OQ-48:** whether the HTTP `Date` header matches the ONVIF clock closely enough to bootstrap
  WS-UsernameToken on cameras that refuse the pre-auth time read. The proposal uses it as a
  fallback; it needs a HIL measurement.
- **OQ-49:** which authentication methods a given camera and firmware actually accept, and whether a
  camera's auth-mode setting removes WS-UsernameToken or HTTP Digest. The capability flags and the
  fallback ladder need a matrix column and a HIL test.
- **OQ-50:** whether the deterministic high/low ranking is the product policy, or whether the owner
  should choose. The proposal uses the ranking as a provisional default.
- The schema version 3 field names, the migration, the quirk file format, the UI for the profile
  picker and the PTZ surface, and the events integration are BIT-027 work. They are not decided
  here.
- No product code is changed in this research. The current time client remains the camera half of
  ADR 0035 until BIT-027 extends it.

## Evidence

The full fact table, specification sections, vendor documents and access dates are in
[the assessment](../research/onvif-compatibility.md). The key sources are the ONVIF Core
Specification 26.06, the Media Service 24.12, Media2 Service 26.06, PTZ Service 26.06, Profile S 1.3,
Profile T 1.0, the OASIS UsernameToken Profile 1.1.1, RFC 7616, RFC 9110, and the Reolink, Hikvision
and Dahua vendor documents, all accessed 2026-11-27.

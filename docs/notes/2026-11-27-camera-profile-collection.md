# Camera profile collection and the per-camera credential model

- Date: 2026-11-27
- Related: [ADR 0037](../adr/0037-nvr-grid-per-pane-quality-and-full-screen.md),
  [ADR 0035](../adr/0035-appliance-as-time-authority.md),
  [ONVIF compatibility strategy](2026-11-27-onvif-compatibility-strategy.md),
  [camera matrix](camera-matrix.md),
  [status contract](../status.md),
  [BIT-027](../plan/implementation-plan.md#bit-027-robust-onvif-profiles-ptz-and-field-interoperability)

## What changed

The secrets model no longer stores one complete RTSP URL per camera. A camera's
`credentials_ref` now resolves to a mapping with `username`, `password`, and optionally
`onvif_username` and `onvif_password`. The renderer builds each profile's complete URL from that one
account and the profile path; apply writes the result to `cameras.env`, one environment variable per
profile. Two profiles produce two URLs with the same credentials and different paths. A camera with
one stream produces one URL under both go2rtc keys. A no-auth camera renders its paths directly.

The old scalar URL and the old mapping with `rtsp_url` stay readable. `MigrateCameraAccounts` rewrites
them to the account shape on the next apply, is idempotent, keeps every unrelated key and value,
leaves a value it cannot parse untouched, and writes mode 0600. An old reference that names
`cameras.<id>.rtsp_url` resolves to the account at `cameras.<id>`.

The ONVIF client gained `GetServices` with a `GetCapabilities` fallback, Media2 and Media1 profile
lists, `GetStreamUri`, `GetSnapshotUri`, encoder-configuration completion, HTTP Digest, and the
WS-Security text form over HTTPS. The probe reads the camera clock before the first authenticated
call, uses a camera-clock `Created` timestamp, falls back to the HTTP `Date` header, and records a
rejected timestamp distinctly.

Classification is the ADR 0037 rule: H.264 or H.265 with a source and encoder token, a positive
resolution, a validated stream URI, and a deduplicated encoder. Sort by area, bitrate, frame rate,
then token. Main is last; sub is the first strictly smaller profile whose URI differs. Names never
participate.

Adoption runs the probe once, before the document is written, with a 30 second budget. It stores the
selection in `appliance.yaml`, records the state, reason, action, media service, auth method, clock
source and tokens in `camera-health.json`, and never fails the apply. `status.md` carries the state
table. The setup result and done pages show the same reason and next step, and the camera list shows
the state.

## Tests

- `agent/internal/secrets/camera_test.go`: migration of all three shapes, idempotence, mode, a value
  that cannot be read, old and new lookups, and redaction of all four account fields.
- `agent/internal/render/frigate_test.go`, `agent/internal/apply/secrets_test.go`: one account to two
  URLs, one URL for one stream, distinct paths, the same account key for both profiles.
- `agent/internal/cameraprofile`: parameterised fake ONVIF endpoint covering Media1, Media2, both,
  WS-Security digest, HTTP Digest, password text over TLS, an out-of-time camera, ONVIF disabled, a
  refused account, one profile, two profiles with one encoder, two profiles with two encoders,
  unusual tokens, no media service, unreadable profiles, and no stream. Classification tests cover
  the tie-breaks, the name rule, duplicate encoders, and unusable profiles.
- `agent/internal/ui/camera_profiles_test.go`: every outcome still commits adoption, the selection is
  stored only on success, the reason and action are recorded, and a camera with stored profiles is
  not re-probed.
- `agent/internal/status/camera_profiles_test.go`: the state vocabulary has an owner action, and a
  profile detail that quotes a username and password is redacted from the document.

## Still unverified

- No real camera has run the probe. OQ-45, OQ-47, OQ-48, OQ-49 and OQ-50 remain open. The fake
  endpoint proves the protocol shape and the failure branches, not a vendor's answer.
- The HTTP Digest implementation handles MD5 and SHA-256 with `qop=auth`; no camera has been tested
  with `auth-int`, `-sess`, or an unusual challenge.
- The text password form is implemented and tested, but no camera field or opt-in reaches it yet.
  Production leaves it off. A camera that only accepts text over HTTPS still falls back to manual
  RTSP until a quirk and an opt-in exist.
- The profile-to-encoder fallback for Media2 shells is implemented but not exercised by a fixture that
  omits configurations. The fake always returns them inline.
- An `rtsps://` answer is rejected: the schema stores a path and the renderer builds `rtsp://`, so
  accepting it would silently downgrade the transport. Adding a scheme field needs an ADR.
- The working account and scheme are recorded per camera in status, but a later edit still starts the
  ladder from the RTSP account. Reusing the recorded order is a small follow-up.
- An old secret whose URL path differs from the camera's `rtsp_path` keeps that path through the
  migration, but only while the camera has no explicit path and no profile. That is the compatibility
  floor, not a profile source.
- PTZ, events, imaging, the profile picker, and the quirk register are not part of this change.
- The golden render and status fixtures were updated, but the browser check was not run: Node and
  Playwright are not provisioned in this environment.

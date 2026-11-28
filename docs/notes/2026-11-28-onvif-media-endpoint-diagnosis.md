# ONVIF media path diagnosis

- Date: 2026-11-28
- Status: fixed and tested offline. The field camera model and firmware are still unknown; the next
  field run runs the probe and fills the matrix row.
- Related: [ADR 0035](../adr/0035-appliance-as-time-authority.md),
  [ADR 0037](../adr/0037-nvr-grid-per-pane-quality-and-full-screen.md),
  [ONVIF compatibility matrix](../engineering/onvif-compatibility.md),
  [camera ONVIF diagnosis runbook](../runbooks/camera-onvif-diagnosis.md),
  [OQ-47](../open-questions.md#oq-47), [OQ-51](../open-questions.md#oq-51),
  [status contract](../status.md)

## Field report

An Amcrest camera on `10.77.0.50` was adopted. The clock was read and set over ONVIF through the
appliance, the camera's hardware address and vendor were known, and streaming and test pictures
worked. The camera page still reported `unreachable`: "Bittern could not reach this camera to read its
stream settings, so it has one stream for now", with the next step to check power and the cable. The
device service, authentication and time operations answered, so the camera was not unreachable. The
failure was on the media path.

## Root causes

1. **The media endpoint was selected from `GetServices` alone.** The client read `GetCapabilities`
   only when `GetServices` named no media service. An Amcrest camera can repeat the device service
   path, or a path that does not answer, for the media namespace in `GetServices` while
   `GetCapabilities/Media/XAddr` names the working media path. The media calls then hit the wrong
   path and failed at the transport level. `DiscoverServices` now always reads both sources and uses
   the `GetCapabilities` media endpoint first; `GetServices` is the fallback. Media2 remains the
   preferred service and the Media1 fallback uses the resolved capabilities endpoint.
2. **A non-`rtsp` stream URI had no reason of its own.** `ReduceRTSPURI` returned a generic error, so
   a camera that answered `rtsps://` became a profile with no URI and then the generic no-stream or
   transport reason. It now returns a typed `UnsupportedSchemeError`; the collector records the
   scheme and reports `rtsps-unsupported` with an action that can help.
3. **Rejected profiles were silent.** The classifier dropped a profile for a missing encoder, an
   unusable codec, no resolution, no stream URI or a duplicate URI without recording which check
   failed. It now returns one `ProfileRejection` per profile. When every profile is rejected the
   stored state is `profiles-rejected` with the token and reason for each one, and the camera page
   Technical details lists the same reasons.
4. **The copy assumed transport failure after an ONVIF answer.** The `unreachable` reason ran for any
   unreachable media call, even when the clock or the device service had just answered. The collector
   now tracks whether the camera answered ONVIF at all. A media-only failure is `media-unavailable`
   with a stream settings action; `unreachable` is reserved for a camera where no ONVIF call answered,
   and it is the only reason that says to check power and the cable.

## The probe

`sudo bittern camera probe <camera>` is a read-only maintainer command. It prints the device service,
every `GetCapabilities` and `GetServices` URL verbatim, `GetDeviceInformation`, the clock,
`GetProfiles`, the encoder configuration behind each profile, `GetStreamUri`, `GetSnapshotUri`, and
one line per call with the URL, HTTP status, SOAP fault code and reason, the authentication or
transport failure class, and the size and duration. It prints the profile tokens, encodings,
resolutions and the stream URI scheme. It registers the saved account values and the reported serial
number with the redactor and refuses to print a report that still contains either one.

## Tests

- `TestCollectUsesTheMediaEndpointFromCapabilities` fails before the endpoint fix: `GetServices`
  repeats the device service path and the media call returns 404.
- `TestCollectFallsBackToCapabilitiesMedia1WhenMedia2Fails` fails before the fix: Media2 fails and
  the fallback would use the device service path instead of the capabilities media endpoint.
- `TestCollectReportsATLSOnlyStream` fails before the fix: an `rtsps` answer became `unreachable` or
  `no-stream`, not a TLS reason.
- `TestCollectReportsPerProfileRejectionReasons` and `TestCollectReportsPerProfileNoStreamReasons`
  fail before the classifier changes: the reasons were not recorded.
- `TestMediaFailureAfterONVIFAnswerIsNotUnreachable` and `TestUnreachableCopyOnlyWhenNothingAnswered`
  fail before the copy change: a camera that answered the clock was described as unreachable.
- `TestDiagnoseUsesTheCapabilitiesMediaPathAndRedacts`,
  `TestDiagnosisRenderRedactsAFaultThatEchoesThePassword` and
  `TestEveryProfileReasonCarriesTheActionThatHelps` cover the probe and the copy contract.

## Decision on TLS RTSP

TLS RTSP is not implemented. The schema stores a path and a single `rtsp_port`, the renderer builds
`rtsp://` for go2rtc and the Frigate input, and go2rtc can carry `rtsps`, so carrying TLS needs a
schema change and a renderer change, not a switch. [OQ-51](../open-questions.md#oq-51) records the
options and estimates 3 to 5 working days, including migration, golden renders, UI copy, tests and a
field verification. Until then the camera is reported as `rtsps-unsupported` and the owner is told to
turn on the unencrypted stream.

## Next step

Run `sudo bittern camera probe 10.77.0.50` on the appliance during the next field visit. Record the
model, firmware, device service, the resolved media endpoint, the media version, the auth method and
the profile facts in the [ONVIF matrix](../engineering/onvif-compatibility.md). If the camera reports
an `rtsps` profile, capture the advertised TLS port for OQ-51. This change does not move the Frigate
pin or change the wall layout or capacity.

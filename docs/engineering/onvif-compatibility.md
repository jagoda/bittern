# ONVIF compatibility matrix

- Date: 2026-11-27
- Status: skeleton. No product camera has a tested ONVIF row.
- Process: [engineering](README.md),
  [findings and principles](../notes/2026-11-15-findings-and-principles-process.md)
- Related: [ONVIF compatibility assessment](../research/onvif-compatibility.md),
  [compatibility strategy](../notes/2026-11-27-onvif-compatibility-strategy.md),
  [camera matrix](../notes/camera-matrix.md), [field parity](field-parity.md),
  [ADR 0024](../adr/0024-onvif-discovery-levels.md),
  [ADR 0035](../adr/0035-appliance-as-time-authority.md),
  [BIT-027](../plan/implementation-plan.md#bit-027-robust-onvif-profiles-ptz-and-field-interoperability),
  [OQ-10](../open-questions.md), [OQ-45](../open-questions.md)

This matrix answers two questions for one camera on one firmware: does ONVIF work, and which ONVIF
features does the camera actually provide? It is evidence, not a certification list, and it does not
promise support for a model or a future firmware.

The [camera matrix](../notes/camera-matrix.md) remains the adoption and OQ-10 evidence register:
discovery layer, RTSP path, credentials in the abstract, time mechanism and quirks. This engineering
matrix is the protocol and feature detail: services, media version, exact profile attributes, stream
and snapshot behaviour, time operations and PTZ operations. A camera row here links to its OQ-10 row
instead of copying that evidence.

## Rules

- One row per vendor, model and firmware. A firmware change is a new row or a new firmware cell with
  a new evidence date. A model without a firmware value is `unknown`.
- Every non-unknown cell cites evidence in the row. A cell without evidence is `unknown`.
- `ONVIF works` is `yes` only when the endpoint, authentication, profile selection, high stream and
  snapshot are all verified. `partial` means at least one of those works and at least one does not.
  `no` means the ONVIF path was attempted and failed. `unknown` means it was not tested.
- `Manual floor` records whether the manual RTSP path works independently of ONVIF. A camera with
  `ONVIF works = no` can still be supported through the manual floor.
- The manual RTSP and snapshot path is the baseline for every camera and does not depend on a row.
- A failed or partial result is a row. Record the stage that failed and the fallback that worked.
  Do not hide a model or call it supported.
- A quirk needs an id, evidence and a test. A quirk candidate is not a quirk and is not applied.
- Do not put credentials, complete stream URLs with user information, serial numbers, MAC addresses,
  or site identifiers in this file. Profile tokens and model names are allowed.
- Read a row with `sudo bittern camera probe <camera>`. The command is read-only, prints the redacted
  call trace, and is documented in [the diagnosis runbook](../runbooks/camera-onvif-diagnosis.md).
  A probe output is not evidence until the model and firmware are recorded beside it.
- Update a row in the same change that fixes or documents its quirk, and link the test, fixture or
  field run that proves the row.

## Columns

| Column | Values | Meaning |
| --- | --- | --- |
| Vendor | text or `unknown` | Device-reported manufacturer from `GetDeviceInformation`. |
| Model | text or `unknown` | Device-reported model. Never a serial number. |
| Firmware | text or `unknown` | Device-reported firmware version. A range is allowed only when the evidence covers the range. |
| ONVIF works | `yes`, `partial`, `no`, `unknown` | Endpoint, auth, profile, high stream and snapshot as defined above. |
| Manual floor | `yes`, `no`, `unknown` | Manual RTSP works without ONVIF. A snapshot may come from the stream or from the camera; the Snapshot column records the camera endpoint. |
| Device service | `http:80`, `https:443`, `http:8000`, `unknown`, with the path | Where the device service answered. The path is expected to be `/onvif/device_service`; a different path needs a quirk. |
| Services | `Device`, `Media1`, `Media2`, `PTZ`, `Events`, `Imaging`, or `none`, or a combination | Services that answered a namespace or `XAddr` query. Absence is a capability gap, not an adoption failure. |
| Media | `media1`, `media2`, `both`, `none`, `unknown` | The service used for the row. `both` records that both were advertised; the detail says which was authoritative. |
| Auth | `anonymous`, `http-digest`, `wsse-digest`, `wsse-text`, or a combination; account `rtsp` or `onvif` | The ONVIF method that worked, and which credential account it used. Never the credential value. Record a non-ONVIF Basic fallback as a quirk candidate, not here. |
| High profile | token, encoding, resolution, fps, kbps | The profile the classifier selected as high. Use `unknown` until measured. |
| Low profile | token, encoding, resolution, fps, kbps, or `none` | The profile the classifier selected as low, or `none` when there is only one eligible stream. |
| Stream | `rtsp/tcp`, `rtsp/udp`, `rtsps/tcp`, or `unknown`; path shape without user information | The transport and path that `GetStreamUri` returned. Do not record the full URL with credentials. |
| Snapshot | `yes`, `no`, `unknown`; JPEG is the expected format | Whether `GetSnapshotUri` and an actual JPEG fetch worked, and whether separate auth was needed. |
| Time | `read pre-auth`, `read auth-only`, `ntp`, `push`, `both`, `none`, `unknown`; observed result | The time operations the camera accepted. `both` means NTP and a pushed time. Record the mechanism and the result, not a vendor claim. |
| PTZ | `none`, `continuous`, `stop`, `presets N`, `zoom`, `home`, `status`, `unknown`, or a combination | The controls the node actually advertised and answered. `presets N` records the count. |
| Evidence | test, fixture, capture or field run, with a date | The smallest proof for the row. A vendor document alone is not device evidence. |
| Quirks | `none`, or a comma-separated list of quirk ids | Quirks in the register that were applied and tested for this row. |
| Last verified | date or `unknown` | When the row was last checked on the named firmware. |

## Matrix

| Vendor | Model | Firmware | ONVIF works | Manual floor | Device service | Services | Media | Auth | High profile | Low profile | Stream | Snapshot | Time | PTZ | Evidence | Quirks | Last verified |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| unknown | Bench camera | unknown | unknown | unknown | unknown | unknown | unknown | unknown | unknown | unknown | unknown | unknown | unknown | unknown | none | none | unknown |

BIT-027 names a recent Axis PTZ as the reference device. The vendor, model, firmware and every
capability stay `unknown` until the maintainer supplies the model and firmware and the field run
records them. [OQ-45](../open-questions.md) records the bench camera's advertised profiles. The
ticket naming Axis is not enough to fill the vendor cell, because the row is evidence for a specific
unit and firmware. Do not fill this row from the ticket, a vendor datasheet or a model family
assumption. A vendor document is not evidence that a specific unit answered a specific operation.

## How to fill a row

1. Run `sudo bittern camera probe <camera>` and keep its redacted report with the run record. Record
   the vendor, model, firmware and hardware id from the report's `GetDeviceInformation` step. If the
   call fails, the row stays `unknown` for identity and cannot link a quirk. Do not copy the serial
   number.
2. Record every service from `GetServices`, and every `XAddr` from `GetCapabilities`. Mark a service
   present only when it answered; do not infer it from the model. The media calls must use the
   `GetCapabilities` `Media` URL; a `GetServices` media URL that repeats the device service path is a
   camera defect to record, not the endpoint to follow.
3. Record the media service that was authoritative. If both were advertised, record both and say
   which produced the stream URI. Record the resolved media URL, not only the device service URL.
4. Record the authentication method and the account type. Never the credential, the nonce or the
   `Authorization` header.
5. Record each eligible profile with its token, encoding, resolution, frame rate limit and bitrate
   limit. Record the high and low choice from the strategy's rule, and the reason when the rule
   produced no low stream.
6. Record the stream transport and path shape. Strip user information and query tokens.
7. Record the snapshot result, including whether it used a separate sign-in.
8. Record the time operations: pre-auth read, authenticated read, NTP list, NTP mode, pushed time,
   and the observed result for each.
9. Record the PTZ service, the node, the supported spaces, the preset count, home support, zoom
   support and the status capabilities. Test one continuous move with Stop, one preset recall, and
   one zoom action if zoom is advertised.
10. Record the evidence: the test name, fixture, capture or field run, with a date. Link the quirk
    ids and their tests.
11. Re-run the row after a firmware change. A firmware change invalidates the quirk match and may
    change the profile list.

## Quirk register

Every quirk is a documented adjustment for a vendor, model and firmware range, with evidence and a
test. The policy is in [the strategy note](../notes/2026-11-27-onvif-compatibility-strategy.md#vendor-quirks).

| Quirk ID | Vendor | Model pattern | Firmware | Symptom | Detection | Adjustment | Scope | Evidence | Test | Owner | Status | Review after |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Q-0001 | Dahua, Amcrest OEM | model unknown in the field report | unknown | ONVIF returns one usable main profile while the sub stream is the same RTSP path with `subtype=1` | Exactly one distinct usable stream URI, and the reported vendor or model contains `Dahua` or `Amcrest` | Construct the same path with `subtype=1`, connect with RTSP DESCRIBE, require an H.264 or H.265 video track, and require any reported dimensions to be smaller than main. Never apply when two distinct URIs were reported. A failed probe records the quirk as unverified and stores no sub profile. | Profile collection only; render never probes | Field report, 2026-11-28, an Amcrest camera: ONVIF surfaced one stream; the same path with `subtype=1` connected and returned H.264 video. Model and firmware were not captured. | `TestCollectProbesTheVendorSubstreamOnlyWhenNeeded`, `TestProbeStreamConnectsChallengesAndReadsTheSDP` | runtime | active | 2027-05-28 or the next firmware change |

Q-0001 is the only active quirk. Its model and firmware cells stay unknown until the next field run
captures them. The row records the camera behaviour as evidence, not a claim about every Amcrest
model. A candidate is not a quirk and is not applied. The candidates below are listed so the next
agent does not turn folklore into code without a record:

| Candidate | Why it is a candidate, not a quirk | Evidence needed |
| --- | --- | --- |
| Alternate device service path | The Core specification fixes the path to `/onvif/device_service`; no vendor document found states a different one | A vendor document and a packet capture from the named model and firmware |
| SOAP 1.1 only | Core 26.06 section 5.3 states the SOAP 1.2 bindings shall be used; a SOAP 1.1-only camera is non-conformant | A captured SOAP 1.1 fault or a vendor document, plus a test that the 1.1 fallback is required |
| `PasswordText` only | The OASIS profile allows text, but ONVIF Profile S and T clients can use digest; text exposes the password to the SOAP layer | A vendor document or capture showing digest is refused and text succeeds over HTTPS, plus a security review |
| Snapshot needs a separate sign-in | `GetSnapshotUri` is `READ_MEDIA` and may use the same account; a separate snapshot credential is not standard | A capture showing a distinct credential is required, and a test that redacts it |
| PTZ presets advertised but none returned | The PTZ specification says all preset operations are available when `MaximumNumberOfPresets` is greater than zero, but a non-conformant camera may return none | A node capture showing the mismatch, plus a test for the degradation |

Protocol behaviour that applies to every camera is not a quirk: Media2 `Type=All`, Media1
`StreamSetup`, Media2 protocol names, the fixed device service path, and the `READ_MEDIA` access
class are implemented as protocol rules. Reolink's port 8000 is a documented default handled by the
`onvif.endpoint` field. Hikvision's ONVIF enablement and separate ONVIF account are owner setup
steps, not client adjustments.

## What the matrix does not decide

A camera that returns only `rtsps://` stream URIs is recorded with the stored reason
`rtsps-unsupported`. The schema stores a path, not a scheme or a TLS port, and the renderer builds
`rtsp://`; the stream is not usable through the manual floor either. [OQ-51](../open-questions.md#oq-51)
records the work to carry TLS RTSP and is not a licence to half-implement it.

OQ-10 remains open. This file does not decide the support policy, the minimum firmware, or whether a
model is sold with the appliance. It records evidence so those decisions can be made without relying
on memory or a vendor claim. A row is evidence for the named firmware only; it is not a promise for
a later firmware.

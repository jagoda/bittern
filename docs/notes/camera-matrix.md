# Camera matrix

This is the living evidence register for OQ-10, the question of which cameras are supported. It
records what has been tested, which discovery layer produced each result, and which quirks are
known. It is not a certification list and does not promise support for a model or a future firmware
version.

Related: [ADR 0024](../adr/0024-onvif-discovery-levels.md), [OQ-10](../open-questions.md),
[BIT-008](../plan/implementation-plan.md#bit-008-camera-discovery-and-adoption-on-cam0),
[Frigate assessment](../research/frigate-assessment.md), and
[ADR 0023](../adr/0023-camera-native-ui-proxy.md) for camera web UI rows.

## Rules

- Every tested row needs evidence from the QEMU fixture, a hardware-in-the-loop run, or a field run:
  the model, firmware, discovery method, authentication, profile, RTSP path, and the layer that
  produced each result.
- A failed or partial result is a row. Record the layer that failed and the fallback that worked
  instead of hiding the model or calling it supported.
- The manual address plus RTSP credentials path is the baseline for every camera and is not
  conditional on a row in this table.
- A quirk that needs code, a template, or a renderer change gets a test or a named follow-up in the
  same change. Do not record folklore as a support claim.
- Do not put credentials, complete stream URLs, serial numbers, MAC addresses, or site identifiers
  in this file. Use the model and firmware only.
- A row is evidence, not a certification. The tested matrix and the manual fallback are the support
  statement.
- Update a row in the same change that fixes or documents its quirk, and link the test or fixture
  that proves it.

## Columns

| Column | Meaning |
| --- | --- |
| Model | Vendor and model name. |
| Firmware | Firmware version string as reported by the device. |
| Discovery | Layer that found it: `lease/ARP`, `WS-Discovery`, `ONVIF Device/Media`, `RTSP probe`, or `manual`. List every layer that contributed. |
| Auth | `none`, RTSP Basic or Digest, ONVIF WS-Security UsernameToken, or the observed combination. |
| Profile | ONVIF profile token or stream profile used, for example main or sub. |
| RTSP path | Stream path used for adoption, with user information removed. |
| Snapshot | Whether the Frigate snapshot path works, and any quirk. |
| Time mechanism | `ntp`, `push`, `both`, `none`, or `not tested`, from the camera time detection. |
| NTP result | Whether the camera accepted the appliance NTP server and its mode, with the observed result. |
| Push result | Whether the camera accepted a pushed UTC time and the fixed-offset POSIX zone, with the observed result. |
| Evidence | Test, fixture, capture, or field run that proves the row. |
| Quirks | Observed deviation, the fallback that worked, and any follow-up. |

## Tested devices

No product camera has been recorded as tested yet. The QEMU virtual camera fixture proves the
discovery, adoption, and RTSP contract, not a vendor model, so it is test infrastructure rather than
a matrix row. Add the first hardware row after a field run records the model, firmware, and stream
path.

| Model | Firmware | Discovery | Auth | Profile | RTSP path | Snapshot | Time mechanism | NTP result | Push result | Evidence | Quirks |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| _none yet_ | | | | | | | | | | | |

## Field candidates

Do not list a model here as supported. A candidate becomes a tested row only when the evidence column
contains a run or a test. A model that fails discovery is still useful as a failed row once the
failure is recorded, because the manual fallback and the failing layer are the evidence support
needs.

## Discovery layer names

The layer names match [ADR 0024](../adr/0024-onvif-discovery-levels.md):

| Layer | Name in this matrix |
| --- | --- |
| Manual baseline | `manual` |
| dnsmasq lease and ARP | `lease/ARP` |
| ONVIF WS-Discovery | `WS-Discovery` |
| ONVIF Device and Media services | `ONVIF Device/Media` |
| RTSP probe | `RTSP probe` |

## What the matrix does not decide

OQ-10 remains open. This file does not decide the support policy, the minimum firmware, or whether a
model is sold with the appliance. It records evidence so those decisions can be made without relying
on memory or a vendor claim.

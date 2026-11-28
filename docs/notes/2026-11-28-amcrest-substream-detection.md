# Amcrest sub-stream detection

- Date: 2026-11-28
- Status: fixed and tested offline; field model and firmware still to capture.
- Related: [ADR 0037](../adr/0037-nvr-grid-per-pane-quality-and-full-screen.md),
  [ONVIF compatibility strategy](2026-11-27-onvif-compatibility-strategy.md),
  [ONVIF compatibility matrix](../engineering/onvif-compatibility.md),
  [OQ-45](../open-questions.md#oq-45), [camera matrix](camera-matrix.md)

## Confirmed root cause

Profile classification deduplicated by encoder token before choosing main and sub. A real Amcrest
camera returned two profiles that shared one encoder token but had different stream URIs. The first
profile by token was kept and the other was dropped, so a working sub stream became invisible and
the camera was reported as one stream. The reproduction test
`TestClassifyKeepsDistinctURIsThatShareAnEncoder` failed against the old rule with `Sub: nil` and
passes with the fix.

## Dedupe rule

Before: eligibility was followed by encoder-token dedupe. The first profile by token won, even when
another profile with the same encoder resolved to a different stream URI.

After: a validated stream URI identifies a stream. Two profiles with different URIs are always
distinct, even when they share an encoder token. Profiles that resolve to the same URI collapse to
the first profile by token, and that collapsed token is recorded as evidence. The encoder token
never removes a URI. Main and sub are then chosen from the distinct URIs by the ADR 0037 ranking.

## Vendor evidence and quirk Q-0001

The field report is an Amcrest camera whose ONVIF profiles produced one usable stream. The same RTSP
path with `subtype=1` connected and returned a decodable H.264 video stream. The model and firmware
were not captured in this change, so the compatibility matrix records them as unknown.

The quirk is active only for a reported Dahua or Amcrest vendor or model. It is constructed only when
ONVIF produced exactly one distinct usable stream URI, so a camera that already reported two
distinct profiles is never given a vendor pattern. The candidate is offered only after an RTSP
DESCRIBE returns an SDP H.264 or H.265 video track; when the SDP reports dimensions, the candidate
must be strictly smaller than main. A failed probe records the quirk as unverified and offers no sub
profile.

## Manual override

The camera settings surface has an optional sub stream override. It accepts a path beginning with
`/`, rejects an address, user information, spaces and the main path, and connects with the saved
camera account before saving. A verified path is stored in the schema version 3 `profiles.sub` with
the stable token `manual-sub`, a blank value removes it, and the renderer uses it like any other sub
profile.

## Reason vocabulary

A one-stream camera now carries one of these states with its own action:

| State | Reason |
| --- | --- |
| `onvif-disabled` | ONVIF is off, so Bittern could not read a second stream. |
| `sign-in-rejected` | The camera refused the ONVIF sign-in. |
| `one-profile` | ONVIF listed one usable profile. |
| `profiles-share-uri` | ONVIF listed profiles that resolve to one stream address. |
| `sub-unverified` | A vendor sub-stream candidate was found but did not verify. |
| `no-smaller-profile` | ONVIF listed more than one stream but none is smaller than main. |

The live page, camera list and setup result show the stored reason instead of the generic "single
stream" sentence.

## Tests

- `TestClassifyKeepsDistinctURIsThatShareAnEncoder`
- `TestClassifyCollapsesDuplicateURIsAndRejectsUnusableProfiles`
- `TestClassifyUsesBitrateFrameRateAndTokenAsTieBreaks`
- `TestCollectProbesTheVendorSubstreamOnlyWhenNeeded`
- `TestCollectCoversMediaServicesAndAuthentication`
- `TestParseStreamSDPRequiresAVideoCodecAndReadsReportedShape`
- `TestProbeStreamConnectsChallengesAndReadsTheSDP`
- `TestProbeStreamRejectsAnInvalidPathBeforeDialling`
- `TestWizardAddVerifiesASubstreamBeforeAddingTheCamera`
- `TestCameraEditValidatesStoresAndRendersAManualSubstream`
- `TestCameraEditRejectsAnUnverifiedManualSubstream`
- `TestCameraEditRemovesASavedManualSubstream`
- `TestManualSubProfileRendersDistinctSubStream`
- `TestOneStreamReasonsAreSpecificAndDistinct`
- `TestOneStreamReasonComesFromTheStoredProfileResult`

## Next step

Capture the Amcrest model and firmware on the next field run, fill the matrix row, and re-check the
quirk against that firmware. This does not change the Frigate pin or the wall layout and capacity
rules.

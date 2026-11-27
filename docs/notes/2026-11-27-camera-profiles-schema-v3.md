# Camera profiles, schema version 3, and the two go2rtc keys

Date: 2026-11-27. Scope: the schema and renderer half of
[ADR 0037](../adr/0037-nvr-grid-per-pane-quality-and-full-screen.md).

## What changed

Schema version 3 adds optional `camera.profiles.main` and `camera.profiles.sub`, each with the ONVIF
profile token, the RTSP path and query, an optional snapshot path, the reported width, height and
codec. The v2 to v3 migration changes only `schema_version`; the migrated camera has no `profiles`
key and keeps the single-stream behaviour. The renderer always emits `<id>_main` and `<id>_sub` and
both friendly live mappings. With profiles the two keys use the two paths; without profiles both
keys use the one configured path. The live proxy resolves a requested quality to a key, and a sub
request on a camera with no sub profile resolves to the main key. Status reports
`distinct_qualities` per camera so the UI can tell a real substream from the same source under two
names.

## Unfinished

- Profiles are not collected yet. That is
  [BIT-027](../plan/implementation-plan.md#bit-027-robust-onvif-profiles-ptz-and-field-interoperability)
  and the ONVIF Media service.
- A camera with `credentials_ref` and profiles currently renders the one env placeholder under both
  keys. The secret value is a complete RTSP URL, so it cannot carry a second path. Status reports
  `distinct_qualities: ["main"]` for that camera rather than claiming a substream. BIT-027 needs a
  secret interface with one URL per profile before a credentialed camera can show two streams.

## Next step

Collect the advertised profiles at adoption, map `main` to the largest reported H.264 profile and
`sub` to the smallest strictly smaller one, and extend the camera secret interface so each profile
has its own URL. Then re-check the Frigate restream roles with measured evidence.

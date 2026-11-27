# Open questions

This is the register of decisions Bittern has not made, plus the record of the few decisions answered
in place in a design document. An open question is not a decision: nothing in this file authorises
an implementation choice. An answered row is a decision with a date, a link to the document that
states it, and its evidence; it authorises only what that document states. Agents must not silently
pick an answer, infer one from a ticket, or write code that hard-codes one option while the question
is open. If work needs an answer, stop and ask, or write the ADR that settles it.

How to use it:

- Every question has a stable ID, OQ-01 onward, and IDs are never reused. Reference the ID in
  commits, tickets and ADRs.
- Answer by writing an ADR. Then delete the row from the table and record it under "Resolved
  questions" with a link to the ADR. A token or component decision that
  [the design-system change classes](design/evolution.md#change-classes) record in a design document
  needs no ADR; keep its row and put the answer in the last cell.
- Update the "Next step / owner" cell as work happens, so the next agent knows where the thread is.
- Keep questions small enough to answer. If an entry has become two decisions, split it and assign a
  new ID; do not recycle old IDs.
- Review the register at every phase gate in the implementation plan. A question nobody has touched for
  two phases is either deferred on purpose or forgotten; say which.

Reading the columns:

- **Question** is the decision, phrased as a question with a single answer.
- **Why it matters** says what breaks or stalls while it stays open. **Blocking** marks work that
  cannot start, not work that is merely inconvenienced.
- **Options** is the shortlist already discussed, not an exhaustive survey. Adding an option is fine;
  deleting one needs a reason in the ADR.
- **Next step / owner** is the next concrete action and the role accountable for it, not the person
  who happens to be reading.

Blocking status, stated honestly:

- OQ-02 (agent language) was resolved on 2026-11-14 by [ADR 0015](adr/0015-agent-language.md), so it
  no longer blocks public release. OQ-01 (license) was resolved on 2026-11-14. OQ-24 (unconfigured
  recording media) was resolved on 2026-11-14 by schema version 2, which makes an empty
  `storage.recording.drives` list the explicit unconfigured state.
- Hardware-in-loop testing and the BOM are blocked by OQ-09. QEMU work can proceed without it.
- OQ-26 (touch-target floor), OQ-27 (360 or 390) and OQ-28 (screenshot retention) were answered on
  2026-11-21 and are no longer blocking. The decisions are in their register rows below and in
  [tokens.md](design/tokens.md#target-size), [review.md](design/review.md#widths) and
  [review.md](design/review.md#screenshots). OQ-29 (route coverage) has a provisional working rule
  since 2026-11-17: each UI ticket adds its routes to the manifest in the same change, and the drift
  test fails a missing route.
- OQ-30 (bulk camera add) does not block BIT-029: the sequential path is the interim path, and the
  question decides whether a batch flow is needed for the four-camera MVP. The camera step's list and
  its single review are specified as of 2026-11-21, so the open part is narrower: whether one search
  pass may adopt several selected cameras at once.
- OQ-31 (boot on a missing recording drive) was answered on 2026-11-21 by
  [ADR 0028](adr/0028-missing-recording-drive-and-boot.md). The stopgap mount options, the Frigate
  start gate and the status states are decided; BIT-012 still decides the product pool mounts.
- OQ-32 (the header destination after first run) and OQ-33 (linkable and transient states) do not
  block BIT-036, which fixes the current flow contradictions. OQ-32 decides what replaces `Setup`
  once a settings surface exists; OQ-33 decides the state-by-state linkability assignment, which
  BIT-014 declares for the operation view when it lands. Accepting ADR 0027 does not mandate either
  change.
- OQ-34 (what `payload.sha256` covers) was answered on 2026-11-22 by
  [ADR 0031](adr/0031-update-channel-on-github-releases.md). The payload is a files-only archive,
  `payload.sha256` is its digest exactly as distributed, and the manifest lists per-file digests.
  OQ-35 (delivery model and supported base) is new and does not block the release-channel or
  transaction core. It becomes blocking before the first device that is not the maintainer's.
- OQ-36 (the publish dry run and the `beta-draft` key) was resolved on 2026-11-22 by
  [ADR 0033](adr/0033-dispatch-driven-release-trigger.md). Both `beta-draft` and `beta` admit the
  `app-beta-*` tag and the `main` branch, so the dispatch release, the dry run, and the tag-push
  fallback all reach the environment they need. The temporary dry-run policy procedure is retired.
- Nothing blocks the P0 and P1 documentation, schema and skeleton work in
  [the implementation plan](plan/implementation-plan.md). OQ-19 (recording-drive redundancy),
  OQ-20 (upstream time sources), OQ-21 (audio capture), OQ-22 (device I/O boundary), OQ-38 (the
  gated advanced view) and OQ-39 (the PTZ command path) are not blocking the shipped surfaces.
  OQ-41 to OQ-43 (camera time capability, the ONVIF authentication window and the POSIX zone shape)
  do not block adoption either: [ADR 0035](adr/0035-appliance-as-time-authority.md) adopts a camera
  whether or not it accepts time, and the camera matrix supplies the evidence later.
  OQ-44 to OQ-46 (bench decode capacity, the bench camera's ONVIF profiles and the dropped-frame
  signal) do not block the live grid: [ADR 0037](adr/0037-nvr-grid-per-pane-quality-and-full-screen.md)
  ships the priors and the one-stream degradation, and the bench runs revise them.
  OQ-23 (live video path) was resolved on 2026-11-26 by
  [ADR 0034](adr/0034-live-view-owned-by-bittern.md); the kiosk question remains deferred under
  OQ-25 and does not block v1. The renderer uses provisional defaults where it needs an answer.

## Register

| ID | Question | Why it matters | Options | Next step / owner |
| --- | --- | --- | --- | --- |
| OQ-04 | Which OS A/B update mechanism? | Update safety and rollback are core promises; the choice shapes the image and signing chain. | systemd-sysupdate (in-tree, signed, simple); RAUC (bundle format, bootloader integration); Mender (hosted control plane). | Prototype sysupdate on a QEMU image, compare with RAUC; ADR; owner: image. The proposed [ADR 0030](adr/0030-ab-from-the-first-image.md) assumes A/B from the first image but leaves the mechanism here; this question stays open until the image ADR is accepted. The delivery model and supported base are [OQ-35](#register). |
| OQ-05 | Is A/B present from day one, or added later? | Effort versus bricking risk; P3 exit criteria depend on it. | A/B from the first image; single slot with USB recovery first. | Decide together with OQ-04; owner: maintainer. The proposed [ADR 0030](adr/0030-ab-from-the-first-image.md) proposes A/B from the first image; this question stays open until the ADR is accepted. The delivery model and supported base are [OQ-35](#register). |
| OQ-06 | Disk encryption, secure boot and TPM? | Footage and secrets at rest; TPM ties an image to hardware and complicates recovery and resale. | LUKS with TPM2 unlock; LUKS with passphrase; no encryption in v1; secure boot via shim. | Threat-model addendum to ADR 0010; owner: security. |
| OQ-07 | Do we support Frigate+ models and buy a commercial license? | Better models and funding for upstream, but the terms restrict customer-site deployment and hardware bundling. | No Frigate+; owner-supplied key only; commercial license for a paid SKU. | Ask Frigate, Inc. directly; decide before any paid SKU; owner: maintainer. |
| OQ-08 | Do we ship a Home Assistant add-on? | Reaches HA users, but needs Full Access and a different config path, and splits the test matrix. | No add-on in v1; document the community add-on; first-party add-on later. | Revisit after P4; owner: maintainer. |
| OQ-09 | Which hardware SKU and BOM? | **Blocking** for HIL testing, driver and thermal validation, PoE budget and price. | N100 mini-PC, dual 2.5GbE, 8-port PoE switch, 4 TB HDD, UPS; N305 for more cameras. | Buy one reference unit, publish the BOM; owner: maintainer. |
| OQ-10 | Which cameras are supported? | Field success depends on quirks (gateway requirement, NTP option 42, ONVIF auth); support load scales with the list, and a claim without evidence is guesswork. | A tested matrix with quirks and the manual fallback; community reports only; no list. | Maintain the [camera matrix](notes/camera-matrix.md) from BIT-008 probe data and field runs: record model, firmware, discovery layer, auth, profile and RTSP path, and turn each quirk into a row and a test. [ADR 0024](adr/0024-onvif-discovery-levels.md) makes manual address plus RTSP the baseline. The POC slice already supplies address, MAC, the ONVIF name scope and layer results; profile and media evidence remain open. Owner: QA. |
| OQ-11 | Is there a multi-site or fleet control plane? | Decides whether Bittern is one appliance or a product line with hosted operations. | Per-device only; signed bundle pull from CI; hosted control plane. | Defer to P4; revisit with tailnet-based support; owner: maintainer. |
| OQ-12 | What are the telemetry and update-check defaults? | The privacy promise versus update awareness; upstream version check is on by default. | All off with manual checks; update-check only against our server; opt-in diagnostics. | Install policy is proposed in [ADR 0021](adr/0021-security-updates.md). The on-demand channel check and manual rollback landed in [the update channel and rollback slice](notes/2026-11-22-update-channel-and-manual-rollback-slice.md); automatic periodic checking, its default interval, and telemetry remain open. Decide with the privacy ADR; owner: security. |
| OQ-13 | What support and warranty model? | Determines docs, remote access, reset design and price; also who answers the phone. | Best-effort community; paid support; reseller warranty; none. | Define before the first handoff; owner: maintainer. |
| OQ-14 | Is the name and domain clear? | Needed for repository, docs, registry, signing identity and any trademark filing. | `bittern.dev`, `bittern.io`, `bittern.org`; alternate bird names if taken. | Search trademarks and registries; owner: maintainer. |
| OQ-15 | Are 8554 (RTSP restream) and Home Assistant access exposed by default? | Convenience versus attack surface; HA integration expects reachable streams. | Closed by default; open on LAN; open on tailnet only. | The BIT-003 renderer closes 8554 by default; decide before exposing it, owner: network. |
| OQ-16 | How are recordings backed up off the appliance? | The recording pool has no redundancy and v1 has no backup path ([ADR 0014](adr/0014-dedicated-recording-drive.md), [ADR 0018](adr/0018-recording-capacity-pool.md)); a failed member loses its share, and losing the pool loses all footage, but cloud breaks local-first. The primary recording target is decided; this question covers backup only. | Local USB or NAS copy target; optional S3-compatible target; both. | Decide after P2; owner: maintainer. |
| OQ-17 | Localization and accessibility scope? | Non-technical owners may not read English; accessibility is a quality bar, not a feature. | English only in v1; i18n framework from the start; a11y audit before P4. | Decide localization and the wider accessibility scope, then record it in a follow-up ADR; baseline accessibility is fixed by [ADR 0019](adr/0019-ui-stack.md); owner: frontend. |
| OQ-18 | What is the target price? | Drives BOM, license, support model and whether Frigate+ is viable. | At cost; 300-500 USD; 800 USD and up with support. | Estimate from the BOM after OQ-09; owner: maintainer. |
| OQ-19 | Does the recording pool need redundancy? | **Not blocking.** Capacity scaling is decided by [ADR 0018](adr/0018-recording-capacity-pool.md); redundancy is not. The pool has no redundancy, so a failed member loses its share of footage, and mirroring adds cost and complexity and does not replace backup. | Keep the non-redundant pool; mirror a second recording drive; external backup target. | Revisit after field data; owner: maintainer. |
| OQ-20 | Which upstream time sources should chrony use on `lan0`? | NTP is an unsolicited outbound flow in the default path, and schema version 2 has no time-source field. A public pool has privacy and availability trade-offs. | Public pool (`pool.ntp.org`, the provisional renderer default); NTP servers supplied by DHCP on `lan0`; a vendor-operated set; owner-supplied addresses. | Decide with the privacy and egress review; owner: runtime. The appliance half of ADR 0035 renders `bindacqdevice lan0` with `pool pool.ntp.org iburst` as the provisional source set, so a future choice changes the rendered source lines and the golden renders, not the serving model. |
| OQ-21 | Should the appliance capture camera audio, and under what privacy posture? | **Not blocking.** Audio can capture private conversations and changes retention, data flow, and the privacy page. | No audio in v1; off by default with explicit enablement and a data-flow note; on by default. | Write the privacy and data-flow note from [ADR 0010](adr/0010-security-and-privacy-posture.md) before any audio field ships; owner: security. |
| OQ-22 | Does `bitternd` own relays, alarm inputs, and audio, and how do Frigate events reach them? | **Not blocking.** Device I/O needs one privileged boundary; wiring Frigate directly to GPIO or relays would bypass the renderer and the trust model. Hardware choice depends on OQ-09. | `bitternd` owns device I/O; a separate privileged helper; direct Frigate integrations or MQTT. | Decide with OQ-09 and write an ADR; see [extension points](notes/2026-11-14-extension-points.md); owner: runtime. |
| OQ-25 | Should the appliance offer a local kiosk UI on an attached display? | **Deferred, not decided; not blocking.** The idea is a local full-screen view beside the network UI, but it adds a second session and auth path and depends on live view, a display choice and a recovery design. Current thinking: reuse the existing Go template and HTMX UI through a loopback TLS endpoint, with a kiosk principal, a local credential or PIN, and an explicit admin unlock, not a UI fork. | No local session in v1; a kiosk session reusing `bittern-ui`; a separate kiosk UI. | Deferred until after the v1 hardware gate; live view is decided by [ADR 0034](adr/0034-live-view-owned-by-bittern.md), but choose the display and write an ADR before implementation. See [kiosk mode note](notes/2026-11-15-kiosk-mode.md); owner: frontend. |
| OQ-26 | What is the minimum touch-target size for the Bittern UI? | BIT-020 says only "touch-friendly", so the browser check cannot pass or fail a control and the same layout work may be done twice. WCAG 2.2 AA SC 2.5.8 sets 24 by 24 CSS pixels as the minimum; Apple and Material guidance use 44 pt and 48 dp; a 44 pixel floor changes row heights and control spacing on every page. | 24 by 24 CSS pixels, the WCAG 2.2 AA floor; 44 by 44 CSS pixels, common practice and stricter for touch; 24 as the hard floor with 44 for primary actions; no numeric floor, review only. | **Answered 2026-11-21, by role.** 44 by 44 CSS pixels for navigation, action rows and any whole-row target; 24 by 24 as the hard floor for every other interactive control, including a link inside a sentence, which WCAG 2.2 AA SC 2.5.8 exempts and this system still holds to 24. Hit area counts, visual size does not. [tokens.md](design/tokens.md#target-size) states the rule with `--touch-target` and `--touch-target-min`, and [components.md](design/components.md) states it per component. Evidence: the warning set at the provisional 24 floor on 2026-11-21, where every warning was a target below 44 and the flagged links were single-line text links at 27 to 32 high while the candidate radio was 24 by 24; the count is in the evidence note below rather than in this cell. History: [ADR 0025](adr/0025-browser-check-for-the-responsive-ui.md) provisionally failed below 24 and warned below 44, and the screens were measured on 2026-11-21. Owner: frontend. |
| OQ-27 | Is the responsive scope 360 or 390 CSS pixels wide? | BIT-020's scope says the layout works from a 360 CSS pixel phone, its acceptance and the UX-8 principle measure at 390 by 844, and the [UI README](../ui/README.md) says 360 to 390. A check at 390 alone can pass a layout that breaks at 360, so a green run at one width does not prove the stated scope. The browser check measured both widths; the acceptance width was the open part. | Check 390 only, matching acceptance; check 360 only, matching scope; check both widths; keep 360 as the design floor and 390 as the acceptance width. | **Answered 2026-11-21.** 360 CSS pixels is the supported minimum and a floor that must pass. Both 360 and 390 are acceptance widths; the check measures 360 by 800, 390 by 844, 1280 by 800 and 1920 by 1080. [review.md](design/review.md#widths) states the four widths and their roles, and [patterns.md](design/patterns.md#cross-pattern-rules) carries the per-screen rule. Evidence: the 2026-11-17 run checked 51 screens at three widths and found 5 failures at 360, all on the status action row, fixed in `ui/static/bittern.css`; the re-run passed 51 screens at 360, 390 and 1280 with 0 failures, and the four-width run measured 68 route-width screens. 360 sits below every layout breakpoint (38rem, 44rem, 60rem), so neither phone width is a midpoint sample. History: [ADR 0025](adr/0025-browser-check-for-the-responsive-ui.md) provisionally measured 390 only, and 360 was added on 2026-11-17. Owner: frontend. |
| OQ-28 | Are browser-check screenshots committed, gitignored build artifacts, or only linked? | BIT-020 requires a screenshot per screen and says the screenshots are checked in with the browser test or linked from the UI README. A binary per route and width on every change is repository weight and review noise; an artifact that is not kept cannot be compared later, and a link can rot. | Commit them under `tests/browser/screenshots/`; keep them as gitignored artifacts under `build/`; commit none and link a per-run artifact directory; commit a curated set per release. | **Answered 2026-11-21.** Per-run images stay gitignored working artifacts under `build/ui-screenshots/`, and a curated set is attached to each gate result or release, not committed per change. [review.md](design/review.md#screenshots) states the retention rule and where the curated set goes. Evidence: a per-run set is 68 default full-page images (17 screens from 13 routes at four widths) plus an `-expanded` copy for each screen with a closed disclosure, and `/build/` already covers the directory; [tests/browser/README.md](../tests/browser/README.md) records how the images are written. History: BIT-020 landed on 2026-11-17 and wrote the gitignored directory, which was the de facto option, and no screenshot has been committed. Owner: maintainer. |
| OQ-29 | How do later UI tickets extend the browser check route list? | Updates, logs, support, reset and live view do not exist yet, so the check cannot cover them, and BIT-022, BIT-023, BIT-024, BIT-025, BIT-030 and BIT-031 all cite the BIT-020 checks. Without a rule each ticket either adds routes ad hoc or silently stays uncovered. | Every UI ticket adds its routes to the manifest in the same change; a separate coverage ticket per surface; the check derives routes from the UI README route table; no extension until the surface exists. | Answered provisionally on 2026-11-17: every UI ticket adds its routes to the [browser-check manifest](../tests/browser/routes.json) in the same change, and the manifest drift test fails a missing route instead of letting it stay unnoticed. This is a working rule, not a review decision, so it stays provisional; owner: frontend. |
| OQ-30 | Is bulk camera add in scope for v1, and if so what is the batch flow? | The [Level 2 hardware run](notes/2026-11-17-level2-hardware-run.md) reported that the one-camera-at-a-time flow will not support adding many cameras at once. The MVP asks for four cameras in under 30 minutes, and a batch flow changes discovery, credential collection, reservation writing, review, apply atomicity and failure handling. | Sequential add from the listing ([BIT-029](plan/implementation-plan.md#bit-029-multi-camera-inventory-and-add-from-listing)); select several discovered cameras and adopt them in one batch; import a list of addresses and credentials; defer bulk add until measured evidence shows sequential add misses the budget. | Measure the sequential add time after BIT-029, then decide against the four-camera MVP budget; owner: frontend. The camera step's list and one Review are specified on 2026-11-21 ([flows.md](design/flows.md#the-camera-step)), so only the arity of one search pass remains open; see the evidence note below. |
| OQ-32 | Which destination replaces `Setup` in the header after the first run completes? | `Setup` stays in the header after setup completes and is the only route to change configuration. [ADR 0027](adr/0027-flow-layer-above-patterns.md) says a completed flow is absent from the header (N5), and a rename with no settings surface would relabel the first-run wizard instead of replacing it. | Keep the entry and rename it `Settings` when a settings surface exists; hide it until a settings surface exists; leave it as it is and record the intent. | Leave the header unchanged for now and let [BIT-036](plan/implementation-plan.md#bit-036-first-run-and-flow-contradictions-from-the-flow-layer) own the rename and the settings surface; see the note below. Owner: frontend. |
| OQ-33 | Which UI states must be stable URLs, and which are transient? | The answer decides what a bookmark or refresh returns and whether a future HTMX swap can change state without a URL. F8 defines three classes but not every state's assignment; the current table marks setup conditional and camera remove object scoped, while the apply operation view has no route yet. | Status, camera list and camera edit stable, with setup, review, apply and remove transient; status and camera list stable only, with every object-scoped screen transient; every GET-rendered state stable, including the wizard steps encoded in the URL. | Decide with the operation view before any HTMX swap replaces a state; see the note below. Owner: frontend. |
| OQ-35 | What is the supported delivery model and base? | The image is not yet required, the packaging install path is a bridge, and the first device that is not the maintainer's needs a reproducible supported base. The choice decides packaging, support, recovery, and whether the image is the only release artifact. | The mkosi image is the only supported delivery; Debian install plus the payload remains a maintainer-only path; the image becomes required after the bridge; another supported base. | Keep [ADR 0007](adr/0007-mkosi-os-image-and-releases.md) in force and treat `packaging/install.sh` as a bridge, not product policy. The image becomes required before the first device that is not the maintainer's. Decide the delivery model and supported base before that trigger; owner: maintainer/image. |
| OQ-37 | How should an administrator session survive a `bitternd` restart? | An update stops `bitternd.service` before activation and restarts it after activation, migration, and the optional image ensure ([engine.go](../agent/internal/update/engine.go):340,347). Sessions are an in-memory map ([auth.go](../agent/internal/auth/auth.go):93,129), and `/var/lib/bittern/auth.json` holds only the password record ([auth.go](../agent/internal/auth/auth.go):158-182,241). Every browser is signed out by an update, and the status fragment then swaps the login page into the panel unless BIT-046 lands. | Persist the existing server-side session map under `/var/lib/bittern/sessions.json` (mode 0600, atomic write, prune on load); persist a signing key under `/var/lib/bittern` and use signed cookies; accept re-login and make the fragment redirect safely; persist a separate refresh token and re-establish the session on a 401. | [BIT-045](plan/implementation-plan.md#bit-045-keep-the-administrator-session-across-a-daemon-restart) is blocked until this is answered. The smallest reversible default is to persist the existing server-side sessions to `/var/lib/bittern/sessions.json`, mode 0600, written atomically, pruned on load, and cleared on logout, password change, setup, and reset. It keeps the current 12 hour absolute lifetime and opaque ids, and adds no signing key. Treat that as provisional until reviewed; owner: security and runtime. |
| OQ-38 | Should a gated advanced view proxy Frigate's own UI for the long tail? | **Not blocking, no default chosen.** The Bittern live view and native screens will not cover every Frigate setting and diagnostic. A proxy would reach the long tail, but it adds a second UI and authentication behavior, couples Bittern to Frigate's frontend and release cadence, and risks a second writer to `config.yml`. Not proxying leaves some settings as a support task. | No advanced view, with the tailnet or console Frigate UI as the documented support path; a gated, read-only advanced view proxy on a separate host and session, with no config writes; a curated set of native Bittern screens for named long-tail settings; a tailnet-only Frigate UI link with a support procedure. | Decide after BIT-023 to BIT-026, using evidence from the support path. Any proxy needs a new ADR that supersedes the exposure clause of [ADR 0016](adr/0016-bittern-owns-frigate-config.md) and preserves the one-writer rule. See the note below. Owner: frontend and security. |
| OQ-39 | How does Bittern send PTZ commands to a camera? | **Not blocking, no default chosen.** Frigate's documented movement commands are MQTT on `frigate/<camera_name>/ptz`, and product config sets `mqtt.enabled: false`. The HTTP API exposes `GET /api/<camera_name>/ptz/info` for capability and presets, not movement. go2rtc has no PTZ API. A continuous move without a verified stop path can leave a camera moving. | Enable a local MQTT broker behind Frigate and use its dispatcher; add an ONVIF client in `bitternd`; use a future documented Frigate HTTP PTZ command; ship no PTZ. | [BIT-027](plan/implementation-plan.md#bit-027-robust-onvif-profiles-ptz-and-field-interoperability) decides with a real PTZ camera, firmware and stream topology. The docs do not promise that a camera behind go2rtc accepts ONVIF commands. See the note below. Owner: runtime and frontend. |
| OQ-40 | Should the live grid use a wider page measure than the 68rem reading shell? | [ADR 0034](adr/0034-live-view-owned-by-bittern.md) reasons about tiles about 400 pixels wide at 1280 and wider at 1920, while the shared page shell capped every page at 68rem. The grid fits three columns either way; at 1920 the tiles stayed the 1280 width. | Keep 68rem everywhere; add a live-only wide maximum as a layout token; widen the shell for every page. | **Answered 2026-11-26: widen the shell for every page.** The page shell fills the device width with only `--layout-gutter` inset, and `--layout-gutter-wide` at 80rem and above. The 68rem page maximum token is removed. Prose keeps `--measure-prose`, hints keep `--measure-hint`, and controls keep `--measure-control`, so a wide screen adds room to the live grid, status panels and tables rather than to sentences. Evidence: [the full-width shell and landing note](notes/2026-11-26-full-width-shell-and-landing.md); the browser check asserts the shell fills 1280 and 1920 and that prose stays inside its measure; the four-width screenshots show the live grid, status, wizard and login pages. Owner: frontend and maintainer. |
| OQ-41 | Which cameras support NTP mode, pushed time, both, or neither, and which honour DHCP option 42? | The time policy in [ADR 0035](adr/0035-appliance-as-time-authority.md) depends on per-camera capability, and the camera matrix has no tested product row. A camera that supports neither is adopted but needs its clock set by hand. | A capability matrix maintained from field runs; a vendor list; a manual per-camera setting. | Record the time mechanism, the NTP result, the pushed result and the evidence for every row in [the camera matrix](notes/camera-matrix.md), starting with the Axis reference device in BIT-027. Owner: QA. |
| OQ-42 | What clock skew do cameras accept on authenticated ONVIF requests, and must the header always use the camera's clock? | A camera that is hours off rejects a timestamp from the appliance, so the first authenticated time write may fail before it can fix the camera. The ten second Axis figure is a client library report, not vendor documentation. | Timestamp the first request from the camera's unauthenticated time read; timestamp from the appliance and rely on a normal window; a per-model offset rule. | Measure the accepted window on the reference camera in BIT-027, and keep the camera-clock timestamp as the provisional default until then. Owner: runtime. |
| OQ-43 | Do cameras accept a fixed-offset POSIX time zone string, and do they apply DST from the `TimeZone` field? | The ONVIF `TimeZone` is a POSIX string, not an IANA name. The push path sends the offset in force now and re-pushes at a transition; that only works if cameras accept a string with no DST rule. | Fixed-offset string with re-push; a generated DST rule string; send no zone and rely on the camera's own zone. | Test both push shapes on the reference camera and a second vendor in BIT-027. Owner: runtime. |
| OQ-44 | What live decode capacity do the bench devices actually sustain? | [ADR 0037](adr/0037-nvr-grid-per-pane-quality-and-full-screen.md) sets class priors of 4, 8 and 16 live sub-stream panes from pixel arithmetic, not measurement. A prior that is too high overloads a device before the dropped-frame signal can react; one that is too low holds a capable device back. | Keep 4, 8 and 16; lower the phone and tablet priors; raise the phone prior and let runtime demotion find the limit. | BIT-023 records a bench run at 4, 8 and 16 sub-streams with the dropped-frame counters, CPU, memory, heat and the visible stutter, and the priors are revised in a dated note. Owner: runtime and frontend. |
| OQ-45 | Does the bench camera expose a sub stream through ONVIF, and which Media profiles does it advertise? | The schema profiles and the two-stream renderer are only useful if the camera advertises a second profile. The QEMU camera has no ONVIF at all, so no fixture proves the path today. | One profile; a main and a sub profile; several profiles with duplicate resolutions. | BIT-027 runs `GetProfiles`, `GetStreamUri` and `GetSnapshotUri` on the reference camera, records the token, resolution and codec of each profile in [the camera matrix](notes/camera-matrix.md), and the no-profile fallback is retested. Owner: runtime. |
| OQ-46 | Does `droppedVideoFrames` on the MSE path track decoder overload closely enough to drive demotion? | The capacity rule depends on the browser counter. It may count compositor drops rather than decoder drops, and a browser without the API cannot demote at all, so the prior would stand on an overloaded device. | Trust the counter with the 2 percent and 0.5 percent thresholds; add a frame-arrival or buffer-health signal from the player's own MSE pipeline; no automatic demotion. | BIT-023 logs the counter, the buffer health and the visible stutter on the bench run, and the signal or the thresholds are adjusted with the evidence. Owner: frontend. |
**OQ-37 session continuity.** The default above is deliberately the smallest change: it reuses the
existing session records and cookie format, adds one mode 0600 file under the state root that already
holds `auth.json`, and does not introduce a second key ceremony. The alternatives move the trust to a
signed cookie or a refresh token, which needs key rotation and invalidation rules. Whichever option is
chosen, a restart must not revive a logged-out or expired session, and no session id, CSRF token, or
session file content may appear in status, logs, or the support bundle. The evidence for the question
is in [the live refresh state loss note](notes/2026-11-26-live-refresh-state-loss.md).

**OQ-38 advanced view.** The proxy is not chosen. A read-only proxy of Frigate's UI would give the
owner the long tail without a shell, but the Frigate UI is built around Frigate's own concepts and
writes its own configuration. Serving it under the Bittern session would put a second frontend and a
second configuration path behind the administrator login. A read-only mode would keep the one-writer
rule but still add the whole UI surface, its assets, its WebSocket traffic and its version coupling.
A curated native screen is smaller but slower to cover the tail. Doing nothing keeps the support path
and leaves some settings out of the product UI. Any of these is a real trade-off; none is the
default. The question is recorded so the tail is not quietly declared out of scope.

**OQ-39 PTZ command path.** The current product config disables MQTT, so the documented Frigate PTZ
path is unavailable. A local broker adds a service to the image and another process to secure; an
ONVIF client in `bitternd` adds protocol code that Frigate already has; a future HTTP endpoint would
be the smallest option if upstream ships one; no PTZ is the safe default until a camera proves the
behavior. The decision belongs with the real camera in BIT-027, not with the live view ADR. The
safety rules for continuous move and stop are in
[ADR 0034](adr/0034-live-view-owned-by-bittern.md).

**OQ-40 live grid measure.** Answered on 2026-11-26. The live grid is the first screen whose reason
for a width is the video wall rather than prose, and the 68rem cap made the 1920 wall no wider than
the 1280 one. The shell now fills the device width at every page, with the layout gutter as the only
inset. The reading measure moves to the content that needs it: `--measure-prose` for paragraphs,
`--measure-hint` for help text, and `--measure-control` for form controls. A future screen that needs
a narrower column sets its own inline measure instead of inheriting a page maximum. The evidence is
the four-width browser run and the screenshots `live-<width>.png`, `live-nine-tiles-<width>.png`,
`status-ready-<width>.png`, `wizard-<width>.png` and `login-<width>.png`, plus
[the full-width shell note](notes/2026-11-26-full-width-shell-and-landing.md).

**OQ-41 to OQ-43 camera time capability.** [ADR 0035](adr/0035-appliance-as-time-authority.md) fixes
the mechanism order and the safety rules, but not what a given camera does. OQ-41 is the capability
matrix; OQ-42 is the authentication window that decides whether a badly skewed camera can be fixed at
all; OQ-43 is the POSIX zone shape that the push path sends. None blocks the adoption path, because a
camera is adopted whether or not it accepts time. All three need the Axis reference device and at
least one other vendor before the matrix can claim a supported pattern.

**OQ-10 reference device.** The reference device for
[BIT-027](plan/implementation-plan.md#bit-027-robust-onvif-profiles-ptz-and-field-interoperability) is
a recent Axis PTZ, used for profile negotiation, PTZ presets and continuous move, home and stop,
snapshot, events, and field quirks. Its model and firmware are added to the tested-device matrix under
this question when the device is tested, with its services, profiles, authentication, and each quirk
and the evidence for it. No credential, serial number, MAC address, or site identifier is recorded.
Until that row exists the model is a candidate, not a supported camera.

**OQ-26 evidence.** On 2026-11-21 the browser-check warning list was read for this question. The
warning list is all text links 27 to 32 CSS pixels high; the candidate radio is 24 by 24 and passes
the floor. The check reports 0 failures at the provisional 24 by 24 floor. That set is the evidence
for the by-role answer: the flagged links are the inline and row links the rule distinguishes, so an
inline link stays at 24 while a navigation or action target moves to 44. The count is not a
constant: the 2026-11-21 design-conformance audit recorded 216 warnings and the review register
records 228 for the 2026-11-17 width commit, so this note cites the dated set rather than a current
figure.

**OQ-27 evidence.** On 2026-11-17 the browser check added 360 by 800 to its existing 390 by 844
and 1280 by 800 viewports. At two widths it had passed 34 route-width screens with 0 failures and
114 warnings, 57 per width. At three widths the first run checked 51 screens and found 5 failures at
360, all on the status routes: the three action links wrapped inside one inline link box, so the
centre of "Back to setup" landed in the gap between its two line boxes and was reported as covered
by the parent paragraph. `ui/static/bittern.css` now lays that row out as a wrapping flex row, and
`ui/templates/status.html` gives the row its class. The re-run passed 51 screens with 0 failures and
171 warnings, 57 per width; the 44 pixel warning set did not grow. That run is the evidence for the
answer: 360 passes, so it is the supported minimum and not a design-only floor, and both phone
widths are acceptance widths. The layout breakpoints are 38rem, 44rem and 60rem, so neither width
is a midpoint sample; the four-width run now measures 68 route-width screens and both phone widths
must pass. [review.md](design/review.md#widths) records the budgets.

**OQ-28 evidence.** As of 2026-11-21, BIT-020 has landed and `make ui-browser-check` writes one
full-page screenshot per route and width to gitignored `build/ui-screenshots/`, with no screenshot
committed. A run is 68 default images from 17 screens and 13 routes at four widths, plus an
`-expanded` copy for each screen with a closed disclosure. That set is the per-run working artifact
the answer keeps out of git; the curated set for a gate result or release is the durable record.
[review.md](design/review.md#screenshots) states the retention rule.

**OQ-30 evidence.** On 2026-11-21 the maintainer decided the camera step's shape: the cameras added
so far are listed at the top of the step, discovery and manual entry add one more, and one review
applies them all. That settles where cameras accumulate and how they are committed, and it settles
that removing one from the list is not the destructive remove. It does not settle how many cameras
one search pass may adopt: a candidate-by-candidate add (today's shape), selecting several
discovered cameras and collecting credentials for each in one pass, or importing a list of addresses
and sign-ins. All three fit the specified destinations, because they differ inside the add
destination and in what one add commits, not in the step's list or its one review. The measurement
that decides is still the four-camera time budget in the register row. Evidence:
[the step's specification](design/flows.md#the-camera-step) and
[its note](notes/2026-11-21-multi-camera-step-spec.md).

**OQ-32 navigation destination after first run.** `Setup` stays in the header after the first run
completes and is the only route to change configuration
([base.html](../ui/templates/base.html), line 18; [flows.md](design/flows.md#the-setup-defect)).
[ADR 0027](adr/0027-flow-layer-above-patterns.md) makes the navigation model binding, and N5 says a
completed flow is absent from the header, so leaving the entry is a known contradiction rather than
an undecided label.

Options: keep the entry and rename it `Settings` when a settings surface exists, with
[BIT-036](plan/implementation-plan.md#bit-036-first-run-and-flow-contradictions-from-the-flow-layer)
owning the route and the surface; hide the entry after the first run completes, with reconfiguration
staying a reset ([ADR 0017](adr/0017-reset-levels-and-decommissioning.md)); leave it as it is and
record the intent that the first-run wizard remains the only route to change configuration.

Recommended: leave the header unchanged for now and let BIT-036 own the rename, so accepting
ADR 0027 does not mandate an undesigned navigation change. A settings surface is not designed, and
renaming the entry to `Settings` while it opens the first-run wizard would relabel the defect
instead of fixing it.

**OQ-33 linkable and transient states.** F8 classifies a state as persistent, object scoped or
transient, and the table in [flows.md](design/flows.md#linkability) records the current assignment.
The assignment decides what a bookmark or refresh returns and what a future HTMX swap may replace
without a URL. The current table marks `/ui/setup` conditional and
`/ui/cameras/{id}/remove` object scoped, while the apply operation view has no route yet
([BIT-014](plan/implementation-plan.md#bit-014-long-running-operation-progress)). No HTMX swap
changes a state today; the status fragment is the only fragment.

Options: stable for status, the camera list and a camera edit page, with setup, review, apply and
remove transient; stable for status and the camera list only, with every object-scoped screen
transient; every GET-rendered state stable, including the wizard steps, with the session encoded in
the URL.

Recommended: the first. Status and the camera list render from appliance state, and a camera edit
page is worth a bookmark while the camera exists. Setup, review, apply and remove depend on a
session, a one-shot result or a destructive commit, so they redirect with an explanation when their
state is gone and are never the only record of a result. A future HTMX swap must keep a stable URL
for a persistent state and must not silently replace a transient one.

## Dependencies between questions

Some questions cannot be answered well in isolation. Answer the left side first, or answer both in one
ADR:

| Pairing | Why they travel together |
| --- | --- |
| OQ-04 then OQ-05 | The A/B mechanism determines whether day-one A/B is cheap or expensive. |
| OQ-09 then OQ-06 | TPM and disk encryption depend on the chosen board and firmware. |
| OQ-09 then OQ-18 | Price follows the BOM, not the other way round. |
| OQ-12 then OQ-13 | Support access and telemetry share the same remote path and privacy story. |
| OQ-12 then OQ-20 | The telemetry and egress promise constrains whether a public NTP pool is acceptable. |

Owner roles are placeholders until the project has named people: maintainer (product and release),
security, frontend, image, runtime, network, QA, release. If a role has no owner, the maintainer owns
it by default and should say so in the next review.

## Resolved questions

Resolved questions graduate to an ADR and are listed here for history: the question, the decision, a
link to the ADR, and the date. They keep their ID and are not renumbered. The process itself is
recorded in [ADR 0001](adr/0001-adopt-architecture-decision-records.md).

| ID | Question | Resolution | Decision | Date |
| --- | --- | --- | --- | --- |
| OQ-01 | Which license does Bittern use? | MIT for all original Bittern code and documentation. No Apache-2.0, no CLA; contributions are inbound under MIT; a DCO is optional and not adopted; the Bittern name and logo are not licensed under MIT. | [ADR 0013](adr/0013-branding-and-licensing-constraints.md), [LICENSE](../LICENSE) | 2026-11-14 |
| OQ-02 | Which language for `bitternd` and the `bittern` CLI? | Go for `bitternd` and the `bittern` CLI, statically linked binaries built from vendored modules. | [ADR 0015](adr/0015-agent-language.md) | 2026-11-14 |
| OQ-03 | Which UI stack for `bittern-ui`, and how does live video reach the browser? | Server-rendered Go `html/template` with HTMX, embedded in `bitternd`, TLS by default. The live-video half was split out as OQ-23 and resolved by ADR 0034. | [ADR 0019](adr/0019-ui-stack.md), [ADR 0034](adr/0034-live-view-owned-by-bittern.md) | 2026-11-14; live view 2026-11-26 |
| OQ-23 | How does live video reach the browser? | **Resolved 2026-11-26.** Live view is a Bittern page on the authenticated 443 surface. MSE is the primary transport, HLS is the fallback, and WebRTC, 8555, go2rtc 1984 and Frigate 5000/8971 stay off the LAN and tailnet. The player is never inside an HTMX swap region. | [ADR 0034](adr/0034-live-view-owned-by-bittern.md) | 2026-11-26 |
| OQ-24 | How does the schema represent an appliance with no recording drive? | Schema version 2 requires `storage.recording.drives` and defines an empty list as the explicit unconfigured media state. Version 1 documents migrate before validation. | [ADR 0018](adr/0018-recording-capacity-pool.md), [schema v1 to v2](../schema/migrations/README.md) | 2026-11-14 |
| OQ-31 | May a missing recording drive block boot? | **Answered 2026-11-21.** No. Boot completes and the UI reports the fault, but Frigate does not start and recording never falls back to the OS disk. The media fstab line gains `nofail` and a bounded `x-systemd.device-timeout`; the Frigate unit uses `RequiresMountsFor` and an `ExecStartPre` mountpoint check; status distinguishes not attached, attached but not mounted, and mounted but failing its write test; `storage-setup.sh` rewrites existing fstab lines, so the change migrates on its next run. The history is in the note below. | [ADR 0028](adr/0028-missing-recording-drive-and-boot.md) | 2026-11-21 |
| OQ-34 | What bytes does `payload.sha256` cover? | **Answered 2026-11-22.** The payload is a files-only archive, and `payload.sha256` is the SHA-256 of that archive exactly as distributed. `manifest.json` and `manifest.json.sig` are separate release assets, not archive members. The manifest's `files` list carries per-file size, SHA-256 and mode for the extracted tree, so both transport and installed content are checked. The rule does not depend on the delivery model. | [ADR 0031](adr/0031-update-channel-on-github-releases.md), [release signing tool note](notes/2026-11-21-release-signing-tool.md) | 2026-11-22 |
| OQ-36 | How should the publish dry run reach the `beta-draft` key without a temporary branch policy? | **Answered 2026-11-22.** Both `beta-draft` and `beta` admit the `app-beta-*` tag and the `main` branch. A dispatch at `main` carries `refs/heads/main` for the signing and approval jobs, so the branch policy admits them; the tag-push fallback still uses the tag policy. The temporary dry-run policy procedure is retired, and the no-secret dry-run environment is not needed for the release path. | [ADR 0033](adr/0033-dispatch-driven-release-trigger.md) | 2026-11-22 |

Frigate+ support remains open as OQ-07; the MIT decision does not settle it.

### OQ-31 history

**Boot hazard, recorded and answered on 2026-11-21.** Before ADR 0028,
`packaging/storage-setup.sh` wrote
`UUID=<uuid> /var/lib/bittern/media ext4 defaults,noatime 0 2`, with no `nofail`, and the
[POC runbook](runbooks/gk41-poc.md#prepare-the-recording-drive) forbade `nofail`. When the drive was
present at install and was missing at the next boot, `local-fs.target` failed; on a headless
appliance the expected result was emergency mode with no UI. That boot behaviour was inferred from
the fstab line on 2026-11-21, not measured on the GK41. `install.sh` continued with a warning when no
eligible drive was found, so an install with no drive wrote no fstab line and still booted.

Consequences: the owner lost the UI and the status page at the moment storage failed, so a cable,
enclosure or power fault became a console visit. Frigate cannot start without media, which is
correct, and recordings must not fall back to `/`, because filling the OS drive breaks updates and
the appliance ([ADR 0014](adr/0014-dedicated-recording-drive.md),
[ADR 0018](adr/0018-recording-capacity-pool.md)).

Options: keep the hard fstab failure and accept emergency mode; use a systemd mount unit with
`nofail`, an explicit degraded storage state and a Frigate start gate such as `RequiresMountsFor` or
a mount-source check, so boot continues but recording does not start on the OS disk; or fall back to
a mount on the OS disk. The fallback was rejected because it violates ADR 0014's separation and
silently fills the OS drive, and it hides the fault.

Recommended at the time: the mount unit with `nofail`, the degraded storage state and the Frigate
start gate, after measuring the boot and gate behaviour on the GK41. That option changes the
runbook's blanket no-`nofail` rule and the reading of ADR 0014's "must be mounted at boot".
[ADR 0028](adr/0028-missing-recording-drive-and-boot.md) records that decision; this note keeps the
history of the question.

Do not leave a resolved question in both places. A question resolved by an ADR moves here; a row
answered in place under the design-system change classes stays in the register with its answer and
is not copied here. Keeping both copies guarantees they diverge.

## How to answer a question

1. Timebox the investigation. If the answer needs more than a day of evidence gathering, split the
   question or choose a reversible interim option and record it as such.
2. Gather evidence, not preference: an upstream document, a spike result, a costed BOM, a legal term,
   or a test on real hardware. Link it in the ADR.
3. Write the ADR in `docs/adr/`, using the next free number. State the decision, the alternatives, the
   consequences and what would make us revisit it.
4. Move the row: delete it from the register above and add a line with the ADR link and the date.
   When [the design-system change classes](design/evolution.md#change-classes) record the decision in
   a design document instead, keep the row and put the answer, the date and the evidence in its last
   cell.
5. Update anything that referenced the ID: tickets in this repository, the implementation plan, and
   README or architecture text that hedged on the answer.

If the answer contradicts an accepted ADR, do not edit that ADR silently. Write a new ADR that
supersedes it and link both directions.

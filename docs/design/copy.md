# Copy

- Date: 2026-11-21
- Status: Accepted as design guidance v0.7. The rules bind new and changed owner-facing strings. The
  current violations are listed in [copy-inventory.md](copy-inventory.md) and are fixed by the
  rewrite that follows
  [BIT-035](../plan/implementation-plan.md#bit-035-decouple-qemu-assertions-from-rendered-copy).
- Applies to: every owner-facing string in `ui/templates/`, the handler messages in
  `agent/internal/ui/`, the console text the first-run flow depends on, and any string a test or a
  screenshot treats as product copy.
- Companion documents: [audience.md](audience.md), [patterns.md](patterns.md),
  [components.md](components.md), [professional-test.md](professional-test.md),
  [copy-inventory.md](copy-inventory.md).

The owner is the installer in [audience.md](audience.md). This file is the word list and the
sentence rules for that owner. The test is simple: a person who installs recorders for a living can
finish the screen without learning how Bittern is built. Technical detail is allowed, and it is
always optional.

## Where a term may appear

| Surface | What it is |
| --- | --- |
| Primary copy | Everything visible by default: navigation, headings, ledes, labels, hints, buttons, links, alerts, empty states, confirmations, page links, the footer. |
| Disclosure | A named `details` element on the screen that belongs to the task, such as "Hardware address and sign-in". Its summary says what is inside. |
| Technical details | The one disclosure whose summary is "Technical details", the support bundle, and the status fragments they contain. |
| Logs only | The journal, the status JSON, and the support bundle internals. Never a screen. |

The glossary is the allowlist. An internal term is not a synonym for the owner term; it is the name
a power user may look for. Do not invent a third name for the same object: add a row to this table
instead. "Parenthetical" in the table means the internal term may follow the owner term once, in
parentheses, on that surface, for example "Hardware address (MAC)".

## Glossary

| Owner term | Internal term | Primary copy | Disclosure | Technical details | Logs only |
| --- | --- | --- | --- | --- | --- |
| Bittern, the appliance | bitternd, appliance agent, daemon | Yes | No | Parenthetical | Yes |
| Camera | camera, camera_1, camera ID | Yes | No | Yes | Yes |
| Camera added in this setup, not applied yet | pending camera, draft camera, staged camera | Yes, as "not applied yet" | No | Yes | Yes |
| Picture, test picture | snapshot, latest.jpg, JPEG | Yes | No | Yes | Yes |
| Recording service | Frigate | Yes, as "recording service"; never "Frigate" | No | Yes, with its version | Yes |
| Stream, stream port, stream path | RTSP, RTSP port, RTSP path, rtsp_url | Yes | No | Parenthetical | Yes |
| Bittern's camera search | discovery, ONVIF, WS-Discovery, ARP, DHCP lease | Yes, as "search" or "find" | No | Yes | Yes |
| Recording software | container image, image digest, image pull | Yes, as "the recording software" | No | Yes | Yes |
| Address reservation | DHCP reservation, dhcp-host entry, pool | Yes | Yes, when the camera must keep one address | Yes | Yes |
| Hardware address | MAC address, MAC | No | Parenthetical | Yes | Yes |
| Recording drive | storage, media path, mount point, pool, filesystem path | Yes | No | Yes | Yes |
| Configuration | appliance.yaml, YAML, config file | Yes | No | Parenthetical | Yes |
| Configuration version | schema version, schema, migration | No | No | Yes | Yes |
| Saved configuration | generation, applied generation, rollback copy | Yes, as "saved configuration" or "previous configuration" | No | Yes | Yes |
| Setup code, backup code | setup token, short code, token lifetime | Yes | No | Parenthetical | Yes, hash only |
| Sign-in, administrator password | credential, credential reference, secret, secrets.yaml | Yes | No | Parenthetical | Yes, redacted |
| Certificate fingerprint | TLS certificate, SHA-256, hash | Yes, as "the fingerprint the browser shows" | No | Yes | Yes |
| House network, camera network | lan0, cam0, interface, carrier | Yes | Parenthetical, "labelled lan0 and cam0" | Yes | Yes |
| Update, installed version | bundle, generation, slot, A/B | Yes | No | Parenthetical | Yes |
| Expired page | CSRF token, form token, session cookie | Yes, as "the page expired" | No | Yes | Yes |

## Sentence rules

1. Name the object with the owner term, then the state, then the next action.
2. One idea per sentence. A hint is one or two sentences, an alert three at most.
3. A button names what happens to the object: "Apply configuration", "Remove camera", "Search
   again". A link names its destination: "Open status", not "here".
4. Sentence case. No exclamation mark, no "please", no blame, and no "invalid" on its own.
5. A number carries its unit. A time carries its zone when the zone matters.
6. An unknown value says "Unknown". Missing data says what is missing; it is never a blank.
7. "Apply" is the product verb for writing a configuration. Do not use it for anything else. A
   single camera edited from the list uses "Apply camera change", the same verb on the same object.
8. No sentence needs a footnote, a glossary lookup, or a second screen to be understood.
9. "Add" puts a camera or another object in this setup's list; "Apply" writes the configuration to
   the appliance. A camera in the list is "not applied yet", never "saved", "installed" or "live",
   and adding one camera applies nothing. "Remove from list" is not "Remove camera": the first takes
   an object out of the list before anything is written, the second deletes a saved camera and its
   sign-in.

## Hints

A hint says what to type, gives one example, and says what happens if the field is left blank. It
does not explain the protocol, the file, or the service behind the field. It never contradicts the
field: a required field is not described as optional, and a hint does not repeat the disclosure
summary that contains it.

Before, [camera.html](../../ui/templates/camera.html) line 120:

> Only needed when discovery did not see it and the address is in the DHCP pool.

After:

> Only needed when Bittern did not find the camera and its address is in the range Bittern hands
> out.

The before names three mechanisms the owner did not ask about. The after names the condition and
the place the address came from, and leaves the pool, the lease, and the reservation for
[technical details](#disclosures-and-technical-details).

## Errors

An error names what happened, whether anything changed on the appliance, and the next action. It
does not contain a path, a status code, a service name, a raw error string, or a command. Those go
to technical details when the screen has a place for them, and to the support bundle when it does
not. Do not say "invalid", "failed" alone, or "an error occurred". Say what was kept: "Nothing was
changed" or "The previous configuration is still running".

Before, [cameras.go](../../agent/internal/ui/cameras.go) line 166:

> The camera was removed from the configuration, but its saved credential is still in
> /etc/bittern/secrets.yaml.

After:

> The camera is no longer in the configuration, but the sign-in saved for it is still on the
> appliance.

The before puts a file path and the internal word "credential" in the primary message. The after
names the object and the effect. The fact that this state needs a manual fix is a finding, not a
copy problem: see [copy-inventory.md](copy-inventory.md#findings).

## Empty states

An empty state says why the region is empty, what will fill it, and gives one action that does
that. It never says "No data available", never shows a bare count, and never offers an action that
opens a different flow than the one that fills the region. The action label names the object.

Before, [cameras.html](../../ui/templates/cameras.html) lines 9 to 11: the heading "No cameras
yet", the sentence "Run setup to discover a camera on the camera network, or add one by address.",
and the only action "Open setup".

After: the heading "No cameras yet", the sentence "Bittern has no camera to record. Add one and it
appears here.", and the action "Add camera" pointing at the camera step.

The before sends the owner back through setup to do something setup does not do, which is the
pattern rule L5 in [patterns.md](patterns.md). The after has one action that adds a camera.

## Confirmations

A confirmation names the object, what stops, what is deleted, and what is kept, in that order. The
button repeats the action on the object; it is never "Yes", "OK", or "Continue". A consequence, a
cost, or the fact that data survives is never behind a disclosure. No component, path, or service
name appears in the statement.

Before, [review.html](../../ui/templates/review.html) line 53:

> Applying asks the appliance agent to validate, render, and write this configuration. The preview
> build stops at the apply interface and writes no files.

After:

> Bittern checks the configuration, saves it, and starts the recording service. If a check fails,
> nothing changes.

The before describes the pipeline and the preview harness. The after describes the effect, the
failure outcome, and what the owner gets. The preview sentence belongs in technical details, and
only in a preview build.

## Disclosures and technical details

One summary, "Technical details", holds everything a power user may want and nobody needs to
finish the step. It is the only disclosure with that name. A second, task-specific disclosure is
allowed, and its summary names its own content, for example "Hardware address and sign-in".

Allowed behind "Technical details":

- File paths, file names, unit names, and service names.
- The name, version, image reference, and digest of the components Bittern installs and runs.
- The schema version, the saved-configuration generation, checksums, hashes, and fingerprints.
- The hardware address, once the owner has chosen to look.
- Protocol names: RTSP, ONVIF, WS-Discovery, ARP, DHCP, TLS.
- Raw status values, error strings, exit codes, and log lines.
- Per-check evidence such as discovery layers, render checks, and validation rows.
- The exact command to run on the console, when the owner can finish without it.
- Internal identifiers such as `camera_1` and status section keys.

Never behind a disclosure:

- A required field, or any field the owner needs to finish the current step.
- An error message, a warning, or the reason an action failed.
- A consequence, a cost, a privacy fact, or the fact that data will be lost.
- A secret, a password, a setup code, or a backup code.
- The only primary action.
- The state of the appliance or the object: recording stopped, disk full, camera offline.
- The certificate warning, or the instruction to compare the fingerprint with the one the browser
  shows. The comparison is a security decision and stays visible.
- The only path to finish a task. If a command is the only way forward, the manual step is a
  finding, not a disclosure.

Two rules keep the block honest. A technical detail may not be the only place a failure is
explained: the alert says what happened and the technical detail explains why. And a technical
detail may not contradict the primary copy; the same event is described once in the owner's words
and once in the component's words.

## Components the owner did not install

The default path never names a component the owner did not install. A component Bittern installs
and runs is not the owner's to configure. Naming it in primary copy moves the question from "is it
working" to "what is that", and implies a task that does not exist. Say what the component does
instead, and keep its name for the technical details.

| May be named in primary copy | Only in technical details |
| --- | --- |
| The camera, the camera switch, the recording drive, the house router, the phone, the browser, the appliance, Bittern | Frigate, bitternd, networkd, nftables, dnsmasq, chrony, Docker, compose, the OS image, the update bundle, the renderer, the schema validator, the certificate stack |

The check is a string read, not a grep alone. A grep for the right column over `ui/templates/` and
`agent/internal/ui/` finds the candidates; each hit is read in context, because a legitimate
mention inside "Technical details" looks the same to grep. A name from the right column in
primary copy is a finding with the line and the sentence.

## What the rules do not allow

- Inventing a setting, a page, or a control to make a sentence true. Record a finding instead. The
  inventory carries the current ones.
- Removing a security instruction to shorten a screen. The certificate comparison stays; only its
  wording gets plainer.
- Softening a failure into a success. "Setup complete" is not allowed beside a failed check, and a
  found camera is not a working camera.
- Renaming a component to hide it. If the owner must not see the name, say what the component does.
- Putting a secret, a password, a setup code, or a one-time code in primary copy, a disclosure, a
  URL, or a log.
- Translating a value into a friendlier lie. "No cameras" when the list failed to load is a
  different fact from "no cameras configured".

## Check

| Rule | How a review checks it |
| --- | --- |
| Glossary | Read every new string against the table. A term from the internal column in primary copy is a finding. |
| Hints | Read the hint without the field label. It still says what to type and what happens if blank. |
| Errors | The message names the effect and the next action, and the raw cause sits in technical details. |
| Empty states | The single action fills the region that is empty. |
| Confirmations | The statement names what stops, what is deleted, and what is kept. The button repeats the action. |
| Disclosure boundary | Nothing from the never list is inside a `details` element. |
| Uninstalled components | No name from the right column of the component table appears in primary copy. |
| One noun | The same object has the same name on every screen in the flow. |

Related: [audience.md](audience.md), [copy-inventory.md](copy-inventory.md),
[patterns.md](patterns.md), [components.md](components.md),
[professional-test.md](professional-test.md), [review.md](review.md), [README.md](README.md).

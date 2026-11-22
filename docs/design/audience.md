# Audience

- Date: 2026-11-21
- Status: Accepted as design guidance v0.7. It defines the owner that [copy.md](copy.md) is written
  for and that [patterns.md](patterns.md) reviews against.
- Applies to: every owner-facing string in `ui/templates/`, the handler messages in
  `agent/internal/ui/`, the console text the first-run flow depends on, and any string a test or
  a screenshot treats as product copy.
- Companion documents: [copy.md](copy.md), [README.md](README.md), [patterns.md](patterns.md),
  [components.md](components.md), [professional-test.md](professional-test.md),
  [copy-inventory.md](copy-inventory.md).

The owner is the person who installs and runs camera systems for a living. They fit cameras,
switches, and recorders on a site, usually alone, and they are paid to leave it working. They know
which camera covers which door, how to find a camera's address and sign-in, how to terminate a
cable, and what a recorder is for: live view, recordings, and a disk that fills up. They may fit
one appliance a month or twenty, they may have learned the trade on a different brand, and they are
reading this screen beside an open ceiling or a rack, on a phone, while the site manager waits.
They are not the person who built the appliance, wrote its software, or chose its parts, and they
should not have to become that person to finish setup.

The first ten minutes decide whether the product feels usable. The owner wants to put the
appliance on the house network, reach it from a phone, confirm the camera network is live, let
Bittern find the camera, see one picture, and leave knowing that recording started and what to
check if it did not. The copy serves that path. Everything else is available when they look for
it, and nothing technical is required to finish a step.

## The first ten minutes

| Minute | What the owner is doing | What the screen must let them do |
| --- | --- | --- |
| 0 to 2 | Reading the setup code from the console and choosing a password | Start without a shell command and without a second device |
| 2 to 4 | Checking the house network and the camera network | See that both cables are connected and what address each side has |
| 4 to 6 | Checking the recording drive | See that the drive is usable and how much space is left |
| 6 to 8 | Finding and adding the camera | Pick a camera Bittern found, or type an address, without knowing a protocol |
| 8 to 10 | Applying and checking the first picture | See that recording started, or exactly what to check next |

## What the owner knows and what we must not assume

| The owner knows | We must not assume |
| --- | --- |
| How to mount, aim, and power a camera. | That they know anything about Bittern, its parts, or its files. |
| How to find a camera's address, sign-in, and stream path in the camera's own page or manual. | That they can open a terminal, read a log, or edit a file on the appliance. |
| What a recorder does: live view, recordings, a disk that fills up. | That they know what a container, an image, a digest, a YAML file, a schema version, a generation, a renderer, or a hash is. |
| How to connect a camera or a switch, and what a DHCP server does at the level of "it hands out addresses". | That they know the name Frigate, or that Bittern installs and runs it. |
| That recordings belong on a separate disk. | That they know ONVIF or RTSP by name, or which port a camera streams on. |
| How to power-cycle equipment and test a cable. | That they know what a MAC address is, or why a fixed address needs one. |
| Which camera is at which door on their own site. | That they have a second screen, a keyboard, or a stable connection while they set up. |
| That a warning can be expected when a device uses its own certificate. | That they will open a disclosure, read a log, or retry without being told what to do. |

## What the owner must never be asked to understand

- Which component records the video, where it came from, or how it is packaged.
- The configuration file format, its version, or how it is generated from a template.
- The difference between the appliance agent, the renderer, the validator, and the apply
  transaction.
- Any file path on the appliance, or which service to restart.
- Why an address is reserved, hashed, fingerprinted, or versioned.

A screen that needs one of these to finish the current step is a copy defect, not a training
problem. The rules that follow from this audience are in [copy.md](copy.md), and the strings that
break them today are in [copy-inventory.md](copy-inventory.md).

Related: [copy.md](copy.md), [copy-inventory.md](copy-inventory.md), [README.md](README.md),
[patterns.md](patterns.md), [review.md](review.md).

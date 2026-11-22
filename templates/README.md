# templates

`templates/` holds the inputs the renderer combines with `appliance.yaml` and the defaults. The set
covers systemd-networkd interface files, nftables rules, dnsmasq DHCP configuration, chrony NTP
configuration, the compose file, and Frigate config fragments for Git-managed mode. Templates are
data, not code: they contain no secrets and no site-specific values, only placeholders and the
comments needed to understand the intent.

## Boundaries

In scope:

- Template files for each generated system file, with their variable contracts documented
- Validation-friendly structure so generated output can be checked before apply
- Comments that explain security or compatibility choices, such as blocked ports and camera DHCP
  options

Out of scope:

- The rendering engine, validation, and apply pipeline (agent)
- Product default values (defaults)
- Secrets, credentials, and per-device identity (generated at first boot)
- Hand-maintained copies of generated files on a running appliance

Status: the networkd, nftables, dnsmasq, dnsmasq reservations, and chrony templates exist. Schema version 2
adds the NIC match template used for `.link` output. Frigate's `config.yml`, compose file, and unit
are assembled by the renderer from structured values and `defaults/frigate.yaml` rather than from
text templates, so no Frigate fragment template is present. The renderer still adds the shared
do-not-edit header to those outputs.

## Current templates

| Template | Rendered file | Family |
| --- | --- | --- |
| `networkd/lan0.network.tmpl` | `networkd/10-lan0.network` | systemd-networkd |
| `networkd/cam0.network.tmpl` | `networkd/10-cam0.network` | systemd-networkd |
| `networkd/link.tmpl` | `networkd/10-lan0.link` or `networkd/10-cam0.link` when a match is configured | systemd-networkd |
| `nftables/bittern.nft.tmpl` | `nftables/bittern.nft` | nftables |
| `dnsmasq/dnsmasq.conf.tmpl` | `dnsmasq/dnsmasq.conf` | dnsmasq |
| `dnsmasq/reservations.conf.tmpl` | `dnsmasq/reservations.conf` when at least one camera has `mac` | dnsmasq |
| `chrony/chrony.conf.tmpl` | `chrony/chrony.conf` | chrony |

The templates are Go `text/template` inputs. They contain placeholders and comments only; the
do-not-edit header is added by the renderer so every family has the same first lines.

The Go module is under `agent/`, and `go:embed` cannot reach above its module root. A build step
copies this directory, `defaults/`, and `schema/appliance.schema.json` into the gitignored
`agent/internal/assets/` tree. `make assets` runs the copy, and `make test` and `make render`
depend on it. Do not edit the generated copy; edit this directory and regenerate.

Related: [architecture](../docs/architecture.md), [ADR 0006](../docs/adr/README.md),
[agent renderer](../agent/README.md).

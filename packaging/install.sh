#!/bin/sh
# Bittern beta preparation kit installer for a manual Debian 13 amd64 test machine.
#
# This is a beta bootstrap, not the product image installer. It verifies the
# bundled kit, installs the packages the beta device needs, installs the two
# static binaries, the units, and the CI test trust anchor, and leaves first
# boot to systemd on the next boot. Every step is idempotent.
set -eu

# Debian 13 splits the Docker CLI out of docker.io into docker-cli. docker.io
# only Recommends docker-cli, so --no-install-recommends skips it and leaves
# /usr/bin/docker absent. Keep docker-cli explicit.
#
# Debian's systemd-networkd no longer writes /etc/resolv.conf when
# systemd-resolved is absent. The beta kit must install the resolver and let
# it take its DNS servers from the lan0 DHCP lease; do not hard-code a public
# resolver.
PACKAGES='docker.io docker-cli docker-compose nftables dnsmasq chrony curl ca-certificates systemd-resolved qrencode sqlite3'
RESOLV_CONF=/etc/resolv.conf
RESOLV_CONF_BACKUP=/etc/resolv.conf.bittern-poc.bak
RESOLVED_STUB_RESOLV_CONF='../run/systemd/resolve/stub-resolv.conf'
RESOLVED_UNIT=systemd-resolved.service

# The install preflight bootstraps the management link and a local resolver
# before apt runs. The bootstrap .network lives in /run so it is transient: it
# disappears on reboot, and first boot writes the rendered files under /etc.
BOOTSTRAP_NETWORK_DIR=/run/systemd/network
BOOTSTRAP_NETWORK_FILE=$BOOTSTRAP_NETWORK_DIR/90-bittern-bootstrap.network

# The appliance never uses Docker's bridge or NAT: Frigate uses host networking
# and the Frigate config check uses --network none. Disable both so Docker does
# not need iptables or create docker0, and so a host firewall reload cannot
# leave Docker with stale rules (ADR 0004).
DOCKER_DAEMON_JSON=/etc/docker/daemon.json
DOCKER_DAEMON_CONFIG='{"iptables": false, "ip-forward": false, "bridge": "none"}'
DOCKER_CONFIG_CHANGED=0
NFTABLES_ENABLE_DROPIN=/etc/systemd/system/nftables.service.d/10-bittern-enable.conf

DRY_RUN=0
OS_RELEASE=${BITTERN_OS_RELEASE:-/etc/os-release}
SCRIPT_DIR=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)

usage() {
    cat <<'EOF'
usage: sudo ./install.sh [--dry-run] [--os-release PATH]

Install the Bittern beta preparation kit on a manual Debian 13 amd64 test
machine.

  --dry-run          verify the payload and print every planned action
  --os-release PATH  read Debian identity from PATH instead of /etc/os-release
  -h, --help         show this help

The os-release override exists so this script can be exercised on a Debian 12
development container with a Debian 13 fixture. It does not relax the root,
Debian 13, or amd64 checks.
EOF
}

log() {
    printf '%s\n' "$*"
}

warn() {
    printf 'warning: %s\n' "$*" >&2
}

die() {
    printf 'error: %s\n' "$*" >&2
    exit 1
}

run() {
    log "+ $*"
    if [ "$DRY_RUN" -eq 0 ]; then
        "$@"
    fi
}

parse_args() {
    while [ "$#" -gt 0 ]; do
        case "$1" in
            --dry-run)
                DRY_RUN=1
                ;;
            --os-release)
                [ "$#" -ge 2 ] || die "--os-release needs a path"
                OS_RELEASE=$2
                shift
                ;;
            --os-release=*)
                OS_RELEASE=${1#*=}
                ;;
            -h|--help)
                usage
                exit 0
                ;;
            *)
                usage >&2
                die "unknown argument: $1"
                ;;
        esac
        shift
    done
    [ -n "$OS_RELEASE" ] || die "the os-release path is empty"
}

os_release_value() {
    _or_key=$1
    while IFS= read -r _or_line || [ -n "$_or_line" ]; do
        _or_line=$(printf '%s' "$_or_line" | tr -d '\r')
        case "$_or_line" in
            "$_or_key"=*)
                _or_value=${_or_line#*=}
                case "$_or_value" in
                    \"*\")
                        _or_value=${_or_value#\"}
                        _or_value=${_or_value%\"}
                        ;;
                    \'*\')
                        _or_value=${_or_value#\'}
                        _or_value=${_or_value%\'}
                        ;;
                esac
                printf '%s\n' "$_or_value"
                return 0
                ;;
        esac
    done < "$OS_RELEASE"
    return 1
}

check_host() {
    [ "$(id -u)" -eq 0 ] || die "run as root, for example: sudo $SCRIPT_DIR/install.sh"

    _arch=$(uname -m)
    case "$_arch" in
        x86_64|amd64)
            ;;
        *)
            die "unsupported architecture $_arch; this payload is linux/amd64 only"
            ;;
    esac

    [ -r "$OS_RELEASE" ] || die "cannot read os-release file: $OS_RELEASE"
    _id=$(os_release_value ID) || die "cannot read ID from $OS_RELEASE"
    _version=$(os_release_value VERSION_ID) || die "cannot read VERSION_ID from $OS_RELEASE"
    [ "$_id" = debian ] || die "Debian is required; $OS_RELEASE reports ID=$_id"
    case "$_version" in
        13|13.*)
            ;;
        *)
            die "Debian 13 is required; $OS_RELEASE reports VERSION_ID=$_version"
            ;;
    esac
}

verify_payload() {
    for _file in \
        bittern \
        bitternd \
        bittern-launch \
        platform.json \
        bittern-first-boot.service \
        bittern-image-prewarm.service \
        bittern-update.service \
        bitternd.service \
        install.sh \
        cleanup.sh \
        storage-setup.sh \
        README.md \
        SHA256SUMS
    do
        [ -f "$SCRIPT_DIR/$_file" ] || die "kit is incomplete: missing $_file"
    done

    log "verifying bundled SHA256SUMS"
    (cd "$SCRIPT_DIR" && sha256sum --check --strict SHA256SUMS) ||
        die "bundled checksums do not match; re-copy the kit"
}

install_packages() {
    command -v apt-get >/dev/null 2>&1 || die "apt-get is not available; this payload expects Debian"

    check_resolver_conflicts
    ensure_install_network

    log "installing Debian packages: $PACKAGES"
    run env DEBIAN_FRONTEND=noninteractive apt-get update ||
        die "apt-get update failed; check the Debian mirror and network"
    # shellcheck disable=SC2086
    run env DEBIAN_FRONTEND=noninteractive apt-get install \
        --yes --no-install-recommends $PACKAGES ||
        die "package installation failed. On Debian 13 the compose package is docker-compose; docker-compose-v2 does not exist."

    if [ "$DRY_RUN" -eq 0 ]; then
        command -v qrencode >/dev/null 2>&1 ||
            die "the qrencode command is missing after package installation; the setup QR would not work"
        command -v sqlite3 >/dev/null 2>&1 ||
            die "the sqlite3 command is missing after package installation; the update database backup would fail"
    fi
}

package_installed() {
    [ "$(dpkg-query -W -f='${Status}' "$1" 2>/dev/null || true)" = "install ok installed" ]
}

prepare_storage() {
    log "preparing recording storage without formatting"
    if [ "$DRY_RUN" -eq 1 ]; then
        log "+ $SCRIPT_DIR/storage-setup.sh < /dev/null (non-destructive; no --format)"
        return 0
    fi

    _storage_rc=0
    "$SCRIPT_DIR/storage-setup.sh" < /dev/null || _storage_rc=$?
    case "$_storage_rc" in
        0)
            log "recording storage is mounted at /var/lib/bittern/media"
            ;;
        2)
            warn "no eligible recording drive was found; the install continues without recordings and the wizard reports storage"
            ;;
        3)
            warn "a recording drive needs formatting; the install continues without recordings. Run the printed --format command before first boot"
            ;;
        4)
            warn "a recording drive needs an explicit choice; the install continues without recordings. Run storage-setup.sh --list, then choose with --device PATH before first boot"
            ;;
        *)
            warn "recording storage setup failed with exit $_storage_rc; the install continues without recordings. Fix the printed error and run storage-setup.sh before first boot"
            ;;
    esac
}

camera_macs() {
    # The camera side must never be selected as the management link. The
    # generated 10-cam0.link carries its MAC, and a first-boot appliance.yaml
    # carries the same match. A clean Debian install has neither, so the runbook
    # also requires the camera NIC to stay unplugged until first boot.
    {
        for _cm_link in /etc/systemd/network/*cam0*.link; do
            [ -f "$_cm_link" ] || continue
            sed -n 's/^[[:space:]]*MACAddress=//p' "$_cm_link"
        done
        if [ -f /etc/bittern/appliance.yaml ]; then
            awk '
                /^[[:space:]]*cam0:[[:space:]]*$/ { in_cam0 = 1; next }
                in_cam0 && /^[^[:space:]]/ { in_cam0 = 0 }
                in_cam0 && /^[[:space:]]*mac:[[:space:]]*/ {
                    sub(/^[[:space:]]*mac:[[:space:]]*/, "")
                    print
                    in_cam0 = 0
                }
            ' /etc/bittern/appliance.yaml
        fi
    } | tr 'A-F' 'a-f' | awk 'NF && !seen[$0]++'
}

physical_ethernet_links() {
    _pel_camera_macs=$1
    for _pel_dir in /sys/class/net/*; do
        [ -d "$_pel_dir" ] || continue
        _pel_name=${_pel_dir##*/}
        case "$_pel_name" in
            lo|cam0|docker*|veth*|br-*|virbr*|tun*|tap*|wg*|tailscale*|cni*|flannel*|dummy*|bond*|wl*)
                continue
                ;;
        esac
        # Only physical links have a device symlink. This excludes the QEMU
        # harness socket links, bridges, and other virtual interfaces.
        [ -e "$_pel_dir/device" ] || continue
        [ "$(cat "$_pel_dir/type" 2>/dev/null || printf '0')" = 1 ] || continue
        [ ! -e "$_pel_dir/wireless" ] || continue
        [ ! -e "$_pel_dir/phy80211" ] || continue
        _pel_mac=$(cat "$_pel_dir/address" 2>/dev/null || true)
        case " $_pel_camera_macs " in
            *" $_pel_mac "*)
                continue
                ;;
        esac
        printf '%s\n' "$_pel_name"
    done
}

select_management_link() {
    _sml_candidates=$1
    _sml_live=
    for _sml_name in $_sml_candidates; do
        _sml_carrier=$(cat "/sys/class/net/$_sml_name/carrier" 2>/dev/null || printf '0')
        if [ "$_sml_carrier" = 1 ]; then
            _sml_live="$_sml_live $_sml_name"
        fi
    done
    # Word splitting is the point: each candidate becomes one positional argument.
    # shellcheck disable=SC2086
    set -- $_sml_live
    if [ "$#" -eq 0 ]; then
        return 1
    fi
    if [ "$#" -eq 1 ]; then
        printf '%s\n' "$1"
        return 0
    fi

    _sml_default=$(ip -4 route show default 2>/dev/null |
        awk '{ for (i = 1; i < NF; i++) if ($i == "dev") { print $(i + 1); exit } }')
    for _sml_name in "$@"; do
        if [ "$_sml_name" = "$_sml_default" ]; then
            printf '%s\n' "$_sml_name"
            return 0
        fi
    done
    for _sml_name in "$@"; do
        if [ "$_sml_name" = lan0 ]; then
            printf '%s\n' "$_sml_name"
            return 0
        fi
    done
    printf 'multiple Ethernet links have carrier: %s\n' "$*" >&2
    return 2
}

network_has_ipv4() {
    ip -4 -o addr show dev "$1" 2>/dev/null | grep -q ' inet '
}

network_has_default_route() {
    ip -4 route show default 2>/dev/null | grep -q "dev $1"
}

networkd_lease_file() {
    _nlf_ifindex=$(cat "/sys/class/net/$1/ifindex" 2>/dev/null || true)
    [ -n "$_nlf_ifindex" ] || return 1
    for _nlf_file in /run/systemd/netif/leases/*; do
        [ -f "$_nlf_file" ] || continue
        case "${_nlf_file##*/}" in
            "$_nlf_ifindex"|"$_nlf_ifindex".*)
                printf '%s\n' "$_nlf_file"
                return 0
                ;;
        esac
    done
    return 1
}

ipv4_values() {
    # Both separators map to a newline; the repeated replacement is intentional.
    # shellcheck disable=SC2020
    tr ' ,' '\n\n' | tr '\t' '\n' |
        sed 's/^[[:space:]]*//; s/[[:space:]]*$//' |
        grep -E '^[0-9]{1,3}(\.[0-9]{1,3}){3}$' |
        awk '!seen[$0]++'
}

dhcp_dns_servers() {
    _dds_iface=$1
    _dds_lease=$(networkd_lease_file "$_dds_iface" || true)
    if [ -n "$_dds_lease" ]; then
        sed -n 's/^DNS=//p' "$_dds_lease" | ipv4_values || true
    fi
    if command -v networkctl >/dev/null 2>&1; then
        networkctl status "$_dds_iface" 2>/dev/null |
            sed -n 's/^[[:space:]]*DNS:[[:space:]]*//p' | ipv4_values || true
    fi
    if command -v resolvectl >/dev/null 2>&1; then
        resolvectl dns "$_dds_iface" 2>/dev/null |
            grep -oE '([0-9]{1,3}\.){3}[0-9]{1,3}' | ipv4_values || true
    fi
    return 0
}

default_gateway_for() {
    ip -4 route show default dev "$1" 2>/dev/null |
        awk '{ for (i = 1; i < NF; i++) if ($i == "via") { print $(i + 1); exit } }'
}

write_bootstrap_network() {
    _wbn_iface=$1
    log "+ write $BOOTSTRAP_NETWORK_FILE for $_wbn_iface (transient, removed at reboot)"
    if [ "$DRY_RUN" -eq 1 ]; then
        return 0
    fi
    install -d -o root -g root -m 0755 "$BOOTSTRAP_NETWORK_DIR" ||
        die "could not create $BOOTSTRAP_NETWORK_DIR for the install network bootstrap"
    _wbn_tmp=$(mktemp "$BOOTSTRAP_NETWORK_FILE.XXXXXX") ||
        die "could not create a temporary bootstrap network file"
    cat > "$_wbn_tmp" <<EOF
# Written by Bittern packaging/install.sh for the install session only.
# This file lives in /run and disappears on reboot.
[Match]
Name=$_wbn_iface

[Network]
DHCP=ipv4
IPv6AcceptRA=no

[DHCPv4]
UseDNS=yes
EOF
    chmod 0644 "$_wbn_tmp"
    chown root:root "$_wbn_tmp"
    if ! mv -f "$_wbn_tmp" "$BOOTSTRAP_NETWORK_FILE"; then
        rm -f "$_wbn_tmp"
        die "could not install $BOOTSTRAP_NETWORK_FILE"
    fi
    log "network: wrote $BOOTSTRAP_NETWORK_FILE for $_wbn_iface"
}

remove_bootstrap_network() {
    [ -f "$BOOTSTRAP_NETWORK_FILE" ] || return 0
    log "+ rm -f $BOOTSTRAP_NETWORK_FILE (install session only; first boot writes /etc/systemd/network)"
    if [ "$DRY_RUN" -eq 1 ]; then
        return 0
    fi
    rm -f "$BOOTSTRAP_NETWORK_FILE" ||
        warn "could not remove $BOOTSTRAP_NETWORK_FILE"
}

bootstrap_dhcp() {
    _bd_iface=$1
    log "network: no IPv4 address on $_bd_iface; starting DHCP for the install session"
    write_bootstrap_network "$_bd_iface"

    _bd_networkd=0
    if command -v networkctl >/dev/null 2>&1; then
        _bd_networkd=1
        log "+ systemctl start systemd-networkd.service"
        if ! systemctl start systemd-networkd.service 2>/dev/null; then
            warn "systemd-networkd did not start; trying dhclient"
            _bd_networkd=0
        fi
    else
        warn "networkctl is not available; trying dhclient"
    fi

    if [ "$_bd_networkd" -eq 1 ]; then
        _bd_reload=0
        log "+ networkctl reload"
        networkctl reload >/dev/null 2>&1 || _bd_reload=1
        log "+ networkctl reconfigure $_bd_iface"
        networkctl reconfigure "$_bd_iface" >/dev/null 2>&1 || _bd_reload=1
        if [ "$_bd_reload" -eq 1 ]; then
            log "+ systemctl restart systemd-networkd.service"
            if ! systemctl restart systemd-networkd.service >/dev/null 2>&1; then
                warn "systemd-networkd restart failed; trying dhclient"
                _bd_networkd=0
            fi
        fi
    fi

    _bd_wait=0
    while [ "$_bd_wait" -lt 60 ]; do
        if network_has_ipv4 "$_bd_iface" && network_has_default_route "$_bd_iface"; then
            _bd_address=$(ip -4 -o addr show dev "$_bd_iface" | awk '{ print $4; exit }')
            log "network: $_bd_iface obtained $_bd_address from DHCP"
            return 0
        fi
        if [ "$_bd_wait" -eq 5 ] && [ "$_bd_networkd" -eq 0 ]; then
            if command -v dhclient >/dev/null 2>&1; then
                log "+ dhclient -1 -v $_bd_iface"
                dhclient -1 -v "$_bd_iface" >/dev/null 2>&1 ||
                    warn "dhclient did not obtain a lease on $_bd_iface"
            fi
        fi
        sleep 1
        _bd_wait=$((_bd_wait + 1))
    done

    die "could not obtain an IPv4 address on $_bd_iface. Connect the house LAN to the management NIC, leave the camera NIC unplugged, verify the DHCP server, and rerun. apt was not started and no packages were changed."
}

seed_resolv_conf() {
    _src_iface=$1
    _src_source=$2
    _src_servers=$3
    log "network: no working DNS; seeding $RESOLV_CONF from the $_src_source on $_src_iface"
    if [ "$DRY_RUN" -eq 1 ]; then
        return 0
    fi
    _src_tmp=$(mktemp "$RESOLV_CONF.bittern-install.XXXXXX") ||
        die "could not create a temporary $RESOLV_CONF"
    {
        printf '# Written by Bittern packaging/install.sh from the %s on %s.\n' \
            "$_src_source" "$_src_iface"
        for _src_server in $_src_servers; do
            printf 'nameserver %s\n' "$_src_server"
        done
    } > "$_src_tmp"
    chmod 0644 "$_src_tmp"
    chown root:root "$_src_tmp"
    if ! mv -f "$_src_tmp" "$RESOLV_CONF"; then
        rm -f "$_src_tmp"
        die "could not seed $RESOLV_CONF from the $_src_source"
    fi
    log "network: seeded $RESOLV_CONF from the $_src_source on $_src_iface ($_src_servers)"
}

dns_resolves() {
    if command -v timeout >/dev/null 2>&1; then
        timeout 5 getent hosts deb.debian.org >/dev/null 2>&1
    else
        getent hosts deb.debian.org >/dev/null 2>&1
    fi
}

verify_install_dns() {
    _vid_iface=$1
    _vid_source=$2
    _vid_wait=0
    while [ "$_vid_wait" -lt 30 ]; do
        if dns_resolves; then
            log "network: getent hosts deb.debian.org resolved before apt"
            log "network: install network bootstrap ready"
            return 0
        fi
        sleep 1
        _vid_wait=$((_vid_wait + 1))
    done
    warn "current $RESOLV_CONF:"
    sed 's/^/warning:   /' "$RESOLV_CONF" >&2 || true
    die "getent hosts deb.debian.org failed after seeding $RESOLV_CONF from the $_vid_source on $_vid_iface. apt was not started and no packages were changed. Check the DHCP server and the local resolver; do not add a public resolver."
}

ensure_install_network() {
    if [ "$DRY_RUN" -eq 0 ] && [ -f "$BOOTSTRAP_NETWORK_FILE" ]; then
        log "network: removing stale $BOOTSTRAP_NETWORK_FILE before the preflight"
        rm -f "$BOOTSTRAP_NETWORK_FILE" || warn "could not remove stale $BOOTSTRAP_NETWORK_FILE"
    fi
    _ein_camera_macs=$(camera_macs)
    _ein_candidates=$(physical_ethernet_links "$_ein_camera_macs")
    if [ -z "$_ein_candidates" ]; then
        if [ "$DRY_RUN" -eq 1 ]; then
            warn "dry run: no physical Ethernet link is available; a real run would stop before apt"
            log "would verify: getent hosts deb.debian.org"
            return 0
        fi
        die "no physical Ethernet link is available for the management network. Connect the house LAN and rerun. apt was not started and no packages were changed."
    fi

    _ein_list=
    for _ein_link in $_ein_candidates; do
        _ein_list="$_ein_list $_ein_link"
    done
    log "network: candidate Ethernet links:$_ein_list"
    if [ "$DRY_RUN" -eq 1 ]; then
        log "network: would bring up the candidate links and choose the one with carrier"
    else
        for _ein_link in $_ein_candidates; do
            log "+ ip link set dev $_ein_link up"
            ip link set dev "$_ein_link" up 2>/dev/null ||
                warn "could not bring up $_ein_link"
        done
        sleep 1
    fi

    _ein_rc=0
    if _ein_iface=$(select_management_link "$_ein_candidates"); then
        :
    else
        _ein_rc=$?
        _ein_iface=
    fi
    if [ -z "$_ein_iface" ]; then
        if [ "$DRY_RUN" -eq 1 ]; then
            warn "dry run: no management Ethernet link has carrier; a real run would stop before apt"
            log "would write $BOOTSTRAP_NETWORK_FILE, obtain DHCP, seed $RESOLV_CONF from the local lease, and verify getent"
            return 0
        fi
        if [ "$_ein_rc" -eq 2 ]; then
            die "multiple Ethernet links have carrier; cannot choose the management NIC. Leave only the house LAN connected and the camera NIC unplugged, then rerun. apt was not started and no packages were changed."
        fi
        die "no management Ethernet link has carrier. Connect the house LAN to the management NIC, leave the camera NIC unplugged, and rerun. apt was not started and no packages were changed."
    fi
    log "network: management interface $_ein_iface has carrier"

    if [ "$DRY_RUN" -eq 1 ]; then
        log "would write $BOOTSTRAP_NETWORK_FILE for $_ein_iface if it has no IPv4 address"
        log "would wait for an IPv4 address and default route on $_ein_iface"
        log "would seed $RESOLV_CONF from the DHCP lease or the default gateway"
        log "would verify: getent hosts deb.debian.org"
        return 0
    fi

    if network_has_ipv4 "$_ein_iface" && network_has_default_route "$_ein_iface"; then
        log "network: $_ein_iface already has an IPv4 address and default route"
    else
        bootstrap_dhcp "$_ein_iface"
    fi

    if dns_resolves; then
        log "network: DNS already resolves deb.debian.org"
        log "network: install network bootstrap ready"
        return 0
    fi

    _ein_servers=$(dhcp_dns_servers "$_ein_iface")
    _ein_source='DHCP lease'
    if [ -z "$_ein_servers" ]; then
        _ein_gateway=$(default_gateway_for "$_ein_iface")
        if [ -n "$_ein_gateway" ]; then
            _ein_servers=$_ein_gateway
            _ein_source='default gateway'
        fi
    fi
    if [ -z "$_ein_servers" ]; then
        die "the DHCP lease on $_ein_iface provided no DNS server and no default gateway is available. apt was not started and no packages were changed. Fix the local DHCP server; do not hard-code a public resolver."
    fi

    seed_resolv_conf "$_ein_iface" "$_ein_source" "$_ein_servers"
    verify_install_dns "$_ein_iface" "$_ein_source"
}

check_resolver_conflicts() {
    # systemd-resolved Provides and Conflicts with resolvconf. openresolv also
    # provides resolvconf and owns the same file, even though dpkg does not
    # treat it as a hard conflict. Fail before apt can remove a manager silently.
    for _conflict in resolvconf openresolv; do
        if package_installed "$_conflict"; then
            die "$_conflict is installed and conflicts with systemd-resolved; remove it with 'sudo apt-get remove $_conflict' and rerun"
        fi
    done
}

configure_docker() {
    _daemon_dir=${DOCKER_DAEMON_JSON%/*}

    if [ -e "$DOCKER_DAEMON_JSON" ] || [ -L "$DOCKER_DAEMON_JSON" ]; then
        if [ ! -f "$DOCKER_DAEMON_JSON" ]; then
            die "$DOCKER_DAEMON_JSON exists but is not a regular file; refusing to replace it"
        fi
        _current=$(cat "$DOCKER_DAEMON_JSON")
        if [ "$_current" = "$DOCKER_DAEMON_CONFIG" ]; then
            log "docker: $DOCKER_DAEMON_JSON already has the Bittern network policy"
            return 0
        fi
        warn "docker: $DOCKER_DAEMON_JSON exists with different settings"
        warn "docker: Bittern requires $DOCKER_DAEMON_CONFIG"
        die "refusing to overwrite $DOCKER_DAEMON_JSON. Move it aside or merge the required keys, then rerun."
    fi

    log "docker: writing $DOCKER_DAEMON_JSON with $DOCKER_DAEMON_CONFIG"
    if [ "$DRY_RUN" -eq 1 ]; then
        log "+ install -d -o root -g root -m 0755 $_daemon_dir"
        log "+ write $DOCKER_DAEMON_JSON"
        DOCKER_CONFIG_CHANGED=1
        return 0
    fi

    install -d -o root -g root -m 0755 "$_daemon_dir" ||
        die "could not create $_daemon_dir"
    _tmp=$(mktemp "$DOCKER_DAEMON_JSON.bittern-poc.XXXXXX") ||
        die "could not create a temporary file beside $DOCKER_DAEMON_JSON"
    printf '%s\n' "$DOCKER_DAEMON_CONFIG" > "$_tmp"
    chmod 0644 "$_tmp"
    chown root:root "$_tmp"
    if ! mv -f "$_tmp" "$DOCKER_DAEMON_JSON"; then
        rm -f "$_tmp"
        die "could not install $DOCKER_DAEMON_JSON"
    fi
    DOCKER_CONFIG_CHANGED=1
}

unit_reboot_consequence() {
    case "$1" in
        docker.service)
            printf '%s' "Docker would not start after reboot, so Frigate could not run and the status page would report Degraded."
            ;;
        nftables.service)
            printf '%s' "The rendered firewall would not load after reboot."
            ;;
        systemd-networkd.service)
            printf '%s' "The appliance would lose lan0, cam0 and DNS after reboot."
            ;;
        systemd-resolved.service)
            printf '%s' "DNS would not survive a reboot."
            ;;
        dnsmasq.service)
            printf '%s' "The camera segment would lose DHCP after reboot."
            ;;
        chrony.service)
            printf '%s' "Time sync and the camera NTP server would not start after reboot."
            ;;
        bittern-first-boot.service)
            printf '%s' "First boot would not run on the next boot, so setup would not complete."
            ;;
        bittern-image-prewarm.service)
            printf '%s' "The recording software would not download in the background, so the first camera apply would have to start and wait for the download."
            ;;
        bittern-update.service)
            printf '%s' "An interrupted update would not resume after reboot."
            ;;
        bitternd.service)
            printf '%s' "The status API and UI would not start after reboot."
            ;;
        *)
            printf '%s' "The unit would not start after reboot."
            ;;
    esac
}

enable_unit() {
    # Enable a unit and prove the enablement took. The caller names the reboot
    # consequence that the shared helper prints on failure. The nftables case
    # keeps the existing no-[Install] drop-in fallback because some Debian
    # builds ship that unit without an install section.
    _eu_unit=$1
    _eu_consequence=$(unit_reboot_consequence "$_eu_unit")
    run systemctl reset-failed "$_eu_unit" || true
    if run systemctl enable "$_eu_unit"; then
        :
    else
        _fragment=$(systemctl show -p FragmentPath --value "$_eu_unit" 2>/dev/null || true)
        if [ -z "$_fragment" ] || [ ! -f "$_fragment" ]; then
            die "could not enable $_eu_unit: the unit is missing. $_eu_consequence"
        fi
        if [ "$_eu_unit" = nftables.service ] && ! grep -q '^\[Install\]' "$_fragment"; then
            warn "$_eu_unit has no [Install] section; adding an explicit WantedBy=multi-user.target drop-in at $NFTABLES_ENABLE_DROPIN"
            run install -d -o root -g root -m 0755 "${NFTABLES_ENABLE_DROPIN%/*}" ||
                die "could not create the nftables.service drop-in directory"
            _tmp=$(mktemp "${NFTABLES_ENABLE_DROPIN}.bittern.XXXXXX") ||
                die "could not create a temporary nftables.service drop-in"
            printf '%s\n' '[Install]' 'WantedBy=multi-user.target' > "$_tmp"
            chmod 0644 "$_tmp"
            chown root:root "$_tmp"
            if ! mv -f "$_tmp" "$NFTABLES_ENABLE_DROPIN"; then
                rm -f "$_tmp"
                die "could not install $NFTABLES_ENABLE_DROPIN"
            fi
            run systemctl daemon-reload || die "systemctl daemon-reload failed after the nftables drop-in"
            run systemctl enable "$_eu_unit" ||
                die "could not enable $_eu_unit after adding $NFTABLES_ENABLE_DROPIN. $_eu_consequence"
        else
            die "could not enable $_eu_unit. $_eu_consequence Check systemctl status $_eu_unit and $_fragment."
        fi
    fi
    if [ "$DRY_RUN" -eq 1 ]; then
        log "would verify: systemctl is-enabled $_eu_unit"
        return 0
    fi
    _enabled=$(systemctl is-enabled "$_eu_unit" 2>/dev/null || true)
    case "$_enabled" in
        enabled|enabled-runtime)
            ;;
        *)
            die "$_eu_unit is not enabled after installation ($_enabled). $_eu_consequence"
            ;;
    esac
    log "$_eu_unit: enabled for the next boot"
}

enable_units() {
    # One owner for every unit the payload installs. Debian may enable some of
    # these from a package postinst, but Bittern does not rely on that: a clean
    # install, a rerun after a failure, or a system where an operator disabled a
    # unit must converge here. Apply owns the rendered Frigate unit because it
    # does not exist until the first camera apply.
    enable_unit docker.service
    enable_unit nftables.service
    enable_unit systemd-networkd.service
    enable_unit systemd-resolved.service
    enable_unit dnsmasq.service
    enable_unit chrony.service
    enable_unit bittern-first-boot.service
    enable_unit bittern-image-prewarm.service
    enable_unit bittern-update.service
    enable_unit bitternd.service
}

configure_resolver() {
    # A stock dnsmasq starts on port 53 beside the resolver and can win the
    # bind before the rendered config sets port=0. First boot restarts dnsmasq
    # with the rendered DHCP-only config, so stop the package copy first unless
    # that config is already in place.
    if systemctl is-active --quiet dnsmasq.service 2>/dev/null &&
        ! grep -qs '^[[:space:]]*port=0' /etc/dnsmasq.d/bittern.conf; then
        run systemctl stop dnsmasq.service || true
        log "dnsmasq: stopped until first boot writes the rendered DHCP-only config"
    fi

    # The package postinst may already have enabled and started the unit. This
    # function only restarts it so the stub resolver file exists for the rest of
    # the install; enable_units owns the persistent enablement.
    run systemctl reset-failed "$RESOLVED_UNIT" || true
    run systemctl restart "$RESOLVED_UNIT" ||
        die "could not start $RESOLVED_UNIT; DNS would not work after installation"

    if [ "$DRY_RUN" -eq 1 ]; then
        log "would point $RESOLV_CONF at the systemd-resolved stub resolver if needed"
        log "would verify: systemctl is-active $RESOLVED_UNIT"
        return 0
    fi

    systemctl is-active --quiet "$RESOLVED_UNIT" ||
        die "$RESOLVED_UNIT is not active after installation. DNS would be lost until the next boot."

    _resolv_target=$(readlink "$RESOLV_CONF" 2>/dev/null || true)
    case "$_resolv_target" in
        "$RESOLVED_STUB_RESOLV_CONF"|/run/systemd/resolve/stub-resolv.conf)
            ;;
        *)
            if [ -e "$RESOLV_CONF" ] && [ ! -L "$RESOLV_CONF" ] && [ ! -f "$RESOLV_CONF" ]; then
                die "$RESOLV_CONF exists but is not a regular file or symlink; refusing to replace it"
            fi
            if [ -f "$RESOLV_CONF" ] && [ ! -L "$RESOLV_CONF" ]; then
                log "resolver: preserving the existing $RESOLV_CONF at $RESOLV_CONF_BACKUP"
                cp -a "$RESOLV_CONF" "$RESOLV_CONF_BACKUP" ||
                    die "could not preserve the existing $RESOLV_CONF"
            fi
            log "+ ln -sfn $RESOLVED_STUB_RESOLV_CONF $RESOLV_CONF"
            ln -sfn "$RESOLVED_STUB_RESOLV_CONF" "$RESOLV_CONF" ||
                die "could not point $RESOLV_CONF at systemd-resolved"
            ;;
    esac

    [ -s /run/systemd/resolve/stub-resolv.conf ] ||
        die "systemd-resolved did not create /run/systemd/resolve/stub-resolv.conf; $RESOLV_CONF would be empty after reboot"
    log "resolver: $RESOLVED_UNIT owns $RESOLV_CONF and takes DNS from the lan0 DHCP lease"
}

start_docker() {
    # enable_units owns the persistent enablement. This starts the daemon with
    # the policy written by configure_docker and restarts it when the policy
    # changed. A failed docker unit from an earlier install must be cleared
    # before the daemon is started.
    run systemctl reset-failed docker.service || true
    run systemctl start docker || die "could not start the Docker service"
    if [ "$DOCKER_CONFIG_CHANGED" -eq 1 ]; then
        run systemctl restart docker ||
            die "could not restart Docker after writing $DOCKER_DAEMON_JSON"
    fi
    if [ "$DRY_RUN" -eq 1 ]; then
        log "would verify: /usr/bin/docker --version"
        log "would verify: /usr/bin/docker-compose version"
        return 0
    fi
    if [ ! -x /usr/bin/docker ]; then
        die "the Docker CLI is missing at /usr/bin/docker. Debian 13 provides it in the docker-cli package; install docker-cli before continuing."
    fi
    if ! /usr/bin/docker --version >/dev/null 2>&1; then
        die "docker --version failed. The beta device needs the Debian docker-cli package at /usr/bin/docker; install it before continuing."
    fi
    if [ ! -x /usr/bin/docker-compose ]; then
        die "the standalone Compose binary is missing at /usr/bin/docker-compose. The beta device needs Docker Compose v2 from the Debian docker-compose package; install it before continuing."
    fi
    if ! /usr/bin/docker-compose version >/dev/null 2>&1; then
        die "docker-compose version failed. The beta device needs Docker Compose v2 from the Debian docker-compose package at /usr/bin/docker-compose; install it before continuing."
    fi
    log "docker: $(/usr/bin/docker --version | sed -n '1p')"
    log "docker-compose: $(/usr/bin/docker-compose version | sed -n '1p')"
}

dnsmasq_include_active() {
    _dnsmasq_conf=/etc/dnsmasq.conf
    if [ -r "$_dnsmasq_conf" ] &&
        grep -Eq '^[[:space:]]*conf-dir=/etc/dnsmasq\.d/?([[:space:]]*,[[:space:]]*\*\.conf)?[[:space:]]*$' "$_dnsmasq_conf"
    then
        log "dnsmasq: $_dnsmasq_conf loads /etc/dnsmasq.d/*.conf"
        return 0
    fi

    _dnsmasq_defaults=/etc/default/dnsmasq
    if [ -r "$_dnsmasq_defaults" ]; then
        _config_dir=$(sed -n 's/^[[:space:]]*CONFIG_DIR=//p' "$_dnsmasq_defaults" | tail -n 1 | tr -d '"')
        case "$_config_dir" in
            /etc/dnsmasq.d|/etc/dnsmasq.d,*|/etc/dnsmasq.d/*)
                log "dnsmasq: the Debian wrapper in $_dnsmasq_defaults loads /etc/dnsmasq.d"
                return 0
                ;;
        esac
    fi
    return 1
}

ensure_dnsmasq_include() {
    if dnsmasq_include_active; then
        return 0
    fi

    warn "dnsmasq does not load /etc/dnsmasq.d; adding a conf-dir line to /etc/dnsmasq.conf"

    if [ "$DRY_RUN" -eq 1 ]; then
        log "+ append 'conf-dir=/etc/dnsmasq.d/,*.conf' to /etc/dnsmasq.conf"
        if [ ! -e /etc/dnsmasq.conf ]; then
            log "+ create /etc/dnsmasq.conf with that line if the package does not provide it"
        fi
        return 0
    fi

    if [ ! -e /etc/dnsmasq.conf ]; then
        warn "/etc/dnsmasq.conf is missing after package installation; creating a minimal one"
        _tmp=$(mktemp /etc/dnsmasq.conf.bittern-poc.XXXXXX)
        printf '%s\n' 'conf-dir=/etc/dnsmasq.d/,*.conf' > "$_tmp"
        chmod 0644 "$_tmp"
        chown root:root "$_tmp"
        mv "$_tmp" /etc/dnsmasq.conf
        log "created /etc/dnsmasq.conf with conf-dir=/etc/dnsmasq.d/,*.conf"
        return 0
    fi

    _backup=/etc/dnsmasq.conf.bittern-poc.bak
    if [ ! -e "$_backup" ]; then
        cp -a /etc/dnsmasq.conf "$_backup"
        log "backed up /etc/dnsmasq.conf to $_backup"
    fi

    _tmp=$(mktemp /etc/dnsmasq.conf.bittern-poc.XXXXXX)
    cp /etc/dnsmasq.conf "$_tmp"
    cat >> "$_tmp" <<'EOF'

# Added by Bittern packaging/install.sh: load drop-ins when Debian does not.
conf-dir=/etc/dnsmasq.d/,*.conf
EOF
    chmod --reference=/etc/dnsmasq.conf "$_tmp"
    chown --reference=/etc/dnsmasq.conf "$_tmp"
    mv "$_tmp" /etc/dnsmasq.conf
    log "added conf-dir=/etc/dnsmasq.d/,*.conf to /etc/dnsmasq.conf"
}

install_binary() {
    _source=$1
    _target=$2
    _temporary="${_target}.bittern-poc.$$"
    log "+ install -o root -g root -m 0755 $_source $_temporary"
    log "+ mv -f $_temporary $_target"
    if [ "$DRY_RUN" -eq 0 ]; then
        install -o root -g root -m 0755 "$_source" "$_temporary"
        mv -f "$_temporary" "$_target"
    fi
}

install_files() {
    install_binary "$SCRIPT_DIR/bittern" /usr/bin/bittern
    install_binary "$SCRIPT_DIR/bitternd" /usr/bin/bitternd

    # The fixed launcher is the only ExecStart of bitternd.service. It lives
    # outside the state root and execs the active bundle or the baseline above.
    run install -d -o root -g root -m 0755 /usr/libexec/bittern ||
        die "could not create /usr/libexec/bittern"
    install_binary "$SCRIPT_DIR/bittern-launch" /usr/libexec/bittern/bitternd

    # The platform provider reads this root-owned file and fails closed without
    # it. Generation 1 is provisional until the image ADR fixes the layout.
    run install -d -o root -g root -m 0755 /usr/lib/bittern ||
        die "could not create /usr/lib/bittern"
    run install -o root -g root -m 0644 \
        "$SCRIPT_DIR/platform.json" /usr/lib/bittern/platform.json ||
        die "could not install /usr/lib/bittern/platform.json"

    run install -o root -g root -m 0644 \
        "$SCRIPT_DIR/bittern-first-boot.service" \
        /etc/systemd/system/bittern-first-boot.service ||
        die "could not install bittern-first-boot.service"
    run install -o root -g root -m 0644 \
        "$SCRIPT_DIR/bittern-image-prewarm.service" \
        /etc/systemd/system/bittern-image-prewarm.service ||
        die "could not install bittern-image-prewarm.service"
    run install -o root -g root -m 0644 \
        "$SCRIPT_DIR/bittern-update.service" \
        /etc/systemd/system/bittern-update.service ||
        die "could not install bittern-update.service"
    run install -o root -g root -m 0644 \
        "$SCRIPT_DIR/bitternd.service" \
        /etc/systemd/system/bitternd.service ||
        die "could not install bitternd.service"

    run install -d -o root -g root -m 0750 /etc/bittern /var/lib/bittern ||
        die "could not create /etc/bittern and /var/lib/bittern"

    if [ -d "$SCRIPT_DIR/trust/release-keys" ]; then
        run install -d -o root -g root -m 0755 /etc/bittern/trust/release-keys ||
            die "could not create /etc/bittern/trust/release-keys"
        for _trust_key in "$SCRIPT_DIR"/trust/release-keys/*.pub; do
            [ -f "$_trust_key" ] || continue
            run install -o root -g root -m 0644 "$_trust_key" \
                "/etc/bittern/trust/release-keys/$(basename "$_trust_key")" ||
                die "could not install the CI test anchor $(basename "$_trust_key")"
        done
    fi

    run systemctl daemon-reload || die "systemctl daemon-reload failed"
}

print_notes() {
    cat <<'EOF'

BETA PREPARATION KIT WARNING
This kit is an unsigned beta bootstrap for disposable test hardware. It is not
an update payload, not a product image, and does not install unattended
upgrades. It installs only the CI test anchor, never a release anchor. Do not
run it on a machine you cannot reinstall.

RECORDING DRIVE
The installer runs the payload's storage-setup.sh in non-destructive mode
without a terminal. It mounts a drive labelled bittern-media that already
carries ext4, and never formats one. An unlabelled drive is listed but not
chosen without a person, so the install continues and first boot reports
storage unconfigured. To prepare a blank drive or choose an unlabelled one,
run storage-setup.sh, then the command it prints, before the first boot. See
docs/runbooks/gk41-poc.md.

RECORDING SOFTWARE
After the reboot, Bittern downloads the pinned recording software in the
background while you work through setup. The status page reports whether it is
downloading, ready, or failed. If the download has not finished, the apply waits
for it; if no download is running, the apply starts one in a named step.

TEMPORARY SSH
The rendered firewall has no SSH accept rule. Use the console, or add a live
rule after first boot and remove it when the management path is no longer
needed:

  sudo nft insert rule inet bittern input iifname "lan0" tcp dport 22 accept comment "bittern-poc-ssh"
  sudo nft -a list chain inet bittern input | grep bittern-poc-ssh
  sudo nft delete rule inet bittern input handle <handle>

A separate nft table with an accept rule does not bypass the rendered drop,
because accept is not final across base chains. A reboot removes the live rule.
Do not persist it.

SETUP TOKEN
After the first boot, the console shows a QR for the setup URL with the token in
the URL fragment, the same token in readable groups, and a short one-time code.
Scan the QR with the phone or laptop that will run the wizard. If no local
console is attached, read the same block on the appliance terminal or over the
temporary SSH path:

  sudo bittern setup-token --qr

The token and short code are single use. The short code expires after 10
minutes. Never redirect the token or QR output into a file, log, or support
bundle.

NETWORK AND RESOLVER
Before apt, the installer brings up the management Ethernet link with carrier,
excludes cam0 and known camera MACs, writes a transient DHCP .network under
/run/systemd/network, and waits for an IPv4 address and default route. It seeds
/etc/resolv.conf from the DHCP-provided DNS or the default gateway, verifies
getent hosts deb.debian.org, and stops before apt if that fails. It never
hard-codes a public resolver. Do not configure the NIC or /etc/resolv.conf by
hand.

After the package step the installer makes systemd-resolved own /etc/resolv.conf.
DNS still comes from the lan0 DHCP lease. The /run bootstrap file disappears at
the next reboot, and first boot writes the generated network files under /etc.
EOF
}

parse_args "$@"
check_host
verify_payload

if [ "$DRY_RUN" -eq 1 ]; then
    log "Bittern beta preparation kit installer: dry run on $(uname -m) with $OS_RELEASE"
else
    log "Bittern beta preparation kit installer: $OS_RELEASE on $(uname -m)"
fi

install_packages
prepare_storage
configure_docker
start_docker
configure_resolver
ensure_dnsmasq_include
install_files
enable_units
remove_bootstrap_network

if [ "$DRY_RUN" -eq 1 ]; then
    log "dry run complete: no packages, files, units, or services were changed"
else
    log "install complete: reboot, then scan the setup QR on the local console"
fi
print_notes

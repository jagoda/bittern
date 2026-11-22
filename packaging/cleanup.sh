#!/bin/sh
# Bittern POC test-device cleanup.
#
# This is a test and development tool, not the product factory reset. It
# removes the POC service state, its generated live configuration, and its
# data so the same machine can take a clean POC install. It removes the
# installed binaries and restores the stock chrony and nftables conffiles. It
# does not implement ADR 0017 reset levels, rotate identity, or wipe the
# recording filesystem. The product reset path is BIT-010.
#
# The intended use is the single Level 1 step in the clean-install runbook:
# run this script, then install the new payload or reinstall Debian. It is
# idempotent. Without --yes it only prints the plan.
set -eu
# shellcheck shell=sh

BITTERN_UNITS='bittern-first-boot.service bittern-image-prewarm.service bitternd.service bittern-frigate.service'
BITTERN_BINARIES='/usr/bin/bittern /usr/bin/bitternd'
BITTERN_DAEMON_JSON='{"iptables":false,"ip-forward":false,"bridge":"none"}'
BITTERN_IMAGE_REPOSITORY='ghcr.io/blakeblackshear/frigate'
NETWORK_RECOVERY_FILES='10-lan0.link 10-cam0.link 10-lan0.network 10-cam0.network'
DNSMASQ_BITTERN_FILES='bittern.conf bittern-reservation.conf'

ROOT=${BITTERN_CLEANUP_ROOT:-/}
PREFIX=
ASSUME_YES=0
DRY_RUN=1
PURGE_IMAGES=0
PURGE_MEDIA=0
PURGE_NETWORK=0
PLAN_FILE=
DAEMON_BACKUP=
NFTABLES_CONFFILE_RESTORED=0
TAB=$(printf '\t')

usage() {
    cat <<'EOF'
usage: sudo ./cleanup.sh [--yes] [--dry-run] [--purge-images] [--purge-media]
                      [--purge-network]

Clean POC Bittern state from a test or development machine. This is not the
product factory reset and it does not wipe the recording filesystem.

  --yes           execute the plan; without it the script only prints the plan
  --dry-run       force plan-only mode, even if --yes is also present
  --purge-images  also remove the Frigate images used by the POC
  --purge-media   also remove /var/lib/bittern/media; the media filesystem
                  must already be unmounted and you must type a second
                  confirmation phrase
  --purge-network also remove the live generated .link and .network files;
                  the default keeps them so the next install still has DHCP
  --root PATH     test only: apply the cleanup under PATH instead of /
  -h, --help      show this help

By default Docker images and media recordings are kept. The script refuses to
recursively remove a mountpoint and refuses to purge media while it is mounted.
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

root_path() {
    printf '%s%s' "$PREFIX" "$1"
}

conffile_backup_path() {
    _backup_dir=$(root_path /var/backups/bittern)
    case "$1" in
        chrony)
            printf '%s/chrony-chrony.conf.rendered' "$_backup_dir"
            ;;
        nftables)
            printf '%s/nftables-nftables.conf.rendered' "$_backup_dir"
            ;;
        *)
            return 1
            ;;
    esac
}

parse_args() {
    while [ "$#" -gt 0 ]; do
        case "$1" in
            --yes)
                ASSUME_YES=1
                DRY_RUN=0
                ;;
            --dry-run)
                DRY_RUN=1
                ;;
            --purge-images)
                PURGE_IMAGES=1
                ;;
            --purge-media)
                PURGE_MEDIA=1
                ;;
            --purge-network)
                PURGE_NETWORK=1
                ;;
            --root)
                [ "$#" -ge 2 ] || die "--root needs a path"
                ROOT=$2
                shift
                ;;
            --root=*)
                ROOT=${1#*=}
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

    if [ "$ASSUME_YES" -eq 0 ]; then
        DRY_RUN=1
    fi

    case "$ROOT" in
        /*)
            ;;
        *)
            die "--root must be an absolute path"
            ;;
    esac
    [ -d "$ROOT" ] || die "root does not exist: $ROOT"
    ROOT=$(CDPATH='' cd -- "$ROOT" && pwd -P) || die "cannot resolve root: $ROOT"
    if [ "$ROOT" = / ]; then
        PREFIX=
    else
        PREFIX=${ROOT%/}
    fi
}

check_environment() {
    command -v findmnt >/dev/null 2>&1 ||
        die "findmnt is required so the script can refuse to cross a mountpoint"

    if [ "$ROOT" = / ]; then
        [ "$(id -u)" -eq 0 ] || die "run as root, for example: sudo $0 --yes"
    else
        warn "test root mode: file changes are limited to $ROOT; systemd, Docker and nft commands still use PATH"
    fi
}

plan() {
    _plan_action=$1
    _plan_value=${2:-}
    printf '%s\t%s\n' "$_plan_action" "$_plan_value" >> "$PLAN_FILE"
}

plan_line() {
    log "plan: $*"
}

path_exists() {
    [ -e "$1" ] || [ -L "$1" ]
}

path_is_mount() {
    _mount_target=$(findmnt -n -o TARGET --target "$1" 2>/dev/null) || return 1
    _mount_target=$(readlink -f "$_mount_target" 2>/dev/null || printf '%s' "$_mount_target")
    _path_target=$(readlink -f "$1" 2>/dev/null || printf '%s' "$1")
    [ "$_mount_target" = "$_path_target" ]
}

mount_below() {
    findmnt -rn -o TARGET 2>/dev/null |
        awk -v prefix="$1/" 'index($0, prefix) == 1 { found = 1 } END { exit found ? 0 : 1 }'
}

has_bittern_header() {
    [ -f "$1" ] && [ ! -L "$1" ] || return 1
    IFS= read -r _header_line < "$1" || true
    [ "$_header_line" = "# Generated by Bittern from appliance.yaml; do not edit." ]
}

known_network_name() {
    case " $NETWORK_RECOVERY_FILES " in
        *" $1 "*)
            return 0
            ;;
    esac
    return 1
}

known_dnsmasq_name() {
    case " $DNSMASQ_BITTERN_FILES " in
        *" $1 "*)
            return 0
            ;;
    esac
    return 1
}

unit_known() {
    _unit=$1
    for _base in /etc/systemd/system /run/systemd/system; do
        if path_exists "$(root_path "$_base/$_unit")"; then
            return 0
        fi
    done
    return 1
}

unit_listed() {
    command -v systemctl >/dev/null 2>&1 || return 1
    systemctl list-unit-files --no-legend --type=service "$1" 2>/dev/null |
        grep -q "^$1" 2>/dev/null
}

unit_present() {
    unit_known "$1" || unit_listed "$1"
}

clean_state() {
    _verbose=${1:-0}
    _clean=0

    fail_check() {
        _clean=1
        if [ "$_verbose" -eq 1 ]; then
            log "remaining: $*"
        fi
    }

    _config=$(root_path /etc/bittern)
    if path_exists "$_config"; then
        fail_check "$_config"
    fi

    for _binary in $BITTERN_BINARIES; do
        _path=$(root_path "$_binary")
        if path_exists "$_path"; then
            fail_check "$_path"
        fi
    done

    _state=$(root_path /var/lib/bittern)
    if [ -d "$_state" ] && [ ! -L "$_state" ]; then
        for _entry in "$_state"/.[!.]* "$_state"/..?* "$_state"/*; do
            path_exists "$_entry" || continue
            [ "${_entry##*/}" = media ] && continue
            fail_check "$_entry"
        done
    elif path_exists "$_state"; then
        fail_check "$_state"
    fi

    _token=$(root_path /var/lib/bittern/setup-token)
    if path_exists "$_token"; then
        fail_check "$_token (setup token)"
    fi

    for _unit in $BITTERN_UNITS; do
        if unit_known "$_unit"; then
            fail_check "unit $_unit"
        fi
        if unit_listed "$_unit"; then
            fail_check "unit $_unit (systemd)"
        fi
        for _base in /etc/systemd/system /run/systemd/system; do
            for _suffix in "$_unit" "$_unit.d"; do
                _path=$(root_path "$_base/$_suffix")
                if path_exists "$_path"; then
                    fail_check "$_path"
                fi
            done
        done
        _wants=$(root_path "/etc/systemd/system/multi-user.target.wants/$_unit")
        if path_exists "$_wants"; then
            fail_check "$_wants"
        fi
    done

    # The live generated network files are a documented exception. They carry
    # the management DHCP lease, and removing them before the next install can
    # leave the machine with no address or resolver. They are removed only by
    # --purge-network, which also makes them part of the clean-state check.
    if [ "$PURGE_NETWORK" -eq 1 ]; then
        _network_dir=$(root_path /etc/systemd/network)
        if [ -d "$_network_dir" ]; then
            for _file in "$_network_dir"/*.link "$_network_dir"/*.network; do
                path_exists "$_file" || continue
                if known_network_name "${_file##*/}" || has_bittern_header "$_file"; then
                    fail_check "$_file"
                fi
            done
        fi
    fi

    _dnsmasq_dir=$(root_path /etc/dnsmasq.d)
    if [ -d "$_dnsmasq_dir" ]; then
        for _file in "$_dnsmasq_dir"/*.conf; do
            path_exists "$_file" || continue
            if known_dnsmasq_name "${_file##*/}" || has_bittern_header "$_file"; then
                fail_check "$_file"
            elif [ ! -L "$_file" ] &&
                grep -Eq '^[[:space:]]*dhcp-host=.*10\.77\.' "$_file" 2>/dev/null; then
                fail_check "manual dnsmasq reservation in $_file"
            fi
        done
    fi
    _dnsmasq_conf=$(root_path /etc/dnsmasq.conf)
    if [ -f "$_dnsmasq_conf" ] && [ ! -L "$_dnsmasq_conf" ] &&
        grep -Eq '^[[:space:]]*dhcp-host=.*10\.77\.' "$_dnsmasq_conf" 2>/dev/null; then
        fail_check "manual dnsmasq reservation in $_dnsmasq_conf"
    fi
    _dnsmasq_backup=$(root_path /etc/dnsmasq.conf.bittern-poc.bak)
    if path_exists "$_dnsmasq_backup"; then
        fail_check "$_dnsmasq_backup"
    fi
    if [ -f "$_dnsmasq_conf" ] && [ ! -L "$_dnsmasq_conf" ] &&
        grep -q '^# Added by Bittern packaging/install.sh' "$_dnsmasq_conf" 2>/dev/null; then
        fail_check "installer dnsmasq append without a backup in $_dnsmasq_conf"
    fi

    _chrony_conf=$(root_path /etc/chrony/chrony.conf)
    if ! path_exists "$_chrony_conf"; then
        fail_check "missing stock chrony conffile $_chrony_conf"
    elif [ -L "$_chrony_conf" ] || [ ! -f "$_chrony_conf" ]; then
        fail_check "chrony conffile is not a regular file: $_chrony_conf"
    elif has_bittern_header "$_chrony_conf"; then
        fail_check "rendered chrony conffile $_chrony_conf"
    fi

    _nft_conf=$(root_path /etc/nftables.conf)
    if ! path_exists "$_nft_conf"; then
        fail_check "missing stock nftables conffile $_nft_conf"
    elif [ -L "$_nft_conf" ] || [ ! -f "$_nft_conf" ]; then
        fail_check "nftables conffile is not a regular file: $_nft_conf"
    elif has_bittern_header "$_nft_conf" ||
        grep -q 'table inet bittern' "$_nft_conf" 2>/dev/null; then
        fail_check "rendered nftables conffile $_nft_conf"
    fi

    if command -v nft >/dev/null 2>&1; then
        if nft list table inet bittern >/dev/null 2>&1; then
            fail_check "nftables table inet bittern"
        fi
    fi

    if command -v docker >/dev/null 2>&1; then
        _containers=$(frigate_container_ids)
        if [ -n "$_containers" ]; then
            fail_check "Frigate container or compose project"
        fi
    fi

    _daemon=$(root_path /etc/docker/daemon.json)
    if path_exists "$_daemon"; then
        fail_check "$_daemon"
    fi

    return "$_clean"
}

frigate_container_ids() {
    command -v docker >/dev/null 2>&1 || return 0
    docker ps -a --format '{{.ID}} {{.Names}} {{.Label "com.docker.compose.project"}}' 2>/dev/null |
        awk '$2 == "frigate" || $3 == "bittern-frigate" { print $1 }'
}

frigate_image_ids() {
    command -v docker >/dev/null 2>&1 || return 0
    docker image ls --filter "reference=$BITTERN_IMAGE_REPOSITORY:*" --format '{{.ID}}' 2>/dev/null |
        sort -u
}

collect_units() {
    _changed=0
    for _unit in $BITTERN_UNITS; do
        if unit_present "$_unit"; then
            plan_line "stop and disable $_unit"
            plan unit "$_unit"
            _changed=1
        fi
        for _base in /etc/systemd/system /run/systemd/system; do
            _file=$(root_path "$_base/$_unit")
            if path_exists "$_file"; then
                plan_line "remove unit file $_file"
                plan remove "$_file"
                _changed=1
            fi
            _dropin=$(root_path "$_base/$_unit.d")
            if path_exists "$_dropin"; then
                plan_line "remove unit drop-ins $_dropin"
                plan remove "$_dropin"
                _changed=1
            fi
        done
        _wants=$(root_path "/etc/systemd/system/multi-user.target.wants/$_unit")
        if path_exists "$_wants"; then
            plan_line "remove enable link $_wants"
            plan remove "$_wants"
            _changed=1
        fi
    done
    if [ "$_changed" -eq 1 ]; then
        plan_line "reload the systemd manager configuration"
        plan daemon-reload
    fi
}

collect_binaries() {
    for _binary in $BITTERN_BINARIES; do
        _path=$(root_path "$_binary")
        if path_exists "$_path"; then
            plan_line "remove binary $_path"
            plan remove "$_path"
        fi
    done
}

collect_docker() {
    _compose=$(root_path /var/lib/bittern/compose/compose.yaml)
    if path_exists "$_compose"; then
        plan_line "stop the Frigate compose project with $_compose"
        plan docker-down "$_compose"
    fi

    _ids=$(frigate_container_ids)
    if [ -n "$_ids" ]; then
        for _id in $_ids; do
            plan_line "remove Frigate container or project container $_id"
            plan docker-remove-container "$_id"
        done
    fi

    if [ "$PURGE_IMAGES" -eq 1 ]; then
        _images=$(frigate_image_ids)
        for _id in $_images; do
            plan_line "remove Frigate Docker image $_id"
            plan docker-remove-image "$_id"
        done
    else
        plan_line "keep Frigate Docker images (use --purge-images to remove them)"
    fi
}

collect_config() {
    _config=$(root_path /etc/bittern)
    if path_exists "$_config"; then
        plan_line "remove configuration, secrets, TLS material and generated config $_config"
        plan remove "$_config"
    fi
}

collect_state() {
    _state=$(root_path /var/lib/bittern)
    if path_exists "$_state" && path_is_mount "$_state"; then
        die "refusing to clean state: $_state is a mount; unmount it first"
    fi
    _media=$(root_path /var/lib/bittern/media)
    _keep_media=0
    if [ "$PURGE_MEDIA" -eq 0 ] && path_exists "$_media"; then
        _keep_media=1
        plan_line "keep media path $_media and its contents"
    fi

    if [ -d "$_state" ] && [ ! -L "$_state" ]; then
        for _entry in "$_state"/.[!.]* "$_state"/..?* "$_state"/*; do
            path_exists "$_entry" || continue
            if [ "${_entry##*/}" = media ]; then
                continue
            fi
            plan_line "remove state $_entry"
            plan remove "$_entry"
        done
    elif path_exists "$_state"; then
        plan_line "remove state $_state"
        plan remove "$_state"
    fi

    if [ "$_keep_media" -eq 0 ]; then
        plan_line "remove empty state root $_state if it remains"
        plan remove-empty-dir "$_state"
    fi
}

collect_media() {
    [ "$PURGE_MEDIA" -eq 1 ] || return 0
    _media=$(root_path /var/lib/bittern/media)
    if ! path_exists "$_media"; then
        return 0
    fi
    if path_is_mount "$_media"; then
        die "refusing --purge-media: $_media is a mount; unmount it first and rerun"
    fi
    if [ -L "$_media" ]; then
        die "refusing --purge-media: $_media is a symlink; remove it by hand after checking the target"
    fi
    if mount_below "$_media"; then
        die "refusing --purge-media: a filesystem is mounted below $_media"
    fi
    plan_line "remove media recordings under $_media"
    plan purge-media "$_media"
}

collect_network() {
    _network_dir=$(root_path /etc/systemd/network)
    [ -d "$_network_dir" ] || return 0
    _kept=0
    for _file in "$_network_dir"/*.link "$_network_dir"/*.network; do
        path_exists "$_file" || continue
        _base=${_file##*/}
        if [ -L "$_file" ]; then
            if known_network_name "$_base"; then
                if [ "$PURGE_NETWORK" -eq 1 ]; then
                    plan_line "remove known Bittern recovery link $_file"
                    plan remove-network "$_file"
                else
                    _kept=1
                fi
            else
                warn "ownership not established for symlink $_file; leaving it"
            fi
            continue
        fi
        if known_network_name "$_base"; then
            if [ "$PURGE_NETWORK" -eq 1 ]; then
                plan_line "remove known Bittern recovery file $_file"
                plan remove-network "$_file"
            else
                _kept=1
            fi
        elif has_bittern_header "$_file"; then
            if [ "$PURGE_NETWORK" -eq 1 ]; then
                plan_line "remove generated network file $_file"
                plan remove-network "$_file"
            else
                _kept=1
            fi
        fi
    done
    if [ "$_kept" -eq 1 ]; then
        plan_line "keep live generated network files under $_network_dir; the install uses them and first boot overwrites them. Use --purge-network for final removal."
    fi
}

collect_dnsmasq() {
    _dnsmasq_dir=$(root_path /etc/dnsmasq.d)
    if [ -d "$_dnsmasq_dir" ]; then
        for _file in "$_dnsmasq_dir"/*.conf; do
            path_exists "$_file" || continue
            _base=${_file##*/}
            if known_dnsmasq_name "$_base" || has_bittern_header "$_file"; then
                plan_line "remove Bittern dnsmasq file $_file"
                plan remove-dnsmasq "$_file"
            elif [ ! -L "$_file" ] &&
                grep -Eq '^[[:space:]]*dhcp-host=.*10\.77\.' "$_file" 2>/dev/null; then
                warn "ownership not established for manual camera reservation in $_file; leaving it"
            fi
        done
    fi
    _dnsmasq_conf=$(root_path /etc/dnsmasq.conf)
    if [ -f "$_dnsmasq_conf" ] && [ ! -L "$_dnsmasq_conf" ] &&
        grep -Eq '^[[:space:]]*dhcp-host=.*10\.77\.' "$_dnsmasq_conf" 2>/dev/null; then
        warn "ownership not established for manual camera reservation in $_dnsmasq_conf; leaving it"
    fi
}

collect_dnsmasq_stock() {
    _dnsmasq_conf=$(root_path /etc/dnsmasq.conf)
    _dnsmasq_backup=$(root_path /etc/dnsmasq.conf.bittern-poc.bak)
    if path_exists "$_dnsmasq_backup"; then
        if [ -L "$_dnsmasq_backup" ] || [ ! -f "$_dnsmasq_backup" ]; then
            warn "ownership not established for dnsmasq backup $_dnsmasq_backup; leaving it"
            return 0
        fi
        if path_exists "$_dnsmasq_conf" && [ ! -L "$_dnsmasq_conf" ] &&
            ! grep -q '^# Added by Bittern packaging/install.sh' "$_dnsmasq_conf" 2>/dev/null; then
            warn "the current $_dnsmasq_conf has no Bittern append; leaving $_dnsmasq_backup for manual review"
            return 0
        fi
        plan_line "restore stock $_dnsmasq_conf from installer backup $_dnsmasq_backup"
        plan restore-dnsmasq "$_dnsmasq_backup"
    elif [ -f "$_dnsmasq_conf" ] && [ ! -L "$_dnsmasq_conf" ] &&
        [ "$(grep -c '' "$_dnsmasq_conf")" = "1" ] &&
        grep -qx 'conf-dir=/etc/dnsmasq.d/,*.conf' "$_dnsmasq_conf"; then
        plan_line "remove installer-created single-line $_dnsmasq_conf"
        plan remove-installer-dnsmasq "$_dnsmasq_conf"
    fi
}

collect_conffile() {
    _conffile=$1
    _package=$2
    if ! path_exists "$_conffile"; then
        plan_line "restore missing stock $_package conffile $_conffile with apt-get"
        plan restore-conffile "$_package"
        return 0
    fi
    if [ -L "$_conffile" ] || [ ! -f "$_conffile" ]; then
        warn "ownership not established for $_conffile; leaving it"
        return 1
    fi
    if has_bittern_header "$_conffile"; then
        _rendered_backup=$(conffile_backup_path "$_package")
        plan_line "preserve rendered $_conffile at $_rendered_backup, remove it, and restore the stock $_package conffile with apt-get"
        plan remove-conffile "$_conffile"
        plan restore-conffile "$_package"
        return 0
    fi
    plan_line "keep stock $_package conffile $_conffile"
    return 1
}

collect_conffiles() {
    # apt needs a working resolver. Restore the stock chrony and nftables
    # conffiles before any .network or dnsmasq removal can take away the DHCP
    # resolver that the restore depends on.
    _chrony_conf=$(root_path /etc/chrony/chrony.conf)
    collect_conffile "$_chrony_conf" chrony || true

    _nft_conf=$(root_path /etc/nftables.conf)
    if collect_conffile "$_nft_conf" nftables; then
        NFTABLES_CONFFILE_RESTORED=1
    fi
}

collect_nftables() {
    if command -v nft >/dev/null 2>&1 && nft list table inet bittern >/dev/null 2>&1; then
        plan_line "delete nftables table inet bittern"
        plan nft-table ""
    fi

    if [ "$NFTABLES_CONFFILE_RESTORED" -eq 1 ] && unit_present nftables.service; then
        plan_line "disable nftables.service after restoring the stock nftables conffile"
        plan nftables-unit ""
    fi
}

collect_daemon() {
    _daemon=$(root_path /etc/docker/daemon.json)
    path_exists "$_daemon" || return 0
    if [ -L "$_daemon" ]; then
        warn "ownership not established for symlink $_daemon; leaving it"
        return 0
    fi
    if [ ! -f "$_daemon" ]; then
        warn "ownership not established for $_daemon; it is not a regular file, leaving it"
        return 0
    fi
    _current=$(tr -d '[:space:]' < "$_daemon")
    if [ "$_current" != "$BITTERN_DAEMON_JSON" ]; then
        warn "ownership not established for $_daemon; it is not the Bittern network policy, leaving it"
        return 0
    fi

    _backup_dir=$(root_path /var/backups/bittern)
    _stamp=$(date -u +%Y%m%dT%H%M%SZ)
    DAEMON_BACKUP="$_backup_dir/daemon.json.$_stamp"
    _suffix=1
    while path_exists "$DAEMON_BACKUP"; do
        DAEMON_BACKUP="$_backup_dir/daemon.json.$_stamp.$_suffix"
        _suffix=$((_suffix + 1))
    done
    plan_line "back up $_daemon to $DAEMON_BACKUP, then remove $_daemon"
    plan daemon-config "$_daemon"
}

collect_plan() {
    : > "$PLAN_FILE"
    collect_units
    collect_binaries
    collect_docker
    collect_config
    collect_media
    collect_state
    # Restore the stock conffiles before any network or dnsmasq removal. A
    # failed restore then leaves the rendered conffile in place while the DHCP
    # resolver is still configured.
    collect_conffiles
    collect_network
    collect_dnsmasq
    collect_dnsmasq_stock
    collect_nftables
    collect_daemon
}

run() {
    log "+ $*"
    if [ "$DRY_RUN" -eq 0 ]; then
        "$@"
    fi
}

stop_disable_unit() {
    _unit=$1
    if ! unit_present "$_unit"; then
        return 0
    fi
    log "+ systemctl disable --now $_unit"
    if [ "$DRY_RUN" -eq 0 ]; then
        systemctl disable --now "$_unit" ||
            {
                systemctl stop "$_unit" 2>/dev/null || true
                systemctl disable "$_unit" 2>/dev/null || true
            }
        systemctl reset-failed "$_unit" 2>/dev/null || true
    fi
}

remove_path() {
    _path=$1
    if [ -L "$_path" ]; then
        run rm -f -- "$_path"
        return 0
    fi
    path_exists "$_path" || return 0
    if path_is_mount "$_path"; then
        die "refusing to remove mountpoint $_path"
    fi
    if mount_below "$_path"; then
        die "refusing to remove $_path: a filesystem is mounted below it"
    fi
    if [ -d "$_path" ]; then
        run rm -rf --one-file-system -- "$_path"
    else
        run rm -f -- "$_path"
    fi
}

remove_network_file() {
    _path=$1
    _base=${_path##*/}
    if [ -L "$_path" ]; then
        known_network_name "$_base" || {
            warn "ownership changed for $_path; leaving it"
            return 0
        }
    elif ! known_network_name "$_base" && ! has_bittern_header "$_path"; then
        warn "ownership changed for $_path; leaving it"
        return 0
    fi
    remove_path "$_path"
}

remove_dnsmasq_file() {
    _path=$1
    _base=${_path##*/}
    if [ -L "$_path" ]; then
        known_dnsmasq_name "$_base" || {
            warn "ownership changed for $_path; leaving it"
            return 0
        }
    elif ! known_dnsmasq_name "$_base" && ! has_bittern_header "$_path"; then
        warn "ownership changed for $_path; leaving it"
        return 0
    fi
    remove_path "$_path"
}

remove_conffile() {
    _path=$1
    if ! has_bittern_header "$_path"; then
        warn "ownership changed for $_path; leaving it"
        return 0
    fi
    case "$_path" in
        */etc/chrony/chrony.conf)
            _package=chrony
            ;;
        */etc/nftables.conf)
            _package=nftables
            ;;
        *)
            die "internal error: no conffile package for $_path"
            ;;
    esac
    _backup=$(conffile_backup_path "$_package")
    _backup_dir=${_backup%/*}
    log "+ install -d -m 0700 $_backup_dir"
    log "+ cp -p $_path $_backup"
    log "+ rm -f $_path"
    if [ "$DRY_RUN" -eq 0 ]; then
        install -d -m 0700 "$_backup_dir" ||
            die "could not create conffile backup directory $_backup_dir"
        cp -p -- "$_path" "$_backup" ||
            die "could not preserve the rendered $_path before restoring the stock conffile"
        rm -f -- "$_path"
    fi
}

restore_conffile() {
    _package=$1
    case "$_package" in
        chrony)
            _target=$(root_path /etc/chrony/chrony.conf)
            ;;
        nftables)
            _target=$(root_path /etc/nftables.conf)
            ;;
        *)
            die "internal error: unknown conffile package $_package"
            ;;
    esac
    _backup=$(conffile_backup_path "$_package")

    restore_rendered_fallback() {
        if [ -f "$_backup" ] && [ ! -L "$_backup" ]; then
            log "restore the rendered $_package conffile from $_backup after the failed stock restore"
            cp -p -- "$_backup" "$_target" ||
                die "could not restore $_target from $_backup; the conffile is missing"
            rm -f -- "$_backup"
        fi
    }

    if [ "$ROOT" != / ] && [ "${BITTERN_CLEANUP_FAKE_APT:-0}" != 1 ]; then
        log "test root: skip apt restore of $_package; set BITTERN_CLEANUP_FAKE_APT=1 with a fake apt-get to exercise it"
        return 0
    fi
    command -v apt-get >/dev/null 2>&1 ||
        die "apt-get is required to restore the $_package conffile"
    log "+ env DEBIAN_FRONTEND=noninteractive apt-get install --reinstall --yes -o Dpkg::Options::=--force-confmiss $_package"
    if [ "$DRY_RUN" -eq 0 ]; then
        if ! env DEBIAN_FRONTEND=noninteractive apt-get install --reinstall --yes \
            -o Dpkg::Options::=--force-confmiss "$_package"; then
            restore_rendered_fallback
            die "could not restore the $_package conffile; cleanup is incomplete and the rendered file was kept in place"
        fi
        if ! path_exists "$_target" || [ -L "$_target" ] || [ ! -f "$_target" ]; then
            restore_rendered_fallback
            die "apt finished but the stock $_package conffile is missing at $_target"
        fi
        if has_bittern_header "$_target"; then
            restore_rendered_fallback
            die "the restored $_package conffile at $_target still has the Bittern header"
        fi
        rm -f -- "$_backup"
    fi
}

restore_dnsmasq_backup() {
    _backup=$1
    _target=$(root_path /etc/dnsmasq.conf)
    if [ -L "$_backup" ] || [ ! -f "$_backup" ]; then
        warn "ownership changed for $_backup; leaving it"
        return 0
    fi
    if path_exists "$_target" && [ ! -L "$_target" ] &&
        ! grep -q '^# Added by Bittern packaging/install.sh' "$_target" 2>/dev/null; then
        warn "the current $_target has no Bittern append; leaving $_backup for manual review"
        return 0
    fi
    log "+ cp -p $_backup $_target"
    log "+ rm -f $_backup"
    if [ "$DRY_RUN" -eq 0 ]; then
        cp -p -- "$_backup" "$_target" ||
            die "could not restore $_target from $_backup"
        rm -f -- "$_backup"
    fi
}

remove_installer_dnsmasq() {
    _path=$1
    if [ -L "$_path" ] || [ ! -f "$_path" ] ||
        [ "$(grep -c '' "$_path")" != "1" ] ||
        ! grep -qx 'conf-dir=/etc/dnsmasq.d/,*.conf' "$_path"; then
        warn "ownership changed for installer dnsmasq file $_path; leaving it"
        return 0
    fi
    remove_path "$_path"
}

docker_down() {
    _compose=$1
    if [ ! -f "$_compose" ]; then
        return 0
    fi
    if command -v docker-compose >/dev/null 2>&1; then
        run docker-compose --file "$_compose" down --remove-orphans
    elif command -v docker >/dev/null 2>&1; then
        run docker compose --file "$_compose" down --remove-orphans
    else
        warn "neither docker-compose nor docker is installed; skipping compose down"
    fi
}

remove_docker_container() {
    _id=$1
    command -v docker >/dev/null 2>&1 || return 0
    log "+ docker rm --force $_id"
    if [ "$DRY_RUN" -eq 0 ]; then
        docker rm --force "$_id" >/dev/null 2>&1 || true
    fi
}

remove_docker_image() {
    _id=$1
    command -v docker >/dev/null 2>&1 || return 0
    log "+ docker image rm --force $_id"
    if [ "$DRY_RUN" -eq 0 ]; then
        docker image rm --force "$_id" >/dev/null 2>&1 || true
    fi
}

delete_nft_table() {
    command -v nft >/dev/null 2>&1 || return 0
    if nft list table inet bittern >/dev/null 2>&1; then
        run nft delete table inet bittern
    fi
}

disable_nftables_unit() {
    if ! unit_present nftables.service; then
        return 0
    fi
    log "+ systemctl disable --now nftables.service"
    if [ "$DRY_RUN" -eq 0 ]; then
        systemctl disable --now nftables.service ||
            {
                systemctl stop nftables.service 2>/dev/null || true
                systemctl disable nftables.service 2>/dev/null || true
            }
        if systemctl is-enabled --quiet nftables.service 2>/dev/null; then
            die "could not disable nftables.service; cleanup is incomplete"
        fi
    fi
}

backup_remove_daemon() {
    _source=$1
    path_exists "$_source" || return 0
    _backup_dir=${DAEMON_BACKUP%/*}
    log "+ install -d -m 0700 $_backup_dir"
    log "+ cp -p $_source $DAEMON_BACKUP"
    log "+ rm -f $_source"
    if [ "$DRY_RUN" -eq 0 ]; then
        install -d -m 0700 "$_backup_dir" ||
            die "could not create backup directory $_backup_dir"
        cp -p "$_source" "$DAEMON_BACKUP" ||
            die "could not back up $_source to $DAEMON_BACKUP; leaving it in place"
        rm -f -- "$_source"
    fi
}

purge_media() {
    _media=$1
    if path_is_mount "$_media"; then
        die "refusing to purge mounted media $_media"
    fi
    if mount_below "$_media"; then
        die "refusing to purge $_media: a filesystem is mounted below it"
    fi
    remove_path "$_media"
}

execute_plan() {
    while IFS="$TAB" read -r _action _value; do
        case "$_action" in
            unit)
                stop_disable_unit "$_value"
                ;;
            remove)
                remove_path "$_value"
                ;;
            remove-empty-dir)
                if [ -d "$_value" ] && [ ! -L "$_value" ]; then
                    log "+ rmdir $_value (if empty)"
                    if [ "$DRY_RUN" -eq 0 ]; then
                        rmdir -- "$_value" 2>/dev/null || true
                    fi
                fi
                ;;
            remove-network)
                remove_network_file "$_value"
                ;;
            remove-dnsmasq)
                remove_dnsmasq_file "$_value"
                ;;
            remove-conffile)
                remove_conffile "$_value"
                ;;
            restore-conffile)
                restore_conffile "$_value"
                ;;
            restore-dnsmasq)
                restore_dnsmasq_backup "$_value"
                ;;
            remove-installer-dnsmasq)
                remove_installer_dnsmasq "$_value"
                ;;
            daemon-reload)
                run systemctl daemon-reload
                ;;
            docker-down)
                docker_down "$_value"
                ;;
            docker-remove-container)
                remove_docker_container "$_value"
                ;;
            docker-remove-image)
                remove_docker_image "$_value"
                ;;
            nft-table)
                delete_nft_table
                ;;
            nftables-unit)
                disable_nftables_unit
                ;;
            daemon-config)
                backup_remove_daemon "$_value"
                ;;
            purge-media)
                purge_media "$_value"
                ;;
            *)
                die "internal error: unknown plan action $_action"
                ;;
        esac
    done < "$PLAN_FILE"
}

clean_state_summary() {
    if [ "$PURGE_NETWORK" -eq 1 ]; then
        printf '%s' "no Bittern units, /etc/bittern, state except media, inet bittern table, Frigate container, generated network files, daemon.json, or setup token"
    else
        printf '%s' "no Bittern units, /etc/bittern, state except media, inet bittern table, Frigate container, daemon.json, or setup token; live generated network files are kept as the documented exception"
    fi
}

main() {
    parse_args "$@"
    check_environment

    PLAN_FILE=$(mktemp "${TMPDIR:-/tmp}/bittern-cleanup-plan.XXXXXX") ||
        die "could not create a temporary plan file"
    trap 'rm -f "$PLAN_FILE"' EXIT HUP INT TERM

    if clean_state 0; then
        log "already clean: $(clean_state_summary)"
        return 0
    fi

    log "Bittern POC cleanup plan"
    log "root: $ROOT"
    if [ "$DRY_RUN" -eq 1 ]; then
        log "mode: plan only (no changes; add --yes to execute)"
    else
        log "mode: execute"
    fi
    log ""

    collect_plan

    log ""
    if [ "$DRY_RUN" -eq 1 ]; then
        log "dry run complete: no files, units, firewall rules, containers, or images were changed"
        return 0
    fi

    if [ "$PURGE_MEDIA" -eq 1 ]; then
        printf 'Type DELETE MEDIA to confirm the media purge: ' >&2
        IFS= read -r _answer || _answer=
        [ "$_answer" = "DELETE MEDIA" ] ||
            die "media purge not confirmed; no media path was removed"
    fi

    log ""
    log "executing cleanup"
    execute_plan
    log ""
    log "clean-state check"

    if clean_state 1; then
        log "clean state verified: $(clean_state_summary)"
        return 0
    fi

    die "clean-state check failed: see the remaining paths above"
}

main "$@"

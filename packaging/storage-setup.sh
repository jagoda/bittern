#!/bin/sh
# Bittern recording-drive setup.
#
# This is the POC bridge for the named BIT-012 storage exception. It selects a
# dedicated recording block device, reuses an existing ext4 filesystem, formats
# only after explicit approval, mounts the filesystem at
# /var/lib/bittern/media, writes one fstab line by UUID, and verifies the
# result. It is idempotent and repairs a partial manual attempt. The product
# path selects and prepares storage in the wizard and renders mount units; see
# BIT-012.
#
# Safety properties this script holds:
# - A missing recording drive does not block boot. The fstab line carries
#   nofail and x-systemd.device-timeout, so local-fs.target does not fail when
#   the enclosure is unplugged or dead. Recordings never fall back to the OS
#   disk: the rendered Frigate unit requires the media mount and refuses to
#   start unless /var/lib/bittern/media is a mount point, and the status
#   storage section reports the drive as failed. The script rewrites an
#   existing line that lacks these options, so an installed appliance migrates
#   on its next run.
# - One run at a time. The whole run holds flock(2) on
#   /run/lock/bittern-storage.lock, and the fstab content is stamped before the
#   run and re-checked immediately before the rename. A second run refuses
#   instead of racing another mkfs or another read-modify-write of fstab.
# - Fail closed on the system disk. When / cannot be mapped to a listed block
#   device (overlay, NFS or 9p root, findmnt failing) no device is offered at
#   all. findmnt is cross-checked against /proc/self/mountinfo and root= in
#   /proc/cmdline, and a FAT (EFI system partition) filesystem is never a
#   candidate.
# - The approval names the object. --format applies only to the exact --device
#   path: a whole disk whose filesystem is on a partition is refused, never
#   silently widened to the partition. The serial, model, size and UUID of the
#   selection are re-read and compared immediately before mkfs and before a
#   reuse mount.
# - A format is recoverable. A marker under /var/lib/bittern is written before
#   mkfs and removed after the new UUID is read. A run that finds the marker for
#   the selected device refuses to reuse or mount the filesystem and needs
#   --format to reformat it.
#
# Whole disks and partitions are candidates. A hand-prepared drive often has a
# partition table with the ext4 filesystem and the bittern-media label on
# /dev/sdb1 while /dev/sdb itself has no filesystem and no label. The candidate
# is the node that holds the filesystem.
#
# Selection is automatic only for one eligible node carrying the filesystem
# label bittern-media. Otherwise the script lists the eligible nodes and asks on
# a terminal, or refuses without blocking when there is no terminal. The
# `--list` and `--list --json` inventory is the same candidate model that the
# BIT-012 wizard will render instead of reimplementing detection.
#
# USB and removable disks are valid targets: the reference appliance records to
# a USB SATA enclosure. Boot media is identified by the running root, boot and
# ESP filesystems, or by discoverable installer media, never by transport or the
# removable flag.
set -eu
# shellcheck shell=sh

MOUNTPOINT=/var/lib/bittern/media
MEDIA_LABEL=bittern-media
# nofail keeps a missing enclosure from failing local-fs.target, and the
# device timeout bounds how long the boot waits for it. The Frigate unit's
# RequiresMountsFor and mount-point assertion provide the safety that the old
# no-nofail rule was protecting: a failed mount never lets Frigate record into
# the OS disk. See docs/runbooks/gk41-poc.md#prepare-the-recording-drive.
FSTAB_OPTIONS='defaults,noatime,nofail,x-systemd.device-timeout=30s'
FSTAB_DUMP=0
FSTAB_PASS=2
LSBLK_COLUMNS='NAME,KNAME,PATH,TYPE,SIZE,MODEL,SERIAL,TRAN,LABEL,FSTYPE,UUID,MOUNTPOINTS,RO,RM,PKNAME'
TAB=$(printf '\t')

ROOT=${BITTERN_STORAGE_ROOT:-/}
FSTAB=
DEVICE=
DRY_RUN=0
FORMAT=0
ASSUME_YES=0
LIST=0
JSON=0
INTERACTIVE=0
TEST_MODE=0
PREFIX=
MOUNT=
FSTAB_LINE=
FS_DEVICE=
FS_DEVICE_TYPE=
FS_DEVICE_SIZE=
FS_DISK=
FS_DISK_SIZE=
FS_MODEL=
FS_SERIAL=
FS_LABEL=
FS_UUID=
FS_INVENTORY_UUID=
NEEDS_FORMAT=0
CANDIDATE_NOTE=
PROBE_FILE=
TEMP_DIR=
DEVICES_FILE=
SAFE_DISKS_FILE=
CANDIDATES_FILE=
SELECTED_FILE=
RAW_FILE=
LOCK_FILE=
FORMAT_MARKER=
SYSTEM_SOURCES_FILE=
SYSTEM_DISKS_FILE=
ROOT_MAPPED=0
FSTAB_STAMP=
FSTAB_BACKUP=
FSTAB_HAD_FILE=0
FSTAB_COMMITTED=0
PENDING_FORMAT=0
FS_DEVICE_FSTYPE=
CURRENT_MOUNT=0

usage() {
    cat <<'EOF'
usage: sudo ./storage-setup.sh [--format [--yes]] [--device PATH] [--dry-run]
       ./storage-setup.sh --list [--json]

Prepare the dedicated recording drive at /var/lib/bittern/media. The script
detects a whole disk or partition, reuses an existing ext4 filesystem, mounts
it, sets mode 0755, writes one fstab line by UUID with
defaults,noatime,nofail,x-systemd.device-timeout=30s, and verifies the result.
It never formats without explicit approval. An existing line without the
boot-safety options is replaced, so an installed appliance migrates when the
script runs again. A missing drive then does not fail the boot, and the
Frigate unit still refuses to start unless the media path is a mount point.

One run holds a lock for its whole duration, and the inventory fails closed:
when the running / cannot be mapped to a listed block device the script offers
no device at all, because it cannot prove that the OS disk or the ESP is not a
candidate. The serial, model, size and UUID of the selection are re-read
immediately before mkfs and before a reuse mount.

  --format       approve formatting the selected device non-interactively.
                 Formatting destroys all data on it. Without --format the
                 script stops and prints the exact command when the selected
                 device is blank or does not carry ext4. In an interactive run
                 the format question and typed confirmation replace this flag.
  --yes          accepted only as a confirmation; it never authorises
                 formatting and does not replace --format.
  --device PATH  select a whole disk or partition explicitly instead of
                 automatic detection. The path may be a /dev/disk/by-id name.
                 Every safety check still runs before the device is touched.
                 --format applies only to this exact path: when the filesystem
                 is on a partition, name the partition.
  --list         print the eligible recording devices and exit. This is a
                 read-only inventory for a person or a later wizard.
  --json         with --list, print stable machine-readable JSON instead of
                 the human list.
  --dry-run      print every action, including the exact fstab line when it is
                 known, and change nothing. A dry run never prompts.
  --root PATH    test only: apply file changes under PATH with a temporary
                 fstab and fake mount commands. It never selects a real device.
  --fstab PATH   test only: use PATH instead of <root>/etc/fstab. PATH must
                 resolve under --root.
  -h, --help     show this help

Detection never uses transport or the removable flag. A USB SATA enclosure is a
normal recording drive. A disk is excluded when it backs the running /, /boot
or ESP filesystem, is mounted anywhere, is part of a RAID or LVM volume, is
read-only, or carries discoverable installer media (ISO/UDF or an installer
label). Partitions are candidates in their own right. A whole disk that holds
only a partition table is not offered as a format target when one of its
partitions is a candidate; the partition is preferred.

Selection is automatic only when exactly one eligible device carries the
filesystem label bittern-media. That is the re-install case and needs no prompt.
Otherwise, on a terminal, the script prints a numbered list with device, type,
size, model, filesystem and label, and asks which device to use. It then asks
whether to format a device without ext4, defaulting to no. A format needs a
second, typed confirmation: the exact device path or --format. A bare y is not
enough. With no terminal, the script prints the eligible list, says why nothing
was chosen, and exits non-zero without blocking. It never guesses. `--device
PATH` names the device and still runs every safety check. `--format` is the
non-interactive format approval.

The human list and `--list --json` are the same candidate inventory. The
BIT-012 wizard will render this inventory instead of reimplementing detection;
the JSON schema version is part of that contract. The JSON fields are
schema_version and devices[] with device, type, size_bytes, size, model,
serial, transport, filesystem, label, filesystem_uuid, needs_format, disk,
disk_size_bytes and note.

Exit status:
  0  the mount and fstab line are present and verified, or --list succeeded
  1  a refusal, path or device error, or verification failure
  2  no eligible recording device was found
  3  a device needs formatting and no format approval was given
  4  a choice is required: no bittern-media label and no terminal, more than
     one labelled candidate, or an explicitly named disk with several
     partition candidates; use the printed list and --device PATH

Test-only environment:
  BITTERN_STORAGE_LSBLK_FIXTURE  read captured `lsblk -P -b -o ...` output
                                 instead of running lsblk. Requires --root.
  A file at <root>/proc/self/mountinfo or <root>/proc/cmdline replaces that
  /proc file when it exists, so a test can model an overlay or container root.

A format that is interrupted leaves a marker at
/var/lib/bittern/storage-format-in-progress. The next run refuses to reuse or
mount that filesystem and needs --format to reformat it, so a half-written
ext4 is never mounted and recorded into.

This is the temporary POC path. BIT-012 replaces it with wizard storage
selection and rendered pool mount units.
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
    rollback_fstab
    exit 1
}

root_path() {
    printf '%s%s' "$PREFIX" "$1"
}

trim() {
    printf '%s' "$1" | sed 's/^[[:space:]]*//; s/[[:space:]]*$//'
}

canonical_path() {
    if [ -e "$1" ] || [ -L "$1" ]; then
        readlink -f -- "$1" 2>/dev/null || printf '%s' "$1"
    else
        printf '%s' "$1"
    fi
}

canonical_device() {
    canonical_path "$1"
}

# Print the /proc file that describes this machine. Under --root a fixture at
# <root>/proc/... is used when it exists, so a test can model a container or an
# overlay root without reading the host's own /proc.
proc_path() {
    _proc_wanted=$1
    if [ "$TEST_MODE" -eq 1 ]; then
        _proc_candidate=$(root_path "$_proc_wanted")
        if [ -r "$_proc_candidate" ]; then
            printf '%s' "$_proc_candidate"
            return 0
        fi
        return 1
    fi
    [ -r "$_proc_wanted" ] || return 1
    printf '%s' "$_proc_wanted"
}

# Resolve a root= kernel argument to a device path. Only local block devices
# are resolved; an NFS or 9p root spec returns nothing.
resolve_root_spec() {
    _root_spec=$1
    case "$_root_spec" in
        UUID=*)
            _root_by=/dev/disk/by-uuid/${_root_spec#UUID=}
            ;;
        PARTUUID=*)
            _root_by=/dev/disk/by-partuuid/${_root_spec#PARTUUID=}
            ;;
        LABEL=*)
            _root_by=/dev/disk/by-label/${_root_spec#LABEL=}
            ;;
        /dev/*)
            _root_by=$_root_spec
            ;;
        *)
            return 1
            ;;
    esac
    canonical_device "$_root_by"
}

human_size() {
    if command -v numfmt >/dev/null 2>&1; then
        numfmt --to=iec --suffix=B "$1" 2>/dev/null || printf '%s bytes' "$1"
    else
        printf '%s bytes' "$1"
    fi
}

json_escape() {
    printf '%s' "$1" |
        tr '\t\r\n' '   ' |
        sed 's/\\/\\\\/g; s/"/\\"/g'
}

print_json_string_or_null() {
    if [ "$1" = - ] || [ -z "$1" ]; then
        printf 'null'
    else
        printf '"%s"' "$(json_escape "$1")"
    fi
}

parse_args() {
    while [ "$#" -gt 0 ]; do
        case "$1" in
            --format)
                FORMAT=1
                ;;
            --yes)
                ASSUME_YES=1
                ;;
            --device)
                [ "$#" -ge 2 ] || die "--device needs a path"
                DEVICE=$2
                shift
                ;;
            --device=*)
                DEVICE=${1#*=}
                ;;
            --dry-run)
                DRY_RUN=1
                ;;
            --list)
                LIST=1
                ;;
            --json)
                JSON=1
                ;;
            --root)
                [ "$#" -ge 2 ] || die "--root needs a path"
                ROOT=$2
                shift
                ;;
            --root=*)
                ROOT=${1#*=}
                ;;
            --fstab)
                [ "$#" -ge 2 ] || die "--fstab needs a path"
                FSTAB=$2
                shift
                ;;
            --fstab=*)
                FSTAB=${1#*=}
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
        TEST_MODE=0
    else
        PREFIX=${ROOT%/}
        TEST_MODE=1
    fi

    MOUNT=$(root_path "$MOUNTPOINT")
    if [ -z "$FSTAB" ]; then
        FSTAB=$(root_path /etc/fstab)
    fi
    case "$FSTAB" in
        /*)
            ;;
        *)
            die "--fstab must be an absolute path"
            ;;
    esac
    # Resolve the parent so ".." and a symlinked directory cannot lift the
    # fstab out of the test root, then require it to stay inside that root.
    _fstab_dir=${FSTAB%/*}
    _fstab_base=${FSTAB##*/}
    [ -n "$_fstab_dir" ] || _fstab_dir=/
    if [ -d "$_fstab_dir" ]; then
        _fstab_dir=$(CDPATH='' cd -- "$_fstab_dir" && pwd -P) ||
            die "cannot resolve the fstab directory: $_fstab_dir"
    fi
    case "$_fstab_dir" in
        */)
            FSTAB=$_fstab_dir$_fstab_base
            ;;
        *)
            FSTAB=$_fstab_dir/$_fstab_base
            ;;
    esac
    if [ "$TEST_MODE" -eq 1 ]; then
        case "$FSTAB" in
            "$ROOT"/*)
                ;;
            *)
                die "--fstab must be inside --root ($ROOT): $FSTAB; a test-root run must never write the host fstab"
                ;;
        esac
    fi

    LOCK_FILE=$(root_path /run/lock/bittern-storage.lock)
    FORMAT_MARKER=$(root_path /var/lib/bittern/storage-format-in-progress)

    if [ "$TEST_MODE" -eq 0 ]; then
        if [ -n "${BITTERN_STORAGE_LSBLK_FIXTURE:-}" ]; then
            die "BITTERN_STORAGE_LSBLK_FIXTURE is test-only; use it with --root in a throwaway tree"
        fi
        if [ "$FSTAB" != /etc/fstab ]; then
            die "--fstab is test-only; use it with --root in a throwaway tree"
        fi
    fi

    if [ "$JSON" -eq 1 ] && [ "$LIST" -eq 0 ]; then
        die "--json is only valid with --list"
    fi
    if [ "$LIST" -eq 1 ] && [ -n "$DEVICE" ]; then
        die "--list cannot be combined with --device"
    fi

    if [ -n "$DEVICE" ]; then
        case "$DEVICE" in
            /*)
                ;;
            *)
                die "--device must be an absolute path"
                ;;
        esac
    fi

    if [ -t 0 ] && [ "$DRY_RUN" -eq 0 ] && [ "$LIST" -eq 0 ]; then
        INTERACTIVE=1
    fi
}

check_environment() {
    for _command in findmnt mount stat awk sort grep sed mktemp chmod chown install sync readlink flock cksum date; do
        command -v "$_command" >/dev/null 2>&1 ||
            die "$_command is required"
    done
    if [ -z "${BITTERN_STORAGE_LSBLK_FIXTURE:-}" ]; then
        command -v lsblk >/dev/null 2>&1 ||
            die "lsblk is required so the script can enumerate block devices"
    else
        [ -f "$BITTERN_STORAGE_LSBLK_FIXTURE" ] ||
            die "lsblk fixture does not exist: $BITTERN_STORAGE_LSBLK_FIXTURE"
        [ -r "$BITTERN_STORAGE_LSBLK_FIXTURE" ] ||
            die "lsblk fixture is not readable: $BITTERN_STORAGE_LSBLK_FIXTURE"
    fi

    if [ "$LIST" -eq 0 ]; then
        command -v blkid >/dev/null 2>&1 ||
            die "blkid is required to read the filesystem UUID that is written to fstab; install util-linux and rerun"
    fi

    if [ "$TEST_MODE" -eq 0 ] && [ "$LIST" -eq 0 ]; then
        [ "$(id -u)" -eq 0 ] ||
            die "run as root, for example: sudo $0; use --list for a read-only inventory"
    elif [ "$TEST_MODE" -eq 1 ]; then
        warn "test root mode: file changes are limited to $ROOT; mount, findmnt, blkid and mkfs must be faked on PATH"
    fi
}

check_paths() {
    if [ -L "$MOUNT" ]; then
        die "refusing a symlink mountpoint: $MOUNT; remove the symlink and create a directory with: install -d -m 0755 $MOUNT"
    fi
    if [ -e "$MOUNT" ] && [ ! -d "$MOUNT" ]; then
        die "refusing a mountpoint that is not a directory: $MOUNT; move it aside and create a directory"
    fi
    if [ -d "$MOUNT" ]; then
        _uid=$(stat -c '%u' -- "$MOUNT" 2>/dev/null) ||
            die "cannot establish the owner of $MOUNT; fix the path and rerun"
        if [ "$TEST_MODE" -eq 0 ]; then
            [ "$_uid" = 0 ] ||
                die "refusing $MOUNT: it is owned by uid $_uid, not root; fix it with: chown root:root $MOUNT"
        else
            [ "$_uid" = "$(id -u)" ] ||
                die "refusing $MOUNT: it is owned by uid $_uid, not the test user; fix its ownership and rerun"
        fi
    fi
    _parent=${MOUNT%/*}
    if [ -n "$_parent" ] && [ "$_parent" != / ] && [ -L "$_parent" ]; then
        die "refusing $MOUNT: its parent $_parent is a symlink; replace the parent with a directory"
    fi

    if [ -L "$FSTAB" ]; then
        die "refusing a symlink fstab: $FSTAB; replace it with a regular fstab file"
    fi
    if [ -e "$FSTAB" ] && [ ! -f "$FSTAB" ]; then
        die "refusing an fstab that is not a regular file: $FSTAB; move it aside and create a regular file"
    fi
    if [ -f "$FSTAB" ]; then
        _uid=$(stat -c '%u' -- "$FSTAB" 2>/dev/null) ||
            die "cannot establish the owner of $FSTAB; fix the path and rerun"
        if [ "$TEST_MODE" -eq 0 ]; then
            [ "$_uid" = 0 ] ||
                die "refusing $FSTAB: it is owned by uid $_uid, not root; fix it with: chown root:root $FSTAB"
        else
            [ "$_uid" = "$(id -u)" ] ||
                die "refusing $FSTAB: it is owned by uid $_uid, not the test user; fix its ownership and rerun"
        fi
    fi
}

# Take the whole-run lock. fd 9 stays open for the life of the script, so the
# lock is released only when the run ends. --list is read-only and takes no
# lock and writes no file.
acquire_lock() {
    if [ "$LIST" -eq 1 ]; then
        return 0
    fi
    _lock_dir=${LOCK_FILE%/*}
    [ -d "$_lock_dir" ] || install -d -m 0755 "$_lock_dir" ||
        die "could not create the lock directory $_lock_dir"
    exec 9>"$LOCK_FILE" ||
        die "could not open the lock file $LOCK_FILE"
    flock -n 9 ||
        die "another storage-setup run is in progress (lock $LOCK_FILE); wait for it to finish and rerun"
}

pair_value() {
    _pair_line=$1
    _pair_key=$2
    case $_pair_line in
        "$_pair_key=\""*)
            _pair_rest=${_pair_line#"$_pair_key=\""}
            ;;
        *" $_pair_key=\""*)
            _pair_rest=${_pair_line#*" $_pair_key=\""}
            ;;
        *)
            return 1
            ;;
    esac
    printf '%s' "${_pair_rest%%\"*}"
}

dump_lsblk() {
    if [ -n "${BITTERN_STORAGE_LSBLK_FIXTURE:-}" ]; then
        cat -- "$BITTERN_STORAGE_LSBLK_FIXTURE"
    else
        lsblk -P -b -o "$LSBLK_COLUMNS"
    fi
}

index_devices() {
    RAW_FILE=$TEMP_DIR/lsblk.txt
    DEVICES_FILE=$TEMP_DIR/devices.tsv
    dump_lsblk > "$RAW_FILE" ||
        die "could not read block devices with lsblk"
    [ -s "$RAW_FILE" ] || die "lsblk returned no block devices"

    : > "$DEVICES_FILE"
    while IFS= read -r _line || [ -n "$_line" ]; do
        [ -n "$_line" ] || continue
        _name=$(trim "$(pair_value "$_line" NAME || true)")
        [ -n "$_name" ] || continue
        _path=$(trim "$(pair_value "$_line" PATH || true)")
        _type=$(trim "$(pair_value "$_line" TYPE || true)")
        _size=$(trim "$(pair_value "$_line" SIZE || true)")
        _model=$(trim "$(pair_value "$_line" MODEL || true)")
        _serial=$(trim "$(pair_value "$_line" SERIAL || true)")
        _tran=$(trim "$(pair_value "$_line" TRAN || true)")
        _label=$(trim "$(pair_value "$_line" LABEL || true)")
        _fstype=$(trim "$(pair_value "$_line" FSTYPE || true)")
        _uuid=$(trim "$(pair_value "$_line" UUID || true)")
        _mounts=$(trim "$(pair_value "$_line" MOUNTPOINTS || true)")
        _ro=$(trim "$(pair_value "$_line" RO || true)")
        _rm=$(trim "$(pair_value "$_line" RM || true)")
        _parent=$(trim "$(pair_value "$_line" PKNAME || true)")
        printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n' \
            "$_name" "$_path" "$_type" "$_size" "$_model" "$_serial" \
            "$_tran" "$_label" "$_fstype" "$_uuid" "$_mounts" "$_ro" \
            "$_rm" "$_parent" >> "$DEVICES_FILE"
    done < "$RAW_FILE"
}

field() {
    awk -F '\t' -v name="$1" -v col="$2" '$1 == name { print $col; exit }' "$DEVICES_FILE"
}

name_for_path() {
    _wanted=$1
    _wanted_canonical=$(canonical_device "$_wanted")
    awk -F '\t' -v wanted="$_wanted" -v canonical="$_wanted_canonical" \
        '$2 == wanted || $2 == canonical { print $1; exit }' "$DEVICES_FILE"
}

whole_disk_name() {
    _whole=$1
    _depth=0
    while [ "$_depth" -lt 16 ]; do
        _parent=$(field "$_whole" 14)
        if [ -z "$_parent" ]; then
            printf '%s' "$_whole"
            return 0
        fi
        _whole=$_parent
        _depth=$((_depth + 1))
    done
    printf '%s' "$_whole"
}

subtree_names() {
    awk -F '\t' -v disk="$1" '
        { parent[$1] = $14; order[NR] = $1 }
        END {
            for (i = 1; i <= NR; i++) {
                n = order[i]
                d = n
                depth = 0
                while (d != "" && depth < 16) {
                    if (d == disk) {
                        print n
                        break
                    }
                    d = parent[d]
                    depth++
                }
            }
        }
    ' "$DEVICES_FILE"
}

subtree_has_child() {
    _count=0
    for _name in $(subtree_names "$1"); do
        _count=$((_count + 1))
    done
    [ "$_count" -gt 1 ]
}

subtree_has_fs_below() {
    _fs_disk=$1
    for _name in $(subtree_names "$_fs_disk"); do
        [ "$_name" = "$_fs_disk" ] && continue
        if [ -n "$(field "$_name" 9)" ]; then
            return 0
        fi
    done
    return 1
}

subtree_has_mount_at() {
    _mount_disk=$1
    _mount_wanted=$2
    for _name in $(subtree_names "$_mount_disk"); do
        for _mount in $(field "$_name" 11); do
            [ "$_mount" = "$_mount_wanted" ] && return 0
        done
    done
    return 1
}

disk_mount_reason() {
    _reason_disk=$1
    _reason_allow=${2:-}
    for _name in $(subtree_names "$_reason_disk"); do
        _mounts=$(field "$_name" 11)
        [ -n "$_mounts" ] || continue
        for _mount in $_mounts; do
            if [ -n "$_reason_allow" ] && [ "$_mount" = "$_reason_allow" ]; then
                continue
            fi
            case "$_mount" in
                '[SWAP]')
                    printf 'has active swap on %s; disable it first with: swapoff -a' "$_name"
                    ;;
                *)
                    printf 'has a filesystem mounted at %s on %s; unmount it first with: umount %s' \
                        "$_mount" "$_name" "$_mount"
                    ;;
            esac
            return 1
        done
    done
    return 0
}

disk_unsafe_reason() {
    _unsafe_disk=$1
    for _name in $(subtree_names "$_unsafe_disk"); do
        _fstype=$(field "$_name" 9)
        _type=$(field "$_name" 3)
        case "$_fstype" in
            linux_raid_member|LVM2_member|crypto_LUKS)
                printf 'is part of RAID or LVM storage (%s on %s); remove it from the volume or choose another device' \
                    "$_fstype" "$_name"
                return 1
                ;;
        esac
        case "$_type" in
            raid*|lvm|crypt|mpath|dm)
                printf 'is part of RAID or LVM storage (%s %s); remove it from the volume or choose another device' \
                    "$_type" "$_name"
                return 1
                ;;
        esac
    done
    return 0
}

disk_boot_media_reason() {
    _boot_disk=$1
    for _name in $(subtree_names "$_boot_disk"); do
        _fstype=$(field "$_name" 9)
        _type=$(field "$_name" 3)
        _label=$(field "$_name" 8)
        _path=$(field "$_name" 2)
        case "$_fstype" in
            iso9660|udf)
                printf 'carries installer media (%s on %s); remove the installer media and rerun' \
                    "$_fstype" "$_name"
                return 1
                ;;
        esac
        case "$_type" in
            rom)
                printf 'is optical install media (%s); remove the installer media and rerun' "$_name"
                return 1
                ;;
        esac
        case "$_path" in
            /dev/sr*)
                printf 'is optical install media (%s); remove the installer media and rerun' "$_path"
                return 1
                ;;
        esac
        _lower_label=$(printf '%s' "$_label" | tr '[:upper:]' '[:lower:]')
        case "$_lower_label" in
            *debian*|*ubuntu*|*fedora*|*install*)
                printf 'carries installer media labelled %s on %s; remove the installer media and rerun' \
                    "$_label" "$_name"
                return 1
                ;;
        esac
    done
    return 0
}

# Print the whole-disk name that contains a root or boot source, or nothing
# when the source is not a listed block device.
disk_for_source() {
    _source_path=$(canonical_device "$1")
    _source_name=$(name_for_path "$_source_path")
    if [ -n "$_source_name" ]; then
        whole_disk_name "$_source_name"
        return 0
    fi
    case "$_source_path" in
        /dev/*)
            ;;
        *)
            return 1
            ;;
    esac
    _source_disk=
    _listed_file=$TEMP_DIR/system-listed-disks.txt
    awk -F '\t' '$3 == "disk" { print $1 }' "$DEVICES_FILE" > "$_listed_file"
    while IFS= read -r _listed || [ -n "$_listed" ]; do
        [ -n "$_listed" ] || continue
        _listed_path=$(field "$_listed" 2)
        case "$_source_path" in
            "$_listed_path"|"$_listed_path"p*|"$_listed_path"[0-9]*)
                _source_disk=$_listed
                ;;
        esac
    done < "$_listed_file"
    [ -n "$_source_disk" ] || return 1
    printf '%s' "$_source_disk"
}

# Collect every source that can back /, /boot or the ESP. findmnt is the
# primary source; /proc/self/mountinfo and root= in /proc/cmdline are
# cross-checks so an overlay, NFS or 9p root, or a container whose lsblk shows
# no MOUNTPOINTS, cannot hide the OS disk. ROOT_MAPPED stays 0 when no source
# for / resolves to a listed device, and the caller then fails closed.
collect_system_sources() {
    if [ -n "$SYSTEM_SOURCES_FILE" ] && [ -f "$SYSTEM_SOURCES_FILE" ]; then
        return 0
    fi
    SYSTEM_SOURCES_FILE=$TEMP_DIR/system-sources.tsv
    SYSTEM_DISKS_FILE=$TEMP_DIR/system-disks.txt
    : > "$SYSTEM_DISKS_FILE"
    _raw_sources=$TEMP_DIR/system-raw.tsv
    : > "$_raw_sources"

    for _mount in / /boot /boot/efi; do
        _source=$(findmnt -n -o SOURCE --target "$(root_path "$_mount")" 2>/dev/null || true)
        _source=${_source%%\[*}
        [ -n "$_source" ] || continue
        printf '%s\t%s\n' "$_mount" "$_source" >> "$_raw_sources"
    done

    _mountinfo=$(proc_path /proc/self/mountinfo || true)
    if [ -n "$_mountinfo" ]; then
        awk '
            {
                mountpoint = $5
                gsub(/\\040/, " ", mountpoint)
                if (mountpoint != "/" && mountpoint != "/boot" && mountpoint != "/boot/efi") next
                for (i = 7; i <= NF; i++) {
                    if ($i == "-" && i + 2 <= NF) {
                        print mountpoint "\t" $(i + 2)
                        break
                    }
                }
            }
        ' "$_mountinfo" >> "$_raw_sources" || true
    fi

    _cmdline=$(proc_path /proc/cmdline || true)
    if [ -n "$_cmdline" ]; then
        _root_specs=$TEMP_DIR/root-specs.txt
        awk '{
            for (i = 1; i <= NF; i++) {
                if ($i ~ /^root=/) {
                    sub(/^root=/, "", $i)
                    print $i
                }
            }
        }' "$_cmdline" > "$_root_specs" || true
        while IFS= read -r _root_spec || [ -n "$_root_spec" ]; do
            [ -n "$_root_spec" ] || continue
            _root_device=$(resolve_root_spec "$_root_spec" || true)
            [ -n "$_root_device" ] || continue
            printf '/\t%s\n' "$_root_device" >> "$_raw_sources"
        done < "$_root_specs"
    fi

    sort -u "$_raw_sources" > "$SYSTEM_SOURCES_FILE"

    ROOT_MAPPED=0
    while IFS="$TAB" read -r _mount _source; do
        [ -n "$_source" ] || continue
        _source_disk=$(disk_for_source "$_source" || true)
        [ -n "$_source_disk" ] || continue
        printf '%s\n' "$_source_disk" >> "$SYSTEM_DISKS_FILE"
        if [ "$_mount" = / ]; then
            ROOT_MAPPED=1
        fi
    done < "$SYSTEM_SOURCES_FILE"
}

# Refuse the whole inventory when the running root cannot be mapped to a listed
# block device. Offering the ESP or an OS partition as a recording target is
# the failure this prevents, so an unknown root offers nothing at all.
require_known_system_devices() {
    collect_system_sources
    if [ "$ROOT_MAPPED" -eq 1 ]; then
        return 0
    fi
    {
        printf 'error: cannot establish the running root device; refusing to offer any device as a recording target\n'
        printf 'findmnt did not map / to a listed block device, and /proc/self/mountinfo and root= in /proc/cmdline did not name one either.\n'
        printf 'a container, overlay, NFS or 9p root reports no single OS disk, so the ESP and the OS partitions cannot be excluded.\n'
        printf 'run this script from a normal block-device root, or fix findmnt, then rerun. --device does not override this check.\n'
    } >&2
    exit 1
}

# A FAT filesystem is what an EFI system partition uses. It is never a
# recording target, even when the ESP is not mounted and no root mapping names
# its disk.
node_boot_fs_reason() {
    case "$(field "$1" 9)" in
        vfat|fat|fat12|fat16|fat32|msdos)
            printf 'carries a FAT filesystem, which is what an EFI system partition uses; it is never a recording target, choose another device'
            return 0
            ;;
    esac
    return 1
}

disk_is_system_reason() {
    _system_disk=$1
    _system_path=$(field "$_system_disk" 2)
    collect_system_sources

    for _mount in / /boot /boot/efi; do
        if subtree_has_mount_at "$_system_disk" "$_mount"; then
            printf 'backs the running filesystem at %s; it is never a recording target, choose another device' "$_mount"
            return 0
        fi
    done

    if [ -f "$SYSTEM_DISKS_FILE" ] && grep -Fqx -- "$_system_disk" "$SYSTEM_DISKS_FILE" 2>/dev/null; then
        printf 'backs the running filesystem; it is never a recording target, choose another device'
        return 0
    fi

    if [ -f "$SYSTEM_SOURCES_FILE" ]; then
        while IFS="$TAB" read -r _mount _source; do
            [ -n "$_source" ] || continue
            _source=$(canonical_device "$_source")
            case "$_source" in
                "$_system_path"|"$_system_path"p*|"$_system_path"[0-9]*)
                    printf 'backs the running filesystem at %s; it is never a recording target, choose another device' "$_mount"
                    return 0
                    ;;
            esac
        done < "$SYSTEM_SOURCES_FILE"
    fi
    return 1
}

add_candidate_node() {
    _candidate_node=$1
    _candidate_node_type=$(field "$_candidate_node" 3)
    _candidate_path=$(field "$_candidate_node" 2)
    _candidate_size=$(field "$_candidate_node" 4)
    _candidate_label=$(field "$_candidate_node" 8)
    _candidate_fstype=$(field "$_candidate_node" 9)
    _candidate_uuid=$(field "$_candidate_node" 10)
    _candidate_disk=$(whole_disk_name "$_candidate_node")
    _candidate_disk_path=$(field "$_candidate_disk" 2)
    _candidate_disk_size=$(field "$_candidate_disk" 4)
    _candidate_model=$(field "$_candidate_disk" 5)
    _candidate_serial=$(field "$_candidate_disk" 6)
    _candidate_tran=$(field "$_candidate_disk" 7)

    _candidate_needs=1
    _candidate_class=other
    if [ "$_candidate_fstype" = ext4 ]; then
        _candidate_needs=0
        _candidate_class=ext4
        _candidate_note=ext4
    elif [ -n "$_candidate_fstype" ]; then
        _candidate_note=$_candidate_fstype
    elif [ "$_candidate_node_type" = disk ] && subtree_has_child "$_candidate_node"; then
        _candidate_note='partitioned, no filesystem'
        _candidate_class=blank
    else
        _candidate_note=blank
        _candidate_class=blank
    fi
    if [ "$_candidate_label" = "$MEDIA_LABEL" ]; then
        _candidate_class=label
    fi

    [ -n "$_candidate_model" ] || _candidate_model=-
    [ -n "$_candidate_serial" ] || _candidate_serial=-
    [ -n "$_candidate_tran" ] || _candidate_tran=-
    [ -n "$_candidate_label" ] || _candidate_label=-
    [ -n "$_candidate_uuid" ] || _candidate_uuid=-
    [ -n "$_candidate_fstype" ] || _candidate_fstype=none

    printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n' \
        "$_candidate_class" "$_candidate_node" "$_candidate_path" \
        "$_candidate_node_type" "$_candidate_size" "$_candidate_model" \
        "$_candidate_serial" "$_candidate_tran" "$_candidate_disk" \
        "$_candidate_disk_path" "$_candidate_disk_size" "$_candidate_fstype" \
        "$_candidate_label" "$_candidate_uuid" "$_candidate_needs" \
        "$_candidate_note" >> "$CANDIDATES_FILE"
}

skip_disk() {
    printf 'skip: %s (%s)\n' "$1" "$2" >&2
}

collect_candidates() {
    _collect_allow=${1:-}
    CANDIDATES_FILE=$TEMP_DIR/candidates.tsv
    SAFE_DISKS_FILE=$TEMP_DIR/safe-disks.txt
    : > "$CANDIDATES_FILE"
    : > "$SAFE_DISKS_FILE"
    _disk_list=$TEMP_DIR/disks.list
    _node_list=$TEMP_DIR/nodes.list
    awk -F '\t' '$3 == "disk" { print $1 }' "$DEVICES_FILE" > "$_disk_list"
    awk -F '\t' '$3 == "disk" || $3 == "part" { print $1 }' "$DEVICES_FILE" > "$_node_list"

    while IFS= read -r _disk || [ -n "$_disk" ]; do
        [ -n "$_disk" ] || continue
        _path=$(field "$_disk" 2)
        _size=$(field "$_disk" 4)
        _ro=$(field "$_disk" 12)

        case "$_path" in
            /dev/zram*|/dev/loop*|/dev/dm-*|/dev/md*)
                skip_disk "$_path" "not a physical whole disk"
                continue
                ;;
        esac
        [ "$_ro" = 0 ] || {
            skip_disk "$_path" "is read-only; make it writable or choose another device"
            continue
        }
        [ "$_size" -gt 0 ] || {
            skip_disk "$_path" "has no size; choose another device"
            continue
        }

        _reason=$(disk_is_system_reason "$_disk" || true)
        if [ -n "$_reason" ]; then
            skip_disk "$_path" "$_reason"
            continue
        fi
        _reason=$(disk_unsafe_reason "$_disk" || true)
        if [ -n "$_reason" ]; then
            skip_disk "$_path" "$_reason"
            continue
        fi
        _reason=$(disk_boot_media_reason "$_disk" || true)
        if [ -n "$_reason" ]; then
            skip_disk "$_path" "$_reason"
            continue
        fi
        _reason=$(disk_mount_reason "$_disk" "$_collect_allow" || true)
        if [ -n "$_reason" ]; then
            skip_disk "$_path" "$_reason"
            continue
        fi

        printf '%s\n' "$_disk" >> "$SAFE_DISKS_FILE"
    done < "$_disk_list"

    # A filesystem on a partition is a first-class candidate. A whole disk
    # with its own filesystem is a candidate too, and its children are not.
    while IFS= read -r _node || [ -n "$_node" ]; do
        [ -n "$_node" ] || continue
        _node_fstype=$(field "$_node" 9)
        [ -n "$_node_fstype" ] || continue
        _node_type=$(field "$_node" 3)
        _node_disk=$(whole_disk_name "$_node")
        grep -Fqx -- "$_node_disk" "$SAFE_DISKS_FILE" || continue
        if [ "$_node_type" = part ] && [ -n "$(field "$_node_disk" 9)" ]; then
            continue
        fi
        _node_ro=$(field "$_node" 12)
        if [ "$_node_ro" != 0 ]; then
            skip_disk "$(field "$_node" 2)" "is read-only; make it writable or choose another device"
            continue
        fi
        _node_size=$(field "$_node" 4)
        if [ "$_node_size" -le 0 ]; then
            skip_disk "$(field "$_node" 2)" "has no size; choose another device"
            continue
        fi
        _reason=$(node_boot_fs_reason "$_node" || true)
        if [ -n "$_reason" ]; then
            skip_disk "$(field "$_node" 2)" "$_reason"
            continue
        fi
        add_candidate_node "$_node"
    done < "$_node_list"

    # A disk with no filesystem anywhere in its tree is a format candidate.
    # A disk that only holds a partition table is not offered when one of its
    # partitions is already a candidate.
    while IFS= read -r _disk || [ -n "$_disk" ]; do
        [ -n "$_disk" ] || continue
        [ -z "$(field "$_disk" 9)" ] || continue
        subtree_has_fs_below "$_disk" && continue
        add_candidate_node "$_disk"
    done < "$SAFE_DISKS_FILE"

    sort -t "$TAB" -k3,3 "$CANDIDATES_FILE" > "$TEMP_DIR/candidates.sorted"
    mv -f -- "$TEMP_DIR/candidates.sorted" "$CANDIDATES_FILE"
}

select_labelled_candidate() {
    _label_count=$(awk -F '\t' '$1 == "label" { n++ } END { print n + 0 }' "$CANDIDATES_FILE")
    [ "$_label_count" = 1 ] || return 1
    awk -F '\t' '$1 == "label"' "$CANDIDATES_FILE" > "$SELECTED_FILE"
    [ -s "$SELECTED_FILE" ]
}

print_human_list() {
    if [ ! -s "$CANDIDATES_FILE" ]; then
        log "no eligible recording devices found"
        return 0
    fi
    log "eligible recording devices:"
    _number=0
    while IFS="$TAB" read -r _class _node_name _node_path _node_type \
        _node_size _model _serial _tran _disk_name _disk_path _disk_size \
        _filesystem _label _uuid _needs_format _note; do
        _number=$((_number + 1))
        _needs=no
        [ "$_needs_format" = 1 ] && _needs=yes
        log "  $_number. device=$_node_path type=$_node_type size=$(human_size "$_node_size") model=$_model filesystem=$_filesystem label=$_label needs_format=$_needs"
    done < "$CANDIDATES_FILE"
}

print_json() {
    printf '{\n  "schema_version": 1,\n  "devices": ['
    _first=1
    while IFS="$TAB" read -r _class _node_name _node_path _node_type \
        _node_size _model _serial _tran _disk_name _disk_path _disk_size \
        _filesystem _label _uuid _needs_format _note; do
        if [ "$_first" -eq 1 ]; then
            printf '\n'
            _first=0
        else
            printf ',\n'
        fi
        _needs=false
        [ "$_needs_format" = 1 ] && _needs=true
        printf '    {\n'
        printf '      "device": "%s",\n' "$(json_escape "$_node_path")"
        printf '      "type": "%s",\n' "$_node_type"
        printf '      "size_bytes": %s,\n' "$_node_size"
        printf '      "size": "%s",\n' "$(human_size "$_node_size")"
        printf '      "model": '
        print_json_string_or_null "$_model"
        printf ',\n'
        printf '      "serial": '
        print_json_string_or_null "$_serial"
        printf ',\n'
        printf '      "transport": '
        print_json_string_or_null "$_tran"
        printf ',\n'
        printf '      "filesystem": "%s",\n' "$_filesystem"
        printf '      "label": '
        print_json_string_or_null "$_label"
        printf ',\n'
        printf '      "filesystem_uuid": '
        print_json_string_or_null "$_uuid"
        printf ',\n'
        printf '      "needs_format": %s,\n' "$_needs"
        printf '      "disk": "%s",\n' "$(json_escape "$_disk_path")"
        printf '      "disk_size_bytes": %s,\n' "$_disk_size"
        printf '      "note": "%s"\n' "$(json_escape "$_note")"
        printf '    }'
    done < "$CANDIDATES_FILE"
    printf '\n  ]\n}\n'
}

select_candidate_by_number() {
    _selection=$1
    _count=$(grep -c '' "$CANDIDATES_FILE" 2>/dev/null || true)
    [ "$_selection" -ge 1 ] 2>/dev/null || return 1
    [ "$_selection" -le "${_count:-0}" ] 2>/dev/null || return 1
    awk -F '\t' -v wanted="$_selection" 'NR == wanted' "$CANDIDATES_FILE" > "$SELECTED_FILE"
    [ -s "$SELECTED_FILE" ]
}

interactive_select() {
    print_human_list
    _count=$(grep -c '' "$CANDIDATES_FILE" 2>/dev/null || true)
    while :; do
        printf 'Choose a recording device [1-%s]: ' "${_count:-0}" >&2
        IFS= read -r _answer || die "no device selected; nothing changed"
        case "$_answer" in
            ''|*[!0-9]*)
                printf 'enter a number between 1 and %s\n' "${_count:-0}" >&2
                continue
                ;;
        esac
        if [ "$_answer" -lt 1 ] || [ "$_answer" -gt "${_count:-0}" ]; then
            printf 'enter a number between 1 and %s\n' "${_count:-0}" >&2
            continue
        fi
        break
    done
    select_candidate_by_number "$_answer" ||
        die "internal error: selected device number is not in the list"
}

print_selection_required() {
    _required_reason=$1
    print_human_list
    printf 'error: no recording device was selected: %s\n' "$_required_reason" >&2
    printf 'use the list above, then run: sudo %s --device PATH\n' "$0" >&2
    printf 'add --format only when needs_format=yes and you accept that the device is destroyed.\n' >&2
    exit 4
}

load_selected_candidate() {
    [ -s "$SELECTED_FILE" ] || die "internal error: no recording device was selected"
    IFS="$TAB" read -r _CANDIDATE_CLASS _CANDIDATE_NODE_NAME CANDIDATE_NODE_PATH \
        CANDIDATE_NODE_TYPE CANDIDATE_NODE_SIZE CANDIDATE_MODEL CANDIDATE_SERIAL \
        _CANDIDATE_TRAN _CANDIDATE_DISK_NAME CANDIDATE_DISK_PATH CANDIDATE_DISK_SIZE \
        _CANDIDATE_FS CANDIDATE_LABEL CANDIDATE_UUID CANDIDATE_NEEDS_FORMAT \
        CANDIDATE_NOTE < "$SELECTED_FILE"

    FS_DEVICE=$CANDIDATE_NODE_PATH
    FS_DEVICE_TYPE=$CANDIDATE_NODE_TYPE
    FS_DEVICE_SIZE=$CANDIDATE_NODE_SIZE
    FS_DISK=$CANDIDATE_DISK_PATH
    FS_DISK_SIZE=$CANDIDATE_DISK_SIZE
    FS_MODEL=$CANDIDATE_MODEL
    FS_SERIAL=$CANDIDATE_SERIAL
    FS_LABEL=$CANDIDATE_LABEL
    FS_UUID=$CANDIDATE_UUID
    FS_INVENTORY_UUID=$CANDIDATE_UUID
    FS_DEVICE_FSTYPE=$_CANDIDATE_FS
    [ "$FS_DEVICE_FSTYPE" = none ] && FS_DEVICE_FSTYPE=
    NEEDS_FORMAT=$CANDIDATE_NEEDS_FORMAT
    [ "$FS_MODEL" = - ] && FS_MODEL=
    [ "$FS_SERIAL" = - ] && FS_SERIAL=
    [ "$FS_LABEL" = - ] && FS_LABEL=
    [ "$FS_UUID" = - ] && FS_UUID=
    CURRENT_MOUNT=0
}

select_explicit_candidate() {
    _explicit_name=$(name_for_path "$DEVICE")
    if [ -z "$_explicit_name" ]; then
        die "--device does not name a block device in lsblk: $DEVICE; run --list and use the device path from it"
    fi
    _explicit_type=$(field "$_explicit_name" 3)
    case "$_explicit_type" in
        disk|part)
            ;;
        *)
            die "--device must name a whole disk or partition, not a $_explicit_type device: $DEVICE; run --list"
            ;;
    esac
    _explicit_path=$(field "$_explicit_name" 2)
    _explicit_disk=$(whole_disk_name "$_explicit_name")
    _explicit_disk_path=$(field "$_explicit_disk" 2)
    _explicit_size=$(field "$_explicit_name" 4)
    if [ "${_explicit_size:-0}" -le 0 ]; then
        die "refusing $_explicit_path: it has no size; choose another device"
    fi

    if [ "$(field "$_explicit_disk" 12)" != 0 ]; then
        die "refusing $_explicit_path: its disk $_explicit_disk_path is read-only; make it writable or choose another device"
    fi
    if [ "$(field "$_explicit_name" 12)" != 0 ]; then
        die "refusing $_explicit_path: it is read-only; make it writable or choose another device"
    fi
    _reason=$(node_boot_fs_reason "$_explicit_name" || true)
    [ -z "$_reason" ] || die "refusing $_explicit_path: $_reason"
    _reason=$(disk_is_system_reason "$_explicit_disk" || true)
    [ -z "$_reason" ] || die "refusing $_explicit_path: $_reason"
    _reason=$(disk_unsafe_reason "$_explicit_disk" || true)
    [ -z "$_reason" ] || die "refusing $_explicit_path: $_reason"
    _reason=$(disk_boot_media_reason "$_explicit_disk" || true)
    [ -z "$_reason" ] || die "refusing $_explicit_path: $_reason"
    _reason=$(disk_mount_reason "$_explicit_disk" || true)
    [ -z "$_reason" ] || die "refusing $_explicit_path: $_reason"

    : > "$CANDIDATES_FILE"
    if [ "$_explicit_type" = part ]; then
        if [ -n "$(field "$_explicit_disk" 9)" ]; then
            die "refusing $_explicit_path: its disk $_explicit_disk_path already carries a filesystem; use --device $_explicit_disk_path"
        fi
        add_candidate_node "$_explicit_name"
    elif [ -n "$(field "$_explicit_name" 9)" ]; then
        add_candidate_node "$_explicit_name"
    else
        _descendants=$TEMP_DIR/explicit-descendants
        : > "$_descendants"
        for _name in $(subtree_names "$_explicit_name"); do
            [ "$_name" = "$_explicit_name" ] && continue
            [ -n "$(field "$_name" 9)" ] || continue
            printf '%s\t%s\n' "$(field "$_name" 2)" "$(field "$_name" 9)" >> "$_descendants"
        done
        if [ ! -s "$_descendants" ]; then
            add_candidate_node "$_explicit_name"
        else
            # The whole disk has no filesystem of its own, so the real target is
            # a partition that was never named. Never widen the approval from
            # the disk to that partition: refuse and name the exact commands.
            printf 'error: refusing %s: it has no filesystem of its own; the filesystem is on one of its partitions\n' \
                "$_explicit_path" >&2
            if [ "$FORMAT" -eq 1 ]; then
                printf '%s\n' '--format would format a partition that was not named; name the partition itself.' >&2
            fi
            while IFS="$TAB" read -r _partition _fstype; do
                printf '  choose %s (%s): sudo %s --device %s\n' \
                    "$_partition" "$_fstype" "$0" "$_partition" >&2
            done < "$_descendants"
            exit 4
        fi
    fi

    cp -- "$CANDIDATES_FILE" "$SELECTED_FILE"
    [ -s "$SELECTED_FILE" ] ||
        die "internal error: could not describe $_explicit_path"
}

media_is_mount() {
    [ -d "$MOUNT" ] || return 1
    _target=$(findmnt -n -o TARGET --target "$MOUNT" 2>/dev/null) || return 1
    [ "$(canonical_path "$_target")" = "$(canonical_path "$MOUNT")" ]
}

select_current_mount() {
    _source=$(findmnt -n -o SOURCE --target "$MOUNT" 2>/dev/null) ||
        die "cannot read the mounted source at $MOUNT; unmount $MOUNT and rerun"
    _source=${_source%%\[*}
    _fstype=$(findmnt -n -o FSTYPE --target "$MOUNT" 2>/dev/null || true)
    [ "$_fstype" = ext4 ] ||
        die "refusing $MOUNT: it is mounted with filesystem ${_fstype:-unknown}, not ext4; unmount $MOUNT and choose an ext4 device"

    _source_name=$(name_for_path "$_source")
    if [ -z "$_source_name" ]; then
        die "refusing $MOUNT: cannot establish the whole disk for $_source; it may be LVM, RAID or a virtual device. Unmount $MOUNT and choose a whole disk or partition with --device"
    fi
    _source_disk=$(whole_disk_name "$_source_name")
    _disk_path=$(field "$_source_disk" 2)

    if [ -n "$DEVICE" ]; then
        _explicit_name=$(name_for_path "$DEVICE")
        [ -n "$_explicit_name" ] ||
            die "--device does not name a block device in lsblk: $DEVICE; run --list"
        _explicit_type=$(field "$_explicit_name" 3)
        if [ "$_explicit_type" = part ]; then
            [ "$_explicit_name" = "$_source_name" ] ||
                die "$MOUNT is already mounted from $_source; unmount it first with: umount $MOUNT"
        else
            [ "$(whole_disk_name "$_explicit_name")" = "$_source_disk" ] ||
                die "$MOUNT is already mounted from $_source; unmount it first with: umount $MOUNT"
        fi
    fi

    _reason=$(disk_is_system_reason "$_source_disk" || true)
    [ -z "$_reason" ] || die "refusing $_source: $_reason"
    _reason=$(disk_unsafe_reason "$_source_disk" || true)
    [ -z "$_reason" ] || die "refusing $_source: $_reason"
    _reason=$(disk_boot_media_reason "$_source_disk" || true)
    [ -z "$_reason" ] || die "refusing $_source: $_reason"
    _reason=$(disk_mount_reason "$_source_disk" "$MOUNT" || true)
    [ -z "$_reason" ] || die "refusing $_source: $_reason"

    _root_source=$(findmnt -n -o SOURCE --target "$(root_path /)" 2>/dev/null || true)
    if [ -n "$_root_source" ] &&
        [ "$(canonical_device "${_root_source%%\[*}")" = "$(canonical_device "$_source")" ]; then
        die "refusing $MOUNT: it is on the root filesystem; unmount it and choose a dedicated device"
    fi

    FS_DEVICE=$(field "$_source_name" 2)
    FS_DEVICE_TYPE=$(field "$_source_name" 3)
    FS_DEVICE_SIZE=$(field "$_source_name" 4)
    FS_DISK=$_disk_path
    FS_DISK_SIZE=$(field "$_source_disk" 4)
    FS_MODEL=$(field "$_source_disk" 5)
    FS_SERIAL=$(field "$_source_disk" 6)
    FS_LABEL=$(field "$_source_name" 8)
    FS_UUID=$(field "$_source_name" 10)
    FS_INVENTORY_UUID=$FS_UUID
    FS_DEVICE_FSTYPE=$(field "$_source_name" 9)
    NEEDS_FORMAT=0
    CANDIDATE_NOTE=ext4
    CURRENT_MOUNT=1
}

fstab_line_for_uuid() {
    printf 'UUID=%s %s ext4 %s %s %s' \
        "$1" "$MOUNT" "$FSTAB_OPTIONS" "$FSTAB_DUMP" "$FSTAB_PASS"
}

# Normalise field 2 so one directory always yields one line: collapse repeated
# separators and drop trailing slashes, but keep / itself.
AWK_NORM_MOUNT='
function norm_mount(p) {
    gsub(/\/+/, "/", p)
    while (length(p) > 1 && substr(p, length(p), 1) == "/") {
        p = substr(p, 1, length(p) - 1)
    }
    return p
}
'

# Content stamp for the fstab. The run stamps the file before any work and
# re-checks the stamp immediately before the rename, so an edit made while the
# run is in flight is never overwritten.
fstab_stamp() {
    if [ -f "$FSTAB" ]; then
        cksum < "$FSTAB" 2>/dev/null || printf 'unreadable'
        return 0
    fi
    printf 'absent'
}

fstab_mountpoint_count() {
    [ -f "$FSTAB" ] || {
        printf '0'
        return 0
    }
    awk -v mp="$MOUNT" "$AWK_NORM_MOUNT"'
        /^[[:space:]]*#/ { next }
        NF >= 2 && norm_mount($2) == norm_mount(mp) { n++ }
        END { print n + 0 }
    ' "$FSTAB"
}

fstab_exact_count() {
    [ -f "$FSTAB" ] || {
        printf '0'
        return 0
    }
    _count=$(grep -Fxc -- "$FSTAB_LINE" "$FSTAB" 2>/dev/null || true)
    printf '%s' "${_count:-0}"
}

fstab_is_correct() {
    [ "$(fstab_mountpoint_count)" = 1 ] && [ "$(fstab_exact_count)" = 1 ]
}

print_existing_fstab_lines() {
    [ -f "$FSTAB" ] || return 0
    awk -v mp="$MOUNT" "$AWK_NORM_MOUNT"'
        /^[[:space:]]*#/ { next }
        NF >= 2 && norm_mount($2) == norm_mount(mp) { print "existing fstab line: " $0 }
    ' "$FSTAB"
}

# Put the fstab back the way it was when a run fails after the rename.
rollback_fstab() {
    [ "$FSTAB_COMMITTED" -eq 1 ] || return 0
    FSTAB_COMMITTED=0
    if [ "$FSTAB_HAD_FILE" -eq 1 ] && [ -n "$FSTAB_BACKUP" ] && [ -f "$FSTAB_BACKUP" ]; then
        if cp -p -- "$FSTAB_BACKUP" "$FSTAB"; then
            sync -- "$FSTAB" 2>/dev/null || true
            printf 'restored %s after the failed verification; the previous content is back\n' "$FSTAB" >&2
        else
            printf 'error: could not restore %s from %s; check the file by hand\n' "$FSTAB" "$FSTAB_BACKUP" >&2
        fi
    else
        if rm -f -- "$FSTAB"; then
            printf 'removed %s after the failed verification; there was no fstab before this run\n' "$FSTAB" >&2
        else
            printf 'error: could not remove the fstab line from %s; remove it by hand\n' "$FSTAB" >&2
        fi
    fi
    sync -- "${FSTAB%/*}" 2>/dev/null || true
}

update_fstab() {
    if fstab_is_correct; then
        log "keep fstab line: $FSTAB_LINE"
        return 0
    fi

    _fstab_action='write'
    _existing=$(fstab_mountpoint_count)
    if [ "$_existing" -gt 0 ]; then
        _fstab_action=replace
        print_existing_fstab_lines
        log "+ replace $_existing fstab line(s) for $MOUNT"
    fi
    log "+ $_fstab_action $FSTAB line: $FSTAB_LINE"
    if [ "$DRY_RUN" -eq 1 ]; then
        return 0
    fi

    # The mount, mode, UUID and root checks must pass before the line is
    # committed. A line for a device that is not mounted correctly is worse
    # than no line at all: it leaves the media path on the OS disk, and the
    # mount assertion in the Frigate unit then refuses to start it.
    verify_mount_state

    _fstab_dir=${FSTAB%/*}
    [ -d "$_fstab_dir" ] || install -d -m 0755 "$_fstab_dir" ||
        die "could not create the fstab directory $_fstab_dir"
    FSTAB_HAD_FILE=0
    FSTAB_BACKUP=
    if [ -f "$FSTAB" ]; then
        FSTAB_HAD_FILE=1
        FSTAB_BACKUP=$TEMP_DIR/fstab.before
        cp -p -- "$FSTAB" "$FSTAB_BACKUP" ||
            die "could not save $FSTAB before replacing it; nothing was written"
    fi
    _temporary=$(mktemp "${FSTAB}.bittern.XXXXXX") ||
        die "could not create a temporary fstab beside $FSTAB"
    if [ "$FSTAB_HAD_FILE" -eq 1 ]; then
        awk -v mp="$MOUNT" -v line="$FSTAB_LINE" "$AWK_NORM_MOUNT"'
            /^[[:space:]]*#/ || NF < 2 { print; next }
            norm_mount($2) == norm_mount(mp) {
                if (!seen) {
                    print line
                    seen = 1
                }
                next
            }
            { print }
            END {
                if (!seen) {
                    print line
                }
            }
        ' "$FSTAB" > "$_temporary" || {
            rm -f -- "$_temporary"
            die "could not write the temporary fstab $_temporary"
        }
        chmod --reference="$FSTAB" -- "$_temporary" 2>/dev/null ||
            chmod 0644 -- "$_temporary"
        if [ "$(id -u)" -eq 0 ]; then
            chown --reference="$FSTAB" -- "$_temporary" 2>/dev/null || true
        fi
    else
        printf '%s\n' "$FSTAB_LINE" > "$_temporary" || {
            rm -f -- "$_temporary"
            die "could not write the temporary fstab $_temporary"
        }
        chmod 0644 -- "$_temporary"
    fi
    sync -- "$_temporary" || {
        rm -f -- "$_temporary"
        die "could not flush the temporary fstab $_temporary; nothing was written"
    }
    [ "$(fstab_stamp)" = "$FSTAB_STAMP" ] || {
        rm -f -- "$_temporary"
        die "$FSTAB changed while this run was in progress; nothing was written. Rerun to pick up the new content"
    }
    mv -f -- "$_temporary" "$FSTAB" ||
        die "could not install the new fstab at $FSTAB"
    FSTAB_COMMITTED=1
    sync -- "$_fstab_dir" 2>/dev/null ||
        warn "could not flush the fstab directory $_fstab_dir; the rename may not survive a power loss"
}

ensure_mountpoint() {
    if [ -d "$MOUNT" ]; then
        return 0
    fi
    if [ -e "$MOUNT" ] || [ -L "$MOUNT" ]; then
        die "refusing a mountpoint that is not a directory: $MOUNT; move it aside and create a directory"
    fi
    log "+ install -d -m 0755 $MOUNT"
    if [ "$DRY_RUN" -eq 0 ]; then
        install -d -m 0755 "$MOUNT" ||
            die "could not create the mountpoint $MOUNT"
    fi
}

mount_target() {
    log "+ mount $FS_DEVICE $MOUNT"
    if [ "$DRY_RUN" -eq 0 ]; then
        mount "$FS_DEVICE" "$MOUNT" ||
            die "could not mount $FS_DEVICE at $MOUNT; check dmesg and the filesystem"
    fi
}

set_mount_mode() {
    if [ ! -d "$MOUNT" ]; then
        log "+ chmod 0755 $MOUNT"
        return 0
    fi
    _mode=$(stat -c '%a' -- "$MOUNT" 2>/dev/null) ||
        die "cannot establish the mode of $MOUNT; fix the path and rerun"
    if [ "$_mode" = 755 ]; then
        return 0
    fi
    log "+ chmod 0755 $MOUNT"
    if [ "$DRY_RUN" -eq 0 ]; then
        chmod 0755 -- "$MOUNT" ||
            die "could not set mode 0755 on $MOUNT"
    fi
}

# blkid is the authority for the UUID: lsblk is udev-backed and can still show
# the previous filesystem's UUID right after mkfs. The lsblk value is only a
# cross-check and is used when blkid has nothing to say about a reused device.
read_uuid() {
    _uuid_device=$1
    _uuid_known=${2:-}
    _uuid_blkid=
    _uuid_lsblk=

    if command -v blkid >/dev/null 2>&1; then
        _uuid_blkid=$(blkid -s UUID -o value "$_uuid_device" 2>/dev/null | head -n 1 || true)
    fi
    if [ -z "$_uuid_known" ] && [ -z "${BITTERN_STORAGE_LSBLK_FIXTURE:-}" ] &&
        command -v lsblk >/dev/null 2>&1; then
        _uuid_lsblk=$(lsblk -n -o UUID "$_uuid_device" 2>/dev/null | head -n 1 || true)
        _uuid_known=$_uuid_lsblk
    fi
    _uuid_known=$(trim "$_uuid_known")
    _uuid_blkid=$(trim "$_uuid_blkid")

    if [ -n "$_uuid_known" ] && [ -n "$_uuid_blkid" ] &&
        [ "$_uuid_known" != "$_uuid_blkid" ]; then
        warn "UUID for $_uuid_device differs: lsblk reports $_uuid_known, blkid reports $_uuid_blkid; using the blkid value"
        printf '%s' "$_uuid_blkid"
        return 0
    fi
    if [ -n "$_uuid_blkid" ]; then
        printf '%s' "$_uuid_blkid"
        return 0
    fi
    if [ -n "$_uuid_known" ]; then
        printf '%s' "$_uuid_known"
        return 0
    fi
    return 1
}

settle_udev() {
    if [ "$TEST_MODE" -eq 1 ]; then
        return 0
    fi
    command -v udevadm >/dev/null 2>&1 || return 0
    udevadm settle --timeout=15 2>/dev/null ||
        warn "udevadm settle did not finish in time; the new UUID is read from blkid"
}

# Read the UUID of a filesystem this run has just created. A stale udev value
# here would put the wrong UUID in fstab, which fails the boot mount, so this
# path requires blkid and refuses an empty answer.
read_uuid_after_format() {
    command -v blkid >/dev/null 2>&1 ||
        die "blkid is required to read the new filesystem UUID of $1; install util-linux and rerun"
    _uuid_new=$(blkid -s UUID -o value "$1" 2>/dev/null | head -n 1 || true)
    _uuid_new=$(trim "$_uuid_new")
    [ -n "$_uuid_new" ] ||
        die "blkid cannot read a filesystem UUID from $1 after formatting; the format may be incomplete. Rerun with --format to reformat it"
    printf '%s' "$_uuid_new"
}

# The marker names the device whose format started and did not finish. It is
# written before mkfs and removed only after the new UUID has been read, so a
# power cut or an I/O error leaves evidence the next run can act on.
marker_field() {
    [ -f "$FORMAT_MARKER" ] || return 1
    sed -n "s/^$1=//p" "$FORMAT_MARKER" | head -n 1
}

write_format_marker() {
    _marker_dir=${FORMAT_MARKER%/*}
    install -d -m 0750 "$_marker_dir" ||
        die "could not create $_marker_dir for the format marker"
    if ! (umask 077 && printf 'device=%s\nserial=%s\nmodel=%s\nsize=%s\ndisk=%s\nstarted=%s\n' \
        "$FS_DEVICE" "$FS_SERIAL" "$FS_MODEL" "$FS_DEVICE_SIZE" "$FS_DISK" \
        "$(date -u +%Y-%m-%dT%H:%M:%SZ)" > "$FORMAT_MARKER"); then
        die "could not write the format marker $FORMAT_MARKER"
    fi
    chmod 0600 -- "$FORMAT_MARKER" 2>/dev/null || true
    sync -- "$FORMAT_MARKER" 2>/dev/null ||
        warn "could not flush the format marker $FORMAT_MARKER; an interrupted format may not be detected"
    sync -- "$_marker_dir" 2>/dev/null || true
}

clear_format_marker() {
    [ -f "$FORMAT_MARKER" ] || return 0
    rm -f -- "$FORMAT_MARKER" ||
        warn "could not remove the format marker $FORMAT_MARKER; the next run will ask before reusing $FS_DEVICE"
    sync -- "${FORMAT_MARKER%/*}" 2>/dev/null || true
}

marker_matches_selection() {
    [ -f "$FORMAT_MARKER" ] || return 1
    _marker_device=$(marker_field device || true)
    _marker_size=$(marker_field size || true)
    _marker_serial=$(marker_field serial || true)
    [ "$_marker_size" = "$FS_DEVICE_SIZE" ] || return 1
    if [ "$_marker_device" = "$FS_DEVICE" ]; then
        return 0
    fi
    if [ -n "$_marker_serial" ] && [ -n "$FS_SERIAL" ] && [ "$_marker_serial" = "$FS_SERIAL" ]; then
        return 0
    fi
    return 1
}

detect_pending_format() {
    PENDING_FORMAT=0
    [ -f "$FORMAT_MARKER" ] || return 0
    if ! marker_matches_selection; then
        warn "ignoring the format marker $FORMAT_MARKER: it names $(marker_field device || printf 'another device'), not $FS_DEVICE"
        return 0
    fi
    PENDING_FORMAT=1
    warn "a previous format of $FS_DEVICE did not finish; its ext4 filesystem may be incomplete"
    if [ "$CURRENT_MOUNT" -eq 1 ]; then
        die "refusing to keep $FS_DEVICE mounted at $MOUNT: the previous format did not finish and the filesystem may be incomplete; unmount it with: umount $MOUNT, then rerun with --format --device $FS_DEVICE"
    fi
    if [ "$NEEDS_FORMAT" -eq 0 ]; then
        NEEDS_FORMAT=1
        CANDIDATE_NOTE='interrupted format'
    fi
}

format_target() {
    command -v mkfs.ext4 >/dev/null 2>&1 ||
        die "mkfs.ext4 is required to format $FS_DEVICE; install e2fsprogs and rerun"
    if [ -n "$DEVICE" ]; then
        _approved=$(canonical_device "$DEVICE")
        _format_path=$(canonical_device "$FS_DEVICE")
        [ "$_approved" = "$_format_path" ] ||
            die "refusing to format $_format_path: --format approved $_approved, not $_format_path"
    fi
    _previous_uuid=$FS_UUID
    log "formatting $FS_DEVICE as ext4; this destroys all data on $FS_DEVICE"
    log "+ mkfs.ext4 -F -L $MEDIA_LABEL $FS_DEVICE"
    if [ "$DRY_RUN" -eq 1 ]; then
        return 0
    fi
    write_format_marker
    mkfs.ext4 -F -L "$MEDIA_LABEL" "$FS_DEVICE" ||
        die "mkfs.ext4 failed on $FS_DEVICE; the device may be partially formatted. Rerun with --format to reformat it"
    settle_udev
    FS_UUID=$(read_uuid_after_format "$FS_DEVICE")
    if [ -n "$_previous_uuid" ]; then
        [ "$FS_UUID" != "$_previous_uuid" ] ||
            die "the filesystem UUID after formatting $FS_DEVICE is still $_previous_uuid, the value before formatting; udev has not caught up and fstab would name the old filesystem. Wait a moment and rerun"
    fi
    clear_format_marker
    FSTAB_LINE=$(fstab_line_for_uuid "$FS_UUID")
}

interactive_format_approval() {
    {
        printf 'Formatting %s destroys all data on it.\n' "$FS_DEVICE"
        printf 'device: %s (%s)\n' "$FS_DEVICE" "$FS_DEVICE_TYPE"
        printf 'size: %s (%s bytes)\n' "$(human_size "$FS_DEVICE_SIZE")" "$FS_DEVICE_SIZE"
        printf 'disk: %s\n' "$FS_DISK"
        printf 'model: %s\n' "${FS_MODEL:--}"
        printf 'serial: %s\n' "${FS_SERIAL:--}"
        printf 'filesystem: %s\n' "${CANDIDATE_NOTE:-none}"
    } >&2

    if [ "$ASSUME_YES" -eq 1 ]; then
        _format_answer=y
    else
        printf 'Format this device? [y/N]: ' >&2
        IFS= read -r _format_answer || _format_answer=
    fi
    case "$_format_answer" in
        y|Y)
            ;;
        *)
            printf 'error: format not approved; nothing was changed\n' >&2
            exit 3
            ;;
    esac

    printf 'Type the device path (%s) or --format to confirm: ' "$FS_DEVICE" >&2
    IFS= read -r _confirmation || _confirmation=
    if [ "$_confirmation" != "$FS_DEVICE" ] && [ "$_confirmation" != --format ]; then
        die "format not confirmed; $FS_DEVICE was not touched. Type $FS_DEVICE or --format to confirm"
    fi
}

refuse_needs_format() {
    {
        printf 'error: refusing to format %s without approval\n' "$FS_DEVICE"
        printf 'device: %s (%s)\n' "$FS_DEVICE" "$FS_DEVICE_TYPE"
        printf 'size: %s (%s bytes)\n' "$(human_size "$FS_DEVICE_SIZE")" "$FS_DEVICE_SIZE"
        printf 'disk: %s\n' "$FS_DISK"
        printf 'model: %s\n' "${FS_MODEL:--}"
        printf 'serial: %s\n' "${FS_SERIAL:--}"
        printf 'filesystem: %s\n' "${CANDIDATE_NOTE:-unknown}"
        printf 'Formatting destroys the contents of %s.\n' "$FS_DEVICE"
        if [ "$ASSUME_YES" -eq 1 ]; then
            printf '%s\n' "--yes only confirms an action; --format is required."
        else
            printf '%s\n' "--format is required; --yes alone is not enough."
        fi
        printf 'Run exactly:\n'
        printf '  sudo %s --format --device %s\n' "$0" "$FS_DEVICE"
    } >&2
    exit 3
}

refuse_interrupted_format() {
    {
        printf 'error: a previous format of %s did not finish; its ext4 filesystem may be incomplete\n' "$FS_DEVICE"
        printf 'device: %s (%s)\n' "$FS_DEVICE" "$FS_DEVICE_TYPE"
        printf 'size: %s (%s bytes)\n' "$(human_size "$FS_DEVICE_SIZE")" "$FS_DEVICE_SIZE"
        printf 'disk: %s\n' "$FS_DISK"
        printf 'model: %s\n' "${FS_MODEL:--}"
        printf 'serial: %s\n' "${FS_SERIAL:--}"
        printf 'marker: %s\n' "$FORMAT_MARKER"
        printf 'The script will not reuse or mount a partially formatted %s.\n' "$FS_DEVICE"
        printf 'Reformat it only when you accept that the incomplete contents are destroyed:\n'
        printf '  sudo %s --format --device %s\n' "$0" "$FS_DEVICE"
    } >&2
    exit 3
}

# Everything except the fstab line: the mount, its source and filesystem, the
# mode, the root check and the live UUID. update_fstab calls this before the
# rename, so a line is never committed for a mount that is not what it claims.
verify_mount_state() {
    if ! media_is_mount; then
        die "verification failed: findmnt does not show $FS_DEVICE mounted at $MOUNT"
    fi
    _target=$(findmnt -n -o TARGET --target "$MOUNT" 2>/dev/null || true)
    [ "$(canonical_path "$_target")" = "$(canonical_path "$MOUNT")" ] ||
        die "verification failed: findmnt shows $_target, not $MOUNT"
    _source=$(findmnt -n -o SOURCE --target "$MOUNT" 2>/dev/null || true)
    _source=${_source%%\[*}
    [ "$(canonical_device "$_source")" = "$(canonical_device "$FS_DEVICE")" ] ||
        die "verification failed: findmnt shows $_source at $MOUNT, expected $FS_DEVICE"
    _fstype=$(findmnt -n -o FSTYPE --target "$MOUNT" 2>/dev/null || true)
    [ "$_fstype" = ext4 ] ||
        die "verification failed: $MOUNT carries filesystem ${_fstype:-unknown}, not ext4"
    _mode=$(stat -c '%a' -- "$MOUNT" 2>/dev/null) ||
        die "verification failed: cannot establish the mode of $MOUNT"
    [ "$_mode" = 755 ] ||
        die "verification failed: $MOUNT has mode $_mode, expected 0755"

    _root_source=$(findmnt -n -o SOURCE --target "$(root_path /)" 2>/dev/null || true)
    if [ -n "$_root_source" ] &&
        [ "$(canonical_device "${_root_source%%\[*}")" = "$(canonical_device "$_source")" ]; then
        die "verification failed: $MOUNT is on the root filesystem"
    fi
    _current_uuid=$(read_uuid "$FS_DEVICE" "$FS_UUID") ||
        die "verification failed: could not read the filesystem UUID of $FS_DEVICE"
    [ "$_current_uuid" = "$FS_UUID" ] ||
        die "verification failed: fstab UUID $FS_UUID does not match the filesystem UUID $_current_uuid"
}

verify_state() {
    verify_mount_state

    _fstab_count=$(fstab_mountpoint_count)
    [ "$_fstab_count" = 1 ] ||
        die "verification failed: $FSTAB has $_fstab_count lines for $MOUNT; expected exactly one"
    _fstab_exact=$(fstab_exact_count)
    [ "$_fstab_exact" = 1 ] ||
        die "verification failed: expected fstab line missing or duplicated in $FSTAB; found $_fstab_exact exact matches for: $FSTAB_LINE"

    if [ "$DRY_RUN" -eq 0 ]; then
        PROBE_FILE=$(mktemp "$MOUNT/.bittern-storage-write.XXXXXX") ||
            die "verification failed: could not create a write-test file at $MOUNT"
        (umask 077 && printf '%s\n' "bittern storage write test" > "$PROBE_FILE") ||
            die "verification failed: could not write to $MOUNT"
        sync -- "$PROBE_FILE" ||
            die "verification failed: sync failed for $PROBE_FILE"
        rm -f -- "$PROBE_FILE" ||
            die "verification failed: could not remove the write-test file $PROBE_FILE"
        PROBE_FILE=
    fi

    # The run is verified. A later failure must not roll the fstab back.
    FSTAB_COMMITTED=0
    log "recording storage verified: $FS_DEVICE at $MOUNT"
    log "fstab: $FSTAB_LINE"
}

cleanup() {
    if [ -n "$PROBE_FILE" ]; then
        rm -f -- "$PROBE_FILE" 2>/dev/null || true
    fi
    if [ -n "$TEMP_DIR" ]; then
        rm -rf -- "$TEMP_DIR"
    fi
}

# Prove the selected device is still the same object immediately before it is
# formatted or mounted. The kernel can reassign /dev/sdX after a replug, so the
# name is not the identity: serial, model, size, UUID and the mount, RAID and
# system checks are re-read from a fresh inventory and compared to the
# selection. The by-id path is resolved again here, at the last moment.
recheck_selected_device() {
    settle_udev
    index_devices
    _recheck_name=$(name_for_path "$FS_DEVICE")
    [ -n "$_recheck_name" ] ||
        die "refusing $FS_DEVICE: it is no longer in the block-device inventory; nothing was changed"
    _recheck_path=$(field "$_recheck_name" 2)
    _recheck_type=$(field "$_recheck_name" 3)
    _recheck_size=$(field "$_recheck_name" 4)
    _recheck_fstype=$(field "$_recheck_name" 9)
    _recheck_uuid=$(field "$_recheck_name" 10)
    _recheck_disk=$(whole_disk_name "$_recheck_name")
    _recheck_disk_path=$(field "$_recheck_disk" 2)
    _recheck_disk_size=$(field "$_recheck_disk" 4)
    _recheck_model=$(field "$_recheck_disk" 5)
    _recheck_serial=$(field "$_recheck_disk" 6)

    [ "$_recheck_type" = "$FS_DEVICE_TYPE" ] ||
        die "refusing $_recheck_path: it was a $FS_DEVICE_TYPE when selected and is a $_recheck_type now; nothing was changed"
    [ "$_recheck_size" = "$FS_DEVICE_SIZE" ] ||
        die "refusing $_recheck_path: the size changed from $FS_DEVICE_SIZE to $_recheck_size since it was selected; nothing was changed"
    [ "$_recheck_disk_path" = "$FS_DISK" ] ||
        die "refusing $_recheck_path: its disk changed from $FS_DISK to $_recheck_disk_path since it was selected; nothing was changed"
    [ "$_recheck_disk_size" = "$FS_DISK_SIZE" ] ||
        die "refusing $_recheck_path: the disk size changed from $FS_DISK_SIZE to $_recheck_disk_size since it was selected; nothing was changed"
    [ "$_recheck_model" = "$FS_MODEL" ] ||
        die "refusing $_recheck_path: the model changed from ${FS_MODEL:--} to ${_recheck_model:--} since it was selected; nothing was changed"
    [ "$_recheck_serial" = "$FS_SERIAL" ] ||
        die "refusing $_recheck_path: the serial changed from ${FS_SERIAL:--} to ${_recheck_serial:--} since it was selected; nothing was changed"
    [ "$_recheck_fstype" = "$FS_DEVICE_FSTYPE" ] ||
        die "refusing $_recheck_path: the filesystem changed from ${FS_DEVICE_FSTYPE:-none} to ${_recheck_fstype:-none} since it was selected; nothing was changed"
    if [ "$NEEDS_FORMAT" -eq 0 ] && [ -n "$FS_INVENTORY_UUID" ]; then
        [ "$_recheck_uuid" = "$FS_INVENTORY_UUID" ] ||
            die "refusing $_recheck_path: the filesystem UUID changed from $FS_INVENTORY_UUID to ${_recheck_uuid:-none} since it was selected; nothing was changed"
    fi

    _reason=$(disk_is_system_reason "$_recheck_disk" || true)
    [ -z "$_reason" ] || die "refusing $_recheck_path: $_reason"
    _reason=$(disk_unsafe_reason "$_recheck_disk" || true)
    [ -z "$_reason" ] || die "refusing $_recheck_path: $_reason"
    _reason=$(disk_boot_media_reason "$_recheck_disk" || true)
    [ -z "$_reason" ] || die "refusing $_recheck_path: $_reason"
    _recheck_allow=
    if [ "$CURRENT_MOUNT" -eq 1 ]; then
        _recheck_allow=$MOUNT
    fi
    _reason=$(disk_mount_reason "$_recheck_disk" "$_recheck_allow" || true)
    [ -z "$_reason" ] || die "refusing $_recheck_path: $_reason"

    if [ -n "$DEVICE" ]; then
        [ "$(canonical_device "$DEVICE")" = "$(canonical_device "$_recheck_path")" ] ||
            die "refusing $_recheck_path: --device $DEVICE now resolves elsewhere; nothing was changed"
    fi
    FS_DEVICE=$_recheck_path
}

resolve_format_decision() {
    if [ "$NEEDS_FORMAT" -eq 1 ]; then
        if [ "$FORMAT" -eq 1 ]; then
            return 0
        fi
        if [ "$INTERACTIVE" -eq 1 ]; then
            interactive_format_approval
            return 0
        fi
        if [ "$PENDING_FORMAT" -eq 1 ]; then
            refuse_interrupted_format
        fi
        refuse_needs_format
    fi
    if [ "$FORMAT" -eq 1 ]; then
        warn "--format was given but $FS_DEVICE already carries ext4; reusing it and keeping its data"
    fi
    return 0
}

describe_selection() {
    log "selected device: $FS_DEVICE"
    log "type: $FS_DEVICE_TYPE"
    log "size: $(human_size "$FS_DEVICE_SIZE") ($FS_DEVICE_SIZE bytes)"
    if [ "$FS_DEVICE_TYPE" = part ]; then
        log "disk: $FS_DISK ($(human_size "$FS_DISK_SIZE"))"
    fi
    log "model: ${FS_MODEL:--}"
    log "serial: ${FS_SERIAL:--}"
    if [ "$NEEDS_FORMAT" -eq 0 ]; then
        log "filesystem: ext4${FS_LABEL:+ label $FS_LABEL} (reuse; data is kept)"
    else
        log "filesystem: ${CANDIDATE_NOTE:-none} (format required; data would be destroyed)"
    fi
}

main() {
    parse_args "$@"
    check_environment

    TEMP_DIR=$(mktemp -d "${TMPDIR:-/tmp}/bittern-storage.XXXXXX") ||
        die "could not create a temporary directory"
    trap cleanup EXIT HUP INT TERM
    SELECTED_FILE=$TEMP_DIR/selected.tsv

    acquire_lock

    index_devices
    require_known_system_devices

    if [ "$LIST" -eq 1 ]; then
        collect_candidates "$MOUNT"
        if [ "$JSON" -eq 1 ]; then
            print_json
        else
            print_human_list
        fi
        return 0
    fi

    check_paths
    FSTAB_STAMP=$(fstab_stamp)

    log "Bittern recording storage setup"
    log "root: $ROOT"
    if [ "$DRY_RUN" -eq 1 ]; then
        log "mode: dry run (no changes)"
    elif [ "$FORMAT" -eq 1 ]; then
        log "mode: execute (ext4 reuse, explicit --format allowed)"
    else
        log "mode: execute (non-destructive; existing ext4 is reused)"
    fi
    log ""

    CURRENT_MOUNT=0
    if media_is_mount; then
        select_current_mount
    else
        collect_candidates
        if [ -n "$DEVICE" ]; then
            select_explicit_candidate
        else
            if [ ! -s "$CANDIDATES_FILE" ]; then
                printf '%s\n' 'error: no eligible recording device found' >&2
                printf '%s\n' 'every whole disk is the OS or boot disk, mounted, read-only, RAID or LVM, or installer media.' >&2
                printf '%s\n' 'connect or free a dedicated recording disk and rerun. The appliance can install without recordings; the wizard reports storage.' >&2
                exit 2
            fi
            if select_labelled_candidate; then
                log "automatic selection: the only device labelled $MEDIA_LABEL"
            else
                _label_count=$(awk -F '\t' '$1 == "label" { n++ } END { print n + 0 }' "$CANDIDATES_FILE")
                if [ "$_label_count" -gt 1 ]; then
                    print_selection_required "more than one eligible device carries the $MEDIA_LABEL label; a person must choose"
                elif [ "$DRY_RUN" -eq 1 ]; then
                    print_selection_required "no eligible device carries the $MEDIA_LABEL label and --dry-run never prompts"
                elif [ "$INTERACTIVE" -eq 0 ]; then
                    print_selection_required "no eligible device carries the $MEDIA_LABEL label and stdin is not a terminal"
                else
                    interactive_select
                fi
            fi
        fi
        load_selected_candidate
    fi

    detect_pending_format
    describe_selection
    resolve_format_decision

    if [ "$NEEDS_FORMAT" -eq 0 ]; then
        FS_UUID=$(read_uuid "$FS_DEVICE" "$FS_UUID") ||
            die "could not read the filesystem UUID of $FS_DEVICE"
        FSTAB_LINE=$(fstab_line_for_uuid "$FS_UUID")
    fi

    log ""

    if [ "$CURRENT_MOUNT" -eq 1 ]; then
        _mode=$(stat -c '%a' -- "$MOUNT" 2>/dev/null || true)
        if [ "$_mode" = 755 ] && fstab_is_correct; then
            log "already configured: $FS_DEVICE is mounted at $MOUNT and $FSTAB has the expected line"
            if [ "$DRY_RUN" -eq 1 ]; then
                log "dry run complete: no changes"
                return 0
            fi
            verify_state
            return 0
        fi
        ensure_mountpoint
        set_mount_mode
        update_fstab
        log ""
        if [ "$DRY_RUN" -eq 1 ]; then
            log "dry run complete: no changes"
            return 0
        fi
        verify_state
        return 0
    fi

    ensure_mountpoint
    recheck_selected_device
    if [ "$NEEDS_FORMAT" -eq 1 ]; then
        format_target
        if [ "$DRY_RUN" -eq 1 ]; then
            FSTAB_LINE='UUID=<filesystem-uuid-after-format> '"$MOUNT"' ext4 '"$FSTAB_OPTIONS"' '"$FSTAB_DUMP"' '"$FSTAB_PASS"
        fi
    fi
    mount_target
    set_mount_mode
    if [ "$NEEDS_FORMAT" -eq 0 ] || [ "$DRY_RUN" -eq 0 ]; then
        update_fstab
    else
        log "+ write $FSTAB line after formatting: $FSTAB_LINE"
    fi
    log ""
    if [ "$DRY_RUN" -eq 1 ]; then
        log "dry run complete: no changes"
        return 0
    fi
    verify_state
}

main "$@"

#!/usr/bin/env python3
"""Tests for packaging/storage-setup.sh against fixture block devices.

The test uses only the Python standard library. It runs the real script against
a temporary root and a captured `lsblk -P` fixture. Fake findmnt, mount, blkid
and mkfs.ext4 commands on PATH model the block-device state, so the suite never
touches a real disk and never runs a real mkfs.

What this proves: device and partition discovery policy, the bittern-media
zero-touch path, interactive and non-interactive selection, format approval,
the fstab rewrite and verification, and the JSON inventory contract.

What this does not prove: kernel mount behavior, real lsblk output, udev timing,
filesystem repair, power-loss safety, or behavior against physical hardware.
The fake mkfs only records an intent and a synthetic UUID.
"""

from __future__ import annotations

import fcntl
import hashlib
import json
import os
import pty
import shutil
import stat
import subprocess
import tempfile
import time
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
SCRIPT = ROOT / "packaging" / "storage-setup.sh"
FORMATTED_UUID = "formatted-uuid-1111"
REPAIRED_UUID = "formatted-uuid-2222"
MEDIA_UUID = "MEDIA-UUID-1111"
OTHER_UUID = "OTHER-UUID-2222"
REAL_INSTALL = shutil.which("install") or "/usr/bin/install"
REAL_SYNC = shutil.which("sync") or "/bin/sync"


FAKE_FINDMNT = """#!/usr/bin/env python3
import os
import sys
from pathlib import Path

root = Path(os.environ["FAKE_ROOT"]).resolve()
root_source = os.environ.get("FAKE_ROOT_SOURCE", "/dev/sda2")
mounts = [(root_source, str(root), "ext4")]
mounts_file = root / "fake" / "mounts"
if mounts_file.exists():
    for line in mounts_file.read_text(encoding="utf-8").splitlines():
        if line.strip():
            device, target, fstype = line.split("\\t")
            mounts.append((device, str(Path(target).resolve()), fstype))

args = sys.argv[1:]
target = None
field = "TARGET"
if "--target" in args:
    target = args[args.index("--target") + 1]
elif "-T" in args:
    target = args[args.index("-T") + 1]
if "-o" in args:
    field = args[args.index("-o") + 1]
if target is None:
    for device, mount, fstype in mounts:
        print(f"{device} {mount} {fstype}")
    sys.exit(0)

wanted = str(Path(target).resolve())
matches = []
for device, mount, fstype in mounts:
    if wanted == mount or wanted.startswith(mount.rstrip("/") + "/"):
        matches.append((len(mount), device, mount, fstype))
if not matches:
    sys.exit(1)
best = max(matches)
print({"TARGET": best[2], "SOURCE": best[1], "FSTYPE": best[3]}[field])
"""

FAKE_MOUNT = """#!/usr/bin/env python3
import os
import sys
from pathlib import Path

root = Path(os.environ["FAKE_ROOT"]).resolve()
log = root / "fake" / "cmd.log"
with log.open("a", encoding="utf-8") as handle:
    handle.write("mount " + " ".join(sys.argv[1:]) + "\\n")
if os.environ.get("FAKE_MOUNT_NOOP") == "1":
    sys.exit(0)
args = sys.argv[1:]
if len(args) < 2:
    sys.exit(2)
device, target = args[-2], str(Path(args[-1]).resolve())
mounts_file = root / "fake" / "mounts"
lines = []
if mounts_file.exists():
    lines = [line for line in mounts_file.read_text(encoding="utf-8").splitlines() if line.split("\\t")[1] != target]
lines.append(f"{device}\\t{target}\\text4")
mounts_file.write_text("\\n".join(lines) + "\\n", encoding="utf-8")
"""

FAKE_BLKID = """#!/usr/bin/env python3
import os
import sys
from pathlib import Path

root = Path(os.environ["FAKE_ROOT"])
args = sys.argv[1:]
if not args:
    sys.exit(2)
device = args[-1]
wanted = "UUID"
if "-s" in args:
    wanted = args[args.index("-s") + 1]
fs_file = root / "fake" / "fs"
if fs_file.exists():
    for line in fs_file.read_text(encoding="utf-8").splitlines():
        fields = line.split("\\t")
        if fields[0] == device:
            print(fields[1] if wanted == "UUID" else fields[2])
            sys.exit(0)
sys.exit(2)
"""

FAKE_MKFS = """#!/usr/bin/env python3
import os
import sys
from pathlib import Path

root = Path(os.environ["FAKE_ROOT"])
log = root / "fake" / "cmd.log"
with log.open("a", encoding="utf-8") as handle:
    handle.write("mkfs.ext4 " + " ".join(sys.argv[1:]) + "\\n")
if os.environ.get("FAKE_MKFS_FAIL") == "1":
    sys.exit(1)
devices = [arg for arg in sys.argv[1:] if arg.startswith("/dev/")]
if not devices:
    sys.exit(2)
device = devices[-1]
fs_file = root / "fake" / "fs"
lines = []
if fs_file.exists():
    lines = [line for line in fs_file.read_text(encoding="utf-8").splitlines() if line.split("\\t")[0] != device]
lines.append(f"{device}\\t{os.environ.get('FAKE_FORMATTED_UUID', 'formatted-uuid-1111')}\\text4")
fs_file.write_text("\\n".join(lines) + "\\n", encoding="utf-8")
"""


FAKE_INSTALL = """#!/usr/bin/env python3
import os
import shutil
import subprocess
import sys

result = subprocess.run([os.environ["FAKE_REAL_INSTALL"], *sys.argv[1:]])
swap = os.environ.get("FAKE_SWAP_FIXTURE")
marker = os.environ.get("FAKE_SWAP_ON_ARG")
if swap and (not marker or any(marker in arg for arg in sys.argv[1:])):
    shutil.copyfile(swap, os.environ["BITTERN_STORAGE_LSBLK_FIXTURE"])
sys.exit(result.returncode)
"""

FAKE_SYNC = """#!/usr/bin/env python3
import os
import subprocess
import sys

if any(".bittern-storage-write" in arg for arg in sys.argv[1:]):
    sys.exit(1)
sys.exit(subprocess.run([os.environ["FAKE_REAL_SYNC"], *sys.argv[1:]]).returncode)
"""

FAKE_MOUNT_EDIT_FSTAB = FAKE_MOUNT + '''
if os.environ.get("FAKE_APPEND_FSTAB") == "1":
    fstab = root / "etc" / "fstab"
    with fstab.open("a", encoding="utf-8") as handle:
        handle.write("UUID=OTHER /backup ext4 defaults 0 2\\n")
'''


def device(
    name: str,
    path: str,
    kind: str,
    size: int,
    model: str = "",
    serial: str = "",
    tran: str = "",
    label: str = "",
    fstype: str = "",
    uuid: str = "",
    mounts: str = "",
    ro: int = 0,
    rm: int = 0,
    pkname: str = "",
) -> dict[str, str]:
    return {
        "name": name,
        "path": path,
        "kind": kind,
        "size": str(size),
        "model": model,
        "serial": serial,
        "tran": tran,
        "label": label,
        "fstype": fstype,
        "uuid": uuid,
        "mounts": mounts,
        "ro": str(ro),
        "rm": str(rm),
        "pkname": pkname,
    }


def os_disk() -> list[dict[str, str]]:
    return [
        device("sda", "/dev/sda", "disk", 500_000_000_000, "OS DISK", "OS1", "sata"),
        device(
            "sda1",
            "/dev/sda1",
            "part",
            1_000_000_000,
            fstype="vfat",
            uuid="EFI-UUID",
            mounts="/boot/efi",
            pkname="sda",
        ),
        device(
            "sda2",
            "/dev/sda2",
            "part",
            499_000_000_000,
            fstype="ext4",
            uuid="ROOT-UUID",
            mounts="/",
            pkname="sda",
        ),
    ]


def os_disk_hidden_mounts() -> list[dict[str, str]]:
    """The OS disk as lsblk reports it inside a container: no MOUNTPOINTS."""
    return [
        device("sda", "/dev/sda", "disk", 500_000_000_000, "OS DISK", "OS1", "sata"),
        device(
            "sda1",
            "/dev/sda1",
            "part",
            1_000_000_000,
            fstype="vfat",
            uuid="EFI-UUID",
            pkname="sda",
        ),
        device(
            "sda2",
            "/dev/sda2",
            "part",
            499_000_000_000,
            fstype="ext4",
            uuid="ROOT-UUID",
            pkname="sda",
        ),
    ]


class StorageSetupTest(unittest.TestCase):
    def setUp(self) -> None:
        self.temp = tempfile.TemporaryDirectory()
        self.root = Path(self.temp.name) / "root"
        self.fakebin = Path(self.temp.name) / "bin"
        self.root.mkdir()
        (self.root / "etc").mkdir()
        (self.root / "fake").mkdir()
        self.fakebin.mkdir()
        self.write_fake("findmnt", FAKE_FINDMNT)
        self.write_fake("mount", FAKE_MOUNT)
        self.write_fake("blkid", FAKE_BLKID)
        self.write_fake("mkfs.ext4", FAKE_MKFS)
        self.fixture = self.root / "fixture.lsblk"
        self.fstab = self.root / "etc" / "fstab"
        self.media = self.root / "var" / "lib" / "bittern" / "media"
        self.noblkid_fakebin = Path(self.temp.name) / "noblkid-bin"
        self.minbin = Path(self.temp.name) / "minbin"

    def tearDown(self) -> None:
        self.temp.cleanup()

    def write_fake(self, name: str, body: str) -> None:
        path = self.fakebin / name
        path.write_text(body, encoding="utf-8")
        path.chmod(0o755)

    def fixture_text(self, devices: list[dict[str, str]]) -> str:
        lines = []
        for item in devices:
            lines.append(
                'NAME="{name}" KNAME="{name}" PATH="{path}" TYPE="{kind}" SIZE="{size}" '
                'MODEL="{model}" SERIAL="{serial}" TRAN="{tran}" LABEL="{label}" '
                'FSTYPE="{fstype}" UUID="{uuid}" MOUNTPOINTS="{mounts}" RO="{ro}" '
                'RM="{rm}" PKNAME="{pkname}"'.format(**item)
            )
        return "\n".join(lines) + "\n"

    def use_fixture(
        self,
        devices: list[dict[str, str]],
        mounts: list[tuple[str, Path, str]] | None = None,
    ) -> None:
        fs_lines = []
        for item in devices:
            if item["uuid"]:
                fs_lines.append(
                    f'{item["path"]}\t{item["uuid"]}\t{item["fstype"]}'
                )
        self.fixture.write_text(self.fixture_text(devices), encoding="utf-8")
        (self.root / "fake" / "fs").write_text(
            "\n".join(fs_lines) + ("\n" if fs_lines else ""), encoding="utf-8"
        )
        mount_lines = []
        for dev, target, fstype in mounts or []:
            mount_lines.append(f"{dev}\t{Path(target).resolve()}\t{fstype}")
        (self.root / "fake" / "mounts").write_text(
            "\n".join(mount_lines) + ("\n" if mount_lines else ""), encoding="utf-8"
        )
        (self.root / "fake" / "cmd.log").write_text("", encoding="utf-8")

    def whole_disk_fixture(self) -> None:
        self.use_fixture(
            os_disk()
            + [
                device(
                    "sdb",
                    "/dev/sdb",
                    "disk",
                    1_000_204_886_016,
                    "USB SATA DISK",
                    "USB1",
                    "usb",
                    label="bittern-media",
                    fstype="ext4",
                    uuid=MEDIA_UUID,
                    rm=1,
                ),
                device(
                    "sdc",
                    "/dev/sdc",
                    "disk",
                    2_000_398_934_016,
                    "SATA BACKUP DISK",
                    "SATA2",
                    "sata",
                    fstype="ext4",
                    uuid=OTHER_UUID,
                ),
                device(
                    "sdd",
                    "/dev/sdd",
                    "disk",
                    4_000_787_030_016,
                    "BLANK DISK",
                    "BLANK1",
                    "usb",
                    rm=1,
                ),
                device(
                    "sde",
                    "/dev/sde",
                    "disk",
                    8_000_000_000,
                    "DEBIAN INSTALLER",
                    "INSTALL1",
                    "usb",
                    label="Debian 13 amd64",
                    fstype="iso9660",
                ),
                device(
                    "sdf",
                    "/dev/sdf",
                    "disk",
                    2_000_398_934_016,
                    "RAID MEMBER",
                    "RAID1",
                    "sata",
                    fstype="linux_raid_member",
                ),
            ]
        )

    def partition_fixture(self) -> None:
        self.use_fixture(
            os_disk()
            + [
                device("sdb", "/dev/sdb", "disk", 1_000_204_886_016, "USB SATA DISK", "USB1", "usb", rm=1),
                device(
                    "sdb1",
                    "/dev/sdb1",
                    "part",
                    1_000_204_000_000,
                    label="bittern-media",
                    fstype="ext4",
                    uuid=MEDIA_UUID,
                    pkname="sdb",
                    rm=1,
                ),
                device("sdc", "/dev/sdc", "disk", 4_000_787_030_016, "BLANK DISK", "BLANK1", "usb", rm=1),
            ]
        )

    def unlabelled_fixture(self) -> None:
        self.use_fixture(
            os_disk()
            + [
                device("sdb", "/dev/sdb", "disk", 1_000_204_886_016, "USB SATA DISK", "USB1", "usb", rm=1),
                device(
                    "sdb1",
                    "/dev/sdb1",
                    "part",
                    1_000_204_000_000,
                    fstype="ext4",
                    uuid=MEDIA_UUID,
                    pkname="sdb",
                    rm=1,
                ),
            ]
        )

    def blank_fixture(self) -> None:
        self.use_fixture(
            os_disk()
            + [
                device(
                    "sdb",
                    "/dev/sdb",
                    "disk",
                    1_000_204_886_016,
                    "USB SATA DISK",
                    "USB1",
                    "usb",
                    rm=1,
                )
            ]
        )

    def mounted_fixture(self) -> None:
        self.use_fixture(
            os_disk()
            + [
                device("sdb", "/dev/sdb", "disk", 1_000_204_886_016, "USB SATA DISK", "USB1", "usb", rm=1),
                device(
                    "sdb1",
                    "/dev/sdb1",
                    "part",
                    1_000_204_000_000,
                    fstype="ext4",
                    uuid=MEDIA_UUID,
                    mounts="/mnt/backup",
                    pkname="sdb",
                    rm=1,
                ),
            ],
            mounts=[("/dev/sdb1", Path("/mnt/backup"), "ext4")],
        )

    def two_labelled_fixture(self) -> None:
        self.use_fixture(
            os_disk()
            + [
                device("sdb", "/dev/sdb", "disk", 1_000_204_886_016, "USB DISK ONE", "USB1", "usb", rm=1),
                device(
                    "sdb1",
                    "/dev/sdb1",
                    "part",
                    1_000_204_000_000,
                    label="bittern-media",
                    fstype="ext4",
                    uuid=MEDIA_UUID,
                    pkname="sdb",
                    rm=1,
                ),
                device("sdc", "/dev/sdc", "disk", 2_000_398_934_016, "USB DISK TWO", "USB2", "usb", rm=1),
                device(
                    "sdc1",
                    "/dev/sdc1",
                    "part",
                    2_000_398_000_000,
                    label="bittern-media",
                    fstype="ext4",
                    uuid=OTHER_UUID,
                    pkname="sdc",
                    rm=1,
                ),
            ]
        )

    def mixed_fixture(self) -> None:
        self.use_fixture(
            os_disk()
            + [
                device(
                    "sdb",
                    "/dev/sdb",
                    "disk",
                    1_000_204_886_016,
                    "USB SATA DISK",
                    "USB1",
                    "usb",
                    label="bittern-media",
                    fstype="ext4",
                    uuid=MEDIA_UUID,
                    rm=1,
                ),
                device("sdc", "/dev/sdc", "disk", 2_000_398_934_016, "SATA DATA DISK", "SATA2", "sata"),
                device(
                    "sdc1",
                    "/dev/sdc1",
                    "part",
                    2_000_398_000_000,
                    fstype="ext4",
                    uuid=OTHER_UUID,
                    pkname="sdc",
                ),
                device("sdd", "/dev/sdd", "disk", 4_000_787_030_016, "BLANK DISK", "BLANK1", "usb", rm=1),
                device(
                    "sde",
                    "/dev/sde",
                    "disk",
                    8_000_000_000,
                    "DEBIAN INSTALLER",
                    "INSTALL1",
                    "usb",
                    label="Debian 13 amd64",
                    fstype="iso9660",
                ),
                device(
                    "sdf",
                    "/dev/sdf",
                    "disk",
                    2_000_398_934_016,
                    "RAID MEMBER",
                    "RAID1",
                    "sata",
                    fstype="linux_raid_member",
                ),
            ]
        )

    def noblkid_path(self) -> str:
        if not self.noblkid_fakebin.exists():
            self.noblkid_fakebin.mkdir()
            for name in ("findmnt", "mount", "mkfs.ext4"):
                target = self.noblkid_fakebin / name
                shutil.copy2(self.fakebin / name, target)
                target.chmod(0o755)
            self.minbin.mkdir()
            for name in (
                "awk",
                "cat",
                "chmod",
                "chown",
                "cp",
                "cksum",
                "cut",
                "date",
                "flock",
                "grep",
                "head",
                "id",
                "install",
                "mktemp",
                "mv",
                "numfmt",
                "python3",
                "readlink",
                "rm",
                "sed",
                "sort",
                "stat",
                "sync",
                "tr",
            ):
                source = shutil.which(name)
                if source is not None:
                    (self.minbin / name).symlink_to(source)
        return f"{self.noblkid_fakebin}:{self.minbin}"

    def env(self, without_blkid: bool = False) -> dict[str, str]:
        env = os.environ.copy()
        if without_blkid:
            env["PATH"] = self.noblkid_path()
        else:
            env["PATH"] = f"{self.fakebin}:{env['PATH']}"
        env["FAKE_ROOT"] = str(self.root)
        env["FAKE_ROOT_SOURCE"] = "/dev/sda2"
        env["FAKE_FORMATTED_UUID"] = FORMATTED_UUID
        env["BITTERN_STORAGE_LSBLK_FIXTURE"] = str(self.fixture)
        return env

    def command(self, *args: str) -> list[str]:
        return [
            str(SCRIPT),
            "--root",
            str(self.root),
            "--fstab",
            str(self.fstab),
            *args,
        ]

    def run_storage(
        self,
        *args: str,
        env_extra: dict[str, str] | None = None,
        input_text: str | None = None,
        without_blkid: bool = False,
    ) -> subprocess.CompletedProcess[str]:
        env = self.env(without_blkid=without_blkid)
        env.update(env_extra or {})
        kwargs: dict[str, object] = {}
        if input_text is None:
            kwargs["stdin"] = subprocess.DEVNULL
        else:
            kwargs["input"] = input_text
        return subprocess.run(
            self.command(*args),
            text=True,
            capture_output=True,
            env=env,
            timeout=20,
            check=False,
            **kwargs,
        )

    def run_storage_pty(
        self,
        *args: str,
        answers: str,
        env_extra: dict[str, str] | None = None,
    ) -> subprocess.CompletedProcess[str]:
        env = self.env()
        env.update(env_extra or {})
        master, slave = pty.openpty()
        process = subprocess.Popen(
            self.command(*args),
            stdin=slave,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
            env=env,
        )
        os.close(slave)
        try:
            time.sleep(0.05)
            os.write(master, answers.encode("utf-8"))
            stdout, stderr = process.communicate(timeout=20)
        finally:
            os.close(master)
            if process.poll() is None:
                process.kill()
                process.communicate()
        return subprocess.CompletedProcess(
            process.args, process.returncode, stdout, stderr
        )

    def snapshot(self) -> dict[str, tuple[str, ...]]:
        result: dict[str, tuple[str, ...]] = {}
        for path in sorted(self.root.rglob("*")):
            relative = path.relative_to(self.root).as_posix()
            if relative == "fake/cmd.log":
                continue
            if relative == "run" or relative.startswith("run/"):
                # The whole-run lock file and its directory are not appliance
                # state; they are created by the run and kept for the next one.
                continue
            mode = oct(stat.S_IMODE(path.lstat().st_mode))
            if path.is_symlink():
                result[relative] = ("link", os.readlink(path))
            elif path.is_file():
                result[relative] = (
                    "file",
                    mode,
                    hashlib.sha256(path.read_bytes()).hexdigest(),
                )
            else:
                result[relative] = ("dir", mode)
        return result

    def command_log(self) -> str:
        path = self.root / "fake" / "cmd.log"
        return path.read_text(encoding="utf-8") if path.exists() else ""

    def mount_state(self) -> list[list[str]]:
        path = self.root / "fake" / "mounts"
        if not path.exists():
            return []
        return [
            line.split("\t")
            for line in path.read_text(encoding="utf-8").splitlines()
            if line
        ]

    def fstab_media_lines(self) -> list[list[str]]:
        if not self.fstab.exists():
            return []
        lines = []
        for line in self.fstab.read_text(encoding="utf-8").splitlines():
            if line.lstrip().startswith("#"):
                continue
            fields = line.split()
            if len(fields) >= 2 and fields[1] == str(self.media):
                lines.append(fields)
        return lines

    def expected_fstab(self, uuid: str) -> str:
        return (
            f"UUID={uuid} {self.media} ext4 "
            f"defaults,noatime,nofail,x-systemd.device-timeout=30s 0 2"
        )

    def assert_untouched(self) -> None:
        self.assertFalse(self.fstab.exists(), "fstab was written")
        self.assertFalse(self.media.exists(), "media path was created")
        log = self.command_log()
        self.assertNotIn("mkfs.ext4", log, "mkfs was called")
        self.assertNotIn("mount ", log, "mount was called")

    def test_labelled_whole_disk_is_automatic_and_dry_run_changes_nothing(self) -> None:
        self.whole_disk_fixture()
        before = self.snapshot()
        result = self.run_storage("--dry-run")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("automatic selection: the only device labelled bittern-media", result.stdout)
        self.assertIn("selected device: /dev/sdb", result.stdout)
        self.assertIn(self.expected_fstab(MEDIA_UUID), result.stdout)
        self.assertIn("dry run complete: no changes", result.stdout)
        self.assertNotIn("Choose a recording device", result.stderr)
        self.assertEqual(before, self.snapshot())
        self.assert_untouched()

    def test_labelled_partition_is_automatic_and_uses_the_partition_uuid(self) -> None:
        self.partition_fixture()
        result = self.run_storage()
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("automatic selection: the only device labelled bittern-media", result.stdout)
        self.assertIn("selected device: /dev/sdb1", result.stdout)
        self.assertIn("type: part", result.stdout)
        self.assertNotIn("Choose a recording device", result.stderr)
        self.assertEqual(
            self.fstab_media_lines(),
            [self.expected_fstab(MEDIA_UUID).split()],
        )
        self.assertEqual(self.mount_state(), [["/dev/sdb1", str(self.media.resolve()), "ext4"]])
        self.assertNotIn("mkfs.ext4", self.command_log())

    def test_st3_format_never_widens_from_a_disk_to_its_partition(self) -> None:
        """ST-3: --format applies to the exact --device path, not to a partition."""
        self.use_fixture(
            os_disk()
            + [
                device("sdb", "/dev/sdb", "disk", 1_000_204_886_016, "USB SATA DISK", "USB1", "usb", rm=1),
                device(
                    "sdb1",
                    "/dev/sdb1",
                    "part",
                    1_000_204_000_000,
                    fstype="ntfs",
                    uuid=OTHER_UUID,
                    pkname="sdb",
                    rm=1,
                ),
            ]
        )
        refused = self.run_storage("--device", "/dev/sdb", "--format")
        self.assertEqual(refused.returncode, 4, refused.stdout + refused.stderr)
        self.assertIn("--device /dev/sdb1", refused.stderr)
        self.assertIn("was not named", refused.stderr)
        self.assertNotIn("mkfs.ext4", self.command_log())
        self.assert_untouched()

        exact = self.run_storage("--device", "/dev/sdb1", "--format")
        self.assertEqual(exact.returncode, 0, exact.stdout + exact.stderr)
        self.assertIn("mkfs.ext4 -F -L bittern-media /dev/sdb1", self.command_log())
        self.assertEqual(
            self.fstab_media_lines(),
            [self.expected_fstab(FORMATTED_UUID).split()],
        )

    def test_correct_existing_mount_is_a_noop(self) -> None:
        self.whole_disk_fixture()
        self.media.mkdir(parents=True)
        self.media.chmod(0o755)
        self.fstab.write_text(self.expected_fstab(MEDIA_UUID) + "\n", encoding="utf-8")
        (self.root / "fake" / "mounts").write_text(
            f"/dev/sdb\t{self.media.resolve()}\text4\n", encoding="utf-8"
        )
        before = self.snapshot()
        result = self.run_storage()
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("already configured", result.stdout)
        self.assertIn("recording storage verified", result.stdout)
        self.assertEqual(before, self.snapshot())
        log = self.command_log()
        self.assertNotIn("mount ", log)
        self.assertNotIn("mkfs.ext4", log)

    def test_existing_line_without_boot_safety_options_is_migrated(self) -> None:
        """An installed appliance gains nofail and the timeout on the next run."""
        self.whole_disk_fixture()
        self.media.mkdir(parents=True)
        self.media.chmod(0o755)
        old_line = f"UUID={MEDIA_UUID} {self.media} ext4 defaults,noatime 0 2"
        self.fstab.write_text(old_line + "\n", encoding="utf-8")
        (self.root / "fake" / "mounts").write_text(
            f"/dev/sdb\t{self.media.resolve()}\text4\n", encoding="utf-8"
        )

        before = self.snapshot()
        planned = self.run_storage("--dry-run")
        self.assertEqual(planned.returncode, 0, planned.stdout + planned.stderr)
        self.assertIn(old_line, planned.stdout)
        self.assertIn(self.expected_fstab(MEDIA_UUID), planned.stdout)
        self.assertIn("dry run complete: no changes", planned.stdout)
        self.assertEqual(before, self.snapshot(), "dry run changed the tree")

        migrated = self.run_storage()
        self.assertEqual(migrated.returncode, 0, migrated.stdout + migrated.stderr)
        self.assertEqual(
            self.fstab_media_lines(),
            [self.expected_fstab(MEDIA_UUID).split()],
        )
        self.assertNotIn(old_line, self.fstab.read_text(encoding="utf-8"))
        log = self.command_log()
        self.assertNotIn("mount ", log)
        self.assertNotIn("mkfs.ext4", log)

    def test_duplicate_and_wrong_fstab_lines_are_replaced(self) -> None:
        self.whole_disk_fixture()
        self.media.mkdir(parents=True)
        self.media.chmod(0o755)
        self.fstab.write_text(
            f"UUID=OLD {self.media} ext4 defaults 0 2\n"
            f"/dev/sdz {self.media} ext4 defaults,noatime 0 2\n"
            f"{self.expected_fstab(MEDIA_UUID)}\n"
            "# keep this comment\n"
            f"UUID={OTHER_UUID} /data ext4 defaults 0 2\n",
            encoding="utf-8",
        )
        result = self.run_storage()
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("replace 3 fstab line(s)", result.stdout)
        self.assertEqual(
            self.fstab_media_lines(),
            [self.expected_fstab(MEDIA_UUID).split()],
        )
        text = self.fstab.read_text(encoding="utf-8")
        self.assertIn("# keep this comment", text)
        self.assertIn(f"UUID={OTHER_UUID} /data ext4 defaults 0 2", text)
        self.assertEqual(self.mount_state(), [["/dev/sdb", str(self.media.resolve()), "ext4"]])

    def test_blank_disk_refuses_without_format_and_names_the_command(self) -> None:
        self.blank_fixture()
        result = self.run_storage("--device", "/dev/sdb")
        self.assertEqual(result.returncode, 3, result.stdout + result.stderr)
        self.assertIn("refusing to format /dev/sdb", result.stderr)
        self.assertIn("--format --device /dev/sdb", result.stderr)
        self.assertIn("USB SATA DISK", result.stderr)
        self.assertIn("1000204886016 bytes", result.stderr)
        self.assertIn("Formatting destroys", result.stderr)
        self.assertIn("--yes alone is not enough", result.stderr)
        self.assert_untouched()

        again = self.run_storage("--device", "/dev/sdb", "--yes")
        self.assertEqual(again.returncode, 3, again.stdout + again.stderr)
        self.assertIn("--format is required", again.stderr)
        self.assert_untouched()

    def test_mounted_device_is_refused_with_the_unmount_command(self) -> None:
        self.mounted_fixture()
        result = self.run_storage("--device", "/dev/sdb1")
        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        self.assertIn("mounted at /mnt/backup", result.stderr)
        self.assertIn("umount /mnt/backup", result.stderr)
        self.assert_untouched()

    def test_verification_failure_names_the_missing_mount(self) -> None:
        self.whole_disk_fixture()
        result = self.run_storage(env_extra={"FAKE_MOUNT_NOOP": "1"})
        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        self.assertIn("verification failed: findmnt does not show /dev/sdb", result.stderr)
        self.assertIn(str(self.media), result.stderr)

    def test_second_run_is_a_noop(self) -> None:
        self.whole_disk_fixture()
        first = self.run_storage()
        self.assertEqual(first.returncode, 0, first.stdout + first.stderr)
        before = self.snapshot()
        second = self.run_storage()
        self.assertEqual(second.returncode, 0, second.stdout + second.stderr)
        self.assertIn("already configured", second.stdout)
        self.assertEqual(before, self.snapshot())

    def test_explicit_device_accepts_unlabelled_ext4_without_a_prompt(self) -> None:
        self.unlabelled_fixture()
        result = self.run_storage("--device", "/dev/sdb1")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertNotIn("Choose a recording device", result.stderr)
        self.assertIn("selected device: /dev/sdb1", result.stdout)
        self.assertEqual(self.mount_state(), [["/dev/sdb1", str(self.media.resolve()), "ext4"]])
        self.assertEqual(
            self.fstab_media_lines(),
            [self.expected_fstab(MEDIA_UUID).split()],
        )

    def test_unlabelled_noninteractive_prints_the_list_and_refuses(self) -> None:
        self.unlabelled_fixture()
        result = self.run_storage()
        self.assertEqual(result.returncode, 4, result.stdout + result.stderr)
        self.assertIn("eligible recording devices:", result.stdout)
        self.assertIn("device=/dev/sdb1", result.stdout)
        self.assertIn("filesystem=ext4", result.stdout)
        self.assertIn("label=-", result.stdout)
        self.assertIn("no eligible device carries the bittern-media label", result.stderr)
        self.assertIn("stdin is not a terminal", result.stderr)
        self.assert_untouched()

    def test_multiple_labelled_devices_refuse_without_a_terminal(self) -> None:
        self.two_labelled_fixture()
        result = self.run_storage()
        self.assertEqual(result.returncode, 4, result.stdout + result.stderr)
        self.assertIn("device=/dev/sdb1", result.stdout)
        self.assertIn("device=/dev/sdc1", result.stdout)
        self.assertIn("more than one eligible device carries the bittern-media label", result.stderr)
        self.assert_untouched()

    def test_interactive_selection_is_honoured(self) -> None:
        self.unlabelled_fixture()
        result = self.run_storage_pty(answers="1\n")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("Choose a recording device [1-1]", result.stderr)
        self.assertNotIn("Format this device?", result.stderr)
        self.assertIn("selected device: /dev/sdb1", result.stdout)
        self.assertEqual(self.mount_state(), [["/dev/sdb1", str(self.media.resolve()), "ext4"]])
        self.assertEqual(
            self.fstab_media_lines(),
            [self.expected_fstab(MEDIA_UUID).split()],
        )

    def test_interactive_format_rejects_a_bare_y_confirmation(self) -> None:
        self.blank_fixture()
        result = self.run_storage_pty(answers="1\ny\ny\n")
        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        self.assertIn("format not confirmed", result.stderr)
        self.assertIn("Type /dev/sdb or --format to confirm", result.stderr)
        self.assert_untouched()

    def test_interactive_format_accepts_the_device_path(self) -> None:
        self.blank_fixture()
        result = self.run_storage_pty(answers="1\ny\n/dev/sdb\n")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("Formatting /dev/sdb destroys all data", result.stderr)
        self.assertIn("mkfs.ext4 -F -L bittern-media /dev/sdb", self.command_log())
        self.assertEqual(
            self.fstab_media_lines(),
            [self.expected_fstab(FORMATTED_UUID).split()],
        )
        self.assertEqual(self.mount_state(), [["/dev/sdb", str(self.media.resolve()), "ext4"]])

    def test_interactive_format_accepts_the_format_word(self) -> None:
        self.blank_fixture()
        result = self.run_storage_pty(answers="1\ny\n--format\n")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("mkfs.ext4 -F -L bittern-media /dev/sdb", self.command_log())
        self.assertEqual(
            self.fstab_media_lines(),
            [self.expected_fstab(FORMATTED_UUID).split()],
        )

    def test_list_works_without_blkid_and_uses_lsblk_fields(self) -> None:
        self.mixed_fixture()
        result = self.run_storage("--list", "--json", without_blkid=True)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        document = json.loads(result.stdout)
        by_name = {entry["device"]: entry for entry in document["devices"]}
        self.assertEqual(by_name["/dev/sdb"]["label"], "bittern-media")
        self.assertEqual(by_name["/dev/sdb"]["filesystem_uuid"], MEDIA_UUID)
        self.assertNotIn("blkid is required", result.stderr)

    def test_st10_action_refuses_without_blkid(self) -> None:
        """ST-10: blkid is required for the action path, not an optional check."""
        self.partition_fixture()
        result = self.run_storage("--device", "/dev/sdb1", without_blkid=True)
        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        self.assertIn("blkid is required", result.stderr)
        self.assert_untouched()

    def test_disagreeing_blkid_warns_and_prefers_the_blkid_uuid(self) -> None:
        self.partition_fixture()
        (self.root / "fake" / "fs").write_text(
            "/dev/sdb1\tBLKID-UUID-3333\text4\n", encoding="utf-8"
        )
        result = self.run_storage("--dry-run")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("UUID for /dev/sdb1 differs", result.stderr)
        self.assertIn(f"lsblk reports {MEDIA_UUID}", result.stderr)
        self.assertIn("blkid reports BLKID-UUID-3333", result.stderr)
        self.assertIn("using the blkid value", result.stderr)
        self.assertIn(self.expected_fstab("BLKID-UUID-3333"), result.stdout)
        self.assertNotIn(self.expected_fstab(MEDIA_UUID), result.stdout)

    def test_list_json_has_stable_machine_readable_fields(self) -> None:
        self.mixed_fixture()
        result = self.run_storage("--list", "--json")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        document = json.loads(result.stdout)
        self.assertEqual(document["schema_version"], 1)
        names = [entry["device"] for entry in document["devices"]]
        self.assertEqual(names, ["/dev/sdb", "/dev/sdc1", "/dev/sdd"])
        expected_keys = {
            "device",
            "type",
            "size_bytes",
            "size",
            "model",
            "serial",
            "transport",
            "filesystem",
            "label",
            "filesystem_uuid",
            "needs_format",
            "disk",
            "disk_size_bytes",
            "note",
        }
        for entry in document["devices"]:
            self.assertEqual(set(entry), expected_keys)

        by_name = {entry["device"]: entry for entry in document["devices"]}
        self.assertEqual(by_name["/dev/sdb"]["type"], "disk")
        self.assertEqual(by_name["/dev/sdb"]["transport"], "usb")
        self.assertEqual(by_name["/dev/sdb"]["filesystem"], "ext4")
        self.assertEqual(by_name["/dev/sdb"]["label"], "bittern-media")
        self.assertFalse(by_name["/dev/sdb"]["needs_format"])
        self.assertEqual(by_name["/dev/sdb"]["filesystem_uuid"], MEDIA_UUID)

        self.assertEqual(by_name["/dev/sdc1"]["type"], "part")
        self.assertEqual(by_name["/dev/sdc1"]["disk"], "/dev/sdc")
        self.assertIsNone(by_name["/dev/sdc1"]["label"])
        self.assertEqual(by_name["/dev/sdc1"]["filesystem"], "ext4")
        self.assertFalse(by_name["/dev/sdc1"]["needs_format"])

        self.assertEqual(by_name["/dev/sdd"]["type"], "disk")
        self.assertEqual(by_name["/dev/sdd"]["filesystem"], "none")
        self.assertIsNone(by_name["/dev/sdd"]["label"])
        self.assertIsNone(by_name["/dev/sdd"]["filesystem_uuid"])
        self.assertTrue(by_name["/dev/sdd"]["needs_format"])
        self.assertEqual(by_name["/dev/sdd"]["note"], "blank")

        self.assertNotIn("/dev/sda", names)
        self.assertNotIn("/dev/sde", names)
        self.assertNotIn("/dev/sdf", names)

    def test_list_includes_a_device_already_mounted_at_media(self) -> None:
        self.partition_fixture()
        self.media.mkdir(parents=True)
        (self.root / "fake" / "mounts").write_text(
            f"/dev/sdb1\t{self.media.resolve()}\text4\n", encoding="utf-8"
        )
        result = self.run_storage("--list", "--json")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        document = json.loads(result.stdout)
        names = [entry["device"] for entry in document["devices"]]
        self.assertIn("/dev/sdb1", names)

    def test_dry_run_with_format_plans_mkfs_and_changes_nothing(self) -> None:
        self.blank_fixture()
        before = self.snapshot()
        result = self.run_storage("--device", "/dev/sdb", "--format", "--dry-run")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("mkfs.ext4 -F -L bittern-media /dev/sdb", result.stdout)
        self.assertIn("UUID=<filesystem-uuid-after-format>", result.stdout)
        self.assertIn("dry run complete: no changes", result.stdout)
        self.assertEqual(before, self.snapshot())
        self.assert_untouched()

    def test_format_with_explicit_approval_formats_and_verifies(self) -> None:
        self.blank_fixture()
        result = self.run_storage("--device", "/dev/sdb", "--format")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("mkfs.ext4 -F -L bittern-media /dev/sdb", self.command_log())
        self.assertEqual(
            self.fstab_media_lines(),
            [self.expected_fstab(FORMATTED_UUID).split()],
        )
        self.assertEqual(self.mount_state(), [["/dev/sdb", str(self.media.resolve()), "ext4"]])
        self.assertIn("recording storage verified", result.stdout)

    def test_symlink_mountpoint_is_refused(self) -> None:
        self.whole_disk_fixture()
        target = self.root / "elsewhere"
        target.mkdir()
        self.media.parent.mkdir(parents=True, exist_ok=True)
        self.media.symlink_to(target)
        result = self.run_storage()
        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        self.assertIn("symlink mountpoint", result.stderr)
        self.assertFalse(self.fstab.exists())
        self.assertNotIn("mkfs.ext4", self.command_log())

    def blank_removable_fixture(self, **overrides: str) -> None:
        item = device(
            "sdb",
            "/dev/sdb",
            "disk",
            1_000_204_886_016,
            "USB SATA DISK",
            "USB1",
            "usb",
            rm=1,
        )
        item.update(overrides)
        self.use_fixture(os_disk() + [item])

    def test_st1_a_second_run_refuses_while_the_lock_is_held(self) -> None:
        """ST-1: the whole run holds flock, so two runs never format at once."""
        self.whole_disk_fixture()
        lock = self.root / "run" / "lock" / "bittern-storage.lock"
        lock.parent.mkdir(parents=True)
        with lock.open("w", encoding="utf-8") as handle:
            fcntl.flock(handle, fcntl.LOCK_EX | fcntl.LOCK_NB)
            blocked = self.run_storage("--format")
        self.assertEqual(blocked.returncode, 1, blocked.stdout + blocked.stderr)
        self.assertIn("another storage-setup run is in progress", blocked.stderr)
        self.assert_untouched()

        released = self.run_storage()
        self.assertEqual(released.returncode, 0, released.stdout + released.stderr)
        self.assertEqual(
            self.fstab_media_lines(),
            [self.expected_fstab(MEDIA_UUID).split()],
        )

    def test_st1_an_fstab_edit_during_the_run_is_not_overwritten(self) -> None:
        """ST-1: the fstab is re-read under the lock before the rename."""
        self.whole_disk_fixture()
        self.media.mkdir(parents=True)
        self.media.chmod(0o755)
        original = f"UUID={OTHER_UUID} /data ext4 defaults 0 2\n"
        self.fstab.write_text(original, encoding="utf-8")
        self.write_fake("mount", FAKE_MOUNT_EDIT_FSTAB)
        result = self.run_storage(env_extra={"FAKE_APPEND_FSTAB": "1"})
        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        self.assertIn("changed while this run was in progress", result.stderr)
        text = self.fstab.read_text(encoding="utf-8")
        self.assertIn("UUID=OTHER /backup", text)
        self.assertEqual(self.fstab_media_lines(), [])
        self.assertEqual(self.mount_state(), [["/dev/sdb", str(self.media.resolve()), "ext4"]])

    def test_st2_an_unknown_root_refuses_the_esp_and_os_partitions(self) -> None:
        """ST-2: an unmappable root offers no device at all instead of the OS disk."""
        self.use_fixture(
            os_disk_hidden_mounts()
            + [device("sdb", "/dev/sdb", "disk", 1_000_204_886_016, "USB SATA DISK", "USB1", "usb", rm=1)]
        )
        blind = {"FAKE_ROOT_SOURCE": "overlay"}
        listed = self.run_storage("--list", "--json", env_extra=blind)
        self.assertEqual(listed.returncode, 1, listed.stdout + listed.stderr)
        self.assertIn("cannot establish the running root device", listed.stderr)
        for refused in ("/dev/sda", "/dev/sda1", "/dev/sda2"):
            self.assertNotIn(refused, listed.stdout)

        formatted = self.run_storage("--device", "/dev/sda1", "--format", env_extra=blind)
        self.assertEqual(formatted.returncode, 1, formatted.stdout + formatted.stderr)
        self.assertIn("cannot establish the running root device", formatted.stderr)
        self.assertNotIn("mkfs.ext4", self.command_log())
        self.assert_untouched()

    def test_st2_mountinfo_names_the_root_when_lsblk_cannot(self) -> None:
        """ST-2: /proc/self/mountinfo excludes the OS disk lsblk cannot map."""
        self.use_fixture(
            os_disk_hidden_mounts()
            + [
                device(
                    "sdb",
                    "/dev/sdb",
                    "disk",
                    1_000_204_886_016,
                    "USB SATA DISK",
                    "USB1",
                    "usb",
                    label="bittern-media",
                    fstype="ext4",
                    uuid=MEDIA_UUID,
                    rm=1,
                )
            ]
        )
        proc = self.root / "proc" / "self"
        proc.mkdir(parents=True)
        (proc / "mountinfo").write_text(
            "36 35 8:2 / / rw,noatime - ext4 /dev/sda2 rw\n", encoding="utf-8"
        )
        result = self.run_storage("--list", "--json", env_extra={"FAKE_ROOT_SOURCE": "overlay"})
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        names = [entry["device"] for entry in json.loads(result.stdout)["devices"]]
        self.assertEqual(names, ["/dev/sdb"])

    def test_st2_cmdline_root_names_the_root_without_mountinfo(self) -> None:
        """ST-2: root= in /proc/cmdline is the last cross-check for the OS disk."""
        self.use_fixture(
            os_disk_hidden_mounts()
            + [
                device(
                    "sdb",
                    "/dev/sdb",
                    "disk",
                    1_000_204_886_016,
                    "USB SATA DISK",
                    "USB1",
                    "usb",
                    label="bittern-media",
                    fstype="ext4",
                    uuid=MEDIA_UUID,
                    rm=1,
                )
            ]
        )
        (self.root / "proc").mkdir(parents=True, exist_ok=True)
        (self.root / "proc" / "cmdline").write_text(
            "BOOT_IMAGE=/vmlinuz root=/dev/sda2 ro quiet\n", encoding="utf-8"
        )
        result = self.run_storage("--list", "--json", env_extra={"FAKE_ROOT_SOURCE": "overlay"})
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        names = [entry["device"] for entry in json.loads(result.stdout)["devices"]]
        self.assertEqual(names, ["/dev/sdb"])

    def test_st2_an_esp_is_never_a_candidate(self) -> None:
        """ST-2: a FAT partition is refused even when nothing mounts it."""
        self.use_fixture(
            os_disk()
            + [
                device("sdb", "/dev/sdb", "disk", 500_000_000_000, "SECOND DISK", "SEC2", "sata"),
                device(
                    "sdb1",
                    "/dev/sdb1",
                    "part",
                    512_000_000,
                    fstype="vfat",
                    uuid="ESP2-UUID",
                    pkname="sdb",
                ),
            ]
        )
        listed = self.run_storage("--list", "--json")
        self.assertEqual(listed.returncode, 0, listed.stdout + listed.stderr)
        names = [entry["device"] for entry in json.loads(listed.stdout)["devices"]]
        self.assertNotIn("/dev/sdb1", names)

        refused = self.run_storage("--device", "/dev/sdb1", "--format")
        self.assertEqual(refused.returncode, 1, refused.stdout + refused.stderr)
        self.assertIn("EFI system partition", refused.stderr)
        self.assertNotIn("mkfs.ext4", self.command_log())
        self.assert_untouched()

    def test_st4_a_replaced_disk_is_refused_before_mkfs(self) -> None:
        """ST-4: serials, models and sizes are re-read immediately before mkfs."""
        self.blank_fixture()
        swapped = Path(self.temp.name) / "swapped.lsblk"
        swapped.write_text(
            self.fixture_text(
                os_disk()
                + [device("sdb", "/dev/sdb", "disk", 256_060_514_304, "SYSTEM SSD", "SYS9", "sata")]
            ),
            encoding="utf-8",
        )
        self.write_fake("install", FAKE_INSTALL)
        result = self.run_storage(
            "--device",
            "/dev/sdb",
            "--format",
            env_extra={
                "FAKE_REAL_INSTALL": REAL_INSTALL,
                "FAKE_SWAP_FIXTURE": str(swapped),
                "FAKE_SWAP_ON_ARG": "bittern/media",
            },
        )
        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        self.assertIn("refusing /dev/sdb", result.stderr)
        self.assertIn("nothing was changed", result.stderr)
        self.assertNotIn("mkfs.ext4", self.command_log())
        self.assertFalse(self.fstab.exists())

    def test_st4_a_changed_filesystem_uuid_is_refused_before_the_mount(self) -> None:
        """ST-4: a reused ext4 is re-identified before it is mounted."""
        self.unlabelled_fixture()
        swapped = Path(self.temp.name) / "swapped.lsblk"
        swapped.write_text(
            self.fixture_text(
                os_disk()
                + [
                    device("sdb", "/dev/sdb", "disk", 1_000_204_886_016, "USB SATA DISK", "USB1", "usb", rm=1),
                    device(
                        "sdb1",
                        "/dev/sdb1",
                        "part",
                        1_000_204_000_000,
                        fstype="ext4",
                        uuid=OTHER_UUID,
                        pkname="sdb",
                        rm=1,
                    ),
                ]
            ),
            encoding="utf-8",
        )
        self.write_fake("install", FAKE_INSTALL)
        result = self.run_storage(
            "--device",
            "/dev/sdb1",
            env_extra={
                "FAKE_REAL_INSTALL": REAL_INSTALL,
                "FAKE_SWAP_FIXTURE": str(swapped),
                "FAKE_SWAP_ON_ARG": "bittern/media",
            },
        )
        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        self.assertIn("filesystem UUID changed", result.stderr)
        self.assertNotIn("mount ", self.command_log())
        self.assertFalse(self.fstab.exists())

    def test_st5_an_interrupted_format_is_not_reused(self) -> None:
        """ST-5: a half-finished format leaves a marker the next run acts on."""
        self.blank_removable_fixture()
        failed = self.run_storage("--device", "/dev/sdb", "--format", env_extra={"FAKE_MKFS_FAIL": "1"})
        self.assertEqual(failed.returncode, 1, failed.stdout + failed.stderr)
        self.assertIn("mkfs.ext4 failed", failed.stderr)
        marker = self.root / "var" / "lib" / "bittern" / "storage-format-in-progress"
        self.assertTrue(marker.exists(), "the interrupted format left no marker")
        self.assertIn("device=/dev/sdb", marker.read_text(encoding="utf-8"))

        # mkfs died after writing the superblock and the label, so the disk now
        # looks like a finished ext4 filesystem with the wrong contents.
        self.blank_removable_fixture(label="bittern-media", fstype="ext4", uuid=FORMATTED_UUID)
        reused = self.run_storage("--device", "/dev/sdb")
        self.assertEqual(reused.returncode, 3, reused.stdout + reused.stderr)
        self.assertIn("did not finish", reused.stderr)
        self.assertIn("--format --device /dev/sdb", reused.stderr)
        self.assertEqual(self.fstab_media_lines(), [])
        self.assertNotIn("mount ", self.command_log())

        # A half-formatted filesystem that is already mounted at the media path
        # must not be accepted as "already configured" either.
        self.media.mkdir(parents=True, exist_ok=True)
        self.media.chmod(0o755)
        (self.root / "fake" / "mounts").write_text(
            f"/dev/sdb\t{self.media.resolve()}\text4\n", encoding="utf-8"
        )
        mounted = self.run_storage("--device", "/dev/sdb")
        self.assertEqual(mounted.returncode, 1, mounted.stdout + mounted.stderr)
        self.assertIn("did not finish", mounted.stderr)
        self.assertIn("unmount", mounted.stderr)
        (self.root / "fake" / "mounts").write_text("", encoding="utf-8")

        repaired = self.run_storage(
            "--device",
            "/dev/sdb",
            "--format",
            env_extra={"FAKE_FORMATTED_UUID": REPAIRED_UUID},
        )
        self.assertEqual(repaired.returncode, 0, repaired.stdout + repaired.stderr)
        self.assertIn("interrupted format", repaired.stdout)
        self.assertIn("mkfs.ext4 -F -L bittern-media /dev/sdb", self.command_log())
        self.assertEqual(
            self.fstab_media_lines(),
            [self.expected_fstab(REPAIRED_UUID).split()],
        )
        self.assertFalse(marker.exists(), "the marker survived a completed format")

    def test_st6_mountpoint_normalisation_yields_one_fstab_line(self) -> None:
        """ST-6: a trailing slash or doubled separator is the same mountpoint."""
        self.whole_disk_fixture()
        self.media.mkdir(parents=True)
        self.media.chmod(0o755)
        self.fstab.write_text(
            f"UUID=OLD1 {self.media}/ ext4 defaults 0 2\n"
            f"UUID=OLD2 {self.media.parent}//media// ext4 defaults 0 2\n"
            f"UUID={OTHER_UUID} /data ext4 defaults 0 2\n",
            encoding="utf-8",
        )
        result = self.run_storage()
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("replace 2 fstab line(s)", result.stdout)
        lines = [line for line in self.fstab.read_text(encoding="utf-8").splitlines() if line.strip()]
        self.assertEqual(len(lines), 2)
        self.assertEqual(self.fstab_media_lines(), [self.expected_fstab(MEDIA_UUID).split()])

    def test_st7_a_failed_verification_restores_the_previous_fstab(self) -> None:
        """ST-7: the fstab line is only kept when the whole state verifies."""
        self.whole_disk_fixture()
        original = f"UUID={OTHER_UUID} /data ext4 defaults 0 2\n# keep this comment\n"
        self.fstab.write_text(original, encoding="utf-8")
        self.write_fake("sync", FAKE_SYNC)
        result = self.run_storage(env_extra={"FAKE_REAL_SYNC": REAL_SYNC})
        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        self.assertIn("sync failed", result.stderr)
        self.assertIn("restored", result.stderr)
        self.assertEqual(self.fstab.read_text(encoding="utf-8"), original)
        self.assertEqual(self.fstab_media_lines(), [])

    def test_st8_a_fstab_outside_the_test_root_is_refused(self) -> None:
        """ST-8: a test-root run never writes a fstab outside the root."""
        self.whole_disk_fixture()
        outside = Path(self.temp.name) / "outside-fstab"
        outside.write_text("", encoding="utf-8")
        env = self.env()
        result = subprocess.run(
            [
                str(SCRIPT),
                "--root",
                str(self.root),
                "--fstab",
                str(outside),
                "--device",
                "/dev/sdb",
            ],
            text=True,
            capture_output=True,
            env=env,
            timeout=20,
            check=False,
            stdin=subprocess.DEVNULL,
        )
        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        self.assertIn("--fstab must be inside --root", result.stderr)
        self.assertEqual(outside.read_text(encoding="utf-8"), "")
        self.assertNotIn("mkfs.ext4", self.command_log())

        escape = subprocess.run(
            [
                str(SCRIPT),
                "--root",
                str(self.root),
                "--fstab",
                f"{self.root}/../fstab",
                "--list",
            ],
            text=True,
            capture_output=True,
            env=env,
            timeout=20,
            check=False,
            stdin=subprocess.DEVNULL,
        )
        self.assertEqual(escape.returncode, 1, escape.stdout + escape.stderr)
        self.assertIn("--fstab must be inside --root", escape.stderr)

    def test_st10_a_stale_post_format_uuid_is_refused(self) -> None:
        """ST-10: the UUID after mkfs must differ from the one before it."""
        self.blank_removable_fixture(fstype="ntfs", uuid=OTHER_UUID)
        result = self.run_storage(
            "--device",
            "/dev/sdb",
            "--format",
            env_extra={"FAKE_FORMATTED_UUID": OTHER_UUID},
        )
        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        self.assertIn(f"still {OTHER_UUID}", result.stderr)
        self.assertIn("udev has not caught up", result.stderr)
        self.assertEqual(self.fstab_media_lines(), [])
        self.assertNotIn("mount ", self.command_log())


if __name__ == "__main__":
    unittest.main()
#!/usr/bin/env python3
"""Run the Bittern two-NIC QEMU harness.

The runner uses only the Python standard library. It fetches and verifies the
Debian cloud image on first use, creates qcow2 overlays, boots an appliance VM
and a camera VM, snapshots both after first boot, and restores those snapshots
before each test.

No artifact under build/qemu/ is committed. The runner never needs a TAP device
or a Docker socket on the host.
"""

from __future__ import annotations

import argparse
import contextlib
import fcntl
import hashlib
import html
from html.parser import HTMLParser
import http.cookiejar
import http.client
import json
import os
import re
import shlex
import shutil
import socket
import ssl
import subprocess
import sys
import tarfile
import time
import urllib.error
import urllib.parse
import urllib.request
from dataclasses import dataclass
from pathlib import Path
from typing import Any

ROOT = Path(__file__).resolve().parents[2]
HERE = Path(__file__).resolve().parent
BUILD = ROOT / "build" / "qemu"
CACHE = BUILD / "cache"
VMS = BUILD / "vms"
SEEDS = BUILD / "seeds"
RUN = BUILD / "run"
LOGS = BUILD / "logs"
SSH_DIR = BUILD / "ssh"
BIN = BUILD / "bin"
STATE_FILE = RUN / "state.json"
QEMU = "qemu-system-x86_64"
RUN_COMMAND = f"python3 {Path(__file__).resolve().relative_to(ROOT).as_posix()}"
VM_MARKERS = {"appliance": "bittern-appliance", "camera": "bittern-camera"}
SNAPSHOT_TAG = "baseline"
KVM_GET_API_VERSION = 0xAE00
RENDERED_NFTABLES = ROOT / "tests" / "render" / "four-cameras" / "expected" / "nftables" / "bittern.nft"
HARNESS_SSH_RULE = 'iifname "lan0" ct state new tcp dport 22 accept'


class HarnessError(RuntimeError):
    pass


def log(message: str) -> None:
    print(f"qemu-harness: {message}", flush=True)


def run_command(args: list[str], **kwargs: Any) -> subprocess.CompletedProcess[str]:
    return subprocess.run(args, text=True, capture_output=True, **kwargs)


class DataAttributeParser(HTMLParser):
    """Collect the values of one data-* attribute from a rendered page."""

    def __init__(self, attribute: str) -> None:
        super().__init__()
        self.attribute = attribute
        self.values: list[str] = []

    def handle_starttag(self, tag: str, attrs: list[tuple[str, str | None]]) -> None:
        for name, value in attrs:
            if name == self.attribute and value is not None:
                self.values.append(value)


DATA_ATTRIBUTE_NAME = re.compile(r"data-[a-z0-9]+(?:-[a-z0-9]+)*")


def data_values(page: str, attribute: str) -> list[str]:
    """Return every value of a data-* hook. The hook, not the copy, is the contract."""
    if DATA_ATTRIBUTE_NAME.fullmatch(attribute) is None:
        raise HarnessError(f"invalid data attribute name {attribute!r}")
    parser = DataAttributeParser(attribute)
    parser.feed(page)
    parser.close()
    return parser.values


def require_data(page: str, attribute: str, expected: str, context: str) -> None:
    values = data_values(page, attribute)
    if expected not in values:
        raise HarnessError(
            f"{context} does not carry {attribute}={expected!r}; found {values!r}"
        )


ANSI_QR_ESCAPE = re.compile(r"\x1b\[([0-9;]*)m")


def ansi_qr_pgm(output: str) -> bytes:
    """Turn qrencode's ANSI terminal output into a PGM image for zbarimg."""
    rows: list[list[int]] = []
    for line in output.splitlines():
        if "\x1b[" not in line:
            continue
        cells: list[int] = []
        background = ""
        index = 0
        while index < len(line):
            if line[index] == "\x1b":
                match = ANSI_QR_ESCAPE.match(line, index)
                if match is None:
                    raise HarnessError("the setup QR has an unterminated ANSI escape")
                code = match.group(1)
                if code in ("40", "47"):
                    background = code
                elif code != "0":
                    raise HarnessError(f"the setup QR has an unexpected ANSI code {code!r}")
                index = match.end()
                continue
            if line[index] != " ":
                raise HarnessError(f"the setup QR has an unexpected character {line[index]!r}")
            if background not in ("40", "47"):
                raise HarnessError("the setup QR has a character without a background colour")
            cells.append(0 if background == "40" else 255)
            index += 1
        if not cells:
            continue
        if len(cells) % 2 != 0:
            raise HarnessError("the setup QR row has an odd number of terminal cells")
        row: list[int] = []
        for offset in range(0, len(cells), 2):
            if cells[offset] != cells[offset + 1]:
                raise HarnessError("the setup QR module is not two terminal cells wide")
            row.append(cells[offset])
        rows.append(row)
    if not rows:
        raise HarnessError("the setup QR output has no ANSI rows")
    width = len(rows[0])
    if any(len(row) != width for row in rows):
        raise HarnessError("the setup QR rows have different widths")
    scale = 4
    image = bytearray(f"P5\n{width * scale} {len(rows) * scale}\n255\n".encode("ascii"))
    for row in rows:
        line = bytearray()
        for value in row:
            line.extend(bytes([value]) * scale)
        for _ in range(scale):
            image.extend(line)
    return bytes(image)


def decode_ansi_qr(output: str) -> str:
    """Decode the terminal QR with the test-only zbarimg command."""
    if shutil.which("zbarimg") is None:
        raise HarnessError(
            "zbarimg is not installed; install zbar-tools on the host to decode the setup QR"
        )
    result = subprocess.run(
        ["zbarimg", "-q", "--raw", "-"],
        input=ansi_qr_pgm(output),
        capture_output=True,
    )
    if result.returncode != 0:
        detail = result.stderr.decode("utf-8", "replace").strip()[:500]
        raise HarnessError(f"zbarimg could not decode the setup QR: {detail}")
    return result.stdout.decode("utf-8", "replace").strip()


def load_config() -> dict[str, Any]:
    # The override keeps a second harness worktree from colliding with the
    # fixed host forwards in config.json. The committed defaults stay unchanged.
    override = os.environ.get("BITTERN_QEMU_CONFIG", "").strip()
    path = Path(override) if override else HERE / "config.json"
    with path.open(encoding="utf-8") as handle:
        return json.load(handle)


def ensure_directories() -> None:
    for path in (CACHE, VMS, SEEDS, RUN, LOGS, SSH_DIR, BIN):
        path.mkdir(parents=True, exist_ok=True)


def require_tools() -> None:
    missing = [
        tool
        for tool in (QEMU, "qemu-img", "cloud-localds", "ssh", "scp", "ssh-keygen")
        if shutil.which(tool) is None
    ]
    if missing:
        raise HarnessError(
            "missing host tools: "
            + ", ".join(missing)
            + ". Install qemu-system-x86 qemu-utils cloud-image-utils openssh-client."
        )


def sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for block in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(block)
    return digest.hexdigest()


def sha512_file(path: Path) -> str:
    digest = hashlib.sha512()
    with path.open("rb") as handle:
        for block in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(block)
    return digest.hexdigest()


def fetch_image(config: dict[str, Any], refresh: bool) -> Path:
    image = config["image"]
    destination = CACHE / image["name"]
    if destination.exists() and not refresh:
        actual = sha256_file(destination)
        if actual == image["sha256"]:
            log(f"using cached image {destination} (sha256 verified)")
            return destination
        raise HarnessError(
            f"cached image {destination} has sha256 {actual}, expected {image['sha256']}. "
            "Remove it or rerun prepare with --refresh-image."
        )

    temporary = destination.with_name(destination.name + ".part")
    log(f"fetching {image['url']} (first run only)")
    request = urllib.request.Request(image["url"], headers={"User-Agent": "bittern-qemu-harness/1"})
    try:
        with urllib.request.urlopen(request, timeout=60) as response, temporary.open("wb") as out:
            total = int(response.headers.get("Content-Length") or 0)
            copied = 0
            last_report = 0
            while True:
                block = response.read(1024 * 1024)
                if not block:
                    break
                out.write(block)
                copied += len(block)
                if total and copied - last_report >= 50 * 1024 * 1024:
                    log(f"  downloaded {copied // (1024 * 1024)} of {total // (1024 * 1024)} MiB")
                    last_report = copied
        actual_sha256 = sha256_file(temporary)
        actual_sha512 = sha512_file(temporary)
        if actual_sha256 != image["sha256"]:
            raise HarnessError(
                f"image sha256 mismatch: got {actual_sha256}, expected {image['sha256']}"
            )
        if actual_sha512 != image["sha512"]:
            raise HarnessError(
                f"image sha512 mismatch: got {actual_sha512}, expected {image['sha512']}"
            )
        temporary.replace(destination)
        log(f"verified image sha256 {actual_sha256}")
        return destination
    finally:
        temporary.unlink(missing_ok=True)


def ensure_ssh_key(config: dict[str, Any]) -> Path:
    key = SSH_DIR / "id_ed25519"
    if not key.exists():
        SSH_DIR.mkdir(parents=True, exist_ok=True)
        os.chmod(SSH_DIR, 0o700)
        result = run_command(
            [
                "ssh-keygen",
                "-q",
                "-t",
                "ed25519",
                "-N",
                "",
                "-C",
                "bittern-qemu-harness",
                "-f",
                str(key),
            ]
        )
        if result.returncode != 0:
            raise HarnessError(f"ssh-keygen failed: {result.stderr.strip()}")
        os.chmod(key, 0o600)
    public_key = key.with_suffix(".pub")
    if not public_key.exists():
        raise HarnessError(f"missing public key {public_key}")
    return key


def render_template(source: Path, destination: Path, replacements: dict[str, str]) -> None:
    text = source.read_text(encoding="utf-8")
    for needle, value in replacements.items():
        text = text.replace(needle, value)
    if re.findall(r"__[A-Z0-9_]+__", text):
        raise HarnessError(f"unrendered placeholder remains in {source}")
    destination.write_text(text, encoding="utf-8")


def render_cloud_init(config: dict[str, Any], ssh_public_key: str) -> None:
    cam = config["network"]["cam"]
    macs = {
        "appliance_lan": "52:54:00:be:00:01",
        "appliance_cam": "52:54:00:be:00:02",
        "camera_mgmt": "52:54:00:be:00:03",
        "camera_cam": "52:54:00:be:00:04",
    }
    common = {
        "__SSH_USER__": config["vm"]["ssh_user"],
        "__SSH_PUBKEY__": ssh_public_key,
    }
    appliance = dict(common)
    appliance.update(
        {
            "__HOSTNAME__": "bittern-appliance",
            "__LAN_MAC__": macs["appliance_lan"],
            "__CAM_MAC__": macs["appliance_cam"],
            "__CAM_ADDR__": cam["appliance_address"],
        }
    )
    camera = dict(common)
    camera.update(
        {
            "__HOSTNAME__": "bittern-camera",
            "__MGMT_MAC__": macs["camera_mgmt"],
            "__CAM_MAC__": macs["camera_cam"],
            "__CAM_ADDR__": cam["camera_address"],
            "__CAM_RES__": config["camera"]["resolution"],
            "__CAM_FPS__": str(config["camera"]["fps"]),
            "__CAM_PATH__": config["camera"]["rtsp_path"],
            "__MEDIAMTX_URL__": config["camera"]["mediamtx"]["url"],
            "__MEDIAMTX_SHA256__": config["camera"]["mediamtx"]["sha256"],
        }
    )
    meta_appliance = {
        "__INSTANCE_ID__": "bittern-appliance-harness",
        "__HOSTNAME__": "bittern-appliance",
    }
    meta_camera = {
        "__INSTANCE_ID__": "bittern-camera-harness",
        "__HOSTNAME__": "bittern-camera",
    }
    render_template(
        HERE / "cloud-init" / "appliance-user-data.yaml",
        SEEDS / "appliance-user-data.yaml",
        appliance,
    )
    render_template(
        HERE / "cloud-init" / "appliance-network-config.yaml",
        SEEDS / "appliance-network-config.yaml",
        appliance,
    )
    render_template(
        HERE / "cloud-init" / "meta-data.template",
        SEEDS / "appliance-meta-data.yaml",
        meta_appliance,
    )
    render_template(
        HERE / "cloud-init" / "camera-user-data.yaml",
        SEEDS / "camera-user-data.yaml",
        camera,
    )
    render_template(
        HERE / "cloud-init" / "camera-network-config.yaml",
        SEEDS / "camera-network-config.yaml",
        camera,
    )
    render_template(
        HERE / "cloud-init" / "meta-data.template",
        SEEDS / "camera-meta-data.yaml",
        meta_camera,
    )
    for name in ("appliance", "camera"):
        result = run_command(
            [
                "cloud-localds",
                "-N",
                str(SEEDS / f"{name}-network-config.yaml"),
                str(SEEDS / f"{name}-seed.iso"),
                str(SEEDS / f"{name}-user-data.yaml"),
                str(SEEDS / f"{name}-meta-data.yaml"),
            ]
        )
        if result.returncode != 0:
            raise HarnessError(f"cloud-localds failed for {name}: {result.stderr.strip()}")


def create_overlay(config: dict[str, Any], base_image: Path, name: str, force: bool) -> Path:
    overlay = VMS / f"{name}.qcow2"
    if overlay.exists() and not force:
        return overlay
    overlay.unlink(missing_ok=True)
    result = run_command(
        [
            "qemu-img",
            "create",
            "-f",
            "qcow2",
            "-F",
            "qcow2",
            "-b",
            str(base_image.resolve()),
            str(overlay),
        ]
    )
    if result.returncode != 0:
        raise HarnessError(f"qemu-img create failed: {result.stderr.strip()}")
    result = run_command(["qemu-img", "resize", str(overlay), config["vm"]["disk_size"]])
    if result.returncode != 0:
        raise HarnessError(f"qemu-img resize failed: {result.stderr.strip()}")
    return overlay


def load_state() -> dict[str, Any]:
    if not STATE_FILE.exists():
        return {}
    try:
        return json.loads(STATE_FILE.read_text(encoding="utf-8"))
    except json.JSONDecodeError:
        return {}


def save_state(state: dict[str, Any]) -> None:
    STATE_FILE.parent.mkdir(parents=True, exist_ok=True)
    STATE_FILE.write_text(json.dumps(state, indent=2, sort_keys=True) + "\n", encoding="utf-8")


def read_pid(pidfile: Path) -> int | None:
    try:
        return int(pidfile.read_text(encoding="utf-8").strip())
    except (FileNotFoundError, ValueError):
        return None


@dataclass(frozen=True)
class QemuProcess:
    pid: int
    name: str
    cmdline: str
    worktree: str
    ports: tuple[int, ...]
    qmp_path: str | None

    def describe(self) -> str:
        worktree = self.worktree or "unknown"
        ports = ",".join(str(port) for port in self.ports) or "none"
        return f"{self.name} pid={self.pid} worktree={worktree} ports={ports}"


def process_is_alive(pid: int) -> bool:
    try:
        stat = Path(f"/proc/{pid}/stat").read_text(encoding="utf-8")
    except OSError:
        return False
    try:
        state = stat.rsplit(") ", 1)[1].split()[0]
    except IndexError:
        return False
    return state != "Z"


def read_argv(pid: int) -> list[str]:
    try:
        content = Path(f"/proc/{pid}/cmdline").read_bytes()
    except OSError:
        return []
    return [
        part.decode("utf-8", "replace")
        for part in content.split(b"\0")
        if part
    ]


def read_cmdline(pid: int) -> str:
    return " ".join(read_argv(pid)).strip()


def process_is_qemu(pid: int) -> bool:
    argv = read_argv(pid)
    return bool(argv) and "qemu-system" in Path(argv[0]).name


def cmdline_worktree(cmdline: str) -> str:
    marker = "build/qemu"
    for token in cmdline.split():
        index = token.rfind(marker)
        if index < 0:
            continue
        prefix = token[:index]
        cut = max(prefix.rfind(separator) for separator in "=:'\"") + 1
        worktree = prefix[cut:].rstrip("/")
        if worktree:
            return worktree
    return ""


def cmdline_qmp_path(cmdline: str) -> str | None:
    tokens = cmdline.split()
    for index, token in enumerate(tokens):
        spec = ""
        if token == "-qmp" and index + 1 < len(tokens):
            spec = tokens[index + 1]
        elif token.startswith("-qmp="):
            spec = token.split("=", 1)[1]
        if spec.startswith("unix:"):
            return spec[5:].split(",", 1)[0]
    return None


def cmdline_host_ports(cmdline: str) -> tuple[int, ...]:
    ports = re.findall(r"hostfwd=tcp:[^\s,]+:(\d+)-:", cmdline)
    return tuple(sorted({int(port) for port in ports}))


def scan_qemu_processes() -> list[QemuProcess]:
    processes: list[QemuProcess] = []
    try:
        entries = list(Path("/proc").iterdir())
    except OSError:
        return processes
    for entry in entries:
        if not entry.name.isdigit():
            continue
        pid = int(entry.name)
        if not process_is_qemu(pid):
            continue
        cmdline = read_cmdline(pid)
        name = next(
            (candidate for candidate, marker in VM_MARKERS.items() if marker in cmdline),
            None,
        )
        if name is None:
            continue
        processes.append(
            QemuProcess(
                pid=pid,
                name=name,
                cmdline=cmdline,
                worktree=cmdline_worktree(cmdline),
                ports=cmdline_host_ports(cmdline),
                qmp_path=cmdline_qmp_path(cmdline),
            )
        )
    return sorted(processes, key=lambda process: (process.worktree, process.name, process.pid))


def process_is_vm(pid: int | None, name: str) -> bool:
    if pid is None or not process_is_alive(pid) or not process_is_qemu(pid):
        return False
    cmdline = read_cmdline(pid)
    return VM_MARKERS[name] in cmdline and str(BUILD) in cmdline


def process_in_this_worktree(process: QemuProcess) -> bool:
    if not process.worktree:
        return False
    return Path(process.worktree).resolve() == ROOT


def process_worktree_is_gone(process: QemuProcess) -> bool:
    return not process.worktree or not Path(process.worktree).is_dir()


def stop_qemu_process(process: QemuProcess) -> bool:
    if not process_is_alive(process.pid):
        return False
    log(f"stopping {process.name} pid {process.pid} ({process.worktree or 'unknown worktree'})")
    if process.qmp_path and Path(process.qmp_path).exists():
        qmp: Qmp | None = None
        try:
            qmp = Qmp(Path(process.qmp_path), timeout=2.0)
            qmp.command("quit")
        except HarnessError:
            pass
        finally:
            if qmp is not None:
                qmp.close()
    deadline = time.monotonic() + 10
    while time.monotonic() < deadline and process_is_alive(process.pid):
        time.sleep(0.2)
    if process_is_alive(process.pid):
        try:
            os.kill(process.pid, 15)
        except OSError:
            pass
        deadline = time.monotonic() + 5
        while time.monotonic() < deadline and process_is_alive(process.pid):
            time.sleep(0.2)
    if process_is_alive(process.pid):
        try:
            os.kill(process.pid, 9)
        except OSError:
            pass
        deadline = time.monotonic() + 2
        while time.monotonic() < deadline and process_is_alive(process.pid):
            time.sleep(0.1)
    if process_is_alive(process.pid):
        raise HarnessError(
            f"could not stop {process.name} pid {process.pid} ({process.worktree or 'unknown worktree'})"
        )
    log(f"stopped {process.name} pid {process.pid}")
    return True


def listening_socket_inodes(port: int) -> set[str]:
    inodes: set[str] = set()
    for table in ("/proc/net/tcp", "/proc/net/tcp6"):
        try:
            lines = Path(table).read_text(encoding="utf-8").splitlines()[1:]
        except OSError:
            continue
        for line in lines:
            fields = line.split()
            if len(fields) < 10 or fields[3] != "0A":
                continue
            try:
                local_port = int(fields[1].rsplit(":", 1)[1], 16)
            except (IndexError, ValueError):
                continue
            if local_port == port and fields[9] != "0":
                inodes.add(fields[9])
    return inodes


def port_owners(port: int) -> list[tuple[int, str]]:
    inodes = listening_socket_inodes(port)
    if not inodes:
        return []
    owners: list[tuple[int, str]] = []
    try:
        entries = list(Path("/proc").iterdir())
    except OSError:
        return owners
    for entry in entries:
        if not entry.name.isdigit():
            continue
        pid = int(entry.name)
        try:
            fds = list(Path(f"/proc/{pid}/fd").iterdir())
        except OSError:
            continue
        for fd in fds:
            try:
                target = os.readlink(fd)
            except OSError:
                continue
            if target.startswith("socket:[") and target[8:-1] in inodes:
                owners.append((pid, read_cmdline(pid) or "<command line unavailable>"))
                break
    return sorted(owners)


class Qmp:
    def __init__(self, path: Path, timeout: float = 10.0) -> None:
        deadline = time.monotonic() + timeout
        self.sock: socket.socket | None = None
        while time.monotonic() < deadline:
            try:
                self.sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
                self.sock.settimeout(timeout)
                self.sock.connect(str(path))
                break
            except OSError:
                if self.sock is not None:
                    self.sock.close()
                self.sock = None
                time.sleep(0.1)
        if self.sock is None:
            raise HarnessError(f"cannot connect to QMP socket {path}")
        self.reader = self.sock.makefile("rb")
        try:
            greeting = self.reader.readline()
        except (OSError, TimeoutError) as exc:
            self.close()
            raise HarnessError(f"QMP socket {path} did not send a greeting: {exc}") from exc
        if not greeting:
            self.close()
            raise HarnessError(f"QMP socket {path} closed before greeting")
        try:
            self.command("qmp_capabilities")
        except (OSError, TimeoutError) as exc:
            self.close()
            raise HarnessError(f"QMP socket {path} did not accept qmp_capabilities: {exc}") from exc

    def command(self, name: str, arguments: dict[str, Any] | None = None) -> Any:
        assert self.sock is not None
        payload: dict[str, Any] = {"execute": name}
        if arguments:
            payload["arguments"] = arguments
        self.sock.sendall((json.dumps(payload) + "\n").encode("utf-8"))
        while True:
            line = self.reader.readline()
            if not line:
                raise HarnessError(f"QMP connection closed while waiting for {name}")
            message = json.loads(line)
            if "event" in message:
                continue
            if "error" in message:
                error = message["error"]
                raise HarnessError(f"QMP {name} failed: {error.get('desc', error)}")
            return message.get("return")

    def close(self) -> None:
        try:
            self.reader.close()
        except Exception:
            pass
        if self.sock is not None:
            self.sock.close()


class Vm:
    def __init__(self, name: str, config: dict[str, Any]) -> None:
        self.name = name
        self.config = config
        self.overlay = VMS / f"{name}.qcow2"
        self.seed = SEEDS / f"{name}-seed.iso"
        self.qmp_path = RUN / f"{name}.qmp"
        self.pidfile = RUN / f"{name}.pid"
        self.serial_log = LOGS / f"{name}-serial.log"
        self.qemu_log = LOGS / f"{name}-qemu.log"
        if name == "appliance":
            self.ssh_port = int(config["network"]["lan"]["hostfwd"]["ssh"])
        else:
            self.ssh_port = int(config["camera"]["host_ssh_port"])
        self.cam_socket = RUN / f"cam0-{name}.sock"

    def pid(self) -> int | None:
        return read_pid(self.pidfile)

    def running(self) -> bool:
        return process_is_vm(self.pid(), self.name)

    def has_snapshot(self, tag: str = SNAPSHOT_TAG) -> bool:
        if not self.overlay.exists():
            return False
        result = run_command(["qemu-img", "snapshot", "-l", "-U", str(self.overlay)])
        if result.returncode != 0:
            return False
        for line in result.stdout.splitlines():
            parts = line.split()
            if len(parts) >= 2 and parts[1] == tag:
                return True
        return False

    def command(self, accelerator: str, load_snapshot: bool) -> list[str]:
        config = self.config
        args = [
            QEMU,
            "-name",
            f"bittern-{self.name}",
            "-machine",
            "q35",
            "-accel",
            accelerator,
            "-cpu",
            "host" if accelerator == "kvm" else "max",
            "-smp",
            str(config["vm"]["cpus"]),
            "-m",
            str(config["vm"]["memory_mb"]),
            "-nodefaults",
            # Debian's GRUB config is GRUB_TERMINAL_OUTPUT="gfxterm serial" and calls
            # load_video before booting. With -nodefaults and no video device, GRUB faults
            # and the guest resets right after printing "Booting", before the kernel or
            # the initrd is loaded; there is no serial output to explain it. A standard
            # VGA device keeps GRUB and the guest happy. -display none still means headless.
            "-vga",
            "std",
            "-boot",
            "order=c",
            "-drive",
            f"file={self.overlay},if=virtio,format=qcow2,cache=writeback,discard=unmap",
            "-drive",
            f"file={self.seed},if=virtio,format=raw,readonly=on",
            "-display",
            "none",
            "-serial",
            f"file:{self.serial_log}",
            "-qmp",
            f"unix:{self.qmp_path},server=on,wait=off",
            "-pidfile",
            str(self.pidfile),
        ]
        if load_snapshot and self.has_snapshot():
            args.extend(["-loadvm", SNAPSHOT_TAG])
        if self.name == "appliance":
            lan = config["network"]["lan"]
            forwards = ",".join(
                f"hostfwd=tcp:127.0.0.1:{port}-:{guest}"
                for port, guest in (
                    (lan["hostfwd"]["ssh"], 22),
                    (lan["hostfwd"]["frigate_internal"], 5000),
                    (lan["hostfwd"]["go2rtc"], 1984),
                    (lan["hostfwd"]["rtsp"], 8554),
                    (lan["hostfwd"]["webrtc"], 8555),
                    (lan["hostfwd"]["ui"], 443),
                )
            )
            args.extend(
                [
                    "-netdev",
                    f"user,id=lan0,{forwards}",
                    "-device",
                    "virtio-net-pci,id=bittern-lan0,netdev=lan0,mac=52:54:00:be:00:01",
                ]
            )
        else:
            args.extend(
                [
                    "-netdev",
                    f"user,id=mgmt0,hostfwd=tcp:127.0.0.1:{config['camera']['host_ssh_port']}-:22,"
                    f"hostfwd=tcp:127.0.0.1:{config['camera']['host_rtsp_port']}-:8554",
                    "-device",
                    "virtio-net-pci,id=bittern-mgmt0,netdev=mgmt0,mac=52:54:00:be:00:03",
                ]
            )
        args.extend(
            [
                "-netdev",
                f"dgram,id=cam0,local.type=unix,local.path={self.cam_socket},"
                f"remote.type=unix,remote.path={RUN / ('cam0-camera.sock' if self.name == 'appliance' else 'cam0-appliance.sock')}",
                "-device",
                "virtio-net-pci,netdev=cam0,mac="
                + ("52:54:00:be:00:02" if self.name == "appliance" else "52:54:00:be:00:04")
                + (",id=bittern-cam0" if self.name == "appliance" else ",id=bittern-camera-cam0"),
            ]
        )
        return args

    def check_host_ports(self) -> None:
        ports = (
            [
                self.config["network"]["lan"]["hostfwd"][key]
                for key in ("ssh", "frigate_internal", "go2rtc", "rtsp", "webrtc", "ui")
            ]
            if self.name == "appliance"
            else [self.config["camera"]["host_ssh_port"], self.config["camera"]["host_rtsp_port"]]
        )
        for port in ports:
            with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as probe:
                probe.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
                try:
                    probe.bind(("127.0.0.1", int(port)))
                except OSError as exc:
                    owners = port_owners(int(port))
                    if owners:
                        owner_lines = "\n".join(
                            f"  pid {pid}: {cmdline}" for pid, cmdline in owners
                        )
                        detail = f"host port {port} for {self.name} is already in use by:\n{owner_lines}"
                    else:
                        detail = (
                            f"host port {port} for {self.name} is already in use: "
                            f"{exc.strerror}; the owning process could not be identified"
                        )
                    raise HarnessError(
                        f"{detail}\n"
                        f"Run '{RUN_COMMAND} stop' to stop this worktree's guests, or "
                        f"'{RUN_COMMAND} status' to list Bittern QEMU processes. "
                        "For a deliberate parallel run, set BITTERN_QEMU_CONFIG to a config "
                        "with different host ports."
                    ) from exc

    def start(self, accelerator: str, load_snapshot: bool) -> bool:
        if self.running():
            return False
        self.check_host_ports()
        self.qmp_path.unlink(missing_ok=True)
        self.pidfile.unlink(missing_ok=True)
        self.cam_socket.unlink(missing_ok=True)
        self.qemu_log.unlink(missing_ok=True)
        self.serial_log.unlink(missing_ok=True)
        log(f"starting {self.name} with {accelerator}")
        with self.qemu_log.open("wb") as output:
            process = subprocess.Popen(
                self.command(accelerator, load_snapshot),
                stdin=subprocess.DEVNULL,
                stdout=output,
                stderr=subprocess.STDOUT,
                start_new_session=True,
                close_fds=True,
            )
        deadline = time.monotonic() + 15
        while time.monotonic() < deadline:
            if process.poll() is not None:
                tail = self.qemu_log.read_text(encoding="utf-8", errors="replace")[-4000:]
                raise HarnessError(f"{self.name} exited during start:\n{tail}")
            try:
                Qmp(self.qmp_path, timeout=2.0).close()
                return True
            except HarnessError:
                time.sleep(0.1)
        raise HarnessError(f"{self.name} did not open QMP at {self.qmp_path}")

    def qmp(self) -> Qmp:
        return Qmp(self.qmp_path)

    def hmp(self, command: str) -> str:
        qmp = self.qmp()
        try:
            result = qmp.command("human-monitor-command", {"command-line": command})
        finally:
            qmp.close()
        text = str(result or "")
        if text.lower().startswith("error"):
            raise HarnessError(f"HMP command failed on {self.name}: {command}: {text}")
        return text

    def set_link(self, device: str, up: bool) -> None:
        qmp = self.qmp()
        try:
            qmp.command("set_link", {"name": device, "up": up})
        finally:
            qmp.close()

    def save_snapshot(self, tag: str = SNAPSHOT_TAG) -> None:
        try:
            self.hmp(f"delvm {tag}")
        except HarnessError:
            pass
        self.hmp(f"savevm {tag}")

    def load_snapshot(self, tag: str = SNAPSHOT_TAG) -> None:
        self.hmp(f"loadvm {tag}")

    def stop(self) -> list[QemuProcess]:
        processes = [
            process
            for process in scan_qemu_processes()
            if process.name == self.name and process_in_this_worktree(process)
        ]
        pid = self.pid()
        if pid is not None and process_is_vm(pid, self.name):
            if not any(process.pid == pid for process in processes):
                cmdline = read_cmdline(pid)
                processes.append(
                    QemuProcess(
                        pid=pid,
                        name=self.name,
                        cmdline=cmdline,
                        worktree=str(ROOT),
                        ports=cmdline_host_ports(cmdline),
                        qmp_path=cmdline_qmp_path(cmdline),
                    )
                )
        stopped = [process for process in processes if stop_qemu_process(process)]
        self.pidfile.unlink(missing_ok=True)
        return stopped

    def ssh(
        self,
        command: str,
        *,
        input_text: str | None = None,
        timeout: float = 30,
        check: bool = True,
        retries: int = 3,
    ) -> subprocess.CompletedProcess[str]:
        key = ensure_ssh_key(self.config)
        args = [
            "ssh",
            "-i",
            str(key),
            "-p",
            str(self.ssh_port),
            "-o",
            "BatchMode=yes",
            "-o",
            "ConnectTimeout=5",
            "-o",
            "StrictHostKeyChecking=no",
            "-o",
            "UserKnownHostsFile=/dev/null",
            "-o",
            "LogLevel=ERROR",
            f"{self.config['vm']['ssh_user']}@127.0.0.1",
            command,
        ]
        result: subprocess.CompletedProcess[str] | None = None
        for attempt in range(1, retries + 1):
            try:
                result = run_command(args, input=input_text, timeout=timeout)
            except subprocess.TimeoutExpired as exc:
                if attempt < retries and input_text is None:
                    time.sleep(1)
                    continue
                raise HarnessError(f"ssh command timed out on {self.name}: {command}") from exc
            if result.returncode == 0 or attempt == retries or input_text is not None:
                break
            # A snapshot restore can briefly leave sshd answering before it
            # accepts the harness key. Retry only transport/authentication
            # failures, never a command that already ran.
            transient = any(
                marker in result.stderr
                for marker in (
                    "Permission denied (publickey)",
                    "Connection closed by remote host",
                    "Connection timed out during banner exchange",
                    "Connection reset by peer",
                )
            )
            if not transient:
                break
            time.sleep(1)
        assert result is not None
        if check and result.returncode != 0:
            raise HarnessError(
                f"ssh command failed on {self.name}: {command}\n"
                f"{result.stdout}{result.stderr}".strip()
            )
        return result

    def scp_to(self, source: Path, destination: str, *, timeout: float = 120) -> None:
        key = ensure_ssh_key(self.config)
        args = [
            "scp",
            "-q",
            "-i",
            str(key),
            "-P",
            str(self.ssh_port),
            "-o",
            "BatchMode=yes",
            "-o",
            "ConnectTimeout=5",
            "-o",
            "StrictHostKeyChecking=no",
            "-o",
            "UserKnownHostsFile=/dev/null",
            "-o",
            "LogLevel=ERROR",
            str(source),
            f"{self.config['vm']['ssh_user']}@127.0.0.1:{destination}",
        ]
        try:
            result = run_command(args, timeout=timeout)
        except subprocess.TimeoutExpired as exc:
            raise HarnessError(f"scp to {self.name} timed out: {destination}") from exc
        if result.returncode != 0:
            raise HarnessError(
                f"scp to {self.name} failed for {destination}: "
                f"{result.stdout}{result.stderr}".strip()
            )

    def write_remote_file(
        self,
        path: str,
        content: str,
        *,
        mode: str = "0644",
        timeout: float = 30,
    ) -> None:
        # install reads /dev/stdin as root, which avoids a temporary file with
        # the content and preserves the requested mode in one step.
        command = f"sudo -n install -m {mode} /dev/stdin {shlex.quote(path)}"
        self.ssh(command, input_text=content, timeout=timeout)


def choose_accelerator(config: dict[str, Any]) -> tuple[str, str]:
    if shutil.which(QEMU) is None:
        raise HarnessError(f"{QEMU} not found; install qemu-system-x86")
    result = run_command([QEMU, "-accel", "help"])
    available = (result.stdout + result.stderr).lower()
    kvm_reason = "/dev/kvm missing"
    if Path("/dev/kvm").exists():
        try:
            fd = os.open("/dev/kvm", os.O_RDWR)
            try:
                api_version = fcntl.ioctl(fd, KVM_GET_API_VERSION, 0)
            finally:
                os.close(fd)
            if api_version >= 12 and "kvm" in available:
                return "kvm", f"/dev/kvm KVM_GET_API_VERSION={api_version}"
            kvm_reason = f"/dev/kvm present but KVM_GET_API_VERSION returned {api_version}"
        except OSError as exc:
            kvm_reason = f"/dev/kvm present but unusable: {exc.strerror}"
    if "tcg" not in available:
        raise HarnessError(
            f"neither KVM nor TCG is available: {kvm_reason}; QEMU reports accel: {available.strip()}"
        )
    return "tcg,thread=multi", f"{kvm_reason}; falling back to TCG"


def budgets(config: dict[str, Any], accelerator: str) -> dict[str, int]:
    prefix = "kvm" if accelerator == "kvm" else "tcg"
    timeouts = config["timeouts_seconds"]
    return {
        "boot": int(timeouts[f"{prefix}_boot"]),
        "snapshot_restore": int(timeouts[f"{prefix}_snapshot_restore"]),
        "test_suite": int(timeouts[f"{prefix}_test_suite"]),
    }


def wait_for_tcp(port: int, timeout: float) -> None:
    deadline = time.monotonic() + timeout
    last_error = "no attempt"
    while time.monotonic() < deadline:
        try:
            with socket.create_connection(("127.0.0.1", port), timeout=2):
                return
        except OSError as exc:
            last_error = str(exc)
            time.sleep(0.25)
    raise HarnessError(f"port 127.0.0.1:{port} did not open within {timeout:.0f}s: {last_error}")


def rtsp_probe(host: str, port: int, path: str, timeout: float = 5) -> str:
    request = (
        f"OPTIONS rtsp://{host}:{port}{path} RTSP/1.0\r\n"
        "CSeq: 1\r\n"
        "User-Agent: bittern-qemu-harness\r\n\r\n"
    )
    try:
        with socket.create_connection((host, port), timeout=timeout) as sock:
            sock.sendall(request.encode())
            response = sock.recv(4096)
    except OSError as exc:
        raise HarnessError(f"RTSP probe to {host}:{port}{path} failed: {exc}") from exc
    first = response.split(b"\r\n", 1)[0].decode("utf-8", "replace")
    if not first.startswith("RTSP/1.0 200"):
        raise HarnessError(f"RTSP probe to {host}:{port}{path} returned {first!r}")
    return first


def wait_ready(
    appliance: Vm,
    camera: Vm,
    config: dict[str, Any],
    accelerator: str,
    budget_seconds: float,
    *,
    cold_boot: bool,
) -> None:
    deadline = time.monotonic() + budget_seconds
    wait_for_tcp(appliance.ssh_port, budget_seconds)
    wait_for_tcp(camera.ssh_port, budget_seconds)
    last_error = "guest not ready"
    while time.monotonic() < deadline:
        try:
            appliance.ssh("test -e /run/bittern-harness-ready", timeout=10)
            camera.ssh("test -e /run/bittern-harness-ready", timeout=10)
            camera.ssh("systemctl is-active --quiet bittern-camera.service", timeout=10)
            appliance.ssh(
                f"ip -4 addr show dev cam0 | grep -q '{config['network']['cam']['appliance_address']}/24'",
                timeout=10,
            )
            if cold_boot:
                appliance.ssh("cloud-init status --wait >/dev/null", timeout=60)
                appliance.ssh("sudo -n docker info >/dev/null", timeout=30)
            rtsp_probe(
                "127.0.0.1",
                int(config["camera"]["host_rtsp_port"]),
                config["camera"]["rtsp_path"],
                timeout=5,
            )
            appliance.ssh(
                "bittern-rtsp-probe "
                f"{config['network']['cam']['camera_address']} 8554 "
                f"{config['camera']['rtsp_path']}",
                timeout=10,
            )
            return
        except (HarnessError, subprocess.TimeoutExpired) as exc:
            last_error = str(exc)
            time.sleep(1)
    raise HarnessError(
        f"guests did not become ready within {budget_seconds:.0f}s: {last_error}"
    )


def start_all(
    appliance: Vm,
    camera: Vm,
    config: dict[str, Any],
    accelerator: str,
    load_snapshot: bool,
) -> None:
    for vm in (appliance, camera):
        if not vm.running():
            vm.check_host_ports()
    appliance.start(accelerator, load_snapshot)
    camera.start(accelerator, load_snapshot)
    state = load_state()
    state.update(
        {
            "accelerator": accelerator,
            "started_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
            "vms": {
                "appliance": {"pid": appliance.pid()},
                "camera": {"pid": camera.pid()},
            },
        }
    )
    save_state(state)


def stop_all(appliance: Vm, camera: Vm, config: dict[str, Any]) -> list[QemuProcess]:
    stopped = camera.stop() + appliance.stop()
    state = load_state()
    state.pop("vms", None)
    state.pop("started_at", None)
    save_state(state)
    return stopped


def finish_guest_lifecycle(
    args: argparse.Namespace,
    appliance: Vm,
    camera: Vm,
    config: dict[str, Any],
    *,
    failed: bool,
) -> None:
    if getattr(args, "keep", False):
        log("guests kept running for inspection (--keep)")
        log(f"stop them with: {RUN_COMMAND} stop")
        log("removing this worktree deletes its pidfiles; run stop before removing it")
        return
    if failed:
        log("stopping both guests after failure (use --keep to leave them for inspection)")
    else:
        log("stopping both guests (use --keep to leave them for inspection)")
    try:
        stopped = stop_all(appliance, camera, config)
        if not stopped:
            log("no running guests to stop")
    except Exception as exc:
        if failed:
            log(f"warning: could not stop the guests: {exc}")
        else:
            raise


@contextlib.contextmanager
def guest_lifecycle(
    args: argparse.Namespace,
    appliance: Vm,
    camera: Vm,
    config: dict[str, Any],
):
    try:
        yield
    except BaseException:
        finish_guest_lifecycle(args, appliance, camera, config, failed=True)
        raise
    else:
        finish_guest_lifecycle(args, appliance, camera, config, failed=False)


def load_baseline(appliance: Vm, camera: Vm) -> None:
    log("restoring baseline snapshot on both guests")
    camera.load_snapshot()
    appliance.load_snapshot()


def save_baseline(appliance: Vm, camera: Vm) -> float:
    log("saving baseline snapshots")
    started = time.monotonic()
    appliance.save_snapshot()
    camera.save_snapshot()
    seconds = time.monotonic() - started
    log(f"baseline snapshots saved in {seconds:.1f}s")
    return seconds


def test_addresses(appliance: Vm, camera: Vm, config: dict[str, Any]) -> str:
    output = appliance.ssh(
        "ip -4 -o addr show dev lan0; "
        "ip -4 -o addr show dev cam0; "
        "ip route show default; "
        "cat /proc/sys/net/ipv6/conf/cam0/disable_ipv6"
    ).stdout
    if f"inet {config['network']['cam']['appliance_address']}/24" not in output:
        raise HarnessError(f"cam0 address missing:\n{output}")
    if "default via" not in output or "dev lan0" not in output:
        raise HarnessError(f"lan0 default route missing:\n{output}")
    if output.strip().splitlines()[-1].strip() != "1":
        raise HarnessError(f"IPv6 is not disabled on cam0:\n{output}")
    return f"cam0={config['network']['cam']['appliance_address']}/24, lan0 default via DHCP, IPv6 off"


def test_camera(appliance: Vm, camera: Vm, config: dict[str, Any]) -> str:
    camera_address = config["network"]["cam"]["camera_address"]
    path = config["camera"]["rtsp_path"]
    guest = appliance.ssh(
        f"bittern-rtsp-probe {camera_address} 8554 {path}"
    ).stdout.strip()
    host = rtsp_probe("127.0.0.1", int(config["camera"]["host_rtsp_port"]), path)
    return f"guest={guest}; host={host}"


def test_cam0_isolated(appliance: Vm, camera: Vm, config: dict[str, Any]) -> str:
    target = config["network"]["cam"]["appliance_address"]
    try:
        with socket.create_connection((target, 22), timeout=2):
            raise HarnessError(
                f"host unexpectedly reached the appliance on cam0 at {target}:22"
            )
    except OSError:
        pass
    return f"host has no route to {target} on cam0"


def test_snapshot_reset(appliance: Vm, camera: Vm, config: dict[str, Any]) -> str:
    marker = "/tmp/bittern-snapshot-marker"
    appliance.ssh(f"touch {marker}")
    load_baseline(appliance, camera)
    accelerator = load_state().get("accelerator", "tcg,thread=multi")
    wait_ready(
        appliance,
        camera,
        config,
        accelerator,
        budgets(config, accelerator)["snapshot_restore"],
        cold_boot=False,
    )
    appliance.ssh(f"test ! -e {marker}")
    return "loadvm removed a post-snapshot file and restored the network state"


TESTS = (
    ("addresses", test_addresses),
    ("camera-reachable", test_camera),
    ("cam0-isolated", test_cam0_isolated),
    ("snapshot-reset", test_snapshot_reset),
)


def run_tests(args: argparse.Namespace) -> int:
    config = load_config()
    require_tools()
    ensure_directories()
    appliance = Vm("appliance", config)
    camera = Vm("camera", config)
    with guest_lifecycle(args, appliance, camera, config):
        return run_test_suite(config, appliance, camera)


def run_test_suite(
    config: dict[str, Any],
    appliance: Vm,
    camera: Vm,
) -> int:
    if not all(
        path.exists()
        for path in (appliance.overlay, camera.overlay, appliance.seed, camera.seed)
    ):
        prepare(argparse.Namespace(force=False, refresh_image=False))
    accelerator, reason = choose_accelerator(config)
    limits = budgets(config, accelerator)
    log(f"accelerator: {accelerator} ({reason})")
    log(
        "budgets: "
        f"boot<={limits['boot']}s, restore<={limits['snapshot_restore']}s, "
        f"suite<={limits['test_suite']}s"
    )

    state = load_state()
    boot_seconds: float | None = state.get("cold_boot_seconds")
    snapshot_seconds: float | None = state.get("snapshot_seconds")
    if appliance.running() or camera.running():
        log("using already running guests")
        if not (appliance.has_snapshot() and camera.has_snapshot()):
            wait_ready(appliance, camera, config, accelerator, limits["boot"], cold_boot=True)
            snapshot_seconds = save_baseline(appliance, camera)
            state["snapshot_seconds"] = round(snapshot_seconds, 2)
            save_state(state)
    else:
        cold = not (appliance.has_snapshot() and camera.has_snapshot())
        started = time.monotonic()
        start_all(appliance, camera, config, accelerator, load_snapshot=not cold)
        if cold:
            log("cold boot; first boot installs Docker and the camera fixture")
            wait_ready(appliance, camera, config, accelerator, limits["boot"], cold_boot=True)
            boot_seconds = time.monotonic() - started
            if boot_seconds > limits["boot"]:
                raise HarnessError(
                    f"cold boot took {boot_seconds:.1f}s, budget is {limits['boot']}s"
                )
            log(f"cold boot: {boot_seconds:.1f}s (budget {limits['boot']}s)")
            snapshot_seconds = save_baseline(appliance, camera)
            state["cold_boot_seconds"] = round(boot_seconds, 2)
            state["snapshot_seconds"] = round(snapshot_seconds, 2)
            save_state(state)
        else:
            log("loaded baseline snapshots; skipping cold boot")
            wait_ready(
                appliance,
                camera,
                config,
                accelerator,
                limits["snapshot_restore"],
                cold_boot=False,
            )
            log(
                f"snapshot restore: {time.monotonic() - started:.1f}s "
                f"(budget {limits['snapshot_restore']}s)"
            )

    suite_started = time.monotonic()
    results: list[dict[str, Any]] = []
    for name, function in TESTS:
        step_started = time.monotonic()
        log(f"test {name}")
        load_baseline(appliance, camera)
        wait_ready(
            appliance,
            camera,
            config,
            accelerator,
            limits["snapshot_restore"],
            cold_boot=False,
        )
        detail = function(appliance, camera, config)
        elapsed = time.monotonic() - step_started
        results.append({"name": name, "result": "pass", "seconds": round(elapsed, 2), "detail": detail})
        log(f"  pass: {detail} ({elapsed:.1f}s)")
    suite_seconds = time.monotonic() - suite_started
    if suite_seconds > limits["test_suite"]:
        raise HarnessError(
            f"test suite took {suite_seconds:.1f}s, budget is {limits['test_suite']}s"
        )
    result_path = LOGS / f"test-results-{time.strftime('%Y%m%d-%H%M%S')}.json"
    result_path.write_text(
        json.dumps(
            {
                "accelerator": accelerator,
                "budgets": limits,
                "boot_seconds": None if boot_seconds is None else round(boot_seconds, 2),
                "snapshot_seconds": (
                    None if snapshot_seconds is None else round(snapshot_seconds, 2)
                ),
                "suite_seconds": round(suite_seconds, 2),
                "tests": results,
            },
            indent=2,
            sort_keys=True,
        )
        + "\n",
        encoding="utf-8",
    )
    log(f"suite passed in {suite_seconds:.1f}s (budget {limits['test_suite']}s)")
    log(f"results: {result_path}")
    return 0



FRIGATE_MANIFEST = json.loads(
    (ROOT / "tests" / "fixtures" / "frigate" / "manifest.json").read_text(encoding="utf-8")
)
FRIGATE_IMAGE_REFERENCE = FRIGATE_MANIFEST["image"]["reference"]
FRIGATE_IMAGE_DIGEST = FRIGATE_MANIFEST["image"]["digest"]
FIRSTBOOT_MARKER = "/var/lib/bittern/first-boot.json"
FIRSTBOOT_TOKEN_PATH = "/var/lib/bittern/setup-token"
FIRSTBOOT_SECRETS = "/etc/bittern/secrets.yaml"
FIRSTBOOT_DOCKER_DAEMON = "/etc/docker/daemon.json"
FIRSTBOOT_DOCKER_POLICY = '{"iptables": false, "ip-forward": false, "bridge": "none"}'
FIRSTBOOT_RESOLV_CONF = "/etc/resolv.conf"
FIRSTBOOT_RESOLVED_UNIT = "systemd-resolved.service"
FIRSTBOOT_RESOLVED_STUB = "/run/systemd/resolve/stub-resolv.conf"
# The clean-install runbook's post-reboot unit checks. The harness must never
# enable these itself: install.sh enables every unit it installs, and apply
# enables the rendered Frigate unit. The baseline is disabled before install so
# the scenario cannot inherit an enabled unit from the QEMU image.
BASELINE_UNITS = (
    "docker.service",
    "nftables.service",
    "systemd-networkd.service",
    "systemd-resolved.service",
    "dnsmasq.service",
    "chrony.service",
)
INSTALL_ENABLED_UNITS = (
    "docker.service",
    "nftables.service",
    "systemd-networkd.service",
    "systemd-resolved.service",
    "dnsmasq.service",
    "chrony.service",
    "bittern-first-boot.service",
    "bittern-image-prewarm.service",
    "bittern-update.service",
    "bitternd.service",
)
BITTERN_UNITS = (
    "bittern-first-boot.service",
    "bittern-image-prewarm.service",
    "bittern-update.service",
    "bitternd.service",
    "bittern-frigate.service",
)
REBOOT_ENABLED_UNITS = INSTALL_ENABLED_UNITS
REBOOT_ACTIVE_UNITS = (
    "docker.service",
    "nftables.service",
    "bitternd.service",
    "dnsmasq.service",
    "chrony.service",
    "systemd-networkd.service",
    "systemd-resolved.service",
)
POST_APPLY_ENABLED_UNITS = INSTALL_ENABLED_UNITS + ("bittern-frigate.service",)
POST_APPLY_ACTIVE_UNITS = REBOOT_ACTIVE_UNITS + ("bittern-frigate.service",)
FIRSTBOOT_HARNESS_SCRIPT = "/usr/local/sbin/bittern-harness-firewall"
FIRSTBOOT_HARNESS_DROPIN = "/etc/systemd/system/nftables.service.d/50-bittern-harness.conf"
CLEAN_INSTALL_UNIT = "bittern-install.service"
CLEAN_INSTALL_UNIT_NAME = "bittern-install"
CLEAN_INSTALL_SCRIPT = "/tmp/bittern-clean-install.sh"
CLEAN_NETWORK_DIR = "/etc/systemd/network"
CLEAN_RUN_NETWORK_DIR = "/run/systemd/network"
CLEAN_NETWORK_FILES = (
    "10-lan0.link",
    "10-lan0.network",
    "10-cam0.link",
    "10-cam0.network",
)
HARNESS_DNSMASQ_DROPIN = "/etc/dnsmasq.d/00-bittern-harness-port0.conf"
HARNESS_DNSMASQ_CONFIG = """# QEMU harness only: the genericcloud image runs systemd-resolved on port 53.
# The rendered config sets port=0, but this lets dnsmasq start before apply.
port=0
"""
HARNESS_FIREWALL_SCRIPT = """#!/bin/sh
# QEMU harness management only. The rendered product table has no SSH accept
# rule; this script adds one after nftables loads so the harness can keep
# driving the guest across a reboot. The Bittern UI on 443 is a rendered rule.
# This script never runs outside this harness.
set -eu
if ! nft list table inet bittern >/dev/null 2>&1; then
    exit 0
fi
nft list chain inet bittern input 2>/dev/null | grep -q 'bittern-harness-ssh' ||
    nft insert rule inet bittern input iifname "lan0" tcp dport 22 accept comment "bittern-harness-ssh"
"""
HARNESS_FIREWALL_DROPIN = """[Service]
# QEMU harness management only; see /usr/local/sbin/bittern-harness-firewall.
ExecStartPost=/usr/local/sbin/bittern-harness-firewall
"""
SYNTHETIC_DHCP_CLIENT = r"""#!/usr/bin/env python3
# Minimal DHCP client for the QEMU camera segment. It sends DISCOVER and
# REQUEST from the camera's cam0 MAC and prints the ACK as one JSON line.
import fcntl
import json
import random
import socket
import struct
import sys
import time

IFACE = "cam0"
COOKIE = b"\x63\x82\x53\x63"
CLIENT_PORT = 68
SERVER_PORT = 67


def checksum(data):
    if len(data) % 2:
        data += b"\x00"
    total = sum(struct.unpack("!%dH" % (len(data) // 2), data))
    total = (total >> 16) + (total & 0xFFFF)
    total += total >> 16
    return (~total) & 0xFFFF


def interface_mac(name):
    with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as sock:
        info = fcntl.ioctl(sock.fileno(), 0x8927, struct.pack("256s", name.encode()[:15]))
    return info[18:24]


def dhcp_packet(xid, mac, message_type, requested=None, server=None):
    options = bytearray([53, 1, message_type])
    if requested:
        options += bytes([50, 4]) + socket.inet_aton(requested)
    if server:
        options += bytes([54, 4]) + socket.inet_aton(server)
    options += bytes([61, 7, 1]) + mac
    options += bytes([12, 13]) + b"bittern-qemu\x00"[:13]
    options += bytes([55, 6, 1, 3, 6, 15, 28, 42])
    options += b"\xff"
    dhcp = struct.pack(
        "!BBBBIHHIIII16s64s128s",
        1, 1, 6, 0, xid, 0, 0x8000, 0, 0, 0, 0,
        mac.ljust(16, b"\x00"), b"\x00" * 64, b"\x00" * 128,
    ) + COOKIE + bytes(options)
    udp = struct.pack("!HHHH", CLIENT_PORT, SERVER_PORT, 8 + len(dhcp), 0) + dhcp
    ip = struct.pack(
        "!BBHHHBBH4s4s",
        0x45, 0, 20 + len(udp), 0, 0, 64, 17, 0,
        socket.inet_aton("0.0.0.0"), socket.inet_aton("255.255.255.255"),
    )
    ip = ip[:10] + struct.pack("!H", checksum(ip)) + ip[12:]
    return b"\xff" * 6 + mac + b"\x08\x00" + ip + udp


def parse_options(dhcp):
    if dhcp[236:240] != COOKIE:
        return {}
    options = {}
    index = 240
    while index < len(dhcp):
        code = dhcp[index]
        index += 1
        if code == 0:
            continue
        if code == 255 or index >= len(dhcp):
            break
        length = dhcp[index]
        index += 1
        options[code] = dhcp[index:index + length]
        index += length
    return options


def parse_reply(frame):
    if len(frame) < 42 or frame[12:14] != b"\x08\x00":
        return None
    ip = frame[14:]
    if len(ip) < 20 or ip[9] != 17:
        return None
    header_length = (ip[0] & 0x0F) * 4
    udp = ip[header_length:]
    if len(udp) < 8:
        return None
    source, destination, length, _ = struct.unpack("!HHHH", udp[:8])
    if source != SERVER_PORT or destination != CLIENT_PORT:
        return None
    dhcp = udp[8:length]
    if len(dhcp) < 240:
        return None
    xid = struct.unpack("!I", dhcp[4:8])[0]
    return xid, socket.inet_ntoa(dhcp[16:20]), parse_options(dhcp)


def wait_for(message_type, xid, sock):
    deadline = time.monotonic() + 10
    while time.monotonic() < deadline:
        try:
            frame = sock.recv(4096)
        except socket.timeout:
            continue
        parsed = parse_reply(frame)
        if parsed is None:
            continue
        reply_xid, address, options = parsed
        if reply_xid != xid or options.get(53) != bytes([message_type]):
            continue
        server = socket.inet_ntoa(options[54]) if 54 in options else ""
        lease = struct.unpack("!I", options[51])[0] if 51 in options and len(options[51]) == 4 else 0
        return address, server, lease
    return None


def main():
    mac = interface_mac(IFACE)
    sock = socket.socket(socket.AF_PACKET, socket.SOCK_RAW, socket.htons(0x0800))
    sock.bind((IFACE, 0))
    sock.settimeout(1)
    xid = random.randrange(1, 0xFFFFFFFF)
    sock.send(dhcp_packet(xid, mac, 1))
    offer = wait_for(2, xid, sock)
    if offer is None:
        print(json.dumps({"error": "no DHCP offer"}))
        return 1
    address, server, _ = offer
    sock.send(dhcp_packet(xid, mac, 3, requested=address, server=server))
    ack = wait_for(5, xid, sock)
    if ack is None:
        print(json.dumps({"error": "no DHCP acknowledgement", "offer": address}))
        return 1
    address, server, lease = ack
    print(json.dumps({
        "address": address,
        "server": server,
        "lease_seconds": lease,
        "mac": ":".join("%02x" % byte for byte in mac),
    }))
    return 0


if __name__ == "__main__":
    sys.exit(main())
"""


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):  # noqa: ANN001
        return None


class WizardClient:
    """Small HTTPS form driver with a cookie jar and per-page CSRF tokens."""

    def __init__(self, port: int) -> None:
        self.base = f"https://127.0.0.1:{port}"
        self.jar = http.cookiejar.CookieJar()
        context = ssl._create_unverified_context()  # noqa: SLF001 - self-signed appliance cert
        self.opener = urllib.request.build_opener(
            urllib.request.HTTPSHandler(context=context),
            urllib.request.HTTPCookieProcessor(self.jar),
            NoRedirect(),
        )
        self.csrf = ""

    def _request(
        self,
        method: str,
        path: str,
        fields: dict[str, str] | None = None,
        *,
        timeout: float = 30,
    ) -> tuple[int, str, str]:
        data = None
        headers = {"User-Agent": "bittern-qemu-firstboot/1"}
        if fields is not None:
            data = urllib.parse.urlencode(fields).encode("utf-8")
            headers["Content-Type"] = "application/x-www-form-urlencoded"
        request = urllib.request.Request(self.base + path, data=data, headers=headers, method=method)
        try:
            with self.opener.open(request, timeout=timeout) as response:
                body = response.read().decode("utf-8", "replace")
                return response.status, response.headers.get("Location", ""), body
        except urllib.error.HTTPError as exc:
            body = exc.read().decode("utf-8", "replace")
            return exc.code, exc.headers.get("Location", ""), body
        except urllib.error.URLError as exc:
            raise HarnessError(f"wizard request {method} {path} failed: {exc.reason}") from exc

    def get_page(self, path: str, *, expect: int = 200, timeout: float = 30) -> str:
        status, _, body = self._request("GET", path, timeout=timeout)
        if status != expect:
            raise HarnessError(f"wizard GET {path} returned HTTP {status}, expected {expect}")
        match = re.search(r'name="csrf_token" value="([^"]+)"', body)
        if match:
            self.csrf = match.group(1)
        return body

    def post(
        self,
        path: str,
        fields: dict[str, str] | None = None,
        *,
        expect: int = 303,
        timeout: float = 60,
    ) -> tuple[str, str]:
        values = dict(fields or {})
        if self.csrf:
            values["csrf_token"] = self.csrf
        status, location, body = self._request("POST", path, values, timeout=timeout)
        if status != expect:
            plain = html.unescape(re.sub(r"<[^>]+>", " ", body))
            snippet = re.sub(r"\s+", " ", plain).strip()[:1000]
            raise HarnessError(
                f"wizard POST {path} returned HTTP {status}, expected {expect}: {snippet}"
            )
        return location, body


def build_firstboot_binaries() -> tuple[Path, Path, Path]:
    if shutil.which("go") is None:
        raise HarnessError("go is not installed; the firstboot command builds the host binaries")
    sync = run_command(["sh", str(ROOT / "agent" / "scripts" / "sync-assets.sh")], timeout=120)
    if sync.returncode != 0:
        raise HarnessError(f"sync-assets failed: {sync.stdout}{sync.stderr}".strip())
    environment = os.environ.copy()
    environment.update({"CGO_ENABLED": "0", "GOPROXY": "off", "GOFLAGS": "-mod=vendor"})
    outputs = []
    for command in ("bittern", "bitternd", "bittern-launch"):
        destination = BIN / command
        result = run_command(
            [
                "go",
                "build",
                "-trimpath",
                "-ldflags=-s -w",
                "-o",
                str(destination),
                f"./cmd/{command}",
            ],
            cwd=ROOT / "agent",
            env=environment,
            timeout=300,
        )
        if result.returncode != 0:
            raise HarnessError(f"go build {command} failed: {result.stdout}{result.stderr}".strip())
        outputs.append(destination)
    return outputs[0], outputs[1], outputs[2]


def build_installer_payload(bittern: Path, bitternd: Path, launcher: Path) -> Path:
    parent = BUILD / "installer-payload"
    stage = parent / "bittern-poc-test"
    archive = parent / "bittern-poc-test.tar.gz"
    if parent.exists():
        shutil.rmtree(parent)
    stage.mkdir(parents=True)
    sources = {
        "bittern": bittern,
        "bitternd": bitternd,
        "bittern-launch": launcher,
        "platform.json": ROOT / "packaging" / "platform.json",
        "bittern-first-boot.service": ROOT / "agent" / "packaging" / "bittern-first-boot.service",
        "bittern-image-prewarm.service": ROOT
        / "agent"
        / "packaging"
        / "bittern-image-prewarm.service",
        "bittern-update.service": ROOT / "agent" / "packaging" / "bittern-update.service",
        "bitternd.service": ROOT / "agent" / "packaging" / "bitternd.service",
        "install.sh": ROOT / "packaging" / "install.sh",
        "cleanup.sh": ROOT / "packaging" / "cleanup.sh",
        "storage-setup.sh": ROOT / "packaging" / "storage-setup.sh",
        "README.md": ROOT / "packaging" / "README-beta-kit.md",
    }
    checksums = []
    for name, source in sources.items():
        destination = stage / name
        shutil.copy2(source, destination)
        destination.chmod(
            0o755
            if name in ("bittern", "bitternd", "bittern-launch", "install.sh", "cleanup.sh", "storage-setup.sh")
            else 0o644
        )
        checksums.append(f"{hashlib.sha256(destination.read_bytes()).hexdigest()}  {name}\n")
    (stage / "SHA256SUMS").write_text("".join(checksums), encoding="utf-8")
    with tarfile.open(archive, "w:gz") as handle:
        handle.add(stage, arcname=stage.name)
    shutil.rmtree(stage)
    return archive


class FirstbootScenario:
    def __init__(
        self,
        appliance: Vm,
        camera: Vm,
        config: dict[str, Any],
        accelerator: str,
        *,
        scenario_name: str = "firstboot",
    ) -> None:
        self.appliance = appliance
        self.camera = camera
        self.config = config
        self.accelerator = accelerator
        self.limits = budgets(config, accelerator)
        stamp = time.strftime("%Y%m%d-%H%M%S")
        self.log_dir = LOGS / f"{scenario_name}-{stamp}"
        self.log_dir.mkdir(parents=True, exist_ok=True)
        self.phases: list[dict[str, Any]] = []
        self.captured: list[str] = []
        self.token = ""
        self.short_code = ""
        self.setup_url = ""
        self.admin_password = ""
        self.camera_address = config["network"]["cam"]["camera_address"]
        self.camera_port = 8554
        self.camera_path = config["camera"]["rtsp_path"]
        self.camera_url = f"rtsp://{self.camera_address}:{self.camera_port}{self.camera_path}"
        self.marker: dict[str, Any] = {}
        self.rendered_tree: list[str] = []
        self.snapshot: dict[str, Any] = {}
        self.status: dict[str, Any] = {}
        self.post_apply_reboot: dict[str, str] = {}
        self.asserted_files: list[str] = []
        self.installer_path = ""
        self.installer_archive = ""
        self.resolver_dns = ""
        self.house_iface = "lan0"
        self.camera_iface = "cam0"
        self.failed_phase = ""

    def redact(self, text: str) -> str:
        values = [
            value
            for value in (
                self.token,
                self.short_code,
                self.admin_password,
                self.camera_url,
                self.setup_url,
            )
            if value
        ]
        for value in values:
            text = text.replace(value, "<redacted>")
        return text

    def note(self, message: str) -> None:
        line = f"qemu-harness: {self.redact(message)}"
        print(line, flush=True)
        self.captured.append(line + "\n")

    def capture(self, name: str, text: str) -> None:
        self.captured.append(text)

    def run_phase(self, name: str, function: Any) -> Any:
        started = time.monotonic()
        self.note(f"firstboot phase: {name}")
        try:
            detail = function()
        except Exception as exc:
            elapsed = time.monotonic() - started
            self.failed_phase = name
            self.phases.append(
                {"name": name, "result": "fail", "seconds": round(elapsed, 2), "detail": self.redact(str(exc))}
            )
            self.write_summary("fail", self.redact(str(exc)))
            raise
        elapsed = time.monotonic() - started
        self.phases.append(
            {"name": name, "result": "pass", "seconds": round(elapsed, 2), "detail": str(detail)}
        )
        self.note(f"  pass: {detail} ({elapsed:.1f}s)")
        return detail

    def run(self) -> int:
        self.run_phase("guests", self.ensure_guests)
        self.run_phase("install", self.install_components)
        self.run_phase("first-boot", self.run_first_boot)
        self.run_phase("installer-rerun", self.reinstall_after_firewall)
        self.run_phase("reboot", self.reboot_once)
        self.run_phase("reboot-units", self.assert_units_after_reboot)
        self.run_phase("image-prewarm", self.assert_image_prewarm)
        self.run_phase("resolver", self.assert_resolver_after_reboot)
        self.run_phase("setup-token", self.read_setup_token)
        self.run_phase("setup-token-qr", self.read_setup_token_qr)
        self.run_phase("daemon", self.start_daemon)
        self.run_phase("cam0-down", self.assert_cam0_down_status)
        self.run_phase("dhcp-lease", self.assert_dhcp_lease)
        self.run_phase("wizard", self.drive_wizard)
        self.run_phase("post-apply-reboot", self.reboot_after_apply)
        self.run_phase("post-reboot", self.assert_appliance_units_after_reboot)
        self.run_phase("docker-frigate", self.restart_docker_with_frigate)
        self.run_phase("assertions", self.assert_applied)
        self.run_phase("cleanup", self.assert_cleanup)
        self.run_phase("logs", self.assert_logs_clean)
        self.write_summary("pass", "")
        self.note(f"firstboot scenario passed; logs: {self.log_dir}")
        return 0

    def service_invocations(self, units: tuple[str, ...]) -> dict[str, str]:
        command = (
            "for unit in "
            + " ".join(units)
            + '; do printf "%s=" "$unit"; systemctl show -p InvocationID --value "$unit"; done'
        )
        output = self.appliance.ssh(command, timeout=30).stdout
        values: dict[str, str] = {}
        for line in output.splitlines():
            name, separator, value = line.partition("=")
            if separator:
                values[name.strip()] = value.strip()
        return values

    def ensure_guests(self) -> str:
        if not all(
            path.exists()
            for path in (
                self.appliance.overlay,
                self.camera.overlay,
                self.appliance.seed,
                self.camera.seed,
            )
        ):
            prepare(argparse.Namespace(force=False, refresh_image=False))
        if self.appliance.has_snapshot() and self.camera.has_snapshot():
            if self.appliance.running() or self.camera.running():
                load_baseline(self.appliance, self.camera)
            else:
                start_all(self.appliance, self.camera, self.config, self.accelerator, True)
            wait_ready(
                self.appliance,
                self.camera,
                self.config,
                self.accelerator,
                self.limits["snapshot_restore"],
                cold_boot=False,
            )
            return "restored the baseline snapshot on both guests"
        if self.appliance.running() or self.camera.running():
            wait_ready(
                self.appliance,
                self.camera,
                self.config,
                self.accelerator,
                self.limits["boot"],
                cold_boot=True,
            )
        else:
            start_all(self.appliance, self.camera, self.config, self.accelerator, False)
            wait_ready(
                self.appliance,
                self.camera,
                self.config,
                self.accelerator,
                self.limits["boot"],
                cold_boot=True,
            )
        seconds = save_baseline(self.appliance, self.camera)
        return f"cold-booted the guests and saved the baseline snapshot in {seconds:.1f}s"

    def stage_installer(self) -> None:
        bittern, bitternd, launcher = build_firstboot_binaries()
        payload = build_installer_payload(bittern, bitternd, launcher)
        remote = f"/tmp/{payload.name}"
        self.appliance.scp_to(payload, remote, timeout=180)
        self.appliance.ssh(
            "set -eu; "
            "rm -rf /tmp/bittern-poc; mkdir -p /tmp/bittern-poc; "
            f"tar -xzf {shlex.quote(remote)} -C /tmp/bittern-poc",
            timeout=180,
        )
        self.installer_path = self.appliance.ssh(
            "cd /tmp/bittern-poc/*; pwd", timeout=30
        ).stdout.strip().splitlines()[-1]
        self.installer_archive = remote

    def strip_baseline_packages(self) -> str:
        # The cloud-init baseline installs recommendations, including
        # docker-cli. Remove it so this phase proves the installer's explicit
        # package list rather than inheriting the CLI from the baseline.
        self.appliance.ssh(
            "sudo -n apt-get remove --yes docker-cli",
            timeout=180,
            check=False,
        )
        self.appliance.ssh("test ! -e /usr/bin/docker", timeout=30)
        # Reproduce a Debian 13 install without systemd-resolved. Debian's
        # networkd no longer writes /etc/resolv.conf when the resolver is
        # absent, which is the GK41 failure.
        if not self.resolver_dns:
            resolver = self.appliance.ssh(
                f"resolvectl dns {self.house_iface}", timeout=30, check=False
            )
            address = re.search(r"\b(?:\d{1,3}\.){3}\d{1,3}\b", resolver.stdout)
            if not address:
                raise HarnessError(
                    "cannot read the DHCP-provided DNS before removing the resolver:\n"
                    f"$ resolvectl dns {self.house_iface}\n{resolver.stdout}{resolver.stderr}"
                )
            self.resolver_dns = address.group(0)
        self.appliance.ssh(
            "sudo -n apt-get remove --yes systemd-resolved",
            timeout=300,
            check=False,
        )
        self.appliance.ssh("test ! -e /usr/bin/resolvectl", timeout=30)
        return "removed docker-cli and systemd-resolved from the baseline"

    def seed_static_resolver(self) -> None:
        # Keep the DHCP-provided DNS in a harness-only static file for the
        # installer's apt run; the installer must replace it with the
        # package-managed resolver path.
        self.appliance.ssh(
            "sudo -n sh -c 'rm -f /etc/resolv.conf; "
            f"printf \"nameserver {self.resolver_dns}\\n\" > /etc/resolv.conf; "
            "chmod 0644 /etc/resolv.conf'",
            timeout=30,
        )

    def run_installer_commands(self) -> str:
        result = self.appliance.ssh(
            f"cd {shlex.quote(self.installer_path)}; "
            "sudo -n ./install.sh --dry-run; "
            "sudo -n ./install.sh",
            timeout=900,
        )
        output = result.stdout + result.stderr
        self.capture("installer-output.txt", output)
        return output

    def assert_installer_result(self, output: str) -> tuple[str, str]:
        if "installing Debian packages: docker.io docker-cli docker-compose" not in output:
            raise HarnessError("installer package list does not name docker-cli")
        if "systemd-resolved" not in output:
            raise HarnessError("installer package list does not name systemd-resolved")
        if "qrencode" not in output:
            raise HarnessError("installer package list does not name qrencode")
        self.appliance.ssh("test -x /usr/bin/resolvectl", timeout=30)
        self.appliance.ssh(
            f'test "$(readlink {FIRSTBOOT_RESOLV_CONF})" = ../run/systemd/resolve/stub-resolv.conf',
            timeout=30,
        )
        enabled = self.assert_units_enabled(
            INSTALL_ENABLED_UNITS, "install.sh did not enable every appliance unit"
        )
        self.capture("installer-units-enabled.txt", enabled)
        daemon_config = self.appliance.ssh(
            f"sudo -n cat {FIRSTBOOT_DOCKER_DAEMON}", timeout=30
        ).stdout.strip()
        if daemon_config != FIRSTBOOT_DOCKER_POLICY:
            raise HarnessError(
                f"installer wrote unexpected Docker daemon configuration: {daemon_config!r}"
            )
        self.capture("docker-daemon.json", daemon_config + "\n")
        self.appliance.ssh("sudo -n systemctl is-active --quiet docker", timeout=30)
        self.appliance.ssh("test ! -e /sys/class/net/docker0", timeout=30)
        self.appliance.ssh("test -x /usr/bin/docker", timeout=30)
        self.appliance.ssh("test -x /usr/bin/docker-compose", timeout=30)
        docker_version = self.appliance.ssh("/usr/bin/docker --version", timeout=30).stdout.strip()
        compose_version = self.appliance.ssh(
            "/usr/bin/docker-compose version", timeout=30
        ).stdout.splitlines()[0]
        self.appliance.write_remote_file(
            FIRSTBOOT_HARNESS_SCRIPT, HARNESS_FIREWALL_SCRIPT, mode="0755"
        )
        self.appliance.ssh(
            "sudo -n install -d -m 0755 /etc/systemd/system/nftables.service.d", timeout=30
        )
        self.appliance.write_remote_file(
            FIRSTBOOT_HARNESS_DROPIN, HARNESS_FIREWALL_DROPIN, mode="0644"
        )
        self.appliance.ssh("sudo -n systemctl daemon-reload", timeout=60)
        self.appliance.ssh(
            "sudo -n sh -c 'PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin; "
            "command -v docker; command -v docker-compose; command -v curl; command -v nft; "
            "command -v dnsmasq; command -v chronyd; command -v qrencode; "
            "test -d /var/lib/bittern/media'",
            timeout=60,
        )
        self.appliance.ssh("sudo -n docker info >/dev/null", timeout=60)
        return docker_version, compose_version

    def disable_baseline_enablement(self) -> dict[str, str]:
        # Reproduce a fresh Debian install where the payload cannot inherit an
        # enabled unit. `disable` removes the static wants symlink without
        # stopping the running service, so SSH, networkd and the pre-apply
        # services stay up. A runtime-enabled networkd is accepted because only
        # the static enablement can survive a boot.
        for unit in BASELINE_UNITS:
            self.appliance.ssh(
                f"sudo -n systemctl disable {unit} 2>/dev/null || true", timeout=60
            )
        states: dict[str, str] = {}
        for unit in BASELINE_UNITS:
            result = self.appliance.ssh(
                f"systemctl is-enabled {unit}", timeout=30, check=False
            )
            state = (result.stdout or result.stderr).strip()
            if "No such file or directory" in state:
                state = "not-found"
            if state not in ("disabled", "enabled-runtime", "not-found"):
                raise HarnessError(
                    f"{unit} is still statically enabled before install: {state!r}"
                )
            states[unit] = state
        for unit in BITTERN_UNITS:
            present = self.appliance.ssh(
                f"sudo -n test -e /etc/systemd/system/{unit}",
                timeout=30,
                check=False,
            )
            if present.returncode == 0:
                raise HarnessError(f"{unit} exists before the payload install")
        return states

    def assert_units_enabled(self, units: tuple[str, ...], context: str) -> str:
        result = self.appliance.ssh(
            "systemctl is-enabled " + " ".join(units), timeout=30, check=False
        )
        states = result.stdout.split()
        if result.returncode != 0 or states != ["enabled"] * len(units):
            raise HarnessError(
                f"{context}: every appliance unit must be enabled:\n"
                f"$ systemctl is-enabled {' '.join(units)}\n"
                f"{result.stdout}{result.stderr}"
            )
        return result.stdout

    def install_components(self) -> str:
        self.stage_installer()
        self.strip_baseline_packages()
        self.seed_static_resolver()
        baseline_states = self.disable_baseline_enablement()
        output = self.run_installer_commands()
        docker_version, compose_version = self.assert_installer_result(output)
        self.appliance.ssh(f"rm -f {shlex.quote(self.installer_archive)}", timeout=30)
        return (
            "clean guest without docker-cli or systemd-resolved; every baseline unit "
            f"was disabled before install ({json.dumps(baseline_states, sort_keys=True)}) "
            "and all installed units are enabled after it; installer "
            f"restored {docker_version}; {compose_version}; resolver uses the DHCP DNS "
            f"{self.resolver_dns}; Docker policy disables iptables and docker0"
        )

    def run_first_boot(self) -> str:
        self.appliance.ssh(f"sudo -n test ! -e {FIRSTBOOT_MARKER}")
        # Reproduce a Debian install where the package units are already active
        # before the first apply writes their rendered configuration. A plain
        # start in apply would be a no-op and leave the old config loaded. The
        # genericcloud image runs systemd-resolved on port 53, so a harness-only
        # drop-in lets the pre-apply dnsmasq start without a DNS listener.
        self.appliance.write_remote_file(HARNESS_DNSMASQ_DROPIN, HARNESS_DNSMASQ_CONFIG, mode="0644")
        self.appliance.ssh(
            "sudo -n systemctl reset-failed dnsmasq.service chrony.service 2>/dev/null || true; "
            "sudo -n systemctl start dnsmasq.service chrony.service",
            timeout=120,
        )
        # This pre-apply start is a fixture, not an enablement shortcut: it
        # reproduces a Debian install where the package postinst already started
        # these units, so the first apply must restart the active dnsmasq and
        # chrony rather than issue a no-op start. The harness does not enable
        # either unit and does not start or restart anything after the
        # post-apply reboot.
        before_units = self.appliance.ssh(
            "systemctl is-active dnsmasq.service chrony.service", timeout=30, check=False
        )
        if before_units.returncode != 0 or before_units.stdout.split() != ["active", "active"]:
            raise HarnessError(
                "dnsmasq or chrony was not active before the first boot apply:\n"
                f"$ systemctl is-active dnsmasq.service chrony.service\n"
                f"{before_units.stdout}{before_units.stderr}"
            )
        before_invocations = self.service_invocations(("dnsmasq.service", "chrony.service"))
        # The pre-apply drop-in only exists to start dnsmasq beside
        # systemd-resolved. Remove it before apply writes bittern.conf so the
        # restart does not load a duplicate port=0 keyword. The running daemon
        # keeps the config it already loaded until that restart.
        self.appliance.ssh(f"sudo -n rm -f {HARNESS_DNSMASQ_DROPIN}", timeout=30)
        # QEMU reports both virtio links as carrying. Take the isolated camera
        # link down so detection sees one live house-LAN link and proposes
        # lan0/cam0, exactly as an idle camera segment would on the appliance.
        # The link stays down through apply: first boot must commit with cam0
        # unplugged, and networkd must configure the address without carrier.
        try:
            self.appliance.set_link("bittern-cam0", False)
        except HarnessError as exc:
            raise HarnessError(
                "cannot take cam0 link down for the first-boot role proposal; "
                f"guest was not started with the harness device id ({exc})"
            ) from exc
        time.sleep(2)
        carrier = self.appliance.ssh(
            f"cat /sys/class/net/{self.camera_iface}/carrier"
        ).stdout.strip()
        if carrier != "0":
            raise HarnessError(
                f"{self.camera_iface} carrier is {carrier!r} after set_link down, expected 0"
            )
        # The installer enabled the unit. This scenario starts the oneshot
        # directly instead of rebooting, so the active dnsmasq and chrony
        # restart path stays deterministic; the post-apply reboot later is the
        # strict no-harness-start check. The harness does not enable any unit.
        first_boot_enabled = self.appliance.ssh(
            "systemctl is-enabled bittern-first-boot.service", timeout=30, check=False
        ).stdout.strip()
        if first_boot_enabled != "enabled":
            raise HarnessError(
                "bittern-first-boot.service is "
                f"{first_boot_enabled!r} after install, expected enabled"
            )
        self.appliance.ssh(
            "sudo -n systemctl reset-failed bittern-first-boot.service 2>/dev/null || true; "
            "sudo -n systemctl start --no-block bittern-first-boot.service",
            timeout=30,
        )
        detected = self.appliance.ssh(
            "for _ in $(seq 1 200); do "
            "if sudo -n test -s /etc/bittern/appliance.yaml; then echo detected; exit 0; fi; "
            "sleep 0.05; done; exit 1",
            timeout=30,
            check=False,
        )
        if detected.returncode != 0:
            detail = self.guest_failure_detail("bittern-first-boot.service")
            raise HarnessError(
                "first boot did not write appliance.yaml during detection\n"
                f"{detected.stdout}{detected.stderr}\n{detail}"
            )
        deadline = time.monotonic() + 600
        marker_error = "marker not written"
        while time.monotonic() < deadline:
            check = self.appliance.ssh(
                f"sudo -n test -s {FIRSTBOOT_MARKER} && echo marker || echo waiting",
                timeout=30,
                check=False,
            )
            if check.returncode == 0 and check.stdout.strip() == "marker":
                break
            marker_error = (check.stdout or check.stderr).strip()
            time.sleep(2)
        else:
            detail = self.guest_failure_detail("bittern-first-boot.service")
            raise HarnessError(
                f"bittern-first-boot.service did not write the marker: {marker_error}\n{detail}"
            )
        status = self.appliance.ssh(
            "sudo -n systemctl show -p Result --value bittern-first-boot.service",
            timeout=30,
        ).stdout.strip()
        if status not in ("success", "exit-code"):
            raise HarnessError(f"bittern-first-boot.service Result is {status!r}")
        raw = self.appliance.ssh(f"sudo -n cat {FIRSTBOOT_MARKER}", timeout=30).stdout
        self.appliance.ssh("sudo -n test -s /etc/bittern/appliance.yaml", timeout=30)
        self.marker = json.loads(raw)
        if self.marker.get("generation", 0) < 1:
            raise HarnessError(f"first-boot marker has no applied generation: {raw}")
        if not self.marker.get("nic_roles_confirmed"):
            raise HarnessError(f"first boot did not confirm NIC roles: {raw}")

        # The marker exists only after apply commits. Prove the camera link was
        # still down at that point, the rendered address exists, the services
        # that bind it are active, and the cam0 address health probe passed.
        carrier = self.appliance.ssh("cat /sys/class/net/cam0/carrier", timeout=15).stdout.strip()
        if carrier != "0":
            raise HarnessError(
                f"cam0 carrier is {carrier!r} after first boot committed, expected 0"
            )
        address = self.appliance.ssh("ip -4 -o addr show dev cam0", timeout=15).stdout
        if "10.77.0.1/24" not in address:
            raise HarnessError(
                "first boot committed with cam0 down but the camera address is missing:\n"
                f"$ ip -4 -o addr show dev cam0\n{address}"
            )
        units = self.appliance.ssh(
            "systemctl is-active nftables.service dnsmasq.service chrony.service",
            timeout=15,
            check=False,
        )
        if units.returncode != 0 or units.stdout.split() != ["active", "active", "active"]:
            raise HarnessError(
                "nftables, dnsmasq or chrony did not stay active with cam0 down:\n"
                f"$ systemctl is-active nftables.service dnsmasq.service chrony.service\n"
                f"{units.stdout}{units.stderr}"
            )
        nft_enabled = self.appliance.ssh(
            "systemctl is-enabled nftables.service", timeout=15, check=False
        ).stdout.strip()
        if nft_enabled != "enabled":
            raise HarnessError(
                f"nftables.service is not enabled after first boot: {nft_enabled!r}"
            )
        after_invocations = self.service_invocations(("dnsmasq.service", "chrony.service"))
        for unit in ("dnsmasq.service", "chrony.service"):
            before = before_invocations.get(unit, "")
            after = after_invocations.get(unit, "")
            if not before or not after or before == after:
                raise HarnessError(
                    f"{unit} was not restarted by the first boot apply: "
                    f"InvocationID before={before!r} after={after!r}"
                )
        generation = self.marker["generation"]
        manifest_raw = self.appliance.ssh(
            f"sudo -n cat /var/lib/bittern/generations/{generation}/manifest.json", timeout=15
        ).stdout
        try:
            manifest = json.loads(manifest_raw)
        except json.JSONDecodeError as exc:
            raise HarnessError(
                f"cannot read the applied generation {generation} manifest: {manifest_raw!r}"
            ) from exc
        health = {
            item.get("name"): item.get("status")
            for item in manifest.get("health", [])
            if isinstance(item, dict)
        }
        for name in (
            "lan0-link",
            "lan0-address",
            "cam0-address",
            "dnsmasq-dhcp",
            "chrony-config",
            "firewall-table",
        ):
            if health.get(name) != "passed":
                raise HarnessError(
                    f"{name} did not pass with cam0 down: {json.dumps(health, sort_keys=True)}"
                )
        invocations = (
            f"$ systemctl is-enabled nftables.service\n{nft_enabled}\n"
            f"$ systemctl show -p InvocationID --value dnsmasq.service chrony.service\n"
            f"before: {json.dumps(before_invocations, sort_keys=True)}\n"
            f"after: {json.dumps(after_invocations, sort_keys=True)}\n"
        )
        down_evidence = (
            f"carrier={carrier}\n{address}"
            f"$ systemctl is-active nftables.service dnsmasq.service chrony.service\n"
            f"{units.stdout}"
            f"{invocations}"
            f"health={json.dumps(health, sort_keys=True)}\n"
        )
        self.capture("firstboot-cam0-down.txt", down_evidence)

        self.rendered_tree = self.appliance.ssh(
            "sudo -n sh -c 'find /etc/bittern /etc/systemd/network /etc/nftables.conf "
            "/etc/dnsmasq.d /etc/chrony /var/lib/bittern -maxdepth 4 -type f | sort'",
            timeout=30,
        ).stdout.splitlines()
        self.capture("marker.json", raw)
        self.capture("rendered-tree.txt", "\n".join(self.rendered_tree) + "\n")
        return (
            f"marker generation {self.marker['generation']} with cam0 carrier down, "
            f"nic_roles_confirmed={self.marker['nic_roles_confirmed']}, "
            f"dnsmasq and chrony restarted, DHCP/NTP/firewall health passed, "
            f"{len(self.rendered_tree)} rendered files"
        )

    def reinstall_after_firewall(self) -> str:
        # Reproduce the GK41 rerun exactly: the firewall is already loaded and
        # the installer runs again. It must keep its daemon.json, start Docker,
        # and leave Docker usable after an explicit restart.
        firewall = self.appliance.ssh(
            "sudo -n nft list chain inet bittern input", timeout=30
        ).stdout
        if 'iifname "lan0" tcp dport 443 accept' not in firewall:
            raise HarnessError(
                "the firewall is not active before the installer rerun:\n" + firewall
            )
        if not self.installer_path:
            raise HarnessError("the extracted installer path was not recorded")
        result = self.appliance.ssh(
            f"cd {shlex.quote(self.installer_path)}; sudo -n ./install.sh", timeout=900
        )
        output = result.stdout + result.stderr
        self.capture("installer-rerun-output.txt", output)
        if "already has the Bittern network policy" not in output:
            raise HarnessError(
                "the installer rerun did not recognise its existing Docker daemon configuration:\n"
                + output
            )
        daemon_config = self.appliance.ssh(
            f"sudo -n cat {FIRSTBOOT_DOCKER_DAEMON}", timeout=30
        ).stdout.strip()
        if daemon_config != FIRSTBOOT_DOCKER_POLICY:
            raise HarnessError(
                f"the installer rerun changed the Docker daemon configuration: {daemon_config!r}"
            )
        self.appliance.ssh("sudo -n systemctl is-active --quiet docker", timeout=30)
        self.appliance.ssh("test ! -e /sys/class/net/docker0", timeout=30)

        self.appliance.ssh("sudo -n systemctl restart docker", timeout=90)
        self.appliance.ssh("sudo -n systemctl is-active --quiet docker", timeout=30)
        self.appliance.ssh("test ! -e /sys/class/net/docker0", timeout=30)
        status = self.appliance.ssh(
            "sudo -n systemctl status docker --no-pager -l", timeout=60
        ).stdout
        journal = self.appliance.ssh(
            "sudo -n journalctl -xeu docker.service -b --no-pager -n 200", timeout=60
        ).stdout
        nftables = self.appliance.ssh(
            "sudo -n nft list table inet bittern", timeout=30
        ).stdout
        evidence = (
            f"$ cat {FIRSTBOOT_DOCKER_DAEMON}\n{daemon_config}\n"
            f"$ systemctl status docker --no-pager -l\n{status}\n"
            f"$ journalctl -xeu docker.service -b --no-pager -n 200\n{journal}\n"
            f"$ nft list table inet bittern\n{nftables}"
        )
        (self.log_dir / "docker-after-firewall.txt").write_text(
            self.redact(evidence), encoding="utf-8"
        )
        self.capture("docker-after-firewall.txt", evidence)
        self.appliance.ssh(
            f"rm -rf /tmp/bittern-poc {shlex.quote(self.installer_archive)}", timeout=30
        )
        self.installer_path = ""
        self.installer_archive = ""
        return (
            "installer rerun kept the Docker policy, Docker restarted with the firewall "
            "active, and docker0 is absent"
        )

    def assert_units_after_reboot(self) -> str:
        enabled = self.appliance.ssh(
            "systemctl is-enabled " + " ".join(REBOOT_ENABLED_UNITS),
            timeout=30,
            check=False,
        )
        active = self.appliance.ssh(
            "systemctl is-active " + " ".join(REBOOT_ACTIVE_UNITS),
            timeout=30,
            check=False,
        )
        enabled_states = enabled.stdout.split()
        active_states = active.stdout.split()
        if enabled.returncode != 0 or enabled_states != ["enabled"] * len(REBOOT_ENABLED_UNITS):
            raise HarnessError(
                "the runbook units are not all enabled after reboot:\n"
                f"$ systemctl is-enabled {' '.join(REBOOT_ENABLED_UNITS)}\n"
                f"{enabled.stdout}{enabled.stderr}"
            )
        if active.returncode != 0 or active_states != ["active"] * len(REBOOT_ACTIVE_UNITS):
            raise HarnessError(
                "the runbook units are not all active after reboot:\n"
                f"$ systemctl is-active {' '.join(REBOOT_ACTIVE_UNITS)}\n"
                f"{active.stdout}{active.stderr}"
            )
        evidence = (
            f"$ systemctl is-enabled {' '.join(REBOOT_ENABLED_UNITS)}\n"
            f"{enabled.stdout}"
            f"$ systemctl is-active {' '.join(REBOOT_ACTIVE_UNITS)}\n"
            f"{active.stdout}"
        )
        (self.log_dir / "units-after-reboot.txt").write_text(
            self.redact(evidence), encoding="utf-8"
        )
        self.capture("units-after-reboot.txt", evidence)
        return (
            "every runbook unit is enabled after reboot and docker, nftables, bitternd, "
            "dnsmasq, chrony, networkd and resolved are active"
        )

    def assert_image_prewarm(self) -> str:
        # The installer enabled the prewarm unit and the reboot started it. This
        # phase owns no pull: it waits for the oneshot and asserts the pinned
        # image is in the local Docker database and the recorded state is ready.
        deadline = time.monotonic() + max(900, self.limits["test_suite"])
        state = ""
        outcome = ""
        while time.monotonic() < deadline:
            result = self.appliance.ssh(
                "systemctl show -p ActiveState -p Result "
                "bittern-image-prewarm.service",
                timeout=30,
                check=False,
            )
            properties: dict[str, str] = {}
            for line in result.stdout.splitlines():
                key, separator, value = line.partition("=")
                if separator:
                    properties[key.strip()] = value.strip()
            state = properties.get("ActiveState", "")
            outcome = properties.get("Result", "")
            if state == "active" and outcome == "success":
                break
            if state == "failed" or outcome in ("failed", "timeout", "exit-code"):
                break
            time.sleep(5)
        if state != "active" or outcome != "success":
            journal = self.appliance.ssh(
                "sudo -n journalctl -xeu bittern-image-prewarm.service -b --no-pager -n 200",
                timeout=60,
                check=False,
            ).stdout
            raise HarnessError(
                "the prewarm unit did not finish successfully after first boot: "
                f"ActiveState={state!r} Result={outcome!r}\n"
                f"$ journalctl -xeu bittern-image-prewarm.service\n{journal}"
            )

        inspect = self.appliance.ssh(
            "sudo -n docker image inspect --format '{{.Id}}' "
            + shlex.quote(FRIGATE_IMAGE_REFERENCE),
            timeout=60,
            check=False,
        )
        local_id = inspect.stdout.strip()
        if inspect.returncode != 0 or not local_id.startswith("sha256:"):
            raise HarnessError(
                "the pinned recording-software image is not present locally after "
                f"the prewarm unit succeeded: {local_id!r}\n{inspect.stderr}"
            )

        recorded = self.appliance.ssh(
            "sudo -n cat /var/lib/bittern/image-prewarm.json", timeout=30, check=False
        )
        if recorded.returncode != 0:
            raise HarnessError(
                "the prewarm unit left no state file for status: " + recorded.stderr
            )
        try:
            image_state = json.loads(recorded.stdout)
        except json.JSONDecodeError as exc:
            raise HarnessError(f"the prewarm state file is not JSON: {exc}") from exc
        if image_state.get("state") != "ready":
            raise HarnessError(
                f"the prewarm state is {image_state.get('state')!r}, want 'ready'"
            )
        if image_state.get("digest") != FRIGATE_IMAGE_DIGEST:
            raise HarnessError(
                "the prewarm state digest does not match the pinned digest: "
                f"{image_state.get('digest')!r}"
            )

        evidence = (
            "$ systemctl show -p ActiveState -p Result "
            f"bittern-image-prewarm.service\n"
            f"ActiveState={state}\nResult={outcome}\n"
            f"$ docker image inspect --format '{{{{.Id}}}}' {FRIGATE_IMAGE_REFERENCE}\n"
            f"{local_id}\n"
            f"$ cat /var/lib/bittern/image-prewarm.json\n{recorded.stdout}"
        )
        (self.log_dir / "image-prewarm.txt").write_text(
            self.redact(evidence), encoding="utf-8"
        )
        self.capture("image-prewarm.txt", evidence)
        return (
            "the prewarm unit succeeded after first boot and the pinned image "
            f"({FRIGATE_IMAGE_DIGEST}) is present locally"
        )

    def reboot_after_apply(self) -> str:
        # Apply must have enabled the rendered unit before this point. Record
        # the pre-reboot state for the failure message, then reboot without any
        # harness enable or start. The post-reboot assertion is the acceptance:
        # a unit that was only started for the current boot does not come back.
        pre_enabled = self.appliance.ssh(
            "systemctl is-enabled bittern-frigate.service", timeout=30, check=False
        ).stdout.strip()
        pre_active = self.appliance.ssh(
            "systemctl is-active bittern-frigate.service", timeout=30, check=False
        ).stdout.strip()
        pre_container = self.appliance.ssh(
            "sudo -n docker inspect --format '{{.State.Status}}' frigate",
            timeout=30,
            check=False,
        ).stdout.strip()
        self.post_apply_reboot = {
            "enabled": pre_enabled,
            "active": pre_active,
            "container": pre_container,
        }
        self.appliance.set_link("bittern-cam0", True)
        self.appliance.ssh("sudo -n systemctl reboot", timeout=30, check=False)
        time.sleep(5)
        self.appliance.set_link("bittern-cam0", True)
        self.wait_for_ssh(self.appliance, self.limits["boot"])
        time.sleep(2)
        return (
            "rebooted after the camera apply without enabling or starting a unit; "
            f"pre-reboot Frigate enabled={pre_enabled!r} active={pre_active!r} "
            f"container={pre_container!r}"
        )

    def wait_for_status_ready(self) -> dict[str, Any]:
        deadline = time.monotonic() + 300
        last = "no attempt"
        while time.monotonic() < deadline:
            try:
                document = self.status_payload()
            except (HarnessError, json.JSONDecodeError) as exc:
                last = str(exc)
                time.sleep(3)
                continue
            sections = document.get("sections", {})
            frigate = sections.get("frigate", {})
            if document.get("state") == "ready" and frigate.get("state") == "ok":
                self.status = document
                self.capture(
                    "status-after-reboot.json",
                    json.dumps(document, indent=2, sort_keys=True) + "\n",
                )
                return document
            last = json.dumps(
                {
                    "state": document.get("state"),
                    "reason": document.get("reason"),
                    "frigate": frigate,
                },
                sort_keys=True,
            )
            time.sleep(3)
        raise HarnessError(
            "the status document did not become ready with Frigate ok after the "
            f"post-apply reboot: {last}"
        )

    def assert_appliance_units_after_reboot(self) -> str:
        enabled = self.appliance.ssh(
            "systemctl is-enabled " + " ".join(POST_APPLY_ENABLED_UNITS),
            timeout=30,
            check=False,
        )
        active = self.appliance.ssh(
            "systemctl is-active " + " ".join(POST_APPLY_ACTIVE_UNITS),
            timeout=30,
            check=False,
        )
        enabled_states = enabled.stdout.split()
        active_states = active.stdout.split()
        if enabled.returncode != 0 or enabled_states != ["enabled"] * len(POST_APPLY_ENABLED_UNITS):
            raise HarnessError(
                "the post-apply reboot did not leave every appliance unit enabled; "
                "apply must enable the rendered Frigate unit, not only start it:\n"
                f"pre-reboot: {json.dumps(self.post_apply_reboot, sort_keys=True)}\n"
                f"$ systemctl is-enabled {' '.join(POST_APPLY_ENABLED_UNITS)}\n"
                f"{enabled.stdout}{enabled.stderr}"
            )
        if active.returncode != 0 or active_states != ["active"] * len(POST_APPLY_ACTIVE_UNITS):
            raise HarnessError(
                "the post-apply reboot did not leave every active appliance unit active; "
                "Frigate must start from the enabled unit without a harness start:\n"
                f"pre-reboot: {json.dumps(self.post_apply_reboot, sort_keys=True)}\n"
                f"$ systemctl is-active {' '.join(POST_APPLY_ACTIVE_UNITS)}\n"
                f"{active.stdout}{active.stderr}"
            )
        container = self.appliance.ssh(
            "sudo -n docker inspect --format '{{.State.Status}}' frigate",
            timeout=30,
            check=False,
        ).stdout.strip()
        if container != "running":
            raise HarnessError(
                "the Frigate container is not running after the post-apply reboot: "
                f"{container!r}\n"
                f"pre-reboot: {json.dumps(self.post_apply_reboot, sort_keys=True)}"
            )
        document = self.wait_for_status_ready()
        sections = document.get("sections", {})
        frigate = sections.get("frigate", {})
        render = sections.get("render", {})
        if frigate.get("camera_count") != 1:
            raise HarnessError(
                f"the post-reboot status does not report one camera: {json.dumps(frigate, sort_keys=True)}"
            )
        image = frigate.get("image", {})
        if image.get("state") != "ready" or image.get("digest") != FRIGATE_IMAGE_DIGEST:
            raise HarnessError(
                "the post-reboot status does not report the pinned recording "
                f"software as ready: {json.dumps(image, sort_keys=True)}"
            )
        if render.get("apply_state") != "applied":
            raise HarnessError(
                f"the post-reboot status is not applied: {json.dumps(render, sort_keys=True)}"
            )
        status = self.appliance.ssh(
            "sudo -n systemctl status bittern-frigate.service --no-pager -l",
            timeout=60,
            check=False,
        ).stdout
        journal = self.appliance.ssh(
            "sudo -n journalctl -xeu bittern-frigate.service -b --no-pager -n 200",
            timeout=60,
            check=False,
        ).stdout
        evidence = (
            f"pre-reboot: {json.dumps(self.post_apply_reboot, sort_keys=True)}\n"
            f"$ systemctl is-enabled {' '.join(POST_APPLY_ENABLED_UNITS)}\n{enabled.stdout}"
            f"$ systemctl is-active {' '.join(POST_APPLY_ACTIVE_UNITS)}\n{active.stdout}"
            f"$ docker inspect --format '{{{{.State.Status}}}}' frigate\n{container}\n"
            f"$ systemctl status bittern-frigate.service --no-pager -l\n{status}\n"
            f"$ journalctl -xeu bittern-frigate.service -b --no-pager -n 200\n{journal}\n"
            f"$ GET /api/status\n{json.dumps(document, indent=2, sort_keys=True)}\n"
        )
        (self.log_dir / "frigate-after-reboot.txt").write_text(
            self.redact(evidence), encoding="utf-8"
        )
        self.capture("frigate-after-reboot.txt", evidence)
        return (
            "post-apply reboot: every appliance unit enabled, every active unit "
            "active, Frigate container running, and status ready with Frigate ok"
        )

    def reboot_once(self) -> str:
        # The guest NICs are already named lan0 and cam0 by cloud-init, so this
        # reboot exercises the marker skip and networkd reading the generated
        # .network files, not a stock-name .link rename. Keep cam0 down across
        # the reboot so the generated .network file must configure the camera
        # address without carrier.
        carrier = self.appliance.ssh("cat /sys/class/net/cam0/carrier").stdout.strip()
        if carrier != "0":
            raise HarnessError(f"cam0 carrier is {carrier!r} before reboot, expected 0")
        # The GK41 came up after reboot with an empty /etc/resolv.conf once
        # networkd owned lan0 and no resolver was installed. Remove the file so
        # this reboot must recreate it from the package tmpfiles rule and
        # systemd-resolved's DHCP data. The installer's resolver configuration
        # is what makes that possible.
        self.appliance.ssh(f"sudo -n rm -f {FIRSTBOOT_RESOLV_CONF}", timeout=30)
        # Do not enable or start any unit here. install.sh owns enablement for
        # the units it installs and apply owns the rendered units; a
        # test-issued enable or start would hide exactly the failure this
        # reboot must catch.
        self.appliance.ssh("sudo -n systemctl reboot", timeout=30, check=False)
        time.sleep(5)
        self.wait_for_ssh(self.appliance, self.limits["boot"])
        nft_table = self.appliance.ssh(
            "sudo -n nft list table inet bittern", timeout=30
        ).stdout
        if "table inet bittern" not in nft_table:
            raise HarnessError(
                "the rendered inet bittern table is not loaded after reboot:\n" + nft_table
            )
        self.appliance.ssh(
            "sudo -n sh -c 'test -s /var/lib/bittern/first-boot.json; "
            "test \"$(cat /sys/class/net/cam0/carrier)\" = 0; "
            "ip -4 addr show dev cam0 | grep -q 10.77.0.1/24; "
            "ip route show default | grep -q \"dev lan0\"'",
            timeout=60,
        )
        self.capture("nftables-after-reboot.txt", f"$ nft list table inet bittern\n{nft_table}")
        return (
            "rebooted once with cam0 still down; marker survived, the generated cam0 "
            "address is present and inet bittern is loaded"
        )

    def assert_resolver_after_reboot(self) -> str:
        deadline = time.monotonic() + 60
        resolv = status = hosts = None
        last = "no attempt"
        while time.monotonic() < deadline:
            resolv = self.appliance.ssh(
                f"test -s {FIRSTBOOT_RESOLV_CONF} && cat {FIRSTBOOT_RESOLV_CONF}",
                timeout=30,
                check=False,
            )
            status = self.appliance.ssh(
                "sudo -n resolvectl status", timeout=30, check=False
            )
            hosts = self.appliance.ssh(
                "getent hosts deb.debian.org", timeout=30, check=False
            )
            if (
                resolv.returncode == 0
                and resolv.stdout.strip()
                and status.returncode == 0
                and self.resolver_dns in status.stdout
                and hosts.returncode == 0
                and hosts.stdout.strip()
            ):
                break
            last = (
                f"$ cat {FIRSTBOOT_RESOLV_CONF}\n{resolv.stdout}{resolv.stderr}"
                f"$ resolvectl status\n{status.stdout}{status.stderr}"
                f"$ getent hosts deb.debian.org\n{hosts.stdout}{hosts.stderr}"
            )
            time.sleep(2)
        else:
            raise HarnessError(
                "DNS did not recover after reboot with the DHCP-provided resolver:\n" + last
            )
        assert resolv is not None and status is not None and hosts is not None
        target = self.appliance.ssh(
            f"readlink -f {FIRSTBOOT_RESOLV_CONF}", timeout=30
        ).stdout.strip()
        if target != FIRSTBOOT_RESOLVED_STUB:
            raise HarnessError(
                f"{FIRSTBOOT_RESOLV_CONF} resolves to {target!r}, expected {FIRSTBOOT_RESOLVED_STUB}"
            )
        enabled = self.appliance.ssh(
            f"systemctl is-enabled {FIRSTBOOT_RESOLVED_UNIT}", timeout=30, check=False
        ).stdout.strip()
        active = self.appliance.ssh(
            f"systemctl is-active {FIRSTBOOT_RESOLVED_UNIT}", timeout=30, check=False
        ).stdout.strip()
        if enabled != "enabled" or active != "active":
            raise HarnessError(
                f"{FIRSTBOOT_RESOLVED_UNIT} is {enabled!r}/{active!r}, expected enabled/active"
            )
        evidence = (
            f"$ readlink -f {FIRSTBOOT_RESOLV_CONF}\n{target}\n"
            f"$ cat {FIRSTBOOT_RESOLV_CONF}\n{resolv.stdout}"
            f"$ resolvectl status\n{status.stdout}"
            f"$ getent hosts deb.debian.org\n{hosts.stdout}"
            f"$ systemctl is-enabled {FIRSTBOOT_RESOLVED_UNIT}\n{enabled}\n"
            f"$ systemctl is-active {FIRSTBOOT_RESOLVED_UNIT}\n{active}\n"
        )
        (self.log_dir / "resolver-after-reboot.txt").write_text(
            self.redact(evidence), encoding="utf-8"
        )
        self.capture("resolver-after-reboot.txt", evidence)
        return (
            f"{FIRSTBOOT_RESOLV_CONF} is {target}, resolvectl shows the DHCP DNS "
            f"{self.resolver_dns}, and getent hosts deb.debian.org resolves"
        )

    def wait_for_ssh(self, vm: Vm, timeout: float) -> None:
        deadline = time.monotonic() + timeout
        last_error = "no attempt"
        while time.monotonic() < deadline:
            try:
                vm.ssh("true", timeout=8)
                return
            except HarnessError as exc:
                last_error = str(exc)
                time.sleep(2)
        raise HarnessError(f"{vm.name} did not answer SSH within {timeout:.0f}s: {last_error}")

    def read_setup_token(self) -> str:
        stat = self.appliance.ssh(
            f"sudo -n stat -c '%a %U:%G' {FIRSTBOOT_TOKEN_PATH}", timeout=30
        ).stdout.strip()
        if stat != "600 root:root":
            raise HarnessError(f"setup token file has mode/owner {stat!r}, expected 600 root:root")
        secrets_stat = self.appliance.ssh(
            f"sudo -n stat -c '%a %U:%G' {FIRSTBOOT_SECRETS}", timeout=30
        ).stdout.strip()
        if secrets_stat != "600 root:root":
            raise HarnessError(f"secrets.yaml has mode/owner {secrets_stat!r}, expected 600 root:root")
        token = self.appliance.ssh("sudo -n bittern setup-token", timeout=30).stdout.strip()
        if not token or any(char.isspace() for char in token):
            raise HarnessError("bittern setup-token did not return one token value")
        self.token = token
        secrets = self.guest_file(FIRSTBOOT_SECRETS, secret=True)
        if "setup_short_code_hash:" not in secrets:
            raise HarnessError(
                "first boot did not write the console short-code hash; the local console block may not have run"
            )
        after = self.appliance.ssh(
            f"sudo -n stat -c '%a %U:%G' {FIRSTBOOT_TOKEN_PATH}", timeout=30
        ).stdout.strip()
        if after != "600 root:root":
            raise HarnessError(f"setup token file changed to {after!r} after reading it")
        journal = self.appliance.ssh(
            "sudo -n journalctl --no-pager -o cat -u bittern-first-boot.service "
            "-u bitternd.service -u systemd-networkd.service -u nftables.service "
            "-u dnsmasq.service -u chrony.service",
            timeout=60,
        ).stdout
        if token in journal:
            raise HarnessError("the setup token appears in the systemd journal")
        if "setup QR not shown" in journal:
            raise HarnessError("first boot could not write the setup block to the local console")
        self.capture("setup-token-journal.txt", journal)
        return "token read with mode 0600 and absent from the service journal"

    def read_setup_token_qr(self) -> str:
        output = self.appliance.ssh("sudo -n bittern setup-token --qr", timeout=30).stdout
        decoded = decode_ansi_qr(output)
        address = self.appliance.ssh(
            "ip -4 -o addr show dev lan0", timeout=30
        ).stdout.split()
        if len(address) < 4 or "/" not in address[3]:
            raise HarnessError("lan0 has no IPv4 address for the setup URL")
        lan_address = address[3].split("/", 1)[0]
        expected = f"https://{lan_address}/ui/setup#token={self.token}"
        if decoded != expected:
            raise HarnessError(
                "the decoded QR URL is not the exact https://<lan0-address>/ui/setup "
                "URL with the setup token in the fragment"
            )
        parsed = urllib.parse.urlsplit(decoded)
        if parsed.fragment != "token=" + self.token:
            raise HarnessError("the QR fragment is not the exact setup token")
        self.setup_url = decoded

        grouped_match = re.search(r"^Setup token:\s*(\S.*)$", output, re.MULTILINE)
        if grouped_match is None:
            raise HarnessError("the setup block does not print the grouped token")
        grouped = grouped_match.group(1).replace(" ", "")
        if grouped != self.token:
            raise HarnessError("the grouped token does not decode to the setup token")

        short_match = re.search(r"^Short code:\s*([0-9A-Za-z-]+)", output, re.MULTILINE)
        if short_match is None:
            raise HarnessError("the setup block does not print the short code")
        short_code = short_match.group(1).replace("-", "").upper()
        if not re.fullmatch(r"[0-9A-HJKMNP-TV-Z]{8}", short_code):
            raise HarnessError("the short code has an unexpected format")
        if short_code in self.token:
            raise HarnessError("the short code is a prefix or truncation of the setup token")
        self.short_code = short_code

        secrets = self.guest_file(FIRSTBOOT_SECRETS, secret=True)
        if short_code in secrets:
            raise HarnessError("the plaintext short code appears in secrets.yaml")
        if "setup_short_code_hash:" not in secrets:
            raise HarnessError("secrets.yaml does not carry the short-code hash")
        return (
            "QR decodes to the lan0 setup URL with the token in the fragment; "
            "the grouped token matches; a separate short code was issued"
        )

    def start_daemon(self) -> str:
        loaded = self.appliance.ssh(
            "sudo -n nft list chain inet bittern input", timeout=30
        ).stdout
        if 'iifname "lan0" tcp dport 443 accept' not in loaded:
            raise HarnessError(
                "the live firewall does not accept the Bittern UI on lan0 tcp 443:\n" + loaded
            )
        self.appliance.ssh("sudo -n systemctl restart bitternd.service", timeout=90)
        self.appliance.ssh(
            "sudo -n sh -c 'systemctl is-active --quiet bitternd.service; "
            "test -s /etc/bittern/tls/server.crt'",
            timeout=30,
        )
        port = int(self.config["network"]["lan"]["hostfwd"]["ui"])
        wait_for_tcp(port, 60)
        context = ssl._create_unverified_context()  # noqa: SLF001 - self-signed appliance cert
        connection = http.client.HTTPSConnection("127.0.0.1", port, context=context, timeout=15)
        try:
            connection.request("GET", "/ui/")
            response = connection.getresponse()
            status = response.status
            location = response.getheader("Location", "")
            response.read()
        finally:
            connection.close()
        if status != 303 or location != "/ui/setup":
            raise HarnessError(
                f"TLS UI returned HTTP {status} Location {location!r}, expected 303 /ui/setup"
            )
        plain = http.client.HTTPConnection("127.0.0.1", port, timeout=8)
        outcome = ""
        try:
            plain.request("GET", "/ui/")
            response = plain.getresponse()
            outcome = f"HTTP {response.status}"
            response.read()
        except (OSError, http.client.HTTPException) as exc:
            outcome = type(exc).__name__
        finally:
            plain.close()
        if outcome.startswith("HTTP 2") or outcome.startswith("HTTP 3"):
            raise HarnessError(f"plain HTTP answered on the TLS UI port: {outcome}")
        return f"TLS UI redirects to setup; plain HTTP rejected ({outcome})"

    def drive_wizard(self) -> str:
        self.admin_password = "Bittern-QEMU-" + hashlib.sha256(
            (self.token + time.strftime("%s")).encode("utf-8")
        ).hexdigest()[:20]
        client = WizardClient(int(self.config["network"]["lan"]["hostfwd"]["ui"]))
        client.get_page("/ui/setup")

        # The short code is a fallback credential. A weak password proves it is
        # accepted without consuming it; the real setup below uses the token
        # from the QR fragment.
        if self.short_code:
            client.post(
                "/ui/setup",
                {
                    "setup_token": self.short_code,
                    "password": "short",
                    "password_confirm": "short",
                },
                expect=422,
            )

        fragment_token = self.token
        if self.setup_url:
            parsed = urllib.parse.urlsplit(self.setup_url)
            if parsed.fragment != "token=" + self.token:
                raise HarnessError("the setup URL fragment does not carry the setup token")
            fragment_token = parsed.fragment[len("token=") :]
        client.post(
            "/ui/setup",
            {
                "setup_token": fragment_token,
                "password": self.admin_password,
                "password_confirm": self.admin_password,
            },
        )

        # A second submission and a reload cannot reuse the token. The
        # authenticated session is redirected away from the setup page.
        status, location, _ = client._request(
            "POST",
            "/ui/setup",
            {
                "csrf_token": client.csrf,
                "setup_token": fragment_token,
                "password": self.admin_password,
                "password_confirm": self.admin_password,
            },
        )
        if status != 303 or location != "/ui/status":
            raise HarnessError(
                f"a second setup submission returned HTTP {status} Location {location!r}"
            )
        status, location, _ = client._request("GET", "/ui/setup")
        if status != 303 or location != "/ui/status":
            raise HarnessError(
                f"a setup reload returned HTTP {status} Location {location!r}"
            )

        body = client.get_page("/ui/wizard")
        require_data(body, "data-step-name", "welcome", "wizard welcome page")
        client.post("/ui/wizard/network")
        body = client.get_page("/ui/wizard/network")
        require_data(body, "data-step-name", "network", "wizard network page")
        self.capture("wizard-network.html", body)
        if "192.168.1.50/24" in data_values(body, "data-network-lan-address"):
            raise HarnessError("wizard network check still shows sample data")
        cam_address = self.config["network"]["cam"]["appliance_address"] + "/24"
        require_data(body, "data-network-cam-address", cam_address, "wizard network check")
        require_data(body, "data-network-cam-carrier", "true", "wizard network check")
        client.post("/ui/wizard/storage")
        body = client.get_page("/ui/wizard/storage")
        require_data(body, "data-step-name", "storage", "wizard storage page")
        self.capture("wizard-storage.html", body)
        require_data(body, "data-storage-present", "true", "wizard storage check")
        if (
            "1 TiB" in data_values(body, "data-storage-free")
            and "100 GiB" in data_values(body, "data-storage-reserve")
        ):
            raise HarnessError("wizard storage check still shows sample data")
        client.post("/ui/wizard/camera/continue")
        body = client.get_page("/ui/wizard/camera")
        require_data(body, "data-step-name", "camera", "wizard camera page")
        client.post(
            "/ui/wizard/camera",
            {
                "name": "Virtual camera",
                "address": self.camera_address,
                "rtsp_port": str(self.camera_port),
                "rtsp_path": self.camera_path,
                "credentials_ref": "",
            },
        )
        body = client.get_page("/ui/wizard/review")
        require_data(body, "data-step-name", "review", "review page")
        require_data(body, "data-camera-address", self.camera_address, "review page")
        require_data(body, "data-camera-port", str(self.camera_port), "review page")
        require_data(body, "data-camera-path", self.camera_path, "review page")
        client.post("/ui/wizard/apply", timeout=1800)
        body = client.get_page("/ui/wizard/done")
        require_data(body, "data-step-name", "done", "wizard done page")
        require_data(body, "data-result-state", "succeeded", "wizard done page")
        self.capture("wizard-done.html", body)
        return "setup token, password, network, storage, camera, review and apply completed"

    def restart_docker_with_frigate(self) -> str:
        image = self.appliance.ssh(
            "sudo -n sed -n 's/^[[:space:]]*image:[[:space:]]*//p' "
            "/var/lib/bittern/compose/compose.yaml | head -n 1",
            timeout=30,
        ).stdout.strip()
        if not image:
            raise HarnessError("the rendered compose file has no image reference")
        daemon_config = self.appliance.ssh(
            f"sudo -n cat {FIRSTBOOT_DOCKER_DAEMON}", timeout=30
        ).stdout.strip()
        if daemon_config != FIRSTBOOT_DOCKER_POLICY:
            raise HarnessError(
                f"the Docker daemon configuration changed before the Frigate restart: {daemon_config!r}"
            )

        # This phase deliberately restarts the Docker daemon, not the Bittern
        # unit. The post-apply reboot already proved that apply enabled the unit
        # and the boot path started the stack. A daemon restart must bring the
        # container back from its rendered `restart: unless-stopped` policy;
        # that policy is a container-level fallback, not a substitute for
        # enabling the unit after a clean shutdown.
        self.appliance.ssh("sudo -n systemctl restart docker", timeout=90)
        self.appliance.ssh("sudo -n systemctl is-active --quiet docker", timeout=30)
        self.appliance.ssh("test ! -e /sys/class/net/docker0", timeout=30)
        none_command = (
            "sudo -n docker run --rm --network none --entrypoint python3 "
            f"{shlex.quote(image)} -c 'print(\"network-none-ok\")'"
        )
        none = self.appliance.ssh(none_command, timeout=180)
        if "network-none-ok" not in none.stdout:
            raise HarnessError(
                "docker run --network none did not complete after the Docker restart:\n"
                f"{none.stdout}{none.stderr}"
            )
        self.wait_for_frigate_health()
        status = self.appliance.ssh(
            "sudo -n systemctl status docker bittern-frigate.service --no-pager -l",
            timeout=60,
        ).stdout
        journal = self.appliance.ssh(
            "sudo -n journalctl -xeu docker.service -b --no-pager -n 120", timeout=60
        ).stdout
        evidence = (
            f"$ docker run --rm --network none ...\n{none.stdout}"
            f"$ systemctl status docker bittern-frigate.service --no-pager -l\n{status}\n"
            f"$ journalctl -xeu docker.service -b --no-pager -n 120\n{journal}"
        )
        (self.log_dir / "docker-frigate-restart.txt").write_text(
            self.redact(evidence), encoding="utf-8"
        )
        self.capture("docker-frigate-restart.txt", evidence)
        return (
            "Docker restarted with the firewall active, --network none worked, and "
            "the Frigate container returned from its restart policy without a unit restart"
        )

    def status_payload(self) -> dict[str, Any]:
        raw = self.appliance.ssh(
            "curl -fsS --max-time 20 http://127.0.0.1:8080/api/status", timeout=30
        ).stdout
        return json.loads(raw)

    def status_document(self, capture_name: str = "status.json") -> dict[str, Any]:
        document = self.status_payload()
        self.status = document
        self.capture(capture_name, json.dumps(document, indent=2, sort_keys=True) + "\n")
        return document

    def assert_cam0_down_status(self) -> str:
        # The daemon is up after the carrierless first boot and reboot. Check
        # that status shows the camera link as down while the address exists,
        # then raise the QEMU link for the wizard, Frigate and the snapshot.
        document = self.status_document("status-cam0-down.json")
        network = document.get("sections", {}).get("network", {})
        lan0 = network.get("lan0", {})
        cam0 = network.get("cam0", {})
        if network.get("state") != "degraded":
            raise HarnessError(
                f"network status with cam0 down is not degraded: {json.dumps(network, sort_keys=True)}"
            )
        if cam0.get("carrier") is not False:
            raise HarnessError(
                f"status does not show cam0 carrier down: {json.dumps(cam0, sort_keys=True)}"
            )
        if not str(cam0.get("address") or "").startswith("10.77.0.1/"):
            raise HarnessError(
                f"status does not show the cam0 address with the link down: "
                f"{json.dumps(cam0, sort_keys=True)}"
            )
        if lan0.get("carrier") is not True or not lan0.get("address"):
            raise HarnessError(
                f"lan0 is not healthy while cam0 is down: {json.dumps(lan0, sort_keys=True)}"
            )
        self.appliance.ssh(
            "sudo -n systemctl is-active --quiet dnsmasq.service chrony.service", timeout=30
        )
        self.appliance.set_link("bittern-cam0", True)
        deadline = time.monotonic() + 60
        last = "cam0 carrier not seen"
        while time.monotonic() < deadline:
            try:
                carrier = self.appliance.ssh("cat /sys/class/net/cam0/carrier", timeout=15).stdout.strip()
                if carrier != "1":
                    last = f"carrier={carrier}"
                    time.sleep(1)
                    continue
                current = self.status_payload()
                current_network = current.get("sections", {}).get("network", {})
                if current_network.get("state") == "ok":
                    return (
                        "status reported cam0 carrier down with 10.77.0.1/24, dnsmasq and "
                        "chrony active; cam0 link raised after first boot and the status "
                        "network section returned to ok"
                    )
                last = json.dumps(current_network, sort_keys=True)
            except (HarnessError, json.JSONDecodeError) as exc:
                last = str(exc)
            time.sleep(1)
        raise HarnessError(
            "cam0 did not return to carrier up with an ok network section after the link "
            f"was raised: {last}"
        )

    def assert_dhcp_lease(self) -> str:
        # The cam0-down phase raised the QEMU camera link. Prove the rendered
        # dnsmasq config is serving DHCP on that segment, not merely active.
        listener = self.appliance.ssh(
            "sudo -n ss -H -lun 'sport = :67'", timeout=30
        ).stdout
        if ":67" not in listener:
            raise HarnessError(f"dnsmasq is not listening on UDP port 67:\n{listener}")
        result = self.camera.ssh(
            "sudo -n python3 -", input_text=SYNTHETIC_DHCP_CLIENT, timeout=60
        )
        output = (result.stdout or result.stderr).strip()
        try:
            lease = json.loads(output.splitlines()[-1])
        except (IndexError, json.JSONDecodeError) as exc:
            raise HarnessError(f"DHCP client did not return a lease: {output}") from exc
        if "error" in lease:
            raise HarnessError(f"DHCP client failed: {json.dumps(lease, sort_keys=True)}")
        address = str(lease.get("address", ""))
        mac = str(lease.get("mac", ""))
        if not address.startswith("10.77.0.") or not mac:
            raise HarnessError(f"DHCP client returned an unexpected lease: {json.dumps(lease, sort_keys=True)}")
        deadline = time.monotonic() + 10
        lease_file = ""
        lease_contents = ""
        while time.monotonic() < deadline:
            found = self.appliance.ssh(
                "sudo -n find /var/lib -maxdepth 4 -type f -name '*.leases' 2>/dev/null | head -n 5",
                timeout=30,
            ).stdout.splitlines()
            for path in found:
                contents = self.appliance.ssh(f"sudo -n cat {shlex.quote(path)}", timeout=30).stdout
                if mac.lower() in contents.lower() and address in contents:
                    lease_file = path
                    lease_contents = contents
                    break
            if lease_file:
                break
            time.sleep(1)
        if not lease_file:
            raise HarnessError(
                "dnsmasq did not record the DHCP client lease under /var/lib: "
                f"client={json.dumps(lease, sort_keys=True)}"
            )
        evidence = (
            f"$ ss -H -lun 'sport = :67'\n{listener}\n"
            f"$ DHCP client on cam0\n{json.dumps(lease, sort_keys=True)}\n"
            f"$ cat {lease_file}\n{lease_contents}"
        )
        (self.log_dir / "dhcp-lease.txt").write_text(self.redact(evidence), encoding="utf-8")
        self.capture("dhcp-lease.txt", evidence)
        return (
            f"camera cam0 received {address} from {lease.get('server', 'dnsmasq')}, "
            f"and {lease_file} records the lease"
        )

    def assert_applied(self) -> str:
        document = self.status_document()
        sections = document.get("sections", {})
        config_section = sections.get("config", {})
        if not config_section.get("valid") or config_section.get("state") != "ok":
            raise HarnessError(f"config is not valid: {json.dumps(config_section, sort_keys=True)}")
        render = sections.get("render", {})
        if render.get("apply_state") != "applied" or not render.get("generation"):
            raise HarnessError(f"render is not applied: {json.dumps(render, sort_keys=True)}")
        frigate = sections.get("frigate", {})
        if not frigate.get("configured") or frigate.get("camera_count") != 1:
            raise HarnessError(f"Frigate/camera inventory is wrong: {json.dumps(frigate, sort_keys=True)}")
        if frigate.get("state") != "ok":
            raise HarnessError(f"Frigate is not healthy in status: {json.dumps(frigate, sort_keys=True)}")
        storage = sections.get("storage", {})
        if not storage.get("present"):
            raise HarnessError(f"recording path is not present: {json.dumps(storage, sort_keys=True)}")
        network = sections.get("network", {})
        if network.get("state") != "ok":
            raise HarnessError(f"network is not healthy: {json.dumps(network, sort_keys=True)}")

        self.appliance.ssh("sudo -n systemctl is-active --quiet bittern-frigate.service", timeout=30)
        self.wait_for_frigate_health()
        self.assert_rendered_files()
        self.assert_frigate_mounts()
        snapshot = self.fetch_snapshot()
        return (
            f"config valid, render applied at generation {render['generation']}, "
            f"Frigate healthy, one camera, snapshot {snapshot['content_type']} "
            f"{snapshot['bytes']} bytes"
        )

    def wait_for_frigate_health(self) -> None:
        deadline = time.monotonic() + 300
        last = "not checked"
        while time.monotonic() < deadline:
            result = self.appliance.ssh(
                "sudo -n docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}"
                "{{else}}none{{end}}' frigate",
                timeout=30,
                check=False,
            )
            last = (result.stdout or result.stderr).strip()
            if result.returncode == 0 and last == "healthy":
                return
            time.sleep(3)
        raise HarnessError(f"Frigate container did not become healthy: {last}")

    def frigate_password(self) -> str:
        result = self.appliance.ssh(
            "sudo -n docker logs --tail 600 frigate 2>&1 | "
            "python3 -c 'import re,sys; m=re.findall(r\"Password:\\s*([^\\s]+)\", sys.stdin.read()); "
            "print(m[-1] if m else \"\")'",
            timeout=60,
            check=False,
        )
        return result.stdout.strip()

    def fetch_snapshot(self) -> dict[str, Any]:
        camera_id = "camera_1"
        base = f"http://127.0.0.1:5000/api/{camera_id}/latest.jpg"
        login = "http://127.0.0.1:5000/api/login"
        password = self.frigate_password()
        script = f"""
set -eu
rm -f /tmp/bittern-frigate.cookies /tmp/bittern-snapshot.jpg
probe() {{
    curl -sS -o /tmp/bittern-snapshot.jpg -w '%{{http_code}} %{{content_type}} %{{size_download}}' \
        -b /tmp/bittern-frigate.cookies "{base}"
}}
result=$(probe)
case "$result" in
    200\\ image/jpeg*) ;;
    *)
        test -n "$1" || {{ printf '%s\\n' "$result"; exit 0; }}
        curl -fsS -c /tmp/bittern-frigate.cookies \
            -H 'Content-Type: application/json' \
            --data '{{"user":"admin","password":"'"$1"'"}}' "{login}" >/dev/null
        result=$(probe)
        ;;
esac
printf '%s\\n' "$result"
rm -f /tmp/bittern-frigate.cookies
"""
        last = ""
        deadline = time.monotonic() + 180
        while time.monotonic() < deadline:
            result = self.appliance.ssh(
                "sudo -n sh -s -- " + shlex.quote(password),
                input_text=script,
                timeout=60,
                check=False,
            )
            last = (result.stdout or result.stderr).strip()
            parts = last.split()
            if result.returncode == 0 and len(parts) >= 3 and parts[0] == "200":
                try:
                    size = int(parts[2])
                except ValueError:
                    size = 0
                if size > 1024 and parts[1].startswith("image/jpeg"):
                    self.snapshot = {"status": parts[0], "content_type": parts[1], "bytes": size}
                    self.appliance.ssh("sudo -n rm -f /tmp/bittern-snapshot.jpg", timeout=15)
                    return self.snapshot
            time.sleep(3)
        raise HarnessError(f"snapshot from {base} did not return a JPEG: {last}")

    def assert_mode(self, path: str, expected: str) -> None:
        mode = self.appliance.ssh(f"sudo -n stat -c '%a' {shlex.quote(path)}", timeout=30).stdout.strip()
        if mode != expected:
            raise HarnessError(f"{path} has mode {mode}, expected {expected}")
        self.asserted_files.append(f"{path} mode {mode}")

    def guest_file(self, path: str, *, secret: bool = False) -> str:
        content = self.appliance.ssh(f"sudo -n cat {shlex.quote(path)}", timeout=30).stdout
        if not secret:
            self.capture(f"file-{Path(path).name}.txt", content)
        return content

    def assert_rendered_files(self) -> None:
        generated = [
            "/etc/nftables.conf",
            "/etc/dnsmasq.d/bittern.conf",
            "/etc/chrony/chrony.conf",
            "/etc/systemd/network/10-lan0.network",
            "/etc/systemd/network/10-cam0.network",
            "/etc/systemd/network/10-lan0.link",
            "/etc/systemd/network/10-cam0.link",
        ]
        for path in generated:
            content = self.guest_file(path)
            if not content.startswith("# Generated by Bittern from appliance.yaml; do not edit."):
                raise HarnessError(f"{path} is not renderer output")
            self.asserted_files.append(path)

        nft = self.guest_file("/etc/nftables.conf")
        if "table inet bittern" not in nft:
            raise HarnessError("/etc/nftables.conf does not define table inet bittern")
        if 'iifname "lan0" tcp dport 443 accept' not in nft:
            raise HarnessError("/etc/nftables.conf does not accept the Bittern UI on lan0 tcp 443")
        if "bittern-harness-" in nft:
            raise HarnessError("the rendered nftables file contains a harness management rule")

        for role in ("lan0", "cam0"):
            network = self.guest_file(f"/etc/systemd/network/10-{role}.network")
            link = self.guest_file(f"/etc/systemd/network/10-{role}.link")
            mac = self.appliance.ssh(f"cat /sys/class/net/{role}/address", timeout=15).stdout.strip()
            if f"Name={role}" not in network:
                raise HarnessError(f"10-{role}.network does not set Name={role}")
            if "[Match]" not in link or f"Name={role}" not in link or mac not in link:
                raise HarnessError(f"10-{role}.link does not match {role} MAC {mac}")
        cam_network = self.guest_file("/etc/systemd/network/10-cam0.network")
        if "10.77.0.1/24" not in cam_network:
            raise HarnessError("10-cam0.network does not carry the camera network address")

        for path, expected in (
            (
                "/etc/systemd/system/bittern-frigate.service",
                "ExecStart=/usr/bin/docker-compose --file /var/lib/bittern/compose/compose.yaml",
            ),
            ("/var/lib/bittern/frigate/config.yml", "camera_1"),
            ("/var/lib/bittern/frigate/config.yml", "enabled: true"),
            ("/var/lib/bittern/frigate/config.yml", "FRIGATE_CAMERA_CAMERA_1_RTSP_URL"),
            ("/var/lib/bittern/compose/compose.yaml", "ghcr.io/blakeblackshear/frigate:0.18.2@sha256:"),
            ("/var/lib/bittern/compose/compose.yaml", "/var/lib/bittern/media:/media/frigate"),
            ("/var/lib/bittern/compose/compose.yaml", "/var/lib/bittern/frigate/cameras.env"),
            ("/var/lib/bittern/compose/compose.yaml", "/var/lib/bittern/frigate:/config"),
            (
                "/var/lib/bittern/compose/compose.yaml",
                "/var/lib/bittern/frigate/config.yml:/config/config.yml:ro",
            ),
        ):
            content = self.guest_file(path)
            if expected not in content:
                raise HarnessError(f"{path} does not contain {expected!r}")
            self.asserted_files.append(path)
        if self.camera_url in self.guest_file("/var/lib/bittern/frigate/config.yml"):
            raise HarnessError("the rendered Frigate config contains the literal camera URL")
        self.assert_mode("/var/lib/bittern/frigate/cameras.env", "600")
        env = self.guest_file("/var/lib/bittern/frigate/cameras.env", secret=True)
        if "FRIGATE_CAMERA_CAMERA_1_RTSP_URL=" not in env:
            raise HarnessError("the camera env file does not name the expected variable")
        self.asserted_files.append("/var/lib/bittern/frigate/cameras.env")

        certificate = self.guest_file("/etc/bittern/tls/server.crt")
        if "-----BEGIN CERTIFICATE-----" not in certificate:
            raise HarnessError("the TLS certificate is not a PEM certificate")
        self.assert_mode("/etc/bittern/tls/server.crt", "644")
        parsed = run_command(
            ["openssl", "x509", "-noout", "-subject", "-ext", "subjectAltName", "-fingerprint", "-sha256"],
            input=certificate,
        )
        if parsed.returncode != 0:
            raise HarnessError(f"openssl could not parse the certificate: {parsed.stderr.strip()}")
        if "CN=bittern" not in parsed.stdout and "CN = bittern" not in parsed.stdout:
            raise HarnessError(f"certificate subject is not hostname bittern:\n{parsed.stdout}")
        if "IP Address:" not in parsed.stdout:
            raise HarnessError(f"certificate has no LAN IP SAN:\n{parsed.stdout}")
        self.asserted_files.append("/etc/bittern/tls/server.crt")

        self.assert_mode("/etc/bittern/secrets.yaml", "600")
        secrets = self.guest_file(FIRSTBOOT_SECRETS, secret=True)
        if re.search(r"^setup_token:\s*\S", secrets, re.MULTILINE):
            raise HarnessError("setup_token is still present in secrets.yaml after setup")
        if re.search(r"^setup_short_code_(hash|expires_at):\s*\S", secrets, re.MULTILINE):
            raise HarnessError("short-code material is still present in secrets.yaml after setup")
        if "tls:" not in secrets or "private_key_pem:" not in secrets:
            raise HarnessError("secrets.yaml does not carry the TLS private key")
        self.asserted_files.append("/etc/bittern/secrets.yaml")

    def assert_frigate_mounts(self) -> None:
        raw = self.appliance.ssh(
            "sudo -n docker inspect --format '{{json .Mounts}}' frigate", timeout=30
        ).stdout
        try:
            mounts = json.loads(raw)
        except json.JSONDecodeError as exc:
            raise HarnessError(f"cannot parse the Frigate mounts: {raw!r}") from exc
        by_destination = {
            mount.get("Destination"): mount
            for mount in mounts
            if isinstance(mount, dict) and mount.get("Destination")
        }
        directory = by_destination.get("/config", {})
        managed_file = by_destination.get("/config/config.yml", {})
        if directory.get("Source") != "/var/lib/bittern/frigate" or directory.get("RW") is not True:
            raise HarnessError(f"Frigate /config is not the writable managed directory: {directory}")
        if (
            managed_file.get("Source") != "/var/lib/bittern/frigate/config.yml"
            or managed_file.get("RW") is not False
        ):
            raise HarnessError(f"Frigate /config/config.yml is not the read-only managed file: {managed_file}")
        self.asserted_files.append("frigate /config rw, /config/config.yml ro")

    def assert_cleanup(self) -> str:
        result = self.appliance.ssh(
            f"sudo -n sh -c 'test ! -e {FIRSTBOOT_TOKEN_PATH} && echo removed || echo present'",
            timeout=30,
        ).stdout.strip()
        if result != "removed":
            raise HarnessError("the setup token file still exists after setup")
        auth_mode = self.appliance.ssh(
            "sudo -n stat -c '%a' /var/lib/bittern/auth.json", timeout=30
        ).stdout.strip()
        if auth_mode != "600":
            raise HarnessError(f"auth.json has mode {auth_mode}, expected 600")
        return "setup token file is gone and auth.json is mode 0600"

    def assert_logs_clean(self) -> str:
        forbidden = {
            "setup token": self.token,
            "short code": self.short_code,
            "administrator password": self.admin_password,
            "camera URL": self.camera_url,
        }
        journal = self.appliance.ssh(
            "sudo -n journalctl --no-pager -o cat", timeout=120
        ).stdout
        serial = ""
        if self.appliance.serial_log.exists():
            serial = self.appliance.serial_log.read_text(encoding="utf-8", errors="replace")
        captured = "".join(self.captured)
        status_text = json.dumps(self.status, sort_keys=True) if self.status else ""
        for label, value in forbidden.items():
            if not value:
                continue
            if value in journal:
                raise HarnessError(f"the systemd journal contains the {label}")
            if value in serial:
                raise HarnessError(f"the appliance serial log contains the {label}")
            if value in captured:
                raise HarnessError(f"a captured harness log contains the {label}")
            if value in status_text:
                raise HarnessError(f"the status document contains the {label}")
        (self.log_dir / "journal.txt").write_text(journal, encoding="utf-8")
        (self.log_dir / "harness-transcript.txt").write_text(captured, encoding="utf-8")
        return (
            "journal, serial log, harness transcript and status contain no token, "
            "short code, admin password or camera URL"
        )

    def capture_failure(self) -> None:
        parts: list[str] = []
        commands = (
            "sudo -n systemctl status bittern-first-boot.service bitternd.service "
            "bittern-image-prewarm.service bittern-frigate.service "
            "systemd-networkd.service dnsmasq.service "
            "chrony.service --no-pager -l",
            "sudo -n journalctl -u bittern-first-boot.service -u bitternd.service "
            "-u bittern-image-prewarm.service -u bittern-frigate.service "
            "-u systemd-networkd.service -u dnsmasq.service "
            "-u chrony.service --no-pager -o cat -n 400",
            "sudo -n sh -c 'test -s /etc/resolv.conf && cat /etc/resolv.conf; "
            "resolvectl status; getent hosts deb.debian.org'",
            "sudo -n docker ps -a --format '{{.Names}} {{.Status}}'",
            "sudo -n sh -c 'for f in /var/lib/bittern/generations/*/manifest.json; do "
            "echo ===$f===; python3 -c \"import json,sys; d=json.load(open(sys.argv[1])); "
            "print(json.dumps({k:d.get(k) for k in (\\\"status\\\",\\\"checks\\\",\\\"health\\\")}, indent=2))\" \"$f\"; done'",
        )
        for command in commands:
            try:
                result = self.appliance.ssh(command, timeout=60, check=False)
            except Exception as exc:  # diagnostics must never hide the real failure
                parts.append(f"{command}: {type(exc).__name__}: {exc}")
                continue
            parts.append(f"$ {command}\n{result.stdout}{result.stderr}")
        text = self.redact("\n".join(parts))
        (self.log_dir / "failure-guest.txt").write_text(text, encoding="utf-8")

    def guest_failure_detail(self, unit: str) -> str:
        parts = [
            self.appliance.ssh(
                f"sudo -n systemctl status {unit} --no-pager -l", timeout=60, check=False
            ).stdout,
            self.appliance.ssh(
                f"sudo -n journalctl --no-pager -o cat -u {unit} -n 200", timeout=60, check=False
            ).stdout,
        ]
        detail = "\n".join(part for part in parts if part)
        (self.log_dir / "failure.txt").write_text(detail, encoding="utf-8")
        return f"failure detail: {self.log_dir / 'failure.txt'}"

    def write_summary(self, status: str, error: str) -> None:
        summary = {
            "status": status,
            "failed_phase": self.failed_phase,
            "error": error,
            "accelerator": self.accelerator,
            "phases": self.phases,
            "marker": self.marker,
            "rendered_tree": self.rendered_tree,
            "snapshot": self.snapshot,
            "asserted_files": sorted(set(self.asserted_files)),
            "logs": str(self.log_dir),
        }
        (self.log_dir / "summary.json").write_text(
            json.dumps(summary, indent=2, sort_keys=True) + "\n", encoding="utf-8"
        )

class StockNameScenario(FirstbootScenario):
    """First boot on stock interface names, proving the .link rename path."""

    def __init__(
        self,
        appliance: Vm,
        camera: Vm,
        config: dict[str, Any],
        accelerator: str,
        *,
        scenario_name: str = "firstboot-stock",
    ) -> None:
        super().__init__(
            appliance, camera, config, accelerator, scenario_name=scenario_name
        )
        self.lan_stock = ""
        self.cam_stock = ""

    def run(self) -> int:
        self.run_phase("guests", self.ensure_guests)
        self.run_phase("stock-names", self.rename_to_stock_names)
        self.run_phase("install", self.install_components)
        self.run_phase("first-boot", self.run_first_boot)
        self.run_phase("stock-name-checks", self.assert_stock_name_result)
        self.run_phase("logs", self.assert_logs_clean)
        self.write_summary("pass", "")
        self.note(f"stock-name firstboot scenario passed; logs: {self.log_dir}")
        return 0

    def link_names(self) -> set[str]:
        output = self.appliance.ssh("ip -br link", timeout=30).stdout
        return {line.split()[0] for line in output.splitlines() if line.strip()}

    def predictable_name(self, role: str) -> str:
        result = self.appliance.ssh(
            f"udevadm test-builtin net_id /sys/class/net/{role} 2>/dev/null",
            timeout=30,
        )
        names = re.findall(r"^ID_NET_NAME_PATH=(\S+)$", result.stdout, re.MULTILINE)
        if len(names) != 1 or not re.fullmatch(r"[A-Za-z0-9_.-]+", names[0]):
            raise HarnessError(
                f"cannot derive the stock name for {role} from udev:\n{result.stdout}"
            )
        return names[0]

    def rename_to_stock_names(self) -> str:
        self.lan_stock = self.predictable_name("lan0")
        self.cam_stock = self.predictable_name("cam0")
        if self.lan_stock in ("lan0", "cam0") or self.cam_stock in ("lan0", "cam0"):
            raise HarnessError(
                f"udev returned role names instead of stock names: "
                f"{self.lan_stock}, {self.cam_stock}"
            )
        if self.lan_stock == self.cam_stock:
            raise HarnessError(f"both NICs predict the same stock name: {self.lan_stock}")
        script = f"""set -eu
rm -f /etc/netplan/50-cloud-init.yaml
rm -f /run/systemd/network/10-netplan-*.link /run/systemd/network/10-netplan-*.network
rm -f /etc/systemd/network/90-bittern-harness-stock-*.network
cat > /etc/systemd/network/90-bittern-harness-stock-lan.network <<'EOF'
[Match]
Name={self.lan_stock}

[Network]
DHCP=ipv4
IPv6AcceptRA=no
EOF
cat > /etc/systemd/network/90-bittern-harness-stock-cam.network <<'EOF'
[Match]
Name={self.cam_stock}

[Network]
ConfigureWithoutCarrier=yes
Address=10.77.0.1/24
DHCP=no
IPv6AcceptRA=no
LinkLocalAddressing=no
EOF
ip link set lan0 down
ip link set lan0 name {self.lan_stock}
ip link set {self.lan_stock} up
ip link set cam0 down
ip link set cam0 name {self.cam_stock}
ip link set {self.cam_stock} up
systemctl restart systemd-networkd.service
systemctl restart systemd-resolved.service || true
"""
        self.appliance.ssh("sudo -n sh -s", input_text=script, timeout=180)
        self.house_iface = self.lan_stock
        self.camera_iface = self.cam_stock

        deadline = time.monotonic() + 60
        last = "no attempt"
        while time.monotonic() < deadline:
            result = self.appliance.ssh(
                f"ip -4 -o addr show dev {self.lan_stock}; ip route show default",
                timeout=30,
                check=False,
            )
            output = result.stdout + result.stderr
            if (
                result.returncode == 0
                and "inet " in result.stdout
                and f"dev {self.lan_stock}" in result.stdout
            ):
                break
            last = output
            time.sleep(1)
        else:
            raise HarnessError(
                f"stock LAN interface {self.lan_stock} did not get a DHCP address:\n{last}"
            )

        deadline = time.monotonic() + 30
        while time.monotonic() < deadline:
            result = self.appliance.ssh(
                f"resolvectl dns {self.lan_stock}", timeout=30, check=False
            )
            address = re.search(r"\b(?:\d{1,3}\.){3}\d{1,3}\b", result.stdout)
            if address:
                self.resolver_dns = address.group(0)
                break
            time.sleep(1)
        if not self.resolver_dns:
            raise HarnessError(
                f"no DHCP DNS on {self.lan_stock} after the networkd restart"
            )

        names = self.link_names()
        if "lan0" in names or "cam0" in names:
            raise HarnessError(f"role names still present before first boot: {sorted(names)}")
        if self.lan_stock not in names or self.cam_stock not in names:
            raise HarnessError(
                f"stock interface names {self.lan_stock}/{self.cam_stock} are missing: "
                f"{sorted(names)}"
            )
        try:
            self.appliance.set_link("bittern-cam0", False)
        except HarnessError as exc:
            raise HarnessError(
                "cannot take the camera link down for the stock-name proposal: "
                f"guest was not started with the harness device id ({exc})"
            ) from exc
        time.sleep(2)
        carrier = self.appliance.ssh(
            f"cat /sys/class/net/{self.cam_stock}/carrier", timeout=30
        ).stdout.strip()
        if carrier != "0":
            raise HarnessError(
                f"{self.cam_stock} carrier is {carrier!r} before first boot, expected 0"
            )
        # The generated .network files must be the only configuration networkd
        # reads after the rename. The temporary harness files have done their job.
        self.appliance.ssh(
            "sudo -n rm -f /etc/systemd/network/90-bittern-harness-stock-*.network",
            timeout=30,
        )
        evidence = self.appliance.ssh(
            f"ip -br link; ip -4 -o addr show; ip route show default; "
            f"resolvectl dns {self.lan_stock}; "
            f"ls -1 /etc/systemd/network /etc/netplan 2>/dev/null || true",
            timeout=30,
        ).stdout
        (self.log_dir / "stockname-before.txt").write_text(
            self.redact(evidence), encoding="utf-8"
        )
        self.capture("stockname-before.txt", evidence)
        return (
            f"renamed lan0/cam0 to stock names {self.lan_stock}/{self.cam_stock}; "
            f"networkd restarted, {self.lan_stock} has DHCP and DNS "
            f"{self.resolver_dns}, {self.cam_stock} carrier is down"
        )

    def assert_stock_name_result(self) -> str:
        names = self.link_names()
        if "lan0" not in names or "cam0" not in names:
            raise HarnessError(
                f"first boot did not rename the stock NICs to lan0/cam0: {sorted(names)}"
            )
        if self.lan_stock in names or self.cam_stock in names:
            raise HarnessError(
                f"stock names {self.lan_stock}/{self.cam_stock} survived first boot: "
                f"{sorted(names)}"
            )
        lan = self.appliance.ssh("ip -4 -o addr show dev lan0", timeout=30).stdout
        cam = self.appliance.ssh("ip -4 -o addr show dev cam0", timeout=30).stdout
        route = self.appliance.ssh("ip route show default", timeout=30).stdout
        carrier = self.appliance.ssh(
            "cat /sys/class/net/cam0/carrier", timeout=30
        ).stdout.strip()
        network = (
            f"$ ip -4 -o addr show dev lan0\n{lan}"
            f"$ ip -4 -o addr show dev cam0\n{cam}"
            f"$ ip route show default\n{route}"
            f"$ cam0 carrier\n{carrier}\n"
        )
        if "inet " not in lan:
            raise HarnessError(f"lan0 has no IPv4 address after first boot:\n{network}")
        if "10.77.0.1/24" not in cam:
            raise HarnessError(f"cam0 is missing 10.77.0.1/24 after first boot:\n{network}")
        if "dev lan0" not in route:
            raise HarnessError(f"the default route is not on lan0 after first boot:\n{network}")
        if carrier != "0":
            raise HarnessError(f"cam0 carrier is not down after first boot:\n{network}")
        journal = self.appliance.ssh(
            "sudo -n journalctl -u bittern-first-boot.service -b --no-pager "
            "-o cat -n 500",
            timeout=60,
        ).stdout
        for line in (
            "apply: apply udev:wait",
            "apply: apply restart:systemd-networkd.service",
            "health lan0-address: passed",
        ):
            if line not in journal:
                raise HarnessError(
                    f"first-boot journal is missing {line!r}:\n{journal[-4000:]}"
                )
        self.appliance.ssh("sudo -n test ! -e /var/lib/bittern/journal.json", timeout=30)
        generation = self.marker.get("generation", 0)
        manifest_raw = self.appliance.ssh(
            f"sudo -n cat /var/lib/bittern/generations/{generation}/manifest.json",
            timeout=30,
        ).stdout
        manifest = json.loads(manifest_raw)
        health = {
            item.get("name"): item.get("status")
            for item in manifest.get("health", [])
            if isinstance(item, dict)
        }
        for name in ("lan0-link", "lan0-address", "cam0-address"):
            if health.get(name) != "passed":
                raise HarnessError(
                    f"{name} did not pass after the stock-name rename: "
                    f"{json.dumps(health, sort_keys=True)}"
                )
        evidence = (
            "$ ip -br link\n"
            + self.appliance.ssh("ip -br link", timeout=30).stdout
            + f"$ addresses and route\n{network}\n"
            + f"$ journal (rename and health)\n{journal[-4000:]}\n"
            + f"health={json.dumps(health, sort_keys=True)}\n"
        )
        (self.log_dir / "stockname-after.txt").write_text(
            self.redact(evidence), encoding="utf-8"
        )
        self.capture("stockname-after.txt", evidence)
        return (
            f"renamed {self.lan_stock}/{self.cam_stock} to lan0/cam0, networkd "
            f"restarted, lan0 has DHCP and cam0 has 10.77.0.1/24; "
            f"health lan0-link, lan0-address and cam0-address passed"
        )


class CleanNetworkScenario(StockNameScenario):
    """Stock-name first boot from a clean state with no network or resolver.

    This is the Level 1 sequencing regression: cleanup must not take away the
    generated network before the install can replace it, and install.sh must
    bootstrap the management link and a local DHCP resolver before apt.
    """

    def __init__(
        self, appliance: Vm, camera: Vm, config: dict[str, Any], accelerator: str
    ) -> None:
        super().__init__(
            appliance,
            camera,
            config,
            accelerator,
            scenario_name="firstboot-clean",
        )

    def run(self) -> int:
        self.run_phase("guests", self.ensure_guests)
        self.run_phase("stock-names", self.rename_to_stock_names)
        self.run_phase("clean-packages", self.strip_baseline_packages)
        self.run_phase("clean-network", self.remove_clean_network_state)
        self.run_phase("install", self.install_clean_network)
        self.run_phase("first-boot", self.reboot_into_first_boot)
        self.run_phase("reboot-units", self.assert_units_after_reboot)
        self.run_phase("generated-network", self.assert_generated_network)
        self.run_phase("stock-name-checks", self.assert_stock_name_result)
        self.run_phase("resolver", self.assert_clean_network_dns)
        self.run_phase("logs", self.assert_logs_clean)
        self.write_summary("pass", "")
        self.note(f"clean-network firstboot scenario passed; logs: {self.log_dir}")
        return 0

    def blackhole_external_dns(self) -> None:
        if not self.resolver_dns:
            raise HarnessError("the local DHCP resolver is not known; cannot black-hole external DNS")
        script = f"""set -eu
nft delete table inet bittern-clean 2>/dev/null || true
nft add table inet bittern-clean
nft 'add chain inet bittern-clean output {{ type filter hook output priority -10; policy accept; }}'
nft add rule inet bittern-clean output oifname "lo" accept
nft add rule inet bittern-clean output ip daddr {self.resolver_dns} udp dport 53 accept
nft add rule inet bittern-clean output ip daddr {self.resolver_dns} tcp dport 53 accept
nft add rule inet bittern-clean output udp dport 53 drop
nft add rule inet bittern-clean output tcp dport 53 drop
"""
        self.appliance.ssh("sudo -n sh -s", input_text=script, timeout=60)

    def remove_clean_network_state(self) -> str:
        script = f"""set -eu
rm -f /etc/netplan/*.yaml
rm -rf {CLEAN_NETWORK_DIR}
rm -rf {CLEAN_RUN_NETWORK_DIR}
rm -f /etc/resolv.conf
: > /etc/resolv.conf
chmod 0644 /etc/resolv.conf
ip -4 addr flush dev {self.cam_stock} scope global || true
ip link set dev {self.cam_stock} down || true
"""
        self.appliance.ssh("sudo -n sh -s", input_text=script, timeout=120)
        baseline_states = self.disable_baseline_enablement()
        # Keep the management lease alive until the detached install unit
        # flushes it. SSH needs that address to start the installer.
        self.blackhole_external_dns()

        checks = (
            f"sudo -n sh -c 'test ! -d {CLEAN_NETWORK_DIR} || test -z \"$(ls -A {CLEAN_NETWORK_DIR})\"'",
            f"sudo -n sh -c 'test ! -d {CLEAN_RUN_NETWORK_DIR} || test -z \"$(ls -A {CLEAN_RUN_NETWORK_DIR})\"'",
            "sudo -n sh -c 'test -f /etc/resolv.conf && test ! -s /etc/resolv.conf'",
            "sudo -n sh -c '! command -v resolvectl >/dev/null 2>&1'",
            "sudo -n sh -c '! systemctl is-active --quiet systemd-resolved.service'",
            "sudo -n nft list table inet bittern-clean >/dev/null",
        )
        for check in checks:
            self.appliance.ssh(check, timeout=30)
        unresolved = self.appliance.ssh(
            "getent hosts deb.debian.org", timeout=20, check=False
        )
        if unresolved.returncode == 0:
            raise HarnessError(
                "the clean state still resolves deb.debian.org with an empty "
                f"/etc/resolv.conf: {unresolved.stdout}{unresolved.stderr}"
            )

        evidence = self.appliance.ssh(
            "ip -br link; ip -4 -o addr show; ip route show default; "
            f"find {CLEAN_NETWORK_DIR} {CLEAN_RUN_NETWORK_DIR} -maxdepth 1 -type f 2>/dev/null || true; "
            "stat -c '%s bytes %n' /etc/resolv.conf; "
            "sudo -n nft list table inet bittern-clean",
            timeout=30,
        ).stdout
        (self.log_dir / "clean-network-before.txt").write_text(
            self.redact(evidence), encoding="utf-8"
        )
        self.capture("clean-network-before.txt", evidence)
        return (
            "removed every .network and .link file, emptied /etc/resolv.conf, "
            f"removed systemd-resolved, left every baseline unit disabled "
            f"({json.dumps(baseline_states, sort_keys=True)}), and black-holed "
            f"every DNS server except the local DHCP resolver {self.resolver_dns}; "
            f"stock names {self.lan_stock}/{self.cam_stock} remain"
        )

    def install_clean_network(self) -> str:
        self.stage_installer()
        # Drop the stale lease inside the detached unit, not over the SSH
        # session that starts it. The installer must reacquire DHCP from the
        # clean state, including the resolver, before apt runs.
        script = f"""#!/bin/sh
set -eu
ip -4 addr flush dev {self.lan_stock} scope global || true
ip -4 route flush default dev {self.lan_stock} || true
ip link set dev {self.lan_stock} up
cd {shlex.quote(self.installer_path)}
./install.sh --dry-run
./install.sh
"""
        self.appliance.write_remote_file(CLEAN_INSTALL_SCRIPT, script, mode="0755")
        launch = self.appliance.ssh(
            f"sudo -n systemctl reset-failed {CLEAN_INSTALL_UNIT} 2>/dev/null || true; "
            f"sudo -n systemd-run --unit={CLEAN_INSTALL_UNIT_NAME} "
            f"--description='Bittern clean install' /bin/sh {CLEAN_INSTALL_SCRIPT}",
            timeout=30,
            check=False,
        )
        self.capture("clean-install-launch.txt", launch.stdout + launch.stderr)

        deadline = time.monotonic() + 900
        state = "unknown"
        result = "unknown"
        missing = 0
        last_error = "no attempt"
        while time.monotonic() < deadline:
            try:
                probe = self.appliance.ssh(
                    f"systemctl show -p ActiveState -p Result {CLEAN_INSTALL_UNIT}",
                    timeout=15,
                    check=False,
                    retries=1,
                )
            except Exception as exc:
                last_error = str(exc)
                time.sleep(2)
                continue
            values: dict[str, str] = {}
            for line in probe.stdout.splitlines():
                key, separator, value = line.partition("=")
                if separator:
                    values[key.strip()] = value.strip()
            if probe.returncode != 0:
                missing += 1
                if missing >= 15:
                    raise HarnessError(
                        "the clean install unit was not created: "
                        f"{launch.stdout}{launch.stderr}{probe.stdout}{probe.stderr}"
                    )
                time.sleep(2)
                continue
            state = values.get("ActiveState", "unknown")
            result = values.get("Result", "unknown")
            if result == "failed":
                break
            if state == "inactive" and result == "success":
                break
            time.sleep(2)

        journal = self.appliance.ssh(
            f"sudo -n journalctl -u {CLEAN_INSTALL_UNIT} -b --no-pager -o cat",
            timeout=120,
            check=False,
        ).stdout
        self.capture("installer-output.txt", journal)
        if state != "inactive" or result != "success":
            (self.log_dir / "clean-install-journal.txt").write_text(
                self.redact(journal), encoding="utf-8"
            )
            raise HarnessError(
                "clean install unit did not succeed: "
                f"ActiveState={state!r} Result={result!r} {last_error}; "
                f"see {self.log_dir / 'clean-install-journal.txt'}"
            )

        required = (
            "network: management interface ",
            "network: no IPv4 address on ",
            f"network: wrote {CLEAN_RUN_NETWORK_DIR}/90-bittern-bootstrap.network",
            "network: seeded /etc/resolv.conf from the ",
            "network: getent hosts deb.debian.org resolved before apt",
            "network: install network bootstrap ready",
        )
        absent = [line for line in required if line not in journal]
        if absent:
            (self.log_dir / "clean-install-journal.txt").write_text(
                self.redact(journal), encoding="utf-8"
            )
            raise HarnessError(
                "the installer did not report the clean-network bootstrap: "
                + "; ".join(absent)
            )
        self.appliance.ssh(
            f"ip -4 -o addr show dev {self.lan_stock} | grep -q ' inet '", timeout=30
        )
        self.assert_installer_result(journal)
        self.appliance.ssh(f"rm -f {shlex.quote(self.installer_archive)}", timeout=30)
        self.capture("clean-network-install.txt", journal)
        return (
            "install.sh found the management link, acquired DHCP through "
            "systemd-networkd, seeded /etc/resolv.conf from the local DHCP "
            "resolver, resolved deb.debian.org, and completed apt"
        )

    def reboot_into_first_boot(self) -> str:
        # The runbook reboots after install, so /run is cleared and the
        # enabled first-boot unit writes the generated network at boot. This is
        # the product sequence: install bootstraps the install session, then
        # first boot owns the persistent network files.
        enabled = self.appliance.ssh(
            "systemctl is-enabled bittern-first-boot.service", timeout=30, check=False
        ).stdout.strip()
        if enabled != "enabled":
            raise HarnessError(
                f"bittern-first-boot.service is {enabled!r} after install, expected enabled"
            )
        self.appliance.ssh("sudo -n test ! -e /var/lib/bittern/first-boot.json", timeout=30)
        # Keep the camera segment unplugged across the reboot so detection sees
        # exactly one live wired link and can confirm the lan0/cam0 proposal.
        self.appliance.set_link("bittern-cam0", False)
        self.appliance.ssh("sudo -n systemctl reboot", timeout=30, check=False)
        time.sleep(5)
        self.appliance.set_link("bittern-cam0", False)
        self.wait_for_ssh(self.appliance, 300)

        deadline = time.monotonic() + 300
        last = "no attempt"
        while time.monotonic() < deadline:
            probe = self.appliance.ssh(
                f"sudo -n test -s {FIRSTBOOT_MARKER} && sudo -n cat {FIRSTBOOT_MARKER}",
                timeout=30,
                check=False,
            )
            if probe.returncode == 0 and probe.stdout.strip():
                break
            last = (probe.stdout or probe.stderr).strip()
            time.sleep(2)
        else:
            detail = self.guest_failure_detail("bittern-first-boot.service")
            raise HarnessError(
                f"first boot after reboot did not write the marker: {last}\n{detail}"
            )
        self.marker = json.loads(probe.stdout)
        if self.marker.get("generation", 0) < 1:
            raise HarnessError(f"first-boot marker has no applied generation: {probe.stdout}")
        if not self.marker.get("nic_roles_confirmed"):
            # A clean boot has no live management address before first boot, so
            # the proposal can be deterministic but unconfirmed. The marker must
            # say why, and the stock-name checks below still prove the mapping.
            notes = " ".join(str(note) for note in self.marker.get("notes", []))
            if "NIC roles are unconfirmed" not in notes:
                raise HarnessError(
                    f"first boot left NIC roles unconfirmed without a note: {probe.stdout}"
                )
        status = self.appliance.ssh(
            "sudo -n systemctl show -p Result --value bittern-first-boot.service",
            timeout=30,
        ).stdout.strip()
        if status != "success":
            detail = self.guest_failure_detail("bittern-first-boot.service")
            raise HarnessError(
                f"bittern-first-boot.service Result is {status!r}\n{detail}"
            )
        carrier = self.appliance.ssh(
            "cat /sys/class/net/cam0/carrier", timeout=30
        ).stdout.strip()
        if carrier != "0":
            raise HarnessError(f"cam0 carrier is {carrier!r} after first boot, expected 0")
        address = self.appliance.ssh("ip -4 -o addr show dev cam0", timeout=30).stdout
        if "10.77.0.1/24" not in address:
            raise HarnessError(
                "first boot committed with cam0 down but the camera address is missing:\n"
                f"{address}"
            )
        units = self.appliance.ssh(
            "systemctl is-active nftables.service dnsmasq.service chrony.service",
            timeout=30,
            check=False,
        )
        if units.returncode != 0 or units.stdout.split() != ["active", "active", "active"]:
            raise HarnessError(
                "nftables, dnsmasq or chrony did not stay active after reboot:\n"
                f"{units.stdout}{units.stderr}"
            )
        nft_enabled = self.appliance.ssh(
            "systemctl is-enabled nftables.service", timeout=30, check=False
        ).stdout.strip()
        if nft_enabled != "enabled":
            raise HarnessError(
                f"nftables.service is not enabled after reboot: {nft_enabled!r}"
            )
        evidence = (
            f"$ systemctl is-enabled bittern-first-boot.service\n{enabled}\n"
            f"$ systemctl show -p Result --value bittern-first-boot.service\n{status}\n"
            f"$ cat {FIRSTBOOT_MARKER}\n{probe.stdout}"
            f"$ cam0 carrier\n{carrier}\n"
            f"$ ip -4 -o addr show dev cam0\n{address}"
            f"$ systemctl is-active nftables.service dnsmasq.service chrony.service\n"
            f"{units.stdout}"
            f"$ systemctl is-enabled nftables.service\n{nft_enabled}\n"
        )
        (self.log_dir / "clean-network-firstboot.txt").write_text(
            self.redact(evidence), encoding="utf-8"
        )
        self.capture("clean-network-firstboot.txt", evidence)
        confirmed = "confirmed" if self.marker.get("nic_roles_confirmed") else "deterministic"
        return (
            f"rebooted after install; first boot wrote marker generation "
            f"{self.marker['generation']} with {confirmed} NIC roles, "
            "cam0 down with 10.77.0.1/24, and nftables, dnsmasq and chrony active"
        )

    def assert_generated_network(self) -> str:
        for name in CLEAN_NETWORK_FILES:
            self.appliance.ssh(f"sudo -n test -s {CLEAN_NETWORK_DIR}/{name}", timeout=30)
            self.appliance.ssh(
                f"sudo -n grep -q 'Generated by Bittern' {CLEAN_NETWORK_DIR}/{name}",
                timeout=30,
            )
        self.appliance.ssh(
            f"sudo -n test ! -e {CLEAN_NETWORK_DIR}/90-bittern-bootstrap.network",
            timeout=30,
        )
        evidence = self.appliance.ssh(
            f"ls -l {CLEAN_NETWORK_DIR}; "
            f"for f in {' '.join(f'{CLEAN_NETWORK_DIR}/{name}' for name in CLEAN_NETWORK_FILES)}; do "
            'echo "== $f =="; sudo -n sed -n "1,12p" "$f"; done',
            timeout=30,
        ).stdout
        (self.log_dir / "clean-network-generated.txt").write_text(
            self.redact(evidence), encoding="utf-8"
        )
        self.capture("clean-network-generated.txt", evidence)
        return (
            "first boot wrote 10-lan0.link, 10-lan0.network, 10-cam0.link and "
            "10-cam0.network from the renderer; the transient /run bootstrap "
            "file was not written under /etc"
        )

    def assert_clean_network_dns(self) -> str:
        self.blackhole_external_dns()
        deadline = time.monotonic() + 60
        resolv = status = hosts = None
        last = "no attempt"
        while time.monotonic() < deadline:
            resolv = self.appliance.ssh(
                f"test -s {FIRSTBOOT_RESOLV_CONF} && cat {FIRSTBOOT_RESOLV_CONF}",
                timeout=30,
                check=False,
            )
            status = self.appliance.ssh(
                "sudo -n resolvectl status", timeout=30, check=False
            )
            hosts = self.appliance.ssh(
                "getent hosts deb.debian.org", timeout=30, check=False
            )
            if (
                resolv.returncode == 0
                and resolv.stdout.strip()
                and status.returncode == 0
                and self.resolver_dns in status.stdout
                and hosts.returncode == 0
                and hosts.stdout.strip()
            ):
                break
            last = (
                f"$ cat {FIRSTBOOT_RESOLV_CONF}\n{resolv.stdout}{resolv.stderr}"
                f"$ resolvectl status\n{status.stdout}{status.stderr}"
                f"$ getent hosts deb.debian.org\n{hosts.stdout}{hosts.stderr}"
            )
            time.sleep(2)
        else:
            raise HarnessError(
                "DNS did not recover from the clean network state with the local "
                "DHCP resolver and external DNS black-holed:\n" + last
            )
        assert resolv is not None and status is not None and hosts is not None
        target = self.appliance.ssh(
            f"readlink -f {FIRSTBOOT_RESOLV_CONF}", timeout=30
        ).stdout.strip()
        if target != FIRSTBOOT_RESOLVED_STUB:
            raise HarnessError(
                f"{FIRSTBOOT_RESOLV_CONF} resolves to {target!r}, expected {FIRSTBOOT_RESOLVED_STUB}"
            )
        enabled = self.appliance.ssh(
            f"systemctl is-enabled {FIRSTBOOT_RESOLVED_UNIT}", timeout=30, check=False
        ).stdout.strip()
        active = self.appliance.ssh(
            f"systemctl is-active {FIRSTBOOT_RESOLVED_UNIT}", timeout=30, check=False
        ).stdout.strip()
        if enabled != "enabled" or active != "active":
            raise HarnessError(
                f"{FIRSTBOOT_RESOLVED_UNIT} is {enabled!r}/{active!r}, expected enabled/active"
            )
        nft = self.appliance.ssh(
            "sudo -n nft list table inet bittern-clean", timeout=30, check=False
        ).stdout
        evidence = (
            f"$ readlink -f {FIRSTBOOT_RESOLV_CONF}\n{target}\n"
            f"$ cat {FIRSTBOOT_RESOLV_CONF}\n{resolv.stdout}"
            f"$ resolvectl status\n{status.stdout}"
            f"$ getent hosts deb.debian.org\n{hosts.stdout}"
            f"$ systemctl is-enabled {FIRSTBOOT_RESOLVED_UNIT}\n{enabled}\n"
            f"$ systemctl is-active {FIRSTBOOT_RESOLVED_UNIT}\n{active}\n"
            f"$ nft list table inet bittern-clean\n{nft}"
        )
        (self.log_dir / "clean-network-after.txt").write_text(
            self.redact(evidence), encoding="utf-8"
        )
        self.capture("clean-network-after.txt", evidence)
        return (
            f"{FIRSTBOOT_RESOLV_CONF} is {target}, resolvectl shows the local "
            f"DHCP resolver {self.resolver_dns}, getent resolves with every other "
            "DNS server black-holed, and systemd-resolved is enabled and active"
        )


class LateCameraScenario(StockNameScenario):
    """First boot from the boot transaction while cam0 does not exist yet.

    The stock-name baseline gives both NICs their udev names, so cam0 does not
    exist until the first apply writes the rendered .link and triggers udev.
    This scenario reboots after install and lets the enabled
    bittern-first-boot.service run from the boot transaction, as it does on an
    appliance, instead of starting the oneshot by hand on a fully booted
    system. The camera link stays down, the field state for an install with the
    camera NIC unplugged.

    The assertion that matters is the dependency order: the apply must restart
    systemd-networkd, wait for the rendered cam0 address, and only then restart
    dnsmasq. The boot transaction also starts dnsmasq with the distribution
    configuration, which fails beside systemd-resolved's port 53; Debian 13's
    dnsmasq unit additionally requires network-online.target. The apply's
    restart must not join either failure before networkd has applied the
    generated .network file.
    """

    def __init__(
        self,
        appliance: Vm,
        camera: Vm,
        config: dict[str, Any],
        accelerator: str,
    ) -> None:
        super().__init__(
            appliance,
            camera,
            config,
            accelerator,
            scenario_name="firstboot-late-camera",
        )

    def run(self) -> int:
        self.run_phase("guests", self.ensure_guests)
        self.run_phase("stock-names", self.rename_to_stock_names)
        self.run_phase("install", self.install_components)
        self.run_phase("boot-first-boot", self.run_first_boot_from_boot)
        self.run_phase("first-boot-result", self.assert_first_boot_from_boot)
        self.run_phase("logs", self.assert_logs_clean)
        self.write_summary("pass", "")
        self.note(f"late-camera firstboot scenario passed; logs: {self.log_dir}")
        return 0

    def run_first_boot_from_boot(self) -> str:
        self.appliance.ssh(f"sudo -n test ! -e {FIRSTBOOT_MARKER}")
        enabled = self.appliance.ssh(
            "systemctl is-enabled dnsmasq.service systemd-networkd.service "
            "systemd-resolved.service bittern-first-boot.service",
            timeout=30,
            check=False,
        )
        if enabled.returncode != 0 or enabled.stdout.split() != ["enabled"] * 4:
            raise HarnessError(
                "the installer did not enable the units the boot transaction needs:\n"
                f"$ systemctl is-enabled dnsmasq systemd-networkd systemd-resolved bittern-first-boot\n"
                f"{enabled.stdout}{enabled.stderr}"
            )
        # A field install has the camera NIC unplugged, so cam0 has no carrier.
        try:
            self.appliance.set_link("bittern-cam0", False)
        except HarnessError as exc:
            raise HarnessError(
                "cannot take the camera link down for the late-camera case; guest "
                f"was not started with the harness device id ({exc})"
            ) from exc
        time.sleep(2)
        carrier = self.appliance.ssh(
            f"cat /sys/class/net/{self.camera_iface}/carrier", timeout=30
        ).stdout.strip()
        if carrier != "0":
            raise HarnessError(
                f"{self.camera_iface} carrier is {carrier!r} before the reboot, expected 0"
            )
        # The point of this scenario: the camera interface does not exist yet.
        # The rendered .link rename during the apply is what creates it.
        exists = self.appliance.ssh(
            "test -e /sys/class/net/cam0", timeout=30, check=False
        )
        if exists.returncode == 0:
            raise HarnessError(
                "cam0 already exists before the first apply (the NIC is "
                f"{self.camera_iface}); the late-rename case is not exercised"
            )
        # The baseline's cloud-init netplan .link files would rename the NICs
        # back to lan0/cam0 at udev time, before the apply. Remove them so the
        # stock names hold at boot and only the rendered .link files create the
        # logical names, which is the field path.
        self.appliance.ssh(
            "sudo -n rm -f /etc/systemd/network/*.link /run/systemd/network/*.link",
            timeout=30,
        )
        # Do not start dnsmasq, networkd or the oneshot: the reboot is the only
        # trigger. A harness start would hide the boot-transaction ordering.
        self.appliance.ssh("sudo -n systemctl reboot", timeout=30, check=False)
        time.sleep(5)
        self.wait_for_ssh(self.appliance, self.limits["boot"])
        return (
            f"rebooted with cam0 absent (the camera NIC is {self.cam_stock}) and "
            "every appliance unit enabled; first boot runs from the boot transaction"
        )

    def assert_first_boot_from_boot(self) -> str:
        deadline = time.monotonic() + self.limits["test_suite"]
        state = ""
        current = ""
        while time.monotonic() < deadline:
            result = self.appliance.ssh(
                "systemctl is-active bittern-first-boot.service; "
                "cat /var/lib/bittern/current 2>/dev/null || true",
                timeout=30,
                check=False,
            )
            lines = [line.strip() for line in result.stdout.splitlines() if line.strip()]
            state = lines[0] if lines else ""
            current = lines[1] if len(lines) > 1 else ""
            if current or state == "failed":
                break
            time.sleep(5)
        evidence = self.appliance.ssh(
            "echo '=== first boot ==='; "
            "sudo -n journalctl -b --no-pager -o cat -u bittern-first-boot.service | tail -200; "
            "echo '=== dnsmasq ==='; "
            "sudo -n journalctl -b --no-pager -o cat -u dnsmasq.service | tail -80; "
            "echo '=== current ==='; sudo -n cat /var/lib/bittern/current 2>&1; "
            "echo '=== rollback journals ==='; "
            "sudo -n sh -c 'ls -l /var/lib/bittern/apply 2>/dev/null || true'; "
            "echo '=== dnsmasq unit ==='; "
            "systemctl is-enabled dnsmasq.service; systemctl is-active dnsmasq.service; "
            "echo '=== networkd unit ==='; "
            "systemctl is-enabled systemd-networkd.service; systemctl is-active systemd-networkd.service; "
            "echo '=== cam0 ==='; cat /sys/class/net/cam0/carrier; ip -4 -o addr show dev cam0; "
            "echo '=== listeners ==='; sudo -n ss -lunp 2>/dev/null | grep -E ':(53|67)\\b' || true",
            timeout=120,
        )
        output = evidence.stdout + evidence.stderr
        (self.log_dir / "late-camera-first-boot.txt").write_text(
            self.redact(output), encoding="utf-8"
        )
        self.capture("late-camera-first-boot.txt", output)
        if current != "1":
            raise HarnessError(
                "first boot did not commit generation 1 with cam0 absent at apply "
                f"time (unit state {state!r}, current {current!r}):\n{output}"
            )
        if "restart:dnsmasq.service failed" in output:
            raise HarnessError(
                "the apply restarted dnsmasq before cam0 existed:\n" + output
            )
        if "interface cam0 does not currently exist" in output:
            raise HarnessError(
                "dnsmasq started before cam0 existed; the apply restarted it "
                "without waiting for the rendered address:\n" + output
            )
        journal = self.appliance.ssh(
            "sudo -n journalctl -b --no-pager -o cat "
            "-u bittern-first-boot.service",
            timeout=60,
        ).stdout
        positions = {}
        for key in (
            "apply: apply restart:systemd-networkd.service",
            "apply: apply wait:cam0-address",
            "apply: apply restart:dnsmasq.service",
        ):
            positions[key] = journal.find(key)
        missing = [key for key, index in positions.items() if index < 0]
        if missing:
            raise HarnessError(
                "the first apply did not run the cam0 dependency order "
                f"({', '.join(missing)}):\n{journal[-4000:]}"
            )
        if not (
            positions["apply: apply restart:systemd-networkd.service"]
            < positions["apply: apply wait:cam0-address"]
            < positions["apply: apply restart:dnsmasq.service"]
        ):
            raise HarnessError(
                "the first apply restarted dnsmasq before networkd and the cam0 "
                f"address wait:\n{journal[-4000:]}"
            )
        self.appliance.ssh(
            "sudo -n test -s /var/lib/bittern/first-boot.json", timeout=30
        )
        rollbacks = self.appliance.ssh(
            "sudo -n sh -c 'ls /var/lib/bittern/apply/rollback-*.json 2>/dev/null | wc -l'",
            timeout=30,
        ).stdout.strip()
        if rollbacks != "0":
            raise HarnessError(
                f"first boot retained {rollbacks} rollback journal(s), so an apply "
                f"attempt rolled back:\n{output}"
            )
        # The field case: the camera network is not plugged in. The rendered
        # .network sets ConfigureWithoutCarrier=yes, so the static address must
        # exist while carrier is 0 and the address wait must not time out.
        carrierless = self.appliance.ssh(
            "cat /sys/class/net/cam0/carrier; ip -4 -o addr show dev cam0",
            timeout=30,
        ).stdout
        lines = [line for line in carrierless.splitlines() if line.strip()]
        if not lines or lines[0].strip() != "0":
            raise HarnessError(
                "cam0 carrier is not 0 after first boot, so the carrierless case "
                f"is not exercised:\n{carrierless}"
            )
        if "10.77.0.1/24" not in carrierless:
            raise HarnessError(
                "cam0 has no rendered address while carrier is 0; the rendered "
                ".network did not apply ConfigureWithoutCarrier=yes, so the "
                f"address wait can time out on an unplugged camera network:\n{carrierless}"
            )
        listeners = self.appliance.ssh(
            "sudo -n ss -lunp 2>/dev/null | grep ':53' || true", timeout=30
        ).stdout
        if "dnsmasq" in listeners:
            raise HarnessError(
                "dnsmasq holds a port 53 listener; the rendered port=0 config "
                f"was not loaded:\n{listeners}"
            )
        if "127.0.0.53" not in listeners:
            raise HarnessError(
                f"systemd-resolved does not hold the stub listener:\n{listeners}"
            )
        dhcp = self.appliance.ssh(
            "sudo -n ss -lunp 2>/dev/null | grep ':67' || true", timeout=30
        ).stdout
        if "dnsmasq" not in dhcp:
            raise HarnessError(
                f"dnsmasq has no DHCP listener on port 67 after first boot:\n{dhcp}"
            )
        return (
            "first boot committed generation 1 from the boot transaction with cam0 "
            "absent at apply time: networkd restarted, cam0 has 10.77.0.1/24 with "
            "carrier 0 (ConfigureWithoutCarrier=yes), dnsmasq is active with a port "
            "67 listener and no port 53 listener, systemd-resolved holds "
            "127.0.0.53:53, and no rollback journal was retained"
        )


CAMERA_DISCOVERY_RESPONDER = r'''#!/usr/bin/env python3
"""Minimal ONVIF WS-Discovery responder for the QEMU camera fixture."""

import socket
import sys
import urllib.parse

address = sys.argv[1]
mac = sys.argv[2]
reply = (
    '<?xml version="1.0" encoding="UTF-8"?>'
    '<e:Envelope xmlns:e="http://www.w3.org/2003/05/soap-envelope"'
    ' xmlns:w="http://schemas.xmlsoap.org/ws/2004/08/addressing"'
    ' xmlns:d="http://schemas.xmlsoap.org/ws/2005/04/discovery"'
    ' xmlns:dn="http://www.onvif.org/ver10/network/wsdl">'
    '<e:Body><d:ProbeMatches><d:ProbeMatch>'
    '<d:EndpointReference><w:Address>urn:uuid:bittern-qemu-camera</w:Address></d:EndpointReference>'
    '<d:Types>dn:NetworkVideoTransmitter</d:Types>'
    '<d:Scopes>onvif://www.onvif.org/name/Virtual%20Camera '
    'onvif://www.onvif.org/hardware/QEMU</d:Scopes>'
    f'<d:XAddrs>http://{address}:8554/onvif/device_service</d:XAddrs>'
    '<d:MetadataVersion>1</d:MetadataVersion>'
    '</d:ProbeMatch></d:ProbeMatches></e:Body></e:Envelope>'
)
sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
sock.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
sock.bind(("0.0.0.0", 3702))
while True:
    data, peer = sock.recvfrom(65535)
    if b"Probe" in data and b"ProbeMatches" not in data:
        sock.sendto(reply.encode("utf-8"), peer)
'''

CAMERA_ADOPTION_HELPER = r'''#!/bin/sh
set -eu
install -d /opt/bittern-camera
cd /opt/bittern-camera
if [ ! -x mediamtx ]; then
    echo "mediamtx is not installed" >&2
    exit 1
fi
cat > mediamtx-adoption.yml <<'EOF'
logLevel: warn
api: false
metrics: false
pprof: false
playback: false
moq: false
rtsp: true
rtspAddress: 0.0.0.0:8554
rtspTransports: [tcp]
rtmp: false
hls: false
webrtc: false
srt: false
authMethod: internal
authInternalUsers:
  - user: __CAMERA_USER__
    pass: __CAMERA_PASSWORD__
    permissions:
      - action: read
      - action: publish
paths:
  all_others:
EOF
./mediamtx mediamtx-adoption.yml >/var/log/bittern-adoption-mediamtx.log 2>&1 &
for _ in $(seq 1 30); do
    if python3 -c 'import socket; s = socket.create_connection(("127.0.0.1", 8554), 1); s.close()' 2>/dev/null; then
        break
    fi
    sleep 1
done
exec ffmpeg -hide_banner -loglevel warning -nostdin -re \
    -f lavfi -i "testsrc=size=__CAM_RES__:rate=__CAM_FPS__" \
    -an -c:v libx264 -preset ultrafast -tune zerolatency \
    -pix_fmt yuv420p -g 20 -f rtsp -rtsp_transport tcp \
    "rtsp://__CAMERA_USER__:__CAMERA_PASSWORD_ENCODED__@127.0.0.1:8554__CAM_PATH__"
'''

RTSP_OPTIONS_PROBE = r'''#!/usr/bin/env python3
"""Send RTSP DESCRIBE requests and print the status codes."""

import base64
import socket
import sys

host = sys.argv[1]
user = sys.argv[2] if len(sys.argv) > 2 else ""
path = sys.argv[3] if len(sys.argv) > 3 else "/cam0"
password = sys.stdin.readline().rstrip("\n") if user else ""


def send(authorization=""):
    headers = "CSeq: 1\r\nAccept: application/sdp\r\nUser-Agent: bittern-qemu-adoption\r\n"
    if authorization:
        headers += f"Authorization: {authorization}\r\n"
    request = f"DESCRIBE rtsp://{host}:8554{path} RTSP/1.0\r\n{headers}\r\n"
    with socket.create_connection((host, 8554), 5) as sock:
        sock.sendall(request.encode("utf-8"))
        sock.settimeout(5)
        return sock.recv(2048).decode("utf-8", "replace")


anonymous = send()
if user:
    credentials = base64.b64encode(f"{user}:{password}".encode("utf-8")).decode("ascii")
    authenticated = send("Basic " + credentials)
    print("anonymous " + (anonymous.splitlines()[0] if anonymous else "no response"))
    print("authenticated " + (authenticated.splitlines()[0] if authenticated else "no response"))
else:
    print(anonymous.splitlines()[0] if anonymous else "no response")
'''


class AdoptionScenario(FirstbootScenario):
    """Adopt a DHCP camera through discovery, credentials and a reservation."""

    def __init__(
        self,
        appliance: Vm,
        camera: Vm,
        config: dict[str, Any],
        accelerator: str,
    ) -> None:
        super().__init__(appliance, camera, config, accelerator, scenario_name="adoption")
        self.camera_mac = ""
        self.camera_username = "bittern"
        self.camera_password = ""
        self.discovered_address = ""
        self.adoption_facts: dict[str, Any] = {}
        self.wizard_client: WizardClient | None = None
        self.cleanup_path = ""

    def redact(self, text: str) -> str:
        text = super().redact(text)
        if self.camera_password:
            text = text.replace(self.camera_password, "<redacted>")
        return text

    def run(self) -> int:
        self.run_phase("guests", self.ensure_guests)
        self.run_phase("install", self.install_components)
        self.run_phase("cleanup-copy", self.stage_cleanup)
        self.run_phase("first-boot", self.run_first_boot)
        self.run_phase("installer-rerun", self.reinstall_after_firewall)
        self.run_phase("reboot", self.reboot_once)
        self.run_phase("resolver", self.assert_resolver_after_reboot)
        self.run_phase("setup-token", self.read_setup_token)
        self.run_phase("setup-token-qr", self.read_setup_token_qr)
        self.run_phase("daemon", self.start_daemon)
        self.run_phase("cam0-down", self.assert_cam0_down_status)
        self.run_phase("dhcp-lease", self.assert_dhcp_lease)
        self.run_phase("camera-dhcp", self.configure_camera_dhcp)
        self.run_phase("wizard", self.drive_wizard)
        self.run_phase("docker-frigate", self.restart_docker_with_frigate)
        self.run_phase("assertions", self.assert_applied)
        self.run_phase("camera-list", self.assert_camera_list)
        self.run_phase("camera-offline", self.assert_camera_offline)
        self.run_phase("cleanup", self.assert_cleanup)
        self.run_phase("logs", self.assert_logs_clean)
        self.write_summary("pass", "")
        self.note(f"adoption scenario passed; logs: {self.log_dir}")
        return 0

    def stage_cleanup(self) -> str:
        if not self.installer_path:
            raise HarnessError("the installer payload path is not available")
        source = f"{self.installer_path}/cleanup.sh"
        self.appliance.ssh(
            f"sudo -n install -m 0755 {shlex.quote(source)} /usr/local/bin/bittern-cleanup",
            timeout=30,
        )
        self.cleanup_path = "/usr/local/bin/bittern-cleanup"
        return "kept cleanup.sh outside the payload directory for the later plan"

    def configure_camera_dhcp(self) -> str:
        self.camera_mac = self.camera.ssh(
            "cat /sys/class/net/cam0/address", timeout=15
        ).stdout.strip().lower()
        if not re.fullmatch(r"[0-9a-f]{2}(:[0-9a-f]{2}){5}", self.camera_mac):
            raise HarnessError(f"camera cam0 MAC is not usable: {self.camera_mac!r}")
        self.camera_password = "QemuPass" + time.strftime("%Y")

        helper = (
            CAMERA_ADOPTION_HELPER.replace("__CAMERA_USER__", self.camera_username)
            .replace(
                "__CAMERA_PASSWORD_ENCODED__",
                urllib.parse.quote(self.camera_password, safe=""),
            )
            .replace("__CAMERA_PASSWORD__", self.camera_password)
            .replace("__CAM_RES__", str(self.config["camera"]["resolution"]))
            .replace("__CAM_FPS__", str(self.config["camera"]["fps"]))
            .replace("__CAM_PATH__", self.camera_path)
        )
        probe = RTSP_OPTIONS_PROBE
        for name, content in (
            ("/usr/local/bin/bittern-adoption-camera", helper),
            ("/usr/local/bin/bittern-discovery.py", CAMERA_DISCOVERY_RESPONDER),
        ):
            self.camera.ssh(
                f"sudo -n tee {name} >/dev/null && sudo -n chmod 0755 {name}",
                input_text=content,
                timeout=30,
            )
        self.appliance.ssh(
            "sudo -n tee /usr/local/bin/bittern-rtsp-probe >/dev/null && "
            "sudo -n chmod 0755 /usr/local/bin/bittern-rtsp-probe",
            input_text=probe,
            timeout=30,
        )
        unit = """[Unit]
Description=Bittern QEMU adoption camera with authentication
After=network-online.target
Wants=network-online.target

[Service]
ExecStart=/usr/local/bin/bittern-adoption-camera
Restart=always
RestartSec=2

[Install]
WantedBy=multi-user.target
"""
        self.camera.ssh(
            "sudo -n tee /etc/systemd/system/bittern-adoption-camera.service >/dev/null",
            input_text=unit,
            timeout=30,
        )

        # Move cam0 from the static fixture address to the rendered DHCP pool.
        # Rewrite netplan with mgmt0 only so a later netplan generate cannot
        # restore the static cam0 address. The 80 file then owns cam0.
        script = """set -eu
mgmt_mac=$(cat /sys/class/net/mgmt0/address)
cat > /etc/netplan/50-cloud-init.yaml <<EOF
network:
  version: 2
  ethernets:
    mgmt0:
      match:
        macaddress: "$mgmt_mac"
      dhcp4: true
      dhcp6: false
      set-name: mgmt0
EOF
/usr/sbin/netplan generate
cat > /etc/systemd/network/80-bittern-adoption-cam0.network <<'EOF'
[Match]
Name=cam0

[Network]
DHCP=ipv4
IPv6AcceptRA=no

[DHCPv4]
UseRoutes=no
UseDNS=no
EOF
systemctl disable --now bittern-camera.service
systemctl daemon-reload
systemctl enable --now bittern-adoption-camera.service
# Restart networkd from a transient timer so the SSH session can close before
# the management link reconfigures. The cam0 runtime file is gone and the 80
# file above asks for DHCP; networkd drops the old static address on restart.
systemd-run --on-active=2 --unit=bittern-adoption-netd --collect     /bin/systemctl restart systemd-networkd.service
"""
        self.camera.ssh(f"sudo -n sh -c {shlex.quote(script)}", timeout=120)

        # The authenticated camera fixture must be listening before the RTSP
        # probe. Its config is generated by the helper script, so a rejected
        # credential or a port conflict is visible here with the service log.
        deadline = time.monotonic() + 30
        while time.monotonic() < deadline:
            listener = self.camera.ssh(
                "ss -H -lnt 'sport = :8554'", timeout=15, check=False
            ).stdout
            if ":8554" in listener:
                break
            time.sleep(1)
        else:
            status = self.camera.ssh(
                "sudo -n systemctl status bittern-adoption-camera --no-pager -l",
                timeout=30,
                check=False,
            )
            log = self.camera.ssh(
                "sudo -n cat /var/log/bittern-adoption-mediamtx.log 2>/dev/null",
                timeout=30,
                check=False,
            )
            raise HarnessError(
                "the authenticated camera fixture is not listening on 8554:\n"
                + status.stdout + status.stderr + log.stdout
            )

        deadline = time.monotonic() + 60
        address = ""
        last = "cam0 has no DHCP address"
        while time.monotonic() < deadline:
            try:
                output = self.camera.ssh("ip -4 -o addr show dev cam0", timeout=15).stdout
            except HarnessError as exc:
                last = str(exc)
                time.sleep(1)
                continue
            match = re.search(r"inet (10\.77\.0\.(\d+))/24", output)
            if match and 50 <= int(match.group(2)) <= 199:
                address = match.group(1)
                break
            last = output.strip()
            time.sleep(1)
        if not address:
            raise HarnessError(f"camera cam0 did not take a DHCP lease: {last}")
        self.camera_address = address
        self.discovered_address = address
        self.camera_url = (
            f"rtsp://{self.camera_username}:{self.camera_password}@{address}:8554{self.camera_path}"
        )

        # Wait for the appliance lease file to name the camera's own MAC. That
        # is the discovery fallback and the source of the reservation.
        deadline = time.monotonic() + 30
        lease = ""
        while time.monotonic() < deadline:
            found = self.appliance.ssh(
                "sudo -n sh -c 'cat /var/lib/misc/dnsmasq.leases 2>/dev/null || true'",
                timeout=15,
            ).stdout
            for line in found.splitlines():
                if self.camera_mac in line.lower() and address in line:
                    lease = line
                    break
            if lease:
                break
            time.sleep(1)
        if not lease:
            raise HarnessError(
                f"the appliance lease file does not record {self.camera_mac} at {address}"
            )

        # Prove the stream requires the credentials the wizard is about to
        # store: an anonymous OPTIONS is rejected, the authenticated one works.
        probe = self.appliance.ssh(
            "sudo -n /usr/bin/python3 /usr/local/bin/bittern-rtsp-probe "
            f"{address} {self.camera_username} {self.camera_path}",
            input_text=self.camera_password + "\n",
            timeout=30,
            check=False,
        )
        probe_status = (probe.stdout or probe.stderr).strip()
        if "anonymous RTSP/1.0 401" not in probe_status:
            raise HarnessError(
                f"the camera did not reject an anonymous RTSP DESCRIBE: {probe_status!r}"
            )
        if "authenticated RTSP/1.0 200" not in probe_status:
            raise HarnessError(
                f"the camera rejected the authenticated RTSP DESCRIBE: {probe_status!r}"
            )

        evidence = (
            f"camera cam0 MAC {self.camera_mac}\n"
            f"cached lease {lease}\n"
            f"camera address after DHCP {address}\n"
            f"RTSP DESCRIBE: {probe_status}\n"
            f"discovery responder: active\n"
        )
        (self.log_dir / "camera-adoption.txt").write_text(self.redact(evidence), encoding="utf-8")
        self.capture("camera-adoption.txt", evidence)
        return (
            f"camera at {address} took a DHCP lease from the pool, requires the "
            "wizard credentials on RTSP, and answered the discovery fallback layers"
        )

    def start_discovery_responder(self) -> None:
        self.camera.ssh(
            "sudo -n systemd-run --unit=bittern-discovery --collect --property=Restart=no "
            f"/usr/bin/python3 /usr/local/bin/bittern-discovery.py "
            f"{self.discovered_address} {self.camera_mac}",
            timeout=30,
        )
        deadline = time.monotonic() + 30
        while time.monotonic() < deadline:
            active = self.camera.ssh(
                "systemctl is-active bittern-discovery.service", timeout=15, check=False
            ).stdout.strip()
            if active == "active":
                return
            time.sleep(1)
        raise HarnessError("the camera WS-Discovery responder did not start")

    def drive_wizard(self) -> str:
        self.admin_password = "Bittern-QEMU-" + hashlib.sha256(
            (self.token + time.strftime("%s")).encode("utf-8")
        ).hexdigest()[:20]
        client = WizardClient(int(self.config["network"]["lan"]["hostfwd"]["ui"]))
        self.wizard_client = client
        client.get_page("/ui/setup")
        client.post(
            "/ui/setup",
            {
                "setup_token": self.token,
                "password": self.admin_password,
                "password_confirm": self.admin_password,
            },
        )
        body = client.get_page("/ui/wizard")
        require_data(body, "data-step-name", "welcome", "wizard welcome page")
        client.post("/ui/wizard/network")
        body = client.get_page("/ui/wizard/network")
        require_data(body, "data-step-name", "network", "wizard network page")
        require_data(body, "data-network-cam-carrier", "true", "wizard network check")
        client.post("/ui/wizard/storage")
        body = client.get_page("/ui/wizard/storage")
        require_data(body, "data-step-name", "storage", "wizard storage page")
        client.post("/ui/wizard/camera/continue")

        # First page: WS-Discovery is deliberately off. The lease and ARP
        # layers and the RTSP probe must still show the camera, and the
        # adoption must not depend on ONVIF.
        body = client.get_page("/ui/wizard/camera")
        self.capture("wizard-camera-fallback.html", body)
        require_data(body, "data-step-name", "camera", "wizard camera page")
        require_data(body, "data-camera-address", self.discovered_address, "fallback discovery page")
        require_data(body, "data-camera-mac", self.camera_mac, "fallback discovery page")
        for layer in ("dhcp lease", "onvif-device", "rtsp"):
            require_data(body, "data-discovery-layer", layer, "fallback discovery page")
        details = data_values(body, "data-discovery-detail")
        for value in ("No ONVIF XAddr", "RTSP OPTIONS", "8554"):
            if not any(value in detail for detail in details):
                raise HarnessError(f"the fallback discovery page does not show {value!r}")
        require_data(body, "data-discovery-state", "ok", "fallback discovery page")

        self.start_discovery_responder()
        time.sleep(6)
        body = client.get_page("/ui/wizard/camera")
        self.capture("wizard-camera-discovery.html", body)
        require_data(body, "data-step-name", "camera", "wizard camera page")
        require_data(body, "data-camera-address", self.discovered_address, "camera discovery page")
        require_data(body, "data-camera-mac", self.camera_mac, "camera discovery page")
        # The candidate meta shows the model as a plain value. The fixture
        # publishes it in the ONVIF name scope, which discovery reports as the
        # model.
        require_data(body, "data-camera-model", "Virtual Camera", "camera discovery page")
        require_data(body, "data-camera-state", "online", "camera discovery page")
        require_data(body, "data-discovery-layer", "ws-discovery", "camera discovery page")

        # Adopt by typing the address and credentials. The MAC comes from the
        # server-side lease/ARP result, not from the browser and not from ONVIF.
        location, _ = client.post(
            "/ui/wizard/camera",
            {
                "name": "Adopted camera",
                "address": self.discovered_address,
                "rtsp_port": "8554",
                "rtsp_path": self.camera_path,
                "username": self.camera_username,
                "password": self.camera_password,
            },
        )
        if location != "/ui/wizard/review":
            raise HarnessError(f"camera submit redirected to {location!r}, wanted review")
        review = client.get_page("/ui/wizard/review")
        self.capture("wizard-review.html", review)
        require_data(review, "data-step-name", "review", "review page")
        require_data(review, "data-camera-address", self.discovered_address, "review page")
        require_data(review, "data-camera-mac", self.camera_mac, "review page")
        require_data(review, "data-camera-reservation", self.discovered_address, "review page")
        require_data(review, "data-camera-credentials", "stored", "review page")
        if self.camera_password in review or "cameras.camera_1" in review:
            raise HarnessError("review page leaks the camera password or credential key")

        client.post("/ui/wizard/apply", timeout=1800)
        body = client.get_page("/ui/wizard/done")
        self.capture("wizard-done.html", body)
        require_data(body, "data-step-name", "done", "wizard done page")
        require_data(body, "data-result-state", "succeeded", "wizard done page")
        require_data(body, "data-camera-picture", "received", "wizard done page")
        return (
            "discovery listed the DHCP camera with its MAC and model; the wizard stored "
            "the credentials and reservation, applied, and Frigate returned a picture"
        )

    def assert_camera_list(self) -> str:
        client = self.wizard_client
        if client is None:
            raise HarnessError("the wizard client is not available")
        body = client.get_page("/ui/cameras")
        self.capture("cameras-adopted.html", body)
        (self.log_dir / "cameras-adopted.html").write_text(self.redact(body), encoding="utf-8")
        for value in (
            "Adopted camera",
            self.discovered_address,
            self.camera_mac,
            "Virtual Camera",
            "/ui/cameras/camera_1/edit",
            "/ui/cameras/camera_1/snapshot",
            "/ui/cameras/camera_1/remove",
        ):
            if value not in body:
                raise HarnessError(f"the managed camera list does not show {value!r}")
        require_data(body, "data-camera-enabled", "true", "managed camera list")
        require_data(body, "data-camera-stream-state", "ok", "managed camera list")
        require_data(body, "data-camera-picture-state", "ok", "managed camera list")
        require_data(body, "data-camera-recording-state", "ok", "managed camera list")

        body = client.get_page("/ui/cameras/camera_1/edit")
        if "Edit camera" not in body or "Apply camera change" not in body:
            raise HarnessError("the camera edit page is missing")
        location, _ = client.post(
            "/ui/cameras/camera_1/edit",
            {
                "name": "Adopted camera edited",
                "address": self.discovered_address,
                "rtsp_port": "8554",
                "rtsp_path": self.camera_path,
            },
        )
        if not (location or "").startswith("/ui/cameras?flash=edited"):
            raise HarnessError(f"camera edit redirected to {location!r}")
        body = client.get_page("/ui/cameras")
        require_data(body, "data-camera-name", "Adopted camera edited", "edited camera list")
        require_data(body, "data-camera-picture-state", "ok", "edited camera list")

        body = ""
        passed = False
        for _ in range(4):
            location, _ = client.post("/ui/cameras/camera_1/snapshot", {}, timeout=180)
            body = client.get_page(location or "/ui/cameras")
            if "The picture test passed" in body and "ok" in data_values(body, "data-camera-picture-state"):
                passed = True
                break
            time.sleep(5)
        self.capture("cameras-edited.html", body)
        (self.log_dir / "cameras-edited.html").write_text(self.redact(body), encoding="utf-8")
        if not passed:
            health = self.appliance.ssh(
                "sudo -n cat /var/lib/bittern/camera-health.json 2>/dev/null",
                timeout=30,
                check=False,
            ).stdout
            raise HarnessError(
                "the explicit test picture did not show success: " + health
            )
        return (
            "the list showed the adopted camera and its snapshot, an edit applied, and the "
            "explicit test picture succeeded"
        )

    def assert_camera_offline(self) -> str:
        client = self.wizard_client
        if client is None:
            raise HarnessError("the wizard client is not available")
        self.camera.ssh(
            "sudo -n systemctl stop bittern-adoption-camera.service; "
            "sudo -n ip -4 addr flush dev cam0 scope global",
            timeout=60,
            check=False,
        )
        time.sleep(8)
        location, _ = client.post("/ui/cameras/camera_1/snapshot", {}, timeout=180)
        health = self.appliance.ssh(
            "sudo -n cat /var/lib/bittern/camera-health.json 2>/dev/null", timeout=30, check=False
        ).stdout
        if '"snapshot": "failed"' not in health:
            # Frigate can keep the last frame after the camera leaves. Its own
            # unavailability gives the same visible failed state deterministically.
            self.appliance.ssh("sudo -n systemctl stop bittern-frigate.service", timeout=90, check=False)
            location, _ = client.post("/ui/cameras/camera_1/snapshot", {}, timeout=180)
            health = self.appliance.ssh(
                "sudo -n cat /var/lib/bittern/camera-health.json 2>/dev/null", timeout=30, check=False
            ).stdout
        body = client.get_page(location or "/ui/cameras")
        self.capture("cameras-offline.html", body)
        (self.log_dir / "cameras-offline.html").write_text(self.redact(body), encoding="utf-8")
        require_data(body, "data-camera-picture-state", "failed", "offline camera list")
        require_data(body, "data-camera-stream-state", "failed", "offline camera list")
        if "The picture test failed" not in body:
            raise HarnessError("the offline snapshot test did not report a failure")

        document = self.status_document("status-camera-offline.json")
        cameras = document.get("sections", {}).get("cameras", {})
        items = cameras.get("items", [])
        if cameras.get("state") != "degraded" or not items:
            raise HarnessError(
                f"status does not report a degraded camera section: {json.dumps(cameras, sort_keys=True)}"
            )
        if items[0].get("snapshot") != "failed" or items[0].get("stream") != "failed":
            raise HarnessError(
                f"status does not report the failed camera row: {json.dumps(items[0], sort_keys=True)}"
            )
        evidence = (
            f"$ cat /var/lib/bittern/camera-health.json\n{health}\n"
            f"$ GET /ui/cameras\n{body}\n"
            f"$ GET /api/status cameras\n{json.dumps(cameras, indent=2, sort_keys=True)}\n"
        )
        (self.log_dir / "camera-offline.txt").write_text(self.redact(evidence), encoding="utf-8")
        self.capture("camera-offline.txt", evidence)
        return (
            "the camera list reported failed picture and stream states, and the status contract "
            "reported the camera section degraded"
        )

    def assert_applied(self) -> str:
        detail = super().assert_applied()
        facts = self.assert_adoption_files()
        self.adoption_facts = facts
        return f"{detail}; {facts['summary']}"

    def assert_adoption_files(self) -> dict[str, Any]:
        address = self.discovered_address
        config = self.guest_file("/etc/bittern/appliance.yaml")
        for value in (
            f"address: {address}",
            f"mac: {self.camera_mac}",
            "credentials_ref: cameras.camera_1.rtsp_url",
        ):
            if value not in config:
                raise HarnessError(f"appliance.yaml does not contain {value!r}")
        if self.camera_password in config or "rtsp://" in config:
            raise HarnessError("appliance.yaml contains a credential value")

        reservation_path = "/etc/dnsmasq.d/bittern-reservation.conf"
        reservation = self.guest_file(reservation_path)
        if not reservation.startswith(
            "# Generated by Bittern from appliance.yaml; do not edit."
        ):
            raise HarnessError(f"{reservation_path} is not Bittern-owned renderer output")
        if f"dhcp-host={self.camera_mac},{address}" not in reservation:
            raise HarnessError(f"{reservation_path} does not pin the camera address")
        self.assert_mode(reservation_path, "644")
        syntax = self.appliance.ssh(
            f"sudo -n dnsmasq --test --conf-file={reservation_path}", timeout=30
        )
        if "syntax check OK" not in (syntax.stdout + syntax.stderr):
            raise HarnessError(f"dnsmasq rejects the reservation file: {syntax.stdout}{syntax.stderr}")

        secrets_mode = self.appliance.ssh(
            "sudo -n stat -c '%a' /etc/bittern/secrets.yaml", timeout=30
        ).stdout.strip()
        if secrets_mode != "600":
            raise HarnessError(f"secrets.yaml mode is {secrets_mode}, expected 600")
        secrets = self.guest_file("/etc/bittern/secrets.yaml", secret=True)
        encoded_password = urllib.parse.quote(self.camera_password, safe="")
        expected_url = (
            f"rtsp://{self.camera_username}:{encoded_password}@{address}:8554{self.camera_path}"
        )
        if "cameras:" not in secrets or "camera_1:" not in secrets or expected_url not in secrets:
            raise HarnessError("secrets.yaml does not carry the camera RTSP URL")

        env = self.guest_file("/var/lib/bittern/frigate/cameras.env", secret=True)
        if f"FRIGATE_CAMERA_CAMERA_1_RTSP_URL={expected_url}" not in env:
            raise HarnessError("the Frigate env file does not use the encoded camera URL")

        if not self.cleanup_path:
            raise HarnessError("the cleanup.sh copy is not available")
        plan = self.appliance.ssh(
            f"sudo -n {shlex.quote(self.cleanup_path)}", timeout=120
        )
        if "remove Bittern dnsmasq file " + reservation_path not in plan.stdout:
            raise HarnessError("cleanup plan does not recognise the Bittern reservation file")
        if "manual dnsmasq reservation" in plan.stderr:
            raise HarnessError("cleanup treats the Bittern reservation as manual")
        (self.log_dir / "cleanup-plan.txt").write_text(
            self.redact(plan.stdout + plan.stderr), encoding="utf-8"
        )
        self.capture("cleanup-plan.txt", plan.stdout + plan.stderr)

        evidence = (
            f"$ cat /etc/bittern/appliance.yaml\n{config}\n"
            f"$ cat {reservation_path}\n{reservation}\n"
            f"$ dnsmasq --test --conf-file={reservation_path}\n{syntax.stdout}{syntax.stderr}\n"
            f"$ stat -c '%a' /etc/bittern/secrets.yaml\n{secrets_mode}\n"
            f"$ cleanup.sh (plan only)\n{plan.stdout}"
        )
        (self.log_dir / "adoption-files.txt").write_text(self.redact(evidence), encoding="utf-8")
        self.capture("adoption-files.txt", evidence)
        self.asserted_files.extend(
            [
                "/etc/bittern/appliance.yaml camera mac and credentials_ref",
                reservation_path,
                "/etc/bittern/secrets.yaml encoded camera URL",
                "cleanup plan removes the Bittern reservation",
            ]
        )
        return {
            "summary": (
                f"reservation {self.camera_mac}->{address} rendered and accepted by dnsmasq, "
                "encoded credential written at mode 0600, cleanup plans its removal"
            )
        }

    def assert_logs_clean(self) -> str:
        detail = super().assert_logs_clean()
        journal = self.appliance.ssh("sudo -n journalctl --no-pager -o cat", timeout=120).stdout
        serial = ""
        if self.appliance.serial_log.exists():
            serial = self.appliance.serial_log.read_text(encoding="utf-8", errors="replace")
        captured = "".join(self.captured)
        for label, value in (
            ("camera password", self.camera_password),
            ("encoded camera URL", urllib.parse.quote(self.camera_password, safe="")),
        ):
            if not value:
                continue
            for source, text in (
                ("journal", journal),
                ("serial log", serial),
                ("harness transcript", captured),
            ):
                if value in text:
                    raise HarnessError(f"the {source} contains the {label}")
        return detail + "; no camera password or encoded credential in the logs"


UPDATE_INCOMING = "/var/lib/bittern/update/incoming"
UPDATE_STATE_DIR = "/var/lib/bittern/update"
UPDATE_POINTER = "/var/lib/bittern/app/current"
UPDATE_PLATFORM = "/usr/lib/bittern/platform.json"
UPDATE_TRUST_DIR = "/etc/bittern/trust/release-keys"
UPDATE_IMAGE_TRUST_DIR = "/usr/lib/bittern/trust/release-keys"


def build_release_tool() -> Path:
    result = run_command(["make", "bittern-release"], cwd=ROOT, timeout=300)
    tool = ROOT / "agent" / "bin" / "bittern-release"
    if result.returncode != 0 or not tool.exists():
        raise HarnessError(
            "make bittern-release failed; the update scenario signs its bundles offline:\n"
            f"{result.stdout}{result.stderr}"
        )
    return tool


def generate_ephemeral_key(tool: Path, directory: Path) -> tuple[Path, Path, str]:
    # The private seed stays in build/qemu, which is gitignored, and is deleted
    # after the bundles are signed. Only the public half reaches the guest.
    key = directory / "ephemeral.key"
    pub_dir = directory / "pub"
    pub_dir.mkdir(parents=True)
    result = run_command(
        [str(tool), "init", "--key", str(key), "--pub-dir", str(pub_dir)], timeout=120
    )
    if result.returncode != 0:
        raise HarnessError(f"bittern-release init failed: {result.stdout}{result.stderr}")
    match = re.search(r"^init: key id ([0-9a-f]{64})$", result.stdout, re.MULTILINE)
    if not match:
        raise HarnessError(f"bittern-release init did not report a key id: {result.stdout}")
    key_id = match.group(1)
    public = pub_dir / f"{key_id}.pub"
    if not public.exists():
        raise HarnessError(f"the ephemeral anchor {public} was not written")
    if hashlib.sha256(public.read_bytes()).hexdigest() != key_id:
        raise HarnessError("the ephemeral anchor digest does not equal its key id")
    return key, public, key_id


def go_tool() -> str:
    # The Makefile uses GO, so a command-line GO=... reaches the recipe
    # environment. Fall back to go on PATH, the same default the Makefile uses.
    return os.environ.get("GO", "go")


def build_beta_kit(public_key: Path, created: str) -> Path:
    # make beta-payload is the one beta kit packer. KEY_PUB overrides the
    # committed CI test anchor with this run's ephemeral public key, and KEY_ID
    # is derived from it, so install.sh installs the ephemeral anchor.
    result = run_command(
        [
            "make",
            "beta-payload",
            f"KEY_PUB={public_key}",
            f"GO={go_tool()}",
            "GENERATION=1",
            f"PAYLOAD_CREATED={created}",
        ],
        cwd=ROOT,
        timeout=900,
    )
    archive = ROOT / "build" / "payload" / "bittern-beta-kit.tar.gz"
    if result.returncode != 0 or not archive.exists():
        raise HarnessError(
            "make beta-payload failed; the update scenario installs from the beta kit:\n"
            f"{result.stdout}{result.stderr}"
        )
    return archive


def build_go_binary(command: str, destination: Path, version: str = "") -> None:
    environment = os.environ.copy()
    environment.update({"CGO_ENABLED": "0", "GOPROXY": "off", "GOFLAGS": "-mod=vendor"})
    flags = "-s -w"
    if version:
        flags += f" -X main.version={version}"
    result = run_command(
        [
            go_tool(),
            "build",
            "-trimpath",
            "-buildvcs=false",
            f"-ldflags={flags}",
            "-o",
            str(destination),
            f"./cmd/{command}",
        ],
        cwd=ROOT / "agent",
        env=environment,
        timeout=300,
    )
    if result.returncode != 0:
        raise HarnessError(
            f"go build {command} failed: {result.stdout}{result.stderr}".strip()
        )
    destination.chmod(0o755)


def build_update_bundle(
    tool: Path,
    work: Path,
    generation: int,
    version: str,
    commit: str,
    key_id: str,
    key: Path,
    created: str,
) -> Path:
    tree = work / f"tree-g{generation}"
    binaries = tree / "usr" / "bin"
    binaries.mkdir(parents=True)
    build_go_binary("bittern", binaries / "bittern")
    build_go_binary("bitternd", binaries / "bitternd", version)
    if generation == 3:
        # Generation 3 fails the post-activation bundle-integrity probe by
        # writing a file into its own verified tree before execing the real
        # agent. The manifest lists both files, so the bundle is valid until the
        # running process adds the marker; the health gate then sees file.extra
        # and the transaction rolls back to generation 2. The padding files are
        # walked and hashed before usr/bin, which keeps the integrity walk open
        # long enough for the just-started agent to create the marker instead of
        # racing the walk.
        padding = tree / "aaa"
        padding.mkdir()
        for name, size in (("padding-1.bin", 60 << 20), ("padding-2.bin", 30 << 20)):
            with (padding / name).open("wb") as handle:
                handle.truncate(size)
        real = binaries / "bitternd.real"
        (binaries / "bitternd").rename(real)
        wrapper = binaries / "bitternd"
        wrapper.write_text(
            "#!/bin/sh\n"
            "# QEMU update scenario only: fail the generation 3 health gate.\n"
            'marker="$(dirname "$0")/post-activation-marker"\n'
            ': > "$marker" || exit 1\n'
            'exec "$(dirname "$0")/bitternd.real" "$@"\n',
            encoding="utf-8",
        )
        wrapper.chmod(0o755)
    out = work / "bundles" / f"g{generation}"
    out.mkdir(parents=True)
    packed = run_command(
        [
            str(tool),
            "pack",
            "--tree",
            str(tree),
            "--out",
            str(out),
            "--version",
            version,
            "--generation",
            str(generation),
            "--channel",
            "beta",
            "--source-commit",
            commit,
            "--key-id",
            key_id,
            "--os-generation-min",
            "1",
            "--os-generation-max",
            "1",
            "--schema-min",
            "1",
            "--schema-max",
            "2",
            "--updater-min",
            "1",
            "--created",
            created,
            "--epoch",
            "0",
        ],
        timeout=300,
    )
    if packed.returncode != 0:
        raise HarnessError(
            f"bittern-release pack g{generation} failed:\n{packed.stdout}{packed.stderr}"
        )
    signed = run_command(
        [
            str(tool),
            "sign",
            str(out / "manifest.json"),
            "--key",
            str(key),
            "--yes",
        ],
        timeout=120,
    )
    if signed.returncode != 0:
        raise HarnessError(
            f"bittern-release sign g{generation} failed:\n{signed.stdout}{signed.stderr}"
        )
    signature = out / "manifest.json.sig"
    if not signature.exists() or len(signature.read_bytes()) != 64:
        raise HarnessError(f"the generation {generation} signature is not 64 bytes")
    return out


class UpdateScenario(FirstbootScenario):
    def __init__(
        self,
        appliance: Vm,
        camera: Vm,
        config: dict[str, Any],
        accelerator: str,
    ) -> None:
        super().__init__(appliance, camera, config, accelerator, scenario_name="update")
        self.work = BUILD / "update"
        self.key_id = ""
        self.commit = ""
        self.versions: dict[int, str] = {}
        self.kit_archive: Path | None = None
        self.public_key_path: Path | None = None
        self.staged: dict[str, Path] = {}

    def run(self) -> int:
        self.run_phase("update-assets", self.prepare_update_assets)
        self.run_phase("guests", self.ensure_guests)
        self.run_phase("install", self.install_components)
        self.run_phase("platform", self.assert_platform_and_anchor)
        self.run_phase("first-boot", self.run_first_boot)
        self.run_phase("device-channel", self.set_device_channel)
        self.run_phase("agent-baseline", self.assert_baseline_agent)
        self.run_phase("install-generation-1", self.install_generation_1)
        self.run_phase("update-generation-2", self.update_generation_2)
        self.run_phase("rollback-generation-3", self.rollback_generation_3)
        self.run_phase("rejections", self.reject_invalid_bundles)
        self.run_phase("assertions", self.assert_update_state)
        self.run_phase("logs", self.assert_logs_clean)
        self.write_summary("pass", "")
        self.note(f"update scenario passed; logs: {self.log_dir}")
        return 0

    def prepare_update_assets(self) -> str:
        toolchain = go_tool()
        if shutil.which(toolchain) is None:
            raise HarnessError(
                "the update scenario builds the beta kit and signed bundles; set GO to the "
                "Go binary or put it on PATH"
            )
        if shutil.which("make") is None:
            raise HarnessError("the update scenario needs make to build the beta kit")
        if self.work.exists():
            shutil.rmtree(self.work)
        self.work.mkdir(parents=True)
        tool = build_release_tool()
        key, public, key_id = generate_ephemeral_key(tool, self.work / "keys")
        self.key_id = key_id
        self.public_key_path = public
        commit = run_command(["git", "rev-parse", "HEAD"], cwd=ROOT, timeout=30)
        self.commit = commit.stdout.strip()
        if commit.returncode != 0 or not re.fullmatch(r"[0-9a-f]{40}", self.commit):
            raise HarnessError(
                f"cannot read the source commit for the bundle manifests: {self.commit!r}"
            )
        created = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())
        self.kit_archive = build_beta_kit(public, created)
        for generation in (1, 2, 3, 4):
            version = f"0.0.0-beta.{generation}-{self.commit[:12]}"
            self.versions[generation] = version
            build_update_bundle(
                tool,
                self.work,
                generation,
                version,
                self.commit,
                self.key_id,
                key,
                created,
            )
        g4 = self.work / "bundles" / "g4"
        tampered = self.work / "bundles" / "tampered-payload"
        shutil.copytree(g4, tampered)
        # Flip one archive byte and keep the size, so the manifest signature
        # stays valid and the transaction must reject the payload digest at
        # verify-payload rather than stopping at the size bound.
        archive = tampered / "bittern-app.tar.gz"
        payload = bytearray(archive.read_bytes())
        payload[0] ^= 0xFF
        archive.write_bytes(bytes(payload))
        bad = self.work / "bundles" / "bad-signature"
        shutil.copytree(g4, bad)
        signature = bytearray((bad / "manifest.json.sig").read_bytes())
        signature[0] ^= 0xFF
        (bad / "manifest.json.sig").write_bytes(bytes(signature))
        sources = {
            "g1": self.work / "bundles" / "g1",
            "g2": self.work / "bundles" / "g2",
            "g3": self.work / "bundles" / "g3",
            "tampered-payload": tampered,
            "bad-signature": bad,
        }
        staged = self.work / "staged"
        staged.mkdir()
        for name, directory in sources.items():
            archive = staged / f"{name}.tar.gz"
            with tarfile.open(archive, "w:gz") as handle:
                handle.add(str(directory), arcname=name)
            self.staged[name] = archive
        # The private seed is not needed after every bundle is signed. Delete it
        # before anything runs in the guest so a failed run cannot leave it in
        # a log or a copied tree.
        key.unlink()
        self.capture(
            "update-assets.txt",
            f"key id {self.key_id}\ncommit {self.commit}\n"
            f"versions {json.dumps(self.versions, sort_keys=True)}\n"
            f"kit {self.kit_archive.name}\n",
        )
        return (
            f"ephemeral key {self.key_id[:16]}..., generation 1 to 4 bundles signed, "
            "tampered and bad-signature copies built, kit from the beta packer"
        )

    def stage_installer(self) -> None:
        if self.kit_archive is None:
            raise HarnessError("the beta kit archive was not built")
        remote = "/tmp/bittern-beta-kit.tar.gz"
        self.appliance.scp_to(self.kit_archive, remote, timeout=180)
        self.appliance.ssh(
            "set -eu; "
            "sudo -n rm -rf /tmp/bittern-beta-kit; sudo -n mkdir -p /tmp/bittern-beta-kit; "
            f"sudo -n tar -xzf {remote} -C /tmp/bittern-beta-kit",
            timeout=180,
        )
        self.installer_path = "/tmp/bittern-beta-kit/bittern-beta-kit"
        self.installer_archive = remote

    def assert_platform_and_anchor(self) -> str:
        platform = self.appliance.ssh(f"sudo -n cat {UPDATE_PLATFORM}", timeout=30).stdout
        try:
            parsed = json.loads(platform)
        except json.JSONDecodeError as exc:
            raise HarnessError(f"platform metadata is not JSON: {platform!r}") from exc
        if parsed != {"schema_version": 1, "generation": 1}:
            raise HarnessError(f"unexpected platform metadata: {platform!r}")
        platform_stat = self.appliance.ssh(
            f"sudo -n stat -c '%a %U' {UPDATE_PLATFORM}", timeout=30
        ).stdout.strip()
        if platform_stat != "644 root":
            raise HarnessError(
                f"platform metadata ownership or mode is {platform_stat!r}, expected 644 root"
            )
        rotation = f"{UPDATE_TRUST_DIR}/{self.key_id}.pub"
        rotation_digest = self.appliance.ssh(
            f"sudo -n sha256sum {rotation}", timeout=30
        ).stdout.split()[0]
        if rotation_digest != self.key_id:
            raise HarnessError(
                f"the rotation anchor digest {rotation_digest} does not equal the key id"
            )
        # The update helper requires the image trust store from ADR 0029 to
        # exist; the beta kit installs only the rotation anchor. The scenario
        # places the same ephemeral public key at the image path so a signed
        # bundle can be verified. This is a kit gap recorded in the scenario
        # note, not a product weakening: the anchor is still a trust input.
        if self.public_key_path is None:
            raise HarnessError("the ephemeral public key was not recorded")
        remote_key = f"/tmp/{self.public_key_path.name}"
        self.appliance.scp_to(self.public_key_path, remote_key, timeout=60)
        self.appliance.ssh(
            f"sudo -n install -d -o root -g root -m 0755 {UPDATE_IMAGE_TRUST_DIR}; "
            f"sudo -n install -o root -g root -m 0644 {remote_key} "
            f"{UPDATE_IMAGE_TRUST_DIR}/{self.key_id}.pub; "
            f"sudo -n rm -f {remote_key}",
            timeout=60,
        )
        image_anchor = f"{UPDATE_IMAGE_TRUST_DIR}/{self.key_id}.pub"
        image_digest = self.appliance.ssh(
            f"sudo -n sha256sum {image_anchor}", timeout=30
        ).stdout.split()[0]
        if image_digest != self.key_id:
            raise HarnessError(
                f"the image anchor digest {image_digest} does not equal the key id"
            )
        anchor_stat = self.appliance.ssh(
            f"sudo -n stat -c '%a %U' {image_anchor}", timeout=30
        ).stdout.strip()
        if anchor_stat != "644 root":
            raise HarnessError(
                f"the image anchor ownership or mode is {anchor_stat!r}, expected 644 root"
            )
        real_ci = hashlib.sha256(
            (ROOT / "packaging" / "trust" / "beta-ci.pub").read_bytes()
        ).hexdigest()
        if real_ci == self.key_id:
            raise HarnessError("the ephemeral key equals the committed CI test key")
        self.appliance.ssh(f"test ! -e {UPDATE_TRUST_DIR}/{real_ci}.pub", timeout=30)
        self.appliance.ssh(f"test ! -e {UPDATE_IMAGE_TRUST_DIR}/{real_ci}.pub", timeout=30)
        self.appliance.ssh("test -x /usr/libexec/bittern/bitternd", timeout=30)
        enabled = self.appliance.ssh(
            "systemctl is-enabled bittern-update.service", timeout=30
        ).stdout.strip()
        if enabled != "enabled":
            raise HarnessError(f"bittern-update.service is {enabled!r}, expected enabled")
        evidence = (
            f"$ cat {UPDATE_PLATFORM}\n{platform}\n"
            f"$ stat -c '%a %U' {UPDATE_PLATFORM}\n{platform_stat}\n"
            f"$ sha256sum {image_anchor}\n{image_digest}  {image_anchor}\n"
            f"$ stat -c '%a %U' {image_anchor}\n{anchor_stat}\n"
            f"$ sha256sum {rotation}\n{rotation_digest}  {rotation}\n"
        )
        self.capture("update-platform.txt", evidence)
        return (
            "platform metadata is schema 1 generation 1, mode 0644 root; the ephemeral "
            "anchor is installed in the image and rotation stores, and the committed CI "
            "test anchor is absent from both"
        )

    def wait_version(self) -> str:
        result = self.appliance.ssh(
            "for _ in $(seq 1 60); do "
            "curl -fsS --max-time 3 http://127.0.0.1:8080/api/version && exit 0; "
            "sleep 1; done; exit 1",
            timeout=90,
            check=False,
        )
        if result.returncode != 0:
            raise HarnessError(
                "bitternd did not answer /api/version:\n" + result.stdout + result.stderr
            )
        try:
            return str(json.loads(result.stdout.strip().splitlines()[-1])["version"])
        except (ValueError, KeyError) as exc:
            raise HarnessError(f"cannot parse /api/version: {result.stdout!r}") from exc

    def set_device_channel(self) -> str:
        # First boot writes the schema default channel, stable. The beta kit is
        # for the beta channel, and the signed bundles claim beta, so the
        # scenario records the device channel the way an owner would before the
        # update. The channel is not a rendered input.
        path = "/etc/bittern/appliance.yaml"
        before = self.appliance.ssh(f"sudo -n cat {path}", timeout=30).stdout
        if "channel: stable" not in before:
            raise HarnessError(f"first boot did not write the default channel:\n{before}")
        self.appliance.ssh(
            f"sudo -n sed -i 's/^    channel: stable$/    channel: beta/' {path}", timeout=30
        )
        after = self.appliance.ssh(f"sudo -n cat {path}", timeout=30).stdout
        if "channel: beta" not in after:
            raise HarnessError(f"the device channel was not changed to beta:\n{after}")
        self.capture("appliance-channel.txt", after)
        return "the device channel is beta before any bundle is offered"

    def assert_baseline_agent(self) -> str:
        self.appliance.ssh("sudo -n systemctl restart bitternd.service", timeout=90)
        version = self.wait_version()
        # The kit packer stamps its own bitternd with the generation 1 version,
        # so the installed baseline and the generation 1 bundle carry the same
        # string. The generation 2 assertion below is the version change.
        if version != self.versions[1]:
            raise HarnessError(
                f"the installed baseline agent reports {version!r}, expected the kit build "
                f"{self.versions[1]!r}"
            )
        if self.appliance.ssh(f"test ! -e {UPDATE_POINTER}", timeout=30).returncode != 0:
            raise HarnessError("the activation pointer exists before the first bundle install")
        status = self.appliance.ssh("sudo -n bittern update status", timeout=30).stdout
        if "committed: no update state" not in status:
            raise HarnessError(f"update status is not empty before the first install:\n{status}")
        return (
            f"baseline agent reports {self.versions[1]}; no activation pointer and no "
            "committed update state"
        )

    def stage_update_sources(self) -> None:
        self.appliance.ssh(
            f"sudo -n install -d -m 0755 -o root -g root {UPDATE_INCOMING}", timeout=30
        )
        for name, archive in self.staged.items():
            remote = f"/tmp/{name}.tar.gz"
            self.appliance.scp_to(archive, remote, timeout=180)
            self.appliance.ssh(
                "set -eu; "
                f"sudo -n rm -rf {UPDATE_INCOMING}/{name}; "
                f"sudo -n tar -xzf {remote} -C {UPDATE_INCOMING}; "
                f"sudo -n chown -R root:root {UPDATE_INCOMING}/{name}; "
                f"sudo -n test -f {UPDATE_INCOMING}/{name}/manifest.json; "
                f"sudo -n rm -f {remote}",
                timeout=180,
            )

    def run_update(self, name: str, timeout: float = 900) -> subprocess.CompletedProcess[str]:
        source = f"{UPDATE_INCOMING}/{name}"
        result = self.appliance.ssh(
            f"sudo -n bittern update install {shlex.quote(source)}",
            timeout=timeout,
            check=False,
        )
        output = result.stdout + result.stderr
        self.capture(f"update-{name}.txt", output)
        return result

    def latest_history(self) -> dict[str, Any]:
        path = self.appliance.ssh(
            f"sudo -n sh -c 'ls -t {UPDATE_STATE_DIR}/history/*.json | head -1'", timeout=30
        ).stdout.strip()
        if not path:
            raise HarnessError("the update history directory has no record")
        raw = self.appliance.ssh(f"sudo -n cat {shlex.quote(path)}", timeout=30).stdout
        try:
            record = json.loads(raw)
        except json.JSONDecodeError as exc:
            raise HarnessError(f"cannot parse the history record {path}: {raw!r}") from exc
        self.capture(f"history-{Path(path).name}", raw)
        return record

    def update_status(self) -> str:
        return self.appliance.ssh("sudo -n bittern update status", timeout=30).stdout

    def agent_start_log(self) -> str:
        return self.appliance.ssh(
            "sudo -n journalctl -u bitternd.service -b --no-pager -o cat "
            "| grep -F daemon.start | tail -1",
            timeout=30,
            check=False,
        ).stdout.strip()

    def assert_running_generation(self, generation: int) -> dict[str, Any]:
        pointer = self.appliance.ssh(f"sudo -n cat {UPDATE_POINTER}", timeout=30).stdout.strip()
        if pointer != str(generation):
            raise HarnessError(
                f"the activation pointer is {pointer!r}, expected generation {generation}"
            )
        expected = self.versions[generation]
        version = self.wait_version()
        if version != expected:
            raise HarnessError(
                f"the running agent reports {version!r}, expected {expected!r}"
            )
        pid = self.appliance.ssh(
            "systemctl show -p MainPID --value bitternd.service", timeout=30
        ).stdout.strip()
        executable = self.appliance.ssh(
            f"sudo -n readlink /proc/{pid}/exe", timeout=30
        ).stdout.strip()
        expected_path = f"/var/lib/bittern/app/bundles/{generation}/files/usr/bin/bitternd"
        if executable != expected_path:
            raise HarnessError(
                f"the running agent executable is {executable!r}, expected {expected_path!r}"
            )
        status = self.update_status()
        if f"committed: generation {generation} ({expected})" not in status:
            raise HarnessError(f"update status does not name generation {generation}:\n{status}")
        if "operation: none" not in status:
            raise HarnessError(f"an update journal is still active:\n{status}")
        record = self.latest_history()
        if record.get("to", {}).get("generation") != generation:
            raise HarnessError(f"the newest history record names the wrong candidate: {record}")
        if record.get("result") != "committed":
            raise HarnessError(
                f"the newest history result is {record.get('result')!r}, expected committed"
            )
        start_log = self.agent_start_log()
        evidence = (
            f"$ cat {UPDATE_POINTER}\n{pointer}\n"
            f"$ GET /api/version\n{version}\n"
            f"$ readlink /proc/{pid}/exe\n{executable}\n"
            f"$ journalctl -u bitternd.service (daemon.start)\n{start_log}\n"
            f"$ bittern update status\n{status}\n"
        )
        self.capture(f"running-generation-{generation}.txt", evidence)
        return record

    def install_generation_1(self) -> str:
        self.stage_update_sources()
        result = self.run_update("g1")
        output = result.stdout + result.stderr
        if result.returncode != 0 or "update: committed" not in output:
            raise HarnessError(
                "the generation 1 install did not commit:\n" + output
            )
        if "from: no previous bundle" not in output:
            raise HarnessError("the generation 1 record does not name a first install:\n" + output)
        record = self.assert_running_generation(1)
        if record.get("from") is not None:
            raise HarnessError("the generation 1 history record has a previous bundle")
        return (
            f"generation 1 {self.versions[1]} committed from no previous bundle; "
            "the daemon execs the bundle binary"
        )

    def update_generation_2(self) -> str:
        result = self.run_update("g2")
        output = result.stdout + result.stderr
        if result.returncode != 0 or "update: committed" not in output:
            raise HarnessError("the generation 2 update did not commit:\n" + output)
        if "from: generation 1" not in output or "to: generation 2" not in output:
            raise HarnessError("the generation 2 record does not name 1 -> 2:\n" + output)
        record = self.assert_running_generation(2)
        if record.get("from", {}).get("generation") != 1:
            raise HarnessError("the generation 2 history record does not name generation 1")
        health = {
            item.get("name"): item.get("state") for item in record.get("health", [])
        }
        if health.get("bundle-integrity") != "passed":
            raise HarnessError(f"the generation 2 integrity gate did not pass: {health}")
        return (
            f"generation 2 {self.versions[2]} committed; the running version changed from "
            f"{self.versions[1]} to {self.versions[2]}"
        )

    def rollback_generation_3(self) -> str:
        result = self.run_update("g3")
        output = result.stdout + result.stderr
        if result.returncode == 0 or "update: rolled-back" not in output:
            raise HarnessError("the generation 3 health failure did not roll back:\n" + output)
        if "failed step: health" not in output:
            raise HarnessError("the generation 3 failure is not the health gate:\n" + output)
        if "rollback: rolled-back, restored generation 2" not in output:
            raise HarnessError("the rollback record does not restore generation 2:\n" + output)
        pointer = self.appliance.ssh(f"sudo -n cat {UPDATE_POINTER}", timeout=30).stdout.strip()
        if pointer != "2":
            raise HarnessError(f"the activation pointer is {pointer!r} after rollback, expected 2")
        version = self.wait_version()
        if version != self.versions[2]:
            raise HarnessError(
                f"the running agent reports {version!r} after rollback, expected "
                f"{self.versions[2]!r}"
            )
        status = self.update_status()
        if f"committed: generation 2 ({self.versions[2]})" not in status:
            raise HarnessError(f"update status does not restore generation 2:\n{status}")
        record = self.latest_history()
        if record.get("result") != "rolled-back":
            raise HarnessError(f"the newest history result is {record.get('result')!r}")
        if record.get("failed_step") != "health":
            raise HarnessError(f"the newest history failed step is {record.get('failed_step')!r}")
        if record.get("rollback", {}).get("restored_generation") != 2:
            raise HarnessError(f"the rollback record does not restore generation 2: {record}")
        failed_candidate = self.appliance.ssh(
            "sudo -n test -f /var/lib/bittern/app/bundles/3/files/usr/bin/"
            "post-activation-marker && echo retained || echo missing",
            timeout=30,
        ).stdout.strip()
        if failed_candidate != "retained":
            raise HarnessError("the failed generation 3 candidate was not kept for support")
        evidence = (
            f"$ bittern update install {UPDATE_INCOMING}/g3\n{output}\n"
            f"$ cat {UPDATE_POINTER}\n{pointer}\n"
            f"$ GET /api/version\n{version}\n"
            f"$ journalctl -u bitternd.service (daemon.start)\n{self.agent_start_log()}\n"
            f"$ bittern update status\n{status}\n"
        )
        self.capture("update-rollback.txt", evidence)
        return (
            "generation 3 activation failed the bundle-integrity health gate; the pointer "
            f"and the running version returned to generation 2 {self.versions[2]}"
        )

    def assert_rejection(
        self,
        name: str,
        expected_step: str,
        expected_reason: str,
        invocation: str,
    ) -> str:
        result = self.run_update(name)
        output = result.stdout + result.stderr
        if result.returncode == 0:
            raise HarnessError(
                f"the {name} bundle was accepted; it must be rejected:\n{output}"
            )
        if "update: rejected" not in output:
            raise HarnessError(f"the {name} bundle did not report a rejection:\n{output}")
        if f"failed step: {expected_step}" not in output:
            raise HarnessError(
                f"the {name} rejection is not at {expected_step}:\n{output}"
            )
        if expected_reason not in output:
            raise HarnessError(
                f"the {name} rejection does not name {expected_reason}:\n{output}"
            )
        pointer = self.appliance.ssh(f"sudo -n cat {UPDATE_POINTER}", timeout=30).stdout.strip()
        if pointer != "2":
            raise HarnessError(
                f"the {name} rejection moved the activation pointer to {pointer!r}"
            )
        version = self.wait_version()
        if version != self.versions[2]:
            raise HarnessError(
                f"the {name} rejection changed the running version to {version!r}"
            )
        after = self.service_invocations(("bitternd.service",))
        if after.get("bitternd.service") != invocation:
            raise HarnessError(
                f"the {name} rejection restarted bitternd.service: "
                f"{invocation!r} -> {after.get('bitternd.service')!r}"
            )
        self.appliance.ssh(f"sudo -n test ! -e {UPDATE_STATE_DIR}/journal.json", timeout=30)
        self.appliance.ssh(f"sudo -n test ! -e {UPDATE_STATE_DIR}/request.json", timeout=30)
        status = self.update_status()
        if f"committed: generation 2 ({self.versions[2]})" not in status:
            raise HarnessError(f"the {name} rejection changed the committed state:\n{status}")
        record = self.latest_history()
        if record.get("result") != "rejected":
            raise HarnessError(
                f"the {name} newest history result is {record.get('result')!r}, expected rejected"
            )
        if record.get("failed_step") != expected_step:
            raise HarnessError(
                f"the {name} history failed step is {record.get('failed_step')!r}"
            )
        return (
            f"{name}: rejected at {expected_step} ({expected_reason}) with the pointer, "
            "committed state and running unit unchanged"
        )

    def reject_invalid_bundles(self) -> str:
        invocation = self.service_invocations(("bitternd.service",)).get("bitternd.service", "")
        if not invocation:
            raise HarnessError("cannot read the bitternd.service InvocationID before rejection")
        evidence = [
            self.assert_rejection(
                "tampered-payload",
                "verify-payload",
                "payload.digest",
                invocation,
            ),
            self.assert_rejection(
                "bad-signature", "verify-manifest", "signature.invalid", invocation
            ),
            self.assert_rejection(
                "g1", "verify-manifest", "generation.rollback", invocation
            ),
        ]
        self.capture("update-rejections.txt", "\n".join(evidence) + "\n")
        return "; ".join(evidence)

    def assert_update_state(self) -> str:
        status = self.update_status()
        journal = self.appliance.ssh(
            f"sudo -n test -e {UPDATE_STATE_DIR}/journal.json && echo present || echo absent",
            timeout=30,
        ).stdout.strip()
        if journal != "absent":
            raise HarnessError("an update journal remains after the scenario")
        paths = self.appliance.ssh(
            f"sudo -n sh -c 'ls {UPDATE_STATE_DIR}/history/*.json'", timeout=30
        ).stdout.split()
        results = []
        for path in paths:
            raw = self.appliance.ssh(f"sudo -n cat {shlex.quote(path)}", timeout=30).stdout
            try:
                results.append(json.loads(raw).get("result"))
            except json.JSONDecodeError as exc:
                raise HarnessError(f"cannot parse the history record {path}: {raw!r}") from exc
        counts = {name: results.count(name) for name in ("committed", "rolled-back", "rejected")}
        if counts != {"committed": 2, "rolled-back": 1, "rejected": 3}:
            raise HarnessError(f"unexpected update history results: {counts} ({results})")
        pointer = self.appliance.ssh(f"sudo -n cat {UPDATE_POINTER}", timeout=30).stdout.strip()
        evidence = (
            f"$ cat {UPDATE_POINTER}\n{pointer}\n"
            f"$ bittern update status\n{status}\n"
            f"history results: {json.dumps(counts, sort_keys=True)}\n"
        )
        self.capture("update-final.txt", evidence)
        return (
            "two commits, one rollback and three rejections recorded; the pointer is on "
            f"generation {pointer} with no journal"
        )


def update(args: argparse.Namespace) -> int:
    config = load_config()
    require_tools()
    ensure_directories()
    appliance = Vm("appliance", config)
    camera = Vm("camera", config)
    with guest_lifecycle(args, appliance, camera, config):
        return run_update_scenario(config, appliance, camera)


def run_update_scenario(
    config: dict[str, Any],
    appliance: Vm,
    camera: Vm,
) -> int:
    accelerator, reason = choose_accelerator(config)
    log(f"accelerator: {accelerator} ({reason})")
    scenario = UpdateScenario(appliance, camera, config, accelerator)
    try:
        return scenario.run()
    except Exception as exc:
        if not scenario.failed_phase:
            scenario.failed_phase = "setup"
            scenario.write_summary("fail", scenario.redact(str(exc)))
        scenario.capture_failure()
        if isinstance(exc, HarnessError):
            raise
        raise HarnessError(
            f"{type(exc).__name__}: {scenario.redact(str(exc))}; "
            f"logs: {scenario.log_dir}"
        ) from exc


def adoption(args: argparse.Namespace) -> int:
    config = load_config()
    require_tools()
    ensure_directories()
    appliance = Vm("appliance", config)
    camera = Vm("camera", config)
    with guest_lifecycle(args, appliance, camera, config):
        return run_adoption_scenario(config, appliance, camera)


def run_adoption_scenario(
    config: dict[str, Any],
    appliance: Vm,
    camera: Vm,
) -> int:
    accelerator, reason = choose_accelerator(config)
    log(f"accelerator: {accelerator} ({reason})")
    scenario = AdoptionScenario(appliance, camera, config, accelerator)
    try:
        return scenario.run()
    except Exception as exc:
        if not scenario.failed_phase:
            scenario.failed_phase = "setup"
            scenario.write_summary("fail", scenario.redact(str(exc)))
        scenario.capture_failure()
        if isinstance(exc, HarnessError):
            raise
        raise HarnessError(
            f"{type(exc).__name__}: {scenario.redact(str(exc))}; "
            f"logs: {scenario.log_dir}"
        ) from exc


def firstboot(args: argparse.Namespace) -> int:
    config = load_config()
    require_tools()
    ensure_directories()
    appliance = Vm("appliance", config)
    camera = Vm("camera", config)
    with guest_lifecycle(args, appliance, camera, config):
        return run_firstboot_scenario(
            config,
            appliance,
            camera,
            stock_names=getattr(args, "stock_names", False),
            clean_network=getattr(args, "clean_network", False),
            late_camera=getattr(args, "late_camera", False),
        )


def run_firstboot_scenario(
    config: dict[str, Any],
    appliance: Vm,
    camera: Vm,
    *,
    stock_names: bool = False,
    clean_network: bool = False,
    late_camera: bool = False,
) -> int:
    accelerator, reason = choose_accelerator(config)
    log(f"accelerator: {accelerator} ({reason})")
    if late_camera:
        scenario: FirstbootScenario = LateCameraScenario(
            appliance, camera, config, accelerator
        )
    elif clean_network:
        scenario: FirstbootScenario = CleanNetworkScenario(
            appliance, camera, config, accelerator
        )
    elif stock_names:
        scenario = StockNameScenario(appliance, camera, config, accelerator)
    else:
        scenario = FirstbootScenario(appliance, camera, config, accelerator)
    try:
        return scenario.run()
    except Exception as exc:
        if not scenario.failed_phase:
            scenario.failed_phase = "setup"
            scenario.write_summary("fail", scenario.redact(str(exc)))
        scenario.capture_failure()
        if isinstance(exc, HarnessError):
            raise
        raise HarnessError(
            f"{type(exc).__name__}: {scenario.redact(str(exc))}; "
            f"logs: {scenario.log_dir}"
        ) from exc


def prepare(args: argparse.Namespace) -> int:
    config = load_config()
    require_tools()
    ensure_directories()
    for name in ("appliance", "camera"):
        vm = Vm(name, config)
        if vm.running():
            raise HarnessError(f"{name} is running; stop it before prepare --force")
    base_image = fetch_image(config, refresh=args.refresh_image)
    ssh_key = ensure_ssh_key(config)
    public_key = ssh_key.with_suffix(".pub").read_text(encoding="utf-8").strip()
    render_cloud_init(config, public_key)
    state = load_state()
    recorded_image = state.get("image_sha256")
    force = args.force or (
        recorded_image is not None and recorded_image != config["image"]["sha256"]
    )
    if force and recorded_image is not None and recorded_image != config["image"]["sha256"]:
        log("base image changed; recreating overlays")
    for name in ("appliance", "camera"):
        create_overlay(config, base_image, name, force=force)
    if force:
        # Fresh overlays have no baseline, so any timing from an older run is stale.
        state.pop("cold_boot_seconds", None)
        state.pop("snapshot_seconds", None)
    state["image_sha256"] = config["image"]["sha256"]
    state["prepared_at"] = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())
    save_state(state)
    log(f"prepared overlays in {VMS} and seeds in {SEEDS}")
    return 0


def bootstrap(args: argparse.Namespace) -> int:
    config = load_config()
    require_tools()
    ensure_directories()
    appliance = Vm("appliance", config)
    camera = Vm("camera", config)
    with guest_lifecycle(args, appliance, camera, config):
        return bootstrap_guests(args, config, appliance, camera)


def bootstrap_guests(
    args: argparse.Namespace,
    config: dict[str, Any],
    appliance: Vm,
    camera: Vm,
) -> int:
    if not all(
        path.exists()
        for path in (appliance.overlay, camera.overlay, appliance.seed, camera.seed)
    ):
        prepare(argparse.Namespace(force=False, refresh_image=False))
    accelerator, reason = choose_accelerator(config)
    limits = budgets(config, accelerator)
    log(f"accelerator: {accelerator} ({reason})")
    started = time.monotonic()
    start_all(appliance, camera, config, accelerator, load_snapshot=not args.cold)
    cold = not (appliance.has_snapshot() and camera.has_snapshot()) or args.cold
    wait_ready(appliance, camera, config, accelerator, limits["boot"] if cold else limits["snapshot_restore"], cold_boot=cold)
    if cold:
        seconds = time.monotonic() - started
        if seconds > limits["boot"]:
            raise HarnessError(f"cold boot took {seconds:.1f}s, budget is {limits['boot']}s")
        log(f"cold boot: {seconds:.1f}s (budget {limits['boot']}s)")
        snapshot_seconds = save_baseline(appliance, camera)
        state = load_state()
        state["cold_boot_seconds"] = round(seconds, 2)
        state["snapshot_seconds"] = round(snapshot_seconds, 2)
        save_state(state)
    else:
        log(
            f"snapshot restore: {time.monotonic() - started:.1f}s "
            f"(budget {limits['snapshot_restore']}s)"
        )
    return 0


def reset(args: argparse.Namespace) -> int:
    config = load_config()
    appliance = Vm("appliance", config)
    camera = Vm("camera", config)
    if not (appliance.running() and camera.running()):
        raise HarnessError("guests are not running; run 'run.py boot' first")
    if not (appliance.has_snapshot() and camera.has_snapshot()):
        raise HarnessError("baseline snapshot is missing; run 'run.py boot' first")
    accelerator, _ = choose_accelerator(config)
    limits = budgets(config, accelerator)
    started = time.monotonic()
    load_baseline(appliance, camera)
    wait_ready(appliance, camera, config, accelerator, limits["snapshot_restore"], cold_boot=False)
    log(
        f"snapshot restore: {time.monotonic() - started:.1f}s "
        f"(budget {limits['snapshot_restore']}s)"
    )
    return 0


def stop(args: argparse.Namespace) -> int:
    config = load_config()
    if args.all:
        processes = scan_qemu_processes()
        selected = [
            process
            for process in processes
            if process_worktree_is_gone(process) or process_in_this_worktree(process)
        ]
        active = [
            process
            for process in processes
            if not process_worktree_is_gone(process) and not process_in_this_worktree(process)
        ]
        print("Bittern QEMU processes selected by --all (worktree gone or this worktree):")
        if selected:
            for process in selected:
                print(f"  {process.describe()}")
        else:
            print("  none")
        if active:
            print("Bittern QEMU processes in other existing worktrees (not selected):")
            for process in active:
                print(f"  {process.describe()}")
        if not selected:
            log("no guests to stop")
            return 0
        if not args.yes:
            try:
                answer = input(f"Stop {len(selected)} Bittern QEMU process(es)? [y/N] ")
            except EOFError:
                answer = ""
            if answer.strip().lower() not in ("y", "yes"):
                log("not stopped; rerun with --yes to confirm")
                return 0
        stopped = 0
        failed = 0
        for process in selected:
            try:
                if stop_qemu_process(process):
                    stopped += 1
            except HarnessError as exc:
                failed += 1
                log(f"warning: {exc}")
        if any(process_in_this_worktree(process) for process in selected):
            for name in VM_MARKERS:
                (RUN / f"{name}.pid").unlink(missing_ok=True)
            state = load_state()
            state.pop("vms", None)
            state.pop("started_at", None)
            save_state(state)
        log(f"stopped {stopped} guest(s)")
        return 1 if failed else 0

    appliance = Vm("appliance", config)
    camera = Vm("camera", config)
    stopped = stop_all(appliance, camera, config)
    if stopped:
        log(f"stopped {len(stopped)} guest(s) in {ROOT}")
    else:
        log(f"no running Bittern QEMU processes in {ROOT}")
    others = [
        process for process in scan_qemu_processes() if not process_in_this_worktree(process)
    ]
    if others:
        log(
            f"{len(others)} other Bittern QEMU process(es) are running; "
            f"run '{RUN_COMMAND} status' to list them"
        )
    return 0


def status(args: argparse.Namespace) -> int:
    config = load_config()
    print(f"worktree: {ROOT}")
    processes = scan_qemu_processes()
    if processes:
        print("bittern qemu processes:")
        for process in processes:
            labels = []
            if process_in_this_worktree(process):
                labels.append("this worktree")
            if process_worktree_is_gone(process):
                labels.append("orphan, worktree gone")
            suffix = f" ({', '.join(labels)})" if labels else ""
            print(f"  {process.describe()}{suffix}")
    else:
        print("bittern qemu processes: none")
    state = load_state()
    print(f"accelerator: {state.get('accelerator', 'not started')}")
    print(f"started_at: {state.get('started_at', 'not started')}")
    for name in ("appliance", "camera"):
        vm = Vm(name, config)
        print(
            f"{name}: pidfile={vm.pid()} running={vm.running()} "
            f"snapshot={vm.has_snapshot()} serial={vm.serial_log}"
        )
    return 0


def console(args: argparse.Namespace) -> int:
    config = load_config()
    vm = Vm(args.vm, config)
    if not vm.serial_log.exists():
        raise HarnessError(f"serial log {vm.serial_log} does not exist")
    lines = vm.serial_log.read_text(encoding="utf-8", errors="replace").splitlines()
    print(f"# serial console: {vm.serial_log}")
    print("\n".join(lines[-args.lines :]))
    return 0


def probe_port(port: int, request: bytes, timeout: float = 6.0) -> tuple[str, str]:
    """Probe a host port that QEMU user networking forwards into a guest.

    QEMU's user-mode network stack completes the host-side TCP handshake itself, so
    connect() succeeds even when the guest drops the packet. A connection that is
    accepted or refused by the guest answers or resets immediately; a connection the
    guest firewall drops produces no answer at all. The distinction matters here:
    'no response' is the evidence that nftables dropped the packet.
    """
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as probe:
        probe.settimeout(3)
        try:
            probe.connect(("127.0.0.1", port))
        except OSError as exc:
            return "no-route", exc.strerror or str(exc)
        try:
            probe.sendall(request)
            probe.settimeout(timeout)
            response = probe.recv(256)
        except socket.timeout:
            return "dropped", f"no answer within {timeout:.0f}s"
        except ConnectionResetError:
            return "refused-by-guest", "the guest stack answered with a reset"
        except OSError as exc:
            return "closed", exc.strerror or str(exc)
        if not response:
            return "closed", "the guest closed the connection"
        first = response.split(b"\r\n", 1)[0].decode("utf-8", "replace")
        return "answered", first


def load_harness_rules() -> str:
    """Take the renderer's four-camera golden and add the harness SSH rule."""
    if not RENDERED_NFTABLES.exists():
        raise HarnessError(f"rendered nftables output missing: {RENDERED_NFTABLES}")
    rules = RENDERED_NFTABLES.read_text(encoding="utf-8")
    anchor = '        iifname "lo" accept\n'
    if rules.count(anchor) != 1:
        raise HarnessError(
            f"rendered nftables output must contain exactly one loopback accept in {RENDERED_NFTABLES}"
        )
    management = (
        "\n"
        "        # QEMU harness management only; the renderer does not emit this rule.\n"
        f"        {HARNESS_SSH_RULE}\n"
    )
    return rules.replace(anchor, anchor + management, 1)


def nft_rule_lines(text: str) -> list[str]:
    """Normalize nft output for comparison, dropping comments and blank lines.

    nft may reorder the elements of an anonymous set, for example the ICMPv6
    neighbour-discovery types. The set is unordered, so sort its elements before
    comparing the rendered input with the live table.
    """
    lines = []
    for line in text.splitlines():
        stripped = line.strip()
        if not stripped or stripped.startswith("#"):
            continue
        normalized = " ".join(stripped.split())

        def sort_set(match: re.Match[str]) -> str:
            elements = [element.strip() for element in match.group(1).split(",")]
            return "{ " + ", ".join(sorted(elements)) + " }"

        lines.append(re.sub(r"\{([^{}]*)\}", sort_set, normalized))
    return lines


def assert_rendered_rules_loaded(loaded: str, expected: str) -> None:
    """Compare the guest's live table with the rendered table, ignoring the SSH rule."""
    expected_lines = [
        line for line in nft_rule_lines(expected) if HARNESS_SSH_RULE not in line
    ]
    loaded_lines = [
        line for line in nft_rule_lines(loaded) if HARNESS_SSH_RULE not in line
    ]
    for index, (want, got) in enumerate(zip(expected_lines, loaded_lines), 1):
        if want != got:
            raise HarnessError(
                "loaded nftables differ from the rendered output at rule "
                f"{index}: expected {want!r}, got {got!r}"
            )
    if len(expected_lines) != len(loaded_lines):
        raise HarnessError(
            "loaded nftables have "
            f"{len(loaded_lines)} rules; the rendered output has {len(expected_lines)}"
        )


def firewall(args: argparse.Namespace) -> int:
    config = load_config()
    appliance = Vm("appliance", config)
    if not appliance.running():
        raise HarnessError("appliance is not running; run 'run.py boot' first")
    rules = load_harness_rules()
    # Reloading the same table name appends chains and fails, so drop the old tables first.
    # bittern_fixture is the name used by the hand-written fixture this replaced.
    appliance.ssh("sudo -n nft delete table inet bittern 2>/dev/null || true", timeout=30)
    appliance.ssh(
        "sudo -n nft delete table inet bittern_fixture 2>/dev/null || true", timeout=30
    )
    appliance.ssh("sudo -n nft -c -f -", input_text=rules, timeout=30)
    appliance.ssh("sudo -n nft -f -", input_text=rules, timeout=30)
    log("applied rendered nftables rules in the appliance guest (plus the harness SSH rule)")
    # The rendered table has no SSH rule; the harness added one for lan0. If that
    # rule is wrong this command is the last one that can run before the guest is unreachable.
    appliance.ssh("true", timeout=15)
    log("management SSH still works through the substituted lan0 rule")

    loaded = appliance.ssh("sudo -n nft list table inet bittern", timeout=30).stdout
    assert_rendered_rules_loaded(loaded, rules)
    log("guest loaded the rendered bittern table (harness SSH rule ignored)")

    listeners = appliance.ssh("ss -lnt", timeout=15).stdout
    frigate_listeners = [line for line in listeners.splitlines() if ":8971" in line]
    if not frigate_listeners or not any("127.0.0.1:8971" in line for line in frigate_listeners):
        raise HarnessError(f"Frigate is not listening on 127.0.0.1:8971:\n{listeners}")
    for line in frigate_listeners:
        if "0.0.0.0:8971" in line or "[::]:8971" in line:
            raise HarnessError(f"Frigate UI is listening beyond loopback: {line.strip()}")
    log("  Frigate UI listens on 127.0.0.1:8971 only")

    # In the pinned 0.18.2 image /api/version answers 401, so the HTTP answer is
    # the unauthenticated positive check for the loopback UI. The fixture serves
    # the login page; the applied product config may answer 400 when plain HTTP
    # reaches its TLS listener, which still proves the loopback listener is up.
    status = appliance.ssh(
        "curl -sS --max-time 5 -o /dev/null -w '%{http_code}' "
        "http://127.0.0.1:8971/",
        timeout=15,
    ).stdout.strip()
    if not re.fullmatch(r"[1-5]\d\d", status):
        raise HarnessError(
            f"Frigate UI did not answer on 127.0.0.1:8971: HTTP status {status!r}"
        )
    log(f"  frigate loopback 8971 inside guest: reachable (HTTP {status})")

    forwards = config["network"]["lan"]["hostfwd"]
    # These forwards exist so the negative assertions cannot pass merely because
    # the host has no route to the guest. nftables must drop the packets.
    checks = (
        ("frigate-internal", forwards["frigate_internal"], b"GET / HTTP/1.0\r\n\r\n"),
        ("go2rtc", forwards["go2rtc"], b"GET / HTTP/1.0\r\n\r\n"),
        (
            "rtsp",
            forwards["rtsp"],
            b"OPTIONS rtsp://127.0.0.1:8554/cam0 RTSP/1.0\r\nCSeq: 1\r\n\r\n",
        ),
        ("webrtc", forwards["webrtc"], b"GET / HTTP/1.0\r\n\r\n"),
    )
    for name, port, request in checks:
        outcome, detail = probe_port(int(port), request)
        if outcome != "dropped":
            raise HarnessError(
                f"{name} on host 127.0.0.1:{port} produced {outcome} ({detail}); "
                "a dropped packet produces no answer, so the firewall is not the boundary"
            )
        log(f"  {name} on host 127.0.0.1:{port}: dropped by nftables ({detail}) (expected)")
    return 0


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    subparsers = parser.add_subparsers(dest="command", required=True)

    prepare_parser = subparsers.add_parser("prepare", help="fetch the image and build overlays and seeds")
    prepare_parser.add_argument("--force", action="store_true", help="recreate overlays and seeds")
    prepare_parser.add_argument("--refresh-image", action="store_true", help="fetch the image again")
    prepare_parser.set_defaults(function=prepare)

    boot_parser = subparsers.add_parser(
        "boot", help="boot the guests, save baseline snapshots, then stop"
    )
    boot_parser.add_argument("--cold", action="store_true", help="ignore an existing snapshot")
    boot_parser.add_argument(
        "--keep", action="store_true", help="leave the guests running for inspection"
    )
    boot_parser.set_defaults(function=bootstrap)

    test_parser = subparsers.add_parser(
        "test", help="boot if needed, run the harness tests, then stop"
    )
    test_parser.add_argument(
        "--keep", action="store_true", help="leave the guests running for inspection"
    )
    test_parser.set_defaults(function=run_tests)
    firstboot_parser = subparsers.add_parser(
        "firstboot",
        help="run first boot, the TLS wizard and one Frigate camera, then stop",
    )
    firstboot_parser.add_argument(
        "--keep", action="store_true", help="leave the guests running for inspection"
    )
    firstboot_parser.add_argument(
        "--stock-names",
        action="store_true",
        help="start from stock NIC names and exercise the .link rename path",
    )
    firstboot_parser.add_argument(
        "--clean-network",
        action="store_true",
        help=(
            "start from stock NIC names with no .network or .link files, an empty "
            "/etc/resolv.conf, and external DNS black-holed"
        ),
    )
    firstboot_parser.add_argument(
        "--late-camera",
        action="store_true",
        help=(
            "reboot after install and run first boot from the boot transaction with "
            "cam0 absent until the apply renames it; assert generation 1 and no port "
            "53 listener"
        ),
    )
    firstboot_parser.set_defaults(function=firstboot)

    update_parser = subparsers.add_parser(
        "update",
        help=(
            "install the beta kit, commit two signed bundles, roll back a failing "
            "health gate, and reject a tampered payload, a bad signature and a downgrade"
        ),
    )
    update_parser.add_argument(
        "--keep", action="store_true", help="leave the guests running for inspection"
    )
    update_parser.set_defaults(function=update)

    adoption_parser = subparsers.add_parser(
        "adoption",
        help="adopt a DHCP camera through discovery, credentials, a reservation and a snapshot",
    )
    adoption_parser.add_argument(
        "--keep", action="store_true", help="leave the guests running for inspection"
    )
    adoption_parser.set_defaults(function=adoption)


    reset_parser = subparsers.add_parser("reset", help="restore the baseline snapshot")
    reset_parser.set_defaults(function=reset)

    stop_parser = subparsers.add_parser("stop", help="stop both guests, including missing pidfiles")
    stop_parser.add_argument(
        "--all",
        action="store_true",
        help="stop orphaned guests whose worktree directory is gone",
    )
    stop_parser.add_argument(
        "--yes", action="store_true", help="do not ask for confirmation with --all"
    )
    stop_parser.set_defaults(function=stop)

    status_parser = subparsers.add_parser(
        "status", help="show guest state and scan /proc for Bittern QEMU processes"
    )
    status_parser.set_defaults(function=status)

    console_parser = subparsers.add_parser("console", help="print recent serial console output")
    console_parser.add_argument("--vm", choices=("appliance", "camera"), default="appliance")
    console_parser.add_argument("--lines", type=int, default=100)
    console_parser.set_defaults(function=console)

    firewall_parser = subparsers.add_parser(
        "firewall", help="apply the rendered nftables and probe lan0 ports"
    )
    firewall_parser.set_defaults(function=firewall)

    args = parser.parse_args()
    try:
        return int(args.function(args))
    except HarnessError as exc:
        print(f"qemu-harness: error: {exc}", file=sys.stderr)
        return 1
    except KeyboardInterrupt:
        print("qemu-harness: interrupted", file=sys.stderr)
        return 130


if __name__ == "__main__":
    raise SystemExit(main())

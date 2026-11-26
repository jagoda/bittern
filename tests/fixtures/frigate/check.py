#!/usr/bin/env python3
"""Static checks for the Frigate fixture bundle.

This does not need Docker. It fails when the manifest lacks a real digest, when a compose file names
a floating image, when host networking is missing, when ports are published, or when the fixture
config and rendered firewall drift from their required shape.
"""

from __future__ import annotations

import json
import re
import shutil
import sys
import tempfile
from pathlib import Path

BASE = Path(__file__).resolve().parent
ROOT = BASE.parents[2]
RENDERED_NFTABLES = ROOT / "tests" / "render" / "four-cameras" / "expected" / "nftables" / "bittern.nft"
DIGEST = re.compile(r"^sha256:[0-9a-f]{64}$")
FLOATING_TAGS = {"latest", "stable", "dev", "main", "master", "edge"}


class CheckError(RuntimeError):
    pass


def require(condition: bool, message: str) -> None:
    if not condition:
        raise CheckError(message)


def read(path: Path) -> str:
    try:
        return path.read_text(encoding="utf-8")
    except OSError as exc:
        raise CheckError(f"cannot read {path}: {exc}") from exc


def validate(base: Path = BASE) -> list[str]:
    manifest = json.loads(read(base / "manifest.json"))
    image = manifest["image"]
    tag = str(image.get("tag", ""))
    digest = str(image.get("digest", ""))
    reference = str(image["reference"])
    registry = str(image["registry"])
    repository = str(image["repository"])

    require(tag.lower() not in FLOATING_TAGS, f"image tag is floating: {tag}")
    require(bool(DIGEST.fullmatch(digest)), f"image digest is not sha256:<64 hex>: {digest}")
    require(
        reference == f"{registry}/{repository}:{tag}@{digest}",
        f"image reference does not match repository:tag@digest: {reference}",
    )
    require(manifest["image"]["platform"] == "linux/amd64", "image platform must be linux/amd64")
    require(
        manifest["config"]["check_command"].startswith("docker run "),
        "config check must use the upstream docker run invocation",
    )
    require(
        "--entrypoint python3" in manifest["config"]["check_command"]
        and "-u -m frigate --validate-config" in manifest["config"]["check_command"],
        "config check is missing the upstream python3 -m frigate --validate-config invocation",
    )

    for compose_name in ("compose.yaml", "compose.qemu.yaml"):
        text = read(base / compose_name)
        require(reference in text, f"{compose_name} does not pin {reference}")
        require("network_mode: host" in text, f"{compose_name} must use host networking")
        require(
            re.search(r"^\s*ports\s*:", text, re.MULTILINE) is None,
            f"{compose_name} must not publish ports",
        )
        require("5000:5000" not in text, f"{compose_name} appears to publish 5000")
        require("1984:1984" not in text, f"{compose_name} appears to publish 1984")
        for required in ("shm_size:", "restart: unless-stopped", "max-size:", "max-file:"):
            require(required in text, f"{compose_name} is missing {required}")

    hardware = read(base / "compose.yaml")
    for required in ("/dev/dri", "/dev/bus/usb", "group_add:", '- "44"', '- "992"'):
        require(required in hardware, f"compose.yaml is missing hardware setting {required}")
    for forbidden in ("- video", "- render"):
        require(forbidden not in hardware, f"compose.yaml still uses group name {forbidden}")

    qemu = read(base / "compose.qemu.yaml")
    require("devices:" not in qemu, "compose.qemu.yaml must omit hardware devices")
    require("group_add:" not in qemu, "compose.qemu.yaml must omit hardware groups")

    config = read(base / "config" / "config.yml")
    for required in (
        "cameras:",
        "cam0:",
        "rtsp://10.77.0.20:8554/cam0",
        "internal: 127.0.0.1:5000",
        "external: 127.0.0.1:8971",
        "path: /media/frigate/frigate.db",
        "version_check: false",
        "auth:",
        "reset_admin_password: true",
    ):
        require(required in config, f"config/config.yml is missing {required}")
    require("0.0.0.0:8971" not in config, "config/config.yml exposes the Frigate UI on all addresses")

    require(
        not (base / "nftables").exists(),
        "bundle must not keep a second firewall copy; use the renderer golden",
    )
    firewall = read(RENDERED_NFTABLES)
    declared = str(manifest["firewall"]["file"])
    require(
        (ROOT / declared).resolve() == RENDERED_NFTABLES.resolve(),
        f"manifest firewall file is not the renderer golden: {declared}",
    )
    require("table inet bittern {" in firewall, "rendered firewall has the wrong table name")
    require("policy drop;" in firewall, "rendered firewall must default to drop")
    require(
        "ADR 0034" in firewall,
        "rendered firewall must name ADR 0034 for the closed video path",
    )
    for iface in ("lan0", "cam0"):
        require(
            f'iifname "{iface}" tcp dport {{ 1984, 5000, 8554, 8555, 8971 }} drop' in firewall,
            f"rendered firewall does not explicitly block the Frigate ports on {iface}",
        )
        require(
            f'iifname "{iface}" udp dport 8555 drop' in firewall,
            f"rendered firewall does not explicitly block WebRTC UDP on {iface}",
        )
    for forbidden in (
        'iifname "lan0" tcp dport 8971 accept',
        'iifname "lan0" tcp dport 8555 accept',
        'iifname "lan0" udp dport 8555 accept',
        'iifname "cam0" tcp dport 443 accept',
    ):
        require(forbidden not in firewall, f"rendered firewall still accepts {forbidden}")
    require(
        'iifname "lan0" tcp dport 443 accept' in firewall,
        "rendered firewall does not allow the Bittern UI on lan0 tcp 443",
    )

    allowed = manifest["firewall"].get("lan0_allowed_tcp_ports")
    require(
        allowed == [443],
        "the product lan0 service is the Bittern UI on 443, not the harness SSH rule",
    )
    require(
        manifest["firewall"].get("harness_management_tcp_port") == 22,
        "manifest must name the harness-only SSH management port",
    )

    sbom = manifest.get("sbom", {})
    require(sbom.get("status") == "not-generated", "SBOM must not be claimed unless generated")
    require(bool(sbom.get("source")), "manifest must say where the SBOM will come from")
    return [
        f"image {reference}",
        "compose files use host networking and pin the digest",
        "config fixture has one camera and loopback-only Frigate listeners",
        "firewall is the renderer golden and allows the Bittern UI on 443, not 8971",
        "SBOM is explicitly not generated",
    ]


def self_test() -> None:
    with tempfile.TemporaryDirectory(prefix="bittern-frigate-check-") as temporary:
        copy = Path(temporary) / "bundle"
        shutil.copytree(BASE, copy)
        manifest_path = copy / "manifest.json"
        manifest = json.loads(read(manifest_path))
        del manifest["image"]["digest"]
        manifest_path.write_text(json.dumps(manifest, indent=2) + "\n", encoding="utf-8")
        try:
            validate(copy)
        except (CheckError, KeyError) as exc:
            print(f"self-test: missing digest rejected: {exc}")
            return
        raise CheckError("self-test failed: a manifest without a digest was accepted")


def main() -> int:
    try:
        if "--self-test" in sys.argv[1:]:
            self_test()
            return 0
        checks = validate()
        for check in checks:
            print(f"bundle-check: ok: {check}")
        return 0
    except (CheckError, KeyError, json.JSONDecodeError) as exc:
        print(f"bundle-check: error: {exc}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())

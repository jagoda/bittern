#!/usr/bin/env python3
"""Reference validator for Bittern's appliance.yaml schema version 2.

This is a development check, not the product CLI. It uses a real JSON Schema
validator for structural rules and a small set of explicit cross-field checks
for semantic rules. See schema/validation.md for the contract.

Usage:
    python3 tests/checks/validate_config.py [CONFIG]
    python3 tests/checks/validate_config.py --suite tests/schema/cases.json

Each error is printed as: JSON_PATH: RULE: MESSAGE
The script never reads secrets and never accesses the network.
"""

from __future__ import annotations

import argparse
import copy
import ipaddress
import json
import posixpath
import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
SCHEMA_PATH = ROOT / "schema" / "appliance.schema.json"
EXAMPLE_PATH = ROOT / "schema" / "examples" / "four-cameras.yaml"
CASES_PATH = ROOT / "tests" / "schema" / "cases.json"

try:
    import yaml
except ModuleNotFoundError as exc:
    missing = "python3-yaml" if exc.name == "yaml" else exc.name
    print(
        f"dependency: missing {missing}; install with: "
        "sudo apt-get install python3-yaml python3-jsonschema",
        file=sys.stderr,
    )
    raise SystemExit(2)

try:
    from jsonschema import Draft202012Validator, FormatChecker
except ModuleNotFoundError as exc:
    missing = "python3-jsonschema" if exc.name == "jsonschema" else exc.name
    print(
        f"dependency: missing {missing}; install with: "
        "sudo apt-get install python3-yaml python3-jsonschema",
        file=sys.stderr,
    )
    raise SystemExit(2)


def json_path(parts: list[object]) -> str:
    path = "$"
    for part in parts:
        if isinstance(part, int):
            path += f"[{part}]"
        elif re.fullmatch(r"[A-Za-z_][A-Za-z0-9_]*", str(part)):
            path += f".{part}"
        else:
            path += f"[{json.dumps(str(part))}]"
    return path


def load_document(path: Path) -> object:
    with path.open(encoding="utf-8") as handle:
        return yaml.safe_load(handle)


def load_schema() -> dict:
    with SCHEMA_PATH.open(encoding="utf-8") as handle:
        return json.load(handle)


def migrate_document(document: object) -> object:
    """Apply the same adjacent migrations as the Go product validator."""
    if not isinstance(document, dict):
        return document
    version = document.get("schema_version")
    if isinstance(version, bool) or not isinstance(version, int) or version != 1:
        return document
    result = copy.deepcopy(document)
    result["schema_version"] = 2
    storage = result.get("storage")
    if not isinstance(storage, dict):
        return result
    recording = storage.get("recording")
    if not isinstance(recording, dict):
        return result
    drives: list[dict[str, object]] = []
    if "device_role" in recording:
        drives.append({"device_role": recording.pop("device_role")})
    recording["drives"] = drives
    return result


def structural_errors(document: object, schema: dict) -> list[tuple[list[object], str, str]]:
    validator = Draft202012Validator(schema, format_checker=FormatChecker())
    errors: list[tuple[list[object], str, str]] = []
    for error in validator.iter_errors(document):
        parts = list(error.path)
        if error.validator == "additionalProperties":
            for key in re.findall(r"'([^']+)'", error.message):
                errors.append((parts + [key], "schema.unknown-key", "unknown key"))
            continue
        if error.validator == "required":
            for key in re.findall(r"'([^']+)'", error.message):
                errors.append((parts + [key], "schema.required", "required property is missing"))
            continue
        errors.append((parts, f"schema.{error.validator}", error.message))
    return errors


def semantic_errors(document: dict) -> list[tuple[list[object], str, str]]:
    errors: list[tuple[list[object], str, str]] = []

    def add(parts: list[object], rule: str, message: str) -> None:
        errors.append((parts, rule, message))

    network = document["network"]
    lan = network["lan0"]
    cam = network["cam0"]
    cam_dhcp = cam["dhcp"]

    try:
        lan_net = ipaddress.ip_network(lan["subnet"], strict=True)
    except ValueError as exc:
        add(["network", "lan0", "subnet"], "network.subnet-invalid", str(exc))
        return errors

    try:
        cam_if = ipaddress.ip_interface(cam["address"])
    except ValueError as exc:
        add(["network", "cam0", "address"], "network.address-invalid", str(exc))
        return errors
    cam_net = cam_if.network

    if lan_net.overlaps(cam_net):
        add(
            ["network", "lan0", "subnet"],
            "network.subnet-overlap",
            f"lan0 subnet {lan_net} overlaps cam0 subnet {cam_net}",
        )

    def check_gateway(value: str, subnet: object, parts: list[object], label: str) -> None:
        gateway = ipaddress.ip_address(value)
        if gateway not in subnet:
            add(parts, "network.gateway-outside-subnet", f"{label} {gateway} is outside {subnet}")

    if "gateway" in lan:
        check_gateway(lan["gateway"], lan_net, ["network", "lan0", "gateway"], "lan0 gateway")
    check_gateway(
        cam_dhcp["gateway"],
        cam_net,
        ["network", "cam0", "dhcp", "gateway"],
        "cam0 DHCP gateway",
    )
    check_gateway(
        cam_dhcp["ntp_server"],
        cam_net,
        ["network", "cam0", "dhcp", "ntp_server"],
        "cam0 NTP server",
    )

    range_keys = ("pool_start", "pool_end", "reservation_start", "reservation_end")
    range_values: dict[str, ipaddress.IPv4Address] = {}
    for key in range_keys:
        address = ipaddress.ip_address(cam_dhcp[key])
        range_values[key] = address
        if address not in cam_net:
            add(
                ["network", "cam0", "dhcp", key],
                "network.dhcp-range-outside-subnet",
                f"{address} is outside {cam_net}",
            )

    if range_values["pool_start"] > range_values["pool_end"]:
        add(
            ["network", "cam0", "dhcp", "pool_start"],
            "network.dhcp-range-order",
            "pool_start must not be after pool_end",
        )
    if range_values["reservation_start"] > range_values["reservation_end"]:
        add(
            ["network", "cam0", "dhcp", "reservation_start"],
            "network.dhcp-range-order",
            "reservation_start must not be after reservation_end",
        )
    if not errors:
        pool = (int(range_values["pool_start"]), int(range_values["pool_end"]))
        reservation = (
            int(range_values["reservation_start"]),
            int(range_values["reservation_end"]),
        )
        if max(pool[0], reservation[0]) <= min(pool[1], reservation[1]):
            add(
                ["network", "cam0", "dhcp"],
                "network.dhcp-ranges-overlap",
                "the DHCP pool and reservation range must not overlap",
            )

    seen_ids: dict[str, int] = {}
    seen_addresses: dict[str, int] = {}
    seen_macs: dict[str, int] = {}
    pool = (int(range_values["pool_start"]), int(range_values["pool_end"]))
    for index, camera in enumerate(document["cameras"]):
        base = ["cameras", index]
        camera_id = camera["id"]
        if camera_id in seen_ids:
            add(
                base + ["id"],
                "cameras.duplicate-id",
                f"camera id is already used at cameras[{seen_ids[camera_id]}]",
            )
        else:
            seen_ids[camera_id] = index

        reserved = bool(camera.get("mac", "").strip())
        if reserved:
            mac = camera["mac"].strip().lower()
            if mac in seen_macs:
                add(
                    base + ["mac"],
                    "cameras.duplicate-mac",
                    f"camera MAC address is already used at cameras[{seen_macs[mac]}]",
                )
            else:
                seen_macs[mac] = index

        address_text = camera["address"]
        if address_text in seen_addresses:
            add(
                base + ["address"],
                "cameras.duplicate-address",
                f"camera address is already used at cameras[{seen_addresses[address_text]}]",
            )
        else:
            seen_addresses[address_text] = index

        address = ipaddress.ip_address(address_text)
        if address in lan_net:
            add(
                base + ["address"],
                "cameras.address-on-lan",
                f"camera address {address} is on the lan0 subnet {lan_net}",
            )
            continue
        if address not in cam_net:
            add(
                base + ["address"],
                "cameras.address-outside-cam0",
                f"camera address {address} is outside the cam0 subnet {cam_net}",
            )
            continue
        if address == cam_if.ip:
            add(
                base + ["address"],
                "cameras.address-conflicts-with-appliance",
                f"camera address {address} is the cam0 appliance address",
            )
        if address in (cam_net.network_address, cam_net.broadcast_address):
            add(
                base + ["address"],
                "cameras.address-network-or-broadcast",
                f"camera address {address} is a network or broadcast address",
            )
        if pool[0] <= int(address) <= pool[1] and not reserved:
            add(
                base + ["address"],
                "cameras.address-in-dhcp-pool",
                f"camera address {address} is inside the DHCP pool; add the camera mac for a reservation",
            )

    recording = document["storage"]["recording"]
    if "mount_point" in recording:
        mount_point = posixpath.normpath(recording["mount_point"])
        allowed_roots = ("/mnt", "/media", "/run/media")
        allowed = mount_point == "/var/lib/bittern/media" or any(
            mount_point == root or mount_point.startswith(root + "/") for root in allowed_roots
        )
        if not allowed:
            add(
                ["storage", "recording", "mount_point"],
                "storage.os-drive-path",
                "recording mount point must be /var/lib/bittern/media or under /mnt, "
                "/media, or /run/media; this path is on the OS drive",
            )

    return errors


def validate_document(document: object, schema: dict) -> list[tuple[list[object], str, str]]:
    document = migrate_document(document)
    errors = structural_errors(document, schema)
    if errors:
        return errors
    return semantic_errors(document)  # type: ignore[arg-type]


def format_error(error: tuple[list[object], str, str]) -> str:
    parts, rule, message = error
    return f"{json_path(parts)}: {rule}: {message}"


def display_path(path: Path) -> str:
    try:
        return path.resolve().relative_to(ROOT).as_posix()
    except ValueError:
        return str(path)


def run_suite(manifest_path: Path, schema: dict) -> int:
    manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
    cases = manifest.get("cases", [])
    if not cases:
        print(f"schema: {display_path(manifest_path)}: no test cases", file=sys.stderr)
        return 1

    failures = 0
    for case in cases:
        name = case["name"]
        config_path = manifest_path.parent / case["file"]
        try:
            document = load_document(config_path)
            errors = validate_document(document, schema)
        except (OSError, yaml.YAMLError) as exc:
            print(f"FAIL: {name}: cannot read {display_path(config_path)}: {exc}")
            failures += 1
            continue

        expected = sorted(
            (item["path"], item["rule"]) for item in case.get("expected", [])
        )
        actual = sorted((json_path(parts), rule) for parts, rule, _ in errors)
        if actual == expected:
            print(f"PASS: {name}")
            continue

        failures += 1
        print(f"FAIL: {name} ({display_path(config_path)})")
        if errors:
            for error in errors:
                print(f"  actual:   {format_error(error)}")
        else:
            print("  actual:   (no errors)")
        expected_paths = {path for path, _ in expected}
        actual_paths = {path for path, _ in actual}
        for path, rule in expected:
            if (path, rule) not in actual:
                print(f"  expected: {path}: {rule}")
        for path in sorted(actual_paths - expected_paths):
            print(f"  unexpected path: {path}")

    if failures:
        print(f"schema: {failures} of {len(cases)} cases failed", file=sys.stderr)
        return 1
    print(f"schema: {len(cases)} cases passed")
    return 0


def main() -> int:
    parser = argparse.ArgumentParser(description="Validate a Bittern appliance.yaml document.")
    parser.add_argument(
        "config",
        nargs="?",
        type=Path,
        default=EXAMPLE_PATH,
        help=f"configuration file to validate (default: {display_path(EXAMPLE_PATH)})",
    )
    parser.add_argument(
        "--suite",
        type=Path,
        help="run the fixture suite in this manifest instead of one configuration",
    )
    args = parser.parse_args()

    try:
        schema = load_schema()
    except (OSError, json.JSONDecodeError) as exc:
        print(f"schema: cannot read {display_path(SCHEMA_PATH)}: {exc}", file=sys.stderr)
        return 2

    if args.suite:
        return run_suite(args.suite, schema)

    try:
        document = load_document(args.config)
    except (OSError, yaml.YAMLError) as exc:
        print(f"schema: cannot read {display_path(args.config)}: {exc}", file=sys.stderr)
        return 2

    errors = validate_document(document, schema)
    for error in errors:
        print(format_error(error))
    if errors:
        return 1
    print(f"valid: {display_path(args.config)}")
    return 0


if __name__ == "__main__":
    sys.exit(main())

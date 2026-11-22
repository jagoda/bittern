#!/usr/bin/env python3
"""Unit enablement check for Bittern.

The class defect this check covers is a unit that the payload starts but never
enables: it works for the current boot and is gone after the next reboot. Two
field releases failed this way, `systemd-networkd.service` and
`bittern-frigate.service`; see docs/engineering/defect-classes.md.

It compares three sources:

- `packaging/install.sh` `enable_units` owns package and Bittern units at
  install time, and any unit the installer starts or restarts must be one it
  enables.
- `agent/internal/apply/layout.go` names the units apply restarts, and its
  `etc/systemd/system/` destination is the unit file apply renders and enables.
- `tests/qemu/run.py` holds the post-install and post-apply reboot assertion
  lists.

It fails naming the missing unit, and it fails closed: a list that cannot be
parsed is an error, never a skip. It reads repository files only, needs no
QEMU, network, or package, and runs in `make check`.
"""

from __future__ import annotations

import ast
import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
INSTALL_SCRIPT = ROOT / "packaging" / "install.sh"
APPLY_LAYOUT = ROOT / "agent" / "internal" / "apply" / "layout.go"
QEMU_HARNESS = ROOT / "tests" / "qemu" / "run.py"

UNIT = r"[A-Za-z0-9@_.:-]+\.service"
ENABLE_UNIT_RE = re.compile(r"^\s*enable_unit\s+(" + UNIT + r")\s*$", re.MULTILINE)
ENABLE_FUNCTION_RE = re.compile(r"^enable_units\(\) \{\n(.*?)^\}", re.MULTILINE | re.DOTALL)
INSTALLED_UNIT_RE = re.compile(
    r"/etc/systemd/system/(" + UNIT + r")(?![.\w-])"
)
SYSTEMCTL_START_RE = re.compile(
    r"\bsystemctl\s+(?:start|restart)\s+([\"']?)([A-Za-z0-9@_.:${}-]+)\1"
)
ASSIGNMENT_RE = re.compile(
    r"^([A-Z][A-Z0-9_]*)=(?:\"([^\"]*)\"|'([^']*)'|(\S+))\s*$", re.MULTILINE
)
APPLY_DESTINATION_RE = re.compile(
    r'Destination:\s*"etc/systemd/system/(' + UNIT + r')"'
)
APPLY_UNIT_RE = re.compile(r'Unit:\s*"(' + UNIT + r')"')

HARNESS_LISTS = (
    "BASELINE_UNITS",
    "INSTALL_ENABLED_UNITS",
    "REBOOT_ENABLED_UNITS",
    "REBOOT_ACTIVE_UNITS",
    "POST_APPLY_ENABLED_UNITS",
    "POST_APPLY_ACTIVE_UNITS",
)


class CheckError(Exception):
    pass


def read(path: Path) -> str:
    try:
        return path.read_text(encoding="utf-8")
    except OSError as error:
        raise CheckError(f"cannot read {path.relative_to(ROOT)}: {error}") from error


def parse_installer() -> tuple[set[str], set[str], set[str]]:
    """Return (units install.sh enables, installs, and starts or restarts)."""
    text = read(INSTALL_SCRIPT)
    match = ENABLE_FUNCTION_RE.search(text)
    if match is None:
        raise CheckError(
            "packaging/install.sh has no enable_units() function; the payload's "
            "one enablement owner is the source this check reads"
        )
    enabled = set(ENABLE_UNIT_RE.findall(match.group(1)))
    if not enabled:
        raise CheckError(
            "packaging/install.sh enable_units() has no enable_unit calls; "
            "refusing to pass without a unit list"
        )
    installed = set(INSTALLED_UNIT_RE.findall(text))
    assignments: dict[str, str] = {}
    for name, quoted, single, bare in ASSIGNMENT_RE.findall(text):
        assignments[name] = quoted or single or bare
    started: set[str] = set()
    for line in text.splitlines():
        if (
            "systemctl" in line
            and re.search(r"\b(?:start|restart)\b", line)
            and not SYSTEMCTL_START_RE.search(line)
        ):
            raise CheckError(
                "cannot parse a systemctl start or restart command in "
                f"packaging/install.sh: {line.strip()}"
            )
    for _, token in SYSTEMCTL_START_RE.findall(text):
        if token.startswith("$"):
            name = token[1:].strip("{}")
            if name not in assignments:
                raise CheckError(
                    f"cannot resolve ${name} in a systemctl start or restart command "
                    "in packaging/install.sh; refusing to guess the unit"
                )
            token = assignments[name]
        if "." not in token:
            token += ".service"
        if not token.endswith(".service"):
            raise CheckError(
                f"cannot interpret {token!r} as a systemd unit in packaging/install.sh"
            )
        started.add(token)
    return enabled, installed, started


def parse_apply() -> tuple[set[str], set[str]]:
    """Return (units apply renders a unit file for, units apply manages)."""
    text = read(APPLY_LAYOUT)
    rendered = set(APPLY_DESTINATION_RE.findall(text))
    if not rendered:
        raise CheckError(
            "agent/internal/apply/layout.go has no etc/systemd/system/ destination; "
            "refusing to pass without the rendered unit list"
        )
    managed = set(APPLY_UNIT_RE.findall(text))
    if not managed:
        raise CheckError(
            "agent/internal/apply/layout.go has no Unit: entries; refusing to pass "
            "without the apply-managed unit list"
        )
    return rendered, managed


def eval_sequence(node: ast.AST, env: dict[str, tuple[str, ...]]) -> tuple[str, ...]:
    if isinstance(node, (ast.Tuple, ast.List)):
        values: list[str] = []
        for element in node.elts:
            item = eval_sequence(element, env)
            if len(item) != 1:
                raise ValueError("nested sequence")
            values.append(item[0])
        return tuple(values)
    if isinstance(node, ast.Constant) and isinstance(node.value, str):
        return (node.value,)
    if isinstance(node, ast.Name):
        if node.id not in env:
            raise ValueError(f"unknown name {node.id}")
        return env[node.id]
    if isinstance(node, ast.BinOp) and isinstance(node.op, ast.Add):
        return eval_sequence(node.left, env) + eval_sequence(node.right, env)
    raise ValueError(f"unsupported expression {type(node).__name__}")


def parse_harness() -> dict[str, tuple[str, ...]]:
    text = read(QEMU_HARNESS)
    try:
        tree = ast.parse(text, filename=str(QEMU_HARNESS))
    except SyntaxError as error:
        raise CheckError(f"cannot parse tests/qemu/run.py: {error}") from error
    env: dict[str, tuple[str, ...]] = {}
    for node in tree.body:
        if not isinstance(node, ast.Assign) or len(node.targets) != 1:
            continue
        target = node.targets[0]
        if not isinstance(target, ast.Name):
            continue
        try:
            env[target.id] = eval_sequence(node.value, env)
        except ValueError:
            continue
    missing = [name for name in HARNESS_LISTS if name not in env]
    if missing:
        raise CheckError(
            "tests/qemu/run.py does not define the harness unit lists: "
            + ", ".join(missing)
        )
    return {name: env[name] for name in HARNESS_LISTS}


def compare(
    installer: set[str],
    installed: set[str],
    started: set[str],
    rendered: set[str],
    apply_managed: set[str],
    harness: dict[str, tuple[str, ...]],
) -> list[str]:
    problems: list[str] = []
    install_asserted = set(harness["INSTALL_ENABLED_UNITS"])
    post_apply_asserted = set(harness["POST_APPLY_ENABLED_UNITS"])
    baseline = set(harness["BASELINE_UNITS"])

    for unit in sorted(installer - install_asserted):
        problems.append(
            f"INSTALL_ENABLED_UNITS does not assert {unit}, which install.sh enables"
        )
    for unit in sorted(installer - post_apply_asserted):
        problems.append(
            f"POST_APPLY_ENABLED_UNITS does not assert {unit}, which install.sh enables"
        )
    for unit in sorted(rendered - post_apply_asserted):
        problems.append(
            f"POST_APPLY_ENABLED_UNITS does not assert {unit}, which apply renders"
        )
    for unit in sorted(installed - installer):
        problems.append(
            f"install.sh installs {unit} but enable_units does not enable it"
        )
    for unit in sorted(started - installer):
        problems.append(
            f"install.sh starts or restarts {unit} but enable_units does not enable it"
        )
    for unit in sorted(apply_managed - installer - rendered):
        problems.append(
            f"apply manages {unit} but no stage enables it: install.sh does not enable "
            "it and apply does not render a unit file for it"
        )
    for unit in sorted(set(harness["REBOOT_ACTIVE_UNITS"]) - set(harness["REBOOT_ENABLED_UNITS"])):
        problems.append(
            f"REBOOT_ACTIVE_UNITS asserts {unit} active but REBOOT_ENABLED_UNITS does not "
            "assert it enabled"
        )
    for unit in sorted(
        set(harness["POST_APPLY_ACTIVE_UNITS"]) - post_apply_asserted
    ):
        problems.append(
            f"POST_APPLY_ACTIVE_UNITS asserts {unit} active but POST_APPLY_ENABLED_UNITS "
            "does not assert it enabled"
        )
    for unit in sorted(install_asserted - post_apply_asserted):
        problems.append(
            f"POST_APPLY_ENABLED_UNITS does not assert {unit}, which INSTALL_ENABLED_UNITS "
            "asserts after install"
        )
    for unit in sorted(baseline - install_asserted):
        problems.append(
            f"BASELINE_UNITS disables {unit} but INSTALL_ENABLED_UNITS does not assert it"
        )
    for unit in sorted(baseline & (installed | rendered)):
        problems.append(
            f"BASELINE_UNITS names {unit}, which the payload installs or renders; "
            "the baseline must not have it"
        )
    return problems


def main() -> int:
    try:
        installer, installed, started = parse_installer()
        rendered, apply_managed = parse_apply()
        harness = parse_harness()
        problems = compare(
            installer, installed, started, rendered, apply_managed, harness
        )
    except CheckError as error:
        print(f"unit-enablement: error: {error}", file=sys.stderr)
        return 1
    if problems:
        for problem in sorted(set(problems)):
            print(f"unit-enablement: error: {problem}", file=sys.stderr)
        return 1
    print(
        "unit-enablement: ok: "
        f"{len(installer)} installer units, {len(rendered)} rendered unit"
        f"{'s' if len(rendered) != 1 else ''}, "
        f"{len(harness['POST_APPLY_ENABLED_UNITS'])} post-apply reboot assertions"
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())

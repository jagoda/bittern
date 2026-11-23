#!/usr/bin/env python3
"""Release asset naming contract check for Bittern.

The publish workflow, ADR 0031, the device channel client, the packer and the
Makefile each name the release assets. They must agree on the same five names,
and the kit and its checksum file must never become update assets.

The check compares six sources:

- `.github/workflows/publish.yml`: the `gh release create` asset arguments, the
  two expected-set literals, and the release notes list.
- `packaging/verify_release_draft.py`: the `ASSET_NAMES` tuple the publish job
  verifies before it publishes a draft.
- `docs/adr/0031-update-channel-on-github-releases.md`: the two asset tables.
- `agent/internal/update/channel.go`: the names the device fetches, plus
  `manifest.Payload.Filename` for the payload.
- `agent/cmd/bittern-release/pack.go`: the payload name constant and the
  manifest path the packer writes.
- `Makefile`: the payload, manifest, kit archive and checksum outputs.

It fails naming the source that drifted and the names involved. It fails
closed: a source it cannot parse is an error, never a skip. It reads repository
files only, needs no network, Go toolchain, or release, and runs in `make
check`.
"""

from __future__ import annotations

import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
WORKFLOW = ROOT / ".github" / "workflows" / "publish.yml"
ADR = ROOT / "docs" / "adr" / "0031-update-channel-on-github-releases.md"
CHANNEL = ROOT / "agent" / "internal" / "update" / "channel.go"
PACKER = ROOT / "agent" / "cmd" / "bittern-release" / "pack.go"
MAKEFILE = ROOT / "Makefile"
VERIFIER = ROOT / "packaging" / "verify_release_draft.py"

# The five names ADR 0031 defines. The check derives the set from the sources
# rather than using this constant as a source, but the exact count and the two
# update-only names are part of the contract.
RELEASE_ASSET_COUNT = 5
UPDATE_ASSETS = {"manifest.json", "manifest.json.sig", "bittern-app.tar.gz"}
KIT_ASSETS = {"bittern-beta-kit.tar.gz", "SHA256SUMS"}

ASSET_NAME_RE = re.compile(
    r"^(?:manifest\.json(?:\.sig)?|bittern-[a-z0-9-]+\.tar\.gz|SHA256SUMS)$"
)
CREATE_STEP = "- name: Create the draft release with exactly five assets"
CONFIRM_STEP = "- name: Confirm the draft carries exactly the five assets"
NOTES_STEP = "- name: Write the release notes"
PRINT_STEP = "- name: Print the dry-run result"


class CheckError(Exception):
    pass


def read(path: Path) -> str:
    try:
        return path.read_text(encoding="utf-8")
    except OSError as error:
        raise CheckError(f"cannot read {path.relative_to(ROOT)}: {error}") from error


def step_text(text: str, start_marker: str, end_marker: str) -> str:
    try:
        start = text.index(start_marker)
    except ValueError as error:
        raise CheckError(f"publish.yml has no step {start_marker!r}") from error
    try:
        end = text.index(end_marker, start)
    except ValueError as error:
        raise CheckError(
            f"publish.yml step {start_marker!r} has no following step {end_marker!r}"
        ) from error
    return text[start:end]


def workflow_release_assets(text: str) -> set[str]:
    create = step_text(text, CREATE_STEP, CONFIRM_STEP)
    names = set(re.findall(r"build/assets/([A-Za-z0-9._-]+)", create))
    if len(names) != RELEASE_ASSET_COUNT:
        raise CheckError(
            "publish.yml creates "
            f"{len(names)} assets ({sorted(names)}), not {RELEASE_ASSET_COUNT}"
        )
    return names


def workflow_expected_sets(text: str) -> list[set[str]]:
    """Return every expected='$(printf ...)' asset set in publish.yml."""
    sets: list[set[str]] = []
    lines = text.splitlines()
    index = 0
    while index < len(lines):
        if "expected=\"$(printf '%s\\n'" not in lines[index]:
            index += 1
            continue
        index += 1
        names: set[str] = set()
        while index < len(lines):
            line = lines[index]
            terminator = "| LC_ALL=C sort)"
            chunk = line.split(terminator, 1)[0]
            token = chunk.strip().rstrip("\\").strip()
            if token and ASSET_NAME_RE.match(token):
                names.add(token)
            index += 1
            if terminator in line:
                break
        else:
            raise CheckError("publish.yml has an unterminated expected asset set")
        sets.append(names)
        index += 1
    if not sets:
        raise CheckError("publish.yml has no expected asset set to compare")
    return sets


def workflow_notes_assets(text: str) -> set[str]:
    notes = step_text(text, NOTES_STEP, CREATE_STEP)
    names = {
        token
        for token in re.findall(r"printf -- '- ([A-Za-z0-9._-]+)(?:\\n)+'", notes)
        if ASSET_NAME_RE.match(token)
    }
    return names


def adr_assets(text: str) -> set[str]:
    names: set[str] = set()
    in_table = False
    for line in text.splitlines():
        if line.strip() == "| Asset | Role |":
            in_table = True
            continue
        if not in_table:
            continue
        if not line.startswith("|"):
            in_table = False
            continue
        match = re.match(r"\| `([^`]+)` \|", line)
        if match:
            names.add(match.group(1))
    if not names:
        raise CheckError("ADR 0031 has no Asset/Role table to compare")
    return names


def packer_names(text: str) -> tuple[str, str]:
    payload = re.search(r'const bundlePayloadName = "([^"]+)"', text)
    if payload is None:
        raise CheckError("pack.go has no bundlePayloadName constant")
    manifest = re.search(r'filepath\.Join\(targetOut, "([^"]+)"\)', text)
    if manifest is None:
        raise CheckError("pack.go does not write a named manifest beside the payload")
    return payload.group(1), manifest.group(1)


def device_assets(channel_text: str, payload_name: str) -> set[str]:
    names = set(re.findall(r'findAsset\(selected, "([^"]+)"\)', channel_text))
    if "findAsset(selected, manifest.Payload.Filename)" not in channel_text:
        raise CheckError(
            "channel.go no longer fetches the payload by manifest.Payload.Filename"
        )
    names.add(payload_name)
    return names


def makefile_assets(text: str) -> set[str]:
    assignments = dict(re.findall(r"^([A-Z][A-Z0-9_]*) = (.+)$", text, re.MULTILINE))
    for required in (
        "APP_PAYLOAD",
        "MANIFEST",
        "BETA_KIT_NAME",
        "BETA_KIT_ARCHIVE",
        "BETA_KIT_SUMS",
    ):
        if required not in assignments:
            raise CheckError(f"Makefile has no {required} assignment")

    def resolve(value: str) -> str:
        for _ in range(4):
            expanded = re.sub(
                r"\$\(([A-Z][A-Z0-9_]*)\)",
                lambda match: assignments.get(match.group(1), match.group(0)),
                value,
            )
            if expanded == value:
                break
            value = expanded
        if "$(" in value:
            raise CheckError(f"Makefile variable {value!r} does not resolve")
        return value

    names = {
        Path(resolve(assignments["APP_PAYLOAD"])).name,
        Path(resolve(assignments["MANIFEST"])).name,
        Path(resolve(assignments["BETA_KIT_ARCHIVE"])).name,
        Path(resolve(assignments["BETA_KIT_SUMS"])).name,
    }
    manifest = Path(resolve(assignments["MANIFEST"])).name
    names.add(manifest + ".sig")
    return names


def verifier_assets(text: str) -> set[str]:
    match = re.search(r"ASSET_NAMES = \((.*?)\)", text, re.DOTALL)
    if match is None:
        raise CheckError("verify_release_draft.py has no ASSET_NAMES tuple")
    names = set(re.findall(r'"([^"]+)"', match.group(1)))
    if len(names) != RELEASE_ASSET_COUNT:
        raise CheckError(
            "verify_release_draft.py names "
            f"{len(names)} assets ({sorted(names)}), not {RELEASE_ASSET_COUNT}"
        )
    return names


def require_equal(source: str, actual: set[str], expected: set[str]) -> None:
    if actual != expected:
        missing = sorted(expected - actual)
        extra = sorted(actual - expected)
        raise CheckError(
            f"{source} does not match the publish workflow asset set; "
            f"missing {missing}, unexpected {extra}"
        )


def main() -> int:
    try:
        workflow = read(WORKFLOW)
        adr = read(ADR)
        channel = read(CHANNEL)
        packer = read(PACKER)
        makefile = read(MAKEFILE)
        verifier = read(VERIFIER)

        release = workflow_release_assets(workflow)
        if release != UPDATE_ASSETS | KIT_ASSETS:
            raise CheckError(
                "publish.yml asset set is not the three update assets plus the "
                f"kit and checksum file: {sorted(release)}"
            )

        for index, expected in enumerate(workflow_expected_sets(workflow), start=1):
            require_equal(f"publish.yml expected set {index}", expected, release)
        require_equal("publish.yml release notes", workflow_notes_assets(workflow), release)
        require_equal("packaging/verify_release_draft.py", verifier_assets(verifier), release)
        require_equal("ADR 0031 asset tables", adr_assets(adr), release)

        payload_name, manifest_name = packer_names(packer)
        if manifest_name != "manifest.json":
            raise CheckError(
                f"pack.go writes the manifest as {manifest_name!r}, not 'manifest.json'"
            )
        if payload_name != "bittern-app.tar.gz":
            raise CheckError(
                f"pack.go names the payload {payload_name!r}, not 'bittern-app.tar.gz'"
            )

        device = device_assets(channel, payload_name)
        if device != UPDATE_ASSETS:
            raise CheckError(
                "the device channel client fetches "
                f"{sorted(device)}, not the three update assets {sorted(UPDATE_ASSETS)}"
            )
        for name in sorted(KIT_ASSETS):
            if name in channel:
                raise CheckError(
                    f"channel.go names the kit asset {name!r}; the kit is not an update payload"
                )
        if not device < release:
            raise CheckError(
                "the device update assets are not a strict subset of the release assets"
            )

        built = makefile_assets(makefile)
        require_equal("Makefile outputs", built, release)
        if payload_name not in built:
            raise CheckError(
                "the packer payload name does not match the Makefile payload output"
            )

        print(
            "release assets: " + ", ".join(sorted(release))
        )
        print(
            "device update assets: " + ", ".join(sorted(device))
        )
        print(
            "ADR 0031, the publish workflow, the draft verifier, the device client, "
            "the packer and the Makefile agree; the kit and SHA256SUMS are not update assets"
        )
        return 0
    except CheckError as error:
        print(f"release-asset-names: {error}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())

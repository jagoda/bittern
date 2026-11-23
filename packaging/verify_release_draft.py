#!/usr/bin/env python3
"""Verify an existing beta draft release before it becomes public.

The publish job used to compare the draft against a digest string carried from
the signing job. A recovery run has no signing job and no such string, so this
script verifies the downloaded assets themselves instead:

- the draft contains exactly the five release asset names;
- manifest.json is canonical and verifies with packaging/trust/beta-ci.pub;
- the manifest channel is beta and its generation equals the tag generation;
- payload.sha256 and payload.filename match the downloaded bittern-app.tar.gz;
- every SHA256SUMS entry matches the downloaded bytes, and the kit has a
  published digest;
- the kit and SHA256SUMS are never treated as update payloads.

ADR 0031 defines SHA256SUMS as the kit checksum file, not a checksum for all
five assets. A checksum file also cannot contain its own digest. The verifier
therefore requires every one of the five assets to have an anchored digest:
the signed manifest anchors manifest.json, manifest.json.sig and the payload;
SHA256SUMS anchors the kit; SHA256SUMS itself is an asset whose entries are all
checked. It rejects every missing, extra, malformed, unexpected or stale entry.

Every gh call names the repository explicitly, so the check never depends on a
git checkout to infer it. The script exits nonzero with one specific message for
the first problem it finds. It never edits the draft, the tag or the release.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import re
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path
from typing import Iterable

PACKAGING = Path(__file__).resolve().parent
ROOT = PACKAGING.parent
sys.path.insert(0, str(PACKAGING))

from release_preflight import PreflightError, parse_tag  # noqa: E402

ASSET_NAMES = (
    "manifest.json",
    "manifest.json.sig",
    "bittern-app.tar.gz",
    "bittern-beta-kit.tar.gz",
    "SHA256SUMS",
)
EXPECTED_ASSETS = frozenset(ASSET_NAMES)
MANIFEST_NAME = "manifest.json"
SIGNATURE_NAME = "manifest.json.sig"
PAYLOAD_NAME = "bittern-app.tar.gz"
KIT_NAME = "bittern-beta-kit.tar.gz"
SUMS_NAME = "SHA256SUMS"
SUMS_LINE_RE = re.compile(r"^([0-9a-f]{64}) [ *](.+)$")


class VerifyError(RuntimeError):
    """A draft verification failure with a stable code and one message."""

    def __init__(self, code: str, message: str):
        super().__init__(message)
        self.code = code
        self.message = message


def run(command: list[str], cwd: Path | None = None) -> subprocess.CompletedProcess[str]:
    return subprocess.run(command, cwd=cwd, capture_output=True, text=True, check=False)


def sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as file:
        for chunk in iter(lambda: file.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def check_asset_names(names: Iterable[str]) -> None:
    actual = list(names)
    duplicates = sorted({name for name in actual if actual.count(name) > 1})
    if duplicates:
        raise VerifyError(
            "asset-duplicate",
            f"the draft lists asset(s) more than once: {', '.join(duplicates)}",
        )
    found = set(actual)
    missing = sorted(EXPECTED_ASSETS - found)
    if missing:
        raise VerifyError(
            "asset-missing",
            f"the draft is missing asset(s): {', '.join(missing)}",
        )
    unexpected = sorted(found - EXPECTED_ASSETS)
    if unexpected:
        raise VerifyError(
            "asset-unexpected",
            f"the draft has unexpected asset(s): {', '.join(unexpected)}",
        )


def read_manifest(path: Path) -> dict:
    try:
        data = json.loads(path.read_text(encoding="utf-8"))
    except OSError as error:
        raise VerifyError("manifest-read", f"cannot read {path}: {error}") from error
    except json.JSONDecodeError as error:
        raise VerifyError("manifest-json", f"{path} is not valid JSON: {error}") from error
    if not isinstance(data, dict):
        raise VerifyError("manifest-json", f"{path} is not a JSON object")
    return data


def check_manifest_fields(manifest: dict, generation: int) -> None:
    channel = manifest.get("channel")
    if channel != "beta":
        raise VerifyError(
            "manifest-channel",
            f"manifest channel is {channel!r}, not 'beta'",
        )
    manifest_generation = manifest.get("generation")
    if manifest_generation != generation:
        raise VerifyError(
            "manifest-generation",
            f"manifest generation {manifest_generation!r} does not equal the "
            f"tag generation {generation}",
        )


def check_payload(manifest: dict, assets_dir: Path) -> str:
    payload = manifest.get("payload")
    if not isinstance(payload, dict):
        raise VerifyError("payload-missing", "manifest has no payload object")
    filename = payload.get("filename")
    if filename in (KIT_NAME, SUMS_NAME):
        raise VerifyError(
            "payload-not-app",
            f"manifest payload filename is {filename}; the kit and SHA256SUMS are not payloads",
        )
    if filename != PAYLOAD_NAME:
        raise VerifyError(
            "payload-filename",
            f"manifest payload filename is {filename!r}, not {PAYLOAD_NAME!r}",
        )
    digest = sha256_file(assets_dir / PAYLOAD_NAME)
    manifest_digest = payload.get("sha256")
    if manifest_digest != digest:
        raise VerifyError(
            "payload-digest",
            f"manifest payload.sha256 {manifest_digest!r} does not match "
            f"{PAYLOAD_NAME} {digest}",
        )
    return digest


def parse_sums(assets_dir: Path) -> dict[str, str]:
    path = assets_dir / SUMS_NAME
    try:
        lines = path.read_text(encoding="utf-8").splitlines()
    except OSError as error:
        raise VerifyError("sums-read", f"cannot read {path}: {error}") from error
    entries: dict[str, str] = {}
    for number, line in enumerate(lines, start=1):
        match = SUMS_LINE_RE.match(line)
        if match is None:
            raise VerifyError(
                "sums-malformed",
                f"SHA256SUMS line {number} is malformed; expected '<sha256>  <asset>'",
            )
        digest, name = match.group(1), match.group(2)
        if name not in EXPECTED_ASSETS:
            raise VerifyError(
                "sums-unexpected",
                f"SHA256SUMS lists {name!r}, which is not one of the five release assets",
            )
        if name in entries:
            raise VerifyError(
                "sums-duplicate",
                f"SHA256SUMS lists {name} more than once",
            )
        if name == SUMS_NAME:
            raise VerifyError(
                "sums-self",
                "SHA256SUMS lists itself; a checksum file cannot contain its own digest",
            )
        asset = assets_dir / name
        if not asset.is_file():
            raise VerifyError(
                "sums-missing-file",
                f"SHA256SUMS lists {name}, which is not in the draft",
            )
        actual = sha256_file(asset)
        if digest != actual:
            raise VerifyError(
                "sums-digest",
                f"SHA256SUMS digest for {name} is {digest}, but the downloaded bytes are {actual}",
            )
        entries[name] = digest
    if KIT_NAME not in entries:
        raise VerifyError(
            "sums-missing-kit",
            f"SHA256SUMS does not list {KIT_NAME}; the kit has no published digest",
        )
    return entries


def check_signature(
    assets_dir: Path,
    anchor: Path,
    release_tool: Path,
    work_dir: Path,
    manifest: dict,
) -> str:
    try:
        anchor_bytes = anchor.read_bytes()
    except OSError as error:
        raise VerifyError("anchor-read", f"cannot read the shipped anchor {anchor}: {error}") from error
    anchor_id = hashlib.sha256(anchor_bytes).hexdigest()
    signing = manifest.get("signing")
    manifest_id = signing.get("key_id") if isinstance(signing, dict) else None
    if manifest_id != anchor_id:
        raise VerifyError(
            "anchor-key-id",
            f"manifest signing key {manifest_id!r} does not match the shipped anchor {anchor} ({anchor_id})",
        )
    trust_dir = work_dir / "trust"
    shutil.rmtree(trust_dir, ignore_errors=True)
    trust_dir.mkdir(mode=0o755)
    os.chmod(trust_dir, 0o755)
    key_path = trust_dir / f"{anchor_id}.pub"
    shutil.copyfile(anchor, key_path)
    os.chmod(key_path, 0o644)
    result = run(
        [
            str(release_tool),
            "verify",
            str(assets_dir / MANIFEST_NAME),
            str(assets_dir / SIGNATURE_NAME),
            "--trust-dir",
            str(trust_dir),
        ]
    )
    if result.returncode != 0:
        detail = result.stderr.strip() or result.stdout.strip() or "no detail"
        raise VerifyError(
            "signature-invalid",
            f"{MANIFEST_NAME} does not verify with the shipped anchor {anchor}: {detail}",
        )
    return anchor_id


def gh_release_view(repo: str, tag: str) -> dict:
    result = run(
        [
            "gh",
            "release",
            "view",
            tag,
            "-R",
            repo,
            "--json",
            "isDraft,tagName,assets",
        ]
    )
    if result.returncode != 0:
        detail = result.stderr.strip() or result.stdout.strip() or "no detail"
        raise VerifyError(
            "draft-missing",
            f"draft release {tag} does not exist in {repo}: {detail}",
        )
    try:
        data = json.loads(result.stdout)
    except json.JSONDecodeError as error:
        raise VerifyError(
            "draft-json",
            f"gh release view {tag} did not return JSON: {error}",
        ) from error
    if not isinstance(data, dict):
        raise VerifyError("draft-json", f"gh release view {tag} did not return an object")
    return data


def check_draft(repo: str, tag: str, generation: int, work_dir: Path) -> tuple[dict, Path]:
    tag_result = run(["gh", "api", f"repos/{repo}/git/ref/tags/{tag}"])
    if tag_result.returncode != 0:
        detail = tag_result.stderr.strip() or tag_result.stdout.strip() or "no detail"
        raise VerifyError(
            "tag-missing",
            f"tag {tag} does not exist in {repo}: {detail}",
        )
    release = gh_release_view(repo, tag)
    if release.get("tagName") != tag:
        raise VerifyError(
            "draft-tag",
            f"the draft release tag is {release.get('tagName')!r}, not {tag!r}",
        )
    if release.get("isDraft") is not True:
        raise VerifyError(
            "not-draft",
            f"release {tag} is not a draft; only a draft is verified and published",
        )
    assets = release.get("assets")
    if not isinstance(assets, list):
        raise VerifyError("draft-assets", f"release {tag} has no asset list")
    check_asset_names(asset.get("name", "") for asset in assets if isinstance(asset, dict))

    assets_dir = work_dir / "assets"
    shutil.rmtree(assets_dir, ignore_errors=True)
    assets_dir.mkdir(parents=True)
    result = run(["gh", "release", "download", tag, "-R", repo, "--dir", str(assets_dir)])
    if result.returncode != 0:
        detail = result.stderr.strip() or result.stdout.strip() or "no detail"
        raise VerifyError(
            "download-failed",
            f"cannot download the draft assets for {tag}: {detail}",
        )
    check_asset_names(
        path.name for path in assets_dir.iterdir() if path.is_file()
    )
    return release, assets_dir


def verify(
    repo: str,
    tag: str,
    anchor: Path,
    release_tool: Path,
    work_dir: Path,
) -> tuple[int, str, dict[str, str]]:
    try:
        generation = parse_tag(tag, "beta")
    except PreflightError as error:
        raise VerifyError(
            "tag-form",
            f"tag {tag!r} is not the expected app-beta-* form: {error.message}",
        ) from error
    if not release_tool.is_file():
        raise VerifyError(
            "release-tool-missing",
            f"the release verifier {release_tool} does not exist",
        )
    _, assets_dir = check_draft(repo, tag, generation, work_dir)
    manifest = read_manifest(assets_dir / MANIFEST_NAME)
    check_manifest_fields(manifest, generation)
    anchor_id = check_signature(assets_dir, anchor, release_tool, work_dir, manifest)
    check_payload(manifest, assets_dir)
    parse_sums(assets_dir)
    digests = {name: sha256_file(assets_dir / name) for name in ASSET_NAMES}
    return generation, anchor_id, digests


def write_summary(tag: str, generation: int, anchor_id: str, digests: dict[str, str]) -> None:
    summary_path = os.environ.get("GITHUB_STEP_SUMMARY")
    if not summary_path:
        return
    try:
        with open(summary_path, "a", encoding="utf-8") as summary:
            summary.write(f"### Verified release draft: {tag}\n\n")
            summary.write(f"- Generation: {generation}\n")
            summary.write(f"- Anchor key id: `{anchor_id}`\n\n")
            summary.write("| Asset | SHA-256 |\n| --- | --- |\n")
            for name in sorted(digests):
                summary.write(f"| `{name}` | `{digests[name]}` |\n")
    except OSError:
        pass


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(
        description="Verify the downloaded assets of an existing beta draft release.",
    )
    parser.add_argument("--repo", default=os.environ.get("GITHUB_REPOSITORY", "jagoda/bittern"))
    parser.add_argument("--tag", required=True)
    parser.add_argument("--anchor", default=str(ROOT / "packaging" / "trust" / "beta-ci.pub"))
    parser.add_argument(
        "--release-tool",
        default=str(ROOT / "agent" / "bin" / "bittern-release"),
    )
    parser.add_argument(
        "--work-dir",
        default=str(ROOT / "build" / "release-draft"),
    )
    args = parser.parse_args(argv)

    work_dir = Path(args.work_dir)
    work_dir.mkdir(parents=True, exist_ok=True)
    try:
        generation, anchor_id, digests = verify(
            args.repo,
            args.tag,
            Path(args.anchor),
            Path(args.release_tool),
            work_dir,
        )
    except VerifyError as error:
        print(f"verify-draft: fail: [{error.code}] {error.message}", file=sys.stderr)
        return 1

    print(f"verify-draft: ok: {args.tag} in {args.repo}")
    print(f"tag: {args.tag} (generation {generation})")
    print(f"anchor: {args.anchor} (key id {anchor_id})")
    print(f"payload: {PAYLOAD_NAME}; the kit and SHA256SUMS are not payloads")
    print("SHA256SUMS: every listed digest matches; all five assets have an anchored digest")
    print("asset digests:")
    for name in ASSET_NAMES:
        print(f"  {name}  {digests[name]}")
    write_summary(args.tag, generation, anchor_id, digests)
    return 0


if __name__ == "__main__":
    sys.exit(main())

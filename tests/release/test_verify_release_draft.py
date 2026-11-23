#!/usr/bin/env python3
"""Tests for packaging/verify_release_draft.py.

The tests cover the local checks that decide whether a downloaded draft can be
published: the exact asset set, the manifest fields, the payload binding, and
every SHA256SUMS rejection. The live verification against GitHub and the Go
release tool is exercised by the recovery proof, not here.
"""

from __future__ import annotations

import hashlib
import importlib.util
import sys
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
SCRIPT = ROOT / "packaging" / "verify_release_draft.py"

spec = importlib.util.spec_from_file_location("verify_release_draft", SCRIPT)
assert spec is not None and spec.loader is not None
verify_release_draft = importlib.util.module_from_spec(spec)
sys.modules[spec.name] = verify_release_draft
spec.loader.exec_module(verify_release_draft)

VerifyError = verify_release_draft.VerifyError
ASSET_NAMES = verify_release_draft.ASSET_NAMES
KIT = verify_release_draft.KIT_NAME
PAYLOAD = verify_release_draft.PAYLOAD_NAME
SUMS = verify_release_draft.SUMS_NAME


def digest(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()


def failure(code: str, message: str, function, *args, **kwargs) -> None:
    with unittest.TestCase().assertRaises(VerifyError) as caught:
        function(*args, **kwargs)
    assert caught.exception.code == code, caught.exception
    assert message in caught.exception.message, caught.exception.message


class AssetSetTests(unittest.TestCase):
    def test_exact_five_assets_pass(self) -> None:
        verify_release_draft.check_asset_names(ASSET_NAMES)

    def test_missing_asset_names_the_asset(self) -> None:
        failure(
            "asset-missing",
            "bittern-app.tar.gz",
            verify_release_draft.check_asset_names,
            tuple(name for name in ASSET_NAMES if name != PAYLOAD),
        )

    def test_extra_asset_names_the_asset(self) -> None:
        failure(
            "asset-unexpected",
            "bittern-app.tar.gz.sha256",
            verify_release_draft.check_asset_names,
            (*ASSET_NAMES, "bittern-app.tar.gz.sha256"),
        )

    def test_duplicate_asset_is_refused(self) -> None:
        failure(
            "asset-duplicate",
            "SHA256SUMS",
            verify_release_draft.check_asset_names,
            (*ASSET_NAMES, SUMS),
        )


class ManifestTests(unittest.TestCase):
    def test_channel_must_be_beta(self) -> None:
        failure(
            "manifest-channel",
            "'stable'",
            verify_release_draft.check_manifest_fields,
            {"channel": "stable", "generation": 1},
            1,
        )

    def test_generation_must_equal_the_tag(self) -> None:
        failure(
            "manifest-generation",
            "tag generation 2",
            verify_release_draft.check_manifest_fields,
            {"channel": "beta", "generation": 1},
            2,
        )

    def test_kit_and_checksums_are_not_payloads(self) -> None:
        for name in (KIT, SUMS):
            with self.subTest(name=name):
                failure(
                    "payload-not-app",
                    "are not payloads",
                    verify_release_draft.check_payload,
                    {"payload": {"filename": name, "sha256": "0" * 64}},
                    Path("/nonexistent"),
                )

    def test_payload_filename_must_be_the_app_archive(self) -> None:
        failure(
            "payload-filename",
            "not 'bittern-app.tar.gz'",
            verify_release_draft.check_payload,
            {"payload": {"filename": "other.tar.gz", "sha256": "0" * 64}},
            Path("/nonexistent"),
        )

    def test_payload_digest_must_match_the_downloaded_bytes(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            assets = Path(temporary)
            (assets / PAYLOAD).write_bytes(b"payload")
            failure(
                "payload-digest",
                "does not match",
                verify_release_draft.check_payload,
                {"payload": {"filename": PAYLOAD, "sha256": "0" * 64}},
                assets,
            )

    def test_payload_digest_is_returned(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            assets = Path(temporary)
            (assets / PAYLOAD).write_bytes(b"payload")
            self.assertEqual(
                verify_release_draft.check_payload(
                    {"payload": {"filename": PAYLOAD, "sha256": digest(b"payload")}},
                    assets,
                ),
                digest(b"payload"),
            )


class ChecksumTests(unittest.TestCase):
    def write(self, assets: Path, lines: list[str]) -> None:
        (assets / SUMS).write_text("\n".join(lines) + "\n", encoding="utf-8")

    def test_kit_digest_must_match(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            assets = Path(temporary)
            (assets / KIT).write_bytes(b"kit")
            self.write(assets, [f"{digest(b'kit')}  {KIT}"])
            self.assertEqual(verify_release_draft.parse_sums(assets), {KIT: digest(b"kit")})

    def test_missing_kit_is_refused(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            assets = Path(temporary)
            self.write(assets, [f"{digest(b'x')}  {PAYLOAD}"])
            (assets / PAYLOAD).write_bytes(b"x")
            failure("sums-missing-kit", KIT, verify_release_draft.parse_sums, assets)

    def test_stale_kit_digest_is_refused(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            assets = Path(temporary)
            (assets / KIT).write_bytes(b"kit")
            self.write(assets, [f"{'0' * 64}  {KIT}"])
            failure("sums-digest", KIT, verify_release_draft.parse_sums, assets)

    def test_unexpected_name_is_refused(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            assets = Path(temporary)
            (assets / KIT).write_bytes(b"kit")
            self.write(assets, [f"{digest(b'kit')}  {KIT}", f"{digest(b'x')}  other.tar.gz"])
            failure("sums-unexpected", "other.tar.gz", verify_release_draft.parse_sums, assets)

    def test_duplicate_name_is_refused(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            assets = Path(temporary)
            (assets / KIT).write_bytes(b"kit")
            self.write(assets, [f"{digest(b'kit')}  {KIT}", f"{digest(b'kit')}  {KIT}"])
            failure("sums-duplicate", KIT, verify_release_draft.parse_sums, assets)

    def test_malformed_line_is_refused(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            assets = Path(temporary)
            (assets / KIT).write_bytes(b"kit")
            self.write(assets, [f"{digest(b'kit')} {KIT}"])
            failure("sums-malformed", "line 1", verify_release_draft.parse_sums, assets)

    def test_self_entry_is_refused(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            assets = Path(temporary)
            (assets / KIT).write_bytes(b"kit")
            (assets / SUMS).write_bytes(b"sums")
            self.write(
                assets,
                [f"{digest(b'kit')}  {KIT}", f"{digest(b'sums')}  {SUMS}"],
            )
            failure("sums-self", "cannot contain its own digest", verify_release_draft.parse_sums, assets)


if __name__ == "__main__":
    unittest.main()

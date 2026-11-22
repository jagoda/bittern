#!/usr/bin/env python3
"""Shared tag corpus check for the Go parser and the Python preflight.

The corpus at tests/fixtures/release/tag-cases.json is the authority for the
application bundle tag rule. This check fails when either implementation
disagrees with a case, when the corpus loses a required edge case, or when the
Go parser test cannot read the corpus. It runs in make check, where the Go
toolchain is already required; make release-preflight does not use it.
"""

from __future__ import annotations

import importlib.util
import os
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
CORPUS = ROOT / "tests" / "fixtures" / "release" / "tag-cases.json"
PREFLIGHT = ROOT / "packaging" / "release_preflight.py"
AGENT = ROOT / "agent"

REQUIRED_TAGS = {
    "app-beta-000001",
    "app-beta-00001f",
    "app-beta-0000ff",
    "app-beta-ffffffffff",
    "app-beta-000000",
    "app-beta-00001F",
    "app-beta-00001",
    "app-beta-00001ff",
    "app-beta-",
    "app-beta-g00001",
    "app-beta-0000g1",
    "app-stable-00001f",
    "app-beta-000001-extra",
}


def load_preflight():
    spec = importlib.util.spec_from_file_location("release_preflight", PREFLIGHT)
    if spec is None or spec.loader is None:
        raise RuntimeError(f"cannot load {PREFLIGHT}")
    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    return module


def main() -> int:
    try:
        release_preflight = load_preflight()
        cases = release_preflight.load_tag_corpus(CORPUS)
    except Exception as error:
        print(f"tag-corpus: cannot load the corpus or the Python rule: {error}", file=sys.stderr)
        return 1

    tags = [case["tag"] for case in cases]
    missing = sorted(REQUIRED_TAGS - set(tags))
    if missing:
        print(f"tag-corpus: the corpus is missing required cases: {missing}", file=sys.stderr)
        return 1
    if len(tags) != len(set(tags)):
        print("tag-corpus: the corpus has duplicate tag strings", file=sys.stderr)
        return 1

    try:
        rule = release_preflight.TagRule.from_corpus(cases)
    except release_preflight.PreflightError as error:
        print(
            f"tag-corpus: the Python rule disagrees with the corpus: [{error.code}] {error.message}",
            file=sys.stderr,
        )
        return 1

    valid = sum(1 for case in cases if case["valid"])
    for case in cases:
        try:
            generation = rule.parse(case["tag"], case["channel"])
            actual_valid = True
        except release_preflight.PreflightError:
            generation = None
            actual_valid = False
        if actual_valid != case["valid"]:
            print(
                f"tag-corpus: Python validity for {case['tag']!r} is {actual_valid}, "
                f"corpus says {case['valid']}",
                file=sys.stderr,
            )
            return 1
        if case["valid"] and generation != case["generation"]:
            print(
                f"tag-corpus: Python generation for {case['tag']!r} is {generation}, "
                f"corpus says {case['generation']}",
                file=sys.stderr,
            )
            return 1

    go = os.environ.get("GO", "go")
    environment = os.environ.copy()
    environment["CGO_ENABLED"] = "0"
    environment["GOPROXY"] = "off"
    environment["GOFLAGS"] = "-mod=vendor"
    result = subprocess.run(
        [go, "test", "./internal/release", "-run", "^TestTagCorpus$", "-count=1"],
        cwd=AGENT,
        capture_output=True,
        text=True,
        check=False,
        env=environment,
    )
    if result.returncode != 0:
        detail = result.stdout.strip() or result.stderr.strip() or "no output"
        print(f"tag-corpus: the Go parser test failed: {detail}", file=sys.stderr)
        return 1

    print(f"tag-corpus: {len(cases)} cases, {valid} valid, {len(cases) - valid} invalid")
    print("tag-corpus: the Python rule agrees with every case")
    print("tag-corpus: the Go parser test agrees with every case")
    print("tag-corpus: Go and Python use the same corpus, no drift")
    return 0


if __name__ == "__main__":
    sys.exit(main())

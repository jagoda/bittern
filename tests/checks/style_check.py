#!/usr/bin/env python3
"""Markdown style check for Bittern.

The em dash, tab, and trailing whitespace rules apply only outside fenced code blocks;
fenced code is literal content and is not checked. The UTF-8 and final-newline rules
apply to the whole file. Heading rules also skip fenced code blocks.

Rules:
- No em dash (U+2014) outside fenced code blocks. Use a comma, colon, parentheses, or
  plain words instead.
- No tab characters outside fenced code blocks.
- No trailing whitespace outside fenced code blocks.
- Every file ends with exactly one newline (the whole file is checked).
- ATX headings only (`# Heading`); do not underline headings with `===` or `---`.
- ATX heading markers must be followed by a space.

Uses only the Python standard library and never accesses the network.
"""

from __future__ import annotations

import os
import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
IGNORED_DIRS = {".git", ".hg", ".svn", ".worktrees", "node_modules", "vendor"}
EM_DASH = "\u2014"
SETEXT_UNDERLINE_RE = re.compile(r"^ {0,3}(?:=+|-+)[ \t]*$")
ATX_RE = re.compile(r"^(#{1,6})(.*)$")
FENCE_RE = re.compile(r"^\s*(`{3,}|~{3,})")


def markdown_files() -> list[Path]:
    files: list[Path] = []
    for dirpath, dirnames, filenames in os.walk(ROOT):
        dirnames[:] = sorted(name for name in dirnames if name not in IGNORED_DIRS)
        for name in sorted(filenames):
            if name.endswith(".md"):
                files.append(Path(dirpath) / name)
    return files


def fenced_line_numbers(lines: list[str]) -> set[int]:
    fenced: set[int] = set()
    fence = None
    for lineno, line in enumerate(lines, 1):
        stripped = line.lstrip()
        if fence is not None:
            fenced.add(lineno)
            if stripped.startswith(fence):
                fence = None
            continue
        match = FENCE_RE.match(line)
        if match:
            fence = match.group(1)
            fenced.add(lineno)
    return fenced


def previous_line_can_be_setext_heading(line: str) -> bool:
    stripped = line.lstrip()
    if not stripped or line.startswith("    "):
        return False
    if stripped.startswith(("#", ">", "|", "```", "~~~", "<!--")):
        return False
    if stripped.startswith(("- ", "* ", "+ ")):
        return False
    return True


def check_file(path: Path, failures: list[str]) -> None:
    rel = path.relative_to(ROOT).as_posix()
    try:
        raw = path.read_bytes()
    except OSError as exc:
        failures.append(f"style: {rel}:1: unreadable file ({exc})")
        return

    try:
        text = raw.decode("utf-8")
    except UnicodeDecodeError as exc:
        failures.append(f"style: {rel}:1: invalid UTF-8 ({exc})")
        return

    lines = text.splitlines()
    if not raw.endswith(b"\n"):
        line = len(lines) or 1
        failures.append(f"style: {rel}:{line}: missing newline at end of file")
    elif raw.endswith(b"\n\n"):
        line = len(lines) or 1
        failures.append(f"style: {rel}:{line}: more than one newline at end of file")

    fenced = fenced_line_numbers(lines)
    for lineno, line in enumerate(lines, 1):
        if lineno in fenced:
            continue

        if EM_DASH in line:
            failures.append(f"style: {rel}:{lineno}: em dash (U+2014)")
        if "\t" in line:
            failures.append(f"style: {rel}:{lineno}: tab character")
        if line.endswith((" ", "\t")):
            failures.append(f"style: {rel}:{lineno}: trailing whitespace")

        heading = ATX_RE.match(line)
        if heading and heading.group(2) and not heading.group(2).startswith(" "):
            failures.append(f"style: {rel}:{lineno}: ATX heading needs a space after #")

        if lineno > 1 and SETEXT_UNDERLINE_RE.match(line):
            previous = lines[lineno - 2]
            if previous_line_can_be_setext_heading(previous):
                failures.append(
                    f"style: {rel}:{lineno}: setext heading underline (use ATX headings)"
                )


def main() -> int:
    failures: list[str] = []
    for path in markdown_files():
        check_file(path, failures)
    for failure in failures:
        print(failure)
    return 1 if failures else 0


if __name__ == "__main__":
    sys.exit(main())

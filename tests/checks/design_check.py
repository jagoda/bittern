#!/usr/bin/env python3
"""Design check for Bittern's stylesheet.

The mechanical rules from docs/design/tokens.md and the retire list in
docs/design/components.md:

- color: no colour literal (`#rgb`, `#rrggbb`, `rgb(`, `rgba(`, `hsl(`, `hsla(`) outside
  the `:root` token block.
- dimension: no `px` or `rem` literal outside `:root`. At-rule preludes such as a media
  query condition are not declarations and are not checked. A bare `0` carries no unit
  and is allowed.
- uppercase: no `text-transform: uppercase`. The retire list replaces it with sentence
  case, weight, and a badge shape.
- important: no `!important`.

It parses every CSS file under `ui/static`. It reports `file:line: rule: detail` for each
violation and exits non-zero when any file has one. It fails closed: an unreadable or
unparseable file is an error, never a skip. It uses only the Python standard library and
never accesses the network.
"""

from __future__ import annotations

import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
STYLE_DIR = ROOT / "ui" / "static"
ROOT_SELECTOR = ":root"

COLOUR_RE = re.compile(r"#[0-9a-fA-F]{3,8}\b|\b(?:rgba?|hsla?)\(", re.IGNORECASE)
DIMENSION_RE = re.compile(
    r"(?<![\w.-])-?(?:\d+(?:\.\d+)?|\.\d+)(?:px|rem)\b", re.IGNORECASE
)
IMPORTANT_RE = re.compile(r"!\s*important\b", re.IGNORECASE)
UPPERCASE_RE = re.compile(r"\buppercase\b", re.IGNORECASE)
PROPERTY_RE = re.compile(r"-{0,2}[A-Za-z][A-Za-z0-9-]*")


class ParseError(Exception):
    pass


def css_files() -> list[Path]:
    return sorted(STYLE_DIR.rglob("*.css"))


def relative(path: Path) -> str:
    return path.relative_to(ROOT).as_posix()


def mask_comments_and_strings(text: str) -> str:
    """Blank comments and strings, preserving every newline and character offset."""
    chars = list(text)
    index = 0
    while index < len(text):
        if text.startswith("/*", index):
            end = text.find("*/", index + 2)
            if end == -1:
                raise ParseError(f"line {line_at(text, index)}: unterminated comment")
            for position in range(index, end + 2):
                if chars[position] != "\n":
                    chars[position] = " "
            index = end + 2
            continue

        if text[index] in "\"'":
            quote = text[index]
            end = index + 1
            while end < len(text):
                if text[end] == "\\":
                    end += 2
                    continue
                if text[end] == quote:
                    break
                if text[end] == "\n":
                    raise ParseError(f"line {line_at(text, index)}: unterminated string")
                end += 1
            if end >= len(text):
                raise ParseError(f"line {line_at(text, index)}: unterminated string")
            for position in range(index, end + 1):
                if chars[position] != "\n":
                    chars[position] = " "
            index = end + 1
            continue

        index += 1
    return "".join(chars)


def line_at(text: str, index: int) -> int:
    return text.count("\n", 0, index) + 1


def declaration_line(chunk: str, chunk_line: int, index: int) -> int:
    return chunk_line + chunk.count("\n", 0, index)


def check_declaration(
    path: Path, chunk: str, chunk_line: int, in_root: bool
) -> list[str]:
    colon = chunk.find(":")
    if colon == -1:
        raise ParseError(
            f"line {chunk_line}: declaration without a colon: {chunk.strip()[:60]!r}"
        )
    prop = chunk[:colon].strip()
    if not PROPERTY_RE.fullmatch(prop):
        raise ParseError(
            f"line {chunk_line}: cannot parse the property in {chunk.strip()[:60]!r}"
        )

    value = chunk[colon + 1 :]
    value_start = colon + 1
    violations: list[str] = []

    if prop.lower() == "text-transform":
        for match in UPPERCASE_RE.finditer(value):
            line = declaration_line(chunk, chunk_line, value_start + match.start())
            violations.append(
                f"{relative(path)}:{line}: uppercase: "
                "text-transform: uppercase is retired; use sentence case"
            )

    for match in IMPORTANT_RE.finditer(value):
        line = declaration_line(chunk, chunk_line, value_start + match.start())
        violations.append(
            f"{relative(path)}:{line}: important: !important is not allowed"
        )

    if in_root:
        return violations

    for match in COLOUR_RE.finditer(value):
        line = declaration_line(chunk, chunk_line, value_start + match.start())
        violations.append(
            f"{relative(path)}:{line}: color: "
            f"raw colour literal {match.group(0)!r} outside :root; use a token"
        )

    for match in DIMENSION_RE.finditer(value):
        line = declaration_line(chunk, chunk_line, value_start + match.start())
        violations.append(
            f"{relative(path)}:{line}: dimension: "
            f"raw {match.group(0)!r} outside :root in {prop}; use a token"
        )

    return violations


def scan_file(path: Path) -> list[str]:
    try:
        text = path.read_text(encoding="utf-8")
    except OSError as error:
        raise ParseError(f"cannot read {relative(path)}: {error}") from error
    except UnicodeDecodeError as error:
        raise ParseError(f"cannot decode {relative(path)} as UTF-8: {error}") from error

    text = mask_comments_and_strings(text)
    violations: list[str] = []
    blocks: list[bool] = [False]
    chunk: list[str] = []
    chunk_line = 1
    paren_depth = 0
    line = 1

    for char in text:
        if char == "(":
            paren_depth += 1
            if not chunk:
                chunk_line = line
            chunk.append(char)
        elif char == ")":
            if paren_depth == 0:
                raise ParseError(f"line {line}: unmatched )")
            paren_depth -= 1
            chunk.append(char)
        elif char == "{" and paren_depth == 0:
            prelude = "".join(chunk).strip()
            if not prelude:
                raise ParseError(f"line {line}: block without a selector or at-rule")
            blocks.append(blocks[-1] or prelude == ROOT_SELECTOR)
            chunk = []
        elif char == ";" and paren_depth == 0:
            statement = "".join(chunk)
            if statement.strip() and not statement.lstrip().startswith("@"):
                if len(blocks) == 1:
                    raise ParseError(
                        f"line {line}: declaration outside a rule block: "
                        f"{statement.strip()[:60]!r}"
                    )
                violations.extend(
                    check_declaration(path, statement, chunk_line, blocks[-1])
                )
            chunk = []
        elif char == "}" and paren_depth == 0:
            if len(blocks) == 1:
                raise ParseError(f"line {line}: unmatched }}")
            statement = "".join(chunk)
            if statement.strip() and not statement.lstrip().startswith("@"):
                violations.extend(
                    check_declaration(path, statement, chunk_line, blocks[-1])
                )
            blocks.pop()
            chunk = []
        else:
            if not chunk:
                chunk_line = line
            chunk.append(char)

        if char == "\n":
            line += 1

    if paren_depth != 0:
        raise ParseError(f"line {line}: unclosed (")
    if len(blocks) != 1:
        raise ParseError(f"line {line}: unclosed block")
    if "".join(chunk).strip():
        raise ParseError(f"line {line}: trailing content after the last block")

    return violations


def main() -> int:
    failures: list[str] = []
    files = css_files()

    if not files:
        failures.append(f"{relative(STYLE_DIR)}:1: parse: no CSS files found")

    for path in files:
        try:
            failures.extend(scan_file(path))
        except ParseError as error:
            message = str(error)
            if not message.startswith(f"{relative(path)}:"):
                message = f"{relative(path)}:1: parse: {message}"
            failures.append(message)

    for failure in failures:
        print(failure)
    return 1 if failures else 0


if __name__ == "__main__":
    sys.exit(main())

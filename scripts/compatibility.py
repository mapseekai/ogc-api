#!/usr/bin/env python3
"""Render or verify support tables from the machine-readable compatibility file."""

import argparse
import json
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
START = "<!-- compatibility:start -->"
END = "<!-- compatibility:end -->"


def rows(text):
    return [
        [cell.strip() for cell in line.strip().strip("|").split("|")]
        for line in text.splitlines()
        if line.lstrip().startswith("|")
        and not set(line.replace("|", "").replace(" ", "")) <= {"-", ":"}
    ]


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--write", action="store_true")
    args = parser.parse_args()
    records = json.loads((ROOT / "docs/compatibility.json").read_text())
    expected = [["范围", "版本 / 绑定", "状态"], *records["rows"]]
    table = "| 范围 | 版本 / 绑定 | 状态 |\n| --- | --- | --- |\n"
    table += "\n".join("| " + " | ".join(row) + " |" for row in records["rows"])
    for name in ["README.md", "docs/compatibility.md"]:
        path = ROOT / name
        text = path.read_text()
        before, middle = text.split(START, 1)
        block, after = middle.split(END, 1)
        if args.write:
            path.write_text(before + START + "\n\n" + table + "\n\n" + END + after)
        elif rows(block) != expected:
            raise SystemExit(f"Compatibility table differs from manifest: {name}")
    print("Compatibility tables match the manifest.")


if __name__ == "__main__":
    main()

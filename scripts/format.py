#!/usr/bin/env python3
"""Format Go/Python source and Go documentation examples, or check their format."""

import argparse
import ast
import os
import re
import subprocess
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
SKIP = {
    ".git",
    ".reference-cache",
    ".artifacts",
    "__pycache__",
    "schemas",
}
GO_FENCE = re.compile(r"(```go\n)(.*?)(\n```)", re.DOTALL)


def source_files():
    for current, dirs, names in os.walk(ROOT):
        dirs[:] = [name for name in dirs if name not in SKIP]
        for name in names:
            yield Path(current) / name


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check", action="store_true")
    args = parser.parse_args()
    files = list(source_files())
    go_files = [str(path) for path in files if path.suffix == ".go"]
    goimports = os.environ.get("GOIMPORTS", str(ROOT / ".artifacts/tools/goimports"))
    if not Path(goimports).is_file():
        raise SystemExit(
            "Missing goimports; run make tools or set GOIMPORTS to its absolute path."
        )
    if args.check:
        dirty = subprocess.check_output([goimports, "-l", *go_files], text=True)
        if dirty.strip():
            raise SystemExit("Unformatted Go files:\n" + dirty)
    else:
        subprocess.run([goimports, "-w", *go_files], check=True)
    for path in files:
        if path.suffix == ".py":
            ast.parse(path.read_text(), filename=str(path))
        if path.suffix != ".md":
            continue
        original = path.read_text()

        def format_block(match):
            result = subprocess.run(
                ["gofmt"],
                input=match[2] + "\n",
                text=True,
                capture_output=True,
                check=True,
            )
            return match[1] + result.stdout.rstrip("\n") + match[3]

        formatted = GO_FENCE.sub(format_block, original)
        if args.check and original != formatted:
            raise SystemExit(f"Unformatted Go code fence: {path.relative_to(ROOT)}")
        if not args.check:
            path.write_text(formatted)
    subprocess.run(
        ["ruff", "format", *(["--check"] if args.check else []), "scripts"],
        cwd=ROOT,
        check=True,
    )


if __name__ == "__main__":
    main()

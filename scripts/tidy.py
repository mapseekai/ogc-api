#!/usr/bin/env python3
"""Tidy every Go module without committing local filesystem replacements."""

import os
import subprocess
import tempfile
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
MODULES = [
    ROOT,
    ROOT / "examples/fiber3",
    ROOT / "examples/gin",
]
LOCAL = {
    "github.com/mapseekai/ogc-api": ROOT,
}


def main() -> None:
    env = dict(os.environ, GOWORK="off")
    for module in MODULES:
        with tempfile.TemporaryDirectory(prefix="ogc-tidy-") as temporary:
            modfile = Path(temporary) / "go.mod"
            sumfile = Path(temporary) / "go.sum"
            modfile.write_bytes((module / "go.mod").read_bytes())
            if (module / "go.sum").exists():
                sumfile.write_bytes((module / "go.sum").read_bytes())
            for name, source in LOCAL.items():
                if source != module:
                    subprocess.run(
                        [
                            "go",
                            "mod",
                            "edit",
                            f"-modfile={modfile}",
                            f"-replace={name}={source}",
                        ],
                        cwd=module,
                        env=env,
                        check=True,
                    )
            subprocess.run(
                ["go", "mod", "tidy", f"-modfile={modfile}"],
                cwd=module,
                env=env,
                check=True,
            )
            for name in LOCAL:
                subprocess.run(
                    [
                        "go",
                        "mod",
                        "edit",
                        f"-modfile={modfile}",
                        f"-dropreplace={name}",
                    ],
                    cwd=module,
                    env=env,
                    check=True,
                )
            (module / "go.mod").write_bytes(modfile.read_bytes())
            if sumfile.exists():
                (module / "go.sum").write_bytes(sumfile.read_bytes())
        print("Tidied", module.relative_to(ROOT), flush=True)


if __name__ == "__main__":
    main()

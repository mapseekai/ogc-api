#!/usr/bin/env python3
"""Test unpublished modules as independent consumers through a temporary file proxy."""

import json
import os
import subprocess
import tempfile
import zipfile
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
VERSION = "v0.0.0"
SKIP = {".git", ".reference-cache", ".artifacts", "__pycache__"}
LOCAL = {
    "github.com/mapseekai/ogc-api": ROOT,
}


def main() -> None:
    with tempfile.TemporaryDirectory(prefix="ogc-consumer-") as temporary:
        temp = Path(temporary)
        proxy = temp / "proxy"
        for name, source in LOCAL.items():
            proxy_module(proxy, name, source)
        fallback = subprocess.check_output(["go", "env", "GOPROXY"], text=True).strip()
        env = dict(
            os.environ,
            GOWORK="off",
            GOPROXY=proxy.as_uri() + "," + fallback,
            GONOSUMDB="github.com/mapseekai/ogc-api*",
            CGO_ENABLED="0",
            GOMODCACHE=str(temp / "modcache"),
        )
        for name, modules, imports in [
            (
                "core",
                ["github.com/mapseekai/ogc-api"],
                [
                    "kvp",
                    "owscore",
                    "wms",
                    "wmts",
                    "server",
                    "response",
                    "exception",
                    "api",
                    "common",
                    "features",
                    "tiles",
                    "maps",
                    "coverages",
                    "processes",
                ],
            ),
        ]:
            consumer = temp / name
            consumer.mkdir()
            requirements = "\n".join(f"    {module} {VERSION}" for module in modules)
            (consumer / "go.mod").write_text(
                f"module example.test/ogc-{name}-consumer\n\ngo 1.25.0\n\nrequire (\n{requirements}\n)\n"
            )
            lines = "\n".join(
                f'    _ "github.com/mapseekai/ogc-api/{part}"' for part in imports
            )
            (consumer / "consumer_test.go").write_text(
                f"package consumer\n\nimport (\n{lines}\n)\n"
            )
            subprocess.run(
                ["go", "test", "-mod=mod", "./..."], cwd=consumer, env=env, check=True
            )
            module_paths = subprocess.check_output(
                ["go", "list", "-mod=mod", "-m", "-f", "{{.Path}}", "all"],
                cwd=consumer,
                env=env,
                text=True,
            ).splitlines()
            expected = {f"example.test/ogc-{name}-consumer", *modules}
            if set(module_paths) != expected:
                raise SystemExit(f"Unexpected core dependencies: {module_paths}")
            denied = consumer / "example_import_test.go"
            denied.write_text(
                'package consumer\n\nimport _ "github.com/mapseekai/ogc-api/examples/internal/nethttp"\n'
            )
            try:
                result = subprocess.run(
                    ["go", "test", "-mod=mod", "./..."],
                    cwd=consumer,
                    env=env,
                    capture_output=True,
                    text=True,
                )
                output = result.stdout + result.stderr
                if result.returncode == 0 or "use of internal package" not in output:
                    raise SystemExit(
                        "Example-only import was not rejected as expected:\n" + output
                    )
            finally:
                denied.unlink()
            print(
                "PASS external consumers cannot import example-only HTTP glue",
                flush=True,
            )
            print(
                f"PASS independent {name} consumer (GOWORK=off, no replace directives)",
                flush=True,
            )


def proxy_module(proxy: Path, name: str, source: Path) -> None:
    target = proxy / name / "@v"
    target.mkdir(parents=True)
    (target / "list").write_text(VERSION + "\n")
    (target / f"{VERSION}.mod").write_bytes((source / "go.mod").read_bytes())
    (target / f"{VERSION}.info").write_text(
        json.dumps(
            {
                "Version": VERSION,
                "Time": "2026-10-12T00:00:00Z",
            }
        )
    )
    with zipfile.ZipFile(
        target / f"{VERSION}.zip", "w", zipfile.ZIP_DEFLATED
    ) as archive:
        for current, dirs, files in os.walk(source):
            here = Path(current)
            dirs[:] = [
                d for d in dirs if d not in SKIP and not (here / d / "go.mod").exists()
            ]
            for filename in files:
                path = here / filename
                if (
                    filename in {"go.work", "go.work.sum", ".DS_Store"}
                    or path.is_symlink()
                ):
                    continue
                archive.write(
                    path, f"{name}@{VERSION}/{path.relative_to(source).as_posix()}"
                )


if __name__ == "__main__":
    main()

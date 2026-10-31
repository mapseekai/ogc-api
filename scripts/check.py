#!/usr/bin/env python3
"""Run all module gates and retain the real outputs in .artifacts/check/."""

import argparse
import datetime
import json
import os
import platform
import subprocess
import time
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
ARTIFACTS = ROOT / ".artifacts/check"


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--consumers",
        action="store_true",
        help="Also test a clean independent consumer.",
    )
    args = parser.parse_args()
    ARTIFACTS.mkdir(parents=True, exist_ok=True)
    report = {
        "started_at": datetime.datetime.now(datetime.timezone.utc).isoformat(),
        "platform": platform.platform(),
        "go": subprocess.check_output(["go", "version"], text=True).strip(),
        "analysis_toolchain": subprocess.check_output(
            ["go", "env", "GOVERSION"], text=True
        ).strip(),
        "gates": [],
    }

    def run(name, command, directory=ROOT, env=None):
        started = time.monotonic()
        log = ARTIFACTS / f"{name}.log"
        with log.open("w") as output:
            result = subprocess.run(
                command, cwd=directory, env=env, stdout=output, stderr=subprocess.STDOUT
            )
        gate = {
            "name": name,
            "command": command,
            "directory": str(directory.relative_to(ROOT)),
            "exit_code": result.returncode,
            "seconds": round(time.monotonic() - started, 3),
            "log": str(log.relative_to(ROOT)),
        }
        if name.startswith("test-"):
            events = []
            for line in log.read_text().splitlines():
                try:
                    events.append(json.loads(line))
                except json.JSONDecodeError:
                    continue
            gate["passed_test_events"] = sum(
                event.get("Action") == "pass" and "Test" in event for event in events
            )
        report["gates"].append(gate)
        report["finished_at"] = datetime.datetime.now(datetime.timezone.utc).isoformat()
        (ARTIFACTS / "report.json").write_text(json.dumps(report, indent=2) + "\n")
        print(("PASS " if result.returncode == 0 else "FAIL ") + name, flush=True)
        if result.returncode:
            print(log.read_text())
            raise SystemExit(result.returncode)

    run("format", ["python3", "scripts/format.py", "--check"])
    run("go-style", ["go", "run", "scripts/check_go.go"])
    run("compatibility", ["python3", "scripts/compatibility.py"])
    run(
        "cite-runner",
        [
            "python3",
            "-m",
            "unittest",
            "discover",
            "-s",
            "test/citefeatures",
            "-p",
            "*_test.py",
        ],
    )
    modules = subprocess.check_output(
        ["go", "list", "-m", "-f", "{{.Dir}}"], cwd=ROOT, text=True
    ).splitlines()
    for item in modules:
        directory = Path(item).resolve()
        relative = directory.relative_to(ROOT)
        name = "core" if directory == ROOT else str(relative).replace("/", "-")
        staticcheck = os.environ.get(
            "STATICCHECK", str(ROOT / ".artifacts/tools/staticcheck")
        )
        if not Path(staticcheck).is_file():
            raise SystemExit("Missing staticcheck; run make tools first.")
        run("staticcheck-" + name, [staticcheck, "./..."], directory)
        run("vet-" + name, ["go", "vet", "./..."], directory)
        run("test-" + name, ["go", "test", "-json", "-count=1", "./..."], directory)
        run("race-" + name, ["go", "test", "-race", "-count=1", "./..."], directory)
        build_dir = ARTIFACTS / "build" / name
        build_dir.mkdir(parents=True, exist_ok=True)
        run(
            "pure-go-" + name,
            ["go", "build", "-o", str(build_dir) + os.sep, "./..."],
            directory,
            dict(os.environ, CGO_ENABLED="0"),
        )
    for package in ["kvp", "wms", "wmts", "common", "cql2", "maps", "coverages"]:
        run(
            "fuzz-" + package.replace("/", "-"),
            [
                "go",
                "test",
                "-run=^$",
                "-fuzz=^FuzzParse$",
                "-fuzztime=3s",
                "-parallel=2",
                "./" + package,
            ],
        )
    run(
        "fuzz-wmts-rest",
        [
            "go",
            "test",
            "-run=^$",
            "-fuzz=^FuzzRESTMatch$",
            "-fuzztime=3s",
            "-parallel=2",
            "./wmts",
        ],
    )
    for package, target in [
        ("features", "FuzzFeature"),
        ("processes", "FuzzDecode"),
        ("internal/jsonutil", "FuzzStringBudget"),
    ]:
        run(
            "fuzz-" + package.replace("/", "-"),
            [
                "go",
                "test",
                "-run=^$",
                "-fuzz=^" + target + "$",
                "-fuzztime=3s",
                "-parallel=2",
                "./" + package,
            ],
        )
    run(
        "fuzz-features-query",
        [
            "go",
            "test",
            "-run=^$",
            "-fuzz=^FuzzParseQuery$",
            "-fuzztime=3s",
            "-parallel=2",
            "./features",
        ],
    )
    run("schema", ["python3", "scripts/validate_xml.py"])
    run("core-independent", ["go", "test", "./..."], env=dict(os.environ, GOWORK="off"))
    run(
        "benchmarks",
        [
            "go",
            "test",
            "-run=^$",
            "-bench=.",
            "-benchmem",
            "-benchtime=200ms",
            "./kvp",
            "./wms",
            "./wmts",
            "./response",
            "./server",
            "./api",
            "./features",
            "./cql2",
        ],
    )
    if args.consumers:
        run("consumers", ["python3", "scripts/test_consumers.py"])
    print(
        "All selected gates passed. Evidence: .artifacts/check/report.json", flush=True
    )


if __name__ == "__main__":
    main()

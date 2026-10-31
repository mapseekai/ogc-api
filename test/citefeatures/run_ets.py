#!/usr/bin/env python3
"""Run the pinned unmodified official Features ETS 1.9; preserve every result."""

import argparse
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import subprocess
from urllib.parse import urlparse
import xml.etree.ElementTree as ET

ETS_SHA256 = "cb8c61c156898f42e9a9a0fbf721b3ac0a2aae6345c0ca0b7ed45f31e14faac2"
ETS_COMMIT = "e7c8a96deff936f7ae9c4cbbafb900ce65371d2c"


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--jar", type=Path, required=True)
    parser.add_argument("--iut", required=True, help="Fixture landing page URL")
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    jar = args.jar.resolve()
    if hashlib.sha256(jar.read_bytes()).hexdigest() != ETS_SHA256:
        raise SystemExit("JAR does not match the pinned official ETS 1.9 artifact")
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=False)
    for directory in ("java-home", "java-tmp", "official"):
        (output / directory).mkdir()
    properties = ET.Element("properties", {"version": "1.0"})
    ET.SubElement(
        properties, "comment"
    ).text = (
        "All published fixture collections; unmodified official ETS; no excluded tests"
    )
    ET.SubElement(properties, "entry", {"key": "iut"}).text = args.iut
    ET.SubElement(properties, "entry", {"key": "noofcollections"}).text = "-1"
    properties_path = output / "test-run-props.xml"
    ET.ElementTree(properties).write(
        properties_path, encoding="utf-8", xml_declaration=True
    )
    command = [
        "java",
        "-Duser.home=" + str(output / "java-home"),
        "-Djava.io.tmpdir=" + str(output / "java-tmp"),
    ]
    # Use only the executor's existing proxy route; never embed credentials or
    # disable certificate verification. Local fixture traffic remains direct.
    for scheme in ("http", "https"):
        proxy_value = os.environ.get(scheme.upper() + "_PROXY") or os.environ.get(
            scheme + "_proxy"
        )
        if proxy_value:
            proxy = urlparse(proxy_value)
            if (
                proxy.username
                or proxy.password
                or not proxy.hostname
                or proxy.port is None
            ):
                raise SystemExit("Proxy requires an unauthenticated explicit host/port")
            command += [
                "-D" + scheme + ".proxyHost=" + proxy.hostname,
                "-D" + scheme + ".proxyPort=" + str(proxy.port),
            ]
    command += [
        "-Dhttp.nonProxyHosts=localhost|127.*|[::1]",
        "-jar",
        str(jar),
        "-o",
        str(output / "official"),
        "-h",
        "true",
        str(properties_path),
    ]
    started = datetime.now(timezone.utc).isoformat()
    with (output / "ets.log").open("w") as log:
        process = subprocess.run(command, stdout=log, stderr=subprocess.STDOUT)
    summary = {
        "ets_version": "1.9",
        "ets_git_commit": ETS_COMMIT,
        "ets_sha256": ETS_SHA256,
        "iut": args.iut,
        "argv": command,
        "started_at_utc": started,
        "finished_at_utc": datetime.now(timezone.utc).isoformat(),
        "process_exit": process.returncode,
    }
    reports = list((output / "official").rglob("testng-results.xml"))
    if len(reports) != 1:
        summary["setup_error"] = f"Expected one TestNG result, found {len(reports)}"
        summary["report_valid"] = False
        (output / "summary.json").write_text(json.dumps(summary, indent=2) + "\n")
        return 1
    try:
        report = ET.parse(reports[0]).getroot()
    except (OSError, ET.ParseError, ValueError) as error:
        summary["setup_error"] = "Malformed/unreadable TestNG report: " + str(error)
        summary["report_valid"] = False
        (output / "summary.json").write_text(json.dumps(summary, indent=2) + "\n")
        return 1
    summary["testng_report"] = str(reports[0].relative_to(output))
    summary.update(analyze_report(report))
    summary["suite_assertions_modified"] = False
    (output / "summary.json").write_text(
        json.dumps(summary, ensure_ascii=False, indent=2) + "\n"
    )
    print(
        json.dumps(
            {
                "iut": args.iut,
                "counts": summary["counts"],
                "by_group": summary["by_group"],
            }
        )
    )
    # AIO exit zero alone is not success. Keep failed assertions/configurations
    # red even when their cause is diagnosed as an upstream ETS limitation.
    return int(
        process.returncode != 0
        or not summary["report_valid"]
        or summary["report_failed"]
    )


def analyze_report(report):
    """Validate actual method/configuration outcomes independently of root counters.

    Kept standalone in both repositories so the official product-side runner
    never imports the unofficial ETS repository. Shared regression vectors cover
    both copies. A valid report may contain SKIP, but zero executed records or
    inconsistent/missing counters are not evidence of success.
    """
    errors = []
    result = {
        "counts": dict(report.attrib),
        "by_group": {},
        "nonpass": [],
        "configuration_failures": [],
    }
    actual = {"PASS": 0, "FAIL": 0, "SKIP": 0}
    if report.tag != "testng-results":
        errors.append("Unexpected TestNG report root")
    for test in report.findall("suite/test"):
        name = test.get("name")
        if not name or name in result["by_group"]:
            errors.append("Missing or duplicate TestNG group name")
        group = {}
        for test_class in test.findall("class"):
            for method in test_class.findall("test-method"):
                status = method.get("status")
                entry = {
                    "group": name,
                    "class": test_class.get("name"),
                    "method": method.get("name"),
                    "status": status,
                    "message": method.findtext("exception/message", "").strip(),
                    "parameters": [
                        param.findtext("value", "").strip()
                        for param in method.findall("params/param")
                    ],
                }
                if not entry["class"] or not entry["method"]:
                    errors.append("Missing executed class/method name")
                if status not in actual:
                    errors.append("Unknown or missing executed method status")
                if method.get("is-config") not in (None, "true", "false"):
                    errors.append("Malformed configuration marker")
                if method.get("is-config") == "true":
                    if status == "FAIL":
                        result["configuration_failures"].append(entry)
                    continue
                if status in actual:
                    actual[status] += 1
                    group[status] = group.get(status, 0) + 1
                if status != "PASS":
                    result["nonpass"].append(entry)
        result["by_group"][name] = group
    executed = sum(actual.values())
    if executed == 0:
        errors.append("No executed non-configuration test records")
    expected = {
        "passed": actual["PASS"],
        "failed": actual["FAIL"],
        "skipped": actual["SKIP"],
        "total": executed,
    }
    for name, count in expected.items():
        raw = report.get(name)
        if not isinstance(raw, str) or not raw.isascii() or not raw.isdecimal():
            errors.append("Missing/invalid TestNG counter: " + name)
        elif int(raw) != count:
            errors.append("TestNG counter differs from actual method records: " + name)
    ignored = report.get("ignored")
    if ignored is not None and (not ignored.isascii() or not ignored.isdecimal()):
        errors.append("Invalid ignored-test counter")
    result["actual_method_counts"] = expected
    result["report_validation_errors"] = errors
    result["report_valid"] = not errors
    result["report_failed"] = bool(actual["FAIL"] or result["configuration_failures"])
    return result


if __name__ == "__main__":
    raise SystemExit(main())

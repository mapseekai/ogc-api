"""Synthetic executor-boundary failures, never labelled as real ETS runs."""

from contextlib import redirect_stdout
import hashlib
import io
import json
from pathlib import Path
import sys
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch
import xml.etree.ElementTree as ET

import run_ets as runner


def report(
    methods='<test-method name="case" status="PASS"/>',
    counters='total="1" passed="1" failed="0" skipped="0"',
):
    return (
        "<testng-results "
        + counters
        + '><suite><test name="Core"><class name="Cases">'
        + methods
        + "</class></test></suite></testng-results>"
    )


class ReportTruthTest(unittest.TestCase):
    def test_methods_not_root_failed_default_determine_truth(self):
        result = runner.analyze_report(
            ET.fromstring(report('<test-method name="bad" status="FAIL"/>'))
        )
        self.assertTrue(result["report_failed"])
        self.assertFalse(result["report_valid"])
        self.assertEqual(result["actual_method_counts"]["failed"], 1)

    def test_configuration_failures_independently_fail(self):
        xml = report(
            '<test-method name="case" status="PASS"/><test-method name="setup" is-config="true" status="FAIL"/>'
        )
        result = runner.analyze_report(ET.fromstring(xml))
        self.assertTrue(result["report_valid"])
        self.assertTrue(result["report_failed"])
        self.assertEqual(len(result["configuration_failures"]), 1)

    def test_skip_is_recorded_not_relabelled_pass(self):
        result = runner.analyze_report(
            ET.fromstring(
                report(
                    '<test-method name="unknown" status="SKIP"/>',
                    'total="1" passed="0" failed="0" skipped="1"',
                )
            )
        )
        self.assertTrue(result["report_valid"])
        self.assertFalse(result["report_failed"])
        self.assertEqual(result["actual_method_counts"]["passed"], 0)
        self.assertEqual(result["nonpass"][0]["status"], "SKIP")

    def test_missing_zero_malformed_and_inconsistent_counters(self):
        for xml in [
            '<testng-results total="0" passed="0" failed="0" skipped="0"/>',
            report(counters='total="1" failed="0" skipped="0"'),
            report(counters='total="2" passed="1" failed="0" skipped="0"'),
            report(counters='total="1" passed="-1" failed="0" skipped="0"'),
            report('<test-method name="unknown" status="OTHER"/>'),
            report('<test-method status="PASS"/>'),
            '<unrelated total="1" passed="1" failed="0" skipped="0"/>',
        ]:
            with self.subTest(xml=xml):
                self.assertFalse(
                    runner.analyze_report(ET.fromstring(xml))["report_valid"]
                )

    def test_actual_main_fails_closed_for_exit_zero_executor_faults(self):
        for name, xml in [
            ("missing", None),
            ("malformed", "<testng-results"),
            ("zero", '<testng-results total="0" passed="0" failed="0" skipped="0"/>'),
            ("method-fail", report('<test-method name="bad" status="FAIL"/>')),
            (
                "configuration-fail",
                report(
                    '<test-method name="case" status="PASS"/><test-method name="setup" is-config="true" status="FAIL"/>'
                ),
            ),
            ("valid", report()),
        ]:
            with self.subTest(name=name), tempfile.TemporaryDirectory() as temporary:
                root = Path(temporary)
                jar = root / "synthetic.jar"
                jar.write_bytes(b"synthetic executor test; not an ETS artifact")
                sha = hashlib.sha256(jar.read_bytes()).hexdigest()
                target = root / "output"
                argv = [
                    "runner",
                    "--jar",
                    str(jar),
                    "--iut",
                    "http://127.0.0.1:1/synthetic-only",
                    "--output",
                    str(target),
                ]
                if False:
                    lock = root / "lock.json"
                    lock.write_text(
                        json.dumps(
                            {
                                "unofficial": True,
                                "version": "1.9-local-test",
                                "jar_sha256": sha,
                            }
                        )
                    )
                    argv.extend(["--lock", str(lock)])

                def executor(command, **_):
                    if xml is not None:
                        output = Path(command[command.index("-o") + 1])
                        (output / "testng-results.xml").write_text(xml)
                    return SimpleNamespace(returncode=0)

                with (
                    patch.object(sys, "argv", argv),
                    patch.object(runner, "ETS_SHA256", sha),
                    patch.object(runner.subprocess, "run", side_effect=executor),
                    redirect_stdout(io.StringIO()),
                ):
                    status = runner.main()
                result = json.loads((target / "summary.json").read_text())
                self.assertEqual(result["process_exit"], 0)
                self.assertEqual(status, 0 if name == "valid" else 1)
                if name in ("missing", "malformed", "zero", "method-fail"):
                    self.assertFalse(result["report_valid"])


if __name__ == "__main__":
    unittest.main()

#!/usr/bin/env python3
"""Validate all metadata/exception fixtures offline against pinned schemas."""

import hashlib
import json
import os
import shutil
import subprocess
import xml.etree.ElementTree as ET
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
SCHEMAS = ROOT / "testdata" / "schemas"
OGC = SCHEMAS / "schemas.opengis.net"


def main() -> None:
    if not shutil.which("xmllint"):
        raise SystemExit("XML validation not executed: install libxml2/xmllint.")
    lock = ROOT / "docs" / "schemas.lock.json"
    if not lock.exists():
        raise SystemExit("Run python3 scripts/fetch_schemas.py first.")
    for entry in json.loads(lock.read_text())["files"]:
        path = ROOT / entry["path"]
        if (
            not path.exists()
            or hashlib.sha256(path.read_bytes()).hexdigest() != entry["sha256"]
        ):
            raise SystemExit(f"Schema hash mismatch: {path}")

    namespace = "urn:oasis:names:tc:entity:xmlns:xml:catalog"
    catalog = ET.Element("catalog", xmlns=namespace)
    for host in ("schemas.opengis.net", "www.w3.org"):
        prefix = (SCHEMAS / host).as_uri() + "/"
        for scheme in ("http", "https"):
            url = f"{scheme}://{host}/"
            ET.SubElement(
                catalog, "rewriteSystem", systemIdStartString=url, rewritePrefix=prefix
            )
            ET.SubElement(
                catalog, "rewriteURI", uriStartString=url, rewritePrefix=prefix
            )
    artifacts = ROOT / ".artifacts"
    artifacts.mkdir(exist_ok=True)
    catalog_file = artifacts / "xml-catalog.xml"
    ET.ElementTree(catalog).write(catalog_file, encoding="utf-8", xml_declaration=True)
    env = dict(os.environ, XML_CATALOG_FILES=str(catalog_file))
    cases = []
    for version in ("1.0.0", "1.1.0", "1.1.1"):
        filename = f"capabilities_{version.replace('.', '_')}.dtd"
        if version == "1.1.1":
            filename = "WMS_MS_Capabilities.dtd"
        cases.append(
            (
                f"wms_{version}_capabilities.xml",
                "--dtdvalid",
                OGC / "wms" / version / filename,
            )
        )
        if version != "1.0.0":
            cases.append(
                (
                    f"wms_{version}_exception.xml",
                    "--dtdvalid",
                    OGC
                    / "wms"
                    / version
                    / f"exception_{version.replace('.', '_')}.dtd",
                )
            )
    cases += [
        (
            "wms_1.3.0_capabilities.xml",
            "--schema",
            OGC / "wms/1.3.0/capabilities_1_3_0.xsd",
        ),
        ("wms_1.3.0_exception.xml", "--schema", OGC / "wms/1.3.0/exceptions_1_3_0.xsd"),
        (
            "wmts_1.0.0_capabilities.xml",
            "--schema",
            OGC / "wmts/1.0/wmtsGetCapabilities_response.xsd",
        ),
        (
            "wmts_1.0.0_exception.xml",
            "--schema",
            OGC / "ows/1.1.0/owsExceptionReport.xsd",
        ),
    ]
    cases += [
        (name.replace("_capabilities.xml", "_dimensions.xml"), option, schema)
        for name, option, schema in cases
        if name.endswith("_capabilities.xml") and not name.startswith("wms_1.0.0_")
    ]
    for filename, option, schema in cases:
        fixture = ROOT / "server/testdata" / filename
        subprocess.run(
            ["xmllint", "--nonet", "--noout", option, str(schema), str(fixture)],
            check=True,
            env=env,
        )
        print("PASS", filename)
    # OGC 00-028 defines WMTException as well-formed XML without a DTD.
    subprocess.run(
        [
            "xmllint",
            "--nonet",
            "--noout",
            str(ROOT / "server/testdata/wms_1.0.0_exception.xml"),
        ],
        check=True,
        env=env,
    )
    print(
        "PASS wms_1.0.0_exception.xml (well-formedness; no DTD exists in the standard)"
    )


if __name__ == "__main__":
    main()

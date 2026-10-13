#!/usr/bin/env python3
"""Fetch official validation schemas; subsequent downloads must match the lock."""

import argparse
import hashlib
import json
import urllib.parse
import urllib.request
import xml.etree.ElementTree as ET
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
DEST = ROOT / "testdata" / "schemas"
LOCK = ROOT / "docs" / "schemas.lock.json"
SEEDS = [
    "https://schemas.opengis.net/wms/1.0.0/capabilities_1_0_0.dtd",
    "https://schemas.opengis.net/wms/1.1.0/capabilities_1_1_0.dtd",
    "https://schemas.opengis.net/wms/1.1.1/WMS_MS_Capabilities.dtd",
    "https://schemas.opengis.net/wms/1.1.0/exception_1_1_0.dtd",
    "https://schemas.opengis.net/wms/1.1.1/exception_1_1_1.dtd",
    "https://schemas.opengis.net/wms/1.3.0/capabilities_1_3_0.xsd",
    "https://schemas.opengis.net/wms/1.3.0/exceptions_1_3_0.xsd",
    "https://schemas.opengis.net/wmts/1.0/wmtsGetCapabilities_response.xsd",
    "https://schemas.opengis.net/ows/1.1.0/owsExceptionReport.xsd",
]


def canonical(url: str) -> str:
    parsed = urllib.parse.urlsplit(url)
    if parsed.hostname not in {"schemas.opengis.net", "www.w3.org"}:
        raise ValueError(f"Non-official schema host: {url}")
    if parsed.query or parsed.fragment or parsed.username:
        raise ValueError(f"Unexpected schema URL: {url}")
    return urllib.parse.urlunsplit(("https", parsed.netloc, parsed.path, "", ""))


def local_path(url: str) -> Path:
    parsed = urllib.parse.urlsplit(url)
    path = (DEST / parsed.netloc / parsed.path.lstrip("/")).resolve()
    if not path.is_relative_to(DEST.resolve()):
        raise ValueError("Schema path escaped the destination")
    return path


def fetch(url: str) -> bytes:
    with urllib.request.urlopen(url, timeout=30) as response:
        body = response.read(2 * 1024 * 1024 + 1)
    if len(body) > 2 * 1024 * 1024:
        raise ValueError(f"Schema too large: {url}")
    return body


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--refresh-lock", action="store_true")
    args = parser.parse_args()
    if LOCK.exists() and not args.refresh_lock:
        for entry in json.loads(LOCK.read_text())["files"]:
            path = ROOT / entry["path"]
            body = path.read_bytes() if path.exists() else fetch(entry["url"])
            if hashlib.sha256(body).hexdigest() != entry["sha256"]:
                raise ValueError(f"Schema hash mismatch: {entry['path']}")
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(body)
        print("All schema files match the pinned hashes.")
        return

    queue = list(SEEDS)
    seen = set()
    entries = []
    while queue:
        url = canonical(queue.pop(0))
        if url in seen:
            continue
        seen.add(url)
        if len(seen) > 80:
            raise ValueError("Schema dependency limit exceeded")
        body = fetch(url)
        path = local_path(url)
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(body)
        entries.append(
            {
                "url": url,
                "path": str(path.relative_to(ROOT)),
                "sha256": hashlib.sha256(body).hexdigest(),
            }
        )
        print("Fetched", path.relative_to(ROOT), flush=True)
        if path.suffix == ".xsd":
            tree = ET.fromstring(body)
            for element in tree.iter():
                if element.tag in {
                    "{http://www.w3.org/2001/XMLSchema}import",
                    "{http://www.w3.org/2001/XMLSchema}include",
                    "{http://www.w3.org/2001/XMLSchema}redefine",
                } and element.get("schemaLocation"):
                    queue.append(
                        urllib.parse.urljoin(url, element.attrib["schemaLocation"])
                    )
    entries.sort(key=lambda item: item["url"])
    LOCK.write_text(
        json.dumps(
            {
                "sources": "Official OGC and W3C validation schemas; original headers retained",
                "files": entries,
            },
            indent=2,
        )
        + "\n"
    )
    print(f"Pinned {len(entries)} official schema files.")


if __name__ == "__main__":
    main()

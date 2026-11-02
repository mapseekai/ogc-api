# Third-party notices

The root [MIT License](LICENSE) covers the original work of this project. It does
not replace the copyright notices, licenses, or other terms applicable to
third-party materials. Preserve those notices when redistributing the materials.

## Bundled validation schemas and DTDs

`testdata/schemas/` contains third-party validation resources, not project-authored
MIT-licensed schemas. [docs/schemas.lock.json](docs/schemas.lock.json) records the
original source URL, local path, and SHA-256 of each of the 62 pinned files.
The files are retained with their original notices and references to terms.

| Paths under `testdata/schemas/` | Source and notices |
| --- | --- |
| `schemas.opengis.net/wms/`, `schemas.opengis.net/wmts/`, `schemas.opengis.net/ows/`, `schemas.opengis.net/gml/3.1.1/base/` | Open Geospatial Consortium (OGC) schemas/DTDs. Retain the notices in each file and consult the terms referenced there, including `http://www.opengeospatial.org/legal/`. Historical DTDs do not all contain an inline license notice; their absence is not an MIT grant. |
| `schemas.opengis.net/gml/3.1.1/smil/` | W3C SMIL schemas distributed from the OGC schema server. Their notices identify “1998-2001 W3C (MIT, INRIA, Keio)” and refer to `http://www.w3.org/Consortium/Legal/`. The hosting domain does not change the identified rights holder. |
| `www.w3.org/1999/xlink.xsd`, `www.w3.org/2001/xml.xsd` | W3C XML/XLink schemas. Preserve the original files and consult the source documents and applicable W3C terms; the project MIT license does not relicense them. |

The checksum lock is evidence of source identity and byte preservation, not a
substitute for the applicable licenses. No OGC or W3C endorsement or certification
is implied by including these validation resources.

## Test fixtures and reference projects

- `server/testdata/*.xml` are golden responses produced by this project's tests
  (`server/engine_test.go`, including the `UPDATE_GOLDEN` workflow). Schema URLs
  in those responses identify external validation resources, not a transfer of
  ownership of those resources.
- `test/citefeatures/` provides project test code and synthetic in-memory data.
  Official ETS software and the separately maintained test executor are not
  bundled here; their own licenses apply when obtained and run separately.
- `examples/demo/fixture.png` is an original project-owned, generated 256×256
  geometric PNG under the root MIT license. The RC preparation replaced the older
  image whose provenance was not recorded. Its deterministic geometry generator and frozen-byte
  verification are in `examples/demo/demo_test.go` (`TestPNGFixture`). Regenerate
  from the repository root with Go 1.27.3:
  `UPDATE_DEMO_FIXTURE=1 GOTOOLCHAIN=go1.27.3 go test ./examples/demo -run TestPNGFixture`.
  The SHA-256 assertion pins the reviewed PNG; all supported Go versions verify
  the decoded pixels against the generator. No external imagery or map data is used.
- [docs/references.lock.json](docs/references.lock.json) records reference
  repositories and inspected license files. It identifies those inspections as
  reference-only or dependency API inspection, without copied source or fixtures;
  a reference entry does not relicense any upstream project.

## Dependencies and build tools

The root Go module uses the standard library and declares no external module
requirements. The separate `examples/fiber3` and `examples/gin` modules declare
framework and transitive dependencies in their own `go.mod` and `go.sum` files.
Those dependencies, the Go toolchain, and development/validation tools remain
under their respective licenses. They are not relicensed by this project's MIT
license. When distributing built examples or bundled tools, include the notices
required by the exact dependency versions and distribution contents.

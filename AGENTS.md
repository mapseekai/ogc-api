# ogc-api development

Read docs/code-style.md for global/Go conventions and source-to-test pairing.
Read docs/architecture.md and docs/compatibility.md before changing public contracts.
Use Ponytail: reuse shared paths and the standard library; complete correctness and boundary tests before reducing code.
Protocol packages must not import Web frameworks, GIS engines, databases or storage SDKs.
HTTP and Web framework glue belongs only in examples/, never in a public transport package.
Share example-only HTTP helpers in examples/internal/; keep Fiber helpers private to the example module.
Do not add empty adapters, plugin registries or dependency injection containers.
Every protocol/version change needs request, metadata and exception regression tests.
Run make tools once for pinned, project-local goimports and Staticcheck.
Document public type/function/method contracts, including mutations and ownership.
Keep each source file's tests in its same-directory <source>_test.go; consolidate scenarios, fuzz targets and benchmarks there.
Run scripts/check.sh; its Go style gate checks naming, test layout, helper markers and receivers across all modules.
Do not commit, push, deploy or publish without explicit authorization.

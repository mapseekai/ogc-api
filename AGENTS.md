# Repository Guidelines

## Project Structure & Module Organization

This Go library implements WMS, WMTS, and resource-oriented OGC APIs. Protocol packages live in `wms/`, `wmts/`, `features/`, `tiles/`, `maps/`, `coverages/`, and `processes/`. `server/` and `api/` orchestrate requests; `response/` manages response ownership and cleanup. Shared implementation belongs in `internal/`; reusable transport contract tests live in `adaptertest/`.

`examples/` contains runnable HTTP integrations. `go.work` joins the root module with the Fiber and Gin example modules. Tests sit beside source files; fixtures and schemas belong in `testdata/`. Architecture and compatibility documentation lives in `docs/`.

## Build, Test, and Development Commands

Use Go 1.25+ for workspace development. Full checks also require Python 3, Make, Ruff, and xmllint.

- `make tools`: install pinned, project-local goimports and Staticcheck; run once during setup.
- `make fmt`: synchronize compatibility tables and format sources.
- `make test`: run Go tests across all workspace modules.
- `go test ./wms`: run a focused package test.
- `make check` or `./scripts/check.sh`: run the repository validation pipeline.
- `make consumers`: verify standalone consumer compatibility.
- `make schema`: validate XML against schemas.
- `go run ./examples/ogcapi`: build and run the combined demo at `127.0.0.1:8080`.

## Coding Style & Naming Conventions

Follow `docs/code-style.md`. Use gofmt tabs for Go and two spaces elsewhere, as specified by `.editorconfig`. Use `snake_case.go`, short lowercase package names, and consistent `ID`, `URL`, and `HTTP` initialisms. Group imports into standard library, third-party, and project packages. Document public contracts, including mutations and ownership.

Reuse existing paths and the standard library. Avoid speculative abstractions, empty adapters, registries, and dependency injection containers.

## Testing Guidelines

Use Go's `testing` package, table-driven cases, and `t.Run`; helpers call `t.Helper()`. Pair `request.go` with same-directory `request_test.go`, including regression cases, fuzz targets, and benchmarks. Prefer public-interface tests for core packages. Every protocol/version change needs request, metadata, and exception regression tests. Complete correctness and boundary tests before simplifying code.

## Architecture Boundaries

Read `docs/architecture.md` and `docs/compatibility.md` before changing public contracts. Protocol packages must not import web frameworks, GIS engines, databases, or storage SDKs. Keep HTTP glue in `examples/`, shared helpers in `examples/internal/`, and Fiber helpers private to its example module.

## Commit & Pull Request Guidelines

History uses Conventional Commit prefixes such as `feat:`, `refactor:`, and `docs:`; mark breaking changes with `!`. Describe the problem, resulting behavior, compatibility impact, and validation in PRs; link relevant issues. Agents must not commit, push, deploy, or publish without explicit authorization.

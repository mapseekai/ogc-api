.PHONY: tools fmt check test consumers tidy schema

fmt:
	python3 scripts/compatibility.py --write
	python3 scripts/format.py

check:
	./scripts/check.sh

test:
	go list -m -f '{{.Dir}}' | while IFS= read -r module; do \
		(cd "$$module" && go test ./...) || exit $$?; \
	done

consumers:
	python3 scripts/test_consumers.py

tidy:
	python3 scripts/tidy.py

schema:
	python3 scripts/validate_xml.py

# Isolate developer binaries and build them with the pinned analysis toolchain.
tools:
	mkdir -p "$(CURDIR)/.artifacts/tools"
	GOWORK=off GOTOOLCHAIN=go1.27.0 GOBIN="$(CURDIR)/.artifacts/tools" go install golang.org/x/tools/cmd/goimports@v0.44.1-0.20260420230617-19499e7caabc
	GOWORK=off GOTOOLCHAIN=go1.27.0 GOBIN="$(CURDIR)/.artifacts/tools" go install honnef.co/go/tools/cmd/staticcheck@v0.8.0

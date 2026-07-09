GO ?= go
GO_PACKAGES := ./...

.PHONY: fmt fmt-check lint test js-check bench check

fmt:
	$(GO) fmt $(GO_PACKAGES)

fmt-check:
	@test -z "$$(gofmt -l .)" || (gofmt -l . && exit 1)

lint:
	$(GO) tool golangci-lint run $(GO_PACKAGES)

test:
	$(GO) test $(GO_PACKAGES)

js-check:
	@if command -v node >/dev/null 2>&1; then \
		node --check web/app.js; \
	else \
		echo "node not found; skipping web/app.js syntax check"; \
	fi

bench:
	$(GO) test -bench=. -benchmem $(GO_PACKAGES)

check: fmt-check lint test js-check

VERSION_FILE := .version
CURRENT_VERSION := $(shell cat $(VERSION_FILE) 2>/dev/null || echo "0.0.0")

.PHONY: build version release-patch release-minor release-major

build:
	go build -ldflags "-s -w -X main.version=$(CURRENT_VERSION)" -o wp-sync ./cmd/wp-sync/

version:
	@echo "Current version: v$(CURRENT_VERSION)"

release-patch:
	@./scripts/release.sh patch

release-minor:
	@./scripts/release.sh minor

release-major:
	@./scripts/release.sh major

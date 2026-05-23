VERSION_FILE := .version
CURRENT_VERSION := $(shell cat $(VERSION_FILE) 2>/dev/null || echo "0.0.0")

MAJOR := $(word 1,$(subst ., ,$(CURRENT_VERSION)))
MINOR := $(word 2,$(subst ., ,$(CURRENT_VERSION)))
PATCH := $(word 3,$(subst ., ,$(CURRENT_VERSION)))

.PHONY: build release-patch release-minor release-major release version

build:
	go build -ldflags "-s -w -X main.version=$(CURRENT_VERSION)" -o wp-sync ./cmd/wp-sync/

version:
	@echo "Current version: v$(CURRENT_VERSION)"

release-patch:
	$(eval NEW_VERSION := $(MAJOR).$(MINOR).$(shell echo $$(($(PATCH)+1))))
	@$(MAKE) release NEW_VERSION=$(NEW_VERSION)

release-minor:
	$(eval NEW_VERSION := $(MAJOR).$(shell echo $$(($(MINOR)+1))).0)
	@$(MAKE) release NEW_VERSION=$(NEW_VERSION)

release-major:
	$(eval NEW_VERSION := $(shell echo $$(($(MAJOR)+1))).0.0)
	@$(MAKE) release NEW_VERSION=$(NEW_VERSION)

release:
ifndef NEW_VERSION
	$(error NEW_VERSION is not set. Use release-patch, release-minor, or release-major)
endif
	@echo "$(NEW_VERSION)" > $(VERSION_FILE)
	@GIT_EDITOR=true git commit --only $(VERSION_FILE) -m "Release v$(NEW_VERSION)" --no-edit
	@git tag "v$(NEW_VERSION)"
	@echo ""
	@echo "✓ Tagged v$(NEW_VERSION)"
	@echo "  Run 'git push origin main --tags' to trigger the release build."

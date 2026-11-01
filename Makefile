SHELL := /bin/bash
.DEFAULT_GOAL := help

# ---- project ---------------------------------------------------------------
NAME     := vrotrss
BINARY   := vrot
CMD      := ./cmd/vrot
DIST     := dist
GOBIN    := $(shell go env GOPATH)/bin

# Pinned tool versions (override on the command line if needed).
GOLANGCI_LINT_VERSION ?= v2.15.0
GOVULNCHECK_VERSION   ?= latest

# ---- versioning ------------------------------------------------------------
# VERSION is what you pass to `make tag` / `make release` (e.g. v0.1.0).
# Plain builds are stamped from git: v0.1.0, v0.1.0-3-gabc1234, or -dirty.
GIT_VERSION  := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
BUILD_VERSION = $(or $(VERSION),$(GIT_VERSION))
COMMIT       := $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE         := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS       = -s -w -X main.version=$(BUILD_VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)

REMOTE ?= origin
BRANCH ?= master

PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64

# Prefer a golangci-lint on PATH, fall back to GOBIN.
GOLANGCI_LINT = $(or $(shell command -v golangci-lint 2>/dev/null),$(GOBIN)/golangci-lint)

##@ General

.PHONY: help
help: ## Show this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage: make \033[36m<target>\033[0m\n"} \
		/^[a-zA-Z0-9_.-]+:.*?##/ { printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2 } \
		/^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) }' $(MAKEFILE_LIST)
	@echo

##@ Develop

.PHONY: deps
deps: ## Download and tidy Go modules
	go mod tidy
	go mod download

.PHONY: build
build: ## Build ./vrot for this machine
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BINARY) $(CMD)

.PHONY: install
install: ## Install vrot into $GOBIN
	CGO_ENABLED=0 go install -trimpath -ldflags "$(LDFLAGS)" $(CMD)

.PHONY: run
run: ## Run against your FreshRSS (uses your config file)
	go run -ldflags "$(LDFLAGS)" $(CMD)

.PHONY: demo
demo: ## Run with built-in sample data (no server needed)
	go run -ldflags "$(LDFLAGS)" $(CMD) -demo

.PHONY: clean
clean: ## Remove build artefacts
	rm -rf $(BINARY) $(DIST) coverage.out coverage.html

##@ Quality

.PHONY: fmt
fmt: tools-lint ## Format code (gofumpt + goimports via golangci-lint)
	$(GOLANGCI_LINT) fmt ./...

.PHONY: vet
vet: ## Run go vet
	go vet ./...

.PHONY: lint
lint: tools-lint ## Run golangci-lint (also checks formatting)
	$(GOLANGCI_LINT) run ./...

.PHONY: test
test: ## Run unit tests
	go test ./...

.PHONY: race
race: ## Run unit tests with the race detector
	go test -race -count=1 ./...

.PHONY: cover
cover: ## Run tests with coverage and print a summary
	go test -race -count=1 -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out | tail -n 1

.PHONY: cover-html
cover-html: cover ## Open an HTML coverage report
	go tool cover -html=coverage.out -o coverage.html
	@echo "Wrote coverage.html"

.PHONY: vuln
vuln: ## Scan dependencies for known vulnerabilities
	@command -v govulncheck >/dev/null 2>&1 || [ -x $(GOBIN)/govulncheck ] || \
		go install golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)
	$(or $(shell command -v govulncheck 2>/dev/null),$(GOBIN)/govulncheck) ./...

.PHONY: check
check: vet lint race ## Everything CI runs: vet, lint, tests with -race

.PHONY: tools-lint
tools-lint:
	@command -v golangci-lint >/dev/null 2>&1 || [ -x $(GOBIN)/golangci-lint ] || { \
		echo "Installing golangci-lint $(GOLANGCI_LINT_VERSION)…"; \
		go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION); }

.PHONY: tools
tools: tools-lint ## Install development tools (golangci-lint, govulncheck)
	go install golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)

##@ Package

.PHONY: docker
docker: ## Build a Docker image (vrotrss:<version>)
	docker build --build-arg VERSION=$(BUILD_VERSION) -t $(NAME):$(BUILD_VERSION) -t $(NAME):latest .

.PHONY: dist
dist: ## Cross-compile release archives into dist/ (uses VERSION or git describe)
	@rm -rf $(DIST) && mkdir -p $(DIST)
	@set -e; for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; \
		name=$(NAME)_$(BUILD_VERSION)_$${os}_$${arch}; \
		ext=""; if [ "$$os" = windows ]; then ext=.exe; fi; \
		echo "  building $$name"; \
		mkdir -p $(DIST)/$$name; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags "$(LDFLAGS)" \
			-o $(DIST)/$$name/$(BINARY)$$ext $(CMD); \
		cp README.md LICENSE $(DIST)/$$name/; \
		if [ "$$os" = windows ]; then \
			(cd $(DIST) && zip -qr $$name.zip $$name); \
		else \
			tar -C $(DIST) -czf $(DIST)/$$name.tar.gz $$name; \
		fi; \
		rm -rf $(DIST)/$$name; \
	done
	@cd $(DIST) && { sha256sum *.tar.gz *.zip 2>/dev/null || shasum -a 256 *.tar.gz *.zip; } > SHA256SUMS
	@echo "Archives and SHA256SUMS written to $(DIST)/"

##@ Release

.PHONY: _require-version
_require-version:
	@test -n "$(VERSION)" || { echo "VERSION is required, e.g.: make $(firstword $(MAKECMDGOALS)) VERSION=v0.1.0"; exit 1; }
	@echo "$(VERSION)" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$$' || \
		{ echo "VERSION must look like v1.2.3 (or v1.2.3-rc.1), got '$(VERSION)'"; exit 1; }

.PHONY: _require-clean
_require-clean:
	@git diff --quiet && git diff --cached --quiet || { echo "Working tree has uncommitted changes; commit or stash first."; exit 1; }
	@test -z "$$(git ls-files --others --exclude-standard)" || { echo "Untracked files present; commit or ignore them first."; exit 1; }

.PHONY: _require-branch
_require-branch:
	@br=$$(git rev-parse --abbrev-ref HEAD); [ "$$br" = "$(BRANCH)" ] || { \
		echo "On branch '$$br'; releases are tagged from '$(BRANCH)' (override with BRANCH=...)."; exit 1; }
	@git fetch --quiet $(REMOTE) || { echo "Cannot reach remote '$(REMOTE)'."; exit 1; }
	@[ "$$(git rev-parse HEAD)" = "$$(git rev-parse $(REMOTE)/$(BRANCH))" ] || { \
		echo "HEAD differs from $(REMOTE)/$(BRANCH): pull or push your commits first."; exit 1; }

.PHONY: tag
tag: _require-version _require-clean _require-branch ## Tag an explicit version and push it: make tag VERSION=v0.1.0 [DRY_RUN=1] [YES=1]
	@if git rev-parse -q --verify "refs/tags/$(VERSION)" >/dev/null; then \
		echo "Tag $(VERSION) already exists locally."; exit 1; fi
	@if git ls-remote --exit-code --tags $(REMOTE) "refs/tags/$(VERSION)" >/dev/null 2>&1; then \
		echo "Tag $(VERSION) already exists on $(REMOTE)."; exit 1; fi
	@if [ -n "$(DRY_RUN)" ]; then \
		echo "[dry-run] would run checks, tag $$(git rev-parse --short HEAD) as $(VERSION) and push to $(REMOTE)."; exit 0; fi; \
	$(MAKE) --no-print-directory check || exit 1; \
	if [ -z "$(YES)" ] && [ -t 0 ]; then \
		read -r -p "Tag $$(git rev-parse --short HEAD) as $(VERSION) and push to $(REMOTE)? This publishes a release. [y/N] " ans; \
		[ "$$ans" = y ] || [ "$$ans" = Y ] || { echo "Aborted."; exit 1; }; \
	fi; \
	git tag -a $(VERSION) -m "Release $(VERSION)" || exit 1; \
	git push $(REMOTE) $(VERSION) || { git tag -d $(VERSION) >/dev/null; echo "Push failed; local tag removed."; exit 1; }; \
	echo "Pushed $(VERSION) to $(REMOTE): GitHub Actions will test, build and publish the release."

.PHONY: _bump
_bump: _require-clean
	@test -n "$(BUMP)"
	@git fetch --quiet --tags $(REMOTE) || { echo "Cannot reach remote '$(REMOTE)'."; exit 1; }
	@latest=$$(git tag --list 'v[0-9]*.[0-9]*.[0-9]*' --sort=-v:refname | grep -Ev -- '-' | head -n1); \
	latest=$${latest:-v0.0.0}; \
	IFS=. read -r maj min pat <<<"$${latest#v}"; \
	case "$(BUMP)" in \
		major) maj=$$((maj+1)); min=0; pat=0 ;; \
		minor) min=$$((min+1)); pat=0 ;; \
		patch) pat=$$((pat+1)) ;; \
	esac; \
	next="v$$maj.$$min.$$pat"; \
	echo "Latest release: $$latest  ->  next ($(BUMP)): $$next"; \
	$(MAKE) --no-print-directory tag VERSION=$$next

.PHONY: tag-patch
tag-patch: ## Bump patch (v1.2.3 -> v1.2.4), tag and push  [DRY_RUN=1 to preview, YES=1 to skip the prompt]
	@$(MAKE) --no-print-directory _bump BUMP=patch

.PHONY: tag-minor
tag-minor: ## Bump minor (v1.2.3 -> v1.3.0), tag and push
	@$(MAKE) --no-print-directory _bump BUMP=minor

.PHONY: tag-major
tag-major: ## Bump major (v1.2.3 -> v2.0.0), tag and push
	@$(MAKE) --no-print-directory _bump BUMP=major

.PHONY: release
release: _require-version _require-clean ## Build archives, push the tag, create a GitHub release (needs gh)
	@git rev-parse -q --verify "refs/tags/$(VERSION)" >/dev/null || { \
		echo "Tag $(VERSION) does not exist; run: make tag VERSION=$(VERSION)"; exit 1; }
	@[ "$$(git rev-parse $(VERSION)^{commit})" = "$$(git rev-parse HEAD)" ] || { \
		echo "HEAD is not at tag $(VERSION); check out the tag before releasing."; exit 1; }
	$(MAKE) dist VERSION=$(VERSION)
	git push origin $(VERSION)
	@if command -v gh >/dev/null 2>&1; then \
		gh release create $(VERSION) $(DIST)/*.tar.gz $(DIST)/*.zip $(DIST)/SHA256SUMS \
			--title "$(VERSION)" --generate-notes $$(echo "$(VERSION)" | grep -q -- '-' && echo --prerelease); \
	else \
		echo "gh (GitHub CLI) not found: tag pushed and archives are in $(DIST)/."; \
		echo "Create the release manually and upload the files from $(DIST)/."; \
	fi

# =============================================================================
# Makefile - Development and CI/CD checks
# =============================================================================
# Run 'make pre-commit' before committing to catch issues locally.
# Run 'make install-hooks' once to set up automatic pre-commit checks.
# =============================================================================

# Build provenance stamped into the binary; served by GET /api/version.
# Overridable so a release pipeline can pass exact values. `?=` keeps a plain
# `make build` honest on a machine without git: it falls back to the same
# "dev"/"unknown" defaults compiled into internal/buildinfo.
BUILDINFO_PKG := github.com/theimaginaryfoundation/what-iff/internal/buildinfo
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT   ?= $(shell git rev-parse HEAD 2>/dev/null || echo unknown)
BUILT_AT ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
BUILDINFO_LDFLAGS := -X $(BUILDINFO_PKG).Version=$(VERSION) -X $(BUILDINFO_PKG).Commit=$(COMMIT) -X $(BUILDINFO_PKG).BuiltAt=$(BUILT_AT)

# Generated Ent code is not checked in (see .gitignore); it is produced from
# ent/schema by `go generate`. ent/client.go is the sentinel: it is (re)generated
# on a fresh checkout, or whenever a schema file changes. The Go-compiling targets
# below depend on it, so a bare `make build` / `make test` works with no manual
# step. CI and the Dockerfile run `make generate` / `go generate ./ent` explicitly.
#
# Keep this above the first target that lists $(ENT_SENTINEL) as a prerequisite.
# Make expands a prerequisite list when it reads the rule, so a definition
# further down the file expands to nothing there and the dependency is lost
# silently -- `make build` on a fresh checkout then fails in `go build` instead
# of generating first.
# gofmt from the toolchain go.mod pins, not whatever `gofmt` is first on PATH. A
# distro Go package one minor version behind formats some constructs differently, so
# a bare `gofmt` makes `make fmt` pass on one machine and fail on another against an
# identical tree. `go` itself already honours the go.mod toolchain directive; this
# makes the formatter agree with it.
GOFMT := $(shell go env GOROOT)/bin/gofmt

ENT_SENTINEL := ent/client.go
$(ENT_SENTINEL): ent/generate.go $(wildcard ent/schema/*.go)
	@echo "Generating Ent code (output missing or schema changed)..."
	@go generate ./ent

# entc writes its output file by file with no temp-tree-and-rename, so a generate
# that fails partway can leave a fresh-mtimed ent/client.go over an incomplete
# tree -- which would satisfy the sentinel from then on. Have make remove a
# target whose recipe failed.
.DELETE_ON_ERROR:

.PHONY: help
help: ## Show this help message
	@echo "Available targets:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'

.PHONY: pre-commit
pre-commit: fmt vet tidy test build check-no-local-models check-compose-defaults check-public-hygiene ## Run all pre-commit checks (matches CI/CD)
	@echo "✅ All pre-commit checks passed!"

.PHONY: fmt
fmt: ## Check code formatting
	@echo "Checking code formatting..."
	@if [ -n "$$($(GOFMT) -l .)" ]; then \
		echo "❌ Code is not formatted. Run 'make fmt-fix' to fix"; \
		$(GOFMT) -l .; \
		exit 1; \
	fi
	@echo "✅ Code is properly formatted"

.PHONY: fmt-fix
fmt-fix: ## Fix code formatting
	@echo "Formatting code..."
	@$(GOFMT) -w .
	@echo "✅ Code formatted"

.PHONY: vet
vet: $(ENT_SENTINEL) ## Run go vet
	@echo "Running go vet..."
	@go vet ./...
	@echo "✅ No vet issues found"

.PHONY: tidy
tidy: $(ENT_SENTINEL) ## Check go.mod and go.sum are tidy
	@echo "Checking go.mod and go.sum..."
	@go mod tidy
	@if [ -n "$$(git diff go.mod go.sum)" ]; then \
		echo "❌ go.mod or go.sum not tidy. Changes:"; \
		git diff go.mod go.sum; \
		echo ""; \
		echo "Run 'git add go.mod go.sum' to include these changes"; \
		exit 1; \
	fi
	@echo "✅ Dependencies are tidy"

.PHONY: test
test: $(ENT_SENTINEL) ## Run all tests
	@echo "Running tests..."
	@go test ./... -v
	@echo "✅ All tests passed"

.PHONY: test-short
test-short: $(ENT_SENTINEL) ## Run tests without verbose output
	@echo "Running tests..."
	@go test ./...
	@echo "✅ All tests passed"

.PHONY: build
build: $(ENT_SENTINEL) ## Verify build works
	@echo "Verifying  build..."
	@GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build \
		-ldflags="-s -w $(BUILDINFO_LDFLAGS)" \
		-o bootstrap \
		cmd/api-server/main.go
	@rm -f bootstrap
	@echo "✅ Build successful"

.PHONY: install-hooks
install-hooks: ## Install git pre-commit hook
	@echo "Installing pre-commit hook..."
	@mkdir -p .git/hooks
	@echo '#!/bin/sh' > .git/hooks/pre-commit
	@echo '# Auto-installed by make install-hooks' >> .git/hooks/pre-commit
	@echo 'make pre-commit' >> .git/hooks/pre-commit
	@chmod +x .git/hooks/pre-commit
	@echo "✅ Pre-commit hook installed"
	@echo "   The hook will run 'make pre-commit' before each commit"
	@echo "   To skip the hook temporarily, use: git commit --no-verify"

.PHONY: uninstall-hooks
uninstall-hooks: ## Remove git pre-commit hook
	@echo "Removing pre-commit hook..."
	@rm -f .git/hooks/pre-commit
	@echo "✅ Pre-commit hook removed"

.PHONY: clean
clean: ## Clean build artifacts
	@echo "Cleaning build artifacts..."
	@rm -f bootstrap
	@rm -f cmd/api-server/bootstrap
	@echo "✅ Clean complete"

# =============================================================================
# Development helpers
# =============================================================================

.PHONY: generate
generate: ## Run go generate (Ent code generation)
	@echo "Running go generate..."
	@go generate ./ent
	@echo "✅ Code generation complete"

.PHONY: run
run: ## Run the API server locally
	@echo "Starting API server..."
	@go run cmd/api-server/main.go

.PHONY: watch
watch: ## Watch for changes and rebuild (requires entr)
	@echo "Watching for changes (ctrl-c to stop)..."
	@find . -name '*.go' | entr -r go run cmd/api-server/main.go

# =============================================================================
# Local development stack (hybrid: Postgres in Docker, native backend + web)
# See ADR 0x018. The only manual step is: cp .env.example .env
# =============================================================================

COMPOSE ?= docker compose
DEV_DIR := .dev
WEB_DIR := web/app

.PHONY: check-env
check-env: ## Validate local environment (.env, DB settings, secrets, docker)
	@./scripts/check-env.sh

.PHONY: db-up
db-up: ## Start Postgres (compose db service) and wait until healthy
	@echo "Starting database (waits for healthcheck)..."
	@$(COMPOSE) up -d --wait db && echo "✅ Database healthy" || { echo "❌ Database did not become healthy"; exit 1; }

.PHONY: db-down
db-down: ## Stop the Postgres service (data volume is kept)
	@$(COMPOSE) stop db

# Local LLM backend (LLM_BACKEND=local, ADR 0x018) — pinned versions, single
# source of truth for local dev and CI. Override on the command line if needed:
#   make local-llm-up LOCAL_LLM_MODEL=llama3.2:1b
# OLLAMA_VERSION pins the Linux/CI installer; macOS installs via Homebrew
# (brew does not pin, so local-llm-install prints the installed version for
# the operator to verify instead).
OLLAMA_VERSION ?= 0.32.9
LOCAL_LLM_MODEL ?= qwen2.5:0.5b
LOCAL_LLM_BASE_URL ?= http://localhost:11434/v1

.PHONY: print-local-llm-pins
print-local-llm-pins: ## Print OLLAMA_VERSION and LOCAL_LLM_MODEL as shell var assignments (for scripts/ci-image.sh)
	@echo "OLLAMA_VERSION=$(strip $(OLLAMA_VERSION))"
	@echo "LOCAL_LLM_MODEL=$(strip $(LOCAL_LLM_MODEL))"

.PHONY: run-mock
run-mock: ## Run the API locally in mock LLM mode (no provider egress, no keys)
	@ENV=development LLM_BACKEND=mock go run cmd/api-server/main.go

.PHONY: run-local
run-local: ## Run the API locally against a real local model server (e.g. Ollama)
	@ENV=development LLM_BACKEND=local \
		LOCAL_LLM_MODEL=$(LOCAL_LLM_MODEL) LOCAL_LLM_BASE_URL=$(LOCAL_LLM_BASE_URL) \
		go run cmd/api-server/main.go

.PHONY: local-llm-install
local-llm-install: ## Install Ollama (macOS: Homebrew; Linux/CI: pinned OLLAMA_VERSION)
	@if command -v ollama >/dev/null 2>&1; then \
		echo "✅ ollama already installed: $$(ollama --version 2>&1 | grep -o 'version is [0-9.]*' | sed 's/version is /v/')"; \
	elif [ "$$(uname -s)" = "Darwin" ]; then \
		echo "Installing ollama via Homebrew..."; \
		brew install ollama; \
		echo "Installed: $$(ollama --version 2>&1 | grep -o 'version is [0-9.]*' | sed 's/version is /v/') (brew does not pin; CI pins $(OLLAMA_VERSION))"; \
	else \
		echo "Installing ollama $(OLLAMA_VERSION) via official installer..."; \
		curl -fsSL https://ollama.com/install.sh | OLLAMA_VERSION=$(OLLAMA_VERSION) sh; \
	fi

.PHONY: local-llm-up
local-llm-up: ## Start the Ollama server (if not running) and pull LOCAL_LLM_MODEL
	@if ! curl -fsS $(LOCAL_LLM_BASE_URL)/models >/dev/null 2>&1; then \
		command -v ollama >/dev/null 2>&1 || { echo "❌ ollama not installed — run 'make local-llm-install' first"; exit 1; }; \
		mkdir -p $(DEV_DIR); \
		echo "Starting ollama server (log: $(DEV_DIR)/ollama-serve.log)..."; \
		nohup ollama serve > $(DEV_DIR)/ollama-serve.log 2>&1 & \
		for i in $$(seq 1 30); do \
			curl -fsS $(LOCAL_LLM_BASE_URL)/models >/dev/null 2>&1 && break; \
			[ $$i -eq 30 ] && { echo "❌ ollama did not become ready in 30s; see $(DEV_DIR)/ollama-serve.log"; exit 1; }; \
			sleep 1; \
		done; \
	fi
	@echo "✅ ollama server ready at $(LOCAL_LLM_BASE_URL)"
	@echo "Pulling $(LOCAL_LLM_MODEL) (no-op if already present)..."
	@ollama pull $(LOCAL_LLM_MODEL)
	@echo "✅ $(LOCAL_LLM_MODEL) available"

.PHONY: web
web: $(WEB_DIR)/node_modules/.stamp ## Run the Angular dev server (:4200)
	@cd $(WEB_DIR) && npm start

.PHONY: web-e2e
web-e2e: $(WEB_DIR)/node_modules/.stamp ## Run Playwright frontend E2E tests (requires the API already running on :8080 — see web/app/e2e/README.md)
	@cd $(WEB_DIR) && npm run e2e

# npm ci runs only when the manifests change, keyed via a stamp file.
$(WEB_DIR)/node_modules/.stamp: $(WEB_DIR)/package.json $(WEB_DIR)/package-lock.json
	@cd $(WEB_DIR) && npm ci
	@touch $@

# The unit-test builder writes lcov with app-relative SF: paths
# (SF:src/app/app.ts), because each Angular workspace is its own project root.
# Codecov matches paths against the repo root, so `src/` from either app would
# land in neither component and both apps' `src/app/app.ts` would collide. Same
# problem the e2e pipeline solves with APP_ROOT in
# web/app/e2e/scripts/merge-coverage.mjs — solved the same way,
# by prefixing on the way out. This can't be codecov.yml's `fixes:` instead:
# that mapping is global, and `src/` is ambiguous between the two apps.
# The negative lookahead keeps a re-run from prefixing twice.
define rewrite_lcov_paths
	@perl -pi -e 's|^SF:(?!$(1)/)|SF:$(1)/|' $(2)
	@echo "✅ rewrote SF: paths to repo-root-relative ($(2))"
endef

.PHONY: web-unit-coverage
web-unit-coverage: $(WEB_DIR)/node_modules/.stamp ## Run the web app unit tests with coverage (lcov for the web-unit Codecov flag)
	@cd $(WEB_DIR) && npm run test:coverage
	$(call rewrite_lcov_paths,$(WEB_DIR),$(WEB_DIR)/coverage/what-iff/lcov.info)

.PHONY: lcov-summary
lcov-summary: ## Print an lcov file's line-coverage total (LCOV=path/to/lcov.info)
	@test -n "$(LCOV)" || { echo "❌ set LCOV=<path to an lcov.info>"; exit 1; }
	@test -f "$(LCOV)" || { echo "❌ no such lcov file: $(LCOV)"; exit 1; }
	@awk -F: '/^LF:/ { found += $$2 } /^LH:/ { hit += $$2 } \
		END { if (found == 0) { print "$(LCOV): no lines instrumented"; exit } \
		printf "%s: %.1f%% of statements (%d/%d lines)\n", "$(LCOV)", hit * 100 / found, hit, found }' "$(LCOV)"

.PHONY: dev-up
dev-up: ## Build and start the API in the background with a readiness poll
	@if [ -f $(DEV_DIR)/api-server.pid ] && kill -0 $$(cat $(DEV_DIR)/api-server.pid) 2>/dev/null; then \
		echo "❌ API already running (pid $$(cat $(DEV_DIR)/api-server.pid)) — run 'make dev-down' first"; exit 1; \
	fi
	@mkdir -p $(DEV_DIR)
	@go build -ldflags "$(BUILDINFO_LDFLAGS)" -o $(DEV_DIR)/api-server ./cmd/api-server
	@$(DEV_DIR)/api-server > $(DEV_DIR)/api-server.log 2>&1 & echo $$! > $(DEV_DIR)/api-server.pid
	@echo "Waiting for API readiness (pid $$(cat $(DEV_DIR)/api-server.pid))..."
	@port=$$(grep -E '^SERVER_PORT=' .env 2>/dev/null | tail -1 | cut -d= -f2); \
	port=$${port:-$${SERVER_PORT:-8080}}; \
	pid=$$(cat $(DEV_DIR)/api-server.pid); \
	for i in $$(seq 1 30); do \
		if curl -fsS "http://localhost:$$port/api/health" >/dev/null 2>&1; then echo "✅ API ready on :$$port"; exit 0; fi; \
		if ! kill -0 $$pid 2>/dev/null; then echo "❌ API exited; see $(DEV_DIR)/api-server.log"; rm -f $(DEV_DIR)/api-server.pid; exit 1; fi; \
		sleep 1; \
	done; \
	echo "❌ API not ready after 30s; killing pid $$pid — see $(DEV_DIR)/api-server.log"; \
	kill $$pid 2>/dev/null; rm -f $(DEV_DIR)/api-server.pid; exit 1

.PHONY: dev-up-cover
dev-up-cover: ## Like dev-up, but the binary is built with -cover (GOCOVERDIR=.dev/gocov)
	@if [ -f $(DEV_DIR)/api-server.pid ] && kill -0 $$(cat $(DEV_DIR)/api-server.pid) 2>/dev/null; then \
		echo "❌ API already running (pid $$(cat $(DEV_DIR)/api-server.pid)) — run 'make dev-down' first"; exit 1; \
	fi
	@mkdir -p $(DEV_DIR) $(DEV_DIR)/gocov
	@go build -cover -ldflags "$(BUILDINFO_LDFLAGS)" -o $(DEV_DIR)/api-server ./cmd/api-server
	@GOCOVERDIR=$(DEV_DIR)/gocov $(DEV_DIR)/api-server > $(DEV_DIR)/api-server.log 2>&1 & echo $$! > $(DEV_DIR)/api-server.pid
	@echo "Waiting for API readiness (pid $$(cat $(DEV_DIR)/api-server.pid))..."
	@port=$$(grep -E '^SERVER_PORT=' .env 2>/dev/null | tail -1 | cut -d= -f2); \
	port=$${port:-$${SERVER_PORT:-8080}}; \
	pid=$$(cat $(DEV_DIR)/api-server.pid); \
	for i in $$(seq 1 30); do \
		if curl -fsS "http://localhost:$$port/api/health" >/dev/null 2>&1; then echo "✅ API ready on :$$port (GOCOVERDIR=$(DEV_DIR)/gocov)"; exit 0; fi; \
		if ! kill -0 $$pid 2>/dev/null; then echo "❌ API exited; see $(DEV_DIR)/api-server.log"; rm -f $(DEV_DIR)/api-server.pid; exit 1; fi; \
		sleep 1; \
	done; \
	echo "❌ API not ready after 30s; killing pid $$pid — see $(DEV_DIR)/api-server.log"; \
	kill $$pid 2>/dev/null; rm -f $(DEV_DIR)/api-server.pid; exit 1

.PHONY: dev-down
dev-down: ## Stop the background API (kills only a verified api-server PID)
	@if [ -f $(DEV_DIR)/api-server.pid ]; then \
		pid=$$(cat $(DEV_DIR)/api-server.pid); \
		if ps -p $$pid -o comm= 2>/dev/null | grep -qE '(^|/)api-server$$'; then \
			kill $$pid && echo "✅ API (pid $$pid) stopped"; \
		else \
			echo "⚠️  pid $$pid is not an api-server process; not killing"; \
		fi; \
		rm -f $(DEV_DIR)/api-server.pid; \
	else \
		echo "No $(DEV_DIR)/api-server.pid — nothing to stop"; \
	fi

.PHONY: local-superuser
local-superuser: ## Create/promote a local admin interactively (never SUPERADMIN_*)
	@go run ./cmd/create-superuser

# =============================================================================
# Hermetic testing (mock LLM, no provider keys, no egress)
# =============================================================================

.PHONY: test-ci
test-ci: $(ENT_SENTINEL) ## Run the hermetic CI suite (mock mode, dummy keys, race detector, coverage)
	@echo "Running hermetic test suite (ENV=test LLM_BACKEND=mock, race detector)..."
	@mkdir -p coverage
	@ENV=test LLM_BACKEND=mock OPENAI_API_KEY=dummy-ci-key \
		ANTHROPIC_API_KEY= ZAI_API_KEY= GEMINI_API_KEY= MISTRAL_API_KEY= \
		DEEPSEEK_API_KEY= QWEN_API_KEY= XIAOMI_API_KEY= \
		go test -race \
			-coverprofile=coverage/go-unit.out -covermode=atomic \
			-coverpkg=./cmd/...,./internal/...,./ent/schema/... \
			./...
	@echo "✅ Hermetic test suite passed (profile: coverage/go-unit.out)"

.PHONY: coverage-summary
coverage-summary: ## Print a Go coverage profile's total (PROFILE=coverage/go-unit.out)
	@test -n "$(PROFILE)" || { echo "❌ set PROFILE=<path to a Go coverage profile>"; exit 1; }
	@test -f "$(PROFILE)" || { echo "❌ no such coverage profile: $(PROFILE)"; exit 1; }
	@# `go tool cover -func` is the only correct reader here: a -coverpkg
	@# profile repeats each block once per test binary, so summing the file
	@# arithmetically undercounts badly.
	@go tool cover -func="$(PROFILE)" | tail -1 | awk '{print "$(PROFILE): " $$NF}'

.PHONY: test-ci-local
test-ci-local: ## Real-inference smoke test against a local model server (CI opt-in job)
	@echo "Running local-model smoke test (ENV=test LLM_BACKEND=local, model $(LOCAL_LLM_MODEL))..."
	@ENV=test LLM_BACKEND=local \
		LOCAL_LLM_MODEL=$(LOCAL_LLM_MODEL) LOCAL_LLM_BASE_URL=$(LOCAL_LLM_BASE_URL) \
		./scripts/local-e2e.sh
	@echo "✅ Local-model smoke test passed"

.PHONY: check-no-local-models
# Depends on the ent sentinel because this is the only check target that shells out
# to `go` (`go list -deps`). Under parallel make it would otherwise run alongside
# `go generate ./ent`, which transiently rewrites go.sum, and the audit fails with a
# resolution error that has nothing to do with the import graph.
check-no-local-models: $(ENT_SENTINEL) ## Audit that no embedded/downloaded local model runtime is depended on (C2)
	@./scripts/check-no-local-models.sh

.PHONY: check-compose-defaults
check-compose-defaults: ## Fail if docker-compose.yml or its Go config re-ships an insecure default
	@./scripts/check-compose-defaults.sh

.PHONY: check-public-hygiene
check-public-hygiene: ## Fail if secrets, AI attribution, or absolute home paths appear in tracked files
	@./scripts/check-public-hygiene.sh

.PHONY: check-skill-symlinks
check-skill-symlinks: ## Verify .claude/skills/ symlinks match .agents/skills/ and CLAUDE.md/GEMINI.md exist
	@./scripts/check-skill-symlinks.sh

.PHONY: mock-e2e
mock-e2e: ## Hermetic backend end-to-end test (isolated DB, mock LLM)
	@./scripts/mock-e2e.sh

# =============================================================================
# TEMPORARY: local replay of the PR CI checks (GitHub Actions minutes are
# scarce). Remove together with scripts/ci-local.sh and .claude/skills/ci-local
# once that stops being a constraint.
# =============================================================================

.PHONY: ci-local
ci-local: ## Run the PR CI checks locally (ARGS="--e2e --only go,frontend ..."); report in .dev/ci-local/report.md
	@./scripts/ci-local.sh $(ARGS)

.PHONY: ci-local-post
ci-local-post: ## Run the PR CI checks locally and post/update the report as a sticky PR comment (ARGS="--pr N")
	@./scripts/ci-local.sh --post $(ARGS)

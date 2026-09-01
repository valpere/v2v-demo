# v2v-demo — dev tasks. Throwaway client demo; keep this simple.
# `make` or `make help` lists targets.

GO      ?= go
BIN     ?= bot
PKG     := ./...
ARGS    ?=
STEP    ?=

.DEFAULT_GOAL := help

## ── build & run ───────────────────────────────────────────────────

.PHONY: build
build: ## compile the bot to ./bot
	$(GO) build -o $(BIN) ./cmd/bot

.PHONY: run
run: build ## run the bot (long-poll; needs .env)
	@./$(BIN) & pid=$$!; trap 'kill -INT $$pid 2>/dev/null; wait $$pid; exit 0' INT TERM; wait $$pid
	# runs the binary (not `go run`) and swallows the Ctrl-C: the bot catches
	# SIGINT, shuts down, and this recipe exits 0 — no "Error 1" / "Interrupt"

.PHONY: install
install: ## go install the bot into GOBIN
	$(GO) install ./cmd/bot

## ── quality gate ──────────────────────────────────────────────────

.PHONY: check
check: fmt-check vet test ## the per-step gate: gofmt + vet + race tests (run before every commit)

.PHONY: test
test: ## race-enabled tests
	$(GO) test $(PKG) -race

.PHONY: test-fast
test-fast: ## tests without the race detector
	$(GO) test $(PKG)

.PHONY: cover
cover: ## race tests + HTML coverage report (coverage.out / coverage.html)
	$(GO) test $(PKG) -race -coverprofile=coverage.out -covermode=atomic
	$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "coverage.html written"

.PHONY: vet
vet: ## go vet
	$(GO) vet $(PKG)

.PHONY: fmt
fmt: ## gofmt -w the tree
	gofmt -w .

.PHONY: fmt-check
fmt-check: ## fail if anything is not gofmt-clean
	@out=$$(gofmt -l .); if [ -n "$$out" ]; then echo "not gofmt-clean:"; echo "$$out"; exit 1; fi

.PHONY: tidy
tidy: ## go mod tidy + verify
	$(GO) mod tidy
	$(GO) mod verify

## ── demo helpers ──────────────────────────────────────────────────

.PHONY: env
env: ## create .env from .env.example if missing
	@test -f .env && echo ".env already exists" || { cp .env.example .env && echo "created .env — fill in the secrets"; }

.PHONY: audition
audition: ## TTS voice audition: make audition ARGS="a" | "b" | "<voiceID> [text]"
	./minions/tts-audition.sh $(ARGS)

.PHONY: hook-logs
hook-logs: ## tail the session-recall / session-end hook log
	@tail -n 40 ~/.cache/$$(basename "$$(git rev-parse --show-toplevel)")/hooks.log 2>/dev/null || echo "(no hook log yet)"

.PHONY: recall-stats
recall-stats: ## session index state
	@session-indexer stats --db .claude/sessions.db 2>/dev/null || echo "(no session index yet)"

## ── opencode subagents (Ollama Pro; see HANDOFF.md §5) ─────────────

.PHONY: subagent-coder
subagent-coder: ## implement a build-order step: make subagent-coder STEP=3
	@test -n "$(STEP)" || { echo "set STEP=<n>"; exit 1; }
	opencode run --agent coder --auto "Implement build-order step $(STEP) exactly per .agents/plan.md. Do not go beyond that step."

.PHONY: subagent-tester
subagent-tester: ## run build/vet/test -race via the tester agent, minimal fixes
	opencode run --agent tester --auto "Run build/vet/test -race, fix minimal, report to .agents/test-report.md."

.PHONY: subagent-reviewer
subagent-reviewer: ## review packages: make subagent-reviewer ARGS="internal/dialog internal/kb"
	opencode run --agent reviewer "Review $(ARGS) for bugs, security, non-idiomatic Go."

## ── housekeeping ──────────────────────────────────────────────────

.PHONY: clean
clean: ## remove build + coverage artifacts
	rm -f $(BIN) coverage.out coverage.html

.PHONY: help
help: ## list targets
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) \
		| sort \
		| awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

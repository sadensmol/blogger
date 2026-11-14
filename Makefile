DEV_ENV ?= $(HOME)/.config/blogger/dev.env

# Local test mode: run the bot on this Mac against the dev Slack app.
# The recipe sources the env file in a subshell and never prints it.
define LOCAL_ENV
test -f "$(DEV_ENV)" || { echo "Missing $(DEV_ENV). Create it from deploy/dev.env.example (see docs/setup.md, section 2)." >&2; exit 1; }; \
mkdir -p "$(CURDIR)/.scratch/dev-data"; \
set -a; . "$(DEV_ENV)"; set +a; \
export BLOGGER_DATA_DIR="$(CURDIR)/.scratch/dev-data" BLOGGER_AGENT_DIR="$(CURDIR)/agent"
endef

TAG ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo dev)

.PHONY: build test test-live image deploy-local deploy-ci secret-scan run-local debug-local

build:
	go build -ldflags "-X main.version=$(TAG)" -o bin/blogger ./cmd/blogger

test:
	go test ./...

test-live:
	go test -tags live ./internal/llm/... -v -timeout 30m

image:
	docker build --platform linux/arm64 --build-arg TAG=$(TAG) -t ghcr.io/sadensmol/blogger:local-$(TAG) .

deploy-local:
	deploy/deploy-local.sh

deploy-ci:
	deploy/deploy-ci.sh

secret-scan:
	docker run --rm -v "$(CURDIR)":/repo ghcr.io/gitleaks/gitleaks:v8.30.1 git -v /repo

run-local:
	@$(LOCAL_ENV); \
	go run ./cmd/blogger serve

debug-local:
	@command -v dlv >/dev/null || { echo "dlv is missing. Install it: go install github.com/go-delve/delve/cmd/dlv@latest" >&2; exit 1; }
	@$(LOCAL_ENV); \
	dlv debug ./cmd/blogger -- serve

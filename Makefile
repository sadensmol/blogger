TAG ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo dev)

.PHONY: build test test-live image deploy-local deploy-ci secret-scan

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

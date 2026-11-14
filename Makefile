TAG ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo dev)

.PHONY: build test test-live image deploy secret-scan

build:
	go build -ldflags "-X main.version=$(TAG)" -o bin/blogger ./cmd/blogger

test:
	go test ./...

test-live:
	go test -tags live ./internal/llm/... -v

image:
	docker build --platform linux/arm64 --build-arg TAG=$(TAG) -t blogger:$(TAG) .

deploy:
	deploy/deploy.sh $(TAG)

secret-scan:
	docker run --rm -v "$(CURDIR)":/repo ghcr.io/gitleaks/gitleaks:v8.30.1 git -v /repo

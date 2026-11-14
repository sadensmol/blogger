FROM golang:1.27.3 AS build
ARG TAG=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "-s -w -X main.version=${TAG}" -o /out/blogger ./cmd/blogger

FROM debian:trixie-slim
SHELL ["/bin/bash", "-o", "pipefail", "-c"]
ARG UID=1000
ARG CLAUDE_VERSION=2.1.318
ARG CODEX_VERSION=0.169.0
RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates curl \
 && rm -rf /var/lib/apt/lists/* \
 && useradd --uid ${UID} --create-home --home-dir /home/blogger --shell /bin/sh blogger \
 && curl -fsSL "https://github.com/openai/codex/releases/download/rust-v${CODEX_VERSION}/codex-aarch64-unknown-linux-musl.tar.gz" \
    | tar -xz -C /usr/local/bin \
 && mv /usr/local/bin/codex-aarch64-unknown-linux-musl /usr/local/bin/codex \
 && chmod 0755 /usr/local/bin/codex
USER blogger
RUN curl -fsSL https://claude.ai/install.sh | bash -s ${CLAUDE_VERSION}
USER root
COPY --from=build /out/blogger /usr/local/bin/blogger
COPY agent /app/agent
RUN mkdir -p /data /home/blogger/.codex && chown blogger:blogger /data /home/blogger/.codex
ENV BLOGGER_DATA_DIR=/data PATH=/home/blogger/.local/bin:$PATH
USER blogger
ENTRYPOINT ["blogger"]
CMD ["serve"]

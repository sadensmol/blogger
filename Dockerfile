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
# The bot runs as root and starts the agents as blogger, so an agent cannot read
# the bot's environment (the Slack tokens) or the database in /data.
# The jobs folder is root's and outside the agent home: the agent cannot rename or replace a job.
RUN install -d -m 0700 /data \
 && install -d -m 0711 /var/lib/blogger /var/lib/blogger/jobs \
 && install -d -m 0700 -o blogger -g blogger /home/blogger/.codex
ENV BLOGGER_DATA_DIR=/data BLOGGER_JOBS_DIR=/var/lib/blogger/jobs BLOGGER_AGENT_USER=blogger \
    PATH=/home/blogger/.local/bin:$PATH
ENTRYPOINT ["blogger"]
CMD ["serve"]

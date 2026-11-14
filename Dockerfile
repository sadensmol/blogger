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
ARG BROWSER_UID=1001
ARG CLAUDE_VERSION=2.1.318
ARG CODEX_VERSION=0.169.0
RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates curl chromium \
 && rm -rf /var/lib/apt/lists/* \
 && useradd --uid ${UID} --create-home --home-dir /home/blogger --shell /bin/sh blogger \
 && useradd --uid ${BROWSER_UID} --user-group --no-create-home --home-dir /var/lib/chromium --shell /usr/sbin/nologin chromium \
 && install -d -m 0700 -o chromium -g chromium /var/lib/chromium \
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
# blogger owns its home: root's PATH and entrypoint never point into it, so the agent cannot plant a program
# that root runs. The bot starts claude by its full path, as blogger.
# Chromium runs as chromium: it holds no login, and its home is its own. It cannot read the agent's home or processes.
ENV BLOGGER_DATA_DIR=/data BLOGGER_JOBS_DIR=/var/lib/blogger/jobs BLOGGER_AGENT_USER=blogger BLOGGER_BROWSER_USER=chromium \
    BLOGGER_CLAUDE_BIN=/home/blogger/.local/bin/claude
ENTRYPOINT ["/usr/local/bin/blogger"]
CMD ["serve"]

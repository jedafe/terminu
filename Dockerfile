# syntax=docker/dockerfile:1
# Ref: .teos/sections/12 — single-artifact deploy: SPA embedded via go:embed.

FROM oven/bun:1 AS web
WORKDIR /build
COPY apps/web/package.json apps/web/bun.lock ./
RUN bun install --frozen-lockfile
COPY apps/web ./
RUN bun run build

FROM golang:1-bookworm AS go
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /build/build ./internal/server/web
RUN CGO_ENABLED=0 go build -ldflags='-s -w' -o /out/terminu-server ./cmd/terminu-server

# util-linux ships nsenter — required for TU_LOCAL_TARGET_MODE=nsenter
# (ADR-006 host-access path; needs privileged + pid:host at runtime).
FROM debian:bookworm-slim
RUN apt-get update \
    && apt-get install -y --no-install-recommends util-linux ca-certificates \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --system --uid 10001 --home-dir /data --shell /usr/sbin/nologin terminu \
    && mkdir -p /data && chown terminu:terminu /data
COPY --from=go /out/terminu-server /usr/local/bin/terminu-server
# §12: default `container` — least privilege; the process runs unprivileged.
# Host access (ADR-006) is opt-in at deploy time:
#   nsenter → docker run --user root --privileged --pid host \
#               -e TU_LOCAL_TARGET_MODE=nsenter -e TU_HOST_USER=<user>
ENV TU_PORT=7700 \
    TU_BIND=0.0.0.0 \
    TU_DATA_DIR=/data \
    TU_LOCAL_TARGET_MODE=container
USER terminu
VOLUME /data
EXPOSE 7700
ENTRYPOINT ["terminu-server"]

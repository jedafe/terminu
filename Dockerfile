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
    && rm -rf /var/lib/apt/lists/*
COPY --from=go /out/terminu-server /usr/local/bin/terminu-server
ENV TU_PORT=7700 \
    TU_BIND=0.0.0.0 \
    TU_DATA_DIR=/data \
    TU_LOCAL_TARGET_MODE=nsenter \
    TU_HOST_USER=japa
VOLUME /data
EXPOSE 7700
ENTRYPOINT ["terminu-server"]

#!/usr/bin/env bash
# Ref: .teos/sections/12-deployment-operations.md — the shell.japa.to
# reference deployment. Layout: <deploy>/repo (git checkout),
# <deploy>/bin/terminu-server (embedded SPA), systemd unit `terminu`.
# Run as the deploy user (japa): NOPASSWD sudoers covers the restart.
set -euo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
DEPLOY="$(dirname "$REPO")" # e.g. /home/japa/terminu
BIN="$DEPLOY/bin/terminu-server"
export PATH="$HOME/.bun/bin:$PATH" # bun lives user-local, not on PATH

cd "$REPO"
git pull --ff-only
git submodule update --init

# The SPA is go:embed'd into the binary — rebuild it first, then swap
# the embedded asset dir before `go build`.
(cd apps/web && bun install && bun run build)
rsync -a --delete apps/web/build/ internal/server/web/

# Atomic binary swap — writing over a running executable can ETXTBSY.
go build -o "$BIN.new" ./cmd/terminu-server
mv "$BIN.new" "$BIN"

sudo -n systemctl restart terminu
systemctl --no-pager status terminu | head -5

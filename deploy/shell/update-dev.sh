#!/usr/bin/env bash
# Ref: .teos/sections/12-deployment-operations.md — dev.shell.japa.to is
# the staging gate: every build lands here first and is promoted to
# shell.japa.to only after verification (see promote.sh). Layout mirrors
# the prod script: <deploy>/repo (dev git checkout, distinct from the
# prod <deploy>/terminu checkout so concurrent builds never collide),
# <deploy>/bin/terminu-dev, systemd unit `terminu-dev`.
set -euo pipefail

# Same private-copy trick as update.sh — git pull can replace this file
# mid-run.
if [[ "${_UPDATE_SH_COPY:-}" != 1 ]]; then
	REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
	_copy="$(mktemp /tmp/terminu-update.XXXXXX.sh)"
	cp "${BASH_SOURCE[0]}" "$_copy"
	export REPO
	_UPDATE_SH_COPY=1 exec bash "$_copy" "$@"
fi
if [[ "${_UPDATE_SH_COPY:-}" == 1 ]]; then
	trap 'rm -f "$0"' EXIT
fi
: "${REPO:?internal error — REPO unset}"
DEPLOY="$(dirname "$REPO")" # e.g. /home/japa/terminu
BIN="$DEPLOY/bin/terminu-dev"
export PATH="$HOME/.bun/bin:$PATH"

cd "$REPO"
git pull --ff-only
git submodule update --init

(cd apps/web && bun install && bun run build)
rsync -a --delete apps/web/build/ internal/server/web/

go build -o "$BIN.new" ./cmd/terminu-server
mv "$BIN.new" "$BIN"

if [[ "${1:-}" == "--no-restart" ]]; then
	echo "dev binary swapped at $BIN — restart deferred:"
	echo "  sudo systemctl restart terminu-dev"
	exit 0
fi

sudo -n systemd-run --unit=terminu-dev-bounce --on-active=2 \
	systemctl restart terminu-dev
echo "terminu-dev restart scheduled in ~2s"

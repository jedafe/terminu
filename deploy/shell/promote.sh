#!/usr/bin/env bash
# Ref: .teos/sections/12-deployment-operations.md — promotion gate.
# Copies the dev-tested binary (bin/terminu-dev, built and verified on
# dev.shell.japa.to) to the prod binary and schedules the prod restart.
# Binary promotion — what you tested is what ships; no second build.
set -euo pipefail

# Resolve <deploy> from this script's location: deploy/shell/promote.sh
# lives two levels under the prod checkout.
REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
DEPLOY="$(dirname "$REPO")"
DEV_BIN="$DEPLOY/bin/terminu-dev"
PROD_BIN="$DEPLOY/bin/terminu-server"

[[ -x "$DEV_BIN" ]] || { echo "no dev binary at $DEV_BIN — run deploy/shell/update-dev.sh first" >&2; exit 1; }

# Guard against regressing prod with an older dev build.
if [[ "$PROD_BIN" -nt "$DEV_BIN" && "${1:-}" != "--force" ]]; then
	echo "prod binary is NEWER than dev binary:"
	ls -l "$DEV_BIN" "$PROD_BIN"
	echo "refusing to regress — rebuild dev first, or pass --force" >&2
	exit 1
fi

echo "promoting:"
sha256sum "$DEV_BIN" "$PROD_BIN" 2>/dev/null || true
install -m755 "$DEV_BIN" "$PROD_BIN.new"
mv "$PROD_BIN.new" "$PROD_BIN"

if [[ "${1:-}" == "--no-restart" ]]; then
	echo "prod binary swapped at $PROD_BIN — restart deferred:"
	echo "  sudo systemctl restart terminu"
	exit 0
fi

# Same deferred-bounce model as update.sh: a synchronous restart would
# kill this very session when run from a terminu-hosted shell.
sudo -n systemd-run --unit=terminu-bounce --on-active=2 \
	systemctl restart terminu
echo "prod restart scheduled in ~2s — live sessions will end (run this from outside terminu, or accept the bounce)"

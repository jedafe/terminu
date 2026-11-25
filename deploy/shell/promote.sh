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

# Refuse when invoked inside a prod session: the bounce ends every prod
# terminu session including this one. Run from a dev.shell.japa.to
# session or an out-of-band shell instead. --force overrides.
if [[ "${1:-}" != "--force" && "${1:-}" != "--no-restart" ]]; then
	_p=$$
	while [[ "$_p" -gt 1 ]]; do
		_c=$(ps -o comm= -p "$_p" 2>/dev/null) || break
		if [[ "$_c" == terminu-server ]]; then
			echo "refusing: this shell is inside a prod terminu session —" >&2
			echo "the restart ends every prod session, this one included." >&2
			echo "run promote.sh from a dev.shell.japa.to session, or pass --force." >&2
			exit 1
		fi
		_p=$(ps -o ppid= -p "$_p" 2>/dev/null | tr -d ' ')
	done
fi

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

#!/usr/bin/env bash
# Ref: .teos/sections/12-deployment-operations.md — the shell.japa.to
# reference deployment. Layout: <deploy>/repo (git checkout),
# <deploy>/bin/terminu-server (embedded SPA), systemd unit `terminu`.
# Run as the deploy user (japa): NOPASSWD sudoers covers the restart.
set -euo pipefail

# Bash reads scripts incrementally — if `git pull` replaces this file
# mid-run the remaining bytes get misread. Run a private copy instead;
# REPO is resolved in the outer script and passed through the env, since
# the copy's own path sits in /tmp and can't resolve the repo.
if [[ "${_UPDATE_SH_COPY:-}" != 1 ]]; then
	REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
	_copy="$(mktemp /tmp/terminu-update.XXXXXX.sh)"
	cp "${BASH_SOURCE[0]}" "$_copy"
	export REPO
	_UPDATE_SH_COPY=1 exec bash "$_copy" "$@"
fi
# The private copy lives in /tmp — remove it on any exit so repeated
# deploys don't accumulate stale script copies.
if [[ "${_UPDATE_SH_COPY:-}" == 1 ]]; then
	trap 'rm -f "$0"' EXIT
fi

# Refuse when invoked inside a prod session: the restart ends every prod
# terminu session including this one. The gate is update-dev.sh → verify
# on dev.shell.japa.to → promote.sh; --force overrides for hotfixes.
if [[ "${1:-}" != "--force" && "${1:-}" != "--no-restart" ]]; then
	_p=$$
	while [[ "$_p" -gt 1 ]]; do
		_c=$(ps -o comm= -p "$_p" 2>/dev/null) || break
		if [[ "$_c" == terminu-server ]]; then
			echo "refusing: this shell is inside a prod terminu session —" >&2
			echo "the restart ends every prod session, this one included." >&2
			echo "gate: update-dev.sh → verify on dev.shell.japa.to → promote.sh" >&2
			echo "      (run promote.sh from a dev session), or --force for hotfixes." >&2
			exit 1
		fi
		_p=$(ps -o ppid= -p "$_p" 2>/dev/null | tr -d ' ')
	done
	echo "warning: update.sh deploys directly to prod, bypassing the dev gate" >&2
	echo "         (update-dev.sh → promote.sh is the standard flow)" >&2
fi

: "${REPO:?internal error — REPO unset}"
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

if [[ "${1:-}" == "--no-restart" ]]; then
	echo "binary swapped at $BIN — restart deferred:"
	echo "  sudo systemctl restart terminu"
	echo "(live sessions keep running the old build until then)"
	exit 0
fi

# Restart via a transient timer, outside this process tree: sessions are
# server-owned, so a synchronous restart kills every live PTY — including
# the caller's own if this runs inside a terminu-hosted devin/shell.
# The timer survives the caller's death; the bounce still happens.
sudo -n systemd-run --unit=terminu-bounce --on-active=2 \
	systemctl restart terminu
echo "restart scheduled in ~2s — any terminu session running this will end"

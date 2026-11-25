// Package shellenv materializes the managed ZDOTDIR tree embedded in
// the binary (vendored zsh plugins + the chaining .zshrc) under the
// data dir, so every spawned zsh gets the enhancement layer with zero
// host setup. Ref: Rule 3 — the dir derives from TU_DATA_DIR.
package shellenv

import (
	"embed"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
)

//go:embed all:zsh
var zshTree embed.FS

// Env is the materialized shell-enhancement layer under the data dir —
// empty fields degrade to plain spawns when writes fail.
type Env struct {
	ZshDir   string // managed ZDOTDIR (vendored plugins + chaining .zshrc)
	FishFile string // augment script — fish -C 'source <file>'
	BashFile string // managed --rcfile
}

// fish's own suggestions/highlighting are built in — the script adds
// the PATH bins users expect plus binary-backed integrations.
const fishScript = `# terminu managed init — sourced via fish -C (after user config)
fish_add_path $HOME/.local/bin $HOME/bin $HOME/.bun/bin
command -q fzf; and fzf --fish | source
command -q zoxide; and zoxide init fish | source

# Inline-TUI preclear — devin's UI paints inline below the cursor (no
# alt screen), leaving the prompt lines hanging above its block on a
# shallow pane. ED(2)+CUP before exec puts the block flush at the top;
# cleared lines still live in the session's server-side scrollback log.
# Same match set as the zsh/bash hooks, plus fish's and/or/begin and
# newline-separated command lines.
function __tu_preclear_inline_tui --on-event fish_preexec
    set -l re '(^|&&|\|\||;|\||\(|\{|\n)\s*((command|builtin|exec|noglob|time|nohup|doas|nice|sudo|env|then|do|elif|else|and|or|begin|-[A-Za-z0-9]+|[A-Za-z_][A-Za-z0-9_]*=\S+)\s+)*(\S*/)*devin(\s|;|\||&|\)|\}|$)'
    string match -qr -- $re $argv; and printf '\033[H\033[2J'
end
`

const bashScript = `# terminu managed rcfile — chain the user's layers, then augment.
# bash --rcfile is only read by non-login interactive shells, so the
# spawn drops -l; emulate the login chain here — profile files when
# they exist (they chain ~/.bashrc themselves), else the interactive
# rc chain, matching how the shell would have sourced them.
if [[ -f /etc/profile || -f "$HOME/.bash_profile" || -f "$HOME/.bash_login" || -f "$HOME/.profile" ]]; then
    [[ -f /etc/profile ]] && source /etc/profile
    for f in "$HOME/.bash_profile" "$HOME/.bash_login" "$HOME/.profile"; do
        [[ -f $f ]] && { source "$f"; break; }
    done
else
    [[ -f /etc/bash.bashrc ]] && source /etc/bash.bashrc
    [[ -f "$HOME/.bashrc" ]] && source "$HOME/.bashrc"
fi
case ":$PATH:" in *":$HOME/.local/bin:"*) ;; *) export PATH="$HOME/.local/bin:$PATH";; esac
command -v fzf >/dev/null 2>&1 && eval "$(fzf --bash 2>/dev/null)"
command -v zoxide >/dev/null 2>&1 && eval "$(zoxide init bash)"

# bash is not a login shell anymore (--rcfile needs interactive
# non-login) — restore the muscle-memory builtin.
logout() { builtin exit "$@"; }

# Inline-TUI preclear — bash has no preexec, so the DEBUG trap evaluates
# each simple command before it runs; $BASH_COMMAND is the post-alias,
# post-expansion command ("cd x && devin" fires once for "cd", once for
# "devin"). Same match set as the zsh/fish hooks, but it catches every
# invocation form — sudo/exec/path/pipelines — a wrapper function can't.
# Replaces any user DEBUG trap; misses just keep the old look.
__tu_preclear_re=$'(^|&&|\\|\\||;|\\||\\(|\\{|\n)[[:space:]]*((command|builtin|exec|noglob|time|nohup|doas|nice|sudo|env|then|do|elif|else|-[A-Za-z0-9]+|[A-Za-z_][A-Za-z0-9_]*=[^[:space:]]+)[[:space:]]+)*([^[:space:]]*/)*devin([[:space:];|&)}]|$)'
__tu_preclear_inline_tui() {
    [[ -n $__tu_pc ]] && return 0 # guard DEBUG re-entry inside the handler
    __tu_pc=1
    [[ $BASH_COMMAND =~ $__tu_preclear_re ]] && printf '\033[H\033[2J'
    __tu_pc=
}
trap '__tu_preclear_inline_tui' DEBUG
`

// Materialize writes the embedded/config trees under <dataDir> —
// idempotent (a rewrite at startup self-heals drifted files).
// An empty dataDir yields a zero Env (plain spawns) rather than
// writing cwd-relative trees.
func Materialize(dataDir string) Env {
	var e Env
	if dataDir == "" {
		return e
	}
	e.ZshDir = writeZshTree(dataDir)
	e.FishFile = writeFile(filepath.Join(dataDir, "fish", "terminu.fish"), fishScript)
	e.BashFile = writeFile(filepath.Join(dataDir, "bash", "terminu.bash"), bashScript)
	return e
}

func writeFile(path, content string) string {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		slog.Warn("shellenv: mkdir", "path", path, "err", err)
		return ""
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		slog.Warn("shellenv: write", "path", path, "err", err)
		return ""
	}
	return path
}

func writeZshTree(dataDir string) string {
	dir := filepath.Join(dataDir, "zsh")
	err := fs.WalkDir(zshTree, "zsh", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(dir, p[len("zsh"):]), 0o700)
		}
		data, err := zshTree.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dir, p[len("zsh"):]), data, 0o600)
	})
	if err != nil {
		slog.Warn("shellenv: zsh env materialize failed", "err", err)
		return ""
	}
	return dir
}

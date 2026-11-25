# terminu managed ZDOTDIR — vendored enhancements, then your own rc.
# Order: user's ~/.zshrc first (their ZSH_* config lands before plugins
# load), then vendored plugins (syntax-highlighting must wrap last).
TU_ZSH_DIR=${ZDOTDIR}

[[ -f "$HOME/.zshrc" ]] && source "$HOME/.zshrc"

# PROMPT_SP clears a possibly-partial line before each prompt — fine — but
# its inverse-% marker wraps into a stray glyph when spawn/fit cols differ.
# Keep the clear, drop the marker.
PROMPT_EOL_MARK=''

# Inline-TUI preclear — `devin` paints its UI inline at the cursor (it
# never takes the alt screen), so on a shallow pane its block hangs under
# the prompt lines. ED(2)+CUP before exec puts the block flush at the
# top — the same look a deep-scrollback terminal produces. The cleared
# lines still live in the session's server-side scrollback log. Only
# tools verified to render this way belong here; alt-screen tools
# already cover the frame.
# Separators: start/&&/||/;/|/(/{\n. Lead tokens cover qualifiers with
# args (sudo -E, env K=V, nice), K=V assignments, and keyword bodies
# (then/do/else/elif); what the regex misses just keeps the old look.
typeset -r __tu_preclear_re=$'(^|&&|\\|\\||;|\\||\\(|\\{|\n)[[:space:]]*((command|builtin|exec|noglob|time|nohup|doas|nice|sudo|env|then|do|elif|else|-[A-Za-z0-9]+|[A-Za-z_][A-Za-z0-9_]*=[^[:space:]]+)[[:space:]]+)*([^[:space:]]*/)*devin([[:space:];|&)}]|$)'
tu_preclear_inline_tui() {
    [[ "$3" =~ "$__tu_preclear_re" ]] && printf '\e[H\e[2J'
}
autoload -Uz add-zsh-hook
add-zsh-hook preexec tu_preclear_inline_tui

# Vendored inside the binary — no packages required.
[[ -f "$TU_ZSH_DIR/vendor/zsh-autosuggestions/zsh-autosuggestions.zsh" ]] \
    && source "$TU_ZSH_DIR/vendor/zsh-autosuggestions/zsh-autosuggestions.zsh"
[[ -f "$TU_ZSH_DIR/vendor/zsh-syntax-highlighting/zsh-syntax-highlighting.zsh" ]] \
    && source "$TU_ZSH_DIR/vendor/zsh-syntax-highlighting/zsh-syntax-highlighting.zsh"

# Binary-backed integrations — only wire them when the binary exists.
if command -v fzf >/dev/null 2>&1; then
    [[ -f "$TU_ZSH_DIR/vendor/key-bindings.zsh" ]] \
        && source "$TU_ZSH_DIR/vendor/key-bindings.zsh"
fi
if command -v zoxide >/dev/null 2>&1; then
    eval "$(zoxide init zsh)"
fi

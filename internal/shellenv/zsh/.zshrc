# terminu managed ZDOTDIR — vendored enhancements, then your own rc.
# Order: user's ~/.zshrc first (their ZSH_* config lands before plugins
# load), then vendored plugins (syntax-highlighting must wrap last).
TU_ZSH_DIR=${ZDOTDIR}

[[ -f "$HOME/.zshrc" ]] && source "$HOME/.zshrc"

# PROMPT_SP clears a possibly-partial line before each prompt — fine — but
# its inverse-% marker wraps into a stray glyph when spawn/fit cols differ.
# Keep the clear, drop the marker.
PROMPT_EOL_MARK=''

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

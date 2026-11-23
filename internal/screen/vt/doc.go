/*
Package vt is a vt10x terminal emulation backend, influenced largely by
st, rxvt, xterm, and iTerm as reference. Use it for terminal muxing, a
terminal emulation frontend, or wherever else you need terminal
emulation.

In development, but very usable.

Vendored from github.com/hinshun/vt10x (MIT, see LICENSE in this dir) at
commit 5011da428d02 — vendored rather than forked upstream because the
detection model needs two additions upstream lacks, and a git-context
build should never depend on a second repo:

  - wcwidth in parse(): wide glyphs occupy two cells; a wide glyph at
    the last column wraps to the next line (st.c/xterm semantics)
  - bounded scrollback: scrollUp pushes copies of lines leaving the top
    of the main screen into a fixed ring (WithScrollback option,
    ScrollbackLen/ScrollbackRow accessors); alt-screen evictions are
    excluded

Local changes are confined to parse.go, state.go, vt.go, vt_posix.go,
vt_other.go.
*/
package vt

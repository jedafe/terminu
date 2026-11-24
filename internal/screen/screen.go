// Ref: .teos/sections/06-screen-interpreter-adapters.md (Screen model)
// One headless VT model per session: cell grid + attrs + cursor + bounded
// scrollback + dirty tracking. The emulator is the vendored vt package
// (internal/screen/vt — forked from hinshun/vt10x, wcwidth + scrollback
// added) so the contract survives any future engine swap.
//
// Concurrency: vt's View methods are caller-locks (they don't lock
// internally); s.mu serializes everything, so we never take the lib's
// public Lock/Unlock — Unlock() resets its change flags, which would make
// pure reads consume the dirty signal. We track dirt ourselves instead.
//
// Known limitation (vt color encoding): colors pack as palette index or
// 0xRRGGBB, so RGB(0,0,N) is indistinguishable from palette index N.
// Detection keys on text + attr bits, not hue, so this is harmless.
package screen

import (
	"strings"
	"sync"

	"github.com/jedafe/terminu/internal/screen/vt"
)

// Cell is one grid position: rune + palette/RGB color + SGR attr bits.
type Cell struct {
	Rune   rune
	FG, BG uint32
	Attr   int16
}

// ScanRange is a half-open row range [Y0, Y1) an adapter may inspect.
type ScanRange struct {
	Y0, Y1 int
}

// Screen is a per-session headless terminal — the `*Screen` of the
// adapter contract. Write is fed from the PTY fan-out (read-only
// observer — it never writes back to the session).
type Screen struct {
	t       vt.Terminal
	mu      sync.Mutex
	cols    int
	rows    int
	dirty   bool
	lastSeq uint64 // last PTY seq the model consumed — resync bookkeeping
}

// New builds a Screen of cols×rows retaining up to scrollback lines that
// scroll off the top of the main screen (0 disables scrollback).
func New(cols, rows, scrollback int) *Screen {
	return &Screen{
		t:    vt.New(vt.WithSize(cols, rows), vt.WithScrollback(scrollback)),
		cols: cols,
		rows: rows,
	}
}

// Write parses PTY output into the grid. Mirrors io.Writer so the session
// read loop can tee straight in.
func (s *Screen) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n, err := s.t.Write(p)
	if n > 0 {
		s.dirty = true
	}
	return n, err
}

// Resize propagates SIGWINCH-equivalent to the model.
func (s *Screen) Resize(cols, rows int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.t.Resize(cols, rows)
	s.cols, s.rows = cols, rows
	s.dirty = true
}

// Cell returns the glyph at (x, y) — 0-indexed, top-left origin.
// Caller must not mutate; Cell is a value copy.
func (s *Screen) Cell(x, y int) Cell {
	s.mu.Lock()
	defer s.mu.Unlock()
	g := s.t.Cell(x, y)
	return Cell{Rune: g.Char, FG: uint32(g.FG), BG: uint32(g.BG), Attr: g.Mode}
}

// Cursor reports the cursor position and visibility.
func (s *Screen) Cursor() (x, y int, visible bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.t.Cursor()
	return c.X, c.Y, s.t.CursorVisible()
}

// Size returns the grid dimensions.
func (s *Screen) Size() (cols, rows int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cols, s.rows
}

// Text returns the rune content of live rows [y0, y1) — the region
// adapters scan. Out-of-range bounds clamp into the grid.
func (s *Screen) Text(y0, y1 int) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if y0 < 0 {
		y0 = 0
	}
	var b strings.Builder
	for y := y0; y < y1 && y < s.rows; y++ {
		b.WriteString(s.rowTextLocked(y))
		b.WriteByte('\n')
	}
	return b.String()
}

// RowText returns one row's text, trailing blanks trimmed.
func (s *Screen) RowText(y int) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if y < 0 || y >= s.rows {
		return ""
	}
	return s.rowTextLocked(y)
}

func (s *Screen) rowTextLocked(y int) string {
	var line strings.Builder
	for x := 0; x < s.cols; x++ {
		c := s.t.Cell(x, y)
		if c.Char == 0 {
			line.WriteByte(' ')
			continue
		}
		line.WriteRune(c.Char)
	}
	return strings.TrimRight(line.String(), " ")
}

// ScrollbackLen reports retained scrollback lines (0 = oldest ... len-1
// = most recently scrolled off).
func (s *Screen) ScrollbackLen() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.t.ScrollbackLen()
}

// ScrollbackRowText returns scrollback line i (0 = oldest). Lines stored
// before a resize keep their original width and are padded/clipped here.
func (s *Screen) ScrollbackRowText(i int) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var line strings.Builder
	for _, g := range s.t.ScrollbackRow(i) {
		if g.Char == 0 {
			line.WriteByte(' ')
			continue
		}
		line.WriteRune(g.Char)
	}
	return strings.TrimRight(line.String(), " ")
}

// DefaultScope returns §06's default scan scope: the above-cursor region
// including the cursor's own line (rows 0..cursorY — line-mode prompts
// sit on the input row) plus the bottom bottomRows rows, merged when
// the cursor sits inside the bottom band.
func (s *Screen) DefaultScope(bottomRows int) []ScanRange {
	s.mu.Lock()
	defer s.mu.Unlock()
	y := s.t.Cursor().Y + 1 // include the cursor row
	top := s.rows - bottomRows
	if top < 0 {
		top = 0
	}
	if y >= top {
		return []ScanRange{{0, s.rows}}
	}
	return []ScanRange{{0, y}, {top, s.rows}}
}

// ScopeText concatenates Text() over the given ranges in order.
func (s *Screen) ScopeText(ranges []ScanRange) string {
	var b strings.Builder
	for _, r := range ranges {
		b.WriteString(s.Text(r.Y0, r.Y1))
	}
	return b.String()
}

// TakeDirty reports-and-resets the dirty flag — the trigger gate's
// coarse "something changed" signal; region-level dirt is derived by
// diffing Text() snapshots between gate ticks.
func (s *Screen) TakeDirty() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	d := s.dirty
	s.dirty = false
	return d
}

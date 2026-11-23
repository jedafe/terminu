// Spike eval for task 2.01: does hinshun/vt10x hold the fidelity the
// screen model contract needs (§06)? Each test feeds real byte streams a
// TUI would emit and asserts cell/cursor-level state. If any of these
// break, the wazero+ghostty path becomes the plan instead.
package screen

import (
	"strings"
	"testing"
)

func feed(t *testing.T, m *Screen, s string) {
	t.Helper()
	if _, err := m.Write([]byte(s)); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func cellText(m *Screen, x, y int) string {
	c := m.Cell(x, y)
	if c.Rune == 0 {
		return " "
	}
	return string(c.Rune)
}

func TestBasicTextAndCRLF(t *testing.T) {
	m := New(80, 24, 0)
	feed(t, m, "hello\r\nworld")
	if got := m.RowText(0); got != "hello" {
		t.Fatalf("row0 = %q", got)
	}
	if got := m.RowText(1); got != "world" {
		t.Fatalf("row1 = %q", got)
	}
	x, y, _ := m.Cursor()
	if x != 5 || y != 1 {
		t.Fatalf("cursor = %d,%d want 5,1", x, y)
	}
}

func TestSGRAttrsAndColors(t *testing.T) {
	m := New(80, 24, 0)
	// bold + red fg on 'A', reset, then 256-color + truecolor
	feed(t, m, "\x1b[1;31mA\x1b[0m\x1b[38;5;196mB\x1b[0m\x1b[38;2;10;20;30mC")
	a := m.Cell(0, 0)
	if a.Rune != 'A' || a.FG == 0 {
		t.Fatalf("A cell = %+v", a)
	}
	c := m.Cell(2, 0)
	if c.Rune != 'C' {
		t.Fatalf("C cell = %+v", c)
	}
	// truecolor packs as 0xRRGGBB. Known vt10x limitation: RGB(0,0,N)
	// is indistinguishable from palette index N — documented in screen.go.
	if c.FG != 0x0a141e {
		t.Fatalf("truecolor FG = %#x want 0xa141e", c.FG)
	}
}

func TestCursorMovementAndSaveRestore(t *testing.T) {
	m := New(80, 24, 0)
	feed(t, m, "\x1b[10;5Hhere") // CUP row10 col5
	if got := cellText(m, 4, 9); got != "h" {
		t.Fatalf("cell(4,9) = %q", got)
	}
	feed(t, m, "\x1b7\x1b[1;1HTL\x1b8") // DECSC, top-left 'TL', DECRC
	x, y, _ := m.Cursor()
	if x != 8 || y != 9 {
		t.Fatalf("restored cursor = %d,%d want 8,9", x, y)
	}
	if got := cellText(m, 0, 0); got != "T" {
		t.Fatalf("cell(0,0) = %q", got)
	}
}

func TestEraseInDisplayAndLine(t *testing.T) {
	m := New(80, 24, 0)
	feed(t, m, "aaaa\r\nbbbb\r\ncccc")
	feed(t, m, "\x1b[2;1H\x1b[0J") // ED0 from row2 col0 → clears rows 2..end
	if got := m.RowText(0); got != "aaaa" {
		t.Fatalf("row0 = %q", got)
	}
	if got := m.RowText(1); got != "" {
		t.Fatalf("row1 after ED0 = %q want empty", got)
	}
	feed(t, m, "\x1b[2J") // ED2 full clear
	if got := m.RowText(0); got != "" {
		t.Fatalf("row0 after ED2 = %q", got)
	}
	// EL1: clear to start of line. Cursor is on row1 after ED2.
	feed(t, m, "abcdef\x1b[3G\x1b[1K") // cursor col3, erase left → "   def"
	if got := m.RowText(1); strings.TrimLeft(got, " ") != "def" {
		t.Fatalf("row1 after EL1 = %q", got)
	}
}

func TestAltScreenEnterExit(t *testing.T) {
	m := New(80, 24, 0)
	feed(t, m, "MAIN-SCREEN")
	feed(t, m, "\x1b[?1049h") // enter alt screen
	feed(t, m, "\x1b[1;1HALT-CONTENT")
	if got := cellText(m, 0, 0); got != "A" {
		t.Fatalf("alt cell(0,0) = %q", got)
	}
	feed(t, m, "\x1b[?1049l") // exit alt screen → restore
	if got := m.RowText(0); got != "MAIN-SCREEN" {
		t.Fatalf("row0 after alt exit = %q want MAIN-SCREEN", got)
	}
}

func TestScrollRegion(t *testing.T) {
	m := New(10, 6, 0)
	for i := 0; i < 6; i++ {
		feed(t, m, "L"+strings.Repeat(" ", 0)+"\r\n")
	}
	feed(t, m, "\x1b[2;5r")        // scroll region rows 2-5
	feed(t, m, "\x1b[5;1H\x1b[1S") // scroll up 1 inside region at row5
	feed(t, m, "\x1b[5;1Hnew")     // write into row5
	if got := cellText(m, 0, 4); got != "n" {
		t.Fatalf("row5 cell = %q want n (region scroll)", got)
	}
}

func TestWideChar(t *testing.T) {
	m := New(80, 24, 0)
	wide := string(rune(0x4F60)) // CJK double-width char
	feed(t, m, wide+"x")         // occupies cells 0-1, x lands at col 2
	if got := cellText(m, 0, 0); got != wide {
		t.Fatalf("wide cell(0,0) = %q", got)
	}
	if got := cellText(m, 2, 0); got != "x" {
		t.Fatalf("cell(2,0) = %q want x", got)
	}
	x, _, _ := m.Cursor()
	if x != 3 {
		t.Fatalf("cursor after wide+x = %d want 3", x)
	}
}

func TestInsertDeleteLines(t *testing.T) {
	m := New(10, 5, 0)
	feed(t, m, "r1\r\nr2\r\nr3")
	feed(t, m, "\x1b[2;1H\x1b[1M") // DL1 at row2 → r3 shifts up
	if got := m.RowText(1); got != "r3" {
		t.Fatalf("row1 after DL = %q want r3", got)
	}
}

func TestResize(t *testing.T) {
	m := New(80, 24, 0)
	feed(t, m, "persistent")
	m.Resize(40, 10)
	c, r := m.Size()
	if c != 40 || r != 10 {
		t.Fatalf("size = %dx%d", c, r)
	}
	if got := m.RowText(0); got != "persistent" {
		t.Fatalf("row0 after shrink = %q", got)
	}
}

func TestDirtyFlag(t *testing.T) {
	m := New(80, 24, 0)
	if m.TakeDirty() {
		t.Fatal("clean model reports dirty")
	}
	feed(t, m, "x")
	if !m.TakeDirty() {
		t.Fatal("write did not mark dirty")
	}
	if m.TakeDirty() {
		t.Fatal("dirty not reset after take")
	}
}

func TestScrollback(t *testing.T) {
	m := New(10, 4, 100)
	for i := 0; i < 8; i++ {
		feed(t, m, "line"+string(rune('0'+i))+"\r\n")
	}
	if got := m.ScrollbackLen(); got < 4 {
		t.Fatalf("scrollback len = %d, want >=4", got)
	}
	// The earliest lines scrolled off; newest scrollback entry should
	// hold the most recently evicted row, live rows stay on screen.
	last := m.ScrollbackRowText(m.ScrollbackLen() - 1)
	if !strings.HasPrefix(last, "line") {
		t.Fatalf("last scrollback row = %q", last)
	}
	// Alt screen must not pollute scrollback.
	before := m.ScrollbackLen()
	feed(t, m, "\x1b[?1049h")
	for i := 0; i < 6; i++ {
		feed(t, m, "alt\r\n")
	}
	feed(t, m, "\x1b[?1049l")
	if got := m.ScrollbackLen(); got != before {
		t.Fatalf("alt screen pushed scrollback: %d → %d", before, got)
	}
}

func TestScrollbackBounded(t *testing.T) {
	m := New(10, 4, 5)
	for i := 0; i < 20; i++ {
		feed(t, m, "row\r\n")
	}
	if got := m.ScrollbackLen(); got != 5 {
		t.Fatalf("scrollback len = %d, want capped at 5", got)
	}
}

func TestDefaultScope(t *testing.T) {
	m := New(80, 24, 0)
	// Cursor near top → above-cursor (incl. cursor row) + bottom band.
	feed(t, m, "\x1b[5;1H")
	sc := m.DefaultScope(12)
	if len(sc) != 2 || sc[0] != (ScanRange{0, 5}) || sc[1] != (ScanRange{12, 24}) {
		t.Fatalf("scope = %+v", sc)
	}
	// Cursor inside bottom band → whole screen.
	feed(t, m, "\x1b[20;1H")
	sc = m.DefaultScope(12)
	if len(sc) != 1 || sc[0] != (ScanRange{0, 24}) {
		t.Fatalf("merged scope = %+v", sc)
	}
	// ScopeText concatenates in order.
	feed(t, m, "\x1b[1;1Htop\x1b[24;1Hbot")
	feed(t, m, "\x1b[5;1H")
	got := m.ScopeText(m.DefaultScope(12))
	if !strings.Contains(got, "top") || !strings.Contains(got, "bot") {
		t.Fatalf("scope text missing regions:\n%s", got)
	}
}

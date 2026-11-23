package adapter

import (
	"testing"

	"github.com/jedafe/terminu/internal/screen"
)

func TestReplayCast(t *testing.T) {
	s := screen.New(10, 5, 0) // resized by header to 80x24
	if err := ReplayCast("testdata/confirm.cast", s); err != nil {
		t.Fatalf("replay: %v", err)
	}
	c, r := s.Size()
	if c != 80 || r != 24 {
		t.Fatalf("size = %dx%d, want 80x24 from cast header", c, r)
	}
	if got := s.RowText(0); got != "agent starting..." {
		t.Fatalf("row0 = %q", got)
	}
	if got := s.RowText(1); got != "Apply changes to config.yaml?" {
		t.Fatalf("row1 = %q", got)
	}
	// Bold SGR landed on the [y/N] token — output events parsed attrs.
	cell := s.Cell(1, 2) // 'y' inside [y/N]
	if cell.Rune != 'y' || cell.Attr == 0 {
		t.Fatalf("prompt cell = %+v, want 'y' with attr", cell)
	}
	// The "i" input event was skipped — the echoed 'y' lands after the
	// "[y/N] " prompt on row 2 (it came from an "o" event).
	if got := s.RowText(2); got != "[y/N] y" {
		t.Fatalf("row2 = %q want \"[y/N] y\"", got)
	}
}

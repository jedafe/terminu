//go:build linux

package session

import (
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"

	"github.com/jedafe/terminu/internal/domain"
	"github.com/jedafe/terminu/internal/gate"
	"github.com/jedafe/terminu/internal/screen"
)

// The §06 feed: PTY bytes reach the screen model and the trigger gate
// fires — proving the wiring before any adapter exists to consume it.
func TestDetectionFeed(t *testing.T) {
	cmd := exec.Command("bash", "-c", "printf 'confirm? [y/N]'; sleep 2")
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: 80, Rows: 24})
	if err != nil {
		t.Fatalf("pty start: %v", err)
	}
	s := &ptySession{
		rec:   &domain.Session{ID: "t", Cols: 80, Rows: 24},
		cmd:   cmd,
		ptmx:  ptmx,
		buf:   newRing(1 << 20),
		conns: map[*Client]struct{}{},
		done:  make(chan struct{}),
		exit:  make(chan int, 1),
		scr:   screen.New(80, 24, 0),
		gt:    gate.New(60*time.Millisecond, 12),
	}
	go s.readLoop()
	go s.gt.Run(s.done, s)
	defer func() { s.close(); _ = cmd.Process.Kill() }()

	select {
	case e := <-s.gt.Events:
		switch e.Reason {
		case gate.ReasonBottomDirty, gate.ReasonCursorToInput,
			gate.ReasonQuiescence, gate.ReasonProcessChange:
		default:
			t.Fatalf("unexpected reason %v", e.Reason)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no gate event within 3s")
	}

	// Screen model consumed the same bytes the clients did.
	for i := 0; i < 50; i++ {
		if strings.Contains(s.scr.Text(0, 24), "confirm?") {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("screen model never saw the output")
}

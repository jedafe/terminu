//go:build linux

package session

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"

	"github.com/jedafe/terminu/internal/config"
	"github.com/jedafe/terminu/internal/db"
	"github.com/jedafe/terminu/internal/db/gen"
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

// Full pipeline: real PTY → gate quiescence → fg binder → generic G1 →
// waiting transition → session_state_log + agent.state on the bus.
func TestRegistryPipeline(t *testing.T) {
	ctx := context.Background()
	d, err := db.Open(ctx, filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	defer func() { _ = d.Close() }()
	bus := NewEventBus()
	m := NewManager(&config.Config{
		LocalTargetMode:  "container",
		SessionBufferKB:  64,
		QuiescenceMS:     80,
		ScanBottomRows:   12,
		ScreenScrollback: 0,
		MaxSessions:      5,
	}, gen.New(d), bus)
	sub := bus.Sub()
	defer bus.Unsub(sub)

	rec, err := m.Spawn(ctx, domain.SpawnRequest{
		Target: "local", Shell: "/bin/bash", Name: "t", Cols: 80, Rows: 24,
	})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	defer func() { _ = m.Kill(ctx, rec.ID) }()

	s, err := m.Attach(rec.ID)
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	// `read` blocks after printf, so the y/n token stays trailing on the
	// bottom line — quiescence fires while bash waits inside `read`.
	if err := s.Write([]byte("printf 'ok? [y/N] '; read x\n")); err != nil {
		t.Fatalf("write: %v", err)
	}

	deadline := time.After(5 * time.Second)
	for {
		select {
		case e := <-sub:
			if e.Type != "agent.state" || e.SessionID != rec.ID {
				continue
			}
			if e.State != domain.StateWaitingForInput {
				t.Fatalf("state = %s, want waiting-for-input", e.State)
			}
			if e.Kind != domain.WaitConfirm || e.Confidence != domain.ConfidenceMedium {
				t.Fatalf("kind=%s conf=%s", e.Kind, e.Confidence)
			}
			if e.Process != "bash" {
				t.Fatalf("process = %q want bash", e.Process)
			}
			return
		case <-deadline:
			t.Fatal("no agent.state within 5s")
		}
	}
}

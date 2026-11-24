//go:build linux

package session

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/jedafe/terminu/internal/config"
	"github.com/jedafe/terminu/internal/db"
	"github.com/jedafe/terminu/internal/db/gen"
	"github.com/jedafe/terminu/internal/domain"
)

// §05: the `alert` frame is the live edge — the alerts table is its
// history. Process exit is a catalog transition: the row must land.
func TestAlertPersistedOnExit(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	d, err := db.Open(ctx, filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	defer func() { _ = d.Close() }()
	q := gen.New(d)
	mgr := NewManager(&config.Config{
		DataDir: dir, LocalTargetMode: "container",
		SessionBufferKB: 64, QuiescenceMS: 80, ScanBottomRows: 12, MaxSessions: 5,
	}, q, NewEventBus())

	ok, err := mgr.Spawn(ctx, domain.SpawnRequest{Target: "local", Shell: "/bin/ls"})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	bad, err := mgr.Spawn(ctx, domain.SpawnRequest{Target: "local", Shell: "/bin/false"})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	killed, err := mgr.Spawn(ctx, domain.SpawnRequest{Target: "local", Shell: "/bin/sh"})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	if err := mgr.Kill(ctx, killed.ID); err != nil {
		t.Fatalf("kill: %v", err)
	}

	var rows []gen.Alert
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if rows, err = q.ListAlerts(ctx, 50); err != nil {
			t.Fatalf("list: %v", err)
		}
		if len(rows) >= 2 {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}

	bySess := map[string]gen.Alert{}
	for _, a := range rows {
		bySess[a.SessionID] = a
	}
	// *→done is "info" — nobody is needed; *→error is "warn".
	if a, ok := bySess[ok.ID]; !ok || a.Level != "info" {
		t.Fatalf("done alert = %+v (want level info)", a)
	}
	if a, ok := bySess[bad.ID]; !ok || a.Level != "warn" {
		t.Fatalf("error alert = %+v (want level warn)", a)
	}
	// A deliberate terminate ends `ended` — not an alert transition.
	if a, ok := bySess[killed.ID]; ok {
		t.Fatalf("killed session should not alert: %+v", a)
	}
}

func TestAlertLevelAndTransition(t *testing.T) {
	if alertLevel(domain.StateError) != "warn" ||
		alertLevel(domain.StateWaitingForInput) != "action" ||
		alertLevel(domain.StateDone) != "info" {
		t.Fatal("alertLevel mapping wrong")
	}
	if got := transition(domain.StateWorking, domain.StateWaitingForInput); got != "working→waiting-for-input" {
		t.Fatalf("transition = %q", got)
	}
	if got := transition("", domain.StateDone); got != "done" {
		t.Fatalf("transition = %q", got)
	}
}

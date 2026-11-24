//go:build linux

package session

import (
	"testing"
	"time"

	"github.com/jedafe/terminu/internal/domain"
)

// §05: alerts persist transition edges — a wait episode yields exactly
// one row until the session holds a non-waiting state long enough to
// re-arm (TU_ALERT_REARM_S). Redraw flaps must not respawn rows.
func TestAlertEdge(t *testing.T) {
	const rearm = 5 * time.Second
	b := &binding{}
	t0 := time.Now()

	// First wait edge emits and latches the episode.
	if !b.alertEdge(domain.StateWorking, domain.StateWaitingForInput, t0, rearm) {
		t.Fatal("first waiting edge must alert")
	}
	// Repeat detection of the same wait: not a transition, no row.
	if b.alertEdge(domain.StateWaitingForInput, domain.StateWaitingForInput, t0.Add(100*time.Millisecond), rearm) {
		t.Fatal("same-state repeat must not alert")
	}
	// Flap: a sub-rearm working blip does not re-arm.
	b.alertEdge(domain.StateWaitingForInput, domain.StateWorking, t0.Add(200*time.Millisecond), rearm)
	if b.alertEdge(domain.StateWorking, domain.StateWaitingForInput, t0.Add(400*time.Millisecond), rearm) {
		t.Fatal("flapped wait inside rearm window must not alert")
	}
	// More flapping — still one episode.
	b.alertEdge(domain.StateWaitingForInput, domain.StateWorking, t0.Add(600*time.Millisecond), rearm)
	if b.alertEdge(domain.StateWorking, domain.StateWaitingForInput, t0.Add(800*time.Millisecond), rearm) {
		t.Fatal("repeated flap must not alert")
	}
	// Sustained non-waiting past the dwell re-arms: a genuinely new wait
	// after the session worked for a while is a real transition again.
	b.alertEdge(domain.StateWaitingForInput, domain.StateWorking, t0.Add(900*time.Millisecond), rearm)
	b.alertEdge(domain.StateWorking, domain.StateWorking, t0.Add(9*time.Second), rearm)
	if !b.alertEdge(domain.StateWorking, domain.StateWaitingForInput, t0.Add(10*time.Second), rearm) {
		t.Fatal("wait after sustained working must alert again")
	}
}

func TestAlertEdgeDoneError(t *testing.T) {
	b := &binding{}
	t0 := time.Now()
	if !b.alertEdge(domain.StateWorking, domain.StateDone, t0, time.Second) {
		t.Fatal("done edge must alert")
	}
	if b.alertEdge(domain.StateDone, domain.StateDone, t0, time.Second) {
		t.Fatal("same-state repeat must not alert")
	}
	// A real edge to error is a transition — spec alerts on *→error.
	if !b.alertEdge(domain.StateWorking, domain.StateError, t0, time.Second) {
		t.Fatal("error edge must alert")
	}
}

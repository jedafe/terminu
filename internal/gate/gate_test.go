package gate

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/jedafe/terminu/internal/proc"
)

type fakeSrc struct {
	bottom  atomic.Value // string
	cursorY atomic.Int32
	rows    int
	fg      proc.Info
	fgErr   error
	fgCalls atomic.Int32
}

func newFakeSrc() *fakeSrc {
	s := &fakeSrc{rows: 24, fg: proc.Info{PID: 100, PGID: 100, Comm: "bash", Argv: []string{"bash"}}}
	s.bottom.Store("")
	s.cursorY.Store(0)
	return s
}

func (f *fakeSrc) BottomText(rows int) string { return f.bottom.Load().(string) }
func (f *fakeSrc) Cursor() (x, y int, v bool) { return 0, int(f.cursorY.Load()), true }
func (f *fakeSrc) Rows() int                  { return f.rows }
func (f *fakeSrc) Foreground() (proc.Info, error) {
	f.fgCalls.Add(1)
	return f.fg, f.fgErr
}

func collect(t *testing.T, g *Gate, d time.Duration) []Event {
	t.Helper()
	deadline := time.After(d)
	var out []Event
	for {
		select {
		case e := <-g.Events:
			out = append(out, e)
		case <-deadline:
			return out
		}
	}
}

func reasons(ev []Event) map[Reason]int {
	m := map[Reason]int{}
	for _, e := range ev {
		m[e.Reason]++
	}
	return m
}

func TestIdleCostsZero(t *testing.T) {
	src := newFakeSrc()
	g := New(80*time.Millisecond, 12) // tick = 25ms
	stop := make(chan struct{})
	defer close(stop)
	go g.Run(stop, src)

	ev := collect(t, g, 300*time.Millisecond)
	if len(ev) != 0 {
		t.Fatalf("idle emitted %v", reasons(ev))
	}
	if n := src.fgCalls.Load(); n != 0 {
		t.Fatalf("idle did %d fg reads, want 0", n)
	}
}

func TestQuiescenceFires(t *testing.T) {
	src := newFakeSrc()
	g := New(80*time.Millisecond, 12)
	stop := make(chan struct{})
	defer close(stop)
	go g.Run(stop, src)

	g.NotifyFrame()
	ev := collect(t, g, 400*time.Millisecond)
	r := reasons(ev)
	if r[ReasonQuiescence] != 1 {
		t.Fatalf("reasons = %v, want exactly 1 quiescence", r)
	}
	// Event carries the fg binding key.
	for _, e := range ev {
		if e.Reason == ReasonQuiescence && e.FG.Comm != "bash" {
			t.Fatalf("quiescence fg = %+v", e.FG)
		}
	}
}

func TestBottomDirtyEdge(t *testing.T) {
	src := newFakeSrc()
	g := New(80*time.Millisecond, 12)
	stop := make(chan struct{})
	defer close(stop)
	go g.Run(stop, src)

	src.bottom.Store("Are you sure? [y/N]")
	g.NotifyFrame()
	// More output in the same burst — dedup must not re-fire dirty.
	g.NotifyFrame()
	g.NotifyFrame()
	ev := collect(t, g, 300*time.Millisecond)
	r := reasons(ev)
	if r[ReasonBottomDirty] != 1 {
		t.Fatalf("reasons = %v, want 1 bottom_dirty", r)
	}
}

func TestDirtyDedupAcrossBursts(t *testing.T) {
	src := newFakeSrc()
	g := New(60*time.Millisecond, 12)
	stop := make(chan struct{})
	defer close(stop)
	go g.Run(stop, src)

	src.bottom.Store("prompt A")
	g.NotifyFrame()
	_ = collect(t, g, 250*time.Millisecond) // burst 1 ends (quiescence)

	// Burst 2 with SAME bottom → no dirty edge (nothing changed).
	src.fg = proc.Info{PID: 100, PGID: 100, Comm: "bash"}
	g.NotifyFrame()
	ev := collect(t, g, 250*time.Millisecond)
	if reasons(ev)[ReasonBottomDirty] != 0 {
		t.Fatalf("unchanged bottom fired dirty: %v", reasons(ev))
	}

	// Burst 3, bottom changed → dirty fires again.
	src.bottom.Store("prompt B")
	g.NotifyFrame()
	ev = collect(t, g, 250*time.Millisecond)
	if reasons(ev)[ReasonBottomDirty] != 1 {
		t.Fatalf("changed bottom did not fire: %v", reasons(ev))
	}
}

func TestCursorToInputEdge(t *testing.T) {
	src := newFakeSrc()
	src.cursorY.Store(23) // bottom row of 24
	g := New(80*time.Millisecond, 12)
	stop := make(chan struct{})
	defer close(stop)
	go g.Run(stop, src)

	g.NotifyFrame()
	ev := collect(t, g, 300*time.Millisecond)
	if reasons(ev)[ReasonCursorToInput] != 1 {
		t.Fatalf("reasons = %v, want 1 cursor_to_input", reasons(ev))
	}
}

func TestProcessChangeEdge(t *testing.T) {
	src := newFakeSrc()
	g := New(60*time.Millisecond, 12)
	stop := make(chan struct{})
	defer close(stop)
	go g.Run(stop, src)

	g.NotifyFrame() // binds fg=bash
	_ = collect(t, g, 200*time.Millisecond)

	src.fg = proc.Info{PID: 200, PGID: 200, Comm: "devin", Argv: []string{"devin"}}
	g.NotifyFrame()
	ev := collect(t, g, 250*time.Millisecond)
	r := reasons(ev)
	if r[ReasonProcessChange] != 1 {
		t.Fatalf("reasons = %v, want 1 process_change", r)
	}
	for _, e := range ev {
		if e.Reason == ReasonProcessChange && e.FG.Comm != "devin" {
			t.Fatalf("process_change fg = %+v want devin", e.FG)
		}
	}
}

func TestOnDemand(t *testing.T) {
	src := newFakeSrc()
	g := New(80*time.Millisecond, 12)
	stop := make(chan struct{})
	defer close(stop)
	go g.Run(stop, src)

	g.Request()
	g.Request() // second Request coalesces (requests chan cap 1)
	ev := collect(t, g, 150*time.Millisecond)
	r := reasons(ev)
	if r[ReasonOnDemand] < 1 {
		t.Fatalf("reasons = %v, want on_demand", r)
	}
}

func TestEventCoalescing(t *testing.T) {
	src := newFakeSrc()
	g := New(60*time.Millisecond, 12)
	// No Run goroutine → Events buffer(1) fills, extra emits drop.
	src.bottom.Store("x")
	g.NotifyFrame()
	g.emit(Event{Reason: ReasonBottomDirty})
	g.emit(Event{Reason: ReasonQuiescence})
	if len(g.Events) != 1 {
		t.Fatalf("events = %d, want 1 (coalesced)", len(g.Events))
	}
}

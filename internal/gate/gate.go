// Ref: .teos/sections/06-screen-interpreter-adapters.md (Trigger gate)
// The trigger gate is the ONLY authorized detection time — adapters run
// when it emits, never per frame. Cost model: NotifyFrame is O(1); tick
// work (fg procfs read, bottom-band hash, cursor read) happens solely on
// ticks where frames arrived; a quiet session does literally nothing.
//
// Dedup: immediate edges (bottom-dirty, cursor-to-input) fire at most
// once per activity burst — during a burst of output, the burst's LAST
// state is what Detect() reads anyway, so coalescing loses nothing.
// Quiescence is the catch-all that always re-arms the next burst.
package gate

import (
	"hash/fnv"
	"sync/atomic"
	"time"

	"github.com/jedafe/terminu/internal/proc"
)

// Reason enumerates §06's authorized scan triggers.
type Reason int

const (
	// ReasonQuiescence — output silent for ≥ quiescence after activity.
	ReasonQuiescence Reason = iota
	// ReasonBottomDirty — bottom band changed during an activity burst.
	ReasonBottomDirty
	// ReasonCursorToInput — cursor reached the bottom line after activity.
	ReasonCursorToInput
	// ReasonOnDemand — explicit Request() (a view opened).
	ReasonOnDemand
	// ReasonProcessChange — foreground process changed between ticks.
	ReasonProcessChange
)

func (r Reason) String() string {
	switch r {
	case ReasonQuiescence:
		return "quiescence"
	case ReasonBottomDirty:
		return "bottom_dirty"
	case ReasonCursorToInput:
		return "cursor_to_input"
	case ReasonOnDemand:
		return "on_demand"
	case ReasonProcessChange:
		return "process_change"
	}
	return "unknown"
}

// Event tells the adapter runner to scan: which trigger fired and what
// process owned the foreground at emit time (the binding key).
type Event struct {
	Reason Reason
	FG     proc.Info
}

// Source is the session-side surface the gate reads — screen bottom
// band, cursor, and the foreground-process binder.
type Source interface {
	// BottomText returns text of the bottom `rows` live rows.
	BottomText(rows int) string
	// Cursor returns the cursor's row position and visibility.
	Cursor() (x, y int, visible bool)
	// Rows returns the live grid height.
	Rows() int
	// Foreground resolves the current foreground process (proc.Info).
	Foreground() (proc.Info, error)
}

// Gate watches one session and emits scan events at authorized times.
type Gate struct {
	quiescence time.Duration
	tick       time.Duration
	bottomRows int

	frames   atomic.Uint64
	requests chan struct{}

	// Events coalesces: buffer of 1 means "a scan is already queued" —
	// extra reasons dropped lose nothing, Detect reads current state.
	Events chan Event

	// owned by the Run goroutine
	lastSeen   uint64
	lastBottom uint64
	haveBottom bool
	lastFG     proc.Info
	haveFG     bool
	deadline   time.Time
	firing     bool
	firedDirty bool
	firedInput bool
}

// New builds a gate: quiescence is the TU_QUIESCENCE_MS silence window;
// bottomRows is TU_SCAN_BOTTOM_ROWS. The tick derives from quiescence
// (quarter window, clamped) — a knob nobody needs to tune separately.
func New(quiescence time.Duration, bottomRows int) *Gate {
	tick := quiescence / 4
	if tick < 25*time.Millisecond {
		tick = 25 * time.Millisecond
	}
	if tick > 250*time.Millisecond {
		tick = 250 * time.Millisecond
	}
	return &Gate{
		quiescence: quiescence,
		tick:       tick,
		bottomRows: bottomRows,
		requests:   make(chan struct{}, 1),
		Events:     make(chan Event, 1),
	}
}

// NotifyFrame marks PTY output arrival — O(1), called from the session
// read loop once per frame regardless of size.
func (g *Gate) NotifyFrame() { g.frames.Add(1) }

// Request fires an on-demand scan (a view opened on this session).
func (g *Gate) Request() {
	select {
	case g.requests <- struct{}{}:
	default:
	}
}

// Run is the gate loop — one goroutine per session, stopped by stop
// (the session's done channel). All Source reads happen here.
func (g *Gate) Run(stop <-chan struct{}, src Source) {
	tk := time.NewTicker(g.tick)
	defer tk.Stop()
	for {
		select {
		case <-stop:
			return
		case <-g.requests:
			g.emit(Event{Reason: ReasonOnDemand, FG: g.fg(src)})
		case now := <-tk.C:
			g.tickOnce(now, src)
		}
	}
}

func (g *Gate) tickOnce(now time.Time, src Source) {
	seen := g.frames.Load()
	if seen == g.lastSeen {
		// No output this tick — the only work is the quiescence check.
		if g.firing && !now.Before(g.deadline) {
			g.emit(Event{Reason: ReasonQuiescence, FG: g.fg(src)})
			g.firing = false
			g.firedDirty = false
			g.firedInput = false
		}
		return
	}
	g.lastSeen = seen
	g.firing = true
	g.deadline = now.Add(g.quiescence)

	fg := g.fg(src)
	if g.haveFG && (fg.PID != g.lastFG.PID || fg.Name() != g.lastFG.Name()) {
		g.emit(Event{Reason: ReasonProcessChange, FG: fg})
	}
	g.lastFG, g.haveFG = fg, true

	if !g.firedDirty {
		h := hashStr(src.BottomText(g.bottomRows))
		if !g.haveBottom || h != g.lastBottom {
			g.lastBottom, g.haveBottom = h, true
			g.emit(Event{Reason: ReasonBottomDirty, FG: fg})
			g.firedDirty = true
		}
	}
	if !g.firedInput {
		if _, y, _ := src.Cursor(); y >= src.Rows()-1 {
			g.emit(Event{Reason: ReasonCursorToInput, FG: fg})
			g.firedInput = true
		}
	}
}

func (g *Gate) fg(src Source) proc.Info {
	info, err := src.Foreground()
	if err != nil {
		return proc.Info{}
	}
	return info
}

func (g *Gate) emit(e Event) {
	select {
	case g.Events <- e:
	default:
	}
}

func hashStr(s string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(s))
	return h.Sum64()
}

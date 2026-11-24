// Ref: .teos/sections/03-session-management.md, §05, §06 —
// session-state registry. Consumes trigger-gate events per session,
// binds the adapter by foreground process, runs Detect only on gate
// emits, and writes transitions (sessions.state + session_state_log +
// agent.state broadcast). Detection never happens off-gate.
package session

import (
	"context"
	"sync"
	"time"

	"github.com/jedafe/terminu/internal/adapter"
	"github.com/jedafe/terminu/internal/domain"
	"github.com/jedafe/terminu/internal/gate"
	"github.com/jedafe/terminu/internal/proc"
)

// registry tracks each live session's detected state — the "waiting-*"
// side of the state machine the process side (starting/done/error) runs
// through onExit.
type registry struct {
	m        *Manager
	adapters []adapter.Adapter
	mu       sync.Mutex
	bound    map[string]*binding
}

// binding is one session's adapter slot + last emitted match. Mutated
// only by the session's consume goroutine; Bound() reads under b.mu.
type binding struct {
	mu      sync.Mutex
	ad      adapter.Adapter // nil → unadapted (generic tiers run)
	fg      proc.Info
	lastKey string      // dedup key of last emitted match
	lastUp  StateUpdate // last emitted transition — session sockets resync from it
}

func newRegistry(m *Manager) *registry {
	return &registry{
		m:        m,
		adapters: []adapter.Adapter{adapter.Claude(), adapter.Codex(), adapter.Gemini(), adapter.Devin(), adapter.Aider()},
		bound:    map[string]*binding{},
	}
}

// Register adds an adapter — later adapters get first-match priority
// order as appended (specific before generic is the caller's problem).
func (r *registry) register(a adapter.Adapter) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.adapters = append(r.adapters, a)
}

// watch starts the gate-event consumer for one session.
func (r *registry) watch(s *ptySession) {
	go r.consume(s)
}

func (r *registry) unwatch(id string) {
	r.mu.Lock()
	delete(r.bound, id)
	r.mu.Unlock()
}

// consume runs Detect on each authorized gate event — this loop is the
// only place adapter.Detect is invoked in production. The gate's Events
// channel never closes, so the session's done is the exit signal.
func (r *registry) consume(s *ptySession) {
	ctx := &adapter.SessionCtx{SessionID: s.rec.ID, ScanBottomRows: r.m.cfg.ScanBottomRows}
	events := s.GateEvents()
	for {
		select {
		case <-s.done:
			return
		case e := <-events:
			r.handle(s, e, ctx)
		}
	}
}

// handle resolves fg → binds adapter → detects → writes transition.
func (r *registry) handle(s *ptySession, e gate.Event, ctx *adapter.SessionCtx) {
	fg := e.FG
	if fg.PID == 0 {
		if f, err := s.Foreground(); err == nil {
			fg = f
		}
	}
	b := r.bindingFor(s)
	if b == nil {
		return // session already dead — nothing to write or emit
	}
	ctx.FG = fg
	ctx.Trigger = e.Reason.String()

	b.mu.Lock()
	if fg.PID != 0 && !fg.Same(b.fg) {
		b.fg = fg
		b.ad = r.match(fg) // process change → rebind adapter slot
	}
	ad := b.ad
	b.mu.Unlock()

	var m *adapter.StateMatch
	if ad != nil {
		m = ad.Detect(s.scr, ctx)
	} else {
		m = adapter.DetectGeneric(s.scr, ctx)
	}

	// nil match on an alive process = working (busy, no prompt seen).
	st := domain.StateWorking
	var kind domain.WaitKind
	var confidence domain.Confidence
	var evidence string
	if m != nil {
		st = m.State
		kind = m.Kind
		confidence = m.Confidence
		evidence = m.Evidence
	}
	key := string(st) + "|" + string(kind) + "|" + string(confidence) + "|" + evidence
	b.mu.Lock()
	if key == b.lastKey {
		b.mu.Unlock()
		return // nothing changed — no write, no emit
	}
	prev := b.lastUp.State
	b.lastKey = key
	b.lastUp = StateUpdate{State: st, Match: m, At: time.Now()}
	b.mu.Unlock()

	_ = r.m.setState(context.Background(), s.rec.ID, st, string(kind), string(confidence), evidence)
	s.setRecState(st)
	s.notifyState(b.lastUp)

	// Catalog transition rule: notify on *detected* states — agent.state
	// feeds the needs-you view (kind only rides waiting-for-input).
	// `alert` is the same transitions with the edge that fired them.
	if st == domain.StateWaitingForInput || st == domain.StateDone || st == domain.StateError {
		r.m.events.Pub(Event{
			Type:       "agent.state",
			SessionID:  s.rec.ID,
			Process:    fg.Name(),
			State:      st,
			Kind:       kind,
			Confidence: confidence,
		})
		r.m.alert(s.rec.ID, fg.Name(), prev, st, kind, confidence)
	}
}

// alertLevel grades the catalog `alert` frame — waits need a human,
// errors need attention, done is informational.
func alertLevel(st domain.SessionState) string {
	switch st {
	case domain.StateError:
		return "warn"
	case domain.StateWaitingForInput:
		return "action"
	default:
		return "info"
	}
}

// transition renders "prev→next" for the alert frame; the first
// transition of a session has no prior emitted state.
func transition(prev, st domain.SessionState) string {
	if prev == "" || prev == st {
		return string(st)
	}
	return string(prev) + "→" + string(st)
}

func (r *registry) match(fg proc.Info) adapter.Adapter {
	name := fg.Name()
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, a := range r.adapters {
		if a.MatchesProcess(name, fg.Argv) {
			return a
		}
	}
	return nil
}

// bindingFor returns the session's binding, creating it on first use.
// nil when the session is already dead — a handle() racing unwatch()
// must not resurrect a deleted map entry (that entry would never be
// removed: the consume goroutine is already gone).
func (r *registry) bindingFor(s *ptySession) *binding {
	select {
	case <-s.done:
		return nil
	default:
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	b, ok := r.bound[s.rec.ID]
	if !ok {
		b = &binding{}
		r.bound[s.rec.ID] = b
	}
	return b
}

// Bound reports the adapter bound to a session, if any (for status UI).
func (r *registry) Bound(id string) (tool string, adapted bool) {
	r.mu.Lock()
	b, ok := r.bound[id]
	r.mu.Unlock()
	if !ok {
		return "", false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.ad != nil {
		return b.ad.Pin().Tool, true
	}
	return "", false
}

// fgName is the last foreground process bound — read before unwatch on
// exit so agent.state still carries the process name.
func (r *registry) fgName(id string) string {
	r.mu.Lock()
	b, ok := r.bound[id]
	r.mu.Unlock()
	if !ok {
		return ""
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.fg.Name()
}

// lastState is the last emitted detected state — read before unwatch so
// the exit alert can name the transition it ended.
func (r *registry) lastState(id string) domain.SessionState {
	r.mu.Lock()
	b, ok := r.bound[id]
	r.mu.Unlock()
	if !ok {
		return ""
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.lastUp.State
}

// current returns the last emitted transition — session sockets resync
// from it so a client attaching after detection still sees the prompt.
func (r *registry) current(id string) StateUpdate {
	r.mu.Lock()
	b, ok := r.bound[id]
	r.mu.Unlock()
	if !ok {
		return StateUpdate{}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.lastUp
}

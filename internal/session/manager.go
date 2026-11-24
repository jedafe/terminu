// Ref: .teos/sections/03-session-management.md
// Registry: live PTY map + SQLite record. Restarts mark rows ended —
// records stay honest rather than pretending processes survive.
package session

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/jedafe/terminu/internal/adapter"
	"github.com/jedafe/terminu/internal/config"
	"github.com/jedafe/terminu/internal/db/gen"
	"github.com/jedafe/terminu/internal/domain"
	"github.com/jedafe/terminu/internal/proc"
)

// Manager is the session registry — the only PTY spawn point, reached
// exclusively through the broker.
type Manager struct {
	cfg  *config.Config
	q    *gen.Queries
	mu   sync.Mutex
	live map[string]*ptySession

	// events fans session lifecycle out to /ws/events subscribers.
	events *EventBus

	// reg is the session-state registry — gate events → Detect →
	// waiting states (§06 pipeline consumer).
	reg *registry
}

var ErrNotFound = errors.New("session not found")
var ErrLimit = errors.New("session limit reached")

func NewManager(cfg *config.Config, q *gen.Queries, bus *EventBus) *Manager {
	m := &Manager{cfg: cfg, q: q, live: map[string]*ptySession{}, events: bus}
	m.reg = newRegistry(m)
	return m
}

// Recover marks rows that were live before restart as ended.
func (m *Manager) Recover(ctx context.Context) error {
	return m.q.MarkLiveSessionsEnded(ctx)
}

// Spawn creates the DB row then the PTY. Called only via broker.Execute.
func (m *Manager) Spawn(ctx context.Context, req domain.SpawnRequest) (*domain.Session, error) {
	n, err := m.q.CountLiveSessions(ctx)
	if err != nil {
		return nil, err
	}
	if int(n) >= m.cfg.MaxSessions {
		return nil, ErrLimit
	}
	shell := req.Shell
	if shell == "" {
		shell = m.cfg.DefaultShell
	}
	if shell == "" {
		shell = "/bin/sh"
	}
	tgt := domain.ParseTarget(req.Target)
	if tgt.Kind == "" {
		tgt.Kind = "local"
	}
	cols, rows := req.Cols, req.Rows
	if cols <= 0 {
		cols = 80
	}
	if rows <= 0 {
		rows = 24
	}
	rec := &domain.Session{
		ID:     newID(),
		Name:   req.Name,
		Target: tgt,
		Cwd:    req.Cwd,
		Shell:  shell,
		State:  domain.StateStarting,
		Cols:   cols, Rows: rows,
		CreatedAt: time.Now().UTC(),
	}
	if err := m.q.CreateSession(ctx, gen.CreateSessionParams{
		ID: rec.ID, Name: rec.Name,
		TargetKind: tgt.Kind, TargetID: tgt.ID,
		Cwd: rec.Cwd, Shell: rec.Shell,
		State: string(rec.State), Cols: int64(cols), Rows: int64(rows),
	}); err != nil {
		return nil, err
	}
	ps, err := m.spawn(ctx, rec)
	if err != nil {
		_ = m.q.UpdateSessionState(ctx, gen.UpdateSessionStateParams{
			State: string(domain.StateError), ID: rec.ID})
		rec.State = domain.StateError
		return rec, fmt.Errorf("spawn: %w", err)
	}
	m.mu.Lock()
	m.live[rec.ID] = ps
	m.mu.Unlock()
	m.reg.watch(ps) // detection consumer starts with the PTY
	rec.State = domain.StateWorking
	_ = m.setState(ctx, rec.ID, domain.StateWorking, "", "", "")
	m.events.Pub(Event{Type: "session.added", Session: rec})
	return rec, nil
}

// Attach returns the live PTY for a session id.
func (m *Manager) Attach(id string) (*ptySession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.live[id]
	if !ok {
		return nil, ErrNotFound
	}
	return s, nil
}

// ForegroundProcess resolves a live session's foreground process —
// task 2.02's binder output, consumed by the adapter slot on gate ticks.
func (m *Manager) ForegroundProcess(id string) (proc.Info, error) {
	s, err := m.Attach(id)
	if err != nil {
		return proc.Info{}, err
	}
	return s.Foreground()
}

func (m *Manager) List(ctx context.Context) ([]domain.Session, error) {
	rows, err := m.q.ListSessions(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]domain.Session, 0, len(rows))
	for _, r := range rows {
		out = append(out, rowToSession(r))
	}
	return out, nil
}

func (m *Manager) Get(ctx context.Context, id string) (*domain.Session, error) {
	r, err := m.q.GetSession(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	s := rowToSession(r)
	return &s, nil
}

func (m *Manager) Rename(ctx context.Context, id, name string) error {
	if err := m.q.UpdateSessionName(ctx, gen.UpdateSessionNameParams{Name: name, ID: id}); err != nil {
		return err
	}
	m.events.Pub(Event{Type: "session.changed", SessionID: id})
	return nil
}

// Kill terminates the process (broker action, audited by caller).
func (m *Manager) Kill(ctx context.Context, id string) error {
	m.mu.Lock()
	s, ok := m.live[id]
	m.mu.Unlock()
	if ok {
		_ = s.cmd.Process.Kill()
	}
	return m.q.DeleteSession(ctx, id)
}

// Scrollback pages the ring buffer — in-memory per session (no persistence;
// restarts end sessions rather than fake scrollback).
func (m *Manager) Scrollback(id string) ([]chunk, error) {
	s, err := m.Attach(id)
	if err != nil {
		return nil, err
	}
	chunks, _ := s.buf.snapshot()
	return chunks, nil
}

// onExit records process exit → done (or error on non-zero).
func (m *Manager) onExit(s *ptySession, code int) {
	m.mu.Lock()
	delete(m.live, s.rec.ID)
	m.mu.Unlock()
	fgName := m.reg.fgName(s.rec.ID)
	m.reg.unwatch(s.rec.ID)
	ctx := context.Background()
	st := domain.StateDone
	ec := sql.NullInt64{Int64: int64(code), Valid: true}
	if code != 0 {
		st = domain.StateError
	}
	_ = m.q.UpdateSessionExit(ctx, gen.UpdateSessionExitParams{
		State: string(st), ExitCode: ec, ID: s.rec.ID})
	_ = m.setState(ctx, s.rec.ID, st, "", "", "")
	s.rec.State = st
	s.rec.ExitCode = &code
	s.notifyState(StateUpdate{State: st})
	m.events.Pub(Event{Type: "session.exited", SessionID: s.rec.ID, Session: s.rec})
	// *→done|error is a catalog notify transition — agent.state carries it.
	m.events.Pub(Event{Type: "agent.state", SessionID: s.rec.ID, Process: fgName, State: st})
}

// AgentView is one row of GET /api/agents — the needs-you surface:
// session identity + bound tool + detected state + live prompt.
// Ref: .teos/sections/05-agent-session-management.md
type AgentView struct {
	Session    *domain.Session     `json:"session"`
	Process    string              `json:"process"`
	Tool       string              `json:"tool,omitempty"`
	State      domain.SessionState `json:"state"`
	Kind       domain.WaitKind     `json:"kind,omitempty"`
	Confidence domain.Confidence   `json:"confidence,omitempty"`
	Since      time.Time           `json:"since"`
	Prompt     *adapter.Prompt     `json:"prompt,omitempty"`
}

// Agents returns live sessions in needs-you order: waiting-for-input
// (highest confidence, then longest-waiting), then working states, then
// done/error most-recent-first.
func (m *Manager) Agents() []AgentView {
	m.mu.Lock()
	live := make([]*ptySession, 0, len(m.live))
	for _, s := range m.live {
		live = append(live, s)
	}
	m.mu.Unlock()

	out := make([]AgentView, 0, len(live))
	for _, s := range live {
		u := s.CurrentState()
		tool, _ := m.reg.Bound(s.rec.ID)
		v := AgentView{
			Session: s.rec, Process: m.reg.fgName(s.rec.ID),
			Tool: tool, State: u.State, Since: u.At,
		}
		if u.Match != nil {
			v.Kind, v.Confidence, v.Prompt = u.Match.Kind, u.Match.Confidence, u.Match.Prompt
		}
		out = append(out, v)
	}

	rank := func(st domain.SessionState) int {
		switch st {
		case domain.StateWaitingForInput:
			return 0
		case domain.StateDone, domain.StateError, domain.StateEnded:
			return 2
		default:
			return 1
		}
	}
	conf := func(c domain.Confidence) int {
		switch c {
		case domain.ConfidenceHigh:
			return 0
		case domain.ConfidenceMedium:
			return 1
		default:
			return 2
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		ri, rj := rank(out[i].State), rank(out[j].State)
		if ri != rj {
			return ri < rj
		}
		if ri == 0 {
			if ci, cj := conf(out[i].Confidence), conf(out[j].Confidence); ci != cj {
				return ci < cj
			}
			return out[i].Since.Before(out[j].Since) // oldest wait first
		}
		return out[i].Since.After(out[j].Since) // recent first
	})
	return out
}

// onDetached notes last-detach time; the process keeps running.
func (m *Manager) onDetached(id string) {
	now := time.Now().UTC().Format(time.RFC3339)
	_ = m.q.UpdateSessionDetached(context.Background(),
		gen.UpdateSessionDetachedParams{
			DetachedAt: sql.NullString{String: now, Valid: true}, ID: id})
	m.events.Pub(Event{Type: "session.detached", SessionID: id})
}

func (m *Manager) setState(ctx context.Context, id string, st domain.SessionState, kind, conf, ev string) error {
	if err := m.q.UpdateSessionState(ctx, gen.UpdateSessionStateParams{State: string(st), ID: id}); err != nil {
		return err
	}
	null := func(s string) sql.NullString {
		if s == "" {
			return sql.NullString{}
		}
		return sql.NullString{String: s, Valid: true}
	}
	return m.q.LogSessionState(ctx, gen.LogSessionStateParams{
		SessionID: id, State: string(st),
		Kind: null(kind), Confidence: null(conf), Evidence: null(ev)})
}

// CloseAll shuts every live PTY — graceful server stop.
func (m *Manager) CloseAll() {
	m.mu.Lock()
	list := make([]*ptySession, 0, len(m.live))
	for _, s := range m.live {
		list = append(list, s)
	}
	m.mu.Unlock()
	for _, s := range list {
		_ = s.cmd.Process.Kill()
		s.close()
	}
}

func rowToSession(r gen.Session) domain.Session {
	var ec *int
	if r.ExitCode.Valid {
		v := int(r.ExitCode.Int64)
		ec = &v
	}
	var wid *int64
	if r.WorkspaceID.Valid {
		wid = &r.WorkspaceID.Int64
	}
	var dAt, eAt *time.Time
	if r.DetachedAt.Valid {
		if t, err := time.Parse(time.RFC3339, r.DetachedAt.String); err == nil {
			dAt = &t
		}
	}
	if r.EndedAt.Valid {
		if t, err := time.Parse(time.RFC3339, r.EndedAt.String); err == nil {
			eAt = &t
		}
	}
	cAt, _ := time.Parse(time.RFC3339, r.CreatedAt)
	return domain.Session{
		ID: r.ID, Name: r.Name,
		Target: domain.Target{Kind: r.TargetKind, ID: r.TargetID},
		Cwd:    r.Cwd, Shell: r.Shell,
		State: domain.SessionState(r.State), ExitCode: ec,
		WorkspaceID: wid, Cols: int(r.Cols), Rows: int(r.Rows),
		CreatedAt: cAt, DetachedAt: dAt, EndedAt: eAt,
	}
}

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return "s_" + hex.EncodeToString(b)
}

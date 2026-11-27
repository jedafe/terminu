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
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/jedafe/terminu/internal/adapter"
	"github.com/jedafe/terminu/internal/config"
	"github.com/jedafe/terminu/internal/db/gen"
	"github.com/jedafe/terminu/internal/domain"
	"github.com/jedafe/terminu/internal/proc"
	"github.com/jedafe/terminu/internal/screen"
	"github.com/jedafe/terminu/internal/shellenv"
)

// Manager is the session registry — the only PTY spawn point, reached
// exclusively through the broker.
type Manager struct {
	cfg     *config.Config
	q       *gen.Queries
	mu      sync.Mutex
	spawnMu sync.Mutex // serializes the cap-check→spawn sequence
	live    map[string]*ptySession

	// events fans session lifecycle out to /ws/events subscribers.
	events *EventBus

	// reg is the session-state registry — gate events → Detect →
	// waiting states (§06 pipeline consumer).
	reg *registry

	// pins tracks adapter pin status per tool — declared constraints
	// synced at boot, seen versions probed lazily on first bind.
	pins *pinStore

	// env is the materialized managed shell layer — empty fields
	// degrade those shells to plain spawns.
	env shellenv.Env

	// slashDetect, when set, runs the server-side slash-command
	// detector for one session — installed via SetSlashDetect by the
	// server (registry + broker live there). Invoked from gate events
	// under a per-session cooldown.
	slashDetect func(sessionID, app, cwd string, lines []string)
}

var ErrNotFound = errors.New("session not found")
var ErrLimit = errors.New("session limit reached")
var ErrStillLive = errors.New("session still live — terminate before delete")

// ErrExecutable marks a spawn whose binary can't resolve locally — a
// caller-fixable problem (400), not an internal one. It wraps the binary
// name so the client can say what was missing.
var ErrExecutable = errors.New("executable not found")

// Sink-side caps every spawn route shares — the HTTP layer rejects
// oversize dims, internal callers (execute/launch) clamp here. cols×rows
// cells + scrollback are per-session memory; the winsize field is uint16.
const (
	maxPTYCols     = 512
	maxPTYRows     = 256
	maxSessionName = 128
)

func NewManager(cfg *config.Config, q *gen.Queries, bus *EventBus) *Manager {
	m := &Manager{cfg: cfg, q: q, live: map[string]*ptySession{}, events: bus}
	m.pins = newPinStore(q)
	m.reg = newRegistry(m)
	m.env = shellenv.Materialize(cfg.DataDir)
	return m
}

// Recover marks rows that were live before restart as ended.
func (m *Manager) Recover(ctx context.Context) error {
	return m.q.MarkLiveSessionsEnded(ctx)
}

// resolvesLocally reports whether spawned binaries resolve on this
// daemon's PATH — true for container mode and host mode running as the
// target user; false for sudo-into-another-user and nsenter (the binary
// lives in the remote env — LookPath here would falsely reject it).
func (m *Manager) resolvesLocally() bool {
	switch m.cfg.LocalTargetMode {
	case "nsenter":
		return false
	case "host":
		u := m.cfg.HostUser
		return u == "" || u == currentUser()
	}
	return true // container
}

// Spawn creates the DB row then the PTY. Called only via broker.Execute.
// spawnMu makes the cap check atomic with the row insert — without it two
// racing spawns both pass CountLiveSessions and overshoot MaxSessions.
func (m *Manager) Spawn(ctx context.Context, req domain.SpawnRequest) (*domain.Session, error) {
	m.spawnMu.Lock()
	defer m.spawnMu.Unlock()
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
	// Direct command spawns (manifest execute, launch profile) record the
	// executable as the row's shell — honest about what the PTY runs.
	// A Wrap request execs argv inside that login shell instead, so the
	// record's shell is the wrapper (the agent surfaces as its fg job).
	cmdArgv := wrapArgv(req.Wrap, req.Argv)
	if len(cmdArgv) > 0 {
		shell = cmdArgv[0]
	}
	envPairs, err := envOverlay(req.Env)
	if err != nil {
		return nil, err
	}
	// When the child execs on this daemon's PATH the missing-binary case
	// is knowable up front: fail before the session row exists so a bad
	// profile/argv answers 400 instead of a dead `error` row + 500.
	// Sudo/nsenter targets resolve in the remote env — skip the check.
	if m.resolvesLocally() {
		bin := shell
		if len(cmdArgv) > 0 {
			bin = cmdArgv[0]
		}
		if _, err := exec.LookPath(bin); err != nil {
			return nil, fmt.Errorf("%w: %s", ErrExecutable, bin)
		}
	}
	cols, rows := req.Cols, req.Rows
	if cols <= 0 {
		cols = 80
	}
	if rows <= 0 {
		rows = 24
	}
	// The PTY size casts to uint16 and cols×rows allocates screen cells —
	// clamp at the sink so every spawn route (HTTP bounds-checks its own
	// inputs, internal callers don't) stays inside the session budget.
	if cols > maxPTYCols {
		cols = maxPTYCols
	}
	if rows > maxPTYRows {
		rows = maxPTYRows
	}
	name := req.Name
	if r := []rune(name); len(r) > maxSessionName {
		name = string(r[:maxSessionName])
	}
	rec := &domain.Session{
		ID:     newID(),
		Name:   name,
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
	// The append-only log starts at `starting` — every state a session
	// entered is in session_state_log, not just the detected ones.
	_ = m.q.LogSessionState(ctx, gen.LogSessionStateParams{
		SessionID: rec.ID, State: string(domain.StateStarting)})
	ps, err := m.spawn(ctx, rec, cmdArgv, envPairs)
	if err != nil {
		_ = m.q.UpdateSessionState(ctx, gen.UpdateSessionStateParams{
			State: string(domain.StateError), ID: rec.ID})
		rec.State = domain.StateError
		return rec, fmt.Errorf("spawn: %w", err)
	}
	rec.State = domain.StateWorking
	_ = m.setState(ctx, rec.ID, domain.StateWorking, "", "", "")
	m.mu.Lock()
	m.live[rec.ID] = ps
	m.mu.Unlock()
	m.reg.watch(ps) // detection consumer starts with the PTY
	ps.start()      // goroutines launch only once the session is reachable
	m.events.Pub(Event{Type: "session.added", Session: ps.Rec()})
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
		out = append(out, m.rowToSession(r))
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
	s := m.rowToSession(r)
	return &s, nil
}

func (m *Manager) Rename(ctx context.Context, id, name string) error {
	if err := m.q.UpdateSessionName(ctx, gen.UpdateSessionNameParams{Name: name, ID: id}); err != nil {
		return err
	}
	m.events.Pub(Event{Type: "session.changed", SessionID: id})
	return nil
}

// Kill terminates the process — SIGTERM, a grace window for the child to
// save state, then SIGKILL (§03 lifecycle). The row stays: onExit marks
// it `ended` so its session_state_log history survives (broker action,
// audited by caller; DELETE on a live session lands here).
func (m *Manager) Kill(ctx context.Context, id string) error {
	m.mu.Lock()
	s, ok := m.live[id]
	m.mu.Unlock()
	if !ok {
		return nil
	}
	s.terminate()
	return nil
}

// Delete hard-removes a finished session row + its state log (FK
// cascade) + its persisted scrollback file. Live sessions refuse —
// terminate first (invalid_transition).
func (m *Manager) Delete(ctx context.Context, id string) error {
	m.mu.Lock()
	_, live := m.live[id]
	m.mu.Unlock()
	if live {
		return ErrStillLive
	}
	if err := m.q.DeleteSession(ctx, id); err != nil {
		return err
	}
	_ = os.Remove(m.scrollbackPath(id))
	return nil
}

// PurgeLog clears only the session's session_state_log history — the
// row + persisted scrollback stay (DELETE /{id}/log semantics).
func (m *Manager) PurgeLog(ctx context.Context, id string) error {
	return m.q.DeleteSessionLog(ctx, id)
}

// ClearFinished hard-removes every finished row (done/error/ended) +
// logs. Returns the deleted ids so callers can fan out session.removed.
func (m *Manager) ClearFinished(ctx context.Context) ([]string, error) {
	rows, err := m.q.ListSessions(ctx)
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, r := range rows {
		if IsFinished(domain.SessionState(r.State)) {
			ids = append(ids, r.ID)
		}
	}
	if len(ids) == 0 {
		return ids, nil
	}
	if _, err := m.q.DeleteFinishedSessions(ctx); err != nil {
		return nil, err
	}
	for _, id := range ids {
		_ = os.Remove(m.scrollbackPath(id))
	}
	return ids, nil
}

// IsFinished is the delete-eligible state set — the process is gone.
func IsFinished(st domain.SessionState) bool {
	return st == domain.StateDone || st == domain.StateError || st == domain.StateEnded
}

// scrollbackPath is the persisted output file for a session id —
// {data_dir}/scrollback/{id}. Written once when the PTY's read loop
// ends; removed when the row is deleted.
func (m *Manager) scrollbackPath(id string) string {
	return filepath.Join(m.cfg.DataDir, "scrollback", id)
}

// Scrollback serves retained output: live sessions read the ring;
// finished rows read the persisted file (§03 — restart- and exit-safe).
func (m *Manager) Scrollback(id string) ([]byte, error) {
	if s, err := m.Attach(id); err == nil {
		chunks, _ := s.buf.snapshot()
		var b []byte
		for _, c := range chunks {
			b = append(b, c.data...)
		}
		return b, nil
	}
	if _, err := m.Get(context.Background(), id); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(m.scrollbackPath(id))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil // finished row without persisted output — empty
	}
	return data, err
}

// DeadScrollback is the WS dead-attach replay source — finished rows
// only; returns the persisted bytes (nil if none were captured).
func (m *Manager) DeadScrollback(id string) ([]byte, error) {
	data, err := os.ReadFile(m.scrollbackPath(id))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return data, err
}

// onExit records process exit → done (or error on non-zero).
func (m *Manager) onExit(s *ptySession, code int) {
	m.mu.Lock()
	delete(m.live, s.rec.ID)
	m.mu.Unlock()
	fgName := m.reg.fgName(s.rec.ID)
	prevSt := m.reg.lastState(s.rec.ID)
	fgTool, _ := m.reg.Bound(s.rec.ID)
	m.reg.unwatch(s.rec.ID)
	ctx := context.Background()
	st := domain.StateDone
	ec := sql.NullInt64{Int64: int64(code), Valid: true}
	s.mu.Lock()
	killed := s.killed
	s.mu.Unlock()
	if killed {
		st = domain.StateEnded // deliberate terminate — not an error
	} else if code != 0 {
		st = domain.StateError
	}
	_ = m.q.UpdateSessionExit(ctx, gen.UpdateSessionExitParams{
		State: string(st), ExitCode: ec, ID: s.rec.ID})
	_ = m.setState(ctx, s.rec.ID, st, "", "", "")
	s.setRecDone(st, code)
	s.notifyState(StateUpdate{State: st, At: time.Now()})
	m.events.Pub(Event{Type: "session.exited", SessionID: s.rec.ID, Session: s.Rec()})
	// *→done|error is a catalog notify transition — agent.state carries it.
	m.events.Pub(Event{Type: "agent.state", SessionID: s.rec.ID, Process: fgName, Tool: fgTool, State: st})
	if st == domain.StateDone || st == domain.StateError {
		m.alert(s.rec.ID, fgName, prevSt, st, "", "")
	}
}

// alert emits the catalog `alert` frame AND persists the ack-able row —
// §05: the WS frame is the live edge, the alerts table is its history.
// Callers pre-guard on the notify set (waiting|done|error).
func (m *Manager) alert(sessionID, process string, prev, st domain.SessionState, kind domain.WaitKind, conf domain.Confidence) {
	lvl := alertLevel(st)
	tr := transition(prev, st)
	null := func(s string) sql.NullString {
		if s == "" {
			return sql.NullString{}
		}
		return sql.NullString{String: s, Valid: true}
	}
	if err := m.q.CreateAlert(context.Background(), gen.CreateAlertParams{
		SessionID: sessionID, Level: lvl, Transition: tr,
		Process: process, Kind: null(string(kind)), Confidence: null(string(conf)),
	}); err != nil {
		slog.Warn("alert insert", "session", sessionID, "err", err)
	}
	m.events.Pub(Event{Type: "alert", SessionID: sessionID, Level: lvl, Transition: tr})
}

// AgentView is one row of GET /api/agents — the needs-you surface:
// session identity + bound tool + detected state + live prompt.
// Ref: .teos/sections/05-agent-session-management.md
type AgentView struct {
	Session    *domain.Session     `json:"session"`
	Process    string              `json:"process"`
	Tool       string              `json:"tool,omitempty"`
	PinStatus  string              `json:"pin_status,omitempty"`
	State      domain.SessionState `json:"state"`
	Kind       domain.WaitKind     `json:"kind,omitempty"`
	Confidence domain.Confidence   `json:"confidence,omitempty"`
	Since      time.Time           `json:"since"`
	Prompt     *adapter.Prompt     `json:"prompt,omitempty"`
}

// Pins returns the adapter pin set with live badge status — /api/adapters.
func (m *Manager) Pins() []adapter.AdapterPin {
	out := make([]adapter.AdapterPin, 0, len(m.reg.adapters))
	for _, a := range m.reg.adapters {
		pin := a.Pin()
		pin.Status = m.pins.Status(pin.Tool)
		pin.SeenVersion = m.pins.Seen(pin.Tool)
		out = append(out, pin)
	}
	return out
}

// SyncPins upserts the declared pin set at boot.
func (m *Manager) SyncPins(ctx context.Context) error {
	return m.pins.SyncDeclared(ctx, m.reg.adapters)
}

// SessionContext feeds /api/commands/context/{session_id}: the live
// foreground process name + bound tool as process signals, the session
// cwd for file matching, and the bound tool's pin badge.
func (m *Manager) SessionContext(id string) (procs []string, cwd, tool, pinStatus string, err error) {
	s, err := m.Attach(id)
	if err != nil {
		return nil, "", "", "", err
	}
	cwd = s.Rec().Cwd
	tool, _ = m.reg.Bound(id)
	if fg := m.reg.fgName(id); fg != "" {
		procs = append(procs, fg)
	}
	if tool != "" {
		procs = append(procs, tool)
	}
	return procs, cwd, tool, m.pins.Status(tool), nil
}

// SetSlashDetect installs the server's slash-command detector — the
// gate event consumer calls it on live sessions when a plausible app
// is foregrounded (a rendered /-menu dirties the screen → quiescence
// fires → detection lands without a client fetch). §04, ADR-007.
func (m *Manager) SetSlashDetect(fn func(sessionID, app, cwd string, lines []string)) {
	m.slashDetect = fn
}

// screenLines flattens a screen — scrollback tail (capped) then
// visible rows — the shared read for ScreenText + gate-time slash
// detection. Read-only; nothing writes to the PTY.
func screenLines(scr *screen.Screen) []string {
	_, rows := scr.Size()
	var lines []string
	const tailRows = 160
	n := scr.ScrollbackLen()
	if n > tailRows {
		n = tailRows
	}
	for i := scr.ScrollbackLen() - n; i < scr.ScrollbackLen(); i++ {
		lines = append(lines, scr.ScrollbackRowText(i))
	}
	for y := 0; y < rows; y++ {
		lines = append(lines, scr.RowText(y))
	}
	return lines
}

// ScreenText returns the session's visible screen plus the scrollback
// tail as plain lines — the read source for observe-only detection
// (slash-menu scraping, §04). Read-only; nothing writes to the PTY.
func (m *Manager) ScreenText(id string) ([]string, error) {
	s, err := m.Attach(id)
	if err != nil {
		return nil, err
	}
	scr := s.Screen()
	lines := screenLines(scr)
	return lines, nil
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
			Session: s.Rec(), Process: m.reg.fgName(s.rec.ID),
			Tool: tool, State: u.State, Since: u.At,
		}
		if tool != "" {
			v.PinStatus = m.pins.Status(tool)
		}
		if u.Match != nil {
			v.Kind, v.Confidence, v.Prompt = u.Match.Kind, u.Match.Confidence, u.Match.Prompt
		}
		v.Session.Detached = m.isDetached(v.Session.DetachedAt)
		out = append(out, v)
	}

	// Ref: .teos/AGENTS.md — "never promote a low-confidence wait":
	// waiting{unknown} stays out of the top rank, the overlay shows
	// "may be waiting" only.
	rank := func(v AgentView) int {
		switch {
		case v.State == domain.StateWaitingForInput && v.Confidence != domain.ConfidenceLow:
			return 0
		case v.State == domain.StateDone || v.State == domain.StateError || v.State == domain.StateEnded:
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
		ri, rj := rank(out[i]), rank(out[j])
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
// Guarded on liveness: a Detach racing session close() must not stamp
// detached_at on a session that's already gone.
func (m *Manager) onDetached(s *ptySession) {
	m.mu.Lock()
	_, live := m.live[s.rec.ID]
	m.mu.Unlock()
	if !live {
		return
	}
	now := time.Now().UTC()
	s.setDetachedAt(now)
	_ = m.q.UpdateSessionDetached(context.Background(),
		gen.UpdateSessionDetachedParams{
			DetachedAt: sql.NullString{String: now.Format(time.RFC3339), Valid: true}, ID: s.rec.ID})
	m.events.Pub(Event{Type: "session.detached", SessionID: s.rec.ID})
}

// clearDetachedDB drops detached_at once a client is attached again —
// the stamp only exists while nobody is watching.
func (m *Manager) clearDetachedDB(id string) {
	if err := m.q.UpdateSessionDetached(context.Background(),
		gen.UpdateSessionDetachedParams{DetachedAt: sql.NullString{}, ID: id}); err != nil {
		slog.Warn("clear detached_at", "session", id, "err", err)
	}
}

// isDetached derives the `detached` overlay flag: nobody attached for
// longer than TU_RECONNECT_WINDOW_S (catalog: flag, not a state).
func (m *Manager) isDetached(at *time.Time) bool {
	return at != nil && time.Since(*at) > time.Duration(m.cfg.ReconnectWindowSec)*time.Second
}

// alertRearm is the dwell a session must hold a non-waiting state
// before its next waiting transition may persist another alert row
// (TU_ALERT_REARM_S) — redraw flaps never re-arm.
func (m *Manager) alertRearm() time.Duration {
	return time.Duration(m.cfg.AlertRearmSec) * time.Second
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

// CloseAll shuts every live PTY — graceful server stop: TERM first,
// one shared grace window, then KILL whatever ignored it. Children
// that exit fast don't cost the wait (done closes as they finish).
func (m *Manager) CloseAll() {
	m.mu.Lock()
	list := make([]*ptySession, 0, len(m.live))
	for _, s := range m.live {
		list = append(list, s)
	}
	m.mu.Unlock()
	for _, s := range list {
		s.terminate()
	}
	deadline := time.After(killGrace)
	for _, s := range list {
		select {
		case <-s.done:
		case <-deadline:
		}
	}
	for _, s := range list {
		_ = s.cmd.Process.Kill()
		s.close()
	}
}

func (m *Manager) rowToSession(r gen.Session) domain.Session {
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
	s := domain.Session{
		ID: r.ID, Name: r.Name,
		Target: domain.Target{Kind: r.TargetKind, ID: r.TargetID},
		Cwd:    r.Cwd, Shell: r.Shell,
		State: domain.SessionState(r.State), ExitCode: ec,
		WorkspaceID: wid, Cols: int(r.Cols), Rows: int(r.Rows),
		CreatedAt: cAt, DetachedAt: dAt, EndedAt: eAt,
	}
	s.Detached = m.isDetached(dAt)
	return s
}

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return "s_" + hex.EncodeToString(b)
}

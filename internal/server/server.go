// Ref: .teos/sections/10-networking-ws-protocol.md, .teos/conventions/api-routes-catalog.md
// HTTP router + middleware + error envelope (api-response-format).
package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jedafe/terminu/internal/auth"
	"github.com/jedafe/terminu/internal/broker"
	"github.com/jedafe/terminu/internal/config"
	"github.com/jedafe/terminu/internal/db/gen"
	"github.com/jedafe/terminu/internal/domain"
	"github.com/jedafe/terminu/internal/registry"
	"github.com/jedafe/terminu/internal/session"
)

type Server struct {
	cfg  *config.Config
	q    *gen.Queries
	auth *auth.Service
	br   *broker.Broker
	mgr  *session.Manager
	bus  *session.EventBus
	reg  *registry.Registry
	mux  *http.ServeMux

	throttle loginThrottle
}

func New(cfg *config.Config, q *gen.Queries, a *auth.Service, b *broker.Broker, m *session.Manager, bus *session.EventBus, reg *registry.Registry) *Server {
	s := &Server{cfg: cfg, q: q, auth: a, br: b, mgr: m, bus: bus, reg: reg, mux: http.NewServeMux(),
		throttle: loginThrottle{fails: map[string][]time.Time{}}}
	s.routes()
	return s
}

func (s *Server) Handler() http.Handler { return s.mux }

type ctxKey string

const ctxPrincipal ctxKey = "principal"

func principal(r *http.Request) *auth.Principal {
	p, _ := r.Context().Value(ctxPrincipal).(*auth.Principal)
	return p
}

// envelope per api-response-format.md: {data} | {error:{code,message,status}}.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"data": v})
}

func writeErr(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]any{"code": code, "message": msg, "status": status},
	})
}

// internalErr logs the real error and returns the catalog `internal`
// shape — error detail is logged server-side, never leaked to clients.
func (s *Server) internalErr(w http.ResponseWriter, err error) {
	slog.Error("internal error", "err", err)
	writeErr(w, 500, "internal", "internal error")
}

// requireAuth wraps handlers behind cookie auth.
func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, err := s.auth.Authenticate(r.Context(), r)
		if err != nil {
			writeErr(w, http.StatusUnauthorized, "unauthorized", "authentication required")
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), ctxPrincipal, p)))
	}
}

// requireRole additionally gates on a catalog role — e.g. the audit log
// is admin-only (routes catalog annotates it "(admin)").
func (s *Server) requireRole(role string, next http.HandlerFunc) http.HandlerFunc {
	return s.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		if p := principal(r); p == nil || p.Role != role {
			writeErr(w, http.StatusForbidden, "forbidden", "insufficient role")
			return
		}
		next(w, r)
	})
}

func (s *Server) routes() {
	// public
	s.mux.HandleFunc("GET /api/system/health", s.health)
	s.mux.HandleFunc("GET /api/system/version", s.version) // catalog: public
	s.mux.HandleFunc("POST /api/auth/login", s.login)

	// authed REST
	s.mux.HandleFunc("POST /api/auth/logout", s.requireAuth(s.logout))
	s.mux.HandleFunc("GET /api/auth/me", s.requireAuth(s.me))
	s.mux.HandleFunc("GET /api/sessions", s.requireAuth(s.listSessions))
	s.mux.HandleFunc("POST /api/sessions", s.requireAuth(s.spawnSession))
	s.mux.HandleFunc("GET /api/sessions/{id}", s.requireAuth(s.getSession))
	s.mux.HandleFunc("PATCH /api/sessions/{id}", s.requireAuth(s.patchSession))
	s.mux.HandleFunc("DELETE /api/sessions", s.requireAuth(s.clearFinished))
	s.mux.HandleFunc("DELETE /api/sessions/{id}", s.requireAuth(s.killSession))
	s.mux.HandleFunc("DELETE /api/sessions/{id}/log", s.requireAuth(s.purgeLog))
	s.mux.HandleFunc("GET /api/sessions/{id}/scrollback", s.requireAuth(s.scrollback))
	s.mux.HandleFunc("GET /api/agents", s.requireAuth(s.listAgents))
	s.mux.HandleFunc("GET /api/agents/{id}/prompt", s.requireAuth(s.agentPrompt))
	s.mux.HandleFunc("GET /api/agents/profiles", s.requireAuth(s.listProfiles))
	s.mux.HandleFunc("POST /api/agents/profiles", s.requireAuth(s.createProfile))
	s.mux.HandleFunc("PATCH /api/agents/profiles/{id}", s.requireAuth(s.patchProfile))
	s.mux.HandleFunc("DELETE /api/agents/profiles/{id}", s.requireAuth(s.deleteProfile))
	s.mux.HandleFunc("POST /api/agents/launch", s.requireAuth(s.launchProfile))
	s.mux.HandleFunc("GET /api/adapters", s.requireAuth(s.listAdapters))
	s.mux.HandleFunc("GET /api/adapters/{tool}", s.requireAuth(s.getAdapter))
	s.mux.HandleFunc("GET /api/commands", s.requireAuth(s.listCommands))
	s.mux.HandleFunc("GET /api/commands/context/{session_id}", s.requireAuth(s.commandContext))
	s.mux.HandleFunc("GET /api/commands/{id}", s.requireAuth(s.getCommand))
	s.mux.HandleFunc("GET /api/discovered", s.requireAuth(s.discovered))
	s.mux.HandleFunc("GET /api/alerts", s.requireAuth(s.listAlerts))
	s.mux.HandleFunc("POST /api/alerts/{id}/ack", s.requireAuth(s.ackAlert))
	s.mux.HandleFunc("GET /api/workspaces", s.requireAuth(s.listWorkspaces))
	s.mux.HandleFunc("PUT /api/workspaces/{id}", s.requireAuth(s.putWorkspace))
	s.mux.HandleFunc("GET /api/audit", s.requireRole("admin", s.listAudit))

	// websockets
	s.mux.HandleFunc("/ws/session/{id}", s.requireAuth(s.wsSession))
	s.mux.HandleFunc("/ws/events", s.requireAuth(s.wsEvents))

	// embedded SPA — last, catch-all
	s.mux.HandleFunc("/", s.staticHandler())
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"ok": true, "version": Version})
}

func (s *Server) version(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{
		"version": Version, "sessions_live": len(mustList(s.mgr)),
	})
}

func mustList(m *session.Manager) []domain.Session {
	l, err := m.List(context.Background())
	if err != nil {
		return nil
	}
	return l
}

// ---------- auth ----------

// loginThrottle buckets failed attempts — argon2 t=1 is ~50ms, so
// without it /api/auth/login is an unthrottled brute-force surface.
// Buckets are keyed, not global: the username key throttles credential
// attacks on an account, the IP key is a generous flood backstop.
// Keying on RemoteAddr alone would let one attacker behind the shared
// reverse proxy lock every user out (RemoteAddr is the proxy's IP).
type loginThrottle struct {
	mu    sync.Mutex
	fails map[string][]time.Time
}

const (
	throttleWindow = 10 * time.Minute
	throttleMax    = 5  // per username
	throttleIPMax  = 50 // per source IP — flood backstop, not the gate
)

func (t *loginThrottle) limited(key string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now()
	f := t.fails[key]
	keep := f[:0]
	for _, ts := range f {
		if now.Sub(ts) < throttleWindow {
			keep = append(keep, ts)
		}
	}
	t.fails[key] = keep
	if len(keep) == 0 {
		delete(t.fails, key)
	}
	max := throttleMax
	if strings.HasPrefix(key, "i:") {
		max = throttleIPMax
	}
	return len(keep) >= max
}

func (t *loginThrottle) fail(key string) {
	t.mu.Lock()
	t.fails[key] = append(t.fails[key], time.Now())
	t.mu.Unlock()
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	if ip == "" {
		ip = r.RemoteAddr
	}
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, 400, "bad_request", "invalid JSON")
		return
	}
	uKey := "u:" + strings.ToLower(strings.TrimSpace(body.Username))
	if s.throttle.limited(uKey) || s.throttle.limited("i:"+ip) {
		writeErr(w, 429, "rate_limited", "too many attempts — try again later")
		return
	}
	token, err := s.auth.Login(r.Context(), body.Username, body.Password)
	if err != nil {
		s.throttle.fail(uKey)
		s.throttle.fail("i:" + ip)
		writeErr(w, 401, "unauthorized", "invalid credentials")
		return
	}
	auth.SetCookie(w, token, r.TLS != nil || s.cfg.PublicURL != "" && strings.HasPrefix(s.cfg.PublicURL, "https"))
	writeJSON(w, 200, map[string]string{"status": "ok"})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(auth.CookieName); err == nil {
		s.auth.Logout(r.Context(), c.Value)
	}
	auth.ClearCookie(w)
	writeJSON(w, 200, map[string]string{"status": "ok"})
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	writeJSON(w, 200, p)
}

// ---------- sessions ----------

func (s *Server) listSessions(w http.ResponseWriter, r *http.Request) {
	l, err := s.mgr.List(r.Context())
	if err != nil {
		s.internalErr(w, err)
		return
	}
	writeJSON(w, 200, l)
}

// Spawn bounds: sessions allocate cols×rows cells + scrollback in
// memory — unvalidated dimensions are a memory-exhaustion vector that
// breaks the <15 MB/session budget (Rule 13).
const (
	maxSpawnCols  = 512
	maxSpawnRows  = 256
	maxSpawnField = 4096 // cwd
	maxSpawnShort = 512  // target / shell
	maxSpawnName  = 128
)

func (s *Server) spawnSession(w http.ResponseWriter, r *http.Request) {
	var req domain.SpawnRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, "bad_request", "invalid JSON")
		return
	}
	// The catalog contract here is {target, cwd, shell} — Argv/Env on
	// SpawnRequest are Go-level surfaces for manifest execution and
	// profile launch, which reach Spawn through their own brokered
	// routes (audited with the real argv). Honoring them on this route
	// would spawn arbitrary commands whose audit row logs only
	// shell+cwd — drop them.
	req.Argv, req.Env = nil, nil
	if req.Cols < 0 || req.Cols > maxSpawnCols || req.Rows < 0 || req.Rows > maxSpawnRows ||
		len(req.Name) > maxSpawnName || len(req.Cwd) > maxSpawnField ||
		len(req.Shell) > maxSpawnShort || len(req.Target) > maxSpawnShort {
		writeErr(w, 400, "bad_request", "field out of bounds")
		return
	}
	p := principal(r)
	op := broker.Op{
		Actor: p, Action: "terminal.spawn",
		Target: req.Target, Argv: []string{req.Shell, req.Cwd}, Risk: broker.RiskControlled,
	}
	if err := s.br.Check(op); err != nil {
		writeErr(w, 403, "action_restricted", "role may not spawn sessions")
		return
	}
	rec, err := s.mgr.Spawn(r.Context(), req)
	res := "ok"
	if err != nil {
		res = "error"
	}
	if aerr := s.br.Audit(r.Context(), op, res); aerr != nil {
		slog.Error("audit write failed", "err", aerr)
		// ADR-004: no silent privileged actions — a failed audit row is a
		// loud error, never a quiet success.
		writeErr(w, 500, "internal", "audit write failed")
		return
	}
	if err != nil {
		if errors.Is(err, session.ErrLimit) {
			writeErr(w, 409, "session_limit", "session limit reached")
			return
		}
		if errors.Is(err, session.ErrExecutable) {
			writeErr(w, 400, "bad_request", err.Error())
			return
		}
		s.internalErr(w, err)
		return
	}
	writeJSON(w, 201, rec)
}

func (s *Server) getSession(w http.ResponseWriter, r *http.Request) {
	rec, err := s.mgr.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, 404, "session_not_found", "no such session")
		return
	}
	writeJSON(w, 200, rec)
}

// ---------- agents (§05 needs-you) ----------

func (s *Server) listAgents(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, s.mgr.Agents())
}

// agentPrompt is the catalog's fetch path for the current detection —
// session-socket attaches resync it too, but a list row shouldn't need
// a full PTY attach to render its buttons.
func (s *Server) agentPrompt(w http.ResponseWriter, r *http.Request) {
	sess, err := s.mgr.Attach(r.PathValue("id"))
	if err != nil {
		writeErr(w, 404, "session_not_found", "no live session")
		return
	}
	u := sess.CurrentState()
	resp := map[string]any{
		"state": u.State, "prompt": nil,
	}
	if u.Match != nil {
		resp["kind"] = u.Match.Kind
		resp["confidence"] = u.Match.Confidence
		resp["region"] = u.Match.Region
		resp["evidence"] = u.Match.Evidence
		resp["prompt"] = u.Match.Prompt
	}
	writeJSON(w, 200, resp)
}

func (s *Server) patchSession(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name *string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, 400, "bad_request", "invalid JSON")
		return
	}
	id := r.PathValue("id")
	if _, err := s.mgr.Get(r.Context(), id); err != nil {
		writeErr(w, 404, "session_not_found", "no such session")
		return
	}
	if body.Name != nil {
		if err := s.mgr.Rename(r.Context(), id, *body.Name); err != nil {
			s.internalErr(w, err)
			return
		}
	}
	writeJSON(w, 200, map[string]string{"status": "ok"})
}

// killSession is the catalog's DELETE /api/sessions/{id}: a live session
// is terminated → `ended` (row + log kept); a finished one is
// hard-deleted with its state log.
func (s *Server) killSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	p := principal(r)
	rec, err := s.mgr.Get(r.Context(), id)
	if err != nil {
		writeErr(w, 404, "session_not_found", "no such session")
		return
	}
	finished := session.IsFinished(rec.State)
	action := "session.kill"
	if finished {
		action = "session.delete"
	}
	op := broker.Op{Actor: p, Action: action, Target: id, Risk: broker.RiskControlled}
	if err := s.br.Check(op); err != nil {
		writeErr(w, 403, "action_restricted", "role may not modify sessions")
		return
	}
	if finished {
		err = s.mgr.Delete(r.Context(), id)
	} else {
		err = s.mgr.Kill(r.Context(), id)
	}
	res := "ok"
	if err != nil {
		res = "error"
	}
	if aerr := s.br.Audit(r.Context(), op, res); aerr != nil {
		slog.Error("audit write failed", "err", aerr)
		writeErr(w, 500, "internal", "audit write failed")
		return
	}
	if err != nil {
		s.internalErr(w, err)
		return
	}
	if finished {
		s.bus.Pub(session.Event{Type: "session.removed", SessionID: id})
	}
	writeJSON(w, 200, map[string]string{"status": "ok"})
}

// clearFinished is DELETE /api/sessions — bulk hard-delete of all
// finished rows (done/error/ended) and their state logs.
func (s *Server) clearFinished(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	op := broker.Op{Actor: p, Action: "session.clear", Risk: broker.RiskControlled}
	if err := s.br.Check(op); err != nil {
		writeErr(w, 403, "action_restricted", "role may not clear sessions")
		return
	}
	ids, err := s.mgr.ClearFinished(r.Context())
	res := "ok"
	if err != nil {
		res = "error"
	}
	if aerr := s.br.Audit(r.Context(), op, res); aerr != nil {
		slog.Error("audit write failed", "err", aerr)
		writeErr(w, 500, "internal", "audit write failed")
		return
	}
	if err != nil {
		s.internalErr(w, err)
		return
	}
	for _, id := range ids {
		s.bus.Pub(session.Event{Type: "session.removed", SessionID: id})
	}
	writeJSON(w, 200, map[string]any{"cleared": len(ids)})
}

// scrollback returns retained output bytes — live sessions read the
// ring, finished rows read the persisted file. Client-side buffer
// search/pages per section-03. ?offset=N pages: bytes before it are
// skipped; X-Total-Size reports the full retained size.
func (s *Server) scrollback(w http.ResponseWriter, r *http.Request) {
	data, err := s.mgr.Scrollback(r.PathValue("id"))
	if err != nil {
		writeErr(w, 404, "session_not_found", "no such session")
		return
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if offset < 0 || offset > len(data) {
		offset = 0
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("X-Total-Size", strconv.Itoa(len(data)))
	_, _ = w.Write(data[offset:])
}

// purgeLog is DELETE /api/sessions/{id}/log — drops only the transition
// history; the row + scrollback stay (the "keep output, drop noise" tier).
func (s *Server) purgeLog(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	p := principal(r)
	op := broker.Op{Actor: p, Action: "session.clearlog", Target: id, Risk: broker.RiskControlled}
	if err := s.br.Check(op); err != nil {
		writeErr(w, 403, "action_restricted", "role may not clear session logs")
		return
	}
	if _, err := s.mgr.Get(r.Context(), id); err != nil {
		writeErr(w, 404, "session_not_found", "no such session")
		return
	}
	err := s.mgr.PurgeLog(r.Context(), id)
	res := "ok"
	if err != nil {
		res = "error"
	}
	if aerr := s.br.Audit(r.Context(), op, res); aerr != nil {
		slog.Error("audit write failed", "err", aerr)
		writeErr(w, 500, "internal", "audit write failed")
		return
	}
	if err != nil {
		s.internalErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]string{"status": "ok"})
}

// ---------- workspaces ----------

func (s *Server) listWorkspaces(w http.ResponseWriter, r *http.Request) {
	rows, err := s.q.ListWorkspaces(r.Context())
	if err != nil {
		s.internalErr(w, err)
		return
	}
	out := []domain.Workspace{}
	for _, x := range rows {
		out = append(out, domain.Workspace{
			ID: x.ID, Name: x.Name, Layout: x.Layout,
			LastSession: x.LastOpenSession.String, UpdatedAt: parseT(x.UpdatedAt),
		})
	}
	writeJSON(w, 200, out)
}

func (s *Server) putWorkspace(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name        string `json:"name"`
		Layout      string `json:"layout"`
		LastSession string `json:"last_open_session"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, 400, "bad_request", "invalid JSON")
		return
	}
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		writeErr(w, 400, "bad_request", "workspace id must be numeric")
		return
	}
	if strings.TrimSpace(body.Name) == "" {
		writeErr(w, 400, "bad_request", "workspace name required")
		return
	}
	if body.Layout != "" && !json.Valid([]byte(body.Layout)) {
		writeErr(w, 400, "bad_request", "layout must be JSON")
		return
	}
	if err := s.q.UpsertWorkspace(r.Context(), gen.UpsertWorkspaceParams{
		ID: id, Name: body.Name, Layout: body.Layout,
		LastOpenSession: nullString(body.LastSession),
	}); err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			writeErr(w, 400, "bad_request", "workspace name already exists")
			return
		}
		s.internalErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]string{"status": "ok"})
}

func (s *Server) listAudit(w http.ResponseWriter, r *http.Request) {
	rows, err := s.q.ListAudit(r.Context(), 200)
	if err != nil {
		s.internalErr(w, err)
		return
	}
	writeJSON(w, 200, rows)
}

// ---------- alerts ----------

// alertOut is the API shape of one alerts row — sql.Null* fields
// flattened to strings/omitted for the client.
type alertOut struct {
	ID         int64  `json:"id"`
	SessionID  string `json:"session_id"`
	Level      string `json:"level"`
	Transition string `json:"transition"`
	Process    string `json:"process,omitempty"`
	Kind       string `json:"kind,omitempty"`
	Confidence string `json:"confidence,omitempty"`
	AckedAt    string `json:"acked_at,omitempty"`
	At         string `json:"at"`
}

// listAlerts is GET /api/alerts — unacknowledged first, newest first.
func (s *Server) listAlerts(w http.ResponseWriter, r *http.Request) {
	rows, err := s.q.ListAlerts(r.Context(), 200)
	if err != nil {
		s.internalErr(w, err)
		return
	}
	out := make([]alertOut, 0, len(rows))
	for _, a := range rows {
		out = append(out, alertOut{
			ID: a.ID, SessionID: a.SessionID, Level: a.Level,
			Transition: a.Transition, Process: a.Process,
			Kind: a.Kind.String, Confidence: a.Confidence.String,
			AckedAt: a.AckedAt.String, At: a.At,
		})
	}
	writeJSON(w, 200, out)
}

// ackAlert is POST /api/alerts/{id}/ack — idempotent UI state, not a
// broker action (no privileged execution happens).
func (s *Server) ackAlert(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		writeErr(w, 400, "bad_request", "alert id must be numeric")
		return
	}
	if err := s.q.AckAlert(r.Context(), id); err != nil {
		s.internalErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]string{"status": "ok"})
}

func nullString(v string) sql.NullString {
	if v == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: v, Valid: true}
}

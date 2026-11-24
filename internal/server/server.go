// Ref: .teos/sections/10-networking-ws-protocol.md, .teos/conventions/api-routes-catalog.md
// HTTP router + middleware + error envelope (api-response-format).
package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/jedafe/terminu/internal/auth"
	"github.com/jedafe/terminu/internal/broker"
	"github.com/jedafe/terminu/internal/config"
	"github.com/jedafe/terminu/internal/db/gen"
	"github.com/jedafe/terminu/internal/domain"
	"github.com/jedafe/terminu/internal/session"
)

type Server struct {
	cfg  *config.Config
	q    *gen.Queries
	auth *auth.Service
	br   *broker.Broker
	mgr  *session.Manager
	bus  *session.EventBus
	mux  *http.ServeMux
}

func New(cfg *config.Config, q *gen.Queries, a *auth.Service, b *broker.Broker, m *session.Manager, bus *session.EventBus) *Server {
	s := &Server{cfg: cfg, q: q, auth: a, br: b, mgr: m, bus: bus, mux: http.NewServeMux()}
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

func (s *Server) routes() {
	// public
	s.mux.HandleFunc("GET /api/system/health", s.health)
	s.mux.HandleFunc("POST /api/auth/login", s.login)

	// authed REST
	s.mux.HandleFunc("POST /api/auth/logout", s.requireAuth(s.logout))
	s.mux.HandleFunc("GET /api/auth/me", s.requireAuth(s.me))
	s.mux.HandleFunc("GET /api/system/version", s.requireAuth(s.version))
	s.mux.HandleFunc("GET /api/sessions", s.requireAuth(s.listSessions))
	s.mux.HandleFunc("POST /api/sessions", s.requireAuth(s.spawnSession))
	s.mux.HandleFunc("GET /api/sessions/{id}", s.requireAuth(s.getSession))
	s.mux.HandleFunc("PATCH /api/sessions/{id}", s.requireAuth(s.patchSession))
	s.mux.HandleFunc("DELETE /api/sessions/{id}", s.requireAuth(s.killSession))
	s.mux.HandleFunc("GET /api/sessions/{id}/scrollback", s.requireAuth(s.scrollback))
	s.mux.HandleFunc("GET /api/agents", s.requireAuth(s.listAgents))
	s.mux.HandleFunc("GET /api/agents/{id}/prompt", s.requireAuth(s.agentPrompt))
	s.mux.HandleFunc("GET /api/workspaces", s.requireAuth(s.listWorkspaces))
	s.mux.HandleFunc("PUT /api/workspaces/{id}", s.requireAuth(s.putWorkspace))
	s.mux.HandleFunc("GET /api/audit", s.requireAuth(s.listAudit))

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

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, 400, "bad_request", "invalid JSON")
		return
	}
	token, err := s.auth.Login(r.Context(), body.Username, body.Password)
	if err != nil {
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
		writeErr(w, 500, "internal", err.Error())
		return
	}
	writeJSON(w, 200, l)
}

func (s *Server) spawnSession(w http.ResponseWriter, r *http.Request) {
	var req domain.SpawnRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, "bad_request", "invalid JSON")
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
	}
	if err != nil {
		if errors.Is(err, session.ErrLimit) {
			writeErr(w, 429, "session_limit", "session limit reached")
			return
		}
		writeErr(w, 500, "internal", err.Error())
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
			writeErr(w, 500, "internal", err.Error())
			return
		}
	}
	writeJSON(w, 200, map[string]string{"status": "ok"})
}

func (s *Server) killSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	p := principal(r)
	op := broker.Op{Actor: p, Action: "session.kill", Target: id, Risk: broker.RiskControlled}
	if err := s.br.Check(op); err != nil {
		writeErr(w, 403, "action_restricted", "role may not kill sessions")
		return
	}
	if _, err := s.mgr.Get(r.Context(), id); err != nil {
		writeErr(w, 404, "session_not_found", "no such session")
		return
	}
	err := s.mgr.Kill(r.Context(), id)
	res := "ok"
	if err != nil {
		res = "error"
	}
	_ = s.br.Audit(r.Context(), op, res)
	if err != nil {
		writeErr(w, 500, "internal", err.Error())
		return
	}
	s.bus.Pub(session.Event{Type: "session.removed", SessionID: id})
	writeJSON(w, 200, map[string]string{"status": "ok"})
}

// scrollback returns buffered output bytes (concatenated) — client-side
// buffer search/pages per section-03. In-memory only; nothing persists.
func (s *Server) scrollback(w http.ResponseWriter, r *http.Request) {
	chunks, err := s.mgr.Scrollback(r.PathValue("id"))
	if err != nil {
		writeErr(w, 404, "session_not_found", "no such session")
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	for _, c := range chunks {
		_, _ = w.Write(c.Data())
	}
}

// ---------- workspaces ----------

func (s *Server) listWorkspaces(w http.ResponseWriter, r *http.Request) {
	rows, err := s.q.ListWorkspaces(r.Context())
	if err != nil {
		writeErr(w, 500, "internal", err.Error())
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
	var id int64
	if _, err := parseID(r.PathValue("id")); err == nil {
		// path id wins when numeric
	}
	id, _ = parseID(r.PathValue("id"))
	if err := s.q.UpsertWorkspace(r.Context(), gen.UpsertWorkspaceParams{
		ID: id, Name: body.Name, Layout: body.Layout,
		LastOpenSession: nullString(body.LastSession),
	}); err != nil {
		writeErr(w, 500, "internal", err.Error())
		return
	}
	writeJSON(w, 200, map[string]string{"status": "ok"})
}

func (s *Server) listAudit(w http.ResponseWriter, r *http.Request) {
	rows, err := s.q.ListAudit(r.Context(), 200)
	if err != nil {
		writeErr(w, 500, "internal", err.Error())
		return
	}
	writeJSON(w, 200, rows)
}

func nullString(v string) sql.NullString {
	if v == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: v, Valid: true}
}

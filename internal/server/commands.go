// Ref: .teos/sections/04-command-intelligence.md, §06 (adapters).
// Palette search, manifest detail, broker-gated execution, context
// actions, --help discovery, and the adapter pin/fixture surfaces.
package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/jedafe/terminu/internal/broker"
	"github.com/jedafe/terminu/internal/domain"
	"github.com/jedafe/terminu/internal/registry"
	"github.com/jedafe/terminu/internal/session"
)

// commandSummary is the palette/list row shape — full manifests only
// come down on the detail route.
type commandSummary struct {
	ID           string   `json:"id"`
	Application  string   `json:"application"`
	Name         string   `json:"name"`
	Description  string   `json:"description,omitempty"`
	Tags         []string `json:"tags,omitempty"`
	Source       string   `json:"source"`
	Risk         string   `json:"risk"`
	Confirmation bool     `json:"confirmation"`
	Context      bool     `json:"context,omitempty"` // matched the session's live context
}

func summaryOf(m *registry.Manifest, ctx bool) commandSummary {
	return commandSummary{
		ID: m.ID, Application: m.Application, Name: m.Name,
		Description: m.Description, Tags: m.Tags, Source: m.Source,
		Risk: m.Execution.Risk, Confirmation: m.Execution.Confirmation,
		Context: ctx,
	}
}

// listCommands is GET /api/commands?q=&session_id=&limit= — the palette
// surface. session_id scopes context-first ranking to that session's
// live process/files (§04 Palette).
func (s *Server) listCommands(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	ctxIDs := map[string]bool{}
	if sid := r.URL.Query().Get("session_id"); sid != "" {
		if procs, cwd, _, _, err := s.mgr.SessionContext(sid); err == nil {
			for _, m := range s.reg.Context(procs, cwd) {
				ctxIDs[m.ID] = true
			}
		}
	}
	out := make([]commandSummary, 0, limit)
	for _, sc := range s.reg.Search(q, ctxIDs, limit) {
		out = append(out, summaryOf(sc.Manifest, sc.Context))
	}
	writeJSON(w, 200, out)
}

// getCommand is GET /api/commands/{id} — full manifest detail.
func (s *Server) getCommand(w http.ResponseWriter, r *http.Request) {
	m, ok := s.reg.Get(r.PathValue("id"))
	if !ok {
		writeErr(w, 404, "command_not_found", "no such command")
		return
	}
	writeJSON(w, 200, m)
}

// commandContext is GET /api/commands/context/{session_id} — manifests
// whose context matches the session's live foreground process/tool or
// cwd files (§04 Context actions).
func (s *Server) commandContext(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("session_id")
	procs, cwd, tool, pinStatus, err := s.mgr.SessionContext(id)
	if err != nil {
		writeErr(w, 404, "session_not_found", "no live session")
		return
	}
	ms := s.reg.Context(procs, cwd)
	out := make([]commandSummary, 0, len(ms))
	for _, m := range ms {
		out = append(out, summaryOf(m, true))
	}
	writeJSON(w, 200, map[string]any{
		"session_id": id, "processes": procs, "tool": tool,
		"pin_status": pinStatus, "cwd": cwd, "commands": out,
	})
}

// execRequest is POST /api/commands/{id}/execute — params resolve into
// the manifest's argv templates; session_id supplies {session.cwd}.
type execRequest struct {
	SessionID    string         `json:"session_id"`
	Params       map[string]any `json:"params"`
	Cwd          string         `json:"cwd"`
	Name         string         `json:"name"`
	Cols         int            `json:"cols"`
	Rows         int            `json:"rows"`
	ConfirmToken string         `json:"confirm_token"`
}

// executeCommand resolves params → structured argv → broker → spawn.
// The command runs as a session — output, scrollback, and detection are
// the same machinery as any terminal (there is no out-of-band exec
// path). `confirmed` risk needs a confirm_token bound to the argv.
func (s *Server) executeCommand(w http.ResponseWriter, r *http.Request) {
	m, ok := s.reg.Get(r.PathValue("id"))
	if !ok {
		writeErr(w, 404, "command_not_found", "no such command")
		return
	}
	var req execRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, "bad_request", "invalid JSON")
		return
	}
	sctx := registry.SessionCtx{}
	if req.SessionID != "" {
		_, cwd, _, _, err := s.mgr.SessionContext(req.SessionID)
		if err != nil {
			writeErr(w, 404, "session_not_found", "no live session")
			return
		}
		sctx.Cwd = cwd
	}
	argv, cwd, err := m.Build(req.Params, sctx)
	if err != nil {
		writeErr(w, 400, "invalid_manifest", err.Error())
		return
	}
	if req.Cwd != "" {
		cwd = req.Cwd
	}
	p := principal(r)
	op := broker.Op{
		Actor: p, Action: "command.execute", Target: "local",
		Argv: argv, Risk: broker.Risk(m.Execution.Risk),
	}
	if err := s.br.Check(op); err != nil {
		writeErr(w, 403, "action_restricted", "role may not execute this command")
		return
	}
	// confirmed risk = explicit confirmation token bound to this argv
	// (§09); controlled rides role policy + the client's confirm dialog.
	if m.Execution.Risk == "confirmed" {
		if !broker.VerifyConfirmation(req.ConfirmToken, s.cfg.SessionSecret, p.Username, op.Action, argv) {
			token := broker.MintConfirmation(s.cfg.SessionSecret, p.Username, op.Action, argv)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(409)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": map[string]any{
					"code": "confirmation_required", "status": 409,
					"message": "execution requires confirmation",
					"detail": map[string]any{
						"confirm_token": token, "argv": argv,
						"expires_in_s": int(broker.ConfirmTTL.Seconds()),
					},
				},
			})
			return
		}
	}
	name := req.Name
	if name == "" {
		name = m.Name
	}
	rec, err := s.mgr.Spawn(r.Context(), domain.SpawnRequest{
		Name: name, Cwd: cwd, Argv: argv, Cols: req.Cols, Rows: req.Rows,
	})
	res := "ok"
	if err != nil {
		res = "error"
	}
	if aerr := s.br.Audit(r.Context(), op, res); aerr != nil {
		s.internalErr(w, aerr)
		return
	}
	if err != nil {
		if errors.Is(err, session.ErrLimit) {
			writeErr(w, 409, "session_limit", "session limit reached")
			return
		}
		s.internalErr(w, err)
		return
	}
	writeJSON(w, 201, map[string]any{
		"session": rec, "argv": argv, "manifest_id": m.ID,
	})
}

// discovered is GET /api/discovered?exe=&target= — with exe it runs
// --help discovery on the local target (broker-gated, audited); without
// it lists provisional manifests already in the registry.
func (s *Server) discovered(w http.ResponseWriter, r *http.Request) {
	exe := strings.TrimSpace(r.URL.Query().Get("exe"))
	if exe == "" {
		ms := s.reg.ListDiscovered()
		out := make([]commandSummary, 0, len(ms))
		for _, m := range ms {
			out = append(out, summaryOf(m, false))
		}
		writeJSON(w, 200, out)
		return
	}
	if tgt := r.URL.Query().Get("target"); tgt != "" && tgt != "local" {
		writeErr(w, 400, "bad_request", "discovery supports target=local in v1")
		return
	}
	p := principal(r)
	op := broker.Op{
		Actor: p, Action: "command.discover", Target: "local",
		Argv: []string{exe, "--help"}, Risk: broker.RiskControlled,
	}
	if err := s.br.Check(op); err != nil {
		writeErr(w, 403, "action_restricted", "role may not run discovery")
		return
	}
	ms, err := s.reg.Discover(r.Context(), exe)
	res := "ok"
	if err != nil {
		res = "error"
	}
	if aerr := s.br.Audit(r.Context(), op, res); aerr != nil {
		s.internalErr(w, aerr)
		return
	}
	if err != nil {
		if errors.Is(err, registry.ErrInvalidManifest) {
			writeErr(w, 400, "invalid_manifest", "invalid tool name")
			return
		}
		writeErr(w, 502, "provider_unavailable", "could not read --help from "+exe)
		return
	}
	out := make([]commandSummary, 0, len(ms))
	for _, m := range ms {
		out = append(out, summaryOf(m, false))
	}
	writeJSON(w, 200, out)
}

// ---------- adapters (§06) ----------

// listAdapters is GET /api/adapters — the pin-badge surface.
func (s *Server) listAdapters(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, s.mgr.Pins())
}

// getAdapter is GET /api/adapters/{tool} — pin detail + fixture replay
// status (the table the boot smoke writes).
func (s *Server) getAdapter(w http.ResponseWriter, r *http.Request) {
	tool := r.PathValue("tool")
	var pin any
	for _, p := range s.mgr.Pins() {
		if p.Tool == tool {
			pin = p
			break
		}
	}
	if pin == nil {
		writeErr(w, 404, "adapter_not_found", "no such adapter")
		return
	}
	rows, err := s.q.ListFixturesByTool(r.Context(), tool)
	if err != nil {
		s.internalErr(w, err)
		return
	}
	type fixtureOut struct {
		Version    string `json:"version"`
		Scenario   string `json:"scenario"`
		LastResult string `json:"last_result"`
		ReplayedAt string `json:"replayed_at,omitempty"`
	}
	fx := make([]fixtureOut, 0, len(rows))
	for _, f := range rows {
		fx = append(fx, fixtureOut{
			Version: f.Version, Scenario: f.Scenario,
			LastResult: f.LastResult, ReplayedAt: f.ReplayedAt.String,
		})
	}
	writeJSON(w, 200, map[string]any{"pin": pin, "fixtures": fx})
}

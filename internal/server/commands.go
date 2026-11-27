// Ref: .teos/sections/04-command-intelligence.md, §06 (adapters).
// Palette search, manifest detail, context cheat-sheet, --help
// discovery, and the adapter pin/fixture surfaces. ADR-007: display
// only — manifests inform the palette, nothing here executes them.
package server

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jedafe/terminu/internal/auth"
	"github.com/jedafe/terminu/internal/broker"
	"github.com/jedafe/terminu/internal/registry"
	"github.com/jedafe/terminu/internal/session"
)

// systemPrincipal is the audit actor for daemon-initiated execution —
// gate-driven slash probes have no HTTP request behind them.
var systemPrincipal = &auth.Principal{Username: "system", Role: "admin"}

// slashProbeInterval throttles the per-app probe exec — screen and
// dir evidence refresh every gate cycle, a subprocess does not.
const slashProbeInterval = 5 * time.Minute

// slashDetect is the Manager's SetSlashDetect hook — gate events drive
// it (a rendered /-menu lands here within seconds, not on the next
// palette open). Same pipeline as the context route: screen
// candidates ∪ command dirs ∪ the agent's own list probe →
// DiscoverSlash's diff-gated persist. §04, observe-only (ADR-007).
func (s *Server) slashDetect(sessionID, app, cwd string, lines []string) {
	cmds := map[string]string{}
	for name, desc := range registry.ParseSlashCandidates(lines) {
		cmds[name] = desc
	}
	home, _ := os.UserHomeDir()
	for _, d := range registry.SlashCommandDirs(app, home, cwd) {
		for name, desc := range registry.ScanSlashDir(d) {
			if _, ok := cmds[name]; !ok {
				cmds[name] = desc
			}
		}
	}
	if pargv := registry.SlashProbeArgv(app); pargv != nil && s.probeDue(app) {
		op := broker.Op{
			Actor: systemPrincipal, Action: "command.slash_probe", Target: "local",
			Argv: append([]string{app}, pargv...), Risk: broker.RiskControlled,
		}
		if s.br.Check(op) == nil {
			for name, desc := range s.reg.ProbeSlash(context.Background(), app, cwd) {
				if _, ok := cmds[name]; !ok {
					cmds[name] = desc
				}
			}
			_ = s.br.Audit(context.Background(), op, "ok")
		}
	}
	if changed, err := s.reg.DiscoverSlash(context.Background(), app, cmds); err == nil && changed {
		// The set grew/shrank — push so an open commands view
		// refetches instead of waiting for the next client trigger.
		s.bus.Pub(session.Event{Type: "commands.changed", SessionID: sessionID})
	}
}

// probeDue rate-limits the agent self-list exec to once per
// slashProbeInterval per app.
func (s *Server) probeDue(app string) bool {
	s.probeMu.Lock()
	defer s.probeMu.Unlock()
	if time.Since(s.probeAt[app]) >= slashProbeInterval {
		s.probeAt[app] = time.Now()
		return true
	}
	return false
}

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
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	// ?application= pins the list to one app — the apps modal's detail
	// surface ("all commands for X"), still q-searchable within it.
	if app := strings.TrimSpace(r.URL.Query().Get("application")); app != "" {
		ms := s.reg.ForApplication(app)
		if len(ms) > limit {
			ms = ms[:limit]
		}
		out := make([]commandSummary, 0, len(ms))
		for _, m := range ms {
			out = append(out, summaryOf(m, false))
		}
		writeJSON(w, 200, out)
		return
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
	// Auto-discovery (§04): a live foreground process with zero
	// non-slash manifests gets one dedup'd, broker-gated --help scrape —
	// an uncurated CLI surfaces its own commands instead of nothing.
	// Slash manifests don't count as coverage: they describe the agent's
	// in-TUI surface, not the binary's real flags.
	cover := 0
	for _, m := range ms {
		slash := false
		for _, t := range m.Tags {
			if t == "slash" {
				slash = true
				break
			}
		}
		if !slash {
			cover++
		}
	}
	if cover == 0 && len(procs) > 0 && procs[0] != tool {
		exe := procs[0]
		op := broker.Op{
			Actor: principal(r), Action: "command.discover", Target: "local",
			Argv: []string{exe, "--help"}, Risk: broker.RiskControlled,
		}
		if s.br.Check(op) == nil {
			dms, ran, derr := s.reg.DiscoverOnce(r.Context(), exe)
			if ran {
				res := "ok"
				if derr != nil {
					res = "error"
				}
				_ = s.br.Audit(r.Context(), op, res)
			}
			if derr == nil && len(dms) > 0 {
				ms = s.reg.Context(procs, cwd)
			}
		}
	}
	// Slash detection (§04): an agent session's slash surface is
	// observed — the rendered screen (an open /menu or /help listing)
	// plus the agent's command dirs under home and the session cwd.
	// The app key is the bound tool, falling back to the live fg proc —
	// an agent the adapter didn't bind still gets detected. Persisted
	// as discovered manifests, rewritten only on a real set change;
	// observe-only, nothing is typed into the PTY (ADR-007).
	detectApp := tool
	if detectApp == "" && len(procs) > 0 {
		detectApp = procs[0]
	}
	if detectApp != "" {
		cmds := map[string]string{}
		if lines, lerr := s.mgr.ScreenText(id); lerr == nil {
			for name, desc := range registry.ParseSlashCandidates(lines) {
				cmds[name] = desc
			}
		}
		home, _ := os.UserHomeDir()
		for _, d := range registry.SlashCommandDirs(detectApp, home, cwd) {
			for name, desc := range registry.ScanSlashDir(d) {
				if _, ok := cmds[name]; !ok {
					cmds[name] = desc
				}
			}
		}
		// Probe: agents that self-report their slash surface (devin
		// `skills list`) — the only source that covers built-ins when
		// no skill dirs exist and no menu has rendered. Broker-gated +
		// audited like the --help discover path.
		if pargv := registry.SlashProbeArgv(detectApp); pargv != nil {
			op := broker.Op{
				Actor: principal(r), Action: "command.slash_probe", Target: "local",
				Argv: append([]string{detectApp}, pargv...), Risk: broker.RiskControlled,
			}
			if s.br.Check(op) == nil {
				for name, desc := range s.reg.ProbeSlash(r.Context(), detectApp, cwd) {
					if _, ok := cmds[name]; !ok {
						cmds[name] = desc
					}
				}
				_ = s.br.Audit(r.Context(), op, "ok")
			}
		}
		if changed, derr := s.reg.DiscoverSlash(r.Context(), detectApp, cmds); derr == nil && changed {
			ms = s.reg.Context(procs, cwd)
		}
	}
	out := make([]commandSummary, 0, len(ms))
	for _, m := range ms {
		out = append(out, summaryOf(m, true))
	}
	writeJSON(w, 200, map[string]any{
		"session_id": id, "processes": procs, "tool": tool,
		"pin_status": pinStatus, "cwd": cwd, "commands": out,
	})
}

// appsInventory is GET /api/apps — the installed-CLI inventory backing
// the ⌘ apps modal: $PATH executables merged with every application
// the registry carries manifests for.
func (s *Server) appsInventory(w http.ResponseWriter, r *http.Request) {
	entries, at := s.reg.Apps()
	writeJSON(w, 200, map[string]any{
		"apps": entries, "count": len(entries), "scanned_at": at,
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

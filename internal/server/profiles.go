// Ref: .teos/sections/05-agent-session-management.md (launch profiles).
// Saved per-tool launch configs — CRUD + broker-gated launch that spawns
// a session running the profile's argv directly.
package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/jedafe/terminu/internal/broker"
	"github.com/jedafe/terminu/internal/db/gen"
	"github.com/jedafe/terminu/internal/domain"
	"github.com/jedafe/terminu/internal/session"
)

// profileOut is the API shape — argv/resume_cmd JSON arrays and env a
// JSON object (the table stores them as TEXT).
type profileOut struct {
	ID        int64             `json:"id"`
	Name      string            `json:"name"`
	Tool      string            `json:"tool"`
	Argv      []string          `json:"argv"`
	Env       map[string]string `json:"env"`
	Cwd       string            `json:"cwd"`
	ResumeCmd []string          `json:"resume_cmd"`
}

func rowToProfile(row gen.LaunchProfile) profileOut {
	p := profileOut{ID: row.ID, Name: row.Name, Tool: row.Tool, Cwd: row.Cwd}
	_ = json.Unmarshal([]byte(row.Argv), &p.Argv)
	_ = json.Unmarshal([]byte(row.Env), &p.Env)
	if row.ResumeCmd != "" {
		_ = json.Unmarshal([]byte(row.ResumeCmd), &p.ResumeCmd)
	}
	return p
}

// profileIn is POST/PATCH input — every field optional on PATCH; the
// *_set flags record which fields the client actually sent (JSON arrays
// can't distinguish "absent" from "[]" with a plain struct on PATCH).
type profileIn struct {
	Name      *string            `json:"name"`
	Tool      *string            `json:"tool"`
	Argv      *[]string          `json:"argv"`
	Env       *map[string]string `json:"env"`
	Cwd       *string            `json:"cwd"`
	ResumeCmd *[]string          `json:"resume_cmd"`
}

func (s *Server) listProfiles(w http.ResponseWriter, r *http.Request) {
	rows, err := s.q.ListProfiles(r.Context())
	if err != nil {
		s.internalErr(w, err)
		return
	}
	out := make([]profileOut, 0, len(rows))
	for _, row := range rows {
		out = append(out, rowToProfile(row))
	}
	writeJSON(w, 200, out)
}

// validateProfile enforces the profile invariants: argv is required and
// non-empty (executable first — the session spawns it directly), tool
// defaults to argv[0].
func validateProfile(in profileIn) (name string, argv []string, envJSON, cwd, resumeJSON string, err error) {
	if in.Name == nil || strings.TrimSpace(*in.Name) == "" {
		return "", nil, "", "", "", errors.New("name required")
	}
	name = strings.TrimSpace(*in.Name)
	if len(name) > 128 {
		return "", nil, "", "", "", errors.New("name too long")
	}
	if in.Argv == nil || len(*in.Argv) == 0 || strings.TrimSpace((*in.Argv)[0]) == "" {
		return "", nil, "", "", "", errors.New("argv[0] executable required")
	}
	argv = *in.Argv
	if len(argv) > 64 {
		return "", nil, "", "", "", errors.New("argv too long")
	}
	for _, a := range argv {
		if len(a) > 4096 {
			return "", nil, "", "", "", errors.New("argv element too long")
		}
	}
	env := map[string]string{}
	if in.Env != nil {
		env = *in.Env
		if len(env) > 64 {
			return "", nil, "", "", "", errors.New("env too large")
		}
	}
	eb, _ := json.Marshal(env)
	envJSON = string(eb)
	if in.Cwd != nil {
		cwd = *in.Cwd
	}
	if in.ResumeCmd != nil && len(*in.ResumeCmd) > 0 {
		rb, _ := json.Marshal(*in.ResumeCmd)
		resumeJSON = string(rb)
	}
	return name, argv, envJSON, cwd, resumeJSON, nil
}

func (s *Server) createProfile(w http.ResponseWriter, r *http.Request) {
	var in profileIn
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, 400, "bad_request", "invalid JSON")
		return
	}
	name, argv, envJSON, cwd, resumeJSON, err := validateProfile(in)
	if err != nil {
		writeErr(w, 400, "bad_request", err.Error())
		return
	}
	tool := ""
	if in.Tool != nil {
		tool = strings.TrimSpace(*in.Tool)
	}
	if tool == "" {
		// Default the adapter binding to the binary basename — a path
		// argv[0] (/usr/bin/claude) must still resolve pins/context by
		// tool name ("claude").
		tool = filepath.Base(argv[0])
	}
	ab, _ := json.Marshal(argv)
	row, err := s.q.CreateProfile(r.Context(), gen.CreateProfileParams{
		Name: name, Tool: tool, Argv: string(ab), Env: envJSON,
		Cwd: cwd, ResumeCmd: resumeJSON,
	})
	if err != nil {
		s.internalErr(w, err)
		return
	}
	writeJSON(w, 201, rowToProfile(row))
}

func (s *Server) patchProfile(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		writeErr(w, 400, "bad_request", "profile id must be numeric")
		return
	}
	cur, err := s.q.GetProfile(r.Context(), id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeErr(w, 404, "profile_not_found", "no such profile")
			return
		}
		s.internalErr(w, err)
		return
	}
	var in profileIn
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, 400, "bad_request", "invalid JSON")
		return
	}
	// PATCH merges: fields the client didn't send keep their value.
	out := rowToProfile(cur)
	if in.Name != nil {
		out.Name = *in.Name
	}
	if in.Tool != nil {
		out.Tool = *in.Tool
	}
	if in.Argv != nil {
		out.Argv = *in.Argv
	}
	if in.Env != nil {
		out.Env = *in.Env
	}
	if in.Cwd != nil {
		out.Cwd = *in.Cwd
	}
	if in.ResumeCmd != nil {
		out.ResumeCmd = *in.ResumeCmd
	}
	merged := profileIn{
		Name: &out.Name, Tool: &out.Tool, Argv: &out.Argv,
		Env: &out.Env, Cwd: &out.Cwd, ResumeCmd: &out.ResumeCmd,
	}
	name, argv, envJSON, cwd, resumeJSON, err := validateProfile(merged)
	if err != nil {
		writeErr(w, 400, "bad_request", err.Error())
		return
	}
	ab, _ := json.Marshal(argv)
	if out.Tool == "" {
		out.Tool = filepath.Base(argv[0])
	}
	if err := s.q.UpdateProfile(r.Context(), gen.UpdateProfileParams{
		Name: name, Tool: out.Tool, Argv: string(ab), Env: envJSON,
		Cwd: cwd, ResumeCmd: resumeJSON, ID: id,
	}); err != nil {
		s.internalErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]string{"status": "ok"})
}

func (s *Server) deleteProfile(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		writeErr(w, 400, "bad_request", "profile id must be numeric")
		return
	}
	if _, err := s.q.GetProfile(r.Context(), id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeErr(w, 404, "profile_not_found", "no such profile")
			return
		}
		s.internalErr(w, err)
		return
	}
	if err := s.q.DeleteProfile(r.Context(), id); err != nil {
		s.internalErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]string{"status": "ok"})
}

// launch is POST /api/agents/launch — {profile_id, resume?, name?, cwd?,
// cols?, rows?} → broker → spawn with the profile's argv + env.
func (s *Server) launchProfile(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ProfileID int64  `json:"profile_id"`
		Resume    bool   `json:"resume"`
		Name      string `json:"name"`
		Cwd       string `json:"cwd"`
		Cols      int    `json:"cols"`
		Rows      int    `json:"rows"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, 400, "bad_request", "invalid JSON")
		return
	}
	row, err := s.q.GetProfile(r.Context(), body.ProfileID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeErr(w, 404, "profile_not_found", "no such profile")
			return
		}
		s.internalErr(w, err)
		return
	}
	prof := rowToProfile(row)
	if len(prof.Argv) == 0 {
		writeErr(w, 400, "bad_request", "profile has no argv")
		return
	}
	argv := prof.Argv
	if body.Resume && len(prof.ResumeCmd) > 0 {
		argv = prof.ResumeCmd
	}
	cwd := body.Cwd
	if cwd == "" {
		cwd = prof.Cwd
	}
	name := body.Name
	if name == "" {
		name = prof.Name
	}
	p := principal(r)
	op := broker.Op{
		Actor: p, Action: "agent.launch", Target: "local",
		Argv: argv, Risk: broker.RiskControlled,
	}
	if err := s.br.Check(op); err != nil {
		writeErr(w, 403, "action_restricted", "role may not launch agents")
		return
	}
	rec, err := s.mgr.Spawn(r.Context(), domain.SpawnRequest{
		Name: name, Cwd: cwd, Argv: argv, Env: prof.Env,
		Cols: body.Cols, Rows: body.Rows,
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
		"session": rec, "profile_id": prof.ID, "argv": argv,
	})
}

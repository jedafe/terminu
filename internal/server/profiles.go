// Ref: .teos/sections/05-agent-session-management.md (launch profiles).
// Saved per-tool launch configs — CRUD + broker-gated launch that spawns
// a session running the profile's argv (optionally inside a login
// shell), a named variant argv, or a bare shell session.
package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/jedafe/terminu/internal/broker"
	"github.com/jedafe/terminu/internal/db/gen"
	"github.com/jedafe/terminu/internal/domain"
	"github.com/jedafe/terminu/internal/session"
)

// profileVariant is a named alternate argv — the +new menu's
// sub-selection rows (e.g. devin → cloud: ["devin","--cloud"]).
type profileVariant struct {
	Name string   `json:"name"`
	Argv []string `json:"argv"`
}

// profileOut is the API shape — argv/resume_cmd/variants JSON arrays
// and env a JSON object (the table stores them as TEXT).
type profileOut struct {
	ID        int64             `json:"id"`
	Name      string            `json:"name"`
	Tool      string            `json:"tool"`
	Argv      []string          `json:"argv"`
	Env       map[string]string `json:"env"`
	Cwd       string            `json:"cwd"`
	ResumeCmd []string          `json:"resume_cmd"`
	Shell     string            `json:"shell"`
	Variants  []profileVariant  `json:"variants"`
}

func rowToProfile(row gen.LaunchProfile) profileOut {
	p := profileOut{ID: row.ID, Name: row.Name, Tool: row.Tool, Cwd: row.Cwd, Shell: row.Shell}
	_ = json.Unmarshal([]byte(row.Argv), &p.Argv)
	_ = json.Unmarshal([]byte(row.Env), &p.Env)
	if row.ResumeCmd != "" {
		_ = json.Unmarshal([]byte(row.ResumeCmd), &p.ResumeCmd)
	}
	if row.Variants != "" {
		_ = json.Unmarshal([]byte(row.Variants), &p.Variants)
	}
	return p
}

// profileIn is POST/PATCH input — every field optional on PATCH; the
// pointer fields record which fields the client actually sent (JSON
// arrays can't distinguish "absent" from "[]" with a plain struct on
// PATCH).
type profileIn struct {
	Name      *string            `json:"name"`
	Tool      *string            `json:"tool"`
	Argv      *[]string          `json:"argv"`
	Env       *map[string]string `json:"env"`
	Cwd       *string            `json:"cwd"`
	ResumeCmd *[]string          `json:"resume_cmd"`
	Shell     *string            `json:"shell"`
	Variants  *[]profileVariant  `json:"variants"`
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

var shellRe = regexp.MustCompile(`^[a-zA-Z0-9_/.-]{1,64}$`)

// validateProfile enforces the profile invariants: a launch path must
// exist (argv, a variant, or a shell session), tool defaults to
// argv[0] (falling back to the first variant's argv[0]).
func validateProfile(in profileIn) (name string, argv []string, envJSON, cwd, resumeJSON, shell, variantsJSON string, err error) {
	fail := func(e error) (string, []string, string, string, string, string, string, error) {
		return "", nil, "", "", "", "", "", e
	}
	if in.Name == nil || strings.TrimSpace(*in.Name) == "" {
		return fail(errors.New("name required"))
	}
	name = strings.TrimSpace(*in.Name)
	if len(name) > 128 {
		return fail(errors.New("name too long"))
	}
	argv = nil
	if in.Argv != nil {
		argv = *in.Argv
	}
	variants := []profileVariant{}
	if in.Variants != nil {
		variants = *in.Variants
	}
	if in.Shell != nil {
		shell = strings.TrimSpace(*in.Shell)
	}
	if shell != "" && !shellRe.MatchString(shell) {
		return fail(errors.New("shell must be a path or name like /bin/zsh"))
	}
	// A launch path is required: argv, one variant, or a shell.
	if len(argv) == 0 && shell == "" && len(variants) == 0 {
		return fail(errors.New("argv[0] executable required"))
	}
	if len(argv) > 0 && strings.TrimSpace(argv[0]) == "" {
		return fail(errors.New("argv[0] executable required"))
	}
	if len(argv) > 64 {
		return fail(errors.New("argv too long"))
	}
	for _, a := range argv {
		if len(a) > 4096 {
			return fail(errors.New("argv element too long"))
		}
	}
	if len(variants) > 8 {
		return fail(errors.New("too many variants"))
	}
	for _, v := range variants {
		if strings.TrimSpace(v.Name) == "" || len(v.Name) > 64 {
			return fail(errors.New("variant name required (≤64 chars)"))
		}
		if len(v.Argv) == 0 || strings.TrimSpace(v.Argv[0]) == "" {
			return fail(errors.New("variant " + v.Name + ": argv[0] required"))
		}
		if len(v.Argv) > 64 {
			return fail(errors.New("variant " + v.Name + ": argv too long"))
		}
		for _, a := range v.Argv {
			if len(a) > 4096 {
				return fail(errors.New("variant " + v.Name + ": argv element too long"))
			}
		}
	}
	variantsJSON = "[]"
	if len(variants) > 0 {
		vb, _ := json.Marshal(variants)
		variantsJSON = string(vb)
	}
	env := map[string]string{}
	if in.Env != nil {
		env = *in.Env
		if len(env) > 64 {
			return fail(errors.New("env too large"))
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
	return name, argv, envJSON, cwd, resumeJSON, shell, variantsJSON, nil
}

func (s *Server) createProfile(w http.ResponseWriter, r *http.Request) {
	var in profileIn
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, 400, "bad_request", "invalid JSON")
		return
	}
	name, argv, envJSON, cwd, resumeJSON, shell, variantsJSON, err := validateProfile(in)
	if err != nil {
		writeErr(w, 400, "bad_request", err.Error())
		return
	}
	tool := ""
	if in.Tool != nil {
		tool = strings.TrimSpace(*in.Tool)
	}
	if tool == "" && len(argv) > 0 {
		// Default the adapter binding to the binary basename — a path
		// argv[0] (/usr/bin/claude) must still resolve pins/context by
		// tool name ("claude").
		tool = filepath.Base(argv[0])
	}
	// Variant-only profile (e.g. a shell session + named argvs): the
	// first variant's executable is the tool stand-in.
	if tool == "" && in.Variants != nil && len(*in.Variants) > 0 && len((*in.Variants)[0].Argv) > 0 {
		tool = filepath.Base((*in.Variants)[0].Argv[0])
	}
	if argv == nil {
		argv = []string{} // column stores a JSON array — never "null"
	}
	ab, _ := json.Marshal(argv)
	row, err := s.q.CreateProfile(r.Context(), gen.CreateProfileParams{
		Name: name, Tool: tool, Argv: string(ab), Env: envJSON,
		Cwd: cwd, ResumeCmd: resumeJSON, Shell: shell, Variants: variantsJSON,
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
	if in.Shell != nil {
		out.Shell = *in.Shell
	}
	if in.Variants != nil {
		out.Variants = *in.Variants
	}
	merged := profileIn{
		Name: &out.Name, Tool: &out.Tool, Argv: &out.Argv,
		Env: &out.Env, Cwd: &out.Cwd, ResumeCmd: &out.ResumeCmd,
		Shell: &out.Shell, Variants: &out.Variants,
	}
	name, argv, envJSON, cwd, resumeJSON, shell, variantsJSON, err := validateProfile(merged)
	if err != nil {
		writeErr(w, 400, "bad_request", err.Error())
		return
	}
	if argv == nil {
		argv = []string{} // column stores a JSON array — never "null"
	}
	ab, _ := json.Marshal(argv)
	if out.Tool == "" && len(argv) > 0 {
		out.Tool = filepath.Base(argv[0])
	}
	if out.Tool == "" && len(out.Variants) > 0 && len(out.Variants[0].Argv) > 0 {
		out.Tool = filepath.Base(out.Variants[0].Argv[0])
	}
	if err := s.q.UpdateProfile(r.Context(), gen.UpdateProfileParams{
		Name: name, Tool: out.Tool, Argv: string(ab), Env: envJSON,
		Cwd: cwd, ResumeCmd: resumeJSON, Shell: shell, Variants: variantsJSON, ID: id,
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

var errVariantNotFound = errors.New("variant_not_found")

// resolveLaunchArgv picks the argv a launch runs: variant > resume >
// default argv; an empty result means shell-only spawn (wrap carries
// the shell either way — the manager no-ops it without argv).
func resolveLaunchArgv(p profileOut, variant string, resume bool) (argv []string, wrap string, err error) {
	wrap = p.Shell
	switch {
	case variant != "":
		for _, v := range p.Variants {
			if v.Name == variant {
				return v.Argv, wrap, nil
			}
		}
		return nil, "", errVariantNotFound
	case resume && len(p.ResumeCmd) > 0:
		return p.ResumeCmd, wrap, nil
	default:
		return p.Argv, wrap, nil
	}
}

// launch is POST /api/agents/launch — {profile_id, resume?, variant?,
// name?, cwd?, cols?, rows?} → broker → spawn with the profile's
// resolved argv + env (inside the profile's login shell when set), or
// a bare shell session for argv-less profiles.
func (s *Server) launchProfile(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ProfileID int64  `json:"profile_id"`
		Resume    bool   `json:"resume"`
		Variant   string `json:"variant"`
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
	argv, wrap, err := resolveLaunchArgv(prof, body.Variant, body.Resume)
	if err != nil {
		writeErr(w, 404, "variant_not_found", "no such profile variant")
		return
	}
	if len(argv) == 0 && prof.Shell == "" {
		writeErr(w, 400, "bad_request", "profile has no argv")
		return
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
	if len(op.Argv) == 0 {
		// Shell-only launch — the audit row names the program honestly.
		op.Argv = []string{prof.Shell}
	}
	if err := s.br.Check(op); err != nil {
		writeErr(w, 403, "action_restricted", "role may not launch agents")
		return
	}
	req := domain.SpawnRequest{
		Name: name, Cwd: cwd, Env: prof.Env,
		Cols: body.Cols, Rows: body.Rows,
	}
	if len(argv) > 0 {
		req.Argv = argv
		req.Wrap = wrap
	} else {
		req.Shell = prof.Shell
	}
	rec, err := s.mgr.Spawn(r.Context(), req)
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
		if errors.Is(err, session.ErrExecutable) {
			writeErr(w, 400, "bad_request", err.Error())
			return
		}
		s.internalErr(w, err)
		return
	}
	writeJSON(w, 201, map[string]any{
		"session": rec, "profile_id": prof.ID, "argv": argv,
	})
}

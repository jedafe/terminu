// Ref: .teos/sections/04 — auto-discovery: --help parsing on targets
// produces provisional `discovered` manifests. They ride the same
// schema, shadowed by any builtin/custom definition of the same id.
package registry

import (
	"context"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// discoveryTimeout bounds one --help scrape — a hanging tool can't stall
// the request path (detection-infra class, like the ps scrape).
const discoveryTimeout = 4 * time.Second

// discoverySubCap keeps a pathological help dump from flooding the
// registry — top subcommands only.
const discoverySubCap = 30

// Discover runs `{exe} --help` on the local target, parses its
// subcommand list, and writes provisional `discovered` manifests — one
// per subcommand. Returns the manifests written this run.
// `exe` is required: discovery is per-tool and invoked explicitly
// (GET /api/discovered?exe=).
func (r *Registry) Discover(ctx context.Context, exe string) ([]*Manifest, error) {
	if exe == "" || strings.ContainsAny(exe, "/\\ \t") {
		return nil, ErrInvalidManifest // bare binary names only
	}
	subs, err := scrapeHelp(ctx, exe)
	if err != nil {
		return nil, err
	}
	var out []*Manifest
	for _, s := range subs {
		m := &Manifest{
			ID:          exe + "." + s.name,
			Application: exe,
			Name:        exe + " " + s.name,
			Description: s.desc,
			Command:     CommandSpec{Executable: exe, Arguments: []string{s.name}},
			Execution:   ExecSpec{Risk: "controlled", Confirmation: true},
			Context:     ContextSpec{Processes: []string{exe}},
			Source:      "discovered",
			Tags:        []string{exe, "discovered"},
		}
		if err := m.Validate(); err != nil {
			continue // a weird subcommand token is skipped, not fatal
		}
		if err := r.upsert(ctx, m); err != nil {
			return out, err
		}
		out = append(out, m)
	}
	if err := r.resolve(ctx); err != nil {
		return out, err
	}
	return out, nil
}

// ListDiscovered returns the registry's discovered-source rows.
func (r *Registry) ListDiscovered() []*Manifest {
	var out []*Manifest
	for _, m := range r.List() {
		if m.Source == "discovered" {
			out = append(out, m)
		}
	}
	return out
}

type subcmd struct{ name, desc string }

// sectionRe matches a subcommand listing header — "Commands:",
// "Available Commands:", "Subcommands:", etc.
var sectionRe = regexp.MustCompile(`(?i)^\s*(available\s+)?(sub)?commands\s*:`)

// subRe matches "  name   description" inside a commands section —
// a leading indent, the subcommand token, then 2+ spaces of gap.
var subRe = regexp.MustCompile(`^\s{1,}([a-zA-Z][\w:.-]*)\s{2,}(.+)$`)

// scrapeHelp runs exe --help and extracts its subcommand table.
// Env scrubbing matches the session spawn path — daemon config never
// leaks into a child (TU_* values are deployment secrets).
func scrapeHelp(ctx context.Context, exe string) ([]subcmd, error) {
	c, cancel := context.WithTimeout(ctx, discoveryTimeout)
	defer cancel()
	cmd := exec.CommandContext(c, exe, "--help")
	env := make([]string, 0, len(os.Environ()))
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "TU_") {
			continue
		}
		env = append(env, e)
	}
	cmd.Env = env
	out, err := cmd.Output()
	if err != nil {
		// --help commonly exits non-zero; a partial stdout still parses.
		if len(out) == 0 {
			return nil, err
		}
	}
	return parseHelp(string(out)), nil
}

// parseHelp finds the commands section and reads its rows. Without a
// section header, "Usage: exe <command>" style tools yield nothing —
// provisional discovery is best-effort, never fabricated.
func parseHelp(text string) []subcmd {
	var out []subcmd
	inSection := false
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimRight(line, " \t")
		if sectionRe.MatchString(trimmed) {
			inSection = true
			continue
		}
		if !inSection {
			continue
		}
		if trimmed == "" {
			continue // blank lines inside sections are common
		}
		m := subRe.FindStringSubmatch(line)
		if m == nil {
			// A non-indented line ends the section (new flag group etc).
			if len(line) > 0 && line[0] != ' ' && line[0] != '\t' {
				inSection = false
			}
			continue
		}
		name := strings.ReplaceAll(strings.ReplaceAll(m[1], ":", "."), " ", "")
		if name == "" || strings.HasPrefix(name, "-") {
			continue
		}
		out = append(out, subcmd{name: name, desc: strings.TrimSpace(m[2])})
		if len(out) >= discoverySubCap {
			break
		}
	}
	return out
}

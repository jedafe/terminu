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

// discoveryFlagCap is the flag-table analogue — a flag-shaped tool's
// surface capped the same way, independently of subcommands.
const discoveryFlagCap = 30

// discoverCooldown lets a failed/empty auto-scrape retry later — a
// binary installed or fixed mid-process surfaces on the next context
// load once the window passes. Successful scrapes still only run once:
// their manifests exist, so the caller never re-enters.
const discoverCooldown = 15 * time.Minute

// Discover runs `{exe} --help` on the local target, parses its
// subcommand list + flag table, and writes provisional `discovered`
// manifests — one per subcommand and one per flag. Returns the
// manifests written this run.
// `exe` is required: discovery is per-tool and invoked explicitly
// (GET /api/discovered?exe=).
func (r *Registry) Discover(ctx context.Context, exe string) ([]*Manifest, error) {
	if exe == "" || strings.ContainsAny(exe, "/\\ \t") {
		return nil, ErrInvalidManifest // bare binary names only
	}
	subs, flags, err := scrapeHelp(ctx, exe)
	if err != nil {
		return nil, err
	}
	// A successful scrape replaces the exe's provisional set wholesale —
	// subs dropped from --help since the last run leave no stale rows.
	// On scrape failure the old set survives (better stale than empty).
	if err := r.q.DeleteDiscoveredManifests(ctx, exe); err != nil {
		return nil, err
	}
	var out []*Manifest
	for _, s := range subs {
		m := &Manifest{
			// IDs are canonical dotted-lowercase (idRe) — a CamelCase
			// sub still validates; argv keeps the real-case token.
			ID:          strings.ToLower(exe + "." + s.name),
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
	for _, f := range flags {
		m := &Manifest{
			// Flag manifests live in a .flag. id segment — never
			// shadow a subcommand of the same name; argv keeps the
			// canonical (long-form when listed) token.
			ID:          strings.ToLower(exe + ".flag." + strings.TrimLeft(f.name, "-")),
			Application: exe,
			Name:        exe + " " + f.disp,
			Description: f.desc,
			Command:     CommandSpec{Executable: exe, Arguments: []string{f.name}},
			Execution:   ExecSpec{Risk: "controlled", Confirmation: true},
			Context:     ContextSpec{Processes: []string{exe}},
			Source:      "discovered",
			Tags:        []string{exe, "discovered", "flag"},
		}
		if err := m.Validate(); err != nil {
			continue // a weird flag token is skipped, not fatal
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

// DiscoverOnce is the auto-discovery path (§04): the context route
// calls it when a session's foreground process has zero manifests —
// dedup'd per exe so a binary that hangs/fails --help can't be
// re-scraped by every palette open; a failed or empty attempt can
// retry once discoverCooldown passes (install-mid-run case).
// ran=false means the exe was attempted inside the window (skip audit).
func (r *Registry) DiscoverOnce(ctx context.Context, exe string) (ms []*Manifest, ran bool, err error) {
	r.appsMu.Lock()
	if at, ok := r.tried[exe]; ok && time.Since(at) < discoverCooldown {
		r.appsMu.Unlock()
		return nil, false, nil
	}
	r.tried[exe] = time.Now()
	r.appsMu.Unlock()
	ms, err = r.Discover(ctx, exe)
	return ms, true, err
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

// subcmd is one parsed help row — name is the canonical token (sub
// name, or the long-form flag for flag rows), disp is the display spec
// (e.g. "-v, --verbose"), desc the trailing description.
type subcmd struct{ name, disp, desc string }

// sectionRe matches a subcommand listing header — "Commands:",
// "Available Commands:", "Subcommands:", etc.
var sectionRe = regexp.MustCompile(`(?i)^\s*(available\s+)?(sub)?commands\s*:`)

// subRe matches "  name   description" inside a commands section —
// a leading indent, the subcommand token, then 2+ spaces of gap.
var subRe = regexp.MustCompile(`^\s{1,}([a-zA-Z][\w:.-]*)\s{2,}(.+)$`)

// twoSpaceRe splits help-table columns: a flag spec and its description
// are always separated by ≥2 spaces of column padding — a stray
// "  rm -rf /" example line (single spaces) can't false-match.
var twoSpaceRe = regexp.MustCompile(`\s{2,}`)

// flagTokRe is one flag token — -v, --verbose, optionally a value
// placeholder (=FILE, <FILE>, [FILE], or a bare word).
var flagTokRe = regexp.MustCompile(`^-{1,2}[A-Za-z0-9][\w.-]*([ =](?:[^\s,]+))?$`)

// parseFlagLine reads "  -v, --verbose   verbose output" — up to two
// comma-separated flag tokens, column-gap, description. Canonical argv
// is the last token (GNU lists prefer the long form); the display spec
// keeps both bare flags minus value placeholders.
func parseFlagLine(line string) (disp, canon, desc string, ok bool) {
	// TrimLeft first — the leading indent is itself a ≥2-space run and
	// would otherwise split the row at column 0.
	line = strings.TrimLeft(line, " \t")
	m := twoSpaceRe.FindStringIndex(line)
	if m == nil {
		return "", "", "", false
	}
	spec := strings.TrimSpace(line[:m[0]])
	desc = strings.TrimSpace(line[m[1]:])
	if spec == "" || desc == "" {
		return "", "", "", false
	}
	toks := strings.Split(spec, ",")
	if len(toks) > 2 {
		return "", "", "", false
	}
	dispToks := make([]string, 0, len(toks))
	for _, t := range toks {
		t = strings.TrimSpace(t)
		if !flagTokRe.MatchString(t) {
			return "", "", "", false
		}
		bare := t
		if i := strings.IndexAny(bare, " ="); i >= 0 {
			bare = bare[:i]
		}
		dispToks = append(dispToks, bare)
	}
	canon = dispToks[len(dispToks)-1]
	return strings.Join(dispToks, ", "), canon, desc, true
}

// scrapeHelp runs exe --help and extracts its subcommand table + flag
// rows. Env scrubbing matches the session spawn path — daemon config
// never leaks into a child (TU_* values are deployment secrets).
func scrapeHelp(ctx context.Context, exe string) (subs, flags []subcmd, err error) {
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
			return nil, nil, err
		}
	}
	subs, flags = parseHelp(string(out))
	return subs, flags, nil
}

// parseHelp finds the commands section and reads its rows, and picks
// up flag-table rows anywhere in the doc — flag-shaped tools (ls,
// grep) express their surface as options, not subcommands. Provisional
// discovery is best-effort, never fabricated.
func parseHelp(text string) (subs, flags []subcmd) {
	inSection := false
	flagSeen := map[string]bool{}
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimRight(line, " \t")
		if sectionRe.MatchString(trimmed) {
			inSection = true
			continue
		}
		if inSection {
			if trimmed == "" {
				continue // blank lines inside sections are common
			}
			m := subRe.FindStringSubmatch(line)
			if m == nil {
				// A non-indented line ends the section (new flag
				// group etc) — indented non-sub rows still fall
				// through to the flag scan below.
				if len(line) > 0 && line[0] != ' ' && line[0] != '\t' {
					inSection = false
				}
			} else {
				name := strings.ReplaceAll(strings.ReplaceAll(m[1], ":", "."), " ", "")
				if name != "" && !strings.HasPrefix(name, "-") {
					subs = append(subs, subcmd{name: name, disp: name, desc: strings.TrimSpace(m[2])})
				}
				continue
			}
		}
		if len(flags) < discoveryFlagCap {
			if disp, canon, desc, ok := parseFlagLine(line); ok && !flagSeen[canon] {
				flagSeen[canon] = true
				flags = append(flags, subcmd{name: canon, disp: disp, desc: desc})
			}
		}
		if len(subs) >= discoverySubCap && len(flags) >= discoveryFlagCap {
			break
		}
	}
	return subs, flags
}

// Ref: .teos/sections/04 — auto-discovery: --help parsing on targets
// produces provisional `discovered` manifests. They ride the same
// schema, shadowed by any builtin/custom definition of the same id.
package registry

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/jedafe/terminu/internal/db/gen"
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
	path := r.ResolveExe(exe)
	if path == "" {
		return nil, errors.New("no such binary: " + exe)
	}
	subs, flags, err := scrapeHelp(ctx, path)
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
// "Available Commands:", "Basic Commands:" (kubectl), "Subcommands:",
// "The commands are:" (go)… Colon-terminated only, so prose like
// "see go help commands" can't open a section.
var sectionRe = regexp.MustCompile(`(?i)^\s*[\w ]*commands\b(\s+(are|available|list))?\s*:\s*$`)

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

// helpArgs is the argv chain tried in order — many CLIs expose help
// under a different flag or subcommand (ssh: -h, docker-style: help).
var helpArgs = []string{"--help", "-h", "help"}

// scrapeHelp runs exe's help form and extracts its subcommand table +
// flag rows. CombinedOutput because most CLIs print usage to *stderr*
// (ssh, ffmpeg, java, openssl...) — stdout-only capture was the top
// "could not read --help" cause. A run that parses to zero rows falls
// through to the next argv form (ls help errors with output; it isn't
// a help screen). Env scrubbing matches the session spawn path —
// daemon config never leaks into a child (TU_* is deployment secret).
func scrapeHelp(ctx context.Context, exe string) (subs, flags []subcmd, err error) {
	env := make([]string, 0, len(os.Environ()))
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "TU_") {
			continue
		}
		env = append(env, e)
	}
	var lastErr error
	for _, arg := range helpArgs {
		c, cancel := context.WithTimeout(ctx, discoveryTimeout)
		cmd := exec.CommandContext(c, exe, arg)
		cmd.Env = env
		out, runErr := cmd.CombinedOutput()
		cancel()
		if runErr != nil {
			lastErr = runErr
		}
		if len(out) == 0 {
			continue
		}
		subs, flags = parseHelp(string(out))
		if len(subs)+len(flags) > 0 {
			return subs, flags, nil
		}
	}
	if lastErr == nil {
		lastErr = errors.New("no parseable help output")
	}
	return nil, nil, lastErr
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

// ---------- slash-command detection ----------
//
// An agent's in-TUI slash surface (/help, /add…) can't come from
// --help: it lives inside the session's rendered screen and the
// agent's command dirs. Detection observes both (ADR-007 — read-only;
// the screen is never written to) and persists the set as `discovered`
// manifests — the same shape as the builtin slash docs they replace,
// so context matching and the palette's slash rows work unchanged.
// A re-detect rewrites the set only when it actually changed (grow OR
// shrink); empty evidence keeps the cached set — an agent screen that
// simply isn't showing its menu right now isn't "zero commands".

// slashMinCluster — the screen-parse trust gate. A real menu or /help
// list produces a cluster of /rows; a stray "/etc/passwd" or one typed
// "/foo" doesn't. Below this, screen evidence is ignored entirely.
const slashMinCluster = 3

// slashNameRe validates one command name — letters/digits/-/_, plus
// `:` for namespaced commands (normalized to `.` in the manifest id).
var slashNameRe = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9:_-]{0,40}$`)

// slashBound is the set allowed on either side of a `/name` token —
// whitespace plus the glyphs TUI menus draw as borders and selectors.
const slashBound = " \t│┃|┆┊>›·*❯"

// slashAnchorRe matches a row *starting* with the command (optional
// indent/border glyph, `/name`, optional gap + description, optional
// trailing border). Bare `/name` tokens must anchor — a mid-line bare
// `/x` is usually a path argument, not a menu row.
var slashAnchorRe = regexp.MustCompile(`^\s{0,10}[>›·*❯│┃|┆┊]?\s*/([a-zA-Z][a-zA-Z0-9:_-]*)(?:\s{2,}([^│┃|┆┊]*?))?\s*[│┃|┆┊]?\s*$`)

func isSlashNameChar(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' ||
		c >= '0' && c <= '9' || c == '-' || c == '_' || c == ':'
}

// ParseSlashCandidates extracts slash commands from rendered screen
// lines — an open `/` menu, a `/help` listing, or similar rows. Two
// shapes count: anchored rows (bare or desc'd) and mid-line tokens
// that carry a description (menus draw right of the input area). A
// bare mid-line `/x` never counts — `ls /a /b /c` can't fake a menu.
// Returns nil under slashMinCluster distinct names (noise guard).
func ParseSlashCandidates(lines []string) map[string]string {
	out := map[string]string{}
	add := func(name, desc string) {
		desc = strings.TrimRight(strings.TrimSpace(desc), slashBound+" ")
		if _, seen := out[strings.ToLower(name)]; !seen {
			out[strings.ToLower(name)] = desc
		}
	}
	for _, line := range lines {
		if m := slashAnchorRe.FindStringSubmatch(line); m != nil {
			add(m[1], m[2])
			continue
		}
		for i := 0; i < len(line); i++ {
			if line[i] != '/' {
				continue
			}
			if i > 0 && !strings.ContainsRune(slashBound, rune(line[i-1])) {
				continue // x/foo — not a command token
			}
			j := i + 1
			for j < len(line) && isSlashNameChar(line[j]) {
				j++
			}
			name := line[i+1 : j]
			if !slashNameRe.MatchString(name) {
				continue
			}
			rest := line[j:]
			if gap := len(rest) - len(strings.TrimLeft(rest, " \t")); gap < 2 {
				continue // bare mid-line token — path, not a menu row
			}
			if strings.TrimSpace(rest) == "" {
				continue
			}
			add(name, rest)
		}
	}
	if len(out) < slashMinCluster {
		return nil
	}
	return out
}

// slashDirExts are the file types agents use for custom commands —
// a stray non-command file in the dir won't join the set.
var slashDirExts = map[string]bool{".md": true, ".toml": true, ".txt": true, ".yaml": true, ".json": true}

// SlashCommandDirs lists the dirs scanned for an agent's user-defined
// slash commands — the agent's home config dir plus the session's
// project-level equivalent under cwd. `commands`/`prompts` dirs hold
// one file per command; `skills` dirs hold one subdir per command with
// a SKILL.md carrying its name/description frontmatter (devin, claude).
// Missing dirs are skipped.
func SlashCommandDirs(app, home, cwd string) []string {
	var dirs []string
	add := func(d string) {
		if d != "" {
			dirs = append(dirs, d)
		}
	}
	// Per-agent conventions, then generic `.{app}/{commands,skills}`.
	switch app {
	case "claude":
		add(filepath.Join(home, ".claude", "commands"))
		add(filepath.Join(home, ".claude", "skills"))
		add(filepath.Join(cwd, ".claude", "commands"))
		add(filepath.Join(cwd, ".claude", "skills"))
	case "gemini":
		add(filepath.Join(home, ".gemini", "commands"))
		add(filepath.Join(cwd, ".gemini", "commands"))
	case "codex":
		add(filepath.Join(home, ".codex", "prompts"))
		add(filepath.Join(cwd, ".codex", "prompts"))
	case "opencode":
		add(filepath.Join(home, ".config", "opencode", "command"))
		add(filepath.Join(home, ".opencode", "command"))
		add(filepath.Join(cwd, ".opencode", "command"))
	case "devin":
		add(filepath.Join(home, ".agents", "skills"))
		add(filepath.Join(home, ".config", "devin", "skills"))
		add(filepath.Join(home, ".config", "cognition", "skills"))
		add(filepath.Join(cwd, ".devin", "skills"))
		add(filepath.Join(cwd, ".cognition", "skills"))
		add(filepath.Join(cwd, ".agents", "skills"))
	}
	add(filepath.Join(home, "."+app, "commands"))
	add(filepath.Join(home, "."+app, "skills"))
	add(filepath.Join(cwd, "."+app, "commands"))
	add(filepath.Join(cwd, "."+app, "skills"))
	return dirs
}

// ScanSlashDir reads one command dir. Two file shapes: a recognized
// file is a command named by its path (`dir:name` namespacing), and a
// `SKILL.md` inside a subdir is one command — the skill's frontmatter
// `name:`/`description:` describe it, falling back to the subdir name.
func ScanSlashDir(dir string) map[string]string {
	out := map[string]string{}
	root := filepath.Clean(dir)
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		base := d.Name()
		ext := filepath.Ext(base)
		if !slashDirExts[strings.ToLower(ext)] || strings.HasPrefix(base, ".") {
			return nil
		}
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil {
			return nil
		}
		parent := filepath.Dir(rel)
		// SKILL.md → the subdir IS the command; frontmatter names it.
		if strings.EqualFold(base, "SKILL.md") && parent != "." {
			name, desc := skillMeta(p)
			if name == "" {
				name = strings.ReplaceAll(parent, string(filepath.Separator), ":")
			}
			if slashNameRe.MatchString(name) {
				out[strings.ToLower(name)] = desc
			}
			return nil
		}
		name := strings.TrimSuffix(base, ext)
		if parent != "." {
			name = strings.ReplaceAll(parent, string(filepath.Separator), ":") + ":" + name
		}
		if slashNameRe.MatchString(name) {
			out[strings.ToLower(name)] = "user command — " + filepath.Join(dir, base)
		}
		return nil
	})
	return out
}

// skillMeta reads name:/description: from a SKILL.md's YAML
// frontmatter — first --- block only, flat keys only.
func skillMeta(path string) (name, desc string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", ""
	}
	text := string(data)
	if !strings.HasPrefix(text, "---") {
		return "", ""
	}
	end := strings.Index(text[3:], "\n---")
	if end < 0 {
		return "", ""
	}
	for _, line := range strings.Split(text[3:3+end], "\n") {
		k, v, ok := strings.Cut(line, ":")
		if !ok || strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
			continue
		}
		v = strings.Trim(strings.TrimSpace(v), `"'`)
		switch strings.TrimSpace(k) {
		case "name":
			name = v
		case "description":
			desc = v
		}
	}
	return name, desc
}

// SlashDocDirs lists an agent's bundled doc roots whose command
// tables enumerate built-in slash commands — the only file-backed
// source for TUI commands that aren't skills and have no `list`
// argv (devin's essential-commands.mdx). `current` wins; without
// it every installed version's docs merge (names dedup anyway).
func SlashDocDirs(app, home string) []string {
	var roots []string
	switch app {
	case "devin":
		cur := filepath.Join(home, ".local/share/devin/cli/_versions/current/share/devin/docs")
		if _, err := os.Stat(cur); err == nil {
			return []string{cur}
		}
		matches, _ := filepath.Glob(filepath.Join(home, ".local/share/devin/cli/_versions/*/share/devin/docs"))
		roots = append(roots, matches...)
	}
	return roots
}

// slashDocExts are the doc file types scanned for command tables.
var slashDocExts = map[string]bool{".md": true, ".mdx": true}

// slashDocTokRe reads a backticked `/name` token inside a table cell.
// The tail inside the backticks is kept so arg specs (` <name>`,
// ` --all`) don't produce fake commands — and a `/`-leading tail
// rejects doc-link paths like `/cli/reference/foo`.
var slashDocTokRe = regexp.MustCompile(`\x60/([a-zA-Z][a-zA-Z0-9:_-]*)([^\x60]*)\x60`)

// mdLinkRe strips markdown links to their label — `[text](url)` →
// `text` — so a desc doesn't carry URL noise.
var mdLinkRe = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)

// ScanSlashDocDir reads markdown doc files for slash-command table
// rows — `| `/name` | desc |`. Every `/name` token in the row maps
// to its description, so `/exit` or `/quit` and `aliases:` mentions
// join the set too. Non-table lines (prose) don't count.
func ScanSlashDocDir(dir string) map[string]string {
	out := map[string]string{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	for _, e := range entries {
		if e.IsDir() || !slashDocExts[strings.ToLower(filepath.Ext(e.Name()))] {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			t := strings.TrimSpace(line)
			if !strings.HasPrefix(t, "|") {
				continue
			}
			cells := strings.Split(t, "|")
			if len(cells) < 3 {
				continue
			}
			// Desc = the second cell (name cell is first); tokens in
			// BOTH cells map to it — alias spellings included.
			desc := strings.TrimSpace(mdLinkRe.ReplaceAllString(cells[2], "$1"))
			desc = strings.ReplaceAll(desc, "`", "")
			for _, c := range cells[1:3] {
				for _, m := range slashDocTokRe.FindAllStringSubmatch(c, -1) {
					name, rest := m[1], m[2]
					if strings.HasPrefix(rest, "/") {
						continue // a doc-link path, not a command
					}
					if _, seen := out[name]; !seen {
						out[name] = desc
					}
				}
			}
		}
	}
	return out
}

// slashProbes are argv that make an agent binary report its own slash
// surface — the agent's own enumeration beats filesystem conventions,
// and covers built-in commands that have no file on disk. Only agents
// with a documented non-interactive list command get an entry.
var slashProbes = map[string][]string{
	"devin": {"skills", "list"},
}

// SlashProbeArgv returns the argv the agent's binary understands as
// "list your slash commands", or nil for agents with no such probe.
func SlashProbeArgv(app string) []string {
	return slashProbes[app]
}

// slashListRowRe reads `  /name [tags] (location) - description` —
// the `devin skills list` row shape. The leading `/` marks a
// user-invocable command (model-only skills are listed bare).
var slashListRowRe = regexp.MustCompile(`^\s{0,4}/([a-zA-Z][a-zA-Z0-9:_-]*)\s*(?:\[[^\]]*\])?\s*(?:\([^)]*\))?\s*-\s*(.*)$`)

// slashListBareRe reads a bare `/name` row in a list output.
var slashListBareRe = regexp.MustCompile(`^\s{0,4}/([a-zA-Z][a-zA-Z0-9:_-]*)\s*$`)

// ParseSlashListOutput extracts slash commands from an agent's own
// list output (`devin skills list`). Only `/`-prefixed rows count —
// bare names are model-triggered skills, not user commands.
func ParseSlashListOutput(text string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		if m := slashListRowRe.FindStringSubmatch(line); m != nil {
			if _, seen := out[m[1]]; !seen {
				out[m[1]] = strings.TrimSpace(m[2])
			}
			continue
		}
		if m := slashListBareRe.FindStringSubmatch(line); m != nil {
			if _, seen := out[m[1]]; !seen {
				out[m[1]] = ""
			}
		}
	}
	return out
}

// ProbeSlash runs the agent's own list argv and parses its output —
// broker-gated by the caller, TU_* env-scrubbed like scrapeHelp, run
// under the session cwd so project-level skills resolve. Returns nil
// when the app has no probe or the run yielded nothing.
func (r *Registry) ProbeSlash(ctx context.Context, app, cwd string) map[string]string {
	argv := slashProbes[app]
	if argv == nil {
		return nil
	}
	exe := r.ResolveExe(app)
	if exe == "" {
		return nil
	}
	cctx, cancel := context.WithTimeout(ctx, discoveryTimeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, exe, argv...)
	env := make([]string, 0, len(os.Environ()))
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "TU_") {
			env = append(env, e)
		}
	}
	cmd.Env = env
	if cwd != "" {
		cmd.Dir = cwd
	}
	out, _ := cmd.CombinedOutput()
	cmds := ParseSlashListOutput(string(out))
	if len(cmds) == 0 {
		return nil
	}
	return cmds
}

// DiscoverSlash persists a detected slash-command set for an agent as
// `discovered` manifests — `app.slash.{name}`, context-matched on the
// agent's process name so they surface exactly like the curated docs
// they replace. Returns changed=true only when the stored set differed;
// an empty candidate map is a no-op (keeps the cached set — absence of
// on-screen evidence isn't absence of commands).
func (r *Registry) DiscoverSlash(ctx context.Context, app string, cmds map[string]string) (bool, error) {
	if app == "" || strings.ContainsAny(app, "/\\ \t") {
		return false, ErrInvalidManifest
	}
	if len(cmds) == 0 {
		return false, nil
	}
	want := map[string]string{}
	for name, desc := range cmds {
		name = strings.ToLower(strings.TrimLeft(name, "/"))
		if !slashNameRe.MatchString(name) {
			continue
		}
		// Default now, not at write time — the stored set compares
		// post-normalization or an empty desc never diffs clean.
		if desc == "" {
			desc = "detected slash command"
		}
		want[name] = desc
	}
	// Diff against the app's current discovered-slash rows — the set
	// rewrites only when names or descriptions changed (grow/shrink),
	// leaving {app}.flag.* and {app}.{sub} rows untouched either way.
	cur := map[string]string{}
	for _, m := range r.ListDiscovered() {
		pfx := app + ".slash."
		if m.Application == app && strings.HasPrefix(m.ID, pfx) {
			cur[strings.TrimPrefix(m.ID, pfx)] = m.Description
		}
	}
	same := len(cur) == len(want)
	for name, desc := range want {
		if same && (cur[name] != desc) {
			same = false
		}
		if !same {
			break
		}
	}
	if same {
		return false, nil
	}
	if err := r.q.DeleteDiscoveredSlashManifests(ctx, gen.DeleteDiscoveredSlashManifestsParams{
		Application: app,
		ID:          app + ".slash.%",
	}); err != nil {
		return false, err
	}
	for name, desc := range want {
		m := &Manifest{
			ID:          app + ".slash." + strings.ReplaceAll(name, ":", "."),
			Application: app,
			Name:        "/" + name,
			Description: desc,
			Command:     CommandSpec{Executable: "/" + name, Arguments: []string{}},
			Execution:   ExecSpec{Risk: "informational"},
			Context:     ContextSpec{Processes: []string{app}},
			Source:      "discovered",
			Tags:        []string{"ai", app, "slash"},
		}
		if err := m.Validate(); err != nil {
			continue
		}
		if err := r.upsert(ctx, m); err != nil {
			return false, err
		}
	}
	if err := r.resolve(ctx); err != nil {
		return false, err
	}
	return true, nil
}

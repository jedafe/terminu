// Ref: .teos/sections/04 — manifest decode/validate, source priority
// shadowing, palette search, --help discovery parsing.
package registry

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	builtinreg "github.com/jedafe/terminu/registry"

	"github.com/jedafe/terminu/internal/db"
	"github.com/jedafe/terminu/internal/db/gen"
)

// The curated set must always decode — a malformed builtin file is a
// build-time bug, not a runtime surprise.
func TestBuiltinSetDecodes(t *testing.T) {
	got := loadFS(builtinreg.FS, "builtin")
	// The builtin set is the shell pseudo-app only — agent slash docs
	// are screen/dir-detected at runtime, real CLIs come from --help.
	if len(got) < 20 {
		t.Fatalf("builtin registry thin: %d manifests", len(got))
	}
	for id, m := range got {
		if m.Source != "builtin" || m.ID != id {
			t.Fatalf("manifest %s malformed source/id: %+v", id, m)
		}
	}
}

func TestDecodeRejectsBadManifest(t *testing.T) {
	for _, doc := range []string{
		"id: BAD ID\napplication: x\nname: x\ncommand: {executable: x}\nexecution: {risk: informational}\n",
		"id: ok.id\nname: x\ncommand: {executable: x}\nexecution: {risk: informational}\n",
		"id: ok.id\napplication: x\nname: x\ncommand: {executable: x}\nexecution: {risk: bogus}\n",
		"id: ok.id\napplication: x\nname: x\ncommand: {executable: x, arguments: [\"{undeclared}\"]}\nexecution: {risk: informational}\n",
	} {
		if _, err := DecodeManifests(strings.NewReader(doc), "custom"); !errors.Is(err, ErrInvalidManifest) {
			t.Fatalf("doc should fail validation: %q → %v", doc, err)
		}
	}
}

// Source priority: builtin > custom > discovered — loser keeps its row
// marked shadowed_by, and regains the index when the winner goes away.
func TestSourceShadowing(t *testing.T) {
	ctx := context.Background()
	d, err := db.Open(ctx, filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	defer func() { _ = d.Close() }()
	r := New(gen.New(d), t.TempDir())

	mk := func(src string) *Manifest {
		return &Manifest{
			ID: "x.y", Application: "x", Name: "x-" + src,
			Command:   CommandSpec{Executable: "x", Arguments: []string{src}},
			Execution: ExecSpec{Risk: "informational"}, Source: src,
		}
	}
	if err := r.upsert(ctx, mk("discovered")); err != nil {
		t.Fatal(err)
	}
	if err := r.upsert(ctx, mk("builtin")); err != nil {
		t.Fatal(err)
	}
	if err := r.resolve(ctx); err != nil {
		t.Fatal(err)
	}
	m, ok := r.Get("x.y")
	if !ok || m.Source != "builtin" {
		t.Fatalf("builtin should win: %+v", m)
	}
	// Loser row keeps its shadowed_by mark.
	row, err := r.q.GetManifestBySource(ctx, gen.GetManifestBySourceParams{ID: "x.y", Source: "discovered"})
	if err != nil {
		t.Fatal(err)
	}
	if row.ShadowedBy.String != "x.y" {
		t.Fatalf("shadowed_by = %q", row.ShadowedBy.String)
	}
	// Builtin vanishes → the discovered row unshadows on re-resolve.
	if err := r.q.DeleteManifestsBySource(ctx, "builtin"); err != nil {
		t.Fatal(err)
	}
	if err := r.resolve(ctx); err != nil {
		t.Fatal(err)
	}
	m, _ = r.Get("x.y")
	if m == nil || m.Source != "discovered" {
		t.Fatalf("discovered should regain index: %+v", m)
	}
}

func TestSearchRanksContextFirst(t *testing.T) {
	ctx := context.Background()
	d, err := db.Open(ctx, filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	defer func() { _ = d.Close() }()
	r := New(gen.New(d), t.TempDir())
	put := func(id, app string, procs []string) {
		m := &Manifest{
			ID: id, Application: app, Name: id,
			Command:   CommandSpec{Executable: app, Arguments: []string{"x"}},
			Execution: ExecSpec{Risk: "informational"},
			Context:   ContextSpec{Processes: procs},
			Source:    "builtin",
		}
		if err := r.upsert(ctx, m); err != nil {
			t.Fatal(err)
		}
	}
	put("git.status", "git", []string{"git"})
	put("docker.ps", "docker", []string{"docker"})
	if err := r.resolve(ctx); err != nil {
		t.Fatal(err)
	}
	got := r.Search("", map[string]bool{"docker.ps": true}, 0)
	if len(got) != 2 || !got[0].Context || got[0].ID != "docker.ps" {
		t.Fatalf("context hit should rank first: %+v", got)
	}
	// Subsequence fuzzy match; non-matching query drops results.
	got = r.Search("gtt", nil, 0)
	if len(got) != 1 || got[0].ID != "git.status" {
		t.Fatalf("fuzzy search: %+v", got)
	}
}

func TestParseHelp(t *testing.T) {
	help := `Usage: tool [flags] <command>

Available Commands:
  serve        Start the server
  status       Show status
  completions  Generate shell completions

Flags:
  -h, --help   help
`
	subs, flags := parseHelp(help)
	if len(subs) != 3 || subs[0].name != "serve" || subs[1].name != "status" {
		t.Fatalf("subs = %+v", subs)
	}
	// The Flags table parses too — -h/--help canonicalize to --help.
	if len(flags) != 1 || flags[0].name != "--help" || flags[0].disp != "-h, --help" {
		t.Fatalf("flags = %+v", flags)
	}
}

func TestParseHelpFlagsOnly(t *testing.T) {
	// GNU-style doc — no Commands section; the flag table is the whole
	// surface. Discovered manifests must come from the flag rows.
	help := `Usage: ls [OPTION]... [FILE]...

  -a, --all                  do not ignore entries starting with .
  -l                         use a long listing format
      --color=WHEN           color the output WHEN
  -h, --human-readable       with -l, print sizes like 1K

Examples stray line shouldn't parse:
  rm -rf / very bad
`
	subs, flags := parseHelp(help)
	if len(subs) != 0 {
		t.Fatalf("subs = %+v", subs)
	}
	if len(flags) != 4 {
		t.Fatalf("flags = %+v", flags)
	}
	if flags[0].name != "--all" || flags[0].disp != "-a, --all" {
		t.Fatalf("all = %+v", flags[0])
	}
	// Single-token rows canonicalize to themselves; value placeholders
	// (=WHEN) stay out of the display spec and the argv token.
	if flags[1].name != "-l" || flags[2].name != "--color" {
		t.Fatalf("canon = %+v", flags)
	}
}

func TestDiscoverOnceCooldown(t *testing.T) {
	ctx := context.Background()
	d, err := db.Open(ctx, filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	defer func() { _ = d.Close() }()
	r := New(gen.New(d), t.TempDir())

	// A first attempt for an exe that doesn't resolve runs (ran=true),
	// fails, and stamps the dedup map — an immediate re-try inside the
	// cooldown window is skipped (ran=false), no audit row.
	_, ran, _ := r.DiscoverOnce(ctx, "no-such-binary-xyz")
	if !ran {
		t.Fatal("first attempt should run")
	}
	_, ran, _ = r.DiscoverOnce(ctx, "no-such-binary-xyz")
	if ran {
		t.Fatal("retry inside cooldown should be skipped")
	}
}

// Re-discovery replaces an exe's provisional set: subs dropped from
// --help leave no stale rows, and a CamelCase sub lands as a lowercase
// id with its real-case argv token.
func TestDiscoverReplacesStale(t *testing.T) {
	ctx := context.Background()
	bin := t.TempDir()
	exe := filepath.Join(bin, "fakecli")
	write := func(subs string) {
		body := "#!/bin/sh\nprintf 'Usage: fakecli <command>\\n\\nAvailable Commands:\\n" + subs + "'\n"
		if err := os.WriteFile(exe, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write("  oldsub     does old\n  CamelSub   does camel\n")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	d, err := db.Open(ctx, filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	defer func() { _ = d.Close() }()
	r := New(gen.New(d), t.TempDir())

	got, err := r.Discover(ctx, "fakecli")
	if err != nil || len(got) != 2 {
		t.Fatalf("discover: %v got=%v", err, got)
	}
	m, ok := r.Get("fakecli.camelsub")
	if !ok || m.Command.Arguments[0] != "CamelSub" {
		t.Fatalf("camel sub: %+v", m)
	}
	// Second run drops oldsub, adds newsub.
	write("  newsub     does new\n")
	if _, err := r.Discover(ctx, "fakecli"); err != nil {
		t.Fatal(err)
	}
	if _, ok := r.Get("fakecli.oldsub"); ok {
		t.Fatal("stale discovered row survived re-discovery")
	}
	if _, ok := r.Get("fakecli.newsub"); !ok {
		t.Fatal("new sub missing after re-discovery")
	}
}

// A CLI that prints usage to stderr and exits non-zero still discovers —
// CombinedOutput is what makes ssh/ffmpeg-style tools scrapeable.
func TestDiscoverReadsStderrHelp(t *testing.T) {
	ctx := context.Background()
	bin := t.TempDir()
	exe := filepath.Join(bin, "stderrcli")
	body := "#!/bin/sh\nprintf 'Usage: stderrcli <command>\\n\\nCommands:\\n  go   do it\\n' >&2\nexit 1\n"
	if err := os.WriteFile(exe, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	d, err := db.Open(ctx, filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	defer func() { _ = d.Close() }()
	r := New(gen.New(d), t.TempDir())

	got, err := r.Discover(ctx, "stderrcli")
	if err != nil || len(got) != 1 || got[0].ID != "stderrcli.go" {
		t.Fatalf("stderr scrape: %v got=%v", err, got)
	}
}

// The help-argv chain falls through: a binary whose --help errors but
// whose `help` subcommand emits a table still discovers.
func TestDiscoverFallsBackToHelpSubcommand(t *testing.T) {
	ctx := context.Background()
	bin := t.TempDir()
	exe := filepath.Join(bin, "subcli")
	body := `#!/bin/sh
if [ "$1" = "help" ]; then
  printf 'Commands:\n  run   go\n'
  exit 0
fi
echo "unknown flag" >&2
exit 2
`
	if err := os.WriteFile(exe, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	d, err := db.Open(ctx, filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	defer func() { _ = d.Close() }()
	r := New(gen.New(d), t.TempDir())

	got, err := r.Discover(ctx, "subcli")
	if err != nil || len(got) != 1 || got[0].ID != "subcli.run" {
		t.Fatalf("help fallback: %v got=%v", err, got)
	}
}

// Screen lines → slash candidates: a menu cluster extracts, stray
// /path lines under the cluster threshold yield nothing.
func TestParseSlashCandidates(t *testing.T) {
	menu := []string{
		"(japa@mail)-[~]",
		"$ ls",
		"  /add      add files",
		"  /drop     drop files",
		"  /clear    clear chat",
		"/etc/passwd was mentioned once",
		"  single /nope inline",
	}
	got := ParseSlashCandidates(menu)
	if len(got) != 3 || got["add"] != "add files" {
		t.Fatalf("menu = %+v", got)
	}
	sparse := []string{"$ cat /etc/passwd", "$ echo /tmp/x"}
	if got := ParseSlashCandidates(sparse); got != nil {
		t.Fatalf("stray paths should not cluster: %+v", got)
	}
}

// A menu drawn mid-screen with box borders and a selector glyph still
// parses; `ls /a /b /c` — bare mid-line tokens — never forms a menu.
func TestParseSlashCandidatesBoxed(t *testing.T) {
	menu := []string{
		"❯ /quit",
		"prompt: what's next?              │ /add    add files    │",
		"                                  │ /drop   drop files   │",
		"                                  │ /clear  clear chat   │",
		"$ ls /a /b /c",
	}
	got := ParseSlashCandidates(menu)
	if len(got) != 4 || got["add"] != "add files" || got["quit"] != "" {
		t.Fatalf("boxed menu = %+v", got)
	}
	paths := []string{"$ ls /a /b /c", "$ cp /x /y /z"}
	if got := ParseSlashCandidates(paths); got != nil {
		t.Fatalf("bare path args must not cluster: %+v", got)
	}
}

// The strict scan — unknown apps only — drops bare anchored tokens:
// a shell's /usr/bin-style output can't cluster, a drawn menu with
// descriptions still parses.
func TestParseSlashCandidatesStrict(t *testing.T) {
	paths := []string{
		"/usr/local/share/zsh/site-functions",
		"/usr/share/zsh/vendor-completions",
		"/home/japa/.zsh/plugins",
	}
	if got := ParseSlashCandidatesStrict(paths); got != nil {
		t.Fatalf("anchored paths must not cluster in strict mode: %+v", got)
	}
	menu := []string{
		"│ /add    add files    │",
		"│ /drop   drop files   │",
		"│ /clear  clear chat   │",
	}
	got := ParseSlashCandidatesStrict(menu)
	if len(got) != 3 || got["add"] != "add files" {
		t.Fatalf("strict scan lost described rows: %+v", got)
	}
	mixed := []string{"/quit", "│ /add  add files │", "│ /drop  drop files │", "│ /x  x │"}
	mgot := ParseSlashCandidatesStrict(mixed)
	if _, ok := mgot["quit"]; ok {
		t.Fatalf("bare anchored row leaked into strict scan: %+v", mgot)
	}
}

// Denied processes (shells) are never slash surfaces; known agents
// always are.
func TestSlashCapabilitySets(t *testing.T) {
	for _, sh := range []string{"zsh", "bash", "fish", "sh", "pwsh"} {
		if !SlashDenied(sh) {
			t.Fatalf("shell %q not denied", sh)
		}
	}
	if SlashDenied("devin") || SlashDenied("claude") {
		t.Fatal("agents must not be denied")
	}
	for _, a := range []string{"devin", "claude", "codex", "gemini", "opencode", "aider"} {
		if !SlashKnownAgent(a) {
			t.Fatalf("agent %q not in known set", a)
		}
	}
}

// A skill dir — subdirs each holding a SKILL.md — yields one command
// per subdir, named by frontmatter when present.
func TestScanSlashDirSkills(t *testing.T) {
	root := t.TempDir()
	for name, fm := range map[string]string{
		"caveman":   "---\nname: caveman\ndescription: Talk like caveman\n---\nbody\n",
		"unnamedsk": "no frontmatter here\n",
	} {
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(fm), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got := ScanSlashDir(root)
	if got["caveman"] != "Talk like caveman" {
		t.Fatalf("frontmatter skill: %+v", got)
	}
	if _, ok := got["unnamedsk"]; !ok {
		t.Fatalf("subdir-name fallback missing: %+v", got)
	}
	if _, ok := got["skill"]; ok {
		t.Fatal("SKILL.md must name its parent, not itself")
	}
}

// A bundled doc's command table — `| `/name` | desc |` rows — yields
// every token ("/exit` or `/quit`", aliases), keeps arg specs off
// the name, and ignores prose + doc-link paths.
func TestScanSlashDocDir(t *testing.T) {
	root := t.TempDir()
	doc := strings.Join([]string{
		"# Commands",
		"| Command | Description |",
		"| --- | --- |",
		"| `/help` | See all available commands |",
		"| `/exit` or `/quit` | Exit the application |",
		"| `/mode <name>` | Switch mode ([modes](/cli/reference/modes)) |",
		"| `/sync` | Full docs at `/cli/reference/sync` |",
		"| `/bypass` | Bypass mode (aliases: `/yolo`, `/dangerous`) |",
		"",
		"Run `/help` in a session — prose mentions don't count.",
	}, "\n")
	if err := os.WriteFile(filepath.Join(root, "commands.mdx"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	got := ScanSlashDocDir(root)
	for _, want := range []string{"help", "exit", "quit", "mode", "bypass", "yolo", "dangerous"} {
		if _, ok := got[want]; !ok {
			t.Fatalf("missing %q: %+v", want, got)
		}
	}
	if got["mode"] != "Switch mode (modes)" {
		t.Fatalf("mode desc: %q", got["mode"])
	}
	for _, reject := range []string{"command", "cli", "reference", "or"} {
		if _, ok := got[reject]; ok {
			t.Fatalf("bogus token %q in set", reject)
		}
	}
}

// `devin skills list` rows: `/`-prefixed names are user-invocable and
// extract with descriptions; bare model-only skills don't count.
func TestParseSlashListOutput(t *testing.T) {
	out := ParseSlashListOutput(`Available skills
  devin-cli [model] (~/.local/share/devin) - Look up docs
  /declarative-repo-setup [model,user] (built-in) - Generate and verify a working environment.yaml
  /upload-secrets [model,user] (built-in) - Securely upload local secrets
  /bare
`)
	if len(out) != 3 {
		t.Fatalf("list parse: %+v", out)
	}
	if _, ok := out["devin-cli"]; ok {
		t.Fatal("model-only skill (no /) must not parse")
	}
	if !strings.HasPrefix(out["declarative-repo-setup"], "Generate") || out["upload-secrets"] == "" {
		t.Fatalf("descs: %+v", out)
	}
}

// Slash detection diffs: same set → no write; changed set → rewrite
// that leaves the app's --help-derived rows alone.
func TestDiscoverSlashDiffs(t *testing.T) {
	ctx := context.Background()
	d, err := db.Open(ctx, filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	defer func() { _ = d.Close() }()
	r := New(gen.New(d), t.TempDir())

	// A --help-derived flag row for the same app must survive slash
	// rewrites.
	if err := r.upsert(ctx, &Manifest{
		ID: "mytool.flag.verbose", Application: "mytool", Name: "mytool -v, --verbose",
		Command:   CommandSpec{Executable: "mytool", Arguments: []string{"--verbose"}},
		Execution: ExecSpec{Risk: "controlled", Confirmation: true},
		Source:    "discovered", Tags: []string{"mytool", "discovered", "flag"},
	}); err != nil {
		t.Fatal(err)
	}

	changed, err := r.DiscoverSlash(ctx, "mytool", map[string]string{"add": "add files", "clear": ""})
	if err != nil || !changed {
		t.Fatalf("first write: changed=%v err=%v", changed, err)
	}
	if _, ok := r.Get("mytool.slash.add"); !ok {
		t.Fatal("slash manifest missing")
	}
	// Same set → no rewrite.
	if changed, _ = r.DiscoverSlash(ctx, "mytool", map[string]string{"add": "add files", "clear": ""}); changed {
		t.Fatal("identical set should be a no-op")
	}
	// Shrinking set → rewrite; flag rows survive.
	if changed, _ = r.DiscoverSlash(ctx, "mytool", map[string]string{"add": "add files"}); !changed {
		t.Fatal("shrunken set should rewrite")
	}
	if _, ok := r.Get("mytool.slash.clear"); ok {
		t.Fatal("removed command should drop")
	}
	if _, ok := r.Get("mytool.flag.verbose"); !ok {
		t.Fatal("slash rewrite wiped --help rows")
	}
	// Empty evidence keeps the cached set.
	if changed, _ = r.DiscoverSlash(ctx, "mytool", nil); changed {
		t.Fatal("empty evidence must not wipe the cache")
	}
	if _, ok := r.Get("mytool.slash.add"); !ok {
		t.Fatal("cache lost on empty evidence")
	}
}

// go's "The commands are:" and kubectl's "Basic Commands:" style
// headers open the section too — colon-terminated headers only.
func TestParseHelpHeaderVariants(t *testing.T) {
	help := `Usage: gocli <command>

The commands are:

	bug      start a bug report
	build    compile packages

See gocli help commands for more.
`
	subs, _ := parseHelp(help)
	if len(subs) != 2 || subs[0].name != "bug" {
		t.Fatalf("go-style header: %+v", subs)
	}
	help2 := `Basic Commands:
  get    fetch
  set    apply
`
	subs, _ = parseHelp(help2)
	if len(subs) != 2 {
		t.Fatalf("kubectl-style header: %+v", subs)
	}
}

// An exe outside PATH but inside an extraBinDir still discovers — the
// inventory's dir map is the exec fallback (this was the
// "could not read --help" root cause for ~/.local/bin-style installs).
func TestDiscoverResolvesOffPathExe(t *testing.T) {
	ctx := context.Background()
	bin := t.TempDir()
	exe := filepath.Join(bin, "offpath")
	body := "#!/bin/sh\nprintf 'Commands:\\n  go   do\\n'\n"
	if err := os.WriteFile(exe, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	// Intentionally NOT on PATH — the exePath map is the only route.
	d, err := db.Open(ctx, filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	defer func() { _ = d.Close() }()
	r := New(gen.New(d), t.TempDir())
	r.exePath = map[string]string{"offpath": bin}

	got, err := r.Discover(ctx, "offpath")
	if err != nil || len(got) != 1 || got[0].ID != "offpath.go" {
		t.Fatalf("off-path discover: %v got=%v", err, got)
	}
}

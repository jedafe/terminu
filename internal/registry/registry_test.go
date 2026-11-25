// Ref: .teos/sections/04 — manifest decode/validate, source priority
// shadowing, palette search, --help discovery parsing, argv building.
package registry

import (
	"context"
	"errors"
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
	if len(got) < 60 {
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

// A flag param renders after the positional templates — `git commit
// -m x`, never `git -m x commit`.
func TestBuildFlagOrdering(t *testing.T) {
	m := &Manifest{
		ID: "git.commit", Application: "git", Name: "Commit",
		Parameters: []Parameter{
			{Name: "message", Type: "string", Required: true, Flag: strp("-m")},
		},
		Command:   CommandSpec{Executable: "git", Arguments: []string{"commit"}},
		Execution: ExecSpec{Risk: "controlled", Confirmation: true},
	}
	argv, _, err := m.Build(map[string]any{"message": "wip"}, SessionCtx{})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"git", "commit", "-m", "wip"}
	if strings.Join(argv, " ") != strings.Join(want, " ") {
		t.Fatalf("argv = %v want %v", argv, want)
	}
}

func TestBuildPositionalAndSessionCwd(t *testing.T) {
	m := &Manifest{
		ID: "git.branch.create", Application: "git", Name: "Branch",
		Parameters: []Parameter{{Name: "branch", Type: "string", Required: true}},
		Command:    CommandSpec{Executable: "git", Arguments: []string{"checkout", "-b", "{branch}"}},
		Execution:  ExecSpec{Risk: "controlled", WorkingDir: "{session.cwd}"},
	}
	argv, cwd, err := m.Build(map[string]any{"branch": "feat/x"}, SessionCtx{Cwd: "/repo"})
	if err != nil {
		t.Fatal(err)
	}
	if cwd != "/repo" || strings.Join(argv, " ") != "git checkout -b feat/x" {
		t.Fatalf("argv=%v cwd=%q", argv, cwd)
	}
	if _, _, err := m.Build(map[string]any{}, SessionCtx{}); !errors.Is(err, ErrInvalidManifest) {
		t.Fatalf("missing required param should fail: %v", err)
	}
}

func TestBuildEnumAndBoolean(t *testing.T) {
	m := &Manifest{
		ID: "x.y", Application: "x", Name: "x",
		Parameters: []Parameter{
			{Name: "sh", Type: "enum", Options: []string{"/bin/sh", "/bin/bash"}},
			{Name: "force", Type: "boolean", Flag: strp("--force")},
		},
		Command:   CommandSpec{Executable: "x", Arguments: []string{"{sh}"}},
		Execution: ExecSpec{Risk: "informational"},
	}
	argv, _, err := m.Build(map[string]any{"sh": "/bin/bash", "force": true}, SessionCtx{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(argv, " ") != "x /bin/bash --force" {
		t.Fatalf("argv = %v", argv)
	}
	if _, _, err := m.Build(map[string]any{"sh": "zsh"}, SessionCtx{}); err == nil {
		t.Fatal("enum violation should fail")
	}
}

// An absent optional param must drop its argv element — an empty-string
// arg would reach execve literally and confuse the child.
func TestBuildOptionalDrops(t *testing.T) {
	m := &Manifest{
		ID: "x.opt", Application: "x", Name: "x",
		Parameters: []Parameter{
			{Name: "ref", Type: "string"},
			{Name: "tag", Type: "string", Flag: strp("--tag")},
		},
		Command:   CommandSpec{Executable: "x", Arguments: []string{"show", "{ref}"}},
		Execution: ExecSpec{Risk: "informational"},
	}
	argv, _, err := m.Build(map[string]any{}, SessionCtx{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(argv, " ") != "x show" {
		t.Fatalf("argv = %v want [x show]", argv)
	}
	// Present-but-empty optional flag drops the flag too.
	argv, _, err = m.Build(map[string]any{"ref": "HEAD", "tag": ""}, SessionCtx{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(argv, " ") != "x show HEAD" {
		t.Fatalf("argv = %v want [x show HEAD]", argv)
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
	subs := parseHelp(help)
	if len(subs) != 3 || subs[0].name != "serve" || subs[1].name != "status" {
		t.Fatalf("subs = %+v", subs)
	}
}

func strp(s string) *string { return &s }

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

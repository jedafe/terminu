// Ref: .teos/sections/04 — registry: source sync (builtin FS > custom
// dir > discovered rows), id-collision shadow resolution, and the
// in-memory index the palette/context endpoints read. The table is
// persistence + audit; the index is the <100 ms search surface.
package registry

import (
	"context"
	"database/sql"
	"encoding/json"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	builtinreg "github.com/jedafe/terminu/registry"

	"github.com/jedafe/terminu/internal/db/gen"
)

// Registry is the command-manifest service.
type Registry struct {
	q       *gen.Queries
	dataDir string // custom manifests live in {dataDir}/registry/*.yaml

	mu   sync.RWMutex
	byID map[string]*Manifest // resolved winners only
}

func New(q *gen.Queries, dataDir string) *Registry {
	return &Registry{q: q, dataDir: dataDir, byID: map[string]*Manifest{}}
}

// Sync (re)builds the table from all three sources then re-resolves the
// index. Boot calls it once; discovery calls it after writing rows.
func (r *Registry) Sync(ctx context.Context) error {
	if err := r.syncSource(ctx, "builtin", loadFS(builtinreg.FS, "builtin")); err != nil {
		return err
	}
	if err := r.syncSource(ctx, "custom", r.loadCustom()); err != nil {
		return err
	}
	return r.resolve(ctx)
}

// syncSource upserts a source's manifests into the table and drops rows
// of that source whose definitions vanished. Parse errors are collected
// per file — one bad custom file can't kill the boot, and its message
// is logged with the file name for the operator to fix.
func (r *Registry) syncSource(ctx context.Context, source string, loaded map[string]*Manifest) error {
	var firstErr error
	if err := r.q.DeleteManifestsBySource(ctx, source); err != nil {
		return err
	}
	for _, m := range loaded {
		if err := r.upsert(ctx, m); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// loadCustom reads user-authored manifests from {dataDir}/registry/ —
// the `custom` source between builtin and discovered.
func (r *Registry) loadCustom() map[string]*Manifest {
	out := map[string]*Manifest{}
	dir := filepath.Join(r.dataDir, "registry")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return out // no custom dir is the normal case
	}
	for _, e := range entries {
		if e.IsDir() || (!strings.HasSuffix(e.Name(), ".yaml") && !strings.HasSuffix(e.Name(), ".yml")) {
			continue
		}
		f, err := os.Open(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		ms, err := DecodeManifests(f, "custom")
		_ = f.Close()
		if err != nil {
			slog.Warn("custom manifest file skipped", "file", e.Name(), "err", err)
			continue
		}
		for _, m := range ms {
			out[m.ID] = m
		}
	}
	return out
}

// loadFS decodes every .yaml stream in an embedded FS.
func loadFS(fsys fs.FS, source string) map[string]*Manifest {
	out := map[string]*Manifest{}
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return out
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		f, err := fsys.Open(e.Name())
		if err != nil {
			continue
		}
		ms, err := DecodeManifests(f, source)
		_ = f.Close()
		if err != nil {
			// A bad builtin file is a build-time bug — loud, not silent.
			slog.Error("builtin manifest file invalid", "file", e.Name(), "err", err)
			continue
		}
		for _, m := range ms {
			out[m.ID] = m
		}
	}
	return out
}

// resolve re-reads the table, marks winners/losers, and rebuilds the
// index. Priority: builtin > custom > discovered (§04 Registry). Every
// row gets an explicit shadowed_by — NULL on winners — so a stale mark
// can't survive the winner's removal (sqlc's sqlite grammar can't emit
// a NULL literal in UPDATE SET, hence the per-row bound params).
func (r *Registry) resolve(ctx context.Context) error {
	rows, err := r.q.ListManifests(ctx)
	if err != nil {
		return err
	}
	byID := map[string]*Manifest{}
	var losers []*Manifest
	for _, row := range rows {
		if row.Enabled == 0 {
			continue // disabled rows stay in the table, never index
		}
		m := rowToManifest(row)
		cur := byID[m.ID]
		if cur == nil || sourceRank[m.Source] > sourceRank[cur.Source] {
			if cur != nil {
				cur.ShadowedBy = m.ID
				losers = append(losers, cur)
			}
			byID[m.ID] = m
		} else {
			m.ShadowedBy = cur.ID
			losers = append(losers, m)
		}
	}
	loserSet := map[string]sql.NullString{}
	for _, l := range losers {
		loserSet[l.Source+":"+l.ID] = sql.NullString{String: l.ShadowedBy, Valid: true}
	}
	for _, row := range rows {
		if row.Enabled == 0 {
			continue
		}
		if err := r.q.SetManifestShadowed(ctx, gen.SetManifestShadowedParams{
			ShadowedBy: loserSet[row.Source+":"+row.ID], ID: row.ID, Source: row.Source,
		}); err != nil {
			return err
		}
	}
	r.mu.Lock()
	r.byID = byID
	r.mu.Unlock()
	return nil
}

// upsert writes one manifest row — JSON columns hold the structured
// fields (params/argv/context/tags).
func (r *Registry) upsert(ctx context.Context, m *Manifest) error {
	params, _ := json.Marshal(m.Parameters)
	args, _ := json.Marshal(m.Command.Arguments)
	procs, _ := json.Marshal(m.Context.Processes)
	files, _ := json.Marshal(m.Context.Files)
	tags, _ := json.Marshal(m.Tags)
	var conf int64
	if m.Execution.Confirmation {
		conf = 1
	}
	return r.q.UpsertManifest(ctx, gen.UpsertManifestParams{
		ID: m.ID, Source: m.Source, Application: m.Application,
		Name: m.Name, Description: m.Description, Params: string(params),
		Executable: m.Command.Executable, Arguments: string(args),
		Risk: m.Execution.Risk, Confirmation: conf,
		TimeoutS: int64(m.Execution.TimeoutS), WorkingDir: m.Execution.WorkingDir,
		CtxProcesses: string(procs), CtxFiles: string(files), Tags: string(tags),
	})
}

// Get resolves a manifest id through the index — shadowed losers are
// invisible to execution and search.
func (r *Registry) Get(id string) (*Manifest, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	m, ok := r.byID[id]
	return m, ok
}

// List returns every resolved manifest (for ?q= with no context).
func (r *Registry) List() []*Manifest {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Manifest, 0, len(r.byID))
	for _, m := range r.byID {
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Context returns resolved manifests whose context matches the running
// session: detected process/tool names, or files present in its cwd
// (§04 — "context matches the session's detected process/files").
func (r *Registry) Context(processes []string, cwd string) []*Manifest {
	procSet := map[string]bool{}
	for _, p := range processes {
		if p != "" {
			procSet[p] = true
		}
	}
	var cwdFiles map[string]bool
	if cwd != "" {
		if ents, err := os.ReadDir(cwd); err == nil {
			cwdFiles = map[string]bool{}
			for _, e := range ents {
				cwdFiles[e.Name()] = true
			}
		}
	}
	var out []*Manifest
	for _, m := range r.List() {
		if matchContext(m, procSet, cwdFiles) {
			out = append(out, m)
		}
	}
	return out
}

func matchContext(m *Manifest, procs map[string]bool, cwdFiles map[string]bool) bool {
	for _, p := range m.Context.Processes {
		if procs[p] {
			return true
		}
	}
	if cwdFiles != nil {
		for _, f := range m.Context.Files {
			// A file entry may be a literal name or a simple *.ext glob.
			if cwdFiles[f] {
				return true
			}
			if strings.HasPrefix(f, "*.") {
				suffix := f[1:]
				for name := range cwdFiles {
					if strings.HasSuffix(name, suffix) {
						return true
					}
				}
			}
		}
	}
	return false
}

// rowToManifest reassembles the manifest from its split columns.
func rowToManifest(row gen.CommandManifest) *Manifest {
	m := &Manifest{
		ID: row.ID, Application: row.Application, Name: row.Name,
		Description: row.Description, Source: row.Source,
		ShadowedBy: row.ShadowedBy.String,
	}
	_ = json.Unmarshal([]byte(row.Params), &m.Parameters)
	_ = json.Unmarshal([]byte(row.Arguments), &m.Command.Arguments)
	m.Command.Executable = row.Executable
	m.Execution = ExecSpec{
		Risk: row.Risk, Confirmation: row.Confirmation != 0,
		TimeoutS: int(row.TimeoutS), WorkingDir: row.WorkingDir,
	}
	_ = json.Unmarshal([]byte(row.CtxProcesses), &m.Context.Processes)
	_ = json.Unmarshal([]byte(row.CtxFiles), &m.Context.Files)
	_ = json.Unmarshal([]byte(row.Tags), &m.Tags)
	return m
}

// Ref: .teos/sections/04 — the ⌘ apps modal needs an inventory of
// installed CLIs: every executable visible on $PATH, merged with the
// registry's applications. Refreshed at boot and periodically — an
// install between ticks surfaces on the next pass.
package registry

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// appsScanInterval is the periodic inventory refresh — same class of
// constant as discoveryTimeout: a tune, not deployment config.
const appsScanInterval = 10 * time.Minute

// AppEntry is one row of the apps inventory. Path is the first $PATH
// dir that provides the binary (empty for registry-only applications);
// Sources are the manifest sources backing it — empty Sources with
// Installed means "on PATH, no manifests" (discoverable on demand).
type AppEntry struct {
	Name      string   `json:"name"`
	Path      string   `json:"path,omitempty"`
	Installed bool     `json:"installed"`
	Manifests int      `json:"manifests"`
	Sources   []string `json:"sources,omitempty"`
}

// ScanLoop refreshes the PATH inventory on an interval until ctx ends —
// boot calls RefreshApps synchronously first so the route never serves
// an empty list during startup.
func (r *Registry) ScanLoop(ctx context.Context) {
	t := time.NewTicker(appsScanInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			r.RefreshApps()
		}
	}
}

// RefreshApps rebuilds the inventory: $PATH executables + every
// application the registry knows. Cheap — one readdir per PATH dir.
func (r *Registry) RefreshApps() {
	onPath := map[string]string{} // name → its dir (first wins: PATH order)
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if dir == "" {
			continue
		}
		ents, err := os.ReadDir(dir)
		if err != nil {
			continue // a dead dir (unmounted share) can't stall the scan
		}
		for _, e := range ents {
			name := e.Name()
			if e.IsDir() || strings.HasPrefix(name, ".") {
				continue
			}
			if _, seen := onPath[name]; seen {
				continue
			}
			fi, err := e.Info()
			if err != nil || fi.Mode()&0o111 == 0 {
				continue // regular executables only — scripts/libs skipped
			}
			onPath[name] = dir
		}
	}
	// Manifest applications get a row even when not installed — the
	// modal shows curated docs regardless of the host's PATH.
	byApp := map[string]*AppEntry{}
	for name, dir := range onPath {
		byApp[name] = &AppEntry{Name: name, Path: dir, Installed: true}
	}
	srcSeen := map[string]map[string]bool{}
	for _, m := range r.List() {
		e, ok := byApp[m.Application]
		if !ok {
			e = &AppEntry{Name: m.Application}
			byApp[m.Application] = e
		}
		e.Manifests++
		if srcSeen[m.Application] == nil {
			srcSeen[m.Application] = map[string]bool{}
		}
		if !srcSeen[m.Application][m.Source] {
			srcSeen[m.Application][m.Source] = true
			e.Sources = append(e.Sources, m.Source)
		}
	}
	out := make([]AppEntry, 0, len(byApp))
	for _, e := range byApp {
		sort.Strings(e.Sources)
		out = append(out, *e)
	}
	sort.Slice(out, func(i, j int) bool {
		// documented first, then alphabetical — the modal's default
		// order groups apps-that-have-commands above bare names.
		if (out[i].Manifests > 0) != (out[j].Manifests > 0) {
			return out[i].Manifests > 0
		}
		return out[i].Name < out[j].Name
	})
	r.appsMu.Lock()
	r.apps, r.appsAt = out, time.Now()
	r.appsMu.Unlock()
}

// Apps returns the cached inventory + when it was built.
func (r *Registry) Apps() ([]AppEntry, time.Time) {
	r.appsMu.RLock()
	defer r.appsMu.RUnlock()
	return r.apps, r.appsAt
}

// ForApplication lists one app's manifests sorted by name — the
// ?application= route's backing call.
func (r *Registry) ForApplication(app string) []*Manifest {
	var out []*Manifest
	for _, m := range r.List() {
		if m.Application == app {
			out = append(out, m)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

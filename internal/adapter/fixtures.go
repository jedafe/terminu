// Ref: .teos/sections/06 — fixture registry. The shipped .cast set is
// embedded so the server can register + smoke-replay it at boot (the
// `fixtures` table is its recorded status).
package adapter

import (
	"embed"
	"io/fs"
	"strings"
)

// FixtureFS is the curated transcript set — fixtures/<tool>/<version>/
// <scenario>.cast, the same files the replay tests consume.
//
//go:embed fixtures
var FixtureFS embed.FS

// FixtureRef is one registered transcript.
type FixtureRef struct {
	Tool     string
	Version  string
	Scenario string
	Path     string
}

// ListFixtures walks the embedded set.
func ListFixtures() []FixtureRef {
	var out []FixtureRef
	_ = fs.WalkDir(FixtureFS, "fixtures", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".cast") {
			return nil
		}
		// fixtures/<tool>/<version>/<scenario>.cast
		parts := strings.Split(path, "/")
		if len(parts) != 4 {
			return nil
		}
		out = append(out, FixtureRef{
			Tool: parts[1], Version: parts[2],
			Scenario: strings.TrimSuffix(parts[3], ".cast"), Path: path,
		})
		return nil
	})
	return out
}

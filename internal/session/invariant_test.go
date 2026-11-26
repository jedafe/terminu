package session

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Ref: .teos/adr/ADR-007 — observe-and-inform. The terminal `input`
// frame is the sole writer to a session PTY: detection surfaces
// inform, they never actuate. This source-level scan is the tripwire —
// a new writer path (e.g. a reintroduced keystroke injection) fails
// here until this file's allowlist is edited deliberately.
//
// Two rules are enforced:
//  1. The PTY master may only be written inside internal/session
//     (the session owns its ptmx). Any *os.File named like a PTY master
//     counts — a renamed field must not slip the check.
//  2. Calls on session-typed receivers (anything named *sess*/*session*)
//     may only live in the ws input handler (internal/server/ws.go).
//
// It is convention-based — a receiver named to dodge the pattern would
// still be caught by rule 1 if it ever reaches a PTY handle.
func TestInputFrameIsSolePTYWriter(t *testing.T) {
	masterOK := map[string]bool{"internal/session/session.go": true}
	callerOK := map[string]bool{"internal/server/ws.go": true}

	// Case-insensitive, suffix-tolerant: myPty/masterFile/PTY_MASTER all
	// count. Over-matching is the safe side for a tripwire — a false
	// positive is loud and trivially renamed, a miss is silent.
	masterWrite := regexp.MustCompile(`(?i)\b\w*(ptmx|pty|master)\w*\.Write\(`)
	sessionWrite := regexp.MustCompile(`(?i)\b\w*sess(?:ion)?\w*\.Write\(`)

	root := filepath.Join("..", "..") // package dir → repo root
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			switch name {
			case ".git", ".teos", "apps", "deploy", "node_modules":
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return rerr
		}
		rel = filepath.ToSlash(rel)
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		for i, line := range strings.Split(string(data), "\n") {
			if masterWrite.MatchString(line) && !masterOK[rel] {
				t.Errorf("%s:%d: PTY master write outside %v: %s", rel, i+1, "internal/session", strings.TrimSpace(line))
			}
			if sessionWrite.MatchString(line) && !callerOK[rel] {
				t.Errorf("%s:%d: session Write call outside the ws input handler: %s", rel, i+1, strings.TrimSpace(line))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
}

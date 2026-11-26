package session

import (
	"go/ast"
	"go/types"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

// Ref: .teos/adr/ADR-007 — observe-and-inform. The terminal `input`
// frame is the sole writer to a session PTY: detection surfaces
// inform, they never actuate. This file is the tripwire — a new writer
// path (e.g. a reintroduced keystroke injection) fails here until the
// allowlists are edited deliberately.
//
// Three rules are enforced:
//  1. The PTY master may only be written inside internal/session —
//     the field is unexported, so this is a renamed-field tripwire
//     (regex over receiver names).
//  2. No Write call on a *session.ptySession receiver outside the ws
//     input handler — TYPE-AWARE: the variable's name is irrelevant.
//     `term.Write(data)` on a session value trips identically to
//     `sess.Write(data)`.
//  3. PTY-handle acquisition (pty.Open/Start*/openpty, /dev/ptm*)
//     may only happen inside internal/session — rules 1+2 are
//     meaningless if another package can mint its own master.
func TestInputFrameIsSolePTYWriter(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..")) // package dir → repo root
	if err != nil {
		t.Fatal(err)
	}
	t.Run("typed session receivers confined to ws.go", func(t *testing.T) {
		checkTypedWriters(t, root)
	})
	t.Run("pty handles confined to internal/session", func(t *testing.T) {
		scanSource(t, root)
	})
}

// checkTypedWriters loads the module's type info and walks every
// selector: `x.Write` where x is a *session.ptySession may only appear
// in internal/server/ws.go. The check is name-independent — a session
// held in `term`, `conn`, `s` trips identically — and form-independent:
// `sess.Write(data)` and method-value extraction `f := sess.Write`
// are the same selector. Residual: boxing into io.Writer (`w := io.
// Writer(sess)`) still hides the concrete type — deliberate evasion is
// a review problem, not a tripwire problem.
func checkTypedWriters(t *testing.T, root string) {
	t.Helper()
	pkgs, err := packages.Load(&packages.Config{
		Mode: packages.NeedName | packages.NeedFiles |
			packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo |
			packages.NeedImports | packages.NeedDeps,
		Dir:   root,
		Tests: false,
	}, "./internal/...")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	for _, pkg := range pkgs {
		if len(pkg.Errors) > 0 {
			t.Fatalf("type errors block the invariant in %s: %v", pkg.PkgPath, pkg.Errors[0])
		}
	}
	for _, pkg := range pkgs {
		for _, f := range pkg.Syntax {
			pos := pkg.Fset.Position(f.Pos())
			rel, rerr := filepath.Rel(root, pos.Filename)
			if rerr != nil {
				continue
			}
			rel = filepath.ToSlash(rel)
			ast.Inspect(f, func(n ast.Node) bool {
				sel, ok := n.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "Write" {
					return true
				}
				allowed := rel == "internal/server/ws.go" || strings.HasPrefix(rel, "internal/session/")
				if isPtySessionPtr(pkg.TypesInfo.TypeOf(sel.X)) && !allowed {
					t.Errorf("%s:%d: .Write reference on *session.ptySession outside the ws input handler", rel, pkg.Fset.Position(sel.Pos()).Line)
				}
				return true
			})
		}
	}
}

func isPtySessionPtr(T types.Type) bool {
	p, ok := T.(*types.Pointer)
	if !ok {
		return false
	}
	n, ok := p.Elem().(*types.Named)
	if !ok {
		return false
	}
	o := n.Obj()
	return o.Name() == "ptySession" &&
		o.Pkg() != nil && o.Pkg().Path() == "github.com/jedafe/terminu/internal/session"
}

// scanSource polices the name-based rules types can't see: writes on
// PTY-named handles (rule 1 — a renamed ptmx field) and PTY-handle
// acquisition (rule 3). Over-matching is the safe side for a tripwire —
// a false positive is loud and trivially renamed, a miss is silent.
func scanSource(t *testing.T, root string) {
	t.Helper()
	masterWrite := regexp.MustCompile(`(?i)\b\w*(ptmx|pty|master)\w*\.Write\(`)
	ptyAcquire := regexp.MustCompile(`(?i)(pty\.(open|start|openpty)|/dev/ptmx|/dev/pts/)`)
	sessionOK := "internal/session/"

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
			if !strings.HasPrefix(rel, sessionOK) {
				if masterWrite.MatchString(line) {
					t.Errorf("%s:%d: PTY master write outside internal/session: %s", rel, i+1, strings.TrimSpace(line))
				}
				if ptyAcquire.MatchString(line) {
					t.Errorf("%s:%d: PTY handle acquisition outside internal/session: %s", rel, i+1, strings.TrimSpace(line))
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
}

// Ref: .teos/sections/06 — fixture replay pipeline. The test suite owns
// the exact-match assertions; this boot-time pass is the coarse check
// recorded per fixture: transcript replays into the screen model and
// the owning adapter returns a match (every curated fixture is a wait
// scenario — nil on your own fixture is a real regression).
package adapter

import (
	"log/slog"

	"github.com/jedafe/terminu/internal/screen"
)

// All returns the curated adapter set — the six v1 tools. The session
// registry binds in this order; specific before generic is the list's
// contract.
func All() []Adapter {
	return []Adapter{Claude(), Codex(), Gemini(), Opencode(), Devin(), Aider()}
}

// ForTool resolves one curated adapter by its pin tool name.
func ForTool(tool string) Adapter {
	for _, a := range All() {
		if a.Pin().Tool == tool {
			return a
		}
	}
	return nil
}

// SmokeResult is one fixture's boot replay outcome.
type SmokeResult struct {
	FixtureRef
	Pass bool
	Err  string
}

// SmokeAll replays every embedded fixture through the same screen model
// Detect reads in production. `generic/` fixtures drive DetectGeneric;
// tool dirs drive their curated adapter. Pass = replay clean, Detect
// non-nil, no panic.
func SmokeAll() []SmokeResult {
	refs := ListFixtures()
	out := make([]SmokeResult, 0, len(refs))
	for _, ref := range refs {
		out = append(out, smokeOne(ref))
	}
	return out
}

func smokeOne(ref FixtureRef) (res SmokeResult) {
	res.FixtureRef = ref
	defer func() {
		if r := recover(); r != nil {
			res.Pass = false
			res.Err = "panic"
		}
	}()
	f, err := FixtureFS.Open(ref.Path)
	if err != nil {
		res.Err = err.Error()
		return
	}
	defer func() { _ = f.Close() }()
	s := screen.New(80, 24, 0)
	if err := ReplayCastReader(f, s); err != nil {
		res.Err = err.Error()
		return
	}
	ctx := &SessionCtx{ScanBottomRows: 12, Trigger: "quiescence"}
	var m *StateMatch
	if ref.Tool == "generic" {
		m = DetectGeneric(s, ctx)
	} else if a := ForTool(ref.Tool); a != nil {
		m = a.Detect(s, ctx)
	} else {
		slog.Warn("fixture owns unknown tool", "fixture", ref.Path)
		res.Err = "unknown tool"
		return
	}
	res.Pass = m != nil
	if !res.Pass {
		res.Err = "no match"
	}
	return
}

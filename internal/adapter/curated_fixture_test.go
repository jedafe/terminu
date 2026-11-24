package adapter

import (
	"testing"

	"github.com/jedafe/terminu/internal/domain"
)

// Ref: .teos/specs/adapter-design.md §7/§8 — curated tool adapters.
// These fixtures are hypothesis transcripts (the §7 tables), not real
// captures — that's why the pins carry no VerifiedAt.

func TestClaudeMatches(t *testing.T) {
	a := Claude()
	for _, c := range []struct {
		proc string
		argv []string
		want bool
	}{
		{"claude", []string{"claude"}, true},
		{"node", []string{"node", "/usr/lib/claude/cli.js"}, true},
		{"bash", []string{"bash"}, false},
		{"claudex", []string{"claudex"}, false},
	} {
		if got := a.MatchesProcess(c.proc, c.argv); got != c.want {
			t.Errorf("match(%q,%v) = %v want %v", c.proc, c.argv, got, c.want)
		}
	}
}

func TestFixtureClaudePermission(t *testing.T) {
	s := replay(t, "fixtures/claude/1.0/permission.cast")
	m := Claude().Detect(s, ctx())
	if m == nil {
		t.Fatal("no match")
	}
	if m.Kind != domain.WaitPermission || m.Confidence != domain.ConfidenceHigh {
		t.Fatalf("match = %+v", m)
	}
	if len(m.Prompt.Options) != 3 || m.Prompt.Options[0].Inject.Char != '1' {
		t.Fatalf("options = %+v", m.Prompt.Options)
	}
	if m.Prompt.Title != "Do you want to proceed?" {
		t.Fatalf("title = %q", m.Prompt.Title)
	}
	if m.Evidence == "" {
		t.Fatal("evidence mandatory")
	}
}

func TestFixtureClaudeTrust(t *testing.T) {
	s := replay(t, "fixtures/claude/1.0/trust.cast")
	m := Claude().Detect(s, ctx())
	if m == nil {
		t.Fatal("no match")
	}
	if m.Kind != domain.WaitPermission || len(m.Prompt.Options) != 2 {
		t.Fatalf("match = %+v", m)
	}
}

func TestFixtureCodexApproval(t *testing.T) {
	s := replay(t, "fixtures/codex/0.40/exec-approval.cast")
	m := Codex().Detect(s, ctx())
	if m == nil {
		t.Fatal("no match")
	}
	if m.Kind != domain.WaitPermission || m.Confidence != domain.ConfidenceHigh {
		t.Fatalf("match = %+v", m)
	}
	want := []byte{'y', 'a', 'n'}
	if len(m.Prompt.Options) != len(want) {
		t.Fatalf("options = %+v", m.Prompt.Options)
	}
	for i, o := range m.Prompt.Options {
		if o.Inject.Kind != KeyChar || o.Inject.Char != want[i] {
			t.Fatalf("option %d inject = %+v", i, o.Inject)
		}
	}
}

func TestFixtureGeminiAllowExec(t *testing.T) {
	s := replay(t, "fixtures/gemini/0.1/allow-exec.cast")
	m := Gemini().Detect(s, ctx())
	if m == nil {
		t.Fatal("no match")
	}
	if m.Kind != domain.WaitPermission || m.Confidence != domain.ConfidenceHigh {
		t.Fatalf("match = %+v", m)
	}
	if len(m.Prompt.Options) != 3 || m.Prompt.Options[2].Inject.Char != '3' {
		t.Fatalf("options = %+v", m.Prompt.Options)
	}
}

func TestFixtureOpencodePermission(t *testing.T) {
	s := replay(t, "fixtures/opencode/0.1/permission.cast")
	m := Opencode().Detect(s, ctx())
	if m == nil {
		t.Fatal("no match")
	}
	if m.Kind != domain.WaitPermission || m.Confidence != domain.ConfidenceHigh {
		t.Fatalf("match = %+v", m)
	}
	if len(m.Prompt.Options) != 3 {
		t.Fatalf("options = %+v", m.Prompt.Options)
	}
	// Position-mapped injects (spec §7.5: arrows, not keys).
	for i, o := range m.Prompt.Options {
		if o.Inject.Kind != KeyArrowSelect || o.Inject.Arrows != i {
			t.Fatalf("option %d inject = %+v", i, o.Inject)
		}
	}
	// The modal sits mid-screen — outside DefaultScope(12)'s bottom
	// band: this is what scan_scope: full buys.
	if _, rows := s.Size(); m.Region.Y0 > rows-12 {
		t.Fatalf("match should sit above the default bottom band: %+v", m.Region)
	}
}

func TestFixtureOpencodeQuestion(t *testing.T) {
	s := replay(t, "fixtures/opencode/0.1/question.cast")
	m := Opencode().Detect(s, ctx())
	if m == nil {
		t.Fatal("no match")
	}
	if m.Kind != domain.WaitOption || len(m.Prompt.Options) != 3 {
		t.Fatalf("match = %+v", m)
	}
}

func TestFixtureDevinOptions(t *testing.T) {
	s := replay(t, "fixtures/devin/1.0/options.cast")
	m := Devin().Detect(s, ctx())
	if m == nil {
		t.Fatal("no match")
	}
	if m.Kind != domain.WaitOption || len(m.Prompt.Options) != 3 {
		t.Fatalf("match = %+v", m)
	}
	if m.Prompt.Options[1].Inject.Char != '2' {
		t.Fatalf("option 1 inject = %+v", m.Prompt.Options[1].Inject)
	}
}

func TestFixtureDevinFreetext(t *testing.T) {
	s := replay(t, "fixtures/devin/1.0/freetext.cast")
	m := Devin().Detect(s, ctx())
	if m == nil {
		t.Fatal("no match")
	}
	if m.Kind != domain.WaitFreetext || m.Confidence != domain.ConfidenceMedium {
		t.Fatalf("match = %+v", m)
	}
}

// Negative fixtures: a busy screen must never fabricate buttons (§8.4 —
// false-positive cost is wrong buttons into a live session).
func TestCuratedNegatives(t *testing.T) {
	for _, a := range []Adapter{Claude(), Codex(), Gemini(), Opencode(), Devin()} {
		s := replay(t, "fixtures/generic/g4/idle.cast") // log scroll + bare $
		if m := a.Detect(s, ctx()); m != nil {
			t.Errorf("%s matched a busy log screen: %+v", a.Pin().Tool, m)
		}
	}
}

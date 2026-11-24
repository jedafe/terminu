package adapter

import (
	"testing"

	"github.com/jedafe/terminu/internal/domain"
	"github.com/jedafe/terminu/internal/screen"
)

// Ref: .teos/specs/adapter-design.md §8 — fixture replay: adapters prove
// patterns against transcripts, never synthetic builder calls alone.
// fixtures/<tool>/<version>/<scenario>.cast replays into the same screen
// model Detect reads in production.

func replay(t *testing.T, path string) *screen.Screen {
	t.Helper()
	s := screen.New(80, 24, 0)
	if err := ReplayCast(path, s); err != nil {
		t.Fatalf("replay %s: %v", path, err)
	}
	return s
}

func TestFixtureAiderOption(t *testing.T) {
	s := replay(t, "fixtures/aider/0.70/option.cast")
	m := Aider().Detect(s, ctx())
	if m == nil {
		t.Fatal("no match")
	}
	if m.Kind != domain.WaitOption || m.Confidence != domain.ConfidenceHigh {
		t.Fatalf("match = %+v", m)
	}
	if len(m.Prompt.Options) != 3 || !m.Prompt.Options[0].Default ||
		m.Prompt.Options[1].Default || m.Prompt.Options[2].Default {
		t.Fatalf("options = %+v", m.Prompt.Options)
	}
	if m.Evidence == "" {
		t.Fatal("evidence mandatory")
	}
}

func TestFixtureG1Yn(t *testing.T) {
	s := replay(t, "fixtures/generic/g1/yn.cast")
	m := DetectGeneric(s, ctx())
	if m == nil {
		t.Fatal("no match")
	}
	if m.Kind != domain.WaitConfirm || m.Confidence != domain.ConfidenceMedium {
		t.Fatalf("match = %+v", m)
	}
	if !m.Prompt.Options[1].Default || m.Prompt.Options[0].Default {
		t.Fatalf("[y/N]: N must be the default — %+v", m.Prompt.Options)
	}
}

func TestFixtureG2Menu(t *testing.T) {
	s := replay(t, "fixtures/generic/g2/menu.cast")
	m := DetectGeneric(s, ctx())
	if m == nil {
		t.Fatal("no match")
	}
	if m.Kind != domain.WaitOption || m.Confidence != domain.ConfidenceMedium {
		t.Fatalf("match = %+v", m)
	}
	if len(m.Prompt.Options) != 3 {
		t.Fatalf("options = %+v", m.Prompt.Options)
	}
	if m.Prompt.Options[1].Inject.Kind != KeyArrowSelect || m.Prompt.Options[1].Inject.Arrows != 1 {
		t.Fatalf("option 1 inject = %+v", m.Prompt.Options[1].Inject)
	}
	if m.Prompt.Title != "Select a deployment target:" {
		t.Fatalf("title = %q", m.Prompt.Title)
	}
}

func TestFixtureG3Question(t *testing.T) {
	s := replay(t, "fixtures/generic/g3/question.cast")
	m := DetectGeneric(s, ctx())
	if m == nil {
		t.Fatal("no match")
	}
	if m.Kind != domain.WaitFreetext || m.Confidence != domain.ConfidenceMedium {
		t.Fatalf("match = %+v", m)
	}
}

// G3/G4 are quiescence-scoped: a bottom-region-dirty trigger on the same
// screen must not manufacture a prompt out of a mid-burst cursor.
func TestFixtureG3GatedByTrigger(t *testing.T) {
	s := replay(t, "fixtures/generic/g3/question.cast")
	c := ctx()
	c.Trigger = "bottom-region"
	if m := DetectGeneric(s, c); m != nil {
		t.Fatalf("non-quiesced trigger matched G3: %+v", m)
	}
}

func TestFixtureG4Idle(t *testing.T) {
	s := replay(t, "fixtures/generic/g4/idle.cast")
	m := DetectGeneric(s, ctx())
	if m == nil {
		t.Fatal("no match")
	}
	if m.State != domain.StateWaitingForInput || m.Kind != domain.WaitUnknown ||
		m.Confidence != domain.ConfidenceLow {
		t.Fatalf("match = %+v", m)
	}
	if m.Prompt != nil {
		t.Fatalf("unknown never carries injectable options: %+v", m.Prompt)
	}
}

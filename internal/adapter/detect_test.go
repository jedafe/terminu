package adapter

import (
	"testing"

	"github.com/jedafe/terminu/internal/domain"
	"github.com/jedafe/terminu/internal/screen"
)

func ctx() *SessionCtx { return &SessionCtx{SessionID: "t", ScanBottomRows: 12} }

func feed(s *screen.Screen, str string) { _, _ = s.Write([]byte(str)) }

func TestAiderMatches(t *testing.T) {
	a := Aider()
	cases := []struct {
		proc string
		argv []string
		want bool
	}{
		{"aider", []string{"aider"}, true},
		{"python3", []string{"python3", "-m", "aider"}, true},
		{"python", []string{"python", "/usr/lib/aider/__main__.py"}, true},
		{"bash", []string{"bash"}, false},
		{"aiderx", []string{"aiderx"}, false},
	}
	for _, c := range cases {
		if got := a.MatchesProcess(c.proc, c.argv); got != c.want {
			t.Errorf("match(%q,%v) = %v want %v", c.proc, c.argv, got, c.want)
		}
	}
}

// Devin's internal todo list renders numbered — it must never surface
// as a waiting-for-input prompt (false positive = wrong inject buttons,
// Rule 7). Status-glyph rows and non-prompt titles both bail to nil.
func TestDevinTodoListIsNotAPrompt(t *testing.T) {
	s := screen.New(80, 24, 0)
	feed(s, "Tasks\r\n1. [x] inspect the repo\r\n2. [ ] apply the fix\r\n3. [ ] commit\r\n")
	if m := Devin().Detect(s, ctx()); m != nil {
		t.Fatalf("todo list must not detect: %+v", m)
	}
	s2 := screen.New(80, 24, 0)
	feed(s2, "working through the plan\r\n1. first step\r\n2. second step\r\n")
	if m := Devin().Detect(s2, ctx()); m != nil {
		t.Fatalf("plain numbered list must not detect: %+v", m)
	}
	s3 := screen.New(80, 24, 0)
	feed(s3, "devin CLI — select organization\r\n1) acme-corp\r\n2) dev-team\r\n")
	m := Devin().Detect(s3, ctx())
	if m == nil || m.Kind != domain.WaitOption {
		t.Fatalf("real option menu should still detect: %+v", m)
	}
}

func TestAiderOptionPrompt(t *testing.T) {
	s := screen.New(80, 24, 0)
	feed(s, "tokens: 4.2k\r\n")
	feed(s, "Add file to the chat? (Y)es/(N)o/(D)on't ask again")
	m := Aider().Detect(s, ctx())
	if m == nil {
		t.Fatal("no match")
	}
	if m.State != domain.StateWaitingForInput || m.Kind != domain.WaitOption {
		t.Fatalf("state=%s kind=%s", m.State, m.Kind)
	}
	if m.Confidence != domain.ConfidenceHigh {
		t.Fatalf("confidence = %s", m.Confidence)
	}
	if m.Prompt == nil || m.Prompt.Title != "Add file to the chat?" {
		t.Fatalf("prompt = %+v", m.Prompt)
	}
	if len(m.Prompt.Options) != 3 {
		t.Fatalf("options = %+v", m.Prompt.Options)
	}
	wantKeys := []string{"y", "n", "d"}
	for i, o := range m.Prompt.Options {
		if o.Inject.Kind != KeyText || o.Inject.Text != wantKeys[i] {
			t.Fatalf("option %d inject = %+v", i, o.Inject)
		}
	}
	if !m.Prompt.Options[0].Default {
		t.Fatal("(Y)es should be marked default")
	}
	if m.Evidence == "" {
		t.Fatal("evidence mandatory")
	}
}

func TestAiderConfirmPrompt(t *testing.T) {
	s := screen.New(80, 24, 0)
	feed(s, "Run shell command? (Y)es/(N)o")
	m := Aider().Detect(s, ctx())
	if m == nil || m.Kind != domain.WaitConfirm {
		t.Fatalf("kind = %+v", m)
	}
}

func TestAiderNegative(t *testing.T) {
	s := screen.New(80, 24, 0)
	feed(s, "diff --git a/f.go b/f.go\r\n+new line\r\n-working\r\nmore output...\r\n")
	if m := Aider().Detect(s, ctx()); m != nil {
		t.Fatalf("busy screen matched: %+v", m)
	}
}

func TestG1BracketToken(t *testing.T) {
	s := screen.New(80, 24, 0)
	feed(s, "Proceed with deletion? [y/N] ")
	m := DetectGeneric(s, ctx())
	if m == nil {
		t.Fatal("no match")
	}
	if m.Kind != domain.WaitConfirm || m.Confidence != domain.ConfidenceMedium {
		t.Fatalf("match = %+v", m)
	}
	if !m.Prompt.Options[1].Default {
		t.Fatal("capital N should be default")
	}
	if m.Prompt.Options[0].Inject.Text != "y" {
		t.Fatalf("yes inject = %+v", m.Prompt.Options[0].Inject)
	}
}

func TestG1ParenWords(t *testing.T) {
	s := screen.New(80, 24, 0)
	feed(s, "Continue? (Y)es/(N)o")
	m := DetectGeneric(s, ctx())
	if m == nil || !m.Prompt.Options[0].Default {
		t.Fatalf("match = %+v", m)
	}
}

func TestG1NoTokenNoMatch(t *testing.T) {
	s := screen.New(80, 24, 0)
	feed(s, "compiling main.go...\r\nlinking...\r\ndone\r\n")
	if m := DetectGeneric(s, ctx()); m != nil {
		t.Fatalf("matched: %+v", m)
	}
}

func TestG1TokenMustBeTrailing(t *testing.T) {
	s := screen.New(80, 24, 0)
	feed(s, "[y/N] was the old prompt, now busy working...\r\nstill busy\r\n")
	if m := DetectGeneric(s, ctx()); m != nil {
		t.Fatalf("mid-line token matched: %+v", m)
	}
}

// Ref: .teos/specs/adapter-design.md §7.6 — aider (line-based prompts).
// Aider prints plain scrolling lines ending in parenthesized options:
//
//	Add file to the chat? (Y)es/(N)o/(D)on't ask again
//	Run shell command?    (Y)es/(N)o/(A)ll
//
// Line-mode: the answer is the option's letter + Enter.
package adapter

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/jedafe/terminu/internal/domain"
	"github.com/jedafe/terminu/internal/screen"
)

// aiderOptGroup matches a /-separated run of ≥2 (X)word tokens —
// "(Y)es/(N)o" or "(Y)es/(N)o/(D)on't".
var aiderOptGroup = regexp.MustCompile(`(?:\(\w\)[\w'-]+)(?:/\s*\(\w\)[\w'-]+)+`)

// aiderOptItem extracts one (X)word inside the group.
var aiderOptItem = regexp.MustCompile(`\((\w)\)([\w'-]+)`)

type aiderAdapter struct{}

// Aider returns the aider CLI adapter.
func Aider() Adapter { return aiderAdapter{} }

func (aiderAdapter) MatchesProcess(proc string, argv []string) bool {
	if proc == "aider" {
		return true
	}
	// `python -m aider` / `python .../aider/__main__.py` invocations.
	if strings.HasPrefix(proc, "python") {
		for i, a := range argv {
			if a == "-m" && i+1 < len(argv) && argv[i+1] == "aider" {
				return true
			}
			if filepath.Base(a) == "aider" || filepath.Base(a) == "__main__.py" && strings.Contains(a, "aider") {
				return true
			}
		}
	}
	return false
}

func (aiderAdapter) Pin() AdapterPin {
	// No real-session .cast fixtures yet — the pin stays empty rather
	// than minting a fake ISO date; the UI badging reads empty as
	// "unverified".
	return AdapterPin{Tool: "aider", Constraint: ">=0.70 <1.0"}
}

func (aiderAdapter) Detect(s *screen.Screen, ctx *SessionCtx) *StateMatch {
	// Scan the §06 default scope; the LAST matching line wins (aider can
	// leave earlier prompts scrolled in the above-cursor region).
	var (
		found     bool
		yHit      int
		line, grp string
	)
	for _, r := range s.DefaultScope(ctx.ScanBottomRows) {
		for y := r.Y0; y < r.Y1; y++ {
			l := s.RowText(y)
			if g := aiderOptGroup.FindString(l); g != "" {
				found, yHit, line, grp = true, y, l, g
			}
		}
	}
	if !found {
		return nil
	}

	items := aiderOptItem.FindAllStringSubmatch(grp, -1)
	opts := make([]PromptOption, 0, len(items))
	for i, it := range items {
		key := it[1]
		label := key + it[2] // "(Y)es" → "Yes", "(D)on't" → "Don't"
		opts = append(opts, PromptOption{
			Label:  label,
			Inject: Text(strings.ToLower(key)), // line-mode: char + Enter
			// Aider's convention is first-option-on-Enter; its letters
			// are all uppercase, so case can't pick a default (marking
			// every option Default made the badge meaningless).
			Default: i == 0,
		})
	}
	// Kind: pure y/n pair → confirm; wider choice → option.
	kind := domain.WaitConfirm
	if len(opts) != 2 {
		kind = domain.WaitOption
	}
	title := strings.TrimSpace(line[:strings.Index(line, grp)])
	return &StateMatch{
		State:      domain.StateWaitingForInput,
		Kind:       kind,
		Confidence: domain.ConfidenceHigh,
		Prompt:     &Prompt{Kind: kind, Title: title, Options: opts},
		Region:     Region{Y0: yHit, Y1: yHit + 1},
		Evidence:   "aider paren-options on row " + strconv.Itoa(yHit) + ": '" + grp + "'",
	}
}

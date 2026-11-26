// Ref: .teos/specs/adapter-design.md §7.4 — gemini (Gemini CLI).
// Bordered confirmation prompts: "Allow execution of: '<cmd>'?" with
// numbered options ("1. Yes, allow once / 2. Yes, allow always /
// 3. No") and "Apply this change?" edit confirmations. Hint: number
// keys.
package adapter

import (
	"strings"

	"github.com/jedafe/terminu/internal/domain"
	"github.com/jedafe/terminu/internal/screen"
)

// geminiTitles are the known confirmation headers — the spec's
// permission prompt plus the edit-apply confirmation.
var geminiTitles = []string{
	"Allow execution of",
	"Apply this change?",
}

type geminiAdapter struct{}

// Gemini returns the Gemini CLI adapter.
func Gemini() Adapter { return geminiAdapter{} }

func (geminiAdapter) MatchesProcess(proc string, argv []string) bool {
	if proc == "gemini" {
		return true
	}
	if strings.Contains(proc, "node") {
		for _, a := range argv {
			if strings.Contains(a, "gemini") {
				return true
			}
		}
	}
	return false
}

func (geminiAdapter) Pin() AdapterPin {
	return AdapterPin{Tool: "gemini", Constraint: ">=0.1 <1.0"}
}

func (geminiAdapter) Detect(s *screen.Screen, ctx *SessionCtx) *StateMatch {
	for _, r := range s.DefaultScope(ctx.ScanBottomRows) {
		for y := r.Y1 - 1; y >= r.Y0; y-- {
			if !numberedOpt.MatchString(s.RowText(y)) {
				continue
			}
			head := y
			for head-1 >= r.Y0 && numberedOpt.MatchString(s.RowText(head-1)) {
				head--
			}
			rows := collectNumbered(s, head, r.Y1)
			if rows == nil || rows[0] != head {
				continue
			}
			// Box-drawing borders may sit between title and options —
			// strip ║│┃| chars; rows that are only border/space (the
			// frame's padding) don't count as the title.
			ty, title := -1, ""
			for yy := head - 1; yy >= 0 && yy >= head-4; yy-- {
				t := strings.Trim(s.RowText(yy), "│║┃| ")
				if t != "" && !strings.HasPrefix(t, "╭") && !strings.HasPrefix(t, "╰") && !strings.HasPrefix(t, "├") {
					ty, title = yy, t
					break
				}
			}
			clean := title
			permission := false
			for _, t := range geminiTitles {
				if strings.HasPrefix(clean, t) {
					permission = true
					break
				}
			}
			kind := domain.WaitOption
			conf := domain.ConfidenceMedium
			if permission {
				kind = domain.WaitPermission
				conf = domain.ConfidenceHigh
			}
			return &StateMatch{
				State: domain.StateWaitingForInput, Kind: kind, Confidence: conf,
				Prompt: &Prompt{Kind: kind, Title: clean,
					Options: numberedOptions(s, rows, true)},
				Region:   Region{Y0: tyOr(head, ty), Y1: rows[len(rows)-1] + 1},
				Evidence: "gemini numbered options under '" + clean + "'",
			}
		}
	}
	return nil
}

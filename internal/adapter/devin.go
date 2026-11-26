// Ref: .teos/specs/adapter-design.md §7.2 — devin (Devin CLI).
// Interactive numbered option lists (org selection, project prompts)
// plus bare `>` free-text questions. Thinner public docs — the pin
// stays unverified and patterns ride the shared numbered/lettered
// grammar at generic-plus-one confidence.
package adapter

import (
	"strings"

	"github.com/jedafe/terminu/internal/domain"
	"github.com/jedafe/terminu/internal/screen"
)

type devinAdapter struct{}

// Devin returns the Devin CLI adapter.
func Devin() Adapter { return devinAdapter{} }

func (devinAdapter) MatchesProcess(proc string, argv []string) bool {
	return proc == "devin"
}

func (devinAdapter) Pin() AdapterPin {
	return AdapterPin{Tool: "devin", Constraint: ">=1.0 <2.0"}
}

func (devinAdapter) Detect(s *screen.Screen, ctx *SessionCtx) *StateMatch {
	for _, r := range s.DefaultScope(ctx.ScanBottomRows) {
		for y := r.Y1 - 1; y >= r.Y0; y-- {
			line := s.RowText(y)
			// Bare `>` prompt: an input marker as the last non-empty
			// line with the cursor parked on it → freetext.
			if strings.TrimSpace(line) == ">" {
				_, cy, vis := s.Cursor()
				if vis && cy == y {
					_, title := findTitle(s, y-1, 3)
					return &StateMatch{
						State: domain.StateWaitingForInput,
						Kind:  domain.WaitFreetext, Confidence: domain.ConfidenceMedium,
						Prompt:   &Prompt{Kind: domain.WaitFreetext, Title: title},
						Region:   Region{Y0: y, Y1: y + 1},
						Evidence: "devin bare '>' input row with cursor",
					}
				}
				continue
			}
			if !numberedOpt.MatchString(line) {
				continue
			}
			if statusRow(line) {
				continue // "1. [x] …" — todo/status row, not a menu option
			}
			head := y
			for head-1 >= r.Y0 && numberedOpt.MatchString(s.RowText(head-1)) {
				head--
			}
			rows := collectNumbered(s, head, r.Y1)
			if rows == nil || rows[0] != head {
				continue
			}
			// Any status row inside the run makes the whole block a
			// status list — devin's internal todo renders numbered and
			// must never become a prompt (Rule 7: nil when uncertain).
			status := false
			for _, ry := range rows {
				if statusRow(s.RowText(ry)) {
					status = true
					break
				}
			}
			ty, title := findTitle(s, head-1, 3)
			if status || title == "" || !promptishTitle(title) {
				continue
			}
			return &StateMatch{
				State: domain.StateWaitingForInput,
				Kind:  domain.WaitOption, Confidence: domain.ConfidenceMedium,
				Prompt: &Prompt{Kind: domain.WaitOption, Title: title,
					Options: numberedOptions(s, rows, true)},
				Region:   Region{Y0: tyOr(head, ty), Y1: rows[len(rows)-1] + 1},
				Evidence: "devin numbered options under '" + title + "'",
			}
		}
	}
	return nil
}

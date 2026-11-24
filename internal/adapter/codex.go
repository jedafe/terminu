// Ref: .teos/specs/adapter-design.md §7.3 — codex (Codex CLI).
// The exec-approval dialog interrupts the composer: a command summary
// line, then letter-keyed options —
//
//	codex wants to run: git commit -m "wip"
//	› Yes, proceed (y)
//	  Yes, and don't ask again for this command (a)
//	  No (n)
//
// Inject: the bracketed letter key; arrow-select is the fallback when
// a numbered variant appears. Highest spec drift risk — unverified pin.
package adapter

import (
	"strings"

	"github.com/jedafe/terminu/internal/domain"
	"github.com/jedafe/terminu/internal/screen"
)

// codexTitles hint at an approval context — a lettered block under one
// of these is a permission wait, not a stray "(x)" token.
var codexTitles = []string{
	"wants to run",
	"Approve",
	"Run command",
	"Allow",
}

type codexAdapter struct{}

// Codex returns the Codex CLI adapter.
func Codex() Adapter { return codexAdapter{} }

func (codexAdapter) MatchesProcess(proc string, argv []string) bool {
	if proc == "codex" {
		return true
	}
	if strings.Contains(proc, "node") {
		for _, a := range argv {
			if strings.Contains(a, "codex") {
				return true
			}
		}
	}
	return false
}

func (codexAdapter) Pin() AdapterPin {
	return AdapterPin{Tool: "codex", Constraint: ">=0.30 <1.0"}
}

func (codexAdapter) Detect(s *screen.Screen, ctx *SessionCtx) *StateMatch {
	for _, r := range s.DefaultScope(ctx.ScanBottomRows) {
		for y := r.Y1 - 1; y >= r.Y0; y-- {
			m := letterOpt.FindStringSubmatch(s.RowText(y))
			if m == nil {
				continue
			}
			// Walk to the block head (contiguous letter-keyed rows).
			head := y
			for head-1 >= r.Y0 && letterOpt.MatchString(s.RowText(head-1)) {
				head--
			}
			var rows []int
			for yy := head; yy < r.Y1 && letterOpt.MatchString(s.RowText(yy)); yy++ {
				rows = append(rows, yy)
			}
			if len(rows) < 2 || rows[0] != head {
				continue
			}
			ty, title := findTitle(s, head-1, 3)
			permission := false
			for _, t := range codexTitles {
				if strings.Contains(title, t) {
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
			opts := make([]PromptOption, len(rows))
			for i, ry := range rows {
				mm := letterOpt.FindStringSubmatch(s.RowText(ry))
				opts[i] = PromptOption{
					Label:  strings.TrimSpace(mm[1]),
					Inject: Char(mm[2][0] | 0x20), // lowercase letter key
				}
			}
			return &StateMatch{
				State: domain.StateWaitingForInput, Kind: kind, Confidence: conf,
				Prompt:   &Prompt{Kind: kind, Title: title, Options: opts},
				Region:   Region{Y0: tyOr(head, ty), Y1: rows[len(rows)-1] + 1},
				Evidence: "codex letter options under '" + title + "'",
			}
		}
	}
	return nil
}

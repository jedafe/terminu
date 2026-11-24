// Ref: .teos/specs/adapter-design.md §7.1 — claude (Claude Code).
// TUI permission prompts render as a title line + numbered options:
//
//	Do you want to proceed?
//	1. Yes
//	2. Yes, and don't ask again for git status
//	3. No
//
// Also cataloged: trust dialog ("Do you trust the files in this
// folder?"), plan approval ("Approve plan?"), MCP tool approval.
// Inject: the option's number key; Esc maps to the "No" option
// client-side. High drift — the pin stays unverified until a real
// transcript fixture confirms the layout.
package adapter

import (
	"strings"

	"github.com/jedafe/terminu/internal/domain"
	"github.com/jedafe/terminu/internal/screen"
)

// claudeTitles are the known permission-prompt headers (spec table).
// A title hit upgrades a numbered block from option/medium to
// permission/high — the title is what makes the wait a *permission*.
var claudeTitles = []string{
	"Do you want to proceed?",
	"Do you trust the files in this folder?",
	"Approve plan?",
}

type claudeAdapter struct{}

// Claude returns the Claude Code adapter.
func Claude() Adapter { return claudeAdapter{} }

func (claudeAdapter) MatchesProcess(proc string, argv []string) bool {
	if proc == "claude" {
		return true
	}
	// `node .../claude` — the CLI is a node program.
	if strings.Contains(proc, "node") {
		for _, a := range argv {
			if strings.Contains(a, "claude") {
				return true
			}
		}
	}
	return false
}

func (claudeAdapter) Pin() AdapterPin {
	return AdapterPin{Tool: "claude", Constraint: ">=1.0 <2.0"}
}

func (claudeAdapter) Detect(s *screen.Screen, ctx *SessionCtx) *StateMatch {
	// Bottom-up over the default scope: the LAST numbered block wins —
	// earlier prompts scroll into the above-cursor region.
	for _, r := range s.DefaultScope(ctx.ScanBottomRows) {
		for y := r.Y1 - 1; y >= r.Y0; y-- {
			if !numberedOpt.MatchString(s.RowText(y)) {
				continue
			}
			// y is inside a block — walk to its head then collect.
			head := y
			for head-1 >= r.Y0 && numberedOpt.MatchString(s.RowText(head-1)) {
				head--
			}
			rows := collectNumbered(s, head, r.Y1)
			if rows == nil || rows[0] != head {
				continue // stray numbered line, not a block head
			}
			ty, title := findTitle(s, head-1, 3)
			permission := false
			for _, t := range claudeTitles {
				if strings.HasPrefix(title, t) {
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
				Prompt: &Prompt{Kind: kind, Title: title,
					Options: numberedOptions(s, rows, true)},
				Region:   Region{Y0: tyOr(head, ty), Y1: rows[len(rows)-1] + 1},
				Evidence: "claude numbered options under '" + title + "'",
			}
		}
	}
	return nil
}

func tyOr(fallback, y int) int {
	if y < 0 {
		return fallback
	}
	return y
}

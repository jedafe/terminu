// Ref: .teos/specs/adapter-design.md §7.5 — opencode (OpenCode).
// Full-TUI agent: a `Permission required` modal can overlay anywhere on
// the grid (center or bottom), so this adapter declares
// `scan_scope: full` — it walks the whole screen, not DefaultScope.
// Hypothesis layout:
//
//	Permission required
//	bash: git status
//	  Allow    Always    Deny        (button row), or
//	  ❯ Allow / Always / Reject      (select list)
//
// Hint: arrow-select + Enter — the modal options take position, not
// keys (spec: "position mapping rather than keys"). Highest drift risk
// of the launch set — pin stays unverified.
package adapter

import (
	"regexp"
	"strings"

	"github.com/jedafe/terminu/internal/domain"
	"github.com/jedafe/terminu/internal/screen"
)

// opencodeTitle anchors the permission modal — the spec's exact
// hypothesis header; border/gutter chars may precede it.
var opencodeTitle = regexp.MustCompile(`(?i)permission required`)

// opencodeBtn matches a single modal option token, standalone or in a
// `❯ label` select row — Allow / Always / Deny / Reject.
var opencodeBtn = regexp.MustCompile(`^\s*[│║┃|]?\s*[❯>]?\s*(Allow|Always|Deny|Reject)\b`)

type opencodeAdapter struct{}

// Opencode returns the opencode adapter (scan_scope: full).
func Opencode() Adapter { return opencodeAdapter{} }

func (opencodeAdapter) MatchesProcess(proc string, argv []string) bool {
	if proc == "opencode" || strings.HasSuffix(proc, "/opencode") {
		return true
	}
	// npm/bun install (`opencode-ai`): the foreground process is the
	// runtime, the tool identity lives in argv.
	if proc == "node" || proc == "bun" || strings.HasSuffix(proc, "/node") || strings.HasSuffix(proc, "/bun") {
		for _, a := range argv {
			if strings.Contains(a, "opencode") {
				return true
			}
		}
	}
	return false
}

func (opencodeAdapter) Pin() AdapterPin {
	return AdapterPin{Tool: "opencode", Constraint: ">=0.1 <1.0"}
}

func (opencodeAdapter) Detect(s *screen.Screen, ctx *SessionCtx) *StateMatch {
	_, rows := s.Size()
	full := screen.ScanRange{Y0: 0, Y1: rows} // scan_scope: full — the modal overlays anywhere

	// Pass 1: the permission modal — title row, then its option block.
	for y := full.Y1 - 1; y >= full.Y0; y-- {
		if !opencodeTitle.MatchString(s.RowText(y)) {
			continue
		}
		// Options sit below the title (tool+command rows in between);
		// bounded hop keeps a stale modal higher up from claiming a
		// distant option row.
		if opts, y1 := collectOpencodeOpts(s, y+1, min(y+8, full.Y1)); len(opts) > 0 {
			return &StateMatch{
				State: domain.StateWaitingForInput,
				Kind:  domain.WaitPermission, Confidence: domain.ConfidenceHigh,
				Prompt:   &Prompt{Kind: domain.WaitPermission, Title: "Permission required", Options: opts},
				Region:   Region{Y0: y, Y1: y1},
				Evidence: "opencode permission modal",
			}
		}
	}

	// Pass 2: the `question` tool's selectable list — a numbered block
	// anywhere under a `?`-suffixed title.
	for _, r := range []screen.ScanRange{full} {
		for y := r.Y1 - 1; y >= r.Y0; y-- {
			if !numberedOpt.MatchString(s.RowText(y)) {
				continue
			}
			head := y
			for head-1 >= r.Y0 && numberedOpt.MatchString(s.RowText(head-1)) {
				head--
			}
			blk := collectNumbered(s, head, r.Y1)
			if blk == nil || blk[0] != head {
				continue
			}
			ty, title := findTitle(s, head-1, 3)
			if !strings.HasSuffix(strings.TrimRight(title, " "), "?") {
				continue // question lists end in '?'; permission uses the modal
			}
			return &StateMatch{
				State: domain.StateWaitingForInput,
				Kind:  domain.WaitOption, Confidence: domain.ConfidenceMedium,
				Prompt: &Prompt{Kind: domain.WaitOption, Title: title,
					Options: numberedOptions(s, blk, false)},
				Region:   Region{Y0: tyOr(head, ty), Y1: blk[len(blk)-1] + 1},
				Evidence: "opencode question options under '" + title + "'",
			}
		}
	}
	return nil
}

// collectOpencodeOpts gathers the modal's Allow/Always/Deny|Reject
// tokens — contiguous button rows become one option each (position =
// arrow count); a single row holding all tokens is the button-row
// layout, options in token order.
func collectOpencodeOpts(s *screen.Screen, y0, y1 int) ([]PromptOption, int) {
	var opts []PromptOption
	for y := y0; y < y1; y++ {
		row := s.RowText(y)
		// Button-row layout first: one line holding ≥2 labels —
		// "❯ Allow    Always    Deny" is a row-start Allow match that
		// must expand to every present token, not emit Allow alone.
		have := make(map[string]bool, 4)
		for _, tok := range []string{"Allow", "Always", "Deny", "Reject"} {
			if strings.Contains(row, tok) {
				have[tok] = true
			}
		}
		if len(have) >= 2 {
			for _, tok := range []string{"Allow", "Always", "Deny", "Reject"} {
				if have[tok] {
					opts = append(opts, PromptOption{
						Label: tok,
						Hint:  arrowHint(len(opts)),
					})
				}
			}
			return opts, y + 1
		}
		if m := opencodeBtn.FindStringSubmatch(row); m != nil {
			opts = append(opts, PromptOption{
				Label: m[1],
				Hint:  arrowHint(len(opts)),
			})
			continue
		}
		if len(opts) > 0 {
			return opts, y // first non-option row ends the block
		}
	}
	return nil, 0
}

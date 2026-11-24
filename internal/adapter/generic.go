// Ref: .teos/specs/adapter-design.md §7.7 — generic fallback tiers.
// Runs for unadapted processes. G1–G3 → medium confidence; G4 → low
// ("may be waiting", never buttons — AGENTS.md: never promote a
// low-confidence wait).
package adapter

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/jedafe/terminu/internal/domain"
	"github.com/jedafe/terminu/internal/screen"
)

// g1Re matches a trailing y/n token: "(y/n)", "[y/N]", "(yes/no)",
// "(Y)es/(N)o" — the last thing on the line (trailing space tolerated).
var g1Re = regexp.MustCompile(`(?:\(([yY])\)(?:es)?/\(([nN])\)(?:o)?|[\(\[]([yY])(?:es)?/([nN])(?:o)?[\)\]])\s*$`)

// g2Re matches a list item: "1. label", "2) label", "a. label", "B) label".
var g2Re = regexp.MustCompile(`^\s*([0-9]+|[a-zA-Z])[.)]\s+\S`)

// DetectGeneric runs the tiers in spec order, first match wins; nil when
// nothing matches — the caller decides the working/unknown fallback.
func DetectGeneric(s *screen.Screen, ctx *SessionCtx) *StateMatch {
	if m := detectG1(s, ctx); m != nil {
		return m
	}
	if m := detectG2(s, ctx); m != nil {
		return m
	}
	if m := detectG3(s, ctx); m != nil {
		return m
	}
	return detectG4(s, ctx)
}

// quiesced reports whether the scan was authorized by silence or a human
// looking — G3/G4 are quiescence-scoped per the trigger table, so a
// mid-burst cursor can never masquerade as an idle prompt.
func quiesced(ctx *SessionCtx) bool {
	return ctx.Trigger == "quiescence" || ctx.Trigger == "on_demand" || ctx.Trigger == ""
}

// G1: trailing y/n token on the last non-empty line of the screen →
// confirm. "Bottom line" in the spec means the bottom-most content —
// on a short screen that's a top row, not a row inside the band.
func detectG1(s *screen.Screen, ctx *SessionCtx) *StateMatch {
	_, rows := s.Size()
	for y := rows - 1; y >= 0; y-- {
		line := s.RowText(y)
		if line == "" {
			continue
		}
		m := g1Re.FindStringSubmatch(line)
		if m == nil {
			return nil // last non-empty line has no token — done
		}
		yes, no := m[1]+m[3], m[2]+m[4]
		yesDef := yes == "Y"
		noDef := no == "N"
		return &StateMatch{
			State:      domain.StateWaitingForInput,
			Kind:       domain.WaitConfirm,
			Confidence: domain.ConfidenceMedium,
			Prompt: &Prompt{
				Kind:  domain.WaitConfirm,
				Title: strings.TrimSpace(g1Re.ReplaceAllString(line, "")),
				Options: []PromptOption{
					{Label: "Yes", Inject: Text("y"), Default: yesDef},
					{Label: "No", Inject: Text("n"), Default: noDef},
				},
			},
			Region:   Region{Y0: y, Y1: y + 1},
			Evidence: "G1 y/n token on row " + strconv.Itoa(y) + ": '" + m[0] + "'",
		}
	}
	return nil
}

// G2: numbered/lettered list in the bottom region with the cursor at or
// below it → option (arrow-select inject). The list is the last
// contiguous run of ≥2 item lines in the band; the option above the
// block supplies the title when one exists.
func detectG2(s *screen.Screen, ctx *SessionCtx) *StateMatch {
	_, rows := s.Size()
	band := ctx.ScanBottomRows
	if band <= 0 || band > rows {
		band = rows
	}
	_, cy, vis := s.Cursor()
	if !vis {
		return nil
	}
	// Collect item rows in the bottom band, then take the last
	// contiguous run — a lone "1." line is a numbered paragraph, not a menu.
	var items []int
	for y := rows - band; y < rows; y++ {
		if g2Re.MatchString(s.RowText(y)) {
			items = append(items, y)
		}
	}
	if len(items) < 2 {
		return nil
	}
	runStart := len(items) - 1
	for runStart > 0 && items[runStart-1] == items[runStart]-1 {
		runStart--
	}
	items = items[runStart:]
	if len(items) < 2 {
		return nil
	}
	// "Cursor at end": sits inside the list or just below it (≤2 rows
	// past the last item — a prompt echo or an empty separator row).
	last := items[len(items)-1]
	if cy < items[0] || cy > last+2 {
		return nil
	}
	title := ""
	if last > 0 {
		for y := items[0] - 1; y >= 0 && y >= items[0]-2; y-- {
			if t := strings.TrimSpace(s.RowText(y)); t != "" {
				title = t
				break
			}
		}
	}
	opts := make([]PromptOption, len(items))
	for i, y := range items {
		opts[i] = PromptOption{
			Label:  strings.TrimSpace(s.RowText(y)),
			Inject: ArrowSelect(i),
		}
	}
	return &StateMatch{
		State:      domain.StateWaitingForInput,
		Kind:       domain.WaitOption,
		Confidence: domain.ConfidenceMedium,
		Prompt:     &Prompt{Kind: domain.WaitOption, Title: title, Options: opts},
		Region:     Region{Y0: items[0], Y1: last + 1},
		Evidence:   fmt.Sprintf("G2 %d-option list rows %d–%d, cursor row %d", len(items), items[0], last, cy),
	}
}

// G3: `?` on the bottom line + quiescence + cursor present → freetext.
// Same "bottom-most content" reading as G1.
func detectG3(s *screen.Screen, ctx *SessionCtx) *StateMatch {
	if !quiesced(ctx) {
		return nil
	}
	_, rows := s.Size()
	_, _, vis := s.Cursor()
	if !vis {
		return nil
	}
	for y := rows - 1; y >= 0; y-- {
		line := s.RowText(y)
		if line == "" {
			continue
		}
		if !strings.HasSuffix(strings.TrimRight(line, " "), "?") {
			return nil
		}
		return &StateMatch{
			State:      domain.StateWaitingForInput,
			Kind:       domain.WaitFreetext,
			Confidence: domain.ConfidenceMedium,
			Prompt:     &Prompt{Kind: domain.WaitFreetext, Title: strings.TrimSpace(line)},
			Region:     Region{Y0: y, Y1: y + 1},
			Evidence:   "G3 '?' on bottom line row " + strconv.Itoa(y) + " (quiesced)",
		}
	}
	return nil
}

// G4: quiescence + cursor at the bottom input line only → the honest
// minimum: waiting{unknown}, low confidence, no buttons — the overlay
// shows "may be waiting" and nothing injectable.
func detectG4(s *screen.Screen, ctx *SessionCtx) *StateMatch {
	if !quiesced(ctx) {
		return nil
	}
	_, rows := s.Size()
	_, cy, vis := s.Cursor()
	if !vis || cy < rows-1 {
		return nil
	}
	return &StateMatch{
		State:      domain.StateWaitingForInput,
		Kind:       domain.WaitUnknown,
		Confidence: domain.ConfidenceLow,
		Region:     Region{Y0: cy, Y1: cy + 1},
		Evidence:   "G4 quiesced with cursor at input line (row " + strconv.Itoa(cy) + ")",
	}
}

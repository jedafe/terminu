// Ref: .teos/specs/adapter-design.md §7.7 — generic fallback tiers.
// Runs for unadapted processes. G1–G3 → medium confidence; G4 → low
// ("may be waiting", never buttons). Tiers land incrementally — G2/G3/G4
// arrive with their fixture-verified patterns (roadmap 2.17).
package adapter

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/jedafe/terminu/internal/domain"
	"github.com/jedafe/terminu/internal/screen"
)

// g1Re matches a trailing y/n token: "(y/n)", "[y/N]", "(yes/no)",
// "(Y)es/(N)o" — the last thing on the line (trailing space tolerated).
var g1Re = regexp.MustCompile(`(?:\(([yY])\)(?:es)?/\(([nN])\)(?:o)?|[\(\[]([yY])(?:es)?/([nN])(?:o)?[\)\]])\s*$`)

// DetectGeneric runs the implemented generic tiers in order; nil when
// nothing matches — the caller decides the working/unknown fallback.
func DetectGeneric(s *screen.Screen, ctx *SessionCtx) *StateMatch {
	return detectG1(s, ctx)
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

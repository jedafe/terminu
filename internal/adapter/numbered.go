// Shared extraction for TUI-style numbered option prompts — claude and
// gemini both render `N. label` rows under a title line; codex/devin
// variants ride the same helpers. Per spec §7 every pattern here is a
// hypothesis until a real transcript verifies it (fixture pins stay
// unverified).
package adapter

import (
	"regexp"
	"strings"

	"github.com/jedafe/terminu/internal/screen"
)

// numberedOpt matches a TUI option row: "1. Yes", "2) No" — digits
// only (letters belong to generic G2's wider grammar). One optional
// leading box-drawing char tolerates bordered prompts (gemini's ║│
// frames wrap the options, not just the title).
var numberedOpt = regexp.MustCompile(`^\s*[│║┃|]?\s*(\d+)[.)]\s+(\S.*)$`)

// letterOpt matches a letter-keyed option row — codex's family:
// "› Yes, proceed (y)", "No (n)", "[a] always". The trailing or
// leading bracket holds the inject key.
var letterOpt = regexp.MustCompile(`^\s*[›>●*·-]?\s*(.+?)\s*[\(\[]([a-zA-Z])[\)\]]\s*$`)

// collectNumbered scans rows [y0, y1) for a contiguous run of
// numberedOpt rows starting anywhere — the run is the option block.
// ≥2 required: a lone "1." is a paragraph, not a menu.
func collectNumbered(s *screen.Screen, y0, y1 int) []int {
	var run []int
	for y := y0; y < y1; y++ {
		if numberedOpt.MatchString(s.RowText(y)) {
			run = append(run, y)
			continue
		}
		if len(run) > 0 {
			break // first gap after the run ends the block
		}
	}
	if len(run) < 2 {
		return nil
	}
	return run
}

// numberedOptions renders the option rows into PromptOptions — number
// key per spec inject ("1" selects row 1; arrow-select is the fallback
// when a tool has no number binding).
func numberedOptions(s *screen.Screen, rows []int, keyInj bool) []PromptOption {
	opts := make([]PromptOption, len(rows))
	for i, y := range rows {
		m := numberedOpt.FindStringSubmatch(s.RowText(y))
		o := PromptOption{Label: strings.TrimSpace(m[0]), Inject: ArrowSelect(i)}
		if keyInj {
			o.Inject = Char(m[1][0])
		}
		opts[i] = o
	}
	return opts
}

// statusGlyphRe leads todo/status rows: "[x]", "[ ]", "•", "✓", "→",
// braille spinners — a numbered task list is a status display, not an
// interactive menu. Checked against the text after the N./N) marker.
var statusGlyphRe = regexp.MustCompile(`^[\[\(✓✔☐□■▪◐●○◌→▶*•·~⠀-⣿-]`)

// statusRow reports whether a numberedOpt-matching line is really a
// todo/status entry — "1. [x] write tests", "2. → running build".
func statusRow(line string) bool {
	m := numberedOpt.FindStringSubmatch(line)
	return m != nil && statusGlyphRe.MatchString(m[2])
}

// promptTitleRe marks a title that reads as a prompt — a question, an
// instruction ending ':', or a select-word. A "Todos"/"Plan"-style
// heading fails it, which is the honest outcome for a non-interactive
// list (Rule 7: nil when uncertain).
var promptTitleRe = regexp.MustCompile(`(?i)\b(select|choose|pick|which|what|enter|confirm|proceed|continue|approve|allow)\b|[?:]\s*$`)

func promptishTitle(title string) bool { return promptTitleRe.MatchString(title) }

// findTitle scans upward from y for the first non-empty line — the
// question a numbered/lettered block answers. bounded hops keeps a
// sparse box-drawing row from swallowing the real title.
func findTitle(s *screen.Screen, y int, hops int) (int, string) {
	for i := 0; i < hops && y >= 0; i, y = i+1, y-1 {
		if t := strings.TrimSpace(s.RowText(y)); t != "" {
			return y, t
		}
	}
	return -1, ""
}

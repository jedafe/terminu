// Ref: .teos/sections/06-screen-interpreter-adapters.md,
// .teos/specs/adapter-design.md §5 — the adapter contract (Rule 7).
//
// Adapters are pure functions over the screen model: no I/O, no PTY
// access — Detect() is callable only through the trigger gate. nil is
// "not confident" — a false negative degrades to the raw pane, a false
// positive injects wrong buttons into a live session. Every match
// carries Evidence; every adapter declares a version Pin.
package adapter

import (
	"github.com/jedafe/terminu/internal/domain"
	"github.com/jedafe/terminu/internal/proc"
	"github.com/jedafe/terminu/internal/screen"
)

// Adapter is the per-tool contract. Implementations must be pure.
type Adapter interface {
	// MatchesProcess binds by foreground identity — which CLI owns this
	// session's PTY group (e.g. "devin", "claude").
	MatchesProcess(proc string, argv []string) bool

	// Detect inspects the screen model; nil = "not confident". Gate-only
	// invocation, may be expensive-ish. Never writes to the session.
	Detect(s *screen.Screen, ctx *SessionCtx) *StateMatch

	// Pin declares the tool-version coverage the patterns were verified
	// against — mismatch runs anyway but badges "unverified".
	Pin() AdapterPin
}

// SessionCtx carries what Detect may know about the session beyond the
// screen — the bound foreground process, the configured scan scope, and
// which gate trigger authorized this scan (generic tiers G3/G4 are
// quiescence-scoped per the spec's trigger table).
type SessionCtx struct {
	SessionID      string
	FG             proc.Info
	ScanBottomRows int    // TU_SCAN_BOTTOM_ROWS — scope for DefaultScope()
	Trigger        string // gate.Reason.String() — "quiescence", "on_demand", ...
}

// AdapterPin is a version fingerprint (UI: "patterns: devin-cli ≥1.x,
// verified <date>"). Constraint is the declared coverage range.
type AdapterPin struct {
	Tool       string `json:"tool"`        // e.g. "devin"
	Constraint string `json:"constraint"`  // e.g. ">=1.0 <2.0"
	VerifiedAt string `json:"verified_at"` // ISO date of last fixture verification
}

// StateMatch is a Detect() result: classified state + extraction.
type StateMatch struct {
	State      domain.SessionState `json:"state"`          // working | waiting-for-input | done | error
	Kind       domain.WaitKind     `json:"kind,omitempty"` // carried when waiting-for-input
	Confidence domain.Confidence   `json:"confidence"`
	Prompt     *Prompt             `json:"prompt,omitempty"` // extracted question (nil if none)
	Region     Region              `json:"region"`           // where on screen it was found
	Evidence   string              `json:"evidence"`         // what matched — mandatory, never empty
}

// Region is the half-open row range [Y0, Y1) where the match was found.
type Region struct {
	Y0 int `json:"y0"`
	Y1 int `json:"y1"`
}

// Prompt is an extracted question + options.
type Prompt struct {
	Kind    domain.WaitKind `json:"kind"`    // permission | option | confirm | freetext
	Title   string          `json:"title"`   // "Do you want to proceed?"
	Options []PromptOption  `json:"options"` // options/confirm/permission; nil for freetext
}

// PromptOption is one selectable answer.
type PromptOption struct {
	Label   string    `json:"label"`   // "1. Yes"
	Inject  Keystroke `json:"inject"`  // what answering sends to the PTY
	Default bool      `json:"default"` // preselected option
}

// KeystrokeKind tags the inject variant.
type KeystrokeKind string

const (
	KeyChar        KeystrokeKind = "char"         // single byte ('y', '1', 'a')
	KeyText        KeystrokeKind = "text"         // free-text reply + Enter
	KeyArrowSelect KeystrokeKind = "arrow_select" // arrows ×N + Enter (numbered TUIs)
	KeyEscape      KeystrokeKind = "escape"       // cancel/deny
)

// Keystroke is what answering a prompt sends — human-invoked only
// (ADR-001: the overlay never auto-injects).
type Keystroke struct {
	Kind   KeystrokeKind `json:"kind"`
	Char   byte          `json:"char,omitempty"`
	Text   string        `json:"text,omitempty"`
	Arrows int           `json:"arrows,omitempty"` // arrow-downs for KeyArrowSelect
}

func Char(c byte) Keystroke   { return Keystroke{Kind: KeyChar, Char: c} }
func Text(s string) Keystroke { return Keystroke{Kind: KeyText, Text: s} }
func ArrowSelect(n int) Keystroke {
	return Keystroke{Kind: KeyArrowSelect, Arrows: n}
}

var Escape = Keystroke{Kind: KeyEscape}

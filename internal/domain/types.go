// Ref: .teos/conventions/session-state-catalog.md, .teos/sections/03-session-management.md
// Host-agnostic domain types — reused by the future desktop sidecar (ADR-002).
package domain

import (
	"strings"
	"time"
)

// Session lifecycle states — the canonical strings from session-state-catalog.
type SessionState string

const (
	StateStarting        SessionState = "starting"
	StateWorking         SessionState = "working"
	StateWaitingForInput SessionState = "waiting-for-input"
	StateDone            SessionState = "done"
	StateError           SessionState = "error"
	StateDetached        SessionState = "detached" // overlay flag, not a process state
	StateEnded           SessionState = "ended"    // process lost on restart
	StateUndetectable    SessionState = "undetectable"
)

// WaitKind is the kind a waiting-for-input state carries (catalog §kinds).
type WaitKind string

const (
	WaitPermission WaitKind = "permission" // tool asking to allow an action
	WaitOption     WaitKind = "option"     // numbered/lettered choices
	WaitConfirm    WaitKind = "confirm"    // y/n confirmation
	WaitFreetext   WaitKind = "freetext"   // open question expecting a reply
	WaitUnknown    WaitKind = "unknown"    // heuristic-only wait — no buttons
)

// Confidence grades a detection match (catalog §confidence).
type Confidence string

const (
	ConfidenceHigh   Confidence = "high"   // adapter pattern matched
	ConfidenceMedium Confidence = "medium" // generic tier G1–G3
	ConfidenceLow    Confidence = "low"    // G4 heuristic only
)

// Execution targets — {target} syntax on spawn (api-routes-catalog).
type Target struct {
	Kind string `json:"kind"` // local | ssh | docker
	ID   string `json:"id"`   // profile/container id; "" for local
}

func ParseTarget(s string) Target {
	kind, id, found := strings.Cut(s, ":")
	if !found {
		return Target{Kind: s}
	}
	return Target{Kind: kind, ID: id}
}

func (t Target) String() string {
	if t.ID == "" {
		return t.Kind
	}
	return t.Kind + ":" + t.ID
}

// Session is the registry record — one row per owned PTY.
type Session struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	Target      Target       `json:"target"`
	Cwd         string       `json:"cwd"`
	Shell       string       `json:"shell"`
	State       SessionState `json:"state"`
	ExitCode    *int         `json:"exit_code,omitempty"`
	WorkspaceID *int64       `json:"workspace_id,omitempty"`
	Cols        int          `json:"cols"`
	Rows        int          `json:"rows"`
	CreatedAt   time.Time    `json:"created_at"`
	DetachedAt  *time.Time   `json:"detached_at,omitempty"`
	EndedAt     *time.Time   `json:"ended_at,omitempty"`
	// Detached is the overlay flag from the state catalog: true when no
	// client has been attached longer than TU_RECONNECT_WINDOW_S. The
	// process state (working/waiting) still applies underneath it.
	Detached bool `json:"detached,omitempty"`
}

// SpawnRequest is POST /api/sessions.
type SpawnRequest struct {
	Target string `json:"target"`
	Cwd    string `json:"cwd"`
	Shell  string `json:"shell"`
	Name   string `json:"name"`
	Cols   int    `json:"cols"`
	Rows   int    `json:"rows"`
}

// Workspace is a saved layout — views over sessions, not processes.
type Workspace struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Layout      string    `json:"layout"` // JSON: panes/tabs → session ids
	LastSession string    `json:"last_open_session,omitempty"`
	UpdatedAt   time.Time `json:"updated_at"`
}

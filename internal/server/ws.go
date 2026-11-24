// Ref: .teos/conventions/websocket-message-catalog.md
// Session socket: attach{last_seq} → hello{next_seq} → binary data frames
// (8-byte seq prefix + payload) / input / resize / detach / eof / error.
package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/coder/websocket"

	"github.com/jedafe/terminu/internal/adapter"
	"github.com/jedafe/terminu/internal/session"
)

// c→s control frames (JSON). Binary c→s frames = terminal input.
type ctl struct {
	Type    string `json:"type"`
	LastSeq uint64 `json:"last_seq,omitempty"`
	Cols    int    `json:"cols,omitempty"`
	Rows    int    `json:"rows,omitempty"`
}

// s→c control frames. Binary s→c frames = seq-prefixed PTY output.
type helloMsg struct {
	Type      string `json:"type"`
	SessionID string `json:"session_id"`
	Cols      int    `json:"cols"`
	Rows      int    `json:"rows"`
	State     string `json:"state"`
	NextSeq   uint64 `json:"next_seq"`
}
type eofMsg struct {
	Type     string `json:"type"`
	ExitCode int    `json:"exit_code"`
}
type errMsg struct {
	Type    string `json:"type"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Ref: .teos/conventions/websocket-message-catalog.md — detection frames.
type stateMsg struct {
	Type       string `json:"type"`
	State      string `json:"state"`
	Kind       string `json:"kind,omitempty"`
	Confidence string `json:"confidence,omitempty"`
	Evidence   string `json:"evidence,omitempty"`
}
type promptMsg struct {
	Type    string                 `json:"type"`
	Kind    string                 `json:"kind"`
	Title   string                 `json:"title"`
	Options []adapter.PromptOption `json:"options"`
}
type promptClearMsg struct {
	Type   string         `json:"type"`
	Region adapter.Region `json:"region"`
}

// sendStateFrames maps one detection transition onto the catalog frames:
// always `state`; `prompt` when a prompt is present; `prompt_clear` when
// the previously shown prompt is gone. Returns the open-prompt region.
func sendStateFrames(c *websocket.Conn, u session.StateUpdate, pending *adapter.Region) (*adapter.Region, error) {
	sm := stateMsg{Type: "state", State: string(u.State)}
	if u.Match != nil {
		sm.Kind = string(u.Match.Kind)
		sm.Confidence = string(u.Match.Confidence)
		sm.Evidence = u.Match.Evidence
	}
	if err := sendCtl(c, sm); err != nil {
		return pending, err
	}
	if u.Match != nil && u.Match.Prompt != nil {
		pending = &u.Match.Region
		return pending, sendCtl(c, promptMsg{
			Type: "prompt", Kind: string(u.Match.Prompt.Kind),
			Title: u.Match.Prompt.Title, Options: u.Match.Prompt.Options,
		})
	}
	if pending != nil {
		err := sendCtl(c, promptClearMsg{Type: "prompt_clear", Region: *pending})
		return nil, err
	}
	return pending, nil
}

func (s *Server) wsSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	sess, err := s.mgr.Attach(id)
	if err != nil {
		writeErr(w, 404, "session_not_found", "no live session")
		return
	}
	if !s.originOK(r) {
		writeErr(w, 403, "ws_upgrade_failed", "origin not allowed")
		return
	}
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		CompressionMode: websocket.CompressionDisabled,
	})
	if err != nil {
		return
	}
	defer c.CloseNow()
	c.SetReadLimit(int64(s.cfg.WSMaxFrameKB) * 1024)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	// First frame must be attach{last_seq} — the resync contract.
	_, first, err := c.Read(ctx)
	if err != nil {
		return
	}
	var att ctl
	if err := json.Unmarshal(first, &att); err != nil || att.Type != "attach" {
		_ = sendCtl(c, errMsg{Type: "error", Code: "protocol", Message: "first frame must be attach{last_seq}"})
		return
	}
	// Atomic snapshot+subscribe: last_seq beyond the retained window
	// transparently falls back to a full snapshot (client dedups by seq).
	client := &session.Client{C: make(chan []byte, 256)}
	chunks, next := sess.AttachClient(client, att.LastSeq)
	defer sess.Detach(client)

	if err := sendCtl(c, helloMsg{
		Type: "hello", SessionID: id,
		Cols: sess.Rec().Cols, Rows: sess.Rec().Rows,
		State: string(sess.Rec().State), NextSeq: next,
	}); err != nil {
		return
	}
	for _, ch := range chunks {
		frame := make([]byte, 8+len(ch.Data()))
		putU64WS(frame[:8], ch.Seq())
		copy(frame[8:], ch.Data())
		if err := c.Write(ctx, websocket.MessageBinary, frame); err != nil {
			return
		}
	}

	// Resync detection state for late attaches: emit the registry's last
	// transition (covers a prompt raised while no client was watching).
	var pending *adapter.Region
	if pending, err = sendStateFrames(c, sess.CurrentState(), pending); err != nil {
		return
	}

	go func() {
		ups := sess.StateUpdates()
		for {
			select {
			case frame, ok := <-client.C:
				if !ok {
					if code := sess.ExitCode(); code != nil {
						// PTY closed → eof.
						_ = sendCtl(c, eofMsg{Type: "eof", ExitCode: *code})
						return
					}
					// Session alive but this client was dropped as a slow
					// consumer — tell it so it reconnects + resyncs instead
					// of showing a dead "eof" state.
					_ = sendCtl(c, errMsg{Type: "error", Code: "dropped", Message: "slow consumer — reconnect to resync"})
					_ = c.Close(websocket.StatusTryAgainLater, "slow consumer")
					return
				}
				wctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				err := c.Write(wctx, websocket.MessageBinary, frame)
				cancel()
				if err != nil {
					return
				}
			case u := <-ups:
				var err error
				if pending, err = sendStateFrames(c, u, pending); err != nil {
					return
				}
			}
		}
	}()

	for {
		mt, data, err := c.Read(ctx)
		if err != nil {
			cancel()
			return
		}
		if mt == websocket.MessageBinary {
			if err := sess.Write(data); err != nil {
				return
			}
			continue
		}
		var m ctl
		if err := json.Unmarshal(data, &m); err != nil {
			continue
		}
		switch m.Type {
		case "input":
			// JSON input variant: {type:"input", data:"..."} — reserved.
		case "resize":
			if m.Cols > 0 && m.Rows > 0 {
				sess.Resize(m.Cols, m.Rows)
			}
		case "detach":
			return
		}
	}
}

// wsEvents is the read-only broadcast socket for session lifecycle.
func (s *Server) wsEvents(w http.ResponseWriter, r *http.Request) {
	if !s.originOK(r) {
		writeErr(w, 403, "ws_upgrade_failed", "origin not allowed")
		return
	}
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer c.CloseNow()
	ctx := c.CloseRead(r.Context())
	sub := s.bus.Sub()
	defer s.bus.Unsub(sub)
	for ev := range sub {
		b, _ := json.Marshal(ev)
		if err := c.Write(ctx, websocket.MessageText, b); err != nil {
			return
		}
	}
}

// originOK enforces TU_ALLOWED_ORIGINS (default: same-origin — no check
// needed since the SPA is served from this origin).
func (s *Server) originOK(r *http.Request) bool {
	allowed := s.cfg.AllowedOrigins
	if len(allowed) == 0 {
		return true
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true // non-browser clients
	}
	for _, a := range allowed {
		if a == "*" || a == origin {
			return true
		}
	}
	slog.Warn("ws origin rejected", "origin", origin)
	return false
}

func sendCtl(c *websocket.Conn, v any) error {
	b, _ := json.Marshal(v)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return c.Write(ctx, websocket.MessageText, b)
}

func putU64WS(b []byte, v uint64) {
	for i := 0; i < 8; i++ {
		b[i] = byte(v >> (8 * (7 - i)))
	}
}

// Ref: .teos/conventions/websocket-message-catalog.md
// Session socket: attach{last_seq} → replayed data frames → hello{next_seq}
// → binary data frames (8-byte seq prefix + payload) / input / resize /
// detach / eof / error. Every control frame carries `t` per the catalog.
package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coder/websocket"

	"github.com/jedafe/terminu/internal/adapter"
	"github.com/jedafe/terminu/internal/auth"
	"github.com/jedafe/terminu/internal/domain"
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

// wsAuthzCadence is how often a live session socket re-resolves its
// cookie — the catalog requires per-message authz; a short cadence
// bounds revoked-session writes without a DB read per keystroke.
const wsAuthzCadence = 10 * time.Second

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
		rec, gerr := s.mgr.Get(r.Context(), id)
		if gerr != nil {
			writeErr(w, 404, "session_not_found", "no live session")
			return
		}
		// Finished rows still answer — persisted scrollback replay +
		// eof, read-only (catalog). Anything else dead-but-rowed is an
		// impossible request: invalid_transition (409), not not-found.
		if session.IsFinished(rec.State) {
			s.wsDead(w, r, rec)
			return
		}
		writeErr(w, 409, "invalid_transition", "session has ended")
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

	// The connection cookie is re-resolved on a cadence for the socket's
	// lifetime (per-message authz) — a revoked login stops authorizing
	// input instead of riding the upgrade forever.
	cookieToken := ""
	if ck, err := r.Cookie(auth.CookieName); err == nil {
		cookieToken = ck.Value
	}
	lastAuthz := time.Now()

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

	// Catalog resync order: replayed data frames first, then hello with
	// next_seq — the client's cursor is current before the ack arrives.
	for _, ch := range chunks {
		frame := make([]byte, 8+len(ch.Data()))
		putU64WS(frame[:8], ch.Seq())
		copy(frame[8:], ch.Data())
		if err := c.Write(ctx, websocket.MessageBinary, frame); err != nil {
			return
		}
	}
	if err := sendCtl(c, helloMsg{
		Type: "hello", SessionID: id,
		Cols: sess.Rec().Cols, Rows: sess.Rec().Rows,
		State: string(sess.Rec().State), NextSeq: next,
	}); err != nil {
		return
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
		// Per-message authz on a cadence: re-resolve the cookie before
		// accepting more input — a revoked session dies mid-stream, not
		// at the next reconnect.
		if cookieToken != "" && time.Since(lastAuthz) > wsAuthzCadence {
			if _, err := s.auth.AuthenticateToken(ctx, cookieToken); err != nil {
				_ = c.Close(websocket.StatusPolicyViolation, "session expired")
				return
			}
			lastAuthz = time.Now()
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
			// JSON input variant: {t:"input", data:"..."} — reserved.
		case "resize":
			// Same bounds as spawn — the screen model allocates
			// cols×rows cells on resize too.
			if m.Cols > 0 && m.Cols <= maxSpawnCols && m.Rows > 0 && m.Rows <= maxSpawnRows {
				sess.Resize(m.Cols, m.Rows)
			}
		case "detach":
			return
		}
	}
}

// wsDead replays a finished session's persisted scrollback then eof —
// read-only view; client input is discarded at CloseRead (no PTY left to
// feed, so no per-message authz cadence is needed). The attach contract
// still holds: first frame must be attach{last_seq}, replayed frames
// carry seqs so a reconnecting client dedups correctly.
func (s *Server) wsDead(w http.ResponseWriter, r *http.Request, rec *domain.Session) {
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
	// Not CloseRead — we must still read the attach frame ourselves, and
	// later client input is simply never consumed (writes fail fast if
	// the peer vanishes mid-replay).
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	_, first, err := c.Read(ctx)
	if err != nil {
		return
	}
	var att ctl
	if err := json.Unmarshal(first, &att); err != nil || att.Type != "attach" {
		_ = sendCtl(c, errMsg{Type: "error", Code: "protocol", Message: "first frame must be attach{last_seq}"})
		return
	}
	data, err := s.mgr.DeadScrollback(rec.ID)
	if err != nil {
		_ = sendCtl(c, errMsg{Type: "error", Code: "internal", Message: "scrollback read failed"})
		return
	}
	// Split the file into seq'd frames; hello.next_seq = one past the
	// last replayed frame so the client's dedup boundary is consistent.
	n := (len(data) + 32767) / 32768
	if err := sendCtl(c, helloMsg{
		Type: "hello", SessionID: rec.ID,
		Cols: rec.Cols, Rows: rec.Rows,
		State: string(rec.State), NextSeq: uint64(n + 1),
	}); err != nil {
		return
	}
	seq := uint64(0)
	for i := 0; i < len(data); i += 32768 {
		end := i + 32768
		if end > len(data) {
			end = len(data)
		}
		seq++
		frame := make([]byte, 8+end-i)
		putU64WS(frame[:8], seq)
		copy(frame[8:], data[i:end])
		if err := c.Write(ctx, websocket.MessageBinary, frame); err != nil {
			return
		}
	}
	code := 0
	if rec.ExitCode != nil {
		code = *rec.ExitCode
	}
	_ = sendCtl(c, eofMsg{Type: "eof", ExitCode: code})
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

// originOK enforces TU_ALLOWED_ORIGINS. With no allowlist configured the
// default is same-origin — the origin's host must match the request's;
// an empty Origin is a non-browser client (cookie auth still applies).
func (s *Server) originOK(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true // non-browser clients
	}
	allowed := s.cfg.AllowedOrigins
	if len(allowed) == 0 {
		u, err := url.Parse(origin)
		if err == nil && strings.EqualFold(u.Host, r.Host) {
			return true
		}
		slog.Warn("ws origin rejected (same-origin default)", "origin", origin, "host", r.Host)
		return false
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

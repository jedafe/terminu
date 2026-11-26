//go:build linux

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/jedafe/terminu/internal/adapter"
	"github.com/jedafe/terminu/internal/config"
	"github.com/jedafe/terminu/internal/db"
	"github.com/jedafe/terminu/internal/db/gen"
	"github.com/jedafe/terminu/internal/domain"
	"github.com/jedafe/terminu/internal/session"
)

// The catalog contract: a waiting match emits state+prompt; its
// resolution emits state+prompt_clear carrying the prompt's region.
func TestSendStateFrames(t *testing.T) {
	wait := session.StateUpdate{
		State: domain.StateWaitingForInput,
		Match: &adapter.StateMatch{
			State:      domain.StateWaitingForInput,
			Kind:       domain.WaitConfirm,
			Confidence: domain.ConfidenceMedium,
			Evidence:   "bottom line: ok? [y/N]",
			Region:     adapter.Region{Y0: 20, Y1: 24},
			Prompt: &adapter.Prompt{
				Kind:  domain.WaitConfirm,
				Title: "ok? [y/N]",
				Options: []adapter.PromptOption{
					{Label: "yes", Hint: "y", Default: true},
					{Label: "no", Hint: "n"},
				},
			},
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		pending, err := sendStateFrames(c, wait, nil)
		if err != nil {
			return
		}
		// Leaving the wait → state + prompt_clear.
		if _, err = sendStateFrames(c, session.StateUpdate{State: domain.StateWorking}, pending); err != nil {
			return
		}
		c.CloseRead(context.Background())
		select {}
	}))
	defer srv.Close()

	ctx := context.Background()
	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.CloseNow()

	read := func() map[string]any {
		t.Helper()
		_, data, err := c.Read(ctx)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		var m map[string]any
		if err := json.Unmarshal(data, &m); err != nil {
			t.Fatalf("frame not JSON: %s", data)
		}
		return m
	}

	st := read()
	if st["type"] != "state" || st["state"] != "waiting-for-input" {
		t.Fatalf("state frame: %v", st)
	}
	if st["kind"] != "confirm" || st["confidence"] != "medium" || st["evidence"] == "" {
		t.Fatalf("state fields: %v", st)
	}
	pm := read()
	if pm["type"] != "prompt" || pm["kind"] != "confirm" || pm["title"] != "ok? [y/N]" {
		t.Fatalf("prompt frame: %v", pm)
	}
	opts, _ := pm["options"].([]any)
	if len(opts) != 2 {
		t.Fatalf("options: %v", opts)
	}
	// The wire contract is {label, hint} — the inject key must be absent
	// entirely, not just null (ADR-007).
	if o0, _ := opts[0].(map[string]any); o0["hint"] != "y" {
		t.Fatalf("option hint missing on wire: %v", opts[0])
	}
	if _, present := opts[0].(map[string]any)["inject"]; present {
		t.Fatalf("inject key leaked on wire: %v", opts[0])
	}
	st2 := read()
	if st2["type"] != "state" || st2["state"] != "working" {
		t.Fatalf("second state: %v", st2)
	}
	pc := read()
	if pc["type"] != "prompt_clear" {
		t.Fatalf("clear: %v", pc)
	}
	reg, _ := pc["region"].(map[string]any)
	if reg["y0"] != float64(20) || reg["y1"] != float64(24) {
		t.Fatalf("clear region: %v", reg)
	}
}

// Dead attach: a finished session replays persisted scrollback then eof —
// hello carries the terminal state, frames carry seq'd bytes, eof the
// stored exit code. No live PTY involved.
func TestWsDeadAttach(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	d, err := db.Open(ctx, filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	defer func() { _ = d.Close() }()
	mgr := session.NewManager(&config.Config{
		DataDir: dir, LocalTargetMode: "container",
		SessionBufferKB: 64, QuiescenceMS: 80, ScanBottomRows: 12, MaxSessions: 5,
	}, gen.New(d), session.NewEventBus())
	s := &Server{mgr: mgr, cfg: &config.Config{}}

	rec, err := mgr.Spawn(ctx, domain.SpawnRequest{Target: "local", Shell: "/bin/ls"})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	// Wait for the process reap — detection can write done/error to the
	// row while the PTY is still live, so the live map (not the DB) is
	// the truth for "the PTY is gone".
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := mgr.Attach(rec.ID); err != nil {
			break
		}
		if time.Now().After(deadline) {
			row, _ := mgr.Get(ctx, rec.ID)
			t.Fatalf("still live: db=%s", row.State)
		}
		time.Sleep(25 * time.Millisecond)
	}
	if _, err := mgr.DeadScrollback(rec.ID); err != nil {
		t.Fatalf("scrollback file: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/ws/session/{id}", s.wsSession)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/ws/session/"+rec.ID, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = c.Close(websocket.StatusNormalClosure, "") }()
	if err := c.Write(ctx, websocket.MessageText,
		[]byte(`{"type":"attach","last_seq":0}`)); err != nil {
		t.Fatalf("attach: %v", err)
	}

	var gotData []byte
	var hello, eof map[string]any
	for i := 0; i < 40; i++ {
		mt, data, err := c.Read(ctx)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if mt == websocket.MessageBinary {
			gotData = append(gotData, data[8:]...)
			continue
		}
		var m map[string]any
		_ = json.Unmarshal(data, &m)
		switch m["type"] {
		case "hello":
			hello = m
		case "eof":
			eof = m
		}
		if eof != nil {
			break
		}
	}
	if hello == nil {
		t.Fatal("no hello frame")
	}
	if hello["state"] != "done" && hello["state"] != "ended" {
		t.Fatalf("hello state: %v", hello)
	}
	if len(gotData) == 0 {
		t.Fatal("no replayed scrollback bytes")
	}
	if eof == nil {
		t.Fatal("no eof frame")
	}
}

//go:build linux

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/coder/websocket"

	"github.com/jedafe/terminu/internal/adapter"
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
					{Label: "yes", Inject: adapter.Char('y'), Default: true},
					{Label: "no", Inject: adapter.Char('N')},
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

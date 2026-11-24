//go:build linux

package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/jedafe/terminu/internal/db"
	"github.com/jedafe/terminu/internal/db/gen"
)

// GET /api/alerts — unacknowledged first, newest first, nullable fields
// flattened (the client never sees sql.NullString).
func TestListAlerts(t *testing.T) {
	ctx := context.Background()
	d, err := db.Open(ctx, filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	defer func() { _ = d.Close() }()
	q := gen.New(d)
	s := &Server{q: q}

	if err := q.CreateSession(ctx, gen.CreateSessionParams{
		ID: "s1", State: "working", TargetKind: "local", Cols: 80, Rows: 24,
	}); err != nil {
		t.Fatalf("session: %v", err)
	}
	mk := func(level, tr string, kind, conf sql.NullString) {
		if err := q.CreateAlert(ctx, gen.CreateAlertParams{
			SessionID: "s1", Level: level, Transition: tr,
			Process: "claude", Kind: kind, Confidence: conf,
		}); err != nil {
			t.Fatalf("alert: %v", err)
		}
	}
	mk("action", "working→waiting-for-input", sql.NullString{String: "permission", Valid: true}, sql.NullString{String: "high", Valid: true})
	mk("info", "working→done", sql.NullString{}, sql.NullString{})
	// Ack the info row — ordering puts the unacked action row first
	// regardless of recency.
	rows, err := q.ListAlerts(ctx, 10)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	for _, r := range rows {
		if r.Level == "info" {
			if err := q.AckAlert(ctx, r.ID); err != nil {
				t.Fatalf("ack seed: %v", err)
			}
		}
	}

	srv := httptest.NewServer(http.HandlerFunc(s.listAlerts))
	defer srv.Close()
	res, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer res.Body.Close()
	var env struct {
		Data []alertOut `json:"data"`
	}
	if err := json.NewDecoder(res.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	out := env.Data
	if len(out) != 2 {
		t.Fatalf("alerts = %+v", out)
	}
	if out[0].AckedAt != "" || out[0].Kind != "permission" || out[0].Confidence != "high" {
		t.Fatalf("first should be the unacked row, flattened: %+v", out[0])
	}
	if out[1].AckedAt == "" {
		t.Fatalf("acked row should carry its timestamp: %+v", out[1])
	}
}

// POST /api/alerts/{id}/ack — idempotent; non-numeric id → bad_request.
func TestAckAlert(t *testing.T) {
	ctx := context.Background()
	d, err := db.Open(ctx, filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	defer func() { _ = d.Close() }()
	q := gen.New(d)
	s := &Server{q: q}

	if err := q.CreateSession(ctx, gen.CreateSessionParams{
		ID: "s1", State: "working", TargetKind: "local", Cols: 80, Rows: 24,
	}); err != nil {
		t.Fatalf("session: %v", err)
	}
	if err := q.CreateAlert(ctx, gen.CreateAlertParams{
		SessionID: "s1", Level: "action", Transition: "working→waiting-for-input",
		Process: "aider",
	}); err != nil {
		t.Fatalf("alert: %v", err)
	}
	rows, err := q.ListAlerts(ctx, 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	id := rows[0].ID

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/alerts/{id}/ack", s.ackAlert)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	for i := 0; i < 2; i++ { // ack twice — idempotent
		req, _ := http.NewRequest("POST", srv.URL+"/api/alerts/"+strconv.FormatInt(id, 10)+"/ack", nil)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("ack: %v", err)
		}
		res.Body.Close()
		if res.StatusCode != 200 {
			t.Fatalf("ack %d: status %d", i, res.StatusCode)
		}
	}
	after, err := q.ListAlerts(ctx, 10)
	if err != nil {
		t.Fatalf("list after: %v", err)
	}
	if !after[0].AckedAt.Valid || after[0].AckedAt.String == "" {
		t.Fatalf("not acked: %+v", after[0])
	}

	req, _ := http.NewRequest("POST", srv.URL+"/api/alerts/abc/ack", nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("bad id: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != 400 {
		t.Fatalf("non-numeric id: status %d", res.StatusCode)
	}

}

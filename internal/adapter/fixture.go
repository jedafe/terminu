// Ref: .teos/specs/adapter-design.md §8 — fixture pipeline.
// Adapters prove patterns against real transcripts: asciinema-v2 .cast
// files at fixtures/<tool>/<version>/<scenario>.cast, replayed through
// the same screen model adapters see in production — Detect() never
// parses bytes itself, so the harness feeds the model, not the adapter.
package adapter

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"testing"

	"github.com/jedafe/terminu/internal/screen"
)

// castHeader is line 1 of an asciinema v2 recording.
type castHeader struct {
	Version int `json:"version"`
	Width   int `json:"width"`
	Height  int `json:"height"`
}

// ReplayCast feeds an asciinema-v2 .cast into s. The screen is resized
// to the recording's dimensions first; only "o" (output) events are
// fed — input events are user keystrokes, not screen bytes. Delays are
// ignored: replay is a state dump, not a timing simulation.
func ReplayCast(path string, s *screen.Screen) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	return ReplayCastReader(f, s)
}

// ReplayCastReader is ReplayCast over any reader.
func ReplayCastReader(r io.Reader, s *screen.Screen) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	first := true
	for sc.Scan() {
		line := sc.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		if first {
			first = false
			var h castHeader
			if err := json.Unmarshal(line, &h); err != nil {
				return fmt.Errorf("cast header: %w", err)
			}
			if h.Version != 2 {
				return fmt.Errorf("cast version %d unsupported (want v2)", h.Version)
			}
			if h.Width > 0 && h.Height > 0 {
				s.Resize(h.Width, h.Height)
			}
			continue
		}
		var ev [3]json.RawMessage
		if err := json.Unmarshal(line, &ev); err != nil {
			return fmt.Errorf("cast event: %w", err)
		}
		var kind string
		if err := json.Unmarshal(ev[1], &kind); err != nil || kind != "o" {
			continue
		}
		var data string
		if err := json.Unmarshal(ev[2], &data); err != nil {
			return fmt.Errorf("cast payload: %w", err)
		}
		if _, err := s.Write([]byte(data)); err != nil {
			return fmt.Errorf("screen write: %w", err)
		}
	}
	return sc.Err()
}

// GoldenJSON compares got (marshaled) to a golden file; -update rewrites.
// Ref: spec §8 — "golden-file compare on extracted options".
func GoldenJSON(t *testing.T, path string, got any) {
	t.Helper()
	b, err := json.MarshalIndent(got, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	b = append(b, '\n')
	if *updateGolden {
		if err := os.WriteFile(path, b, 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s (run with -update to create): %v", path, err)
	}
	if !bytes.Equal(want, b) {
		t.Fatalf("golden mismatch vs %s:\n--- want ---\n%s\n--- got ---\n%s", path, want, b)
	}
}

var updateGolden = flag.Bool("update", false, "update golden fixture files")

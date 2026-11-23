package session

import "testing"

func TestRingReplay(t *testing.T) {
	r := newRing(1 << 20)
	for i := 0; i < 5; i++ {
		r.append([]byte{byte('a' + i)})
	}
	out, next, ok := r.replay(3)
	if !ok {
		t.Fatal("replay within window should be ok")
	}
	if next != 6 {
		t.Fatalf("next=%d, want 6", next)
	}
	if len(out) != 2 || out[0].seq != 4 || out[1].seq != 5 {
		t.Fatalf("want seqs [4 5], got %+v", out)
	}
}

func TestRingOverflow(t *testing.T) {
	// ring clamps caps below 4KB — use 4608 with 1KB chunks: 5 chunks
	// (5120B) exceed it, evicting down to 4 (4096 ≤ cap).
	r := newRing(4608)
	for i := 0; i < 5; i++ {
		r.append(make([]byte, 1024))
	}
	// snapshot() is the full-refresh path — returns the retained window.
	out, _ := r.snapshot()
	if len(out) != 4 || out[0].seq != 2 {
		t.Fatalf("want 4 chunks from seq 2, got %+v", out)
	}
	// last_seq behind the retained window → overflow → caller falls
	// back to snapshot.
	if _, _, ok := r.replay(0); ok {
		t.Fatal("replay(0) behind window must fail")
	}
	// In-window replay still works (client holding seq 1 needs 2+).
	out, _, ok := r.replay(1)
	if !ok || len(out) != 4 || out[0].seq != 2 {
		t.Fatalf("in-window replay broken: ok=%v chunks=%+v", ok, out)
	}
}

func TestRingSeqMonotonic(t *testing.T) {
	r := newRing(1 << 20)
	prev := uint64(0)
	for i := 0; i < 100; i++ {
		s := r.append([]byte{1})
		if s <= prev {
			t.Fatalf("seq not monotonic: %d after %d", s, prev)
		}
		prev = s
	}
}

// Ref: .teos/sections/03-session-management.md
// Sequence-numbered ring buffer — replay source for attach{last_seq}.
package session

import "sync"

// chunk is one buffered output frame.
type chunk struct {
	seq  uint64
	data []byte
}

func (c chunk) Seq() uint64   { return c.seq }
func (c chunk) Data() []byte  { return c.data }
func (c chunk) Bytes() []byte { return c.data }

// ring is a byte-bounded FIFO of output chunks. Oldest chunks drop when
// the cap is exceeded; clients behind the drop get a resync error.
type ring struct {
	mu     sync.Mutex
	chunks []chunk
	cap    int // bytes
	size   int
	next   uint64 // next seq to assign
	head   uint64 // oldest seq still buffered (0 = empty)
}

func newRing(capBytes int) *ring {
	if capBytes < 4096 {
		capBytes = 4096
	}
	return &ring{cap: capBytes, next: 1}
}

// append stores data under the next seq and returns it.
func (r *ring) append(data []byte) uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	seq := r.next
	r.next++
	cp := make([]byte, len(data))
	copy(cp, data)
	r.chunks = append(r.chunks, chunk{seq: seq, data: cp})
	r.size += len(cp)
	if len(r.chunks) == 1 {
		r.head = seq
	}
	for r.size > r.cap && len(r.chunks) > 1 {
		r.size -= len(r.chunks[0].data)
		r.chunks = r.chunks[1:]
		r.head = r.chunks[0].seq
	}
	return seq
}

// replay returns chunks with seq > lastSeq. ok=false means lastSeq is
// older than the retained window — client must full-refresh.
func (r *ring) replay(lastSeq uint64) (out []chunk, next uint64, ok bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.chunks) == 0 || lastSeq >= r.next-1 {
		return nil, r.next, true
	}
	if lastSeq+1 < r.head {
		return nil, r.next, false
	}
	for _, c := range r.chunks {
		if c.seq > lastSeq {
			out = append(out, c)
		}
	}
	return out, r.next, true
}

// snapshot returns every retained chunk (fresh attach).
func (r *ring) snapshot() ([]chunk, uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]chunk(nil), r.chunks...), r.next
}

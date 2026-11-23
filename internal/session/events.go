// Ref: .teos/sections/10-networking-ws-protocol.md (/ws/events)
// Broadcast bus for session lifecycle events.
package session

import (
	"sync"

	"github.com/jedafe/terminu/internal/domain"
)

// Event is one /ws/events broadcast frame — lifecycle events ride
// Type/SessionID/Session; agent.state (catalog) carries the detection
// fields below.
type Event struct {
	Type      string          `json:"type"`
	SessionID string          `json:"session_id,omitempty"`
	Session   *domain.Session `json:"session,omitempty"`

	// agent.state frame (catalog): fg process + detected state.
	Process    string              `json:"process,omitempty"`
	State      domain.SessionState `json:"state,omitempty"`
	Kind       domain.WaitKind     `json:"kind,omitempty"`
	Confidence domain.Confidence   `json:"confidence,omitempty"`
}

// EventBus fans events to every subscriber — read-only broadcast socket.
type EventBus struct {
	mu   sync.Mutex
	subs map[chan Event]struct{}
}

func NewEventBus() *EventBus {
	return &EventBus{subs: map[chan Event]struct{}{}}
}

func (b *EventBus) Sub() chan Event {
	c := make(chan Event, 64)
	b.mu.Lock()
	b.subs[c] = struct{}{}
	b.mu.Unlock()
	return c
}

func (b *EventBus) Unsub(c chan Event) {
	b.mu.Lock()
	delete(b.subs, c)
	close(c)
	b.mu.Unlock()
}

func (b *EventBus) Pub(e Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for c := range b.subs {
		select {
		case c <- e:
		default:
		}
	}
}

package realtime

import (
	"sync"
	"sync/atomic"
)

// DatabaseEvent is intentionally homogeneous: it only says that the database
// changed. Connected pages re-query their own route and query-parameter view.
type DatabaseEvent struct {
	Revision uint64
}

type Hub struct {
	mu          sync.RWMutex
	subscribers map[chan DatabaseEvent]struct{}
	revision    atomic.Uint64
}

func NewHub() *Hub {
	return &Hub{subscribers: make(map[chan DatabaseEvent]struct{})}
}

func (h *Hub) Subscribe() (<-chan DatabaseEvent, func()) {
	ch := make(chan DatabaseEvent, 1)
	h.mu.Lock()
	h.subscribers[ch] = struct{}{}
	h.mu.Unlock()

	var once sync.Once
	unsubscribe := func() {
		once.Do(func() {
			h.mu.Lock()
			delete(h.subscribers, ch)
			close(ch)
			h.mu.Unlock()
		})
	}
	return ch, unsubscribe
}

func (h *Hub) Broadcast() {
	event := DatabaseEvent{Revision: h.revision.Add(1)}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for ch := range h.subscribers {
		select {
		case ch <- event:
		default:
			// Frame dropping is deliberate: every event means the same thing, and
			// the buffered event still causes a render of the latest snapshot.
		}
	}
}

func (h *Hub) SubscriberCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.subscribers)
}

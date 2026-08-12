package realtime

import (
	"testing"
	"time"
)

func TestHubDropsIntermediateHomogeneousEvents(t *testing.T) {
	hub := NewHub()
	events, unsubscribe := hub.Subscribe()
	defer unsubscribe()

	hub.Broadcast()
	hub.Broadcast()
	hub.Broadcast()

	select {
	case event := <-events:
		if event.Revision == 0 {
			t.Fatal("event revision was not assigned")
		}
	case <-time.After(time.Second):
		t.Fatal("subscriber did not receive an event")
	}

	select {
	case <-events:
		t.Fatal("buffer retained intermediate events")
	default:
	}
}

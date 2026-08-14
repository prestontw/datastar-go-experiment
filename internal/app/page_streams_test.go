package app

import (
	"context"
	"testing"
	"time"
)

func TestPageStreamRegistryUsesClientRevisionNotArrivalOrder(t *testing.T) {
	registry := newPageStreamRegistry()
	registry.retention = time.Millisecond
	first, accepted := registry.acquire(context.Background(), "session/page", 4)
	if !accepted {
		t.Fatal("initial stream was rejected")
	}

	current, accepted := registry.acquire(context.Background(), "session/page", 5)
	if !accepted {
		t.Fatal("newer stream was rejected")
	}
	select {
	case <-first.Context().Done():
	default:
		t.Fatal("newer stream did not cancel its predecessor")
	}
	if first.Current() {
		t.Fatal("replaced stream still reports itself current")
	}

	stale, accepted := registry.acquire(context.Background(), "session/page", 4)
	if accepted || stale != nil {
		t.Fatal("late retry from older route was accepted")
	}
	if !current.Current() {
		t.Fatal("late retry displaced the current stream")
	}

	// A reconnect of the current route uses the same revision and replaces the
	// broken transport without being mistaken for older navigation.
	reconnected, accepted := registry.acquire(context.Background(), "session/page", 5)
	if !accepted {
		t.Fatal("same-revision reconnect was rejected")
	}
	select {
	case <-current.Context().Done():
	default:
		t.Fatal("reconnect did not cancel the previous transport")
	}

	first.Release() // stale cleanup must not remove the latest stream.
	current.Release()
	if !reconnected.Current() {
		t.Fatal("stale cleanup removed the reconnected stream")
	}
	reconnected.Release()
	if reconnected.Current() {
		t.Fatal("released stream still reports itself current")
	}

	deadline := time.Now().Add(time.Second)
	for {
		registry.mu.Lock()
		remaining := len(registry.streams)
		registry.mu.Unlock()
		if remaining == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("released stream tombstone did not expire")
		}
		time.Sleep(time.Millisecond)
	}
}

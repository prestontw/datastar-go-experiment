package app

import (
	"context"
	"sync"
	"time"
)

// pageStreamRegistry gives the server one page-stream owner per session and
// active document. The client-supplied revision lets the server reject a late
// retry from an older route instead of mistaking arrival order for navigation
// order. Released revision tombstones expire after a short retry window.
type pageStreamRegistry struct {
	mu        sync.Mutex
	streams   map[string]*pageStreamState
	retention time.Duration
}

type pageStreamState struct {
	mu       sync.Mutex
	revision int64
	token    uint64
	cancel   context.CancelFunc
	expires  *time.Timer
}

type pageStreamLease struct {
	registry *pageStreamRegistry
	key      string
	state    *pageStreamState
	token    uint64
	ctx      context.Context
}

func newPageStreamRegistry() *pageStreamRegistry {
	return &pageStreamRegistry{
		streams:   make(map[string]*pageStreamState),
		retention: 5 * time.Minute,
	}
}

func (r *pageStreamRegistry) acquire(parent context.Context, key string, revision int64) (*pageStreamLease, bool) {
	r.mu.Lock()
	state := r.streams[key]
	if state == nil {
		state = &pageStreamState{revision: revision}
		r.streams[key] = state
	}
	state.mu.Lock()
	r.mu.Unlock()
	defer state.mu.Unlock()
	if revision < state.revision {
		return nil, false
	}
	if state.expires != nil {
		state.expires.Stop()
		state.expires = nil
	}
	if state.cancel != nil {
		state.cancel()
	}
	ctx, cancel := context.WithCancel(parent)
	state.revision = revision
	state.token++
	state.cancel = cancel
	return &pageStreamLease{
		registry: r,
		key:      key,
		state:    state,
		token:    state.token,
		ctx:      ctx,
	}, true
}

func (l *pageStreamLease) Context() context.Context { return l.ctx }

func (l *pageStreamLease) Current() bool {
	l.state.mu.Lock()
	defer l.state.mu.Unlock()
	return l.state.token == l.token && l.state.cancel != nil
}

func (l *pageStreamLease) Release() {
	l.state.mu.Lock()
	defer l.state.mu.Unlock()
	if l.state.token != l.token || l.state.cancel == nil {
		return
	}
	l.state.cancel()
	l.state.cancel = nil
	l.state.expires = time.AfterFunc(l.registry.retention, func() {
		l.registry.mu.Lock()
		defer l.registry.mu.Unlock()
		l.state.mu.Lock()
		defer l.state.mu.Unlock()
		if l.state.token == l.token && l.state.cancel == nil && l.registry.streams[l.key] == l.state {
			delete(l.registry.streams, l.key)
		}
	})
}

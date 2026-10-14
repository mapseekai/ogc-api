package main

import (
	"net/http"
	"sync"
	"sync/atomic"
)

// publication pins the whole host router, including both protocol engines, for
// each synchronous HTTP response. It owns no providers and starts no goroutines.
// After publish returns, the host may retire the old providers once drained closes.
// A handler must return only after it has finished its response and owned streams.
type publication struct {
	mu      sync.RWMutex
	current *generation
}

type generation struct {
	handler http.Handler
	refs    atomic.Int64
	drained chan struct{}
}

func (p *publication) publish(handler http.Handler) <-chan struct{} {
	var next *generation
	if handler != nil {
		next = &generation{handler: handler, drained: make(chan struct{})}
		next.refs.Store(1) // Publication holds one reference until it is replaced.
	}
	p.mu.Lock()
	old := p.current
	p.current = next
	p.mu.Unlock()
	if old != nil {
		old.release()
		return old.drained
	}
	drained := make(chan struct{})
	close(drained)
	return drained
}

func (p *publication) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.mu.RLock()
	current := p.current
	if current != nil {
		current.refs.Add(1)
	}
	p.mu.RUnlock()
	if current == nil {
		http.Error(w, "Service unavailable", http.StatusServiceUnavailable)
		return
	}
	defer current.release()
	current.handler.ServeHTTP(w, r)
}

func (g *generation) release() {
	if g.refs.Add(-1) == 0 {
		close(g.drained)
	}
}

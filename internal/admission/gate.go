// Package admission bounds in-flight work without spawning or queueing goroutines.
package admission

import "sync/atomic"

// Gate is an immutable handle to a concurrency-safe admission counter. Its zero value
// is unlimited. Copies share the same counter, so no atomic primitive is copied after use.
type Gate struct{ counter *counter }

type counter struct {
	active  atomic.Int64
	maximum int64
}

// New constructs a gate. The caller must validate that maximum is non-negative.
func New(maximum int) Gate {
	if maximum == 0 {
		return Gate{}
	}
	return Gate{counter: &counter{maximum: int64(maximum)}}
}

// Acquire reserves one slot with an atomic compare-and-swap. Full capacity is rejected
// without a waiting queue. CAS retries only reconcile concurrently changed counters.
func (g Gate) Acquire() bool {
	if g.counter == nil {
		return true
	}
	for {
		current := g.counter.active.Load()
		if current >= g.counter.maximum {
			return false
		}
		if g.counter.active.CompareAndSwap(current, current+1) {
			return true
		}
	}
}

// Release returns an acquired slot. It must be called exactly once per successful Acquire.
func (g Gate) Release() {
	if g.counter != nil {
		g.counter.active.Add(-1)
	}
}

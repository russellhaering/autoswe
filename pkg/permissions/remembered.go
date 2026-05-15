package permissions

import (
	"context"
	"sync"
)

// Remembered wraps an inner Policy and caches decisions whose Remember flag
// is set. Cached decisions are keyed by tool name and persist for the
// lifetime of the Remembered instance.
type Remembered struct {
	inner Policy

	mu    sync.Mutex
	cache map[string]Decision
}

func NewRemembered(inner Policy) *Remembered {
	return &Remembered{inner: inner, cache: map[string]Decision{}}
}

func (r *Remembered) Check(ctx context.Context, call ToolCall) (Decision, error) {
	r.mu.Lock()
	cached, ok := r.cache[call.Name]
	r.mu.Unlock()
	if ok {
		return cached, nil
	}
	d, err := r.inner.Check(ctx, call)
	if err != nil {
		return d, err
	}
	if d.Remember {
		// Drop Remember from cached form — the cache itself is the "remembering".
		toStore := d
		toStore.Remember = false
		r.mu.Lock()
		r.cache[call.Name] = toStore
		r.mu.Unlock()
	}
	return d, nil
}

// Forget removes any cached decision for name. No-op if not cached.
func (r *Remembered) Forget(name string) {
	r.mu.Lock()
	delete(r.cache, name)
	r.mu.Unlock()
}

// Clear removes all cached decisions.
func (r *Remembered) Clear() {
	r.mu.Lock()
	r.cache = map[string]Decision{}
	r.mu.Unlock()
}

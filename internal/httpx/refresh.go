package httpx

import (
	"context"
	"sync"
)

// refreshing is a request's one look past the cache: every response body and
// memo it reads is asked anew once, and what upstream answers replaces what
// was kept. A body read again within the same request comes from the cache,
// as it was just fetched.
type refreshing struct {
	mu   sync.Mutex
	seen map[string]bool
}

type refreshKey struct{}

// WithRefresh marks ctx as reading past the cache, once per body and memo.
func WithRefresh(ctx context.Context) context.Context {
	return context.WithValue(ctx, refreshKey{}, &refreshing{seen: map[string]bool{}})
}

// Refreshing reports whether ctx reads past the cache.
func Refreshing(ctx context.Context) bool {
	_, ok := ctx.Value(refreshKey{}).(*refreshing)
	return ok
}

// refreshFirst reports whether ctx reads past the cache and has not yet read
// id that way, marking it read.
func refreshFirst(ctx context.Context, id string) bool {
	r, ok := ctx.Value(refreshKey{}).(*refreshing)
	if !ok {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.seen[id] {
		return false
	}
	r.seen[id] = true
	return true
}

package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"sync"
	"time"
)

// Memo returns the value derived under key, building it with build only when
// there is none yet or when one of the response bodies it was derived from
// has changed upstream. The bodies build reads through the cache-backed
// getters (GetJSON, GetHTML, GetText) are recorded as its sources; a memo
// whose sources are all within the freshness window costs a file read per
// source and no request, and a stale source costs one HEAD request.
//
// A memo built from nothing the cache tracks — a redirect, a request with
// credentials — is rebuilt once it is older than the freshness window.
//
// When a rebuild fails for any reason but the content being gone (HTTP 404 or
// 410), the last value is returned instead: a page that cannot be read now is
// better shown as it was than not at all.
//
// Every caller gets a copy of its own: values travel as JSON, as they are kept.
func Memo[T any](ctx context.Context, c *Cache, key string, build func(context.Context) (T, error)) (T, error) {
	var zero T
	if !c.active() {
		return build(ctx)
	}
	// a memo read past the cache is built anew, sharing nothing with the
	// reads that use it
	flightKey, anew := key, refreshFirst(ctx, "memo "+key)
	if anew {
		flightKey += "\x00refresh"
	}
	ch := c.memoFlight.DoChan(flightKey, func() (any, error) {
		// shared by every caller waiting on key, so not cut short by the first
		// one giving up
		return c.memo(context.WithoutCancel(ctx), key, anew, func(ctx context.Context) (any, error) {
			return build(ctx)
		})
	})
	var r memoResult
	select {
	case <-ctx.Done():
		return zero, ctx.Err()
	case res := <-ch:
		if res.Err != nil {
			return zero, res.Err
		}
		r = res.Val.(memoResult)
	}
	// a memo built from other memos depends on what they depend on
	if t := trackerFrom(ctx); t != nil {
		for id, h := range r.deps {
			t.add(id, h)
		}
	}
	var out T
	if err := json.Unmarshal(r.value, &out); err != nil {
		return zero, err
	}
	return out, nil
}

type memoResult struct {
	value json.RawMessage
	deps  map[string]string
}

func (c *Cache) memo(ctx context.Context, key string, anew bool, build func(context.Context) (any, error)) (memoResult, error) {
	old, have := c.loadMemo(key)
	if have && !anew && c.memoCurrent(ctx, old) {
		return memoResult{old.Value, old.Deps}, nil
	}
	tctx, t := withTracker(ctx)
	v, err := build(tctx)
	if err != nil {
		if IsGone(err) {
			c.removeMemo(key)
			return memoResult{}, err
		}
		if have {
			return memoResult{old.Value, old.Deps}, nil
		}
		return memoResult{}, err
	}
	b, err := json.Marshal(v)
	if err != nil {
		return memoResult{}, err
	}
	rec := memoRecord{Deps: t.snapshot(), At: time.Now().Unix(), Value: b}
	c.saveMemo(key, rec)
	return memoResult{rec.Value, rec.Deps}, nil
}

// memoCurrent reports whether every source of a memo still has the body it
// was built from, revalidating the stale ones.
func (c *Cache) memoCurrent(ctx context.Context, m memoRecord) bool {
	if len(m.Deps) == 0 {
		return time.Since(time.Unix(m.At, 0)) < c.fresh
	}
	cl := c.client.Load()
	for id, want := range m.Deps {
		e, _, ok := c.loadEntry(id, true)
		if !ok {
			return false // evicted: rebuilding reads it again
		}
		got := e.Hash
		if !e.fresh(c.fresh) && cl != nil {
			if got, ok = cl.revalidate(ctx, id, e); !ok {
				return false
			}
		}
		if got != want {
			return false
		}
	}
	return true
}

// IsGone reports whether err says the content no longer exists upstream.
func IsGone(err error) bool {
	var se *StatusError
	return errors.As(err, &se) && (se.StatusCode == http.StatusNotFound || se.StatusCode == http.StatusGone)
}

// tracker collects the response bodies a memo is built from.
type tracker struct {
	mu   sync.Mutex
	deps map[string]string
}

type trackerKey struct{}

func withTracker(ctx context.Context) (context.Context, *tracker) {
	t := &tracker{deps: map[string]string{}}
	return context.WithValue(ctx, trackerKey{}, t), t
}

func trackerFrom(ctx context.Context) *tracker {
	t, _ := ctx.Value(trackerKey{}).(*tracker)
	return t
}

func (t *tracker) add(id, hash string) {
	t.mu.Lock()
	t.deps[id] = hash
	t.mu.Unlock()
}

func (t *tracker) snapshot() map[string]string {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.deps) == 0 {
		return nil
	}
	return maps.Clone(t.deps)
}

// record notes a body as a source of the memo being built under ctx, if any.
func record(ctx context.Context, id, hash string) {
	if t := trackerFrom(ctx); t != nil && hash != "" {
		t.add(id, hash)
	}
}

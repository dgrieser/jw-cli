package httpx

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// A request marked to refresh asks upstream again once for each body and memo
// it reads, keeps what came, and reads it from the cache after that.
func TestRefreshReadsPastTheCacheOnce(t *testing.T) {
	var gets atomic.Int32
	var body atomic.Value
	body.Store("one")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			gets.Add(1)
		}
		w.Write([]byte(body.Load().(string)))
	}))
	defer srv.Close()
	cache := Open(t.TempDir(), CacheOptions{})
	c := New(WithResponseCache(cache))
	builds := 0
	read := func(ctx context.Context) string {
		t.Helper()
		v, err := Memo(ctx, cache, "k", func(ctx context.Context) (string, error) {
			builds++
			return c.GetText(ctx, srv.URL, nil)
		})
		if err != nil {
			t.Fatal(err)
		}
		return v
	}

	ctx := context.Background()
	if got := read(ctx); got != "one" || gets.Load() != 1 || builds != 1 {
		t.Fatalf("first read: %q, %d gets, %d builds", got, gets.Load(), builds)
	}
	body.Store("two")
	if got := read(ctx); got != "one" || gets.Load() != 1 {
		t.Fatalf("fresh cache: %q, %d gets", got, gets.Load())
	}

	rctx := WithRefresh(ctx)
	if got := read(rctx); got != "two" || gets.Load() != 2 || builds != 2 {
		t.Fatalf("refresh: %q, %d gets, %d builds", got, gets.Load(), builds)
	}
	// once per request: the same body again is the one just fetched
	if _, err := c.GetText(rctx, srv.URL, nil); err != nil || gets.Load() != 2 {
		t.Fatalf("refresh again: %v, %d gets", err, gets.Load())
	}
	if got := read(rctx); got != "two" || builds != 2 {
		t.Fatalf("memo again: %q, %d builds", got, builds)
	}
	// and what came is what the next plain read sees
	if got := read(ctx); got != "two" || gets.Load() != 2 {
		t.Fatalf("after refresh: %q, %d gets", got, gets.Load())
	}
}

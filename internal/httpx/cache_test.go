package httpx

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// A body read once is read from disk the second time, by a new client on the
// same directory as much as by the same one — which is what a restart is.
func TestResponseCacheKeepsBodies(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		switch r.URL.Path {
		case "/bad":
			w.Write([]byte("not json"))
		default:
			w.Write([]byte(`{"n": 1}`))
		}
	}))
	defer srv.Close()
	dir := t.TempDir()
	ctx := context.Background()
	var out struct{ N int }

	c := New(WithResponseCache(OpenCacheAt(dir), time.Hour))
	for range 2 {
		if err := c.GetJSON(ctx, srv.URL+"/a", nil, &out); err != nil || out.N != 1 {
			t.Fatalf("GetJSON: %v %+v", err, out)
		}
	}
	if hits.Load() != 1 {
		t.Errorf("%d upstream reads, want 1", hits.Load())
	}
	restarted := New(WithResponseCache(OpenCacheAt(dir), time.Hour))
	if _, err := restarted.GetText(ctx, srv.URL+"/a", http.Header{"Accept": {"application/json"}}); err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 1 {
		t.Errorf("a new client on the same directory read upstream again")
	}

	// credentials are never kept, and neither is a body that did not decode
	hits.Store(0)
	auth := http.Header{"Authorization": {"Bearer x"}}
	for range 2 {
		_ = c.GetJSON(ctx, srv.URL+"/b", auth.Clone(), &out)
		_ = c.GetJSON(ctx, srv.URL+"/bad", nil, &out)
	}
	if hits.Load() != 4 {
		t.Errorf("%d upstream reads, want every one of the 4", hits.Load())
	}

	// and without the option nothing is kept
	hits.Store(0)
	plain := New()
	for range 2 {
		_ = plain.GetJSON(ctx, srv.URL+"/a", nil, &out)
	}
	if hits.Load() != 2 {
		t.Errorf("uncached client: %d reads, want 2", hits.Load())
	}
}

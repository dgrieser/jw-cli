package server_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dgrieser/jw-cli/internal/httpx"
	"github.com/dgrieser/jw-cli/internal/server"
	"github.com/dgrieser/jw-cli/internal/service"
)

// A page asked for ahead of the reader picks no language for them.
func TestReadAheadPicksNoLanguage(t *testing.T) {
	srv := newTestServer(t, languagesMux(t))
	for _, h := range [][2]string{{"Sec-Purpose", "prefetch"}, {"Purpose", "prefetch"}, {"X-JW-Ahead", "1"}} {
		req, _ := http.NewRequest(http.MethodGet, srv.URL+"/?lang=de", nil)
		req.Header.Set(h[0], h[1])
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if c := resp.Header.Get("Set-Cookie"); c != "" {
			t.Errorf("%s: a page read ahead set %q", h[0], c)
		}
	}
}

// With background revalidation the server reads the start pages ahead, with
// no reader asking; without it, Warm does nothing.
func TestWarmReadsTheStartPages(t *testing.T) {
	var hits atomic.Int32
	mux := languagesMux(t)
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		mux.ServeHTTP(w, r)
	}))
	defer up.Close()
	base := httpx.BaseURLs{CDN: up.URL, JWOrg: up.URL, WOL: up.URL}

	plain := server.New(server.Config{Svc: service.New(httpx.New(httpx.WithBaseURLs(base)), httpx.OpenCacheAt(t.TempDir()))})
	done := make(chan struct{})
	go func() { plain.Warm(context.Background()); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Warm without background revalidation did not return")
	}
	if hits.Load() != 0 {
		t.Errorf("warmed without background revalidation: %d requests", hits.Load())
	}

	hc := httpx.New(httpx.WithBaseURLs(base), httpx.WithBackgroundRevalidation(),
		httpx.WithRetryDelays(time.Millisecond))
	// the background may still be writing as the test ends
	dir, err := os.MkdirTemp("", "warm")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	srv := server.New(server.Config{Svc: service.New(hc, httpx.OpenCacheAt(dir))})
	srv.Handler()
	go srv.Warm(t.Context())
	deadline := time.Now().Add(5 * time.Second)
	for hits.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if hits.Load() == 0 {
		t.Error("the start pages were not read ahead")
	}
}

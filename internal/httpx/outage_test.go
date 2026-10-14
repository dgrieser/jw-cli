package httpx

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// pastBackOff has c forget every back-off, kept or not, as if it had run out.
func pastBackOff(c *Client) {
	c.failed.Range(func(k, _ any) bool { c.failed.Delete(k); return true })
	c.down.mu.Lock()
	c.down.until = map[string]int64{}
	c.down.mu.Unlock()
	cache := c.responses
	cache.Put(hostsKey, map[string]int64{})
	entries, _ := os.ReadDir(filepath.Join(cache.dir, responseDir))
	for _, de := range entries {
		if e, body, ok := cache.loadEntry(de.Name(), false); ok && e.Retry != 0 {
			e.Retry = 0
			cache.saveEntry(de.Name(), e, body)
		}
	}
	memos, _ := os.ReadDir(filepath.Join(cache.dir, memoDir))
	for _, de := range memos {
		p := filepath.Join(cache.dir, memoDir, de.Name())
		var m memoRecord
		if b, err := os.ReadFile(p); err == nil && json.Unmarshal(b, &m) == nil && m.Retry != 0 {
			m.Retry = 0
			b, _ = json.Marshal(m)
			_ = os.WriteFile(p, b, 0o644)
		}
	}
}

// shortWait has servers given a moment, not seconds, to answer.
func shortWait(t *testing.T) {
	old := answerWait
	answerWait = 200 * time.Millisecond
	t.Cleanup(func() { answerWait = old })
}

// hangingSite answers until told to hang: then it accepts every request and
// says nothing, as a proxy that lost its way does.
type hangingSite struct {
	hang     atomic.Bool
	asked    atomic.Int32
	released chan struct{}
	once     sync.Once
}

func newHangingSite(t *testing.T) (*hangingSite, *httptest.Server) {
	s := &hangingSite{released: make(chan struct{})}
	srv := httptest.NewServer(s)
	t.Cleanup(func() {
		s.once.Do(func() { close(s.released) })
		srv.Close()
	})
	return s, srv
}

func (s *hangingSite) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if s.hang.Load() {
		s.asked.Add(1)
		select {
		case <-r.Context().Done():
		case <-s.released:
		}
		return
	}
	w.Header().Set("ETag", `W/"`+r.URL.Path+`"`)
	if r.Method == http.MethodGet {
		_, _ = io.WriteString(w, "page "+r.URL.Path)
	}
}

// With upstream accepting requests and never answering, what is kept is
// used after one short wait — not one full timeout per page and per command
// — and the next command uses it at once.
func TestHangingUpstreamUsesWhatIsKept(t *testing.T) {
	shortWait(t)
	site, srv := newHangingSite(t)
	dir := t.TempDir()
	ctx := context.Background()
	builds := 0
	// run is one command over the cache: a value memoized from a request the
	// cache does not track (as the wol config is), a page, and a memo built
	// from another page
	run := func() {
		t.Helper()
		cache := Open(dir, CacheOptions{Fresh: -1})
		defer cache.Close()
		c := New(WithResponseCache(cache))
		cfg, err := Memo(ctx, cache, "cfg", func(ctx context.Context) (string, error) {
			builds++
			resp, err := c.Get(ctx, srv.URL+"/home", nil)
			if err != nil {
				return "", err
			}
			defer resp.Body.Close()
			b, err := io.ReadAll(resp.Body)
			return string(b), err
		})
		if err != nil || cfg != "page /home" {
			t.Fatalf("cfg: %q, %v", cfg, err)
		}
		if got, err := c.GetText(ctx, srv.URL+"/a", nil); err != nil || got != "page /a" {
			t.Fatalf("page: %q, %v", got, err)
		}
		derived, err := Memo(ctx, cache, "derived", func(ctx context.Context) (string, error) {
			return c.GetText(ctx, srv.URL+"/b", nil)
		})
		if err != nil || derived != "page /b" {
			t.Fatalf("memo: %q, %v", derived, err)
		}
	}
	run()

	site.hang.Store(true)
	start := time.Now()
	run()
	if took := time.Since(start); took > 5*answerWait {
		t.Errorf("a command took %s with upstream hanging, want about one wait of %s", took, answerWait)
	}
	if n := site.asked.Swap(0); n != 1 {
		t.Errorf("%d requests to the hanging host, want 1", n)
	}

	// the next command knows: it asks nothing, and does not rebuild
	builds = 0
	start = time.Now()
	run()
	if took := time.Since(start); took > 5*answerWait {
		t.Errorf("the next command took %s", took)
	}
	if n := site.asked.Load(); n != 0 || builds != 0 {
		t.Errorf("next command: %d requests, %d builds; want none", n, builds)
	}

	// once the back-off has run out, upstream is asked again
	site.hang.Store(false)
	cache := Open(dir, CacheOptions{Fresh: -1})
	pastBackOff(New(WithResponseCache(cache)))
	builds = 0
	run()
	if builds != 1 {
		t.Errorf("after the back-off: %d builds, want 1", builds)
	}
}

// A memo built from nothing the cache tracks, whose rebuild failed, is not
// rebuilt again on every use — by this command or the next.
func TestFailedRebuildWaits(t *testing.T) {
	var status atomic.Int32
	var gets atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gets.Add(1)
		if s := status.Load(); s != 0 {
			w.WriteHeader(int(s))
			return
		}
		_, _ = io.WriteString(w, "v1")
	}))
	defer srv.Close()
	dir := t.TempDir()
	ctx := context.Background()
	get := func() string {
		t.Helper()
		cache := Open(dir, CacheOptions{Fresh: -1})
		c := New(WithResponseCache(cache))
		v, err := Memo(ctx, cache, "plain", func(ctx context.Context) (string, error) {
			resp, err := c.Get(ctx, srv.URL, nil)
			if err != nil {
				return "", err
			}
			defer resp.Body.Close()
			b, err := io.ReadAll(resp.Body)
			return string(b), err
		})
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	get()
	status.Store(http.StatusInternalServerError)
	gets.Store(0)
	for range 3 {
		if got := get(); got != "v1" {
			t.Fatalf("got %q, want the last value", got)
		}
	}
	if n := gets.Load(); n != 1 {
		t.Errorf("%d requests, want the one that failed", n)
	}
}

// A refused connection is answered at once from what is kept, as before.
func TestRefusedUpstreamUsesWhatIsKept(t *testing.T) {
	shortWait(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "kept")
	}))
	c := staleClient(t)
	ctx := context.Background()
	if _, err := c.GetText(ctx, srv.URL, nil); err != nil {
		t.Fatal(err)
	}
	srv.Close()
	start := time.Now()
	for range 3 {
		if got, err := c.GetText(ctx, srv.URL, nil); err != nil || got != "kept" {
			t.Fatalf("%q, %v", got, err)
		}
	}
	if took := time.Since(start); took > 5*answerWait {
		t.Errorf("took %s", took)
	}
}

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

// Hosts are backed off one by one, with a cache or without, and an answer
// lifts the back-off of its host alone.
func TestHostsBackOffOneByOne(t *testing.T) {
	for _, c := range []*Client{New(), staleClient(t)} {
		c.hostFailed("a")
		c.hostFailed("b")
		if !c.hostDown("a") || !c.hostDown("b") {
			t.Fatalf("a %v, b %v; want both down", c.hostDown("a"), c.hostDown("b"))
		}
		c.hostAnswered("a")
		if c.hostDown("a") || !c.hostDown("b") {
			t.Errorf("a %v, b %v; want b alone down", c.hostDown("a"), c.hostDown("b"))
		}
		if c.responses != nil {
			next := New(WithResponseCache(c.responses))
			if next.hostDown("a") || !next.hostDown("b") {
				t.Errorf("next process: a %v, b %v; want b alone down", next.hostDown("a"), next.hostDown("b"))
			}
		}
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

// quickRetries has busy servers asked again at once.
func quickRetries(t *testing.T, delays ...time.Duration) {
	old := retryDelays
	retryDelays = []time.Duration{time.Millisecond, 2 * time.Millisecond}
	if delays != nil {
		retryDelays = delays
	}
	t.Cleanup(func() { retryDelays = old })
}

// busySite answers its next busy requests with status, then the page.
type busySite struct {
	mu         sync.Mutex
	busy       int
	status     int
	retryAfter string
	heads      atomic.Int32
	gets       atomic.Int32
}

func (s *busySite) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodHead {
		s.heads.Add(1)
	} else {
		s.gets.Add(1)
	}
	s.mu.Lock()
	busy, status, retryAfter := s.busy > 0, s.status, s.retryAfter
	if busy {
		s.busy--
	}
	s.mu.Unlock()
	if busy {
		if retryAfter != "" {
			w.Header().Set("Retry-After", retryAfter)
		}
		w.WriteHeader(status)
		return
	}
	w.Header().Set("ETag", `W/"page"`)
	if r.Method == http.MethodGet {
		_, _ = io.WriteString(w, "page")
	}
}

func (s *busySite) set(busy, status int, retryAfter string) {
	s.mu.Lock()
	s.busy, s.status, s.retryAfter = busy, status, retryAfter
	s.mu.Unlock()
	s.heads.Store(0)
	s.gets.Store(0)
}

// A server that says it is busy, or that its gateway failed, is asked again
// a couple of times before a read with nothing kept gives up.
func TestBusyUpstreamIsAskedAgain(t *testing.T) {
	quickRetries(t)
	ctx := context.Background()
	site := &busySite{}
	srv := httptest.NewServer(site)
	defer srv.Close()

	for _, status := range []int{http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout, http.StatusTooManyRequests} {
		site.set(2, status, "")
		c := New(WithResponseCache(Open(t.TempDir(), CacheOptions{})))
		if got, err := c.GetText(ctx, srv.URL, nil); err != nil || got != "page" {
			t.Fatalf("after two %d: %q, %v", status, got, err)
		}
		if n := site.gets.Load(); n != 3 {
			t.Errorf("after two %d: %d requests, want 3", status, n)
		}
	}

	// three times is all
	site.set(3, http.StatusServiceUnavailable, "")
	c := New()
	if _, err := c.GetText(ctx, srv.URL, nil); err == nil {
		t.Error("a server busy three times answered")
	}
	if n := site.gets.Load(); n != 3 {
		t.Errorf("%d requests, want 3", n)
	}
	// a 500 is about what was asked: not asked again
	site.set(1, http.StatusInternalServerError, "")
	if _, err := c.GetText(ctx, srv.URL, nil); err == nil || site.gets.Load() != 1 {
		t.Errorf("500: %v after %d requests, want an error after 1", err, site.gets.Load())
	}
	// a server asking for a long pause is not waited on
	site.set(1, http.StatusServiceUnavailable, "120")
	start := time.Now()
	if _, err := c.GetText(ctx, srv.URL, nil); err == nil || site.gets.Load() != 1 || time.Since(start) > time.Second {
		t.Errorf("Retry-After 120: %v after %d requests", err, site.gets.Load())
	}
	// a short one is
	site.set(1, http.StatusServiceUnavailable, "0")
	if _, err := c.GetText(ctx, srv.URL, nil); err != nil || site.gets.Load() != 2 {
		t.Errorf("Retry-After 0: %v after %d requests", err, site.gets.Load())
	}
}

// Reads of a page that share one request share its retries too.
func TestBusyUpstreamSharedReads(t *testing.T) {
	quickRetries(t, 50*time.Millisecond, time.Millisecond)
	site := &busySite{}
	site.set(1, http.StatusServiceUnavailable, "")
	srv := httptest.NewServer(site)
	defer srv.Close()
	c := New(WithResponseCache(Open(t.TempDir(), CacheOptions{})))
	release := make(chan struct{})
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			<-release
			if s, err := c.GetText(context.Background(), srv.URL, nil); err != nil || s != "page" {
				t.Errorf("GetText: %q %v", s, err)
			}
		})
	}
	close(release)
	wg.Wait()
	if n := site.gets.Load(); n != 2 {
		t.Errorf("%d requests, want 2", n)
	}
}

// A revalidation is not held up by retries: the kept body answers at once,
// and the host is left alone for a while.
func TestBusyUpstreamDuringRevalidation(t *testing.T) {
	quickRetries(t, time.Hour)
	ctx := context.Background()
	site := &busySite{}
	srv := httptest.NewServer(site)
	defer srv.Close()
	c := staleClient(t)
	if _, err := c.GetText(ctx, srv.URL, nil); err != nil {
		t.Fatal(err)
	}
	site.set(5, http.StatusServiceUnavailable, "")
	for range 3 {
		if got, err := c.GetText(ctx, srv.URL, nil); err != nil || got != "page" {
			t.Fatalf("%q, %v", got, err)
		}
	}
	if h, g := site.heads.Load(), site.gets.Load(); h != 1 || g != 0 {
		t.Errorf("%d HEAD, %d GET; want the one HEAD", h, g)
	}
}

// A caller that gives up during the pause is let go at once.
func TestBusyUpstreamCancelled(t *testing.T) {
	quickRetries(t, time.Hour)
	site := &busySite{}
	site.set(5, http.StatusServiceUnavailable, "")
	srv := httptest.NewServer(site)
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := New().Get(ctx, srv.URL, nil); err == nil || time.Since(start) > time.Second {
		t.Errorf("%v after %s", err, time.Since(start))
	}
}

package httpx

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
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

	c := New(WithResponseCache(OpenCacheAt(dir)))
	for range 2 {
		if err := c.GetJSON(ctx, srv.URL+"/a", nil, &out); err != nil || out.N != 1 {
			t.Fatalf("GetJSON: %v %+v", err, out)
		}
	}
	if hits.Load() != 1 {
		t.Errorf("%d upstream reads, want 1", hits.Load())
	}
	restarted := New(WithResponseCache(OpenCacheAt(dir)))
	if _, err := restarted.GetText(ctx, srv.URL+"/a#frag", http.Header{"Accept": {"application/json"}}); err != nil {
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

// upstream is a page whose body, validators and availability a test changes
// as it goes, counting the requests of each method.
type upstream struct {
	mu      sync.Mutex
	body    string
	etag    bool // send an ETag derived from the body
	lastMod string
	length  bool // send a Content-Length on HEAD
	status  int  // when set, every request answers with it
	// when set, the next HEADs answer with it, or every one with headsLeft 0
	headStatus int
	headsLeft  int
	heads      atomic.Int32
	gets       atomic.Int32
}

func (u *upstream) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	u.mu.Lock()
	body, status := u.body, u.status
	if r.Method == http.MethodHead && u.headStatus != 0 {
		status = u.headStatus
		if u.headsLeft > 0 {
			if u.headsLeft--; u.headsLeft == 0 {
				u.headStatus = 0
			}
		}
	}
	if u.etag {
		w.Header().Set("ETag", fmt.Sprintf(`W/"%x"`, bodyHash([]byte(body))))
	}
	if u.lastMod != "" {
		w.Header().Set("Last-Modified", u.lastMod)
	}
	length := u.length
	u.mu.Unlock()
	if r.Method == http.MethodHead {
		u.heads.Add(1)
	} else {
		u.gets.Add(1)
	}
	if status != 0 {
		w.WriteHeader(status)
		return
	}
	if r.Method == http.MethodHead {
		if length {
			w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		}
		return
	}
	w.Write([]byte(body))
}

func (u *upstream) set(f func(u *upstream)) {
	u.mu.Lock()
	f(u)
	u.mu.Unlock()
}

func (u *upstream) counts() (heads, gets int32) {
	return u.heads.Swap(0), u.gets.Swap(0)
}

// always stale: every read past the first revalidates
func staleClient(t *testing.T) *Client {
	return New(WithResponseCache(Open(t.TempDir(), CacheOptions{Fresh: -1})))
}

func TestRevalidation(t *testing.T) {
	ctx := context.Background()
	text := func(c *Client, url string) string {
		t.Helper()
		s, err := c.GetText(ctx, url, nil)
		if err != nil {
			t.Fatalf("GetText: %v", err)
		}
		return s
	}

	t.Run("within the window nothing is asked", func(t *testing.T) {
		u := &upstream{body: "one", etag: true}
		srv := httptest.NewServer(u)
		defer srv.Close()
		c := New(WithResponseCache(OpenCacheAt(t.TempDir())))
		text(c, srv.URL)
		text(c, srv.URL)
		if h, g := u.counts(); h != 0 || g != 1 {
			t.Errorf("%d HEAD, %d GET; want 0, 1", h, g)
		}
	})

	t.Run("an unchanged ETag costs one HEAD", func(t *testing.T) {
		u := &upstream{body: "one", etag: true}
		srv := httptest.NewServer(u)
		defer srv.Close()
		c := staleClient(t)
		text(c, srv.URL)
		u.counts()
		if got := text(c, srv.URL); got != "one" {
			t.Errorf("body %q", got)
		}
		if h, g := u.counts(); h != 1 || g != 0 {
			t.Errorf("%d HEAD, %d GET; want 1, 0", h, g)
		}
	})

	t.Run("a changed ETag reads the new body", func(t *testing.T) {
		u := &upstream{body: "one", etag: true}
		srv := httptest.NewServer(u)
		defer srv.Close()
		c := staleClient(t)
		text(c, srv.URL)
		u.set(func(u *upstream) { u.body = "two" })
		u.counts()
		if got := text(c, srv.URL); got != "two" {
			t.Errorf("body %q, want the new one", got)
		}
		if h, g := u.counts(); h != 1 || g != 1 {
			t.Errorf("%d HEAD, %d GET; want 1, 1", h, g)
		}
	})

	t.Run("a length is all the CDN says", func(t *testing.T) {
		u := &upstream{body: "one", length: true}
		srv := httptest.NewServer(u)
		defer srv.Close()
		c := staleClient(t)
		text(c, srv.URL)
		u.counts()
		// a length that stayed is no proof: a GET decides, and the HEAD,
		// which never could, is not sent again
		text(c, srv.URL)
		if h, g := u.counts(); h != 1 || g != 1 {
			t.Errorf("same length: %d HEAD, %d GET; want 1, 1", h, g)
		}
		u.set(func(u *upstream) { u.body = "two" })
		if got := text(c, srv.URL); got != "two" {
			t.Errorf("a change of the same length: body %q, want the new one", got)
		}
		if h, g := u.counts(); h != 0 || g != 1 {
			t.Errorf("after: %d HEAD, %d GET; want 0, 1", h, g)
		}
		u.set(func(u *upstream) { u.body = "three" })
		if got := text(c, srv.URL); got != "three" {
			t.Errorf("body %q, want the new one", got)
		}
	})

	t.Run("a HEAD refused once is asked again next time", func(t *testing.T) {
		u := &upstream{body: "one", etag: true, headStatus: http.StatusForbidden, headsLeft: 1}
		srv := httptest.NewServer(u)
		defer srv.Close()
		c := staleClient(t)
		text(c, srv.URL)
		u.counts()
		if got := text(c, srv.URL); got != "one" {
			t.Errorf("body %q", got)
		}
		if h, g := u.counts(); h != 1 || g != 1 {
			t.Errorf("refused HEAD: %d HEAD, %d GET; want 1, 1", h, g)
		}
		text(c, srv.URL)
		if h, g := u.counts(); h != 1 || g != 0 {
			t.Errorf("then: %d HEAD, %d GET; want 1, 0", h, g)
		}
	})

	t.Run("a HEAD the server does not answer is not sent again", func(t *testing.T) {
		u := &upstream{body: "one", etag: true, headStatus: http.StatusMethodNotAllowed}
		srv := httptest.NewServer(u)
		defer srv.Close()
		c := staleClient(t)
		text(c, srv.URL)
		text(c, srv.URL)
		u.counts()
		text(c, srv.URL)
		if h, g := u.counts(); h != 0 || g != 1 {
			t.Errorf("%d HEAD, %d GET; want 0, 1", h, g)
		}
	})

	t.Run("no validators: a GET decides, and an equal body is no change", func(t *testing.T) {
		u := &upstream{body: "one"}
		srv := httptest.NewServer(u)
		defer srv.Close()
		dir := t.TempDir()
		c := New(WithResponseCache(Open(dir, CacheOptions{Fresh: -1})))
		text(c, srv.URL)
		u.counts()
		text(c, srv.URL)
		if h, g := u.counts(); h != 1 || g != 1 {
			t.Errorf("%d HEAD, %d GET; want 1, 1", h, g)
		}
		// and the body is current again for a window
		fresh := New(WithResponseCache(Open(dir, CacheOptions{Fresh: time.Hour})))
		text(fresh, srv.URL)
		if h, g := u.counts(); h != 0 || g != 0 {
			t.Errorf("after the check: %d HEAD, %d GET; want none", h, g)
		}
	})

	t.Run("an unreachable upstream serves what is kept", func(t *testing.T) {
		u := &upstream{body: "one", etag: true}
		srv := httptest.NewServer(u)
		defer srv.Close()
		c := staleClient(t)
		text(c, srv.URL)
		u.set(func(u *upstream) { u.status = http.StatusBadGateway })
		if got := text(c, srv.URL); got != "one" {
			t.Errorf("body %q, want the kept one", got)
		}
		u.counts()
		// and does not ask again for a while
		text(c, srv.URL)
		if h, g := u.counts(); h != 0 || g != 0 {
			t.Errorf("during the back-off: %d HEAD, %d GET; want none", h, g)
		}
	})

	t.Run("content gone upstream is gone here", func(t *testing.T) {
		u := &upstream{body: "one", etag: true}
		srv := httptest.NewServer(u)
		defer srv.Close()
		c := staleClient(t)
		text(c, srv.URL)
		u.set(func(u *upstream) { u.status = http.StatusNotFound })
		if _, err := c.GetText(ctx, srv.URL, nil); !IsGone(err) {
			t.Errorf("err = %v, want a 404", err)
		}
	})
}

// Concurrent reads of the same page share one request.
func TestConcurrentReadsShareARequest(t *testing.T) {
	release := make(chan struct{})
	var gets atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gets.Add(1)
		<-release
		w.Write([]byte("body"))
	}))
	defer srv.Close()
	c := New(WithResponseCache(OpenCacheAt(t.TempDir())))
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if s, err := c.GetText(context.Background(), srv.URL, nil); err != nil || s != "body" {
				t.Errorf("GetText: %q %v", s, err)
			}
		})
	}
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()
	if gets.Load() != 1 {
		t.Errorf("%d requests, want 1", gets.Load())
	}
}

func TestMemoFollowsItsSources(t *testing.T) {
	ctx := context.Background()
	u := &upstream{body: "one", etag: true}
	srv := httptest.NewServer(u)
	defer srv.Close()
	cache := Open(t.TempDir(), CacheOptions{Fresh: -1})
	c := New(WithResponseCache(cache))
	builds := 0
	getIn := func(ctx context.Context) string {
		t.Helper()
		v, err := Memo(ctx, cache, "k", func(ctx context.Context) (string, error) {
			builds++
			s, err := c.GetText(ctx, srv.URL, nil)
			return "derived " + s, err
		})
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	get := func() string { return getIn(ctx) }
	if got := get(); got != "derived one" || builds != 1 {
		t.Fatalf("%q after %d builds", got, builds)
	}
	u.counts()

	// an unchanged source costs a HEAD and no rebuild
	if got := get(); got != "derived one" || builds != 1 {
		t.Errorf("unchanged source: %q after %d builds, want no rebuild", got, builds)
	}
	if h, g := u.counts(); h != 1 || g != 0 {
		t.Errorf("unchanged source: %d HEAD, %d GET; want 1, 0", h, g)
	}

	// a changed one rebuilds, reading the new body once
	u.set(func(u *upstream) { u.body = "two" })
	if got := get(); got != "derived two" || builds != 2 {
		t.Errorf("changed source: %q after %d builds", got, builds)
	}
	if h, g := u.counts(); h != 1 || g != 1 {
		t.Errorf("changed source: %d HEAD, %d GET; want 1, 1", h, g)
	}

	// a failing rebuild keeps the last value
	u.set(func(u *upstream) { u.body = "three"; u.status = http.StatusInternalServerError })
	pastBackOff(c)
	if got := get(); got != "derived two" {
		t.Errorf("upstream down: %q, want the last value", got)
	}

	// a memo of memos depends on what they depend on
	u.set(func(u *upstream) { u.status = 0 })
	pastBackOff(c)
	outer, err := Memo(ctx, cache, "outer", func(ctx context.Context) (string, error) {
		return "outer " + getIn(ctx), nil
	})
	if err != nil || outer != "outer derived three" {
		t.Fatalf("outer = %q, %v", outer, err)
	}
	m, _ := cache.loadMemo("outer")
	if len(m.Deps) != 1 {
		t.Errorf("outer memo deps = %v, want the page", m.Deps)
	}
}

// A memo whose source on disk has moved on, while upstream still serves the
// body the memo was built from, has that body kept again: otherwise nothing
// reads it, and every use asks upstream for it anew.
func TestMemoKeepsTheBodyItWasBuiltFrom(t *testing.T) {
	ctx := context.Background()
	u := &upstream{body: "one", etag: true}
	srv := httptest.NewServer(u)
	defer srv.Close()
	cache := Open(t.TempDir(), CacheOptions{Fresh: -1})
	c := New(WithResponseCache(cache))
	builds := 0
	get := func() string {
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
	get()
	// another body was kept since — read on its own, say, from a server
	// that answered differently for a while
	id := entryID(c.responseKey(srv.URL, http.Header{}))
	cache.saveEntry(id, entry{URL: srv.URL, ETag: `W/"other"`}, []byte("two"))
	u.counts()
	if got := get(); got != "one" || builds != 1 {
		t.Fatalf("%q after %d builds, want the memo as it was", got, builds)
	}
	if _, body, _ := cache.loadEntry(id, false); string(body) != "one" {
		t.Errorf("kept body %q, want the one upstream serves", body)
	}
	// and the next command asks with a HEAD alone
	c = New(WithResponseCache(cache))
	u.counts()
	get()
	if h, g := u.counts(); h != 1 || g != 0 {
		t.Errorf("then: %d HEAD, %d GET; want 1, 0", h, g)
	}
}

// The hash a body is told apart by leaves out the nonce every render of a wol
// page carries, and nothing else.
func TestBodyHashIgnoresNonces(t *testing.T) {
	page := func(nonce, text string) []byte {
		return fmt.Appendf(nil, "<script nonce=\"%s\">x()</script>\n<style nonce=\"%s\" id=\"s\"></style><p>%s</p>", nonce, nonce, text)
	}
	if bodyHash(page("5b1c0a", "one")) != bodyHash(page("9f00ee", "one")) {
		t.Error("two renders of one page hash apart")
	}
	if bodyHash(page("5b1c0a", "one")) == bodyHash(page("5b1c0a", "two")) {
		t.Error("a changed page hashes the same")
	}
	// a body without a nonce hashes as it did before, so what is kept
	// stays current
	plain := []byte(`{"n": 1, "text": "nonce=\"x\" or anonce="y"}`)
	sum := sha256.Sum256(plain)
	if bodyHash(plain) != hex.EncodeToString(sum[:16]) {
		t.Error("a body without a nonce attribute hashes differently")
	}
}

// wolPage renders a page as wol.jw.org does: a nonce of its own in every
// response, and a weak ETag that differs between renders and between HEAD and
// GET.
type wolPage struct {
	mu          sync.Mutex
	text        string
	renders     int
	heads, gets atomic.Int32
}

func (p *wolPage) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	p.renders++
	n, text := p.renders, p.text
	p.mu.Unlock()
	w.Header().Set("ETag", fmt.Sprintf(`W/"%s-%d"`, r.Method, n))
	if r.Method == http.MethodHead {
		p.heads.Add(1)
		return
	}
	p.gets.Add(1)
	fmt.Fprintf(w, "<html><script nonce=\"%08x\">init()</script><body>%s</body></html>", n*7919, text)
}

// A page whose every render differs in its nonce alone is no change: it is
// confirmed, not kept anew, and nothing built from it is rebuilt.
func TestRevalidationIgnoresNonces(t *testing.T) {
	ctx := context.Background()
	page := &wolPage{text: "one"}
	srv := httptest.NewServer(page)
	defer srv.Close()
	cache := Open(t.TempDir(), CacheOptions{Fresh: -1})
	c := New(WithResponseCache(cache))
	builds := 0
	get := func() string {
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
	first := get()
	id := entryID(c.responseKey(srv.URL, http.Header{}))
	kept, _, _ := cache.loadEntry(id, true)
	for range 3 {
		if got := get(); got != first {
			t.Fatalf("body %q, want the kept %q", got, first)
		}
	}
	if builds != 1 {
		t.Errorf("%d builds, want 1: a new nonce rebuilt the memo", builds)
	}
	if e, _, _ := cache.loadEntry(id, true); e.Hash != kept.Hash {
		t.Errorf("hash moved from %s to %s", kept.Hash, e.Hash)
	}
	// a real change is still one
	page.mu.Lock()
	page.text = "two"
	page.mu.Unlock()
	if got := get(); !strings.Contains(got, "two") || builds != 2 {
		t.Errorf("changed page: %q after %d builds", got, builds)
	}
}

func TestEvictionKeepsTheRecentlyUsed(t *testing.T) {
	dir := t.TempDir()
	cache := Open(dir, CacheOptions{MaxBytes: 10 << 10})
	body := make([]byte, 1<<10)
	// every entry is used a second after the one before: files written back
	// to back share a modification time (the file system's clock ticks in
	// milliseconds), which would leave which of them is the least recently
	// used to chance
	old := time.Now().Add(-48 * time.Hour)
	recent := time.Now().Add(-time.Minute)
	for i := range 20 {
		id := entryID(strconv.Itoa(i))
		cache.saveEntry(id, entry{URL: strconv.Itoa(i)}, body)
		used := recent.Add(time.Duration(i) * time.Second)
		if i < 10 {
			used = old.Add(time.Duration(i) * time.Second)
		}
		_ = os.Chtimes(cache.entryPath(id), used, used)
	}
	cache.Close()
	var total int64
	entries, _ := os.ReadDir(filepath.Join(dir, responseDir))
	for _, e := range entries {
		info, _ := e.Info()
		total += info.Size()
	}
	if total > 10<<10 {
		t.Errorf("%d bytes left, over the 10 KiB limit", total)
	}
	for i := 15; i < 20; i++ {
		if _, _, ok := cache.loadEntry(entryID(strconv.Itoa(i)), true); !ok {
			t.Errorf("recently written entry %d was evicted", i)
		}
	}
	if _, _, ok := cache.loadEntry(entryID("0"), true); ok {
		t.Errorf("the least recently used entry survived")
	}
}

func TestOldLayoutIsDropped(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, responseDir), 0o755)
	_ = os.WriteFile(filepath.Join(dir, responseDir, entryID("x")), []byte("raw body"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "binav-en-nwtsty.json"), []byte("{}"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "last-results.json"), []byte("{}"), 0o644)
	cache := OpenCacheAt(dir)
	if _, _, ok := cache.loadEntry(entryID("x"), false); ok {
		t.Error("a body without validators was read")
	}
	if _, err := os.Stat(filepath.Join(dir, "binav-en-nwtsty.json")); err == nil {
		t.Error("an old derived blob survived")
	}
	if _, err := os.Stat(filepath.Join(dir, "last-results.json")); err != nil {
		t.Error("the results of the last listing were removed")
	}
}

func TestParseSize(t *testing.T) {
	for in, want := range map[string]int64{
		"1GB": 1 << 30, "1gb": 1 << 30, "1GiB": 1 << 30, "512M": 512 << 20, "1.5K": 1536,
		"100": 100, "0": -1, "off": -1, "2 TB": 2 << 40,
	} {
		if got, err := ParseSize(in); err != nil || got != want {
			t.Errorf("ParseSize(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "lots", "-1G", "1X"} {
		if _, err := ParseSize(bad); err == nil {
			t.Errorf("ParseSize(%q) accepted", bad)
		}
	}
}

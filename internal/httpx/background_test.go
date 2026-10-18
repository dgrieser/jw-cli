package httpx

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

// drained waits until the background tasks scheduled so far, and those they
// scheduled, are done.
func drained(c *Client) { c.bg.running.Wait() }

func backgroundClient(t *testing.T) (*Client, *Cache) {
	t.Helper()
	cache := Open(t.TempDir(), CacheOptions{Fresh: -1})
	c := New(WithResponseCache(cache), WithBackgroundRevalidation())
	// before the directory goes
	t.Cleanup(func() { drained(c) })
	return c, cache
}

// With background revalidation a stale body is used at once, and asked about
// upstream after: the reader waits on nothing, and the next one reads what
// upstream answered.
func TestStaleBodyIsRevalidatedInTheBackground(t *testing.T) {
	ctx := context.Background()
	u := &upstream{body: "one", etag: true}
	srv := httptest.NewServer(u)
	defer srv.Close()
	c, _ := backgroundClient(t)
	if got, err := c.GetText(ctx, srv.URL, nil); err != nil || got != "one" {
		t.Fatalf("first read: %q, %v", got, err)
	}
	u.counts()

	u.set(func(u *upstream) { u.body = "two" })
	if got, _ := c.GetText(ctx, srv.URL, nil); got != "one" {
		t.Errorf("stale read: %q, want the kept body at once", got)
	}
	drained(c)
	if h, g := u.counts(); h != 1 || g != 1 {
		t.Errorf("background revalidation: %d HEAD, %d GET; want 1, 1", h, g)
	}
	if got, _ := c.GetText(ctx, srv.URL, nil); got != "two" {
		t.Errorf("read after revalidation: %q, want the new body", got)
	}

	// work nobody waits on revalidates before it reads
	u.set(func(u *upstream) { u.body = "three" })
	drained(c)
	u.counts()
	if got, _ := c.GetText(Background(ctx), srv.URL, nil); got != "three" {
		t.Errorf("background read: %q, want the body revalidated first", got)
	}
}

// A body kept as JSON is not replaced in the background by one that is not:
// the reader who would have refused it is not there to.
func TestBackgroundKeepsOnlyWhatDecodesAlike(t *testing.T) {
	ctx := context.Background()
	u := &upstream{body: `{"n":1}`, etag: true}
	srv := httptest.NewServer(u)
	defer srv.Close()
	c, _ := backgroundClient(t)
	var out struct{ N int }
	if err := c.GetJSON(ctx, srv.URL, nil, &out); err != nil {
		t.Fatal(err)
	}
	u.set(func(u *upstream) { u.body = "<html>maintenance</html>" })
	if err := c.GetJSON(ctx, srv.URL, nil, &out); err != nil || out.N != 1 {
		t.Fatalf("stale read: %v %+v", err, out)
	}
	drained(c)
	if err := c.GetJSON(ctx, srv.URL, nil, &out); err != nil || out.N != 1 {
		t.Errorf("after revalidation: %v %+v, want the JSON kept", err, out)
	}
}

// A memo whose sources are stale is used as it is, and rebuilt in the
// background when one of them changed.
func TestStaleMemoIsRebuiltInTheBackground(t *testing.T) {
	ctx := context.Background()
	u := &upstream{body: "one", etag: true}
	srv := httptest.NewServer(u)
	defer srv.Close()
	c, cache := backgroundClient(t)
	var builds atomic.Int32
	get := func() string {
		t.Helper()
		v, err := Memo(ctx, cache, "k", func(ctx context.Context) (string, error) {
			builds.Add(1)
			s, err := c.GetText(ctx, srv.URL, nil)
			return "derived " + s, err
		})
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	if got := get(); got != "derived one" {
		t.Fatalf("first: %q", got)
	}
	u.counts()

	// unchanged: used at once, one HEAD behind it, no rebuild
	if got := get(); got != "derived one" {
		t.Errorf("stale memo: %q", got)
	}
	drained(c)
	if h, g := u.counts(); h != 1 || g != 0 || builds.Load() != 1 {
		t.Errorf("unchanged source: %d HEAD, %d GET, %d builds; want 1, 0, 1", h, g, builds.Load())
	}

	// changed: used at once, rebuilt behind it
	u.set(func(u *upstream) { u.body = "two" })
	if got := get(); got != "derived one" {
		t.Errorf("stale memo of a changed source: %q, want the last value at once", got)
	}
	drained(c)
	if builds.Load() != 2 {
		t.Errorf("%d builds, want the background's rebuild", builds.Load())
	}
	if got := get(); got != "derived two" {
		t.Errorf("after the background: %q", got)
	}
}

// Background work waits while a reader's request is under way — for a
// while, not for ever.
func TestBackgroundGivesWayToReaders(t *testing.T) {
	defer func(y, s time.Duration) { yieldFor, settle = y, s }(yieldFor, settle)
	yieldFor, settle = 300*time.Millisecond, 10*time.Millisecond
	var asked atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked.Store(time.Now().UnixNano())
	}))
	defer srv.Close()
	c, _ := backgroundClient(t)

	done := c.Busy()
	start := time.Now()
	c.Go("t", func(ctx context.Context) { _, _ = c.Head(ctx, srv.URL, nil) })
	time.Sleep(100 * time.Millisecond)
	if asked.Load() != 0 {
		t.Fatal("asked upstream while a reader's request was under way")
	}
	done()
	drained(c)
	if at := time.Unix(0, asked.Load()); at.Sub(start) < 100*time.Millisecond {
		t.Errorf("asked %s after scheduling, want after the reader was done", at.Sub(start))
	}

	// a reader at length holds it up no longer than yieldFor
	asked.Store(0)
	done = c.Busy()
	defer done()
	start = time.Now()
	c.Go("t", func(ctx context.Context) { _, _ = c.Head(ctx, srv.URL, nil) })
	drained(c)
	if waited := time.Since(start); asked.Load() == 0 || waited > 2*time.Second {
		t.Errorf("background held up %s by a long reader", waited)
	}
}

// What is kept in memory of a file is used only while the file is the one it
// was read from: another process writing the directory is seen.
func TestHotCacheSeesOtherWriters(t *testing.T) {
	dir := t.TempDir()
	a, b := OpenCacheAt(dir), OpenCacheAt(dir)
	a.Put("k", "one")
	var v string
	if !a.Get("k", &v) || v != "one" {
		t.Fatalf("Get = %q", v)
	}
	b.Put("k", "a longer two")
	if !a.Get("k", &v) || v != "a longer two" {
		t.Errorf("after another writer: %q", v)
	}
	b.Put("k", "same three") // same size as the last, written later
	st, _ := os.Stat(b.memoPath("k"))
	later := st.ModTime().Add(time.Second)
	_ = os.Chtimes(b.memoPath("k"), later, later)
	if !a.Get("k", &v) || v != "same three" {
		t.Errorf("after a write of the same size: %q", v)
	}
	b.removeMemo("k")
	if a.Get("k", &v) {
		t.Error("a removed memo is still read")
	}
}

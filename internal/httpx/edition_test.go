package httpx

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"sync"
	"testing"
)

// fakeEditions reads "ed=NNNN" out of a body and busts with ?_=.
type fakeEditions struct{}

var edField = regexp.MustCompile(`ed=(\d+)`)

var scopeField = regexp.MustCompile(`lib=(\w+)`)

func (fakeEditions) Edition(_ string, body []byte) (string, int, bool) {
	m := edField.FindSubmatch(body)
	if m == nil {
		return "", 0, false
	}
	n, _ := strconv.Atoi(string(m[1]))
	scope := "lib"
	if s := scopeField.FindSubmatch(body); s != nil {
		scope = string(s[1])
	}
	return scope, n, true
}

func (fakeEditions) Bust(rawURL string, try int) string {
	u, _ := url.Parse(rawURL)
	q := u.Query()
	q.Set("_", strconv.Itoa(try))
	u.RawQuery = q.Encode()
	return u.String()
}

// editionSite answers each path with the editions queued for it, the last
// one over and over; it counts requests.
type editionSite struct {
	mu    sync.Mutex
	queue map[string][]int
	gets  map[string]int
	busts int
}

func (s *editionSite) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if r.URL.Path == "/r" {
		http.Redirect(w, r, "/a", http.StatusTemporaryRedirect)
		return
	}
	s.gets[r.URL.Path]++
	if r.URL.Query().Has("_") {
		s.busts++
	}
	q := s.queue[r.URL.Path]
	ed := q[0]
	if len(q) > 1 {
		s.queue[r.URL.Path] = q[1:]
	}
	if lib := r.URL.Query().Get("lib"); lib != "" {
		fmt.Fprintf(w, "%s lib=%s ed=%d", r.URL.Path, lib, ed)
		return
	}
	fmt.Fprintf(w, "%s ed=%d", r.URL.Path, ed)
}

func (s *editionSite) serve(path string, eds ...int) {
	s.mu.Lock()
	s.queue[path] = eds
	s.mu.Unlock()
}

func editionClient(t *testing.T) (*Client, *editionSite, *httptest.Server) {
	t.Helper()
	site := &editionSite{queue: map[string][]int{}, gets: map[string]int{}}
	srv := httptest.NewServer(site)
	t.Cleanup(srv.Close)
	c := New(WithResponseCache(Open(t.TempDir(), CacheOptions{})), WithEditions(fakeEditions{}))
	return c, site, srv
}

func TestEditionsNeverOlder(t *testing.T) {
	ctx := context.Background()
	c, site, srv := editionClient(t)
	get := func(ctx context.Context, path string) string {
		t.Helper()
		s, err := c.GetText(ctx, srv.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}

	// a page from last year's library is all there is to go by at first
	site.serve("/a", 2026, 2027)
	if got := get(ctx, "/a"); got != "/a ed=2026" {
		t.Fatalf("first read: %q", got)
	}
	// another page says there is this year's: the first is asked for again,
	// past the cache, and the newer one kept
	site.serve("/b", 2027)
	if got := get(ctx, "/b"); got != "/b ed=2027" {
		t.Fatalf("b: %q", got)
	}
	if got := get(ctx, "/a"); got != "/a ed=2027" || site.busts == 0 {
		t.Fatalf("a again: %q, %d busts", got, site.busts)
	}
	gets := site.gets["/a"]
	if got := get(ctx, "/a"); got != "/a ed=2027" || site.gets["/a"] != gets {
		t.Fatalf("a once more: %q, %d gets after %d", got, site.gets["/a"], gets)
	}

	// a server with last year's library, read past the cache, never replaces
	// this year's page kept
	site.serve("/a", 2026)
	if got := get(WithRefresh(ctx), "/a"); got != "/a ed=2027" {
		t.Fatalf("refresh from an old server: %q", got)
	}
	// a page newest everywhere asked for once: no retry
	site.serve("/c", 2027)
	site.busts = 0
	get(ctx, "/c")
	if site.busts != 0 {
		t.Fatalf("%d busts for a current page", site.busts)
	}
	// the newest edition outlives the client
	c2 := New(WithResponseCache(c.responses), WithEditions(fakeEditions{}))
	if n := c2.newestEdition("lib"); n != 2027 {
		t.Fatalf("kept newest: %d", n)
	}
}

// A memo built from a page of last year's library is built again once a
// newer edition is seen.
func TestEditionsRebuildMemos(t *testing.T) {
	ctx := context.Background()
	c, site, srv := editionClient(t)
	site.serve("/a", 2026, 2027)
	builds := 0
	memo := func() string {
		t.Helper()
		v, err := Memo(ctx, c.responses, "m", func(ctx context.Context) (string, error) {
			builds++
			return c.GetText(ctx, srv.URL+"/a", nil)
		})
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	if got := memo(); got != "/a ed=2026" || builds != 1 {
		t.Fatalf("first: %q, %d builds", got, builds)
	}
	site.serve("/b", 2027)
	if _, err := c.GetText(ctx, srv.URL+"/b", nil); err != nil {
		t.Fatal(err)
	}
	if got := memo(); got != "/a ed=2027" || builds != 2 {
		t.Fatalf("after a newer edition: %q, %d builds", got, builds)
	}
	if got := memo(); got != "/a ed=2027" || builds != 2 {
		t.Fatalf("then: %q, %d builds", got, builds)
	}
}

// A library seen only at last year's edition while another is at this
// year's is looked into, once, for this year's.
func TestEditionsProbeOtherLibraries(t *testing.T) {
	ctx := context.Background()
	c, site, srv := editionClient(t)
	site.serve("/en", 2027)
	if _, err := c.GetText(ctx, srv.URL+"/en?lib=en", nil); err != nil {
		t.Fatal(err)
	}
	// German: kept from last year's library before this year's was known
	site.serve("/de", 2026, 2026, 2027)
	got, err := c.GetText(ctx, srv.URL+"/de?lib=de", nil)
	if err != nil || got != "/de lib=de ed=2027" {
		t.Fatalf("de: %q %v", got, err)
	}
	// a library at last year's on every server is looked into on a few
	// pages a day, not on every read
	for i := range probePages {
		path := fmt.Sprintf("/fr%d", i)
		site.serve(path, 2026)
		if _, err := c.GetText(ctx, srv.URL+path+"?lib=fr", nil); err != nil {
			t.Fatal(err)
		}
	}
	busts := site.busts
	site.serve("/fr9", 2026)
	if _, err := c.GetText(ctx, srv.URL+"/fr9?lib=fr", nil); err != nil {
		t.Fatal(err)
	}
	if site.busts != busts {
		t.Fatalf("looked into again: %d busts after %d", site.busts, busts)
	}
}

// A page asked past the caches is followed past them through a redirect.
func TestEditionsBustThroughRedirects(t *testing.T) {
	c, site, srv := editionClient(t)
	site.serve("/a", 2027)
	if _, _, err := c.downloadPast(context.Background(), srv.URL+"/r", nil, 1); err != nil {
		t.Fatal(err)
	}
	if site.busts != 1 || site.gets["/a"] != 1 {
		t.Fatalf("%d busts, %d gets", site.busts, site.gets["/a"])
	}
}

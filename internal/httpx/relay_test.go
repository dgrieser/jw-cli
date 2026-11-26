package httpx

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// fakeRelay answers every request with its page, or fails; it is there
// unless told it is not.
type fakeRelay struct {
	calls atomic.Int32
	fail  bool
	page  string
	away  atomic.Bool
	epoch atomic.Uint64
}

func (f *fakeRelay) Allows(string, string) bool { return true }

func (f *fakeRelay) Available() bool { return !f.away.Load() }

func (f *fakeRelay) Epoch() uint64 { return f.epoch.Load() }

func (f *fakeRelay) Fetch(ctx context.Context, method, rawURL string, hdr http.Header) (*http.Response, error) {
	f.calls.Add(1)
	if f.fail || f.away.Load() {
		return nil, ErrNoRelay
	}
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/html"}},
		Body: io.NopCloser(strings.NewReader(f.page))}, nil
}

// The proxies are asked for what upstream refused or answered with a
// challenge, and for nothing else: not a page that is not there, not a page
// as it should be.
func TestRelayOnlyWhenTurnedAway(t *testing.T) {
	for _, c := range []struct {
		name    string
		status  int
		ctype   string
		body    string
		relayed bool
	}{
		{"page", 200, "text/html", "<html><body><main>the page</main></body></html>", false},
		{"not found", 404, "text/html", "<html>gone</html>", false},
		{"json", 200, "application/json", `{"a":1}`, false},
		{"refused", 403, "text/html", "Access Denied", true},
		{"challenge 202", 202, "text/html", "<html></html>", true},
		{"aws challenge 200", 200, "text/html; charset=utf-8", `<html><script src="https://a.token.awswaf.com/x/challenge.js"></script><noscript>verify that you're not a robot</noscript></html>`, true},
		{"akamai denial", 200, "text/html", `<html><head><title>Access Denied</title></head><body>Reference #18.1</body></html>`, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", c.ctype)
				w.WriteHeader(c.status)
				_, _ = w.Write([]byte(c.body))
			}))
			defer srv.Close()
			cl := New(WithRetryDelays())
			relay := &fakeRelay{page: "from the proxy"}
			cl.SetRelay(relay)
			resp, err := cl.send(context.Background(), http.MethodGet, srv.URL+"/x", http.Header{})
			if err != nil {
				t.Fatal(err)
			}
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if got := relay.calls.Load() > 0; got != c.relayed {
				t.Fatalf("relayed %v, want %v", got, c.relayed)
			}
			want := c.body
			if c.relayed {
				want = "from the proxy"
			}
			if string(body) != want {
				t.Errorf("body %q, want %q (what was peeked at must stay in the body)", body, want)
			}
		})
	}
}

// When no proxy can, the caller gets upstream's own answer, as without
// proxies; the host is not marked to ask the proxies first.
func TestRelayFailsKeepsUpstream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()
	cl := New(WithRetryDelays())
	cl.SetRelay(&fakeRelay{fail: true})
	resp, err := cl.send(context.Background(), http.MethodGet, srv.URL+"/x", http.Header{})
	if err != nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("got %v %v", err, resp)
	}
	resp.Body.Close()
	if len(cl.RelayedHosts()) != 0 {
		t.Error("a host marked though no proxy answered")
	}
}

// A page that upstream answered but that is not the page — its reader
// rejects it — is asked of a proxy before it is given up on.
func TestGetHTMLCheckedAsksProxy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><body>` + strings.Repeat("a new kind of wall ", 2000) + `</body></html>`))
	}))
	defer srv.Close()
	cl := New(WithRetryDelays(), WithResponseCache(OpenCacheAt(t.TempDir())))
	relay := &fakeRelay{page: `<html><body><main class="content">real</main></body></html>`}
	cl.SetRelay(relay)
	check := func(doc *goquery.Document) error {
		if doc.Find("main.content").Length() == 0 {
			return errors.New("not the page")
		}
		return nil
	}
	doc, err := cl.GetHTMLChecked(context.Background(), srv.URL+"/p", check)
	if err != nil || doc.Find("main").Text() != "real" || relay.calls.Load() != 1 {
		t.Fatalf("got %v, %d proxy calls", err, relay.calls.Load())
	}
}

// A page in place of the JSON asked for — wol's lookups, the APIs — is asked
// of a proxy, and not kept.
func TestGetJSONAsksProxy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<!DOCTYPE html><html><body>` + strings.Repeat("an unknown wall ", 2000) + `</body></html>`))
	}))
	defer srv.Close()
	cl := New(WithRetryDelays(), WithResponseCache(OpenCacheAt(t.TempDir())))
	relay := &fakeRelay{page: `{"items":[1,2]}`}
	cl.SetRelay(relay)
	var out struct{ Items []int }
	if err := cl.GetJSON(context.Background(), srv.URL+"/api", nil, &out); err != nil || len(out.Items) != 2 || relay.calls.Load() != 1 {
		t.Fatalf("got %v %+v, %d proxy calls", err, out, relay.calls.Load())
	}
	// JSON that is simply wrong is no reason to ask a proxy
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"items":`)) }))
	defer bad.Close()
	if err := cl.GetJSON(context.Background(), bad.URL+"/api", nil, &out); err == nil || relay.calls.Load() != 1 {
		t.Errorf("broken JSON: %v, %d proxy calls", err, relay.calls.Load())
	}
}

// A small page that is nothing but a demand for JavaScript is a challenge,
// whoever's; a short page with text of its own is not.
func TestJSWall(t *testing.T) {
	for page, want := range map[string]bool{
		`<html><head><script src="/x.js"></script></head><body><noscript>Please enable JavaScript to continue.</noscript></body></html>`:                                        true,
		`<html><body><div id="c"></div><noscript><p>JavaScript is required</p></noscript><script>go()</script></body></html>`:                                                   true,
		`<html><body><noscript>Enable JavaScript for the player.</noscript><h1>Tagestext</h1><p>` + strings.Repeat("Ein Vers und seine Erklärung. ", 12) + `</p></body></html>`: false,
		`<html><body><p>No script here, and short.</p></body></html>`:                                                                                                           false,
	} {
		if got := jsWall([]byte(page)); got != want {
			t.Errorf("jsWall = %v, want %v: %.80s", got, want, page)
		}
	}
}

// failing is a transport that fails every request once told to.
type failing struct {
	fail atomic.Bool
	next http.RoundTripper
}

func (f *failing) RoundTrip(req *http.Request) (*http.Response, error) {
	if f.fail.Load() {
		return nil, errors.New("connection reset")
	}
	return f.next.RoundTrip(req)
}

// An upstream that takes the request and never answers fails on the
// client's own timeout, not the caller's: a proxy is asked.
func TestClientTimeoutAsksProxy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()
	cl := New(WithHTTPClient(&http.Client{Timeout: 50 * time.Millisecond}), WithRetryDelays())
	relay := &fakeRelay{page: "from the proxy"}
	cl.SetRelay(relay)
	resp, err := cl.send(context.Background(), http.MethodGet, srv.URL+"/x", http.Header{})
	if err != nil {
		t.Fatalf("timeout not handed to the proxy: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(body) != "from the proxy" || relay.calls.Load() != 1 {
		t.Errorf("got %q, %d proxy calls", body, relay.calls.Load())
	}
}

// A host the server cannot reach but the proxies serve is not one whose
// kept bodies are used as they are: they are revalidated through a proxy.
func TestRevalidationThroughProxyDuringOutage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("old"))
	}))
	defer srv.Close()
	tr := &failing{next: http.DefaultTransport}
	cl := New(WithHTTPClient(&http.Client{Transport: tr}), WithRetryDelays(),
		WithResponseCache(Open(t.TempDir(), CacheOptions{Fresh: -1})))
	relay := &fakeRelay{page: "new, through the proxy"}
	cl.SetRelay(relay)
	ctx := context.Background()
	if got, err := cl.GetText(ctx, srv.URL+"/kept", nil); err != nil || got != "old" {
		t.Fatalf("first read: %v %q", err, got)
	}
	tr.fail.Store(true)
	// another page fails directly and is fetched through the proxy: the
	// host is down for the client, and served by the proxies
	if _, err := cl.GetText(ctx, srv.URL+"/other", nil); err != nil {
		t.Fatal(err)
	}
	calls := relay.calls.Load()
	got, err := cl.GetText(ctx, srv.URL+"/kept", nil)
	if err != nil || got != "new, through the proxy" || relay.calls.Load() == calls {
		t.Errorf("the kept body was not revalidated through the proxy: %v %q", err, got)
	}
}

// A read that goes to a proxy because the page upstream gave was not the
// page does not come back with what the cache held, and only marks the host
// when a proxy answered.
func TestForcedRelayPastTheCache(t *testing.T) {
	var wall atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		if wall.Load() {
			_, _ = w.Write([]byte(`<html><body>` + strings.Repeat("a wall ", 3000) + `</body></html>`))
			return
		}
		_, _ = w.Write([]byte(`<html><body><main class="content">old</main></body></html>`))
	}))
	defer srv.Close()
	cl := New(WithRetryDelays(), WithResponseCache(OpenCacheAt(t.TempDir())))
	relay := &fakeRelay{page: `<html><body><main class="content">new</main></body></html>`}
	cl.SetRelay(relay)
	check := func(doc *goquery.Document) error {
		if doc.Find("main.content").Length() == 0 {
			return errors.New("not the page")
		}
		return nil
	}
	ctx := context.Background()
	if _, err := cl.GetHTMLChecked(ctx, srv.URL+"/p", check); err != nil {
		t.Fatal(err)
	}
	wall.Store(true)
	doc, err := cl.GetHTMLChecked(WithRefresh(ctx), srv.URL+"/p", check)
	if err != nil || doc.Find("main").Text() != "new" || relay.calls.Load() != 1 {
		t.Fatalf("got %v %q, %d proxy calls", err, doc.Find("main").Text(), relay.calls.Load())
	}

	// no proxy answering: the error, and no host marked
	relay.fail = true
	cl2 := New(WithRetryDelays(), WithResponseCache(OpenCacheAt(t.TempDir())))
	cl2.SetRelay(relay)
	if _, err := cl2.GetHTMLChecked(ctx, srv.URL+"/q", check); err == nil {
		t.Error("a wall read as the page")
	}
	if len(cl2.RelayedHosts()) != 0 {
		t.Error("host marked as served by the proxies, though none answered")
	}
}

// A forced read through a proxy that no proxy answers ends in the error,
// not in the body the cache kept: what upstream gave on the refresh was
// not the page, and the kept one must not pass for a proxy's.
func TestForcedRelayFailsWithCachedPage(t *testing.T) {
	var wall atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		if wall.Load() {
			_, _ = w.Write([]byte(`<html><body>` + strings.Repeat("a wall ", 3000) + `</body></html>`))
			return
		}
		_, _ = w.Write([]byte(`<html><body><main class="content">old</main></body></html>`))
	}))
	defer srv.Close()
	cl := New(WithRetryDelays(), WithResponseCache(OpenCacheAt(t.TempDir())))
	relay := &fakeRelay{fail: true}
	cl.SetRelay(relay)
	check := func(doc *goquery.Document) error {
		if doc.Find("main.content").Length() == 0 {
			return errors.New("not the page")
		}
		return nil
	}
	ctx := context.Background()
	if _, err := cl.GetHTMLChecked(ctx, srv.URL+"/p", check); err != nil {
		t.Fatal(err)
	}
	wall.Store(true)
	if doc, err := cl.GetHTMLChecked(WithRefresh(ctx), srv.URL+"/p", check); err == nil {
		t.Errorf("the kept page passed for a proxy's: %q", doc.Find("main").Text())
	}
	if relay.calls.Load() != 1 {
		t.Errorf("%d proxy calls", relay.calls.Load())
	}
	if len(cl.RelayedHosts()) != 0 {
		t.Error("host marked as served by the proxies, though none answered")
	}
}

// A proxy that comes online while a host is backed off serves its kept
// bodies at once; with no proxy there, the backoff holds even for a host
// the proxies served before.
func TestRelayAvailabilityDecidesRevalidation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("old"))
	}))
	defer srv.Close()
	tr := &failing{next: http.DefaultTransport}
	cl := New(WithHTTPClient(&http.Client{Transport: tr}), WithRetryDelays(),
		WithResponseCache(Open(t.TempDir(), CacheOptions{Fresh: -1})))
	relay := &fakeRelay{page: "new, through the proxy"}
	relay.away.Store(true) // no proxy yet
	cl.SetRelay(relay)
	ctx := context.Background()
	if got, err := cl.GetText(ctx, srv.URL+"/kept", nil); err != nil || got != "old" {
		t.Fatalf("first read: %v %q", err, got)
	}
	tr.fail.Store(true)
	// upstream fails with no proxy there: the kept body, and the host backed off
	if got, err := cl.GetText(ctx, srv.URL+"/kept", nil); err != nil || got != "old" || relay.calls.Load() != 0 {
		t.Fatalf("no proxy: %v %q, %d proxy calls", err, got, relay.calls.Load())
	}
	// a proxy comes online: the kept body is revalidated through it at once
	relay.away.Store(false)
	if got, err := cl.GetText(ctx, srv.URL+"/kept", nil); err != nil || got != "new, through the proxy" {
		t.Fatalf("proxy online: %v %q", err, got)
	}
	if len(cl.RelayedHosts()) == 0 {
		t.Fatal("the host is not asked through the proxies first")
	}
	// the proxy goes away: the host is down, asked through the proxies
	// first, and yet the kept body stands without asking anybody
	relay.away.Store(true)
	calls := relay.calls.Load()
	if got, err := cl.GetText(ctx, srv.URL+"/kept", nil); err != nil || got != "new, through the proxy" {
		t.Fatalf("proxy gone: %v %q", err, got)
	}
	if relay.calls.Load() != calls {
		t.Errorf("a proxy that is not there was asked %d times", relay.calls.Load()-calls)
	}
}

// Proxies that are there but fail are held off a host for the backoff, as
// the host itself is: the kept body is used at once rather than every
// reader waiting on them again — until another proxy comes online.
func TestFailingProxyRespectsBackoff(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("old"))
	}))
	defer srv.Close()
	tr := &failing{next: http.DefaultTransport}
	cl := New(WithHTTPClient(&http.Client{Transport: tr}), WithRetryDelays(),
		WithResponseCache(Open(t.TempDir(), CacheOptions{Fresh: -1})))
	relay := &fakeRelay{fail: true} // there, and failing
	cl.SetRelay(relay)
	ctx := context.Background()
	if got, err := cl.GetText(ctx, srv.URL+"/kept", nil); err != nil || got != "old" {
		t.Fatalf("first read: %v %q", err, got)
	}
	tr.fail.Store(true)
	// upstream and the proxy fail: the kept body, after one try of each
	if got, err := cl.GetText(ctx, srv.URL+"/kept", nil); err != nil || got != "old" {
		t.Fatalf("both failing: %v %q", err, got)
	}
	tried := relay.calls.Load()
	if tried == 0 {
		t.Fatal("the proxy was not tried")
	}
	// the next readers get the kept body without asking either again
	for range 3 {
		if got, err := cl.GetText(ctx, srv.URL+"/kept", nil); err != nil || got != "old" {
			t.Fatalf("held off: %v %q", err, got)
		}
	}
	if relay.calls.Load() != tried {
		t.Errorf("a failing proxy was asked %d more times within the backoff", relay.calls.Load()-tried)
	}
	// another proxy comes online, and is tried at once
	relay.fail = false
	relay.page = "new, through the new proxy"
	relay.epoch.Add(1)
	if got, err := cl.GetText(ctx, srv.URL+"/kept", nil); err != nil || got != "new, through the new proxy" {
		t.Errorf("new proxy: %v %q", err, got)
	}
}

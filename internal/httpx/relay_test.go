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

	"github.com/PuerkitoBio/goquery"
)

// fakeRelay answers every request with its page, or fails.
type fakeRelay struct {
	calls atomic.Int32
	fail  bool
	page  string
}

func (f *fakeRelay) Allows(string, string) bool { return true }

func (f *fakeRelay) Fetch(ctx context.Context, method, rawURL string, hdr http.Header) (*http.Response, error) {
	f.calls.Add(1)
	if f.fail {
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

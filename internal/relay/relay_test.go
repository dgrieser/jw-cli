package relay

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dgrieser/jw-cli/internal/httpx"
)

// toServer sends every request, whatever its address, to srv: jw.org's
// sites in the tests, without leaving the machine.
type toServer struct{ srv *httptest.Server }

func (t toServer) RoundTrip(req *http.Request) (*http.Response, error) {
	u, _ := url.Parse(t.srv.URL)
	r := req.Clone(req.Context())
	r.URL.Scheme, r.URL.Host, r.Host = u.Scheme, u.Host, ""
	return http.DefaultTransport.RoundTrip(r)
}

// mappedClient is an httpx client whose requests to jw.org's sites reach srv.
func mappedClient(srv *httptest.Server) *httpx.Client {
	return httpx.New(httpx.WithHTTPClient(&http.Client{Transport: toServer{srv}}), httpx.WithRetryDelays(time.Millisecond))
}

// testPool is a pool behind a test server.
func testPool(t *testing.T) (*Pool, *httptest.Server) {
	t.Helper()
	p, err := OpenPool(filepath.Join(t.TempDir(), "proxies.json"), "v-test", t.Logf)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(p.Handler())
	t.Cleanup(srv.Close)
	return p, srv
}

// enrolled registers a proxy with the pool and returns its file.
func enrolled(t *testing.T, p *Pool, srv *httptest.Server, name string) Config {
	t.Helper()
	token, _, err := p.Enroll(name, "admin@example.org", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := Register(context.Background(), srv.URL, token, "v-proxy")
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestAllowed(t *testing.T) {
	for raw, want := range map[string]bool{
		"https://www.jw.org/de/":                   true,
		"https://wol.jw.org/de/wol/h/r10/lp-x":     true,
		"https://b.jw-cdn.org/apis/mediator/v1/x":  true,
		"https://jw.org/":                          true,
		"http://www.jw.org/":                       false, // https only
		"https://evil.example/www.jw.org":          false,
		"https://www.jw.org.evil.example/":         false,
		"https://notjw.org/":                       false,
		"https://user:pw@www.jw.org/":              false,
		"https://www.jw.org:8443/":                 false,
		"https://169.254.169.254/latest/meta-data": false,
	} {
		if got := Allowed(http.MethodGet, raw); got != want {
			t.Errorf("Allowed(GET %s) = %v", raw, got)
		}
	}
	if Allowed(http.MethodPost, "https://www.jw.org/") {
		t.Error("a POST may go through a proxy")
	}
}

func TestCheckServerURL(t *testing.T) {
	for raw, ok := range map[string]bool{
		"https://jw.example.org":     true,
		"https://jw.example.org/":    true,
		"http://127.0.0.1:8080":      true,
		"http://localhost:8080":      true,
		"http://jw.example.org":      false,
		"ftp://jw.example.org":       false,
		"https://u:p@jw.example.org": false,
		"jw.example.org":             false,
	} {
		if _, err := CheckServerURL(raw); (err == nil) != ok {
			t.Errorf("CheckServerURL(%s): %v", raw, err)
		}
	}
}

// An enrollment registers one proxy, once; the credential it gets opens
// the protocol and is kept on the server as a hash alone.
func TestRegister(t *testing.T) {
	p, srv := testPool(t)
	token, e, err := p.Enroll("  home   pi ", "admin@example.org", time.Now())
	if err != nil || e.Name != "home pi" {
		t.Fatalf("enroll: %v %+v", err, e)
	}
	if _, _, err := p.Enroll("", "a", time.Now()); !errors.Is(err, ErrBadName) {
		t.Errorf("empty name: %v", err)
	}
	cfg, err := Register(context.Background(), srv.URL, token, "v1")
	if err != nil || cfg.Name != "home pi" || cfg.ID == "" {
		t.Fatalf("register: %v %+v", err, cfg)
	}
	if _, err := Register(context.Background(), srv.URL, token, "v1"); err == nil {
		t.Error("an enrollment registered twice")
	}
	if len(p.Enrollments(time.Now())) != 0 {
		t.Error("the enrollment is left after registering")
	}
	b, _ := os.ReadFile(p.path)
	_, s, _ := splitCredential(cfg.Credential)
	if strings.Contains(string(b), s) || strings.Contains(string(b), token) {
		t.Error("the pool's file holds a secret")
	}
	if st, _ := os.Stat(p.path); st.Mode().Perm() != 0o600 {
		t.Errorf("the pool's file is %v", st.Mode().Perm())
	}
	self, err := NewAgent(cfg, "v1", nil).Self(context.Background())
	if err != nil || !self.Enabled || self.Name != "home pi" {
		t.Errorf("self: %v %+v", err, self)
	}

	// expired
	token, _, _ = p.Enroll("late", "a", time.Now().Add(-2*EnrollTTL))
	if _, err := Register(context.Background(), srv.URL, token, "v1"); err == nil {
		t.Error("an expired enrollment registered")
	}

	// reopened, the pool knows its proxies
	again, err := OpenPool(p.path, "", nil)
	if err != nil || len(again.Proxies(time.Now())) != 1 {
		t.Fatalf("reopened: %v", err)
	}
}

// Wrong credentials, and too many failures from one address, are refused.
func TestCredentialsRefused(t *testing.T) {
	p, srv := testPool(t)
	cfg := enrolled(t, p, srv, "pi")
	for _, cred := range []string{"", "Bearer x", "jwp_nope", cfg.Credential[:len(cfg.Credential)-1] + "x"} {
		bad := cfg
		bad.Credential = cred
		if _, err := NewAgent(bad, "v", nil).Self(context.Background()); !errors.Is(err, ErrRevoked) {
			t.Errorf("credential %q: %v", cred, err)
		}
	}
	// an API key's scheme opens nothing
	req, _ := http.NewRequest(http.MethodGet, srv.URL+PathPrefix+"self", nil)
	req.Header.Set("Authorization", "Bearer "+cfg.Credential)
	if resp, err := http.DefaultClient.Do(req); err != nil || resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("credential as a Bearer key: %v %v", err, resp.StatusCode)
	}
	// until the address has failed too often: then even the right one waits
	for range registerFailures {
		_, _ = Register(context.Background(), srv.URL, "jwpe_wrong", "v")
	}
	if _, err := NewAgent(cfg, "v", nil).Self(context.Background()); err == nil || !strings.Contains(err.Error(), "429") {
		t.Errorf("after many failures: %v", err)
	}
}

// running is a proxy of the pool, fetching from upstream, until the test
// ends.
func running(t *testing.T, cfg Config, upstream *httptest.Server) *Agent {
	t.Helper()
	a := NewAgent(cfg, "v-proxy", t.Logf)
	a.withFetchClient(mappedClient(upstream))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	return a
}

// waitOnline waits until the pool has a proxy to hand work to.
func waitOnline(t *testing.T, p *Pool) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if p.available(time.Now()) {
			p.mu.Lock()
			polling := 0
			for _, px := range p.proxies {
				polling += px.polls
			}
			p.mu.Unlock()
			if polling > 0 {
				return
			}
		}
	}
	t.Fatal("no proxy came online")
}

// A request the pool is asked for goes to a proxy, which fetches it and
// sends the answer back: status, the headers worth keeping, the body.
func TestFetchThroughProxy(t *testing.T) {
	var asked atomic.Value
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked.Store(r.URL.Path + "|" + r.Header.Get("Accept-Language") + "|" + r.Header.Get("Cookie"))
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("ETag", `"v1"`)
		w.Header().Set("Set-Cookie", "upstream=secret")
		_, _ = w.Write([]byte("<html><body>the real page</body></html>"))
	}))
	defer upstream.Close()
	p, srv := testPool(t)

	// none online: at once, no proxy
	start := time.Now()
	if _, err := p.Fetch(context.Background(), http.MethodGet, "https://www.jw.org/de/", nil); !errors.Is(err, httpx.ErrNoRelay) || time.Since(start) > time.Second {
		t.Fatalf("no proxy: %v after %s", err, time.Since(start))
	}

	cfg := enrolled(t, p, srv, "pi")
	running(t, cfg, upstream)
	waitOnline(t, p)
	hdr := http.Header{"Accept-Language": {"de"}, "Cookie": {"session=server-secret"}}
	resp, err := p.Fetch(context.Background(), http.MethodGet, "https://www.jw.org/de/seite/", hdr)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || string(body) != "<html><body>the real page</body></html>" ||
		resp.Header.Get("ETag") != `"v1"` || resp.Header.Get(ProxyHeader) != "pi" || resp.Header.Get("Set-Cookie") != "" {
		t.Errorf("answer: %d %q %v", resp.StatusCode, body, resp.Header)
	}
	// the proxy sends its own cookies (the consent given), never the server's
	if got, _ := asked.Load().(string); !strings.HasPrefix(got, "/de/seite/|de|") || strings.Contains(got, "server-secret") {
		t.Errorf("upstream was asked %q", got)
	}
	if st := p.Proxies(time.Now())[0]; st.OK != 1 || st.State() != "online" || st.Version != "v-proxy" {
		t.Errorf("status: %+v", st)
	}

	// what is no request for a proxy is not handed out
	if _, err := p.Fetch(context.Background(), http.MethodGet, "https://example.org/", nil); !errors.Is(err, httpx.ErrNoRelay) {
		t.Errorf("example.org: %v", err)
	}
}

// A proxy refuses what is not jw.org's even when its server asks, and a
// redirect off jw.org's sites.
func TestAgentRefuses(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://evil.example/", http.StatusFound)
	}))
	defer upstream.Close()
	a := NewAgent(Config{}, "v", nil)
	a.fetch = httpx.New(httpx.WithHTTPClient(&http.Client{
		Transport:     toServer{upstream},
		CheckRedirect: stayOnJW,
	}))
	if res, _ := a.fetchJob(context.Background(), Job{Method: "GET", URL: "https://169.254.169.254/"}); !strings.Contains(res.Error, "refused") {
		t.Errorf("metadata address: %+v", res)
	}
	if res, _ := a.fetchJob(context.Background(), Job{Method: "POST", URL: "https://www.jw.org/"}); !strings.Contains(res.Error, "refused") {
		t.Errorf("POST: %+v", res)
	}
	if res, _ := a.fetchJob(context.Background(), Job{Method: "GET", URL: "https://www.jw.org/"}); !strings.Contains(res.Error, "redirected off") {
		t.Errorf("redirect: %+v", res)
	}
}

// A proxy disabled on the server gets no work and waits to be enabled; one
// deleted there stops with ErrRevoked.
func TestDisableAndDelete(t *testing.T) {
	old := disabledWait
	disabledWait = 20 * time.Millisecond
	defer func() { disabledWait = old }()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("ok")) }))
	defer upstream.Close()
	p, srv := testPool(t)
	cfg := enrolled(t, p, srv, "pi")

	if err := p.SetEnabled(cfg.ID, false); err != nil {
		t.Fatal(err)
	}
	a := NewAgent(cfg, "v", t.Logf)
	a.withFetchClient(mappedClient(upstream))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()
	time.Sleep(100 * time.Millisecond)
	if p.available(time.Now()) {
		t.Error("a disabled proxy is available")
	}
	if _, err := p.Fetch(context.Background(), http.MethodGet, "https://www.jw.org/", nil); !errors.Is(err, httpx.ErrNoRelay) {
		t.Errorf("disabled: %v", err)
	}
	if st := p.Proxies(time.Now())[0]; st.State() != "disabled" {
		t.Errorf("state: %s", st.State())
	}

	if err := p.SetEnabled(cfg.ID, true); err != nil {
		t.Fatal(err)
	}
	waitOnline(t, p)
	if _, err := p.Fetch(context.Background(), http.MethodGet, "https://www.jw.org/", nil); err != nil {
		t.Errorf("enabled again: %v", err)
	}

	if err := p.Delete(cfg.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, ErrRevoked) {
			t.Errorf("deleted: %v", err)
		}
	case <-time.After(PollWait + 5*time.Second):
		t.Error("a deleted proxy goes on")
	}
	cancel()
}

// The server's client asks jw.org itself, and a proxy only for what jw.org
// turned it away from: the challenge page is not what the reader gets.
func TestClientFallsBackToProxy(t *testing.T) {
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><body><main><h1>Neu auf jw.org</h1></main></body></html>`))
	}))
	defer good.Close()
	var direct atomic.Int32
	blocked := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		direct.Add(1)
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><head><script src="https://x.token.awswaf.com/x/challenge.js"></script></head><body></body></html>`))
	}))
	defer blocked.Close()

	p, srv := testPool(t)
	running(t, enrolled(t, p, srv, "pi"), good)
	waitOnline(t, p)

	server := mappedClient(blocked)
	server.SetRelay(p)
	doc, err := server.GetHTML(context.Background(), "https://www.jw.org/de/neu/")
	if err != nil {
		t.Fatal(err)
	}
	if h := doc.Find("h1").Text(); h != "Neu auf jw.org" {
		t.Fatalf("got %q", h)
	}
	if direct.Load() != 1 {
		t.Errorf("asked directly %d times", direct.Load())
	}
	if hosts := server.RelayedHosts(); len(hosts) != 1 || hosts[0].Host != "www.jw.org" {
		t.Errorf("relayed hosts: %+v", hosts)
	}
	// the host turned the server away: asked through the proxy first now
	if _, err := server.GetHTML(context.Background(), "https://www.jw.org/de/andere/"); err != nil || direct.Load() != 1 {
		t.Errorf("second page: %v, asked directly %d times", err, direct.Load())
	}
}

// Every site of jw.org's goes through the proxies the same way: the
// library's pages and its JSON, the CDN's APIs — refused, unreachable, or
// answered with a page in place of JSON.
func TestEverySiteFallsBack(t *testing.T) {
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/apis/") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true}`))
			return
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><body><article><h1>` + r.URL.Path + `</h1></article></body></html>`))
	}))
	defer good.Close()
	refusing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/apis/") {
			// a bot page in place of the JSON
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(`<!DOCTYPE html><html><body>` + strings.Repeat("x", 20000) + `</body></html>`))
			return
		}
		w.WriteHeader(http.StatusForbidden)
	}))
	defer refusing.Close()

	p, srv := testPool(t)
	running(t, enrolled(t, p, srv, "pi"), good)
	waitOnline(t, p)
	server := mappedClient(refusing)
	server.SetRelay(p)
	ctx := context.Background()
	doc, err := server.GetHTML(ctx, "https://wol.jw.org/de/wol/h/r10/lp-x")
	if err != nil || doc.Find("h1").Text() != "/de/wol/h/r10/lp-x" {
		t.Errorf("wol page: %v", err)
	}
	var out struct{ OK bool }
	if err := server.GetJSON(ctx, "https://b.jw-cdn.org/apis/mediator/v1/languages/E/all", nil, &out); err != nil || !out.OK {
		t.Errorf("CDN API: %v %+v", err, out)
	}
	hosts := map[string]bool{}
	for _, h := range server.RelayedHosts() {
		hosts[h.Host] = true
	}
	if !hosts["wol.jw.org"] || !hosts["b.jw-cdn.org"] {
		t.Errorf("relayed hosts: %v", hosts)
	}
}

package server_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"net/http/httptest"

	"github.com/dgrieser/jw-cli/internal/httpx"
	"github.com/dgrieser/jw-cli/internal/server"
	"github.com/dgrieser/jw-cli/internal/service"
)

// A picture jw.org refuses the browser is read through the server: what wol
// answers comes back as it was, and nothing but a picture of jw.org does.
func TestImageProxy(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/en/wol/mp/r1/lp-e/w26/2026/524", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Sec-Fetch-Dest") != "image" {
			t.Errorf("Sec-Fetch-Dest = %q, want image", r.Header.Get("Sec-Fetch-Dest"))
		}
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write([]byte("JPEG"))
	})
	mux.HandleFunc("/challenge", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<html>are you human?</html>"))
	})
	// the test upstream stands in for every jw.org site
	up := httptest.NewServer(mux)
	t.Cleanup(up.Close)
	upstream := up.URL
	hc := httpx.New(httpx.WithBaseURLs(httpx.BaseURLs{CDN: up.URL, JWOrg: up.URL, WOL: up.URL}),
		httpx.WithRetryDelays(time.Millisecond, time.Millisecond))
	srv := httptest.NewServer(server.New(server.Config{Svc: service.New(hc, httpx.OpenCacheAt(t.TempDir()))}).Handler())
	t.Cleanup(srv.Close)

	resp, body := get(t, srv, "/image?u="+url.QueryEscape(upstream+"/en/wol/mp/r1/lp-e/w26/2026/524"))
	if resp.StatusCode != 200 || body != "JPEG" || resp.Header.Get("Content-Type") != "image/jpeg" {
		t.Fatalf("picture: %d %q %q", resp.StatusCode, resp.Header.Get("Content-Type"), body)
	}
	if cc := resp.Header.Get("Cache-Control"); !strings.Contains(cc, "max-age") {
		t.Errorf("Cache-Control = %q, want the picture kept", cc)
	}

	resp, _ = get(t, srv, "/image?u="+url.QueryEscape(upstream+"/challenge"))
	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("a page instead of a picture: %d, want 502", resp.StatusCode)
	}
	for _, foreign := range []string{"https://example.com/a.jpg", "https://jw.org.example.com/a.jpg", "file:///etc/passwd", "http://wol.jw.org/x", ""} {
		resp, _ = get(t, srv, "/image?u="+url.QueryEscape(foreign))
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%q: %d, want 400", foreign, resp.StatusCode)
		}
	}
}

package server_test

import (
	"net/http"
	"strings"
	"testing"
)

// The page's own files carry a strong ETag, so a browser without the service
// worker asks again and gets a 304 rather than the file.
func TestStaticETag(t *testing.T) {
	srv := newTestServer(t, languagesMux(t))
	resp, body := get(t, srv, "/static/app.js")
	tag := resp.Header.Get("ETag")
	if resp.StatusCode != 200 || len(body) == 0 || !strings.HasPrefix(tag, `"`) || strings.HasPrefix(tag, "W/") {
		t.Fatalf("status %d, ETag %q", resp.StatusCode, tag)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("Cache-Control %q", cc)
	}
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/static/app.js", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("If-None-Match", tag)
	again, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	again.Body.Close()
	if again.StatusCode != http.StatusNotModified {
		t.Errorf("conditional request: status %d", again.StatusCode)
	}
	if resp, _ := get(t, srv, "/static/style.css"); resp.Header.Get("ETag") == tag {
		t.Errorf("two files share an ETag")
	}
}

// What a page or the API answers depends on the language cookie: no shared
// cache may keep it, and the browser keeps it per cookie. What keeps nothing
// says so.
func TestPrivateAnswers(t *testing.T) {
	srv := newTestServer(t, languagesMux(t))
	for _, path := range []string{"/?lang=en", "/api/v1/languages"} {
		resp, _ := get(t, srv, path)
		if cc, vary := resp.Header.Get("Cache-Control"), resp.Header.Values("Vary"); cc != "private" || !strings.Contains(strings.Join(vary, ","), "Cookie") {
			t.Errorf("%s: Cache-Control %q, Vary %q", path, cc, vary)
		}
	}
	if resp, _ := get(t, srv, "/?lang=en&refresh=1"); resp.Header.Get("Cache-Control") != "no-store" {
		t.Errorf("a refresh: Cache-Control %q", resp.Header.Get("Cache-Control"))
	}
	if resp, _ := get(t, srv, "/sw.js"); resp.Header.Get("Cache-Control") != "no-cache" {
		t.Errorf("the worker: Cache-Control %q", resp.Header.Get("Cache-Control"))
	}
}

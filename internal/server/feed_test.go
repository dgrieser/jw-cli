package server_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dgrieser/jw-cli/internal/httpx"
	"github.com/dgrieser/jw-cli/internal/server"
	"github.com/dgrieser/jw-cli/internal/service"
)

// feedServer is a server that keeps posted feeds, before a jw.org that
// turns it away and counts how often it is asked.
func feedServer(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	mux := languagesMux(t)
	mux.HandleFunc("/finder", func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<html><body><noscript>verify that you're not a robot</noscript></body></html>`))
	})
	up := httptest.NewServer(mux)
	t.Cleanup(up.Close)
	hc := httpx.New(httpx.WithBaseURLs(httpx.BaseURLs{CDN: up.URL, JWOrg: up.URL, WOL: up.URL}),
		httpx.WithRetryDelays(time.Millisecond, time.Millisecond))
	svc := service.New(hc, httpx.OpenCacheAt(t.TempDir()))
	feeds, err := service.OpenFeeds(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	svc.Feeds = feeds
	srv := httptest.NewServer(server.New(server.Config{Svc: svc}).Handler())
	t.Cleanup(srv.Close)
	return srv, &hits
}

func postFeed(t *testing.T, srv *httptest.Server, path, contentType, body string) (*http.Response, string) {
	t.Helper()
	resp, err := http.Post(srv.URL+path, contentType, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, string(b)
}

// The feed of jw.org's page of what is new, posted by a machine jw.org lets
// through, is what is new for a day: the page shows it, with no notice, the
// API lists it, and neither they nor a reload ask jw.org.
func TestPostedFeedStandsIn(t *testing.T) {
	srv, hits := feedServer(t)
	feed, err := os.ReadFile("../api/jworg/testdata/whatsnew_de.xml")
	if err != nil {
		t.Fatal(err)
	}

	resp, body := postFeed(t, srv, "/api/v1/latest/feed", "application/rss+xml", string(feed))
	if resp.StatusCode != 200 {
		t.Fatalf("post: %d %s", resp.StatusCode, body)
	}
	var posted struct {
		Lang  string    `json:"lang"`
		Count int       `json:"count"`
		Holds bool      `json:"holds"`
		Until time.Time `json:"until"`
	}
	_ = json.Unmarshal([]byte(body), &posted)
	if posted.Lang != "X" || posted.Count != 20 || !posted.Holds || time.Until(posted.Until) < 23*time.Hour {
		t.Errorf("post answered %s", body)
	}

	for _, path := range []string{"/latest?lang=de", "/latest?lang=de&refresh=1", "/latest?lang=de&kind=news&refresh=1", "/pub?lang=de&refresh=1"} {
		_, page := get(t, srv, path)
		if strings.HasPrefix(path, "/latest") && !strings.Contains(page, "Ein Jahrhundert der „Verteidigung") {
			t.Errorf("%s: the feed's news missing", path)
		}
		if strings.Contains(page, "konnte nicht gelesen werden") {
			t.Errorf("%s: says jw.org's list could not be read", path)
		}
	}
	_, api := get(t, srv, "/api/v1/latest?lang=de&kind=news")
	if !strings.Contains(api, "Drei Glaubensbrüder") || strings.Contains(api, `"error"`) {
		t.Errorf("API: %s", api)
	}
	if hits.Load() != 0 {
		t.Errorf("jw.org was asked %d times while the feed holds", hits.Load())
	}

	resp, body = get(t, srv, "/api/v1/latest/feed?lang=de")
	if resp.StatusCode != 200 || !strings.Contains(body, `"holds": true`) || strings.Contains(body, `"items"`) {
		t.Errorf("status: %d %s", resp.StatusCode, body)
	}
	if resp, _ := get(t, srv, "/api/v1/latest/feed?lang=en"); resp.StatusCode != 404 {
		t.Errorf("status in a language without a feed: %d", resp.StatusCode)
	}
}

// What is not a feed of jw.org's page, or is posted as a type a form could
// send, or in another language than asked, is refused and changes nothing.
func TestPostedFeedRefused(t *testing.T) {
	srv, _ := feedServer(t)
	feed, _ := os.ReadFile("../api/jworg/testdata/whatsnew_de.xml")
	for name, c := range map[string]struct {
		path, ct, body string
		status         int
	}{
		"form type":     {"/api/v1/latest/feed", "application/x-www-form-urlencoded", string(feed), 415},
		"plain text":    {"/api/v1/latest/feed", "text/plain", string(feed), 415},
		"bot page":      {"/api/v1/latest/feed", "application/xml", `<html><body>robot</body></html>`, 400},
		"wrong lang":    {"/api/v1/latest/feed?lang=en", "application/rss+xml", string(feed), 400},
		"no feed items": {"/api/v1/latest/feed", "text/xml", `<rss><channel><link>https://www.jw.org/de/neu-auf-unserer-website/rss/WhatsNewWebArticles/feed.xml</link></channel></rss>`, 400},
	} {
		if resp, body := postFeed(t, srv, c.path, c.ct, c.body); resp.StatusCode != c.status {
			t.Errorf("%s: %d, want %d: %s", name, resp.StatusCode, c.status, body)
		}
	}
	if resp, _ := get(t, srv, "/api/v1/latest/feed?lang=de"); resp.StatusCode != 404 {
		t.Errorf("a refused feed was kept: %d", resp.StatusCode)
	}
}

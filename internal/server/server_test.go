package server_test

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dgrieser/jw-cli/internal/httpx"
	"github.com/dgrieser/jw-cli/internal/server"
	"github.com/dgrieser/jw-cli/internal/service"
)

// newTestServer wires the server against a mock upstream, mirroring the CLI
// test harness: every base URL points at the mux, the cache is throwaway.
func newTestServer(t *testing.T, upstream *http.ServeMux) *httptest.Server {
	t.Helper()
	up := httptest.NewServer(upstream)
	t.Cleanup(up.Close)
	hc := httpx.New(httpx.WithBaseURLs(httpx.BaseURLs{CDN: up.URL, JWOrg: up.URL, WOL: up.URL}))
	svc := service.New(hc, httpx.OpenCacheAt(t.TempDir()))
	srv := httptest.NewServer(server.New(server.Config{Svc: svc}).Handler())
	t.Cleanup(srv.Close)
	return srv
}

// get fetches a path off the test server without following redirects.
func get(t *testing.T, srv *httptest.Server, path string) (*http.Response, string) {
	t.Helper()
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.Get(srv.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp, string(body)
}

// fixture serves a testdata file with the given content type.
func fixture(t *testing.T, path, contentType string) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		b, err := os.ReadFile(filepath.Join("testdata", path))
		if err != nil {
			t.Errorf("fixture %s: %v", path, err)
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", contentType)
		w.Write(b)
	}
}

// wolFixture serves a fixture that lives with the wol client's own tests.
func wolFixture(t *testing.T, name string) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		b, err := os.ReadFile(filepath.Join("..", "api", "wol", "testdata", name))
		if err != nil {
			t.Errorf("fixture %s: %v", name, err)
			http.Error(w, "missing", 500)
			return
		}
		w.Write(b)
	}
}

func languagesMux(t *testing.T) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/apis/mediator/v1/languages/E/web", fixture(t, "languages.json", "application/json"))
	return mux
}

// wolMux adds the config discovery and the John 3 chapter the bible endpoints
// read.
func wolMux(t *testing.T) *http.ServeMux {
	mux := languagesMux(t)
	mux.HandleFunc("/en", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<a href="/en/wol/h/r1/lp-e">home</a>`))
	})
	mux.HandleFunc("/en/wol/b/r1/lp-e/nwtsty/43/3", wolFixture(t, "chapter_john3.html"))
	mux.HandleFunc("/en/wol/d/r1/lp-e/2024360", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html><head><title>t</title></head><body>
		<div id="article">
		  <header><h1>Caleb—He Fought Loyally</h1></header>
		  <div class="bodyTxt">
		    <p>CALEB trusted in Jehovah. (<a href="/en/wol/bc/r1/lp-e/2024360/0/0" data-bid="1-1" class="b">Num. 14:24</a>)</p>
		  </div>
		</div></body></html>`))
	})
	return mux
}

func searchMux(t *testing.T) *http.ServeMux {
	mux := languagesMux(t)
	mux.HandleFunc("/tokens/jworg.jwt", func(w http.ResponseWriter, r *http.Request) {
		payload := base64.RawURLEncoding.EncodeToString(fmt.Appendf(nil, `{"exp":%d}`, time.Now().Add(time.Hour).Unix()))
		fmt.Fprint(w, "h."+payload+".s")
	})
	videos := func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			http.Error(w, "no token", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"results": [{"type": "item", "subtype": "video", "title": "Daniel&nbsp;7:27 Schöpfung", "lank": "pub-xyz_1_VIDEO",
			             "snippet": "vom <strong>Königreich</strong>\nbekannt machen?",
			             "duration": "3:10", "links": {"jw.org": "https://www.jw.org/finder?lank=pub-xyz_1_VIDEO"}}],
			"insight": {"total": {"value": 1}}
		}`)
	}
	for _, symbol := range []string{"E", "X"} {
		mux.HandleFunc("/apis/search/results/"+symbol+"/videos", videos)
	}
	return mux
}

func mediaMux(t *testing.T) *http.ServeMux {
	mux := languagesMux(t)
	mux.HandleFunc("/apis/mediator/v1/categories/E", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"categories": [
			{"key": "VideoOnDemand", "name": "Videos", "type": "container", "subcategories": [], "media": []},
			{"key": "Audio", "name": "Audio", "type": "container", "subcategories": [], "media": []}
		]}`)
	})
	mux.HandleFunc("/apis/mediator/v1/categories/E/LatestVideos", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"category": {
			"key": "LatestVideos", "name": "Latest Videos", "type": "ondemand",
			"subcategories": [],
			"media": [{
				"languageAgnosticNaturalKey": "pub-abc_1_VIDEO", "type": "video",
				"title": "A New Video", "durationFormattedMinSec": "5:00",
				"images": {"lss": {"lg": "https://cdn.example/a.jpg"}},
				"files": []
			}]
		}}`)
	})
	mux.HandleFunc("/apis/mediator/v1/media-items/E/pub-abc_1_VIDEO", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"media": [{
			"languageAgnosticNaturalKey": "pub-abc_1_VIDEO", "type": "video",
			"title": "A New Video", "description": "About something.",
			"durationFormattedMinSec": "5:00", "availableLanguages": ["E","X"],
			"files": [
				{"progressiveDownloadURL": "https://cdn.example/v_r240P.mp4", "label": "240p", "frameHeight": 240, "mimetype": "video/mp4", "filesize": 5},
				{"progressiveDownloadURL": "https://cdn.example/v_r720P.mp4", "label": "720p", "frameHeight": 720, "mimetype": "video/mp4", "filesize": 9,
				 "subtitles": {"url": "https://cdn.example/v.vtt"}}
			]
		}]}`)
	})
	return mux
}

func pubMux(t *testing.T) *http.ServeMux {
	mux := languagesMux(t)
	mux.HandleFunc("/apis/pub-media/GETPUBMEDIALINKS", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"pubName": "Der Wachtturm Mai 2024",
			"pub": "w", "issue": "202405",
			"languages": {"X": {"name": "German", "locale": "de"}},
			"files": {"X": {"PDF": [{
				"title": "Der Wachtturm, Mai 2024",
				"file": {"url": "https://cdn.example/w_X_202405.pdf", "checksum": ""},
				"filesize": 11, "label": "", "track": 0, "docid": 2024365,
				"mimetype": "application/pdf"
			}]}}
		}`)
	})
	return mux
}

func TestAPILanguages(t *testing.T) {
	srv := newTestServer(t, languagesMux(t))
	resp, body := get(t, srv, "/api/v1/languages")
	if resp.StatusCode != 200 {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("content type %q", ct)
	}
	for _, want := range []string{`"symbol": "X"`, `"locale": "de"`} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in %s", want, body)
		}
	}
	_, filtered := get(t, srv, "/api/v1/languages?q=german")
	if !strings.Contains(filtered, "German") || strings.Contains(filtered, "French") {
		t.Errorf("filter failed: %s", filtered)
	}
}

func TestAPISearch(t *testing.T) {
	srv := newTestServer(t, searchMux(t))
	resp, body := get(t, srv, "/api/v1/search?q=Sch%C3%B6pfung&type=videos&lang=de")
	if resp.StatusCode != 200 {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
	var out service.SearchOutcome
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatal(err)
	}
	if out.Kind != "search" || out.Total != 1 || len(out.Items) != 1 || out.Items[0].LANK != "pub-xyz_1_VIDEO" {
		t.Errorf("unexpected outcome: %s", body)
	}
}

func TestAPISearchValidation(t *testing.T) {
	srv := newTestServer(t, searchMux(t))
	resp, body := get(t, srv, "/api/v1/search")
	if resp.StatusCode != 400 || !strings.Contains(body, `"code": "bad_request"`) {
		t.Errorf("missing q: status %d body %s", resp.StatusCode, body)
	}
	resp, body = get(t, srv, "/api/v1/search?q=x&limit=nope")
	if resp.StatusCode != 400 || !strings.Contains(body, "invalid limit") {
		t.Errorf("bad limit: status %d body %s", resp.StatusCode, body)
	}
}

func TestAPIArticle(t *testing.T) {
	srv := newTestServer(t, wolMux(t))
	resp, body := get(t, srv, "/api/v1/article?target=2024360&lang=en")
	if resp.StatusCode != 200 {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
	var art struct {
		Title, Format, Body string
	}
	if err := json.Unmarshal([]byte(body), &art); err != nil {
		t.Fatal(err)
	}
	if art.Title != "Caleb—He Fought Loyally" || art.Format != "html" {
		t.Errorf("title/format: %+v", art)
	}
	if !strings.Contains(art.Body, "CALEB trusted in Jehovah") || !strings.Contains(art.Body, "<p>") {
		t.Errorf("html body: %q", art.Body)
	}

	_, md := get(t, srv, "/api/v1/article?target=2024360&lang=en&format=markdown")
	if !strings.Contains(md, `[Num. 14:24]`) {
		t.Errorf("markdown body: %s", md)
	}

	resp, body = get(t, srv, "/api/v1/article?target=2024360&lang=en&format=nope")
	if resp.StatusCode != 400 || !strings.Contains(body, "invalid format") {
		t.Errorf("bad format: status %d body %s", resp.StatusCode, body)
	}

	resp, body = get(t, srv, "/api/v1/article?lang=en")
	if resp.StatusCode != 400 || !strings.Contains(body, "target") {
		t.Errorf("missing target: status %d body %s", resp.StatusCode, body)
	}
}

func TestAPIArticleUpstream404(t *testing.T) {
	srv := newTestServer(t, wolMux(t))
	resp, body := get(t, srv, "/api/v1/article?target=999&lang=en")
	if resp.StatusCode != 404 || !strings.Contains(body, `"code": "not_found"`) {
		t.Errorf("status %d body %s", resp.StatusCode, body)
	}
}

func TestAPIBibleRead(t *testing.T) {
	srv := newTestServer(t, wolMux(t))
	resp, body := get(t, srv, "/api/v1/bible/read?ref=John+3:16&lang=en")
	if resp.StatusCode != 200 {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
	var out struct {
		Ref      string
		Body     string
		Passages []service.Passage
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Passages) != 1 || out.Passages[0].Ref != "John 3:16" {
		t.Fatalf("passages: %s", body)
	}
	if !strings.Contains(out.Body, "John 3:16") {
		t.Errorf("rendered body: %q", out.Body)
	}
}

func TestAPIBibleBooks(t *testing.T) {
	srv := newTestServer(t, wolMux(t))
	resp, body := get(t, srv, "/api/v1/bible/books?lang=en")
	if resp.StatusCode != 200 || !strings.Contains(body, `"name": "Genesis"`) {
		t.Errorf("status %d body %.300s", resp.StatusCode, body)
	}
}

func TestAPIMedia(t *testing.T) {
	srv := newTestServer(t, mediaMux(t))
	resp, body := get(t, srv, "/api/v1/media/categories?lang=en")
	if resp.StatusCode != 200 || !strings.Contains(body, "VideoOnDemand") {
		t.Errorf("categories: status %d body %s", resp.StatusCode, body)
	}
	resp, body = get(t, srv, "/api/v1/media/categories/LatestVideos?lang=en")
	if resp.StatusCode != 200 || !strings.Contains(body, "A New Video") {
		t.Errorf("category: status %d body %s", resp.StatusCode, body)
	}
	resp, body = get(t, srv, "/api/v1/media/items/pub-abc_1_VIDEO?lang=en")
	if resp.StatusCode != 200 || !strings.Contains(body, "v_r720P.mp4") {
		t.Errorf("item: status %d body %s", resp.StatusCode, body)
	}
}

func TestDownloadMediaRedirect(t *testing.T) {
	srv := newTestServer(t, mediaMux(t))
	resp, _ := get(t, srv, "/download/media/pub-abc_1_VIDEO?lang=en")
	if resp.StatusCode != 302 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "https://cdn.example/v_r720P.mp4" {
		t.Errorf("best quality should pick 720p, got %q", loc)
	}
	resp, _ = get(t, srv, "/download/media/pub-abc_1_VIDEO?lang=en&quality=240p")
	if loc := resp.Header.Get("Location"); loc != "https://cdn.example/v_r240P.mp4" {
		t.Errorf("quality selection: %q", loc)
	}
	resp, _ = get(t, srv, "/download/media/pub-abc_1_VIDEO?lang=en&subtitles=true")
	if loc := resp.Header.Get("Location"); loc != "https://cdn.example/v.vtt" {
		t.Errorf("subtitles: %q", loc)
	}
}

func TestAPIPubAndDownload(t *testing.T) {
	srv := newTestServer(t, pubMux(t))
	resp, body := get(t, srv, "/api/v1/pub?pub=w&issue=202405&lang=de")
	if resp.StatusCode != 200 {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
	if !strings.Contains(body, "Der Wachtturm Mai 2024") || !strings.Contains(body, "w_X_202405.pdf") {
		t.Errorf("pub response: %s", body)
	}

	resp, _ = get(t, srv, "/download/pub?pub=w&issue=202405&lang=de")
	if resp.StatusCode != 302 || resp.Header.Get("Location") != "https://cdn.example/w_X_202405.pdf" {
		t.Errorf("single file should redirect: %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}

	resp, body = get(t, srv, "/api/v1/pub?lang=de")
	if resp.StatusCode != 400 || !strings.Contains(body, "pub") {
		t.Errorf("missing pub: status %d body %s", resp.StatusCode, body)
	}
}

func TestUIIndexAndLanguages(t *testing.T) {
	srv := newTestServer(t, languagesMux(t))
	resp, body := get(t, srv, "/")
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	for _, want := range []string{"<title>jw · jw</title>", "Daily text", "/search"} {
		if !strings.Contains(body, want) {
			t.Errorf("index missing %q", want)
		}
	}

	resp, body = get(t, srv, "/languages?q=german")
	if resp.StatusCode != 200 || !strings.Contains(body, "Deutsch") || strings.Contains(body, "Français") {
		t.Errorf("languages page: status %d\n%s", resp.StatusCode, body)
	}

	resp, _ = get(t, srv, "/static/style.css")
	if resp.StatusCode != 200 {
		t.Errorf("stylesheet: status %d", resp.StatusCode)
	}
}

func TestUISearch(t *testing.T) {
	srv := newTestServer(t, searchMux(t))
	resp, body := get(t, srv, "/search?q=Sch%C3%B6pfung&type=videos&lang=de")
	if resp.StatusCode != 200 {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
	for _, want := range []string{
		"1 Ergebnis für", // the header follows ?lang=de
		"Daniel 7:27 Schöpfung",
		"Königreich",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in:\n%s", want, body)
		}
	}
	// the empty form renders without hitting upstream
	resp, _ = get(t, srv, "/search")
	if resp.StatusCode != 200 {
		t.Errorf("empty search form: status %d", resp.StatusCode)
	}
}

func TestUIArticle(t *testing.T) {
	srv := newTestServer(t, wolMux(t))
	resp, body := get(t, srv, "/article?target=2024360&lang=en")
	if resp.StatusCode != 200 {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
	for _, want := range []string{"Caleb—He Fought Loyally", "CALEB trusted in Jehovah"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}
	// script injection cannot survive the sanitizer
	if strings.Contains(body, "<script") {
		t.Errorf("unexpected script tag in article page")
	}
}

func TestUIBibleRead(t *testing.T) {
	srv := newTestServer(t, wolMux(t))
	resp, body := get(t, srv, "/bible?ref=John+3:16&lang=en")
	if resp.StatusCode != 200 {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
	if !strings.Contains(body, "John 3:16") {
		t.Errorf("missing passage heading:\n%s", body)
	}
	resp, body = get(t, srv, "/bible?ref=John+3:16&lang=en&view=bogus")
	if resp.StatusCode != 400 || !strings.Contains(body, "unknown view") {
		t.Errorf("bad view: status %d", resp.StatusCode)
	}
}

func TestUIError(t *testing.T) {
	srv := newTestServer(t, wolMux(t))
	resp, body := get(t, srv, "/article?target=999&lang=en")
	if resp.StatusCode != 404 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if !strings.Contains(body, "HTTP 404") {
		t.Errorf("error page:\n%s", body)
	}
}

// TestConcurrentRequests feeds the race detector: several goroutines share the
// language memo, the template sets, and the service clients.
func TestConcurrentRequests(t *testing.T) {
	mux := searchMux(t)
	srv := newTestServer(t, mux)
	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() {
			for _, path := range []string{"/api/v1/search?q=x&type=videos&lang=de", "/api/v1/languages"} {
				resp, err := http.Get(srv.URL + path)
				if err != nil {
					t.Error(err)
					return
				}
				_, _ = io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
			}
		})
	}
	wg.Wait()
}

package server_test

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
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
	mux.HandleFunc("/apis/mediator/v1/categories/E/VideoOnDemand", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"category": {"key": "VideoOnDemand", "name": "Videos", "type": "container", "subcategories": []}}`)
	})
	mux.HandleFunc("/apis/mediator/v1/categories/E/BJF", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"category": {"key": "BJF", "name": "Become Jehovah's Friend", "type": "ondemand",
			"parentCategory": {"key": "VODChildren", "name": "Children"}, "media": []}}`)
	})
	mux.HandleFunc("/apis/mediator/v1/categories/E/VODChildren", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"category": {
			"key": "VODChildren", "name": "Children", "type": "container",
			"parentCategory": {"key": "VideoOnDemand", "name": "Videos"},
			"subcategories": [
				{"key": "BJF", "name": "Become Jehovah's Friend", "type": "ondemand", "media": [{
					"languageAgnosticNaturalKey": "pub-pk_1_VIDEO", "type": "video", "title": "Caleb Video",
					"images": {"lss": {"lg": "https://cdn.example/c.jpg"}}, "files": []
				}]},
				{"key": "ChildrenMore", "name": "More For Children", "type": "container", "media": []}
			]
		}}`)
	})
	mux.HandleFunc("/apis/mediator/v1/media-items/E/pub-abc_1_VIDEO", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"media": [{
			"languageAgnosticNaturalKey": "pub-abc_1_VIDEO", "type": "video",
			"title": "A New Video", "description": "About something.", "primaryCategory": "BJF",
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

func TestUIMedia(t *testing.T) {
	srv := newTestServer(t, mediaMux(t))
	// LatestVideos is a carousel, not a link in the category list
	resp, body := get(t, srv, "/media?lang=en")
	if resp.StatusCode != 200 || !strings.Contains(body, `class="car-track"`) || !strings.Contains(body, "A New Video") {
		t.Errorf("start page: status %d body %.2000s", resp.StatusCode, body)
	}
	if strings.Contains(body, `/media/category/LatestVideos`) || strings.Contains(body, ">category<") ||
		strings.Contains(body, "VideoOnDemand<") {
		t.Errorf("start page shows internal names: %.3000s", body)
	}
	resp, body = get(t, srv, "/media/item/pub-abc_1_VIDEO?lang=en")
	if resp.StatusCode != 200 || !strings.Contains(body, `<source src="https://cdn.example/v_r720P.mp4"`) ||
		!strings.Contains(body, `<track kind="subtitles" src="https://cdn.example/v.vtt"`) {
		t.Errorf("item page should play the video: status %d body %.3000s", resp.StatusCode, body)
	}
	if strings.Contains(body, `href="/download/media/`) {
		t.Errorf("item page still links the best rendition")
	}
}

func TestUIMediaCategoryCarousels(t *testing.T) {
	srv := newTestServer(t, mediaMux(t))
	resp, body := get(t, srv, "/media/category/VODChildren?lang=en")
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	// the leaf category is a carousel under a heading linking to it
	if !strings.Contains(body, `class="car-track"`) || !strings.Contains(body, "Caleb Video") ||
		!strings.Contains(body, `<h2><a href="/media/category/BJF?lang=en">`) {
		t.Errorf("leaf category should be a carousel: %.3000s", body)
	}
	// a category holding further categories stays a link in the list
	if !strings.Contains(body, `class="title" href="/media/category/ChildrenMore?lang=en"`) {
		t.Errorf("container category should stay a link: %.3000s", body)
	}
}

func TestUIMediaCategoryCrumbs(t *testing.T) {
	srv := newTestServer(t, mediaMux(t))
	_, body := get(t, srv, "/media/category/BJF?lang=en")
	want := `<a href="/media?lang=en">Media</a><span class="sep" aria-hidden="true">›</span>` +
		`<a href="/media/category/VideoOnDemand?lang=en">Videos</a><span class="sep" aria-hidden="true">›</span>` +
		`<a href="/media/category/VODChildren?lang=en">Children</a>`
	if !strings.Contains(body, want) {
		t.Errorf("breadcrumb trail missing: %.1500s", body)
	}
}

func TestUIMediaItemCrumbs(t *testing.T) {
	srv := newTestServer(t, mediaMux(t))
	_, body := get(t, srv, "/media/item/pub-abc_1_VIDEO?lang=en")
	want := `<a href="/media/category/VODChildren?lang=en">Children</a><span class="sep" aria-hidden="true">›</span>` +
		`<a href="/media/category/BJF?lang=en">Become Jehovah&#39;s Friend</a>`
	if !strings.Contains(body, want) || !strings.Contains(body, `<a href="/media?lang=en">Media</a>`) {
		t.Errorf("item breadcrumb should end in its primary category: %.1500s", body)
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
	resp, body := get(t, srv, "/?lang=en")
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	for _, want := range []string{"<title>JW · JW</title>", "Daily text", "/search", `<html lang="en">`} {
		if !strings.Contains(body, want) {
			t.Errorf("index missing %q", want)
		}
	}

	// the chrome is written in the language the content is read in
	resp, body = get(t, srv, "/?lang=de")
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	for _, want := range []string{"Tagestext", "Zusammenkünfte", `<html lang="de">`} {
		if !strings.Contains(body, want) {
			t.Errorf("German index missing %q", want)
		}
	}
	if strings.Contains(body, ">Daily text<") {
		t.Errorf("English chrome on a German page:\n%s", body)
	}
	// and the choice is remembered, so a link without one comes back in it
	if cookie := resp.Header.Get("Set-Cookie"); !strings.Contains(cookie, "lang=de") {
		t.Errorf("language not remembered: %q", cookie)
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
	// script injection cannot survive the sanitizer: the page's own script is
	// outside the article, nothing from upstream may be inside it
	start, end := strings.Index(body, "<article"), strings.Index(body, "</article>")
	if start < 0 || end < start {
		t.Fatalf("no article element in:\n%s", body)
	}
	if strings.Contains(body[start:end], "<script") {
		t.Errorf("unexpected script tag inside the article")
	}

	// the unfold switcher sits above the document, each level a link back here
	if !strings.Contains(body, `class="tabs unfold"`) ||
		!strings.Contains(body, `/article?lang=en&amp;target=2024360&amp;unfold=2`) {
		t.Errorf("missing unfold switcher in:\n%s", body)
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
	// the text comes first, every verse an item the page can unfold on its own
	if !strings.Contains(body, `class="tabs unfold"`) || !strings.Contains(body, "unfold=1") {
		t.Errorf("missing unfold switcher:\n%s", body)
	}
	if !strings.Contains(body, `data-vid="43003016"`) || strings.Contains(body, `class="expansion"`) {
		t.Errorf("want the verse as an unexpanded item:\n%s", body)
	}
	// the reader is the only view: no tabs, and a link from before, still
	// carrying ?view=, shows the reading
	if strings.Contains(body, "view=notes") {
		t.Errorf("the bible page still offers other views:\n%s", body)
	}
	resp, body = get(t, srv, "/bible?ref=John+3:16&lang=en&view=notes")
	if resp.StatusCode != 200 || !strings.Contains(body, `data-vid="43003016"`) {
		t.Errorf("an old ?view= link: status %d", resp.StatusCode)
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

// libraryMux serves the publication tree: the categories, a category with
// covers, an issue with its articles and the edition above it, a bible, and
// the files of the issue.
func libraryMux(t *testing.T) *http.ServeMux {
	mux := wolMux(t)
	mux.HandleFunc("/en/wol/library/r1/lp-e", wolFixture(t, "library_en.html"))
	mux.HandleFunc("/en/wol/library/r1/lp-e/all-publications/books", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html><body><article id="article"><h1><a class="backNav" href="/en/wol/library/r1/lp-e/all-publications">Books</a></h1>
		<ul class="directory">
		  <li class="row card"><a class="cardContainer" href="/en/wol/publication/r1/lp-e/lff">
		    <img class="cardThumbnailImage" src="/en/wol/publication/r1/lp-e/lff/thumbnail"/>
		    <div class="cardLine1">Enjoy Life Forever! (lff)</div></a></li>
		  <li class="row card"><a class="cardContainer" href="/en/wol/publication/r1/lp-e/wcg">
		    <img class="cardThumbnailImage" src="/en/wol/publication/r1/lp-e/wcg/thumbnail"/>
		    <div class="cardLine1">Courage (wcg)</div></a></li>
		  <li class="row card"><a class="cardContainer" href="/en/wol/publication/r1/lp-e/ad">
		    <span class="cardThumbnailImage"></span><div class="cardLine1">Aid (ad)</div></a></li>
		</ul></article></body></html>`))
	})
	mux.HandleFunc("/en/wol/library/r1/lp-e/all-publications/watchtower/the-watchtower-2024/study-edition/may",
		wolFixture(t, "library_w_202405.html"))
	// the shelves above the issue, which an address is checked against
	shelf := func(path, title, back, child, childTitle string) {
		mux.HandleFunc("/en/wol/library/r1/lp-e/"+path, func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintf(w, `<html><body><article id="article"><h1><a class="backNav" href="/en/wol/library/r1/lp-e/%s">%s</a></h1>
			<ul class="directory"><li class="row card"><a class="cardContainer" href="/en/wol/library/r1/lp-e/%s/%s">
			<span class="cardThumbnailImage icon-w"></span><div class="cardLine1">%s</div></a></li></ul>
			</article></body></html>`, back, title, path, child, childTitle)
		})
	}
	shelf("all-publications/watchtower", "Watchtower", "all-publications", "the-watchtower-2024", "The Watchtower—2024")
	shelf("all-publications/watchtower/the-watchtower-2024", "The Watchtower—2024", "all-publications/watchtower", "study-edition", "Study Edition")
	shelf("all-publications/watchtower/the-watchtower-2024/study-edition", "Study Edition", "all-publications/watchtower/the-watchtower-2024", "may", "May")
	mux.HandleFunc("/en/wol/publication/r1/lp-e/nwt", wolFixture(t, "publication_nwt.html"))
	mux.HandleFunc("/apis/pub-media/GETPUBMEDIALINKS", func(w http.ResponseWriter, r *http.Request) {
		if q := r.URL.Query(); q.Get("pub") != "w" || q.Get("issue") != "202405" || q.Get("langwritten") != "E" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"pubName": "Watchtower (Study)", "pub": "w", "issue": "202405",
			"languages": {"E": {"name": "English", "locale": "en"}},
			"files": {"E": {
				"PDF": [{"title": "Regular", "file": {"url": "https://cdn.example/w_E_202405.pdf"},
				         "filesize": 1667374, "label": "0p", "mimetype": "application/pdf"}],
				"MP3": [
				  {"title": "Article 1", "file": {"url": "https://cdn.example/w_E_202405_01.mp3"}, "filesize": 10, "track": 1},
				  {"title": "Article 2", "file": {"url": "https://cdn.example/w_E_202405_02.mp3"}, "filesize": 10, "track": 2},
				  {"title": "Article 3", "file": {"url": "https://cdn.example/w_E_202405_03.mp3"}, "filesize": 10, "track": 3},
				  {"title": "Article 4", "file": {"url": "https://cdn.example/w_E_202405_04.mp3"}, "filesize": 10, "track": 4}
				]
			}}
		}`)
	})
	return mux
}

// TestUIPubStart: the start page lists the categories, and a row of covers
// for a category that has them.
func TestUIPubStart(t *testing.T) {
	srv := newTestServer(t, libraryMux(t))
	resp, body := get(t, srv, "/pub?lang=en")
	if resp.StatusCode != 200 {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
	for _, want := range []string{
		`href="/pub/library/publications/watchtower?lang=en">Watchtower</a>`,
		`href="/pub/library/publications/meeting-workbooks?lang=en">Meeting Workbooks</a>`,
		// the books row, with the book that has no cover left out of it
		`<h2><a href="/pub/library/publications/books?lang=en">Books</a></h2>`,
		`href="/pub/publication/lff?lang=en"`,
		`/en/wol/publication/r1/lp-e/wcg/thumbnail`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("start page lacks %s", want)
		}
	}
	if strings.Contains(body, "/pub/publication/ad?") {
		t.Errorf("a book without a cover is on the row")
	}
	if strings.Contains(body, `name="pub"`) {
		t.Errorf("the start page still carries the symbol form")
	}

	_, body = get(t, srv, "/pub/library/publications/books?lang=en")
	if !strings.Contains(body, `class="pubgrid"`) || !strings.Contains(body, `<span class="blank">Aid (ad)</span>`) {
		t.Errorf("category page should show the books as a grid of covers:\n%s", body)
	}
}

// TestUIPubIssue: an issue lists its articles, the files of the issue, and the
// trail up to the start page.
func TestUIPubIssue(t *testing.T) {
	srv := newTestServer(t, libraryMux(t))
	resp, body := get(t, srv, "/pub/library/publications/watchtower/the-watchtower-2024/study-edition/may?lang=en")
	if resp.StatusCode != 200 {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
	for _, want := range []string{
		"<h1>May</h1>",
		`<span class="label">STUDY ARTICLE 18</span><span class="title">Trust in the Merciful “Judge of All the Earth”!</span>`,
		`href="/article?lang=en&amp;target=2024404"`,
		"https://cdn.example/w_E_202405.pdf",
		"1.6 MiB",
		"<dd>2024-05</dd>",
		// four tracks fold away
		`<details class="filegroup">`,
		"4 files",
		// the trail: the start page and the edition above the issue
		`<a href="/pub?lang=en">Publications</a>`,
		`<a href="/pub/library/publications/watchtower/the-watchtower-2024/study-edition?lang=en">Study Edition</a>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("issue page lacks %s", want)
		}
	}
	if strings.Contains(body, ">0p<") {
		t.Errorf("empty resolution label shown")
	}
}

func TestUIPubRedirects(t *testing.T) {
	srv := newTestServer(t, libraryMux(t))
	// a bible is read on the bible page
	resp, _ := get(t, srv, "/pub/publication/nwt?lang=en")
	if loc := resp.Header.Get("Location"); resp.StatusCode != http.StatusSeeOther || loc != "/bible?bible=nwt&lang=en" {
		t.Errorf("bible: status %d location %q", resp.StatusCode, loc)
	}
	// a library path of another language leads back to the start
	// wol answers a page it does not carry with the shelf above it; the
	// address leads back to the start all the same
	resp, _ = get(t, srv, "/pub/library/publications/books/fm?lang=en")
	if loc := resp.Header.Get("Location"); resp.StatusCode != http.StatusSeeOther || loc != "/pub?lang=en" {
		t.Errorf("unknown book: status %d location %q", resp.StatusCode, loc)
	}
	resp, _ = get(t, srv, "/pub/library/alle-publikationen/wachtturm?lang=en")
	if loc := resp.Header.Get("Location"); resp.StatusCode != http.StatusSeeOther || loc != "/pub?lang=en" {
		t.Errorf("unknown path: status %d location %q", resp.StatusCode, loc)
	}
}

func TestAPIPubLibrary(t *testing.T) {
	srv := newTestServer(t, libraryMux(t))
	resp, body := get(t, srv, "/api/v1/pub/library?lang=en")
	var root struct {
		Kind   string `json:"kind"`
		Groups []struct {
			Cards []struct{ Kind, Title, Path string } `json:"cards"`
		} `json:"groups"`
	}
	if resp.StatusCode != 200 || json.Unmarshal([]byte(body), &root) != nil || root.Kind != "library" ||
		len(root.Groups) == 0 || root.Groups[0].Cards[0].Path != "publications/bibles" {
		t.Errorf("library root: status %d body %.300s", resp.StatusCode, body)
	}
	resp, body = get(t, srv, "/api/v1/pub/library/publications/watchtower/the-watchtower-2024/study-edition/may?lang=en")
	if resp.StatusCode != 200 || !strings.Contains(body, `"symbol": "w"`) || !strings.Contains(body, `"issue": "202405"`) {
		t.Errorf("issue: status %d body %.300s", resp.StatusCode, body)
	}
	resp, body = get(t, srv, "/api/v1/pub/publication/nwt?lang=en")
	if resp.StatusCode != 200 || !strings.Contains(body, `"bible": true`) {
		t.Errorf("publication: status %d body %.300s", resp.StatusCode, body)
	}
}

// studyMux adds what an unfolded verse walks through on top of wolMux: the
// verse's citation endpoint and the research-guide passage its pane points at.
func studyMux(t *testing.T) *http.ServeMux {
	mux := wolMux(t)
	mux.HandleFunc("/wol/bc/r1/lp-e/2024360/0/0", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"items": [{"title": "John 3:16",
			"content": "<p>For God loved the world so much</p>", "url": "/en/wol/b/r1/lp-e/nwtsty/43/3"}]}`))
	})
	mux.HandleFunc("/wol/pc/r1/lp-e/1204433/5/0", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"items": [{"content": "<p>Jehovah loved the world of redeemable mankind.</p>",
			"title": "God So Loved the World", "url": "/en/wol/d/r1/lp-e/2014486"}]}`))
	})
	return mux
}

type event struct {
	Type, HTML, Key, In, Stage, Text string
	Count                            int
}

// events reads a stream line by line.
func events(t *testing.T, body string) []event {
	t.Helper()
	var out []event
	for line := range strings.SplitSeq(strings.TrimSpace(body), "\n") {
		var ev event
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatalf("bad line %q: %v", line, err)
		}
		out = append(out, ev)
	}
	return out
}

func TestUnfoldVerseStream(t *testing.T) {
	srv := newTestServer(t, studyMux(t))
	resp, body := get(t, srv, "/unfold/verse?vid=43003016&depth=1&lang=en")
	if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "application/x-ndjson") {
		t.Fatalf("status %d, type %q: %s", resp.StatusCode, resp.Header.Get("Content-Type"), body)
	}
	evs := events(t, body)
	var sections []event
	for _, ev := range evs {
		if ev.Type == "section" {
			sections = append(sections, ev)
		}
	}
	if len(sections) == 0 {
		t.Fatalf("no sections in:\n%s", body)
	}
	// the study notes come first, as a disclosure of their own
	if !strings.Contains(sections[0].HTML, `<details class="section"><summary>Study notes</summary>`) {
		t.Errorf("first section is not the study notes: %s", sections[0].HTML)
	}
	last := evs[len(evs)-1]
	if last.Type != "done" || last.Count != len(sections) {
		t.Errorf("stream should close with done and the count of sections: %+v", last)
	}
	for _, s := range sections {
		if strings.Contains(s.HTML, "<script") {
			t.Errorf("script in a section: %s", s.HTML)
		}
	}

	resp, _ = get(t, srv, "/unfold/verse?vid=0&lang=en")
	if resp.StatusCode != 400 {
		t.Errorf("missing vid: status %d", resp.StatusCode)
	}
}

func TestUnfoldRefsStream(t *testing.T) {
	srv := newTestServer(t, studyMux(t))
	resp, body := get(t, srv, "/unfold/refs?lang=en&depth=1&path="+
		"https%3A%2F%2Fwol.jw.org%2Fen%2Fwol%2Fbc%2Fr1%2Flp-e%2F2024360%2F0%2F0&text=Joh+3%3A16")
	if resp.StatusCode != 200 {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
	evs := events(t, body)
	found := false
	for _, ev := range evs {
		if ev.Type == "section" && strings.Contains(ev.HTML, "<summary>Joh 3:16</summary>") &&
			strings.Contains(ev.HTML, "For God loved the world so much") {
			found = true
		}
	}
	if !found {
		t.Errorf("the citation did not come back as a section:\n%s", body)
	}

	// nothing but a citation is followed
	resp, _ = get(t, srv, "/unfold/refs?lang=en&path=%2Fen%2Fwol%2Fd%2Fr1%2Flp-e%2F2024360")
	if resp.StatusCode != 400 {
		t.Errorf("non-citation path: status %d", resp.StatusCode)
	}
}

// With ?lazy=1 the page is the document alone; the browser unfolds it to the
// level asked for once it is shown.
func TestUIArticleLazy(t *testing.T) {
	srv := newTestServer(t, studyMux(t))
	resp, body := get(t, srv, "/article?target=2024360&lang=en&unfold=2&lazy=1")
	if resp.StatusCode != 200 {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
	if !strings.Contains(body, `data-auto="2"`) || strings.Contains(body, `class="expansion"`) {
		t.Errorf("lazy page should defer the expansion:\n%s", body)
	}
	// without lazy the server unfolds, and folds what it brought
	_, body = get(t, srv, "/article?target=2024360&lang=en&unfold=1")
	if !strings.Contains(body, `class="expansion"`) || !strings.Contains(body, `<details class="section">`) {
		t.Errorf("server-side unfold should come folded:\n%s", body)
	}
	// who quotes a cited verse is a heading the page loads once opened, as in
	// a streamed expansion
	if !strings.Contains(body, `<details class="section" data-lazy="/unfold/cited?to=0&amp;vid=43003016">`) ||
		strings.Contains(body, "cited-lazy") {
		t.Errorf("server-side unfold should leave the citations to the page:\n%s", body)
	}
}

type budgetEvent struct {
	Type     string
	Requests int
	Count    int
}

// streamEvents fetches a stream and returns its events.
func streamEvents(t *testing.T, srv *httptest.Server, path string) []budgetEvent {
	t.Helper()
	resp, body := get(t, srv, path)
	if resp.StatusCode != 200 {
		t.Fatalf("%s: status %d: %s", path, resp.StatusCode, body)
	}
	var out []budgetEvent
	for line := range strings.SplitSeq(strings.TrimSpace(body), "\n") {
		var ev budgetEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatalf("bad line %q: %v", line, err)
		}
		out = append(out, ev)
	}
	return out
}

func hasEvent(evs []budgetEvent, typ string) bool {
	for _, ev := range evs {
		if ev.Type == typ {
			return true
		}
	}
	return false
}

// Citations that are each cheap are still one expansion: two dozen verse
// citations, each priced at its text, its chapter page and a quotation lookup,
// add up to more than the server spends unasked, and are asked about before
// any of them is spent.
func TestUnfoldRefsSharesTheBudget(t *testing.T) {
	srv := newTestServer(t, studyMux(t))
	q := url.Values{"lang": {"en"}, "depth": {"1"}}
	for i := range 24 {
		q.Add("path", fmt.Sprintf("/en/wol/bc/r1/lp-e/2024360/%d/0", i))
		q.Add("text", "v")
	}
	// who quotes a verse loads only once opened, so 24 citations are cheap
	// alone; on top of a run that spent most of the budget they are asked
	// about as one, before any of them is spent
	evs := streamEvents(t, srv, "/unfold/refs?"+q.Encode())
	if hasEvent(evs, "expensive") {
		t.Errorf("24 citations without their citation searches should be cheap: %+v", evs)
	}
	evs = streamEvents(t, srv, "/unfold/refs?"+q.Encode()+"&spent=1980")
	if !hasEvent(evs, "expensive") || hasEvent(evs, "section") {
		t.Errorf("24 citations should be asked about as one: %+v", evs)
	}
	// one of them alone goes through, and says what it spent
	one := "/unfold/refs?lang=en&depth=1&path=%2Fen%2Fwol%2Fbc%2Fr1%2Flp-e%2F2024360%2F0%2F0&text=Joh+3%3A16"
	evs = streamEvents(t, srv, one)
	if hasEvent(evs, "expensive") || evs[len(evs)-1].Requests == 0 {
		t.Errorf("a single citation should go through and report its cost: %+v", evs)
	}
	// but not on top of what the run before it already spent
	evs = streamEvents(t, srv, one+"&spent=1999")
	if !hasEvent(evs, "expensive") {
		t.Errorf("?spent= should count towards the budget: %+v", evs)
	}
	if evs = streamEvents(t, srv, one+"&spent=1999&force=1"); hasEvent(evs, "expensive") {
		t.Errorf("force should spend it: %+v", evs)
	}
	if resp, _ := get(t, srv, one+"&spent=lots"); resp.StatusCode != 400 {
		t.Errorf("bad spent: status %d", resp.StatusCode)
	}
}

// A verse is weighed together with the run it is part of as well.
func TestUnfoldVerseCountsWhatWasSpent(t *testing.T) {
	srv := newTestServer(t, studyMux(t))
	evs := streamEvents(t, srv, "/unfold/verse?vid=43003016&depth=1&lang=en&spent=1999")
	if !hasEvent(evs, "expensive") {
		t.Errorf("want the verse asked about on top of ?spent=: %+v", evs)
	}
	evs = streamEvents(t, srv, "/unfold/verse?vid=43003016&depth=1&lang=en")
	if hasEvent(evs, "expensive") || evs[len(evs)-1].Requests == 0 {
		t.Errorf("alone the verse goes through and reports its cost: %+v", evs)
	}
}

// Only a footnote marker is followed by the footnote stream, and only an
// article of the library or of jw.org by the article stream.
func TestUnfoldFootnoteAndArticleRefuseOthers(t *testing.T) {
	srv := newTestServer(t, studyMux(t))
	for _, path := range []string{
		"/unfold/footnote?lang=en&path=%2Fen%2Fwol%2Fbc%2Fr1%2Flp-e%2F2024360%2F0%2F0",
		"/unfold/article?lang=en&url=https%3A%2F%2Fevil.example%2Fx",
		"/unfold/translations?lang=en&vid=0",
	} {
		if resp, _ := get(t, srv, path); resp.StatusCode != 400 {
			t.Errorf("%s: status %d, want 400", path, resp.StatusCode)
		}
	}
	// the article stream reads a library document into one section, keyed by
	// its path
	resp, body := get(t, srv, "/unfold/article?lang=en&url=https%3A%2F%2Fwol.jw.org%2Fen%2Fwol%2Fd%2Fr1%2Flp-e%2F2024360")
	if resp.StatusCode != 200 || !strings.Contains(body, `data-ref=\"/en/wol/d/r1/lp-e/2024360\"`) ||
		!strings.Contains(body, "CALEB trusted in Jehovah") {
		t.Errorf("article stream: status %d:\n%s", resp.StatusCode, body)
	}
}

// binavMux adds the library's bible navigation: the book grid of nwtsty and the
// chapter grid of Matthew (the fixtures are the German pages; the layout is the
// same in every language).
func binavMux(t *testing.T) *http.ServeMux {
	mux := wolMux(t)
	mux.HandleFunc("/en/wol/binav/r1/lp-e/nwtsty", wolFixture(t, "binav_de.html"))
	mux.HandleFunc("/en/wol/binav/r1/lp-e/nwtsty/40", wolFixture(t, "binav_de_40.html"))
	return mux
}

func TestUIBibleNav(t *testing.T) {
	srv := newTestServer(t, binavMux(t))

	// the page opens on the book grid, under the edition and the reference
	resp, body := get(t, srv, "/bible?lang=en")
	if resp.StatusCode != 200 {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
	edition, query, grid := strings.Index(body, `class="edition"`), strings.Index(body, `name="ref"`), strings.Index(body, `class="bible-nav"`)
	if edition < 0 || query < edition || grid < query {
		t.Errorf("want edition, then reference, then the book grid:\n%s", body)
	}
	if !strings.Contains(body, `<li class="book gospels"><a href="/bible?bible=nwtsty&amp;book=40&amp;lang=en"`) {
		t.Errorf("missing the Matthew tile:\n%s", body)
	}
	if !strings.Contains(body, `href="/bible?lang=en" data-reset="bible"`) {
		t.Errorf("the start page has no reset:\n%s", body)
	}

	// a book opens its chapter grid, every chapter a reading
	resp, body = get(t, srv, "/bible?bible=nwtsty&book=40&lang=en")
	if resp.StatusCode != 200 {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
	if !strings.Contains(body, "Das Evangelium nach Matthäus") ||
		!strings.Contains(body, `href="/bible?bible=nwtsty&amp;ref=Matthew&#43;28&amp;lang=en">28</a>`) {
		t.Errorf("missing the chapter grid:\n%s", body)
	}
	if !strings.Contains(body, `href="/bible?lang=en" data-reset="bible"`) {
		t.Errorf("missing the reset:\n%s", body)
	}

	// a reading shows no grid, and resets back to it
	_, body = get(t, srv, "/bible?ref=John+3:16&lang=en")
	if strings.Contains(body, `bible-nav`) || !strings.Contains(body, `data-reset="bible"`) {
		t.Errorf("a reading with a grid, or without a reset:\n%s", body)
	}

	resp, body = get(t, srv, "/api/v1/bible/nav?book=40&lang=en")
	if resp.StatusCode != 200 || !strings.Contains(body, `"chapters": [`) {
		t.Errorf("api: status %d: %s", resp.StatusCode, body)
	}
	resp, _ = get(t, srv, "/api/v1/bible/nav?book=99&lang=en")
	if resp.StatusCode != 400 {
		t.Errorf("api: book 99: status %d", resp.StatusCode)
	}
}

// Who quotes a verse is a heading the page loads once it is opened, never part
// of the verse's own stream.
func TestUnfoldVerseCitedIsLazy(t *testing.T) {
	srv := newTestServer(t, studyMux(t))
	_, body := get(t, srv, "/unfold/verse?vid=43003016&depth=1&lang=en")
	if !strings.Contains(body, `data-lazy=\"/unfold/cited?to=0\u0026amp;vid=43003016\"`) {
		t.Errorf("want a lazy citations section: %s", body)
	}
	if strings.Contains(body, `"stage":"cited"`) {
		t.Errorf("the citation search should not run with the verse: %s", body)
	}
	if resp, _ := get(t, srv, "/unfold/cited?vid=0"); resp.StatusCode != 400 {
		t.Errorf("bad vid: status %d", resp.StatusCode)
	}
	if resp, _ := get(t, srv, "/unfold/cited?vid=43003016&to=2"); resp.StatusCode != 400 {
		t.Errorf("a range ending before it starts: status %d", resp.StatusCode)
	}
}

// A bible page unfolded by the server gives every verse its citations heading,
// loaded by the page once opened rather than searched for with the page.
func TestUIBibleUnfoldCitedIsLazy(t *testing.T) {
	srv := newTestServer(t, studyMux(t))
	_, body := get(t, srv, "/bible?ref=John+3:16&lang=en&unfold=1")
	if !strings.Contains(body, `<details class="section" data-lazy="/unfold/cited?to=0&amp;vid=43003016"><summary>Quotations of John 3:16</summary>`) ||
		strings.Contains(body, "cited-lazy") {
		t.Errorf("want a lazy citations section:\n%s", body)
	}
}

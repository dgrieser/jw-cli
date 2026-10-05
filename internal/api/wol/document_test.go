package wol

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/dgrieser/jw-cli/internal/httpx"
)

func testClient(t *testing.T, mux *http.ServeMux) *Client {
	t.Helper()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	cache := httpx.OpenCacheAt(t.TempDir())
	hc := httpx.New(httpx.WithBaseURLs(httpx.BaseURLs{WOL: srv.URL, CDN: srv.URL, JWOrg: srv.URL}), httpx.WithResponseCache(cache))
	return New(hc, cache)
}

func serveFile(t *testing.T, path string) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("fixture %s: %v", path, err)
			http.Error(w, err.Error(), 500)
			return
		}
		w.Write(b)
	}
}

// The real homepage lists every other language via <link rel="alternate">
// before the first same-locale link, and those hreflang locales collide with
// real ones — so the body must never win over the redirect target.
const altLangHead = `<link rel="alternate" hreflang="mio" href="/mio/wol/h/r996/lp-mxc" />
	<link rel="alternate" hreflang="de" href="/de/wol/h/r969/lp-mnn" />`

func TestConfigFor(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/de", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/de/wol/h/r10/lp-x", http.StatusFound)
	})
	mux.HandleFunc("/de/wol/h/r10/lp-x", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html><head>` + altLangHead + `</head><body>Startseite</body></html>`))
	})
	c := testClient(t, mux)
	cfg, err := c.ConfigFor(context.Background(), "de")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Rsconf != "r10" || cfg.Lp != "lp-x" || cfg.Locale != "de" {
		t.Errorf("cfg = %+v", cfg)
	}
	// cached second call must not hit the network
	mux2 := http.NewServeMux() // no handler: would 404
	c.hc = httpx.New(httpx.WithBaseURLs(httpx.BaseURLs{WOL: httptest.NewServer(mux2).URL}))
	if _, err := c.ConfigFor(context.Background(), "de"); err != nil {
		t.Errorf("cached lookup failed: %v", err)
	}
}

// Without a redirect, the body scan must still stay on the requested locale.
func TestConfigForNoRedirect(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/de", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html><head>
			<link rel="alternate" hreflang="mio" href="/mio/wol/h/r996/lp-mxc" />
		</head><body>
			<a href="/de/wol/h/r10/lp-x">Startseite</a>
		</body></html>`))
	})
	c := testClient(t, mux)
	cfg, err := c.ConfigFor(context.Background(), "de")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Rsconf != "r10" || cfg.Lp != "lp-x" || cfg.Locale != "de" {
		t.Errorf("cfg = %+v", cfg)
	}
}

func TestDocument(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/en/wol/d/r1/lp-e/2024360", serveFile(t, "testdata/document.html"))
	c := testClient(t, mux)

	art, err := c.Document(context.Background(), Config{Locale: "en", Rsconf: "r1", Lp: "lp-e"}, 2024360)
	if err != nil {
		t.Fatal(err)
	}
	if art.Title != "Caleb—He Fought Loyally for His God" {
		t.Errorf("title = %q", art.Title)
	}
	if art.DocID != 2024360 {
		t.Errorf("docid = %d", art.DocID)
	}
	if len(art.ScriptureRefs) != 2 {
		t.Fatalf("got %d scripture refs, want 2: %+v", len(art.ScriptureRefs), art.ScriptureRefs)
	}
	ref := art.ScriptureRefs[0]
	if ref.Text != "Num. 14:24" || ref.BID != "1-1" || ref.BCPath == "" {
		t.Errorf("ref = %+v", ref)
	}
	if len(art.Images) != 1 {
		t.Fatalf("got %d images: %+v", len(art.Images), art.Images)
	}
	img := art.Images[0]
	if img.URL != "https://cms-imgp.example/caleb_lg.jpg" || img.Caption != "Caleb receives Hebron as an inheritance" {
		t.Errorf("img = %+v", img)
	}
}

// A citation wol answers without content is read from the paragraphs its link
// names on the document: the outermost of them only, so a box brings its
// paragraphs once.
func TestPassageReadsTheNamedParagraphs(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/de/wol/d/r10/lp-x/1102010144", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html><body><div id="article">
		<p data-pid="42">before</p>
		<div class="boxContent" data-pid="43"><p data-pid="44">in the box</p></div>
		<p data-pid="45">after the box</p>
		<p data-pid="49">past the end</p>
		</div></body></html>`))
	})
	c := testClient(t, mux)
	got, err := c.Passage(context.Background(), c.hc.Base.WOL+"/de/wol/d/r10/lp-x/1102010144#h=43:0-48:0")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"in the box", "after the box"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %s", want, got)
		}
	}
	if strings.Contains(got, "before") || strings.Contains(got, "past the end") || strings.Count(got, "in the box") != 1 {
		t.Errorf("wrong extent: %s", got)
	}
	if got, _ := c.Passage(context.Background(), c.hc.Base.WOL+"/de/wol/d/r10/lp-x/1102010144"); got != "" {
		t.Errorf("a link without an extent names nothing: %s", got)
	}
}

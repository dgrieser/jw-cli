package wol

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/dgrieser/jw-cli/internal/httpx"
)

func testClient(t *testing.T, mux *http.ServeMux) *Client {
	t.Helper()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	hc := httpx.New(httpx.WithBaseURLs(httpx.BaseURLs{WOL: srv.URL, CDN: srv.URL, JWOrg: srv.URL}))
	return New(hc, httpx.OpenCacheAt(t.TempDir()))
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

func TestConfigFor(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/de", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html><body>
			<a href="/de/wol/h/r10/lp-x">Startseite</a>
			<a href="/de/wol/d/r10/lp-x/2024360">Artikel</a>
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
	// cached second call must not hit the network
	mux2 := http.NewServeMux() // no handler: would 404
	c.hc = httpx.New(httpx.WithBaseURLs(httpx.BaseURLs{WOL: httptest.NewServer(mux2).URL}))
	if _, err := c.ConfigFor(context.Background(), "de"); err != nil {
		t.Errorf("cached lookup failed: %v", err)
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

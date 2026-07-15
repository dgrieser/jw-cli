package pubmedia

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/dgrieser/jw-cli/internal/httpx"
)

func testClient(t *testing.T, handler http.HandlerFunc) (*Client, *http.Request) {
	t.Helper()
	var captured *http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r
		handler(w, r)
	}))
	t.Cleanup(srv.Close)
	hc := httpx.New(httpx.WithBaseURLs(httpx.BaseURLs{CDN: srv.URL, JWOrg: srv.URL, WOL: srv.URL}))
	return New(hc), captured
}

func serveFile(t *testing.T, path string) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("fixture %s: %v", path, err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(b)
	}
}

func TestLinks(t *testing.T) {
	var got *http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r
		serveFile(t, "testdata/w_202405.json")(w, r)
	}))
	t.Cleanup(srv.Close)
	c := New(httpx.New(httpx.WithBaseURLs(httpx.BaseURLs{CDN: srv.URL})))

	pm, err := c.Links(context.Background(), Query{Pub: "w", Issue: "202405", Lang: "E", Formats: []string{"PDF", "EPUB"}})
	if err != nil {
		t.Fatal(err)
	}
	q := got.URL.Query()
	if q.Get("pub") != "w" || q.Get("issue") != "202405" || q.Get("langwritten") != "E" ||
		q.Get("fileformat") != "PDF,EPUB" || q.Get("output") != "json" {
		t.Errorf("unexpected query: %s", got.URL.RawQuery)
	}
	if pm.PubName == "" || pm.Issue != "202405" {
		t.Errorf("unexpected meta: %+v", pm)
	}
	pdfs := pm.Files["E"]["PDF"]
	if len(pdfs) != 1 || pdfs[0].URL != "https://download.example/w_E_202405.pdf" ||
		pdfs[0].Checksum == "" || pdfs[0].Filesize != 2048000 || pdfs[0].Format != "PDF" {
		t.Errorf("unexpected PDF entry: %+v", pdfs)
	}
	if len(pm.Files["E"]["EPUB"]) != 1 {
		t.Errorf("missing EPUB entry")
	}
}

func TestLinksNotFoundEnvelope(t *testing.T) {
	c, _ := testClient(t, serveFile(t, "testdata/error.json"))
	_, err := c.Links(context.Background(), Query{Pub: "nope", Lang: "E"})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestLinksHTTP404(t *testing.T) {
	c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", 404)
	})
	_, err := c.Links(context.Background(), Query{Pub: "nope", Lang: "E"})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

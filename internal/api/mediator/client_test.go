package mediator

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
	hc := httpx.New(httpx.WithBaseURLs(httpx.BaseURLs{CDN: srv.URL, JWOrg: srv.URL, WOL: srv.URL}))
	return New(hc)
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
		w.Header().Set("Content-Type", "application/json")
		w.Write(b)
	}
}

func TestLanguages(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/apis/mediator/v1/languages/E/web", serveFile(t, "testdata/languages.json"))
	c := testClient(t, mux)

	langs, err := c.Languages(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(langs) != 5 {
		t.Fatalf("got %d languages, want 5", len(langs))
	}
	de := langs[1]
	if de.Symbol != "X" || de.Locale != "de" || de.Name != "German" || de.Vernacular != "Deutsch" {
		t.Errorf("unexpected German mapping: %+v", de)
	}
	if !langs[4].IsSignLanguage {
		t.Errorf("ASL should be a sign language: %+v", langs[4])
	}
}

func TestMediaItem(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/apis/mediator/v1/media-items/E/pub-jwb_202401_1_VIDEO", serveFile(t, "testdata/media_item.json"))
	c := testClient(t, mux)

	item, err := c.MediaItem(context.Background(), "E", "pub-jwb_202401_1_VIDEO")
	if err != nil {
		t.Fatal(err)
	}
	if item.Title != "JW Broadcasting—January 2024" {
		t.Errorf("title = %q", item.Title)
	}
	if len(item.Files) != 3 {
		t.Fatalf("got %d files, want 3", len(item.Files))
	}
	best := item.Files[2]
	if best.Label != "720p" || best.FrameHeight != 720 || best.SubtitlesURL == "" {
		t.Errorf("unexpected 720p file: %+v", best)
	}
}

func TestCategory(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/apis/mediator/v1/categories/E/VideoOnDemand", serveFile(t, "testdata/category.json"))
	c := testClient(t, mux)

	cat, err := c.Category(context.Background(), "E", "VideoOnDemand", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if cat.Key != "VideoOnDemand" || cat.Type != "container" {
		t.Errorf("unexpected category: %+v", cat)
	}
	if len(cat.Subcategories) != 2 {
		t.Errorf("got %d subcategories, want 2", len(cat.Subcategories))
	}
}

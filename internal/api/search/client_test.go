package search

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dgrieser/jw-cli/internal/httpx"
)

func makeJWT(exp time.Time) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`{"exp":%d}`, exp.Unix())))
	return header + "." + payload + ".sig"
}

const searchBody = `{
  "layout": ["flat"],
  "results": [
    {
      "type": "item", "subtype": "article",
      "title": "Caleb—He Fought Loyally", "snippet": "In what situations...",
      "context": "WALK COURAGEOUSLY",
      "links": {"jw.org": "https://www.jw.org/open?docid=1102025912", "wol": "https://wol.jw.org/wol/finder?docid=1102025912"},
      "lank": "pa-1102025912",
      "image": {"type": "sqs", "url": "https://cms-imgp.example/img.jpg", "altText": "Caleb"}
    },
    {
      "type": "group", "title": "Videos",
      "results": [
        {"type": "item", "subtype": "video", "title": "A Video", "lank": "pub-abc_1_VIDEO",
         "duration": "5:00", "links": {"jw.org": "https://www.jw.org/finder?lank=pub-abc_1_VIDEO"}}
      ]
    }
  ],
  "insight": {"query": "Caleb", "filter": "all", "sort": "rel", "total": {"value": 354, "relation": "eq"}},
  "pagination": {"label": "Showing 1 - 10 of 354", "links": []},
  "filters": [{"label": "Publications", "selected": false}],
  "sorts": [{"label": "Newest", "selected": false}]
}`

func TestSearchWithTokenRefresh(t *testing.T) {
	var tokenFetches, unauthorized atomic.Int32
	staleToken := makeJWT(time.Now().Add(time.Hour)) // valid-looking but server rejects it once

	mux := http.NewServeMux()
	mux.HandleFunc("/tokens/jworg.jwt", func(w http.ResponseWriter, r *http.Request) {
		n := tokenFetches.Add(1)
		if n == 1 {
			fmt.Fprint(w, staleToken)
			return
		}
		fmt.Fprint(w, makeJWT(time.Now().Add(2*time.Hour)))
	})
	mux.HandleFunc("/apis/search/results/E/all", func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth == "Bearer "+staleToken {
			unauthorized.Add(1)
			http.Error(w, "expired", http.StatusUnauthorized)
			return
		}
		if r.URL.Query().Get("q") != "Caleb" {
			http.Error(w, "bad q", 400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, searchBody)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	c := New(httpx.New(httpx.WithBaseURLs(httpx.BaseURLs{CDN: srv.URL})))
	page, err := c.Search(context.Background(), "E", Params{Query: "Caleb", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if unauthorized.Load() != 1 || tokenFetches.Load() != 2 {
		t.Errorf("token refresh flow: unauthorized=%d fetches=%d", unauthorized.Load(), tokenFetches.Load())
	}
	if page.Total != 354 {
		t.Errorf("total = %d", page.Total)
	}
	if len(page.Results) != 2 {
		t.Fatalf("got %d results (group not flattened?)", len(page.Results))
	}
	art := page.Results[0]
	if art.Kind != "article" || art.DocID != 1102025912 || art.WOLLink == "" {
		t.Errorf("article result: %+v", art)
	}
	vid := page.Results[1]
	if vid.Kind != "video" || vid.LANK != "pub-abc_1_VIDEO" || vid.Duration != "5:00" {
		t.Errorf("video result: %+v", vid)
	}
}

func TestSearchValidation(t *testing.T) {
	c := New(httpx.New())
	if _, err := c.Search(context.Background(), "E", Params{Query: "x", Facet: "bogus"}); err == nil {
		t.Error("want error for invalid facet")
	}
	if _, err := c.Search(context.Background(), "E", Params{Query: "x", Sort: "bogus"}); err == nil {
		t.Error("want error for invalid sort")
	}
}

func TestTokenCaching(t *testing.T) {
	var fetches atomic.Int32
	ts := NewTokenSource(func(ctx context.Context) (string, error) {
		fetches.Add(1)
		return `"` + makeJWT(time.Now().Add(time.Hour)) + `"` + "\n", nil
	})
	tok1, err := ts.Token(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if tok1[0] == '"' {
		t.Errorf("token not unquoted: %q", tok1)
	}
	tok2, _ := ts.Token(context.Background())
	if tok1 != tok2 || fetches.Load() != 1 {
		t.Errorf("token should be cached: fetches=%d", fetches.Load())
	}
	ts.Invalidate()
	_, _ = ts.Token(context.Background())
	if fetches.Load() != 2 {
		t.Errorf("invalidate should force refetch: fetches=%d", fetches.Load())
	}
}

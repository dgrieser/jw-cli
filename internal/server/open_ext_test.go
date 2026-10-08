package server_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestOpenCitation(t *testing.T) {
	mux := studyMux(t)
	mux.HandleFunc("/wol/bc/r1/lp-e/2024360/1/0", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"items": [{"title": "John 3:16, 17",
			"content": "<p><span id=\"v43-3-16-1\">For God</span> <span id=\"v43-3-17-1\">For God sent</span></p>",
			"url": "/en/wol/b/r1/lp-e/nwtsty/43/3#v=43:3:16-43:3:17"}]}`))
	})
	srv := newTestServer(t, mux)
	for path, want := range map[string]string{
		// the verses a citation quotes open in the bible reader
		"/en/wol/bc/r1/lp-e/2024360/1/0": "/bible?bible=nwt&lang=en&ref=" + url.QueryEscape("John 3:16-17"),
	} {
		resp, body := get(t, srv, "/open?lang=en&bible=nwt&path="+url.QueryEscape(path))
		if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != want {
			t.Errorf("%s: status %d location %q, want %q; body %.300s", path, resp.StatusCode, resp.Header.Get("Location"), want, body)
		}
	}
	// a passage of a publication opens the whole article
	resp, _ := get(t, srv, "/open?lang=en&path="+url.QueryEscape("/en/wol/pc/r1/lp-e/1204433/5/0"))
	loc, _ := url.Parse(resp.Header.Get("Location"))
	if resp.StatusCode != http.StatusFound || loc == nil || loc.Path != "/article" ||
		!strings.HasSuffix(loc.Query().Get("target"), "/en/wol/d/r1/lp-e/2014486") {
		t.Errorf("publication: status %d location %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	if resp, _ := get(t, srv, "/open?lang=en&path="+url.QueryEscape("https://evil.example/x")); resp.StatusCode == http.StatusFound {
		t.Errorf("a path that is no citation was followed to %q", resp.Header.Get("Location"))
	}
}

func TestOpenCitationAPI(t *testing.T) {
	srv := newTestServer(t, studyMux(t))
	resp, body := get(t, srv, "/api/v1/open?lang=en&path="+url.QueryEscape("/en/wol/pc/r1/lp-e/1204433/5/0"))
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, `"url"`) || !strings.Contains(body, "/article?") {
		t.Errorf("status %d body %.300s", resp.StatusCode, body)
	}
	resp, _ = get(t, srv, "/api/v1/open?lang=en&path="+url.QueryEscape("/en/wol/d/r1/lp-e/1"))
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("a document that is no citation: status %d, want 400", resp.StatusCode)
	}
}

func TestServiceWorker(t *testing.T) {
	srv := newTestServer(t, studyMux(t))
	resp, body := get(t, srv, "/sw.js")
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/javascript") {
		t.Fatalf("status %d type %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	if strings.Contains(body, "__BUILD__") || !strings.Contains(body, `var BUILD = "`) {
		t.Errorf("the worker is not stamped with the build: %.400s", body)
	}
}

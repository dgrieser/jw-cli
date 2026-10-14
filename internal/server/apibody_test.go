package server_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// untitledMux serves a document whose body does not open with its title.
func untitledMux(t *testing.T) *http.ServeMux {
	mux := wolMux(t)
	mux.HandleFunc("/en/wol/d/r1/lp-e/2024361", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html><head><title>Plain Words</title></head><body>
		<div id="article"><div class="bodyTxt">
		  <p>Read <a href="/en/wol/bc/r1/lp-e/2024361/0/0" class="b">Num. 14:24</a> and look <img src="/img/a.jpg" alt="a field"></p>
		</div></div></body></html>`))
	})
	return mux
}

// apiBody is the rendered body of an article-shaped API answer.
func apiBody(t *testing.T, body string) string {
	t.Helper()
	var out struct{ Body string }
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("%v: %s", err, body)
	}
	return out.Body
}

// A markdown or text body is headed by the document's title, as jw article
// prints it; an HTML body is not.
func TestAPIBodyTitle(t *testing.T) {
	srv := newTestServer(t, untitledMux(t))
	_, body := get(t, srv, "/api/v1/article?target=2024361&lang=en&format=markdown")
	if md := apiBody(t, body); !strings.HasPrefix(md, "# Plain Words\n\nRead ") {
		t.Errorf("markdown body:\n%s", md)
	}
	_, body = get(t, srv, "/api/v1/article?target=2024361&lang=en&format=text")
	if txt := apiBody(t, body); !strings.HasPrefix(txt, "Plain Words\n\nRead ") {
		t.Errorf("text body:\n%s", txt)
	}
	_, body = get(t, srv, "/api/v1/article?target=2024361&lang=en")
	if html := apiBody(t, body); strings.Contains(html, "Plain Words") {
		t.Errorf("html body:\n%s", html)
	}
	// a document that opens with its title is not headed twice
	_, body = get(t, srv, "/api/v1/article?target=2024360&lang=en&format=markdown")
	if md := apiBody(t, body); strings.Count(md, "Caleb—He Fought Loyally") != 1 {
		t.Errorf("titled document:\n%s", md)
	}
}

// urls=0 renders a body as --no-urls does: a link keeps its text, a picture
// its alt text, and nothing says where either was.
func TestAPIBodyNoURLs(t *testing.T) {
	srv := newTestServer(t, untitledMux(t))
	for _, format := range []string{"markdown", "text", "html"} {
		_, body := get(t, srv, "/api/v1/article?target=2024361&lang=en&urls=0&format="+format)
		out := apiBody(t, body)
		if !strings.Contains(out, "Num. 14:24") || !strings.Contains(out, "[image: a field]") ||
			strings.Contains(out, "http") || strings.Contains(out, "href") {
			t.Errorf("%s body:\n%s", format, out)
		}
	}
	_, body := get(t, srv, "/api/v1/article?target=2024361&lang=en&format=markdown")
	if md := apiBody(t, body); !strings.Contains(md, "[Num. 14:24](http") {
		t.Errorf("without urls=0 the link stays:\n%s", md)
	}
	// a section left to load says so without its address
	srv = newTestServer(t, hopsMux(t))
	_, body = get(t, srv, "/api/v1/bible/read?ref=John+3:16&lang=en&unfold=1&format=markdown&urls=0")
	if md := apiBody(t, body); strings.Contains(md, "/unfold/") || strings.Contains(md, "](") ||
		!strings.Contains(md, "### Quotations of John 3:16\n\n…") {
		t.Errorf("bible read without urls:\n%s", md)
	}
}

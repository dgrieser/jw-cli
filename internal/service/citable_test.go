package service

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/dgrieser/jw-cli/internal/i18n"
	"github.com/dgrieser/jw-cli/internal/model"
)

// A jw.org article writes the verses it cites as links into jw.org's study
// bible, which are no citations to follow; unfolded, it is read from the
// library's copy of the same document, whose citations are.
func TestUnfoldJWOrgArticleFromTheLibrary(t *testing.T) {
	s := unfoldUpstream(t, func(mux *http.ServeMux) {
		mux.HandleFunc("/en/wol/d/r1/lp-e/2024360", func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`<html><body><article id="article"><h1>Love</h1>` +
				`<p data-pid="1">Love (<a href="/en/wol/bc/r1/lp-e/2024360/0/0" class="b">Joh 3:16</a>).</p>` +
				`</article></body></html>`))
		})
		mux.HandleFunc("/wol/bc/r1/lp-e/2024360/0/0", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"items":[{"title":"John 3:16",` +
				`"content":"<p><span id=\"v43-3-16-1\" class=\"v\">For God loved the world</span></p>"}]}`))
		})
		// a jw.org page the library does not carry
		mux.HandleFunc("/en/wol/d/r1/lp-e/9999", func(w http.ResponseWriter, r *http.Request) {
			http.NotFound(w, r)
		})
	})
	jw := `<p>Love (<a class="jsBibleLink" data-targetverses="43003016-43003016" ` +
		`href="https://www.jw.org/en/library/bible/study-bible/books/john/3/#v43003016">John 3:16</a>).</p>`
	art := model.Article{DocID: 2024360, URL: "https://www.jw.org/en/library/magazines/love/", HTML: jw}
	out, err := s.UnfoldArticle(context.Background(), english, art, UnfoldConfig{Depth: 1}, i18n.EN.Text())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "For God loved the world") {
		t.Errorf("the verse the article cites is not unfolded:\n%s", out)
	}

	art = model.Article{DocID: 9999, URL: "https://www.jw.org/en/news/x/", HTML: jw}
	out, err = s.UnfoldArticle(context.Background(), english, art, UnfoldConfig{Depth: 1}, i18n.EN.Text())
	if err != nil || !strings.Contains(out, "jsBibleLink") {
		t.Errorf("a page the library lacks is left as it is: err %v\n%s", err, out)
	}
}

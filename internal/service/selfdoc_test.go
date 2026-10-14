package service

import (
	"context"
	"net/http"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dgrieser/jw-cli/internal/i18n"
	"github.com/dgrieser/jw-cli/internal/model"
)

// The daily text of 1 January 2026 is a day of the January issue of
// Examining the Scriptures Daily—2026, docid 1102026200, and the page says so
// only in the classes of the element holding it.
func TestDailyTextKnowsItsDocument(t *testing.T) {
	page, err := os.ReadFile("testdata/dailytext_2026-01-01.html")
	if err != nil {
		t.Fatal(err)
	}
	s := unfoldUpstream(t, func(mux *http.ServeMux) {
		mux.HandleFunc("/en/wol/dt/r1/lp-e/2026/1/1", func(w http.ResponseWriter, r *http.Request) {
			w.Write(page)
		})
	})
	art, err := s.DailyText(context.Background(), english, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if art.DocID != 1102026200 {
		t.Errorf("DocID = %d, want 1102026200", art.DocID)
	}
}

// A document is never listed as quoting the verses it cites, whatever it
// knows of itself: a daily text has no id and a link that is a date, but its
// citations are written in the document it is part of.
func TestArticleDocsOfADailyText(t *testing.T) {
	art := model.Article{
		URL: "https://wol.jw.org/en/wol/dt/r1/lp-e/2026/1/1",
		HTML: `<p>(<a href="/en/wol/bc/r1/lp-e/1102026200/1/0" class="b">1 Cor. 3:1</a>) ` +
			`<a href="/en/wol/pc/r1/lp-e/1102026200/0/0"><em>w24.04</em> 2 ¶1</a></p>`,
	}
	docs := articleDocs(art)
	if !slices.Contains(docs, 1102026200) || slices.Contains(docs, 1) {
		t.Errorf("articleDocs = %v, want the daily text's document and not the day of the month", docs)
	}
	r := newTooltipResolver(nil, english, nil).excluding(docs...)
	if !r.isSelf(model.Result{WOLLink: "https://wol.jw.org/en/wol/d/r1/lp-e/1102026200?q=x"}) {
		t.Error("the daily text's own document is listed as quoting its verses")
	}
	if r.isSelf(model.Result{WOLLink: "https://wol.jw.org/en/wol/d/r1/lp-e/1"}) {
		t.Error("a document is left out for being the day of the month")
	}
}

// A paragraph unfolded on a page leaves the document it is part of out of who
// quotes the verses it brings, whether the page says which document that is or
// not: the citations it follows are written in it.
func TestStreamRefsLeaveOutTheCitingDocument(t *testing.T) {
	s := unfoldUpstream(t, func(mux *http.ServeMux) {
		mux.HandleFunc("/wol/pc/r1/lp-e/1102026200/0/0", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"items":[{"title":"Keep growing","url":"/wol/d/r1/lp-e/2024284#h=1:0-1:0",` +
				`"content":"<p>Grow (<a href=\"/wol/bc/r1/lp-e/2024284/1/0\" class=\"b\">Joh 3:16</a>).</p>"}]}`))
		})
		mux.HandleFunc("/wol/bc/r1/lp-e/2024284/1/0", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"items":[{"title":"John 3:16",` +
				`"content":"<p><span id=\"v43-3-16-1\" class=\"v\">For God loved the world</span></p>"}]}`))
		})
	})
	var bodies []string
	_, _, err := s.StreamRefsUnfold(context.Background(), english, []CitationRef{
		{Path: "/en/wol/pc/r1/lp-e/1102026200/0/0", Text: "w24.04 2 ¶1"},
	}, UnfoldConfig{Depth: 1}, i18n.EN.Text(), UnfoldStream{Section: func(sec UnfoldSection) {
		bodies = append(bodies, sec.Body)
	}})
	if err != nil {
		t.Fatal(err)
	}
	body := strings.Join(bodies, "")
	if !strings.Contains(body, "/unfold/cited?") || !strings.Contains(body, "self=1102026200") {
		t.Errorf("who quotes John 3:16 does not leave out the daily text:\n%s", body)
	}
}

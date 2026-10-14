package service

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/dgrieser/jw-cli/internal/bibleref"
	"github.com/dgrieser/jw-cli/internal/i18n"
	"github.com/dgrieser/jw-cli/internal/model"
)

// busySearch is wol answering its search with 503, as it does now and then.
func busySearch(mux *http.ServeMux) {
	mux.HandleFunc("/en/wol/s/r1/lp-e", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
	})
	mux.HandleFunc("/wol/bc/r1/lp-e/2024360/0/0", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"items":[{"title":"John 3:16",` +
			`"content":"<p><span id=\"v43-3-16-1\" class=\"v\">For God loved the world</span></p>"}]}`))
	})
}

const citedFailed = "the quotations could not be listed"

// jw bible read --unfold says the quotations of a verse could not be listed
// when the search behind them fails, rather than leaving them out as if
// nobody quoted the verse.
func TestReadUnfoldSaysTheQuotationsFailed(t *testing.T) {
	s := unfoldUpstream(t, busySearch)
	res, err := s.ReadPassages(context.Background(), english, ReadRequest{
		Refs: "John 3:16", Edition: "nwtsty", NoOutlines: true, Unfold: UnfoldConfig{Depth: 1, Cited: true},
	}, i18n.EN.Text())
	if err != nil {
		t.Fatal(err)
	}
	out := res.Passages[0].Verses[0].Unfold
	at := strings.Index(out, "Quotations of John 3:16")
	if at < 0 || !strings.Contains(out[at:], citedFailed) {
		t.Errorf("the failed search is not reported under the quotations:\n%s", out)
	}
}

// A document unfolded on the command line says the same for a verse it cites.
func TestUnfoldArticleSaysTheQuotationsFailed(t *testing.T) {
	s := unfoldUpstream(t, busySearch)
	art := model.Article{DocID: 2024360, HTML: `<p>Love (<a href="/en/wol/bc/r1/lp-e/2024360/0/0" class="b">Joh 3:16</a>).</p>`}
	out, err := s.UnfoldArticle(context.Background(), english, art, UnfoldConfig{Depth: 1, Cited: true}, i18n.EN.Text())
	if err != nil {
		t.Fatal(err)
	}
	at := strings.Index(out, "Quotations of John 3:16")
	if at < 0 || !strings.Contains(out[at:], citedFailed) {
		t.Errorf("the failed search is not reported under the quotations:\n%s", out)
	}
}

// The page's stream of a verse's quotations ends in an error worded for the
// reader, never in an empty answer.
func TestStreamCitedReportsAFailedSearch(t *testing.T) {
	s := unfoldUpstream(t, busySearch)
	sections := 0
	requests, err := s.StreamCited(context.Background(), english,
		[]bibleref.Ref{{Book: 43, Chapter: 3, VerseStart: 16, VerseEnd: 16}}, nil,
		UnfoldStream{Section: func(UnfoldSection) { sections++ }})
	if err == nil || !strings.HasPrefix(err.Error(), citedFailed) || sections != 0 {
		t.Fatalf("err = %v, sections = %d; want the failure worded for the reader", err, sections)
	}
	var failure citedFailure
	if !errors.As(err, &failure) || errors.Unwrap(err) == nil {
		t.Errorf("the cause is lost: %#v", err)
	}
	if requests < 1 {
		t.Errorf("requests = %d: the failed search was asked all the same", requests)
	}
	// in the passage's language
	_, err = s.StreamCited(context.Background(), model.Language{Symbol: "X", Locale: "de"},
		[]bibleref.Ref{{Book: 43, Chapter: 3, VerseStart: 16, VerseEnd: 16}}, nil,
		UnfoldStream{Section: func(UnfoldSection) {}})
	if err == nil || !strings.HasPrefix(err.Error(), "Die Zitate konnten nicht aufgelistet werden") {
		t.Errorf("German: err = %v", err)
	}
}

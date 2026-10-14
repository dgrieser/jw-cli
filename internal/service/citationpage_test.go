package service

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/dgrieser/jw-cli/internal/bibleref"
)

// citationPages is wol as far as opening a footnote or a citation on a page
// of its own reads it: the recorded footnotes of John (a book of the bible)
// and of a book chapter, the book of John as the document holding verse 18's
// marker, and a citation of a publication asked for without a locale.
func citationPages(t *testing.T, mux *http.ServeMux) {
	serve := func(path string, body []byte) {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write(body)
		})
	}
	serve("/wol/fn/r10/lp-x/1001070147/8", wolFile(t, "footnote_bible.json"))
	serve("/wol/fn/r10/lp-x/1102025912/0", wolFile(t, "footnote_article.json"))
	mux.HandleFunc("/de/wol/d/r10/lp-x/1001070147", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html><body><article id="article" class="document bible bibleBook"><div class="bodyTxt">` +
			`<p><span id="v43-3-17-1" class="v">Gott sandte seinen Sohn nicht in die Welt.</span>` +
			`<span id="v43-3-18-1" class="v">Wer an ihn glaubt<a data-fnid="9" class="fn" href="/de/wol/fn/r10/lp-x/1001070147/8">*</a>, ` +
			`wird nicht verurteilt.</span></p></div></article></body></html>`))
	})
	serve("/wol/pc/r1/lp-e/1102026209/13/0", []byte(`{"items":[{"title":"Comfort",`+
		`"content":"<p>Jehovah is close.</p>","url":"/wol/d/r1/lp-e/2024681#h=13:0-14:0"}]}`))
}

// A footnote of the bible leads to the verse carrying its marker, in the
// bible: the answer names only the book's document, which says where the
// marker is.
func TestCitationPageOfABibleFootnote(t *testing.T) {
	s := unfoldUpstream(t, func(mux *http.ServeMux) { citationPages(t, mux) })
	refs, doc, err := s.CitationPage(context.Background(), english, "/de/wol/fn/r10/lp-x/1001070147/8")
	if err != nil {
		t.Fatal(err)
	}
	want := bibleref.Ref{Book: 43, Chapter: 3, VerseStart: 18, VerseEnd: 18}
	if len(refs) != 1 || refs[0] != want || doc != "" {
		t.Errorf("refs = %v, doc = %q; want John 3:18 in the bible", refs, doc)
	}
}

// A footnote of an article leads to the article, in the locale and the
// library its link names.
func TestCitationPageOfAnArticleFootnote(t *testing.T) {
	s := unfoldUpstream(t, func(mux *http.ServeMux) { citationPages(t, mux) })
	refs, doc, err := s.CitationPage(context.Background(), english, "/de/wol/fn/r10/lp-x/1102025912/0")
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 0 || !strings.HasSuffix(doc, "/de/wol/d/r10/lp-x/1102025912") || !strings.HasPrefix(doc, "http") {
		t.Errorf("refs = %v, doc = %q; want the article", refs, doc)
	}
}

// A citation written without a locale, the way wol's own endpoints write it,
// leads to its document in the locale of the request: without one, the
// document's link leads nowhere.
func TestCitationPageWithoutALocale(t *testing.T) {
	s := unfoldUpstream(t, func(mux *http.ServeMux) { citationPages(t, mux) })
	_, doc, err := s.CitationPage(context.Background(), english, "/wol/pc/r1/lp-e/1102026209/13/0")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(doc, "/en/wol/d/r1/lp-e/2024681") {
		t.Errorf("doc = %q, want the document in the request's locale", doc)
	}
}

// A marker the document does not hold leads to the book's first chapter
// rather than to the book as an article.
func TestFootnoteVerseFallsBackToTheChapter(t *testing.T) {
	doc := `<p><span id="v43-1-1-1" class="v">In the beginning</span></p>`
	ref, ok := footnoteVerse(doc, "/de/wol/fn/r10/lp-x/1001070147/8")
	if !ok || ref != (bibleref.Ref{Book: 43, Chapter: 1}) {
		t.Errorf("ref = %v, %v; want John 1", ref, ok)
	}
	if _, ok := footnoteVerse(`<p>no bible text</p>`, "/de/wol/fn/r10/lp-x/1/8"); ok {
		t.Error("a document without verses has no verse to lead to")
	}
}

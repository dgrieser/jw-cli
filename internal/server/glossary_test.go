package server_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/dgrieser/jw-cli/internal/httpx"
	"github.com/dgrieser/jw-cli/internal/server"
	"github.com/dgrieser/jw-cli/internal/service"
)

// glossaryMux is a library with two of the glossaries: the study bible's,
// split into letter tabs besides its introduction, and Insight, a shelf of
// letters. The Research Guide cannot be read.
func glossaryMux(t *testing.T) *http.ServeMux {
	mux := wolMux(t)
	card := func(href, title string) string {
		return fmt.Sprintf(`<li class="row card"><a class="cardContainer" href="%s"><div class="cardLine1">%s</div></a></li>`, href, title)
	}
	page := func(title, tabs string, cards ...string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintf(w, `<div id="article"><h1>%s</h1><ul class="directory pageList">%s</ul><ul class="directory">%s</ul></div>`,
				title, tabs, strings.Join(cards, ""))
		}
	}
	tab := func(href, title string) string {
		return fmt.Sprintf(`<li class="viewPage"><a href="%s"><span class="title">%s</span></a></li>`, href, title)
	}
	// the same library in English and in German, its entries the same
	// documents in both
	for _, l := range []struct{ loc, r, lp, insight, glossary, intro, abyss, aram, night, jehovah string }{
		{"en", "r1", "lp-e", "Insight", "Glossary", "INTRODUCTION", "Abyss", "Aram; Aramaeans", "Night", "Jehovah"},
		{"de", "r10", "lp-x", "Einsichten", "Worterklärungen", "Einführung", "Abgrund", "Aram; Aramäer", "Nacht", "Jehova"},
	} {
		lib := "/" + l.loc + "/wol/library/" + l.r + "/" + l.lp
		pub := "/" + l.loc + "/wol/publication/" + l.r + "/" + l.lp
		doc := "/" + l.loc + "/wol/d/" + l.r + "/" + l.lp + "/"
		mux.HandleFunc(lib, page("Publications", "", card(lib+"/all-publications/insight", l.insight)))
		mux.HandleFunc(lib+"/all-publications/insight", page(l.insight, "", card(lib+"/all-publications/insight/a", "A")))
		mux.HandleFunc(lib+"/all-publications/insight/a", page("A", "",
			card(doc+"1200000005", "Aaron"),
			card(doc+"1200000007", l.abyss),
			card(doc+"1200000009", "Amos, Book of"),
			card(doc+"1200003253", l.night),
			card(doc+"1200002300", l.jehovah)))
		tabs := tab(pub+"/nwtstg/0", l.intro) + tab(pub+"/nwtstg/4", "A")
		mux.HandleFunc(pub+"/nwtstg", page(l.glossary, tabs, card(doc+"1001077000", l.intro)))
		mux.HandleFunc(pub+"/nwtstg/0", page(l.glossary, tabs, card(doc+"1001077000", l.intro)))
		mux.HandleFunc(pub+"/nwtstg/4", page(l.glossary, tabs,
			card(doc+"1001077007", l.abyss),
			card(doc+"1001077021", l.aram)))
	}
	mux.HandleFunc("/de", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<a href="/de/wol/h/r10/lp-x">home</a>`))
	})
	return mux
}

// The glossaries' tables of contents are read into an index of the words
// they explain, the study bible's glossary first; one that cannot be read is
// left out, and the index said to be partial. A word of every day is left out
// too.
func TestAPIGlossary(t *testing.T) {
	srv := newTestServer(t, glossaryMux(t))
	resp, body := get(t, srv, "/api/v1/glossary?lang=en")
	if resp.StatusCode != 200 {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
	var g service.Glossary
	if err := json.Unmarshal([]byte(body), &g); err != nil {
		t.Fatal(err)
	}
	if g.Lang != "E" || g.CaseSensitive || !g.Partial || !strings.HasSuffix(g.DocURL, "/en/wol/d/r1/lp-e/") {
		t.Errorf("glossary = %+v", g)
	}
	wantBooks := []service.GlossaryBook{{ID: "nwtstg", Title: "Glossary"}, {ID: "it", Title: "Insight"}}
	if !reflect.DeepEqual(g.Books, wantBooks) {
		t.Errorf("books = %+v, want %+v", g.Books, wantBooks)
	}
	want := []service.GlossaryTerm{
		{Term: "Aaron", Book: 1, DocID: 1200000005},
		{Term: "Abyss", Book: 0, DocID: 1001077007},
		{Term: "Abyss", Book: 1, DocID: 1200000007},
		{Term: "Amos", Book: 1, DocID: 1200000009, Title: "Amos, Book of"},
		{Term: "Aram", Book: 0, DocID: 1001077021, Title: "Aram; Aramaeans"},
		{Term: "Aramaeans", Book: 0, DocID: 1001077021, Title: "Aram; Aramaeans"},
		{Term: "Jehovah", Book: 1, DocID: 1200002300},
	}
	if !reflect.DeepEqual(g.Terms, want) {
		t.Errorf("terms = %+v\nwant %+v", g.Terms, want)
	}
	if !reflect.DeepEqual(g.Common, []int{1200003253}) {
		t.Errorf("common = %v", g.Common)
	}
}

// Another language leaves out the entries the English index names as words
// of every day: the same documents ("Nacht" is the "Night" of English).
func TestAPIGlossaryCommonInGerman(t *testing.T) {
	srv := newTestServer(t, glossaryMux(t))
	resp, body := get(t, srv, "/api/v1/glossary?lang=de")
	if resp.StatusCode != 200 {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
	var g service.Glossary
	if err := json.Unmarshal([]byte(body), &g); err != nil {
		t.Fatal(err)
	}
	var terms []string
	for _, t := range g.Terms {
		terms = append(terms, t.Term)
	}
	want := []string{"Aaron", "Abgrund", "Abgrund", "Amos", "Aram", "Aramäer", "Jehova"}
	if !g.CaseSensitive || !reflect.DeepEqual(terms, want) || !reflect.DeepEqual(g.Common, []int{1200003253}) {
		t.Errorf("terms = %q, common = %v, want %q without Nacht", terms, g.Common, want)
	}
}

// A language whose glossaries are not indexed has no terms, and nothing is
// read for it.
func TestAPIGlossaryOff(t *testing.T) {
	mux := glossaryMux(t)
	up := httptest.NewServer(mux)
	t.Cleanup(up.Close)
	hc := httpx.New(httpx.WithBaseURLs(httpx.BaseURLs{CDN: up.URL, JWOrg: up.URL, WOL: up.URL}),
		httpx.WithRetryDelays(time.Millisecond, time.Millisecond))
	svc := service.New(hc, httpx.OpenCacheAt(t.TempDir()))
	srv := httptest.NewServer(server.New(server.Config{Svc: svc, GlossaryLangs: []string{}}).Handler())
	t.Cleanup(srv.Close)
	resp, body := get(t, srv, "/api/v1/glossary?lang=en")
	if resp.StatusCode != 200 {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
	var g service.Glossary
	if err := json.Unmarshal([]byte(body), &g); err != nil {
		t.Fatal(err)
	}
	if len(g.Terms) != 0 || len(g.Books) != 0 {
		t.Errorf("a language not indexed: %+v", g)
	}
}

// The server marks the words of the index in the pages it sends and in the
// sections it streams: a link to the entry, naming its glossary.
func TestGlossaryMarkedInPages(t *testing.T) {
	srv := newTestServer(t, glossaryMux(t))
	// the index is read first; a page asked for before it is read is sent
	// unmarked, and reads it in the background
	if resp, body := get(t, srv, "/api/v1/glossary?lang=en"); resp.StatusCode != 200 {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
	_, body := get(t, srv, "/article?target=2024360&lang=en")
	if !strings.Contains(body, `class="gloss"`) || !strings.Contains(body, `data-gloss-book="Insight" title="Insight">Jehovah</a>`) {
		t.Errorf("the page does not mark Jehovah:\n%s", body)
	}
	_, body = get(t, srv, "/unfold/article?lang=en&url=https%3A%2F%2Fwol.jw.org%2Fen%2Fwol%2Fd%2Fr1%2Flp-e%2F2024360")
	if !strings.Contains(body, `title=\"Insight\"\u003eJehovah\u003c/a\u003e`) && !strings.Contains(body, `title=\"Insight\">Jehovah</a>`) {
		t.Errorf("the streamed section does not mark Jehovah:\n%s", body)
	}
}

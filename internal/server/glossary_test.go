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
	const lib, pub = "/en/wol/library/r1/lp-e", "/en/wol/publication/r1/lp-e"
	mux.HandleFunc(lib, page("Publications", "", card(lib+"/all-publications/insight", "Insight")))
	mux.HandleFunc(lib+"/all-publications/insight", page("Insight", "", card(lib+"/all-publications/insight/a", "A")))
	mux.HandleFunc(lib+"/all-publications/insight/a", page("A", "",
		card("/en/wol/d/r1/lp-e/1200000005", "Aaron"),
		card("/en/wol/d/r1/lp-e/1200000007", "Abyss"),
		card("/en/wol/d/r1/lp-e/1200000009", "Amos, Book of")))
	tabs := tab(pub+"/nwtstg/0", "INTRODUCTION") + tab(pub+"/nwtstg/4", "A")
	mux.HandleFunc(pub+"/nwtstg", page("Glossary", tabs, card("/en/wol/d/r1/lp-e/1001077000", "Introduction")))
	mux.HandleFunc(pub+"/nwtstg/0", page("Glossary", tabs, card("/en/wol/d/r1/lp-e/1001077000", "Introduction")))
	mux.HandleFunc(pub+"/nwtstg/4", page("Glossary", tabs,
		card("/en/wol/d/r1/lp-e/1001077007", "Abyss"),
		card("/en/wol/d/r1/lp-e/1001077021", "Aram; Aramaeans")))
	return mux
}

// The glossaries' tables of contents are read into an index of the words
// they explain, the study bible's glossary first; one that cannot be read is
// left out, and the index said to be partial.
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
	}
	if !reflect.DeepEqual(g.Terms, want) {
		t.Errorf("terms = %+v\nwant %+v", g.Terms, want)
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

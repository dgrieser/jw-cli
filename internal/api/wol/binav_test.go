package wol

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

var cfgDE = Config{Locale: "de", Rsconf: "r10", Lp: "lp-x"}

func binavClient(t *testing.T) *Client {
	mux := http.NewServeMux()
	mux.HandleFunc("/de/wol/binav/r10/lp-x/nwtsty", serveFile(t, "testdata/binav_de.html"))
	mux.HandleFunc("/de/wol/binav/r10/lp-x/nwtsty/40", serveFile(t, "testdata/binav_de_40.html"))
	return testClient(t, mux)
}

func TestBibleNav(t *testing.T) {
	nav, err := binavClient(t).BibleNav(context.Background(), cfgDE, "nwtsty")
	if err != nil {
		t.Fatal(err)
	}
	if nav.Title != "Die Bibel. Neue-Welt-Übersetzung (Studienausgabe)" {
		t.Errorf("title = %q", nav.Title)
	}
	if len(nav.Sections) != 2 {
		t.Fatalf("sections = %d", len(nav.Sections))
	}
	heb, gr := nav.Sections[0], nav.Sections[1]
	if heb.Key != "hebrew" || len(heb.Books) != 39 || heb.Heading != "HEBRÄISCH-ARAMÄISCHE SCHRIFTEN" {
		t.Errorf("hebrew = %q %q %d books", heb.Key, heb.Heading, len(heb.Books))
	}
	if gr.Key != "greek" || len(gr.Books) != 27 || gr.Heading != "CHRISTLICHE GRIECHISCHE SCHRIFTEN" {
		t.Errorf("greek = %q %q %d books", gr.Key, gr.Heading, len(gr.Books))
	}
	want := NavBook{Number: 40, Name: "Matthäus", Abbreviation: "Mat.", Official: "Mat", Group: "gospels", Study: true}
	if gr.Books[0] != want {
		t.Errorf("matthew = %+v", gr.Books[0])
	}
	if b := heb.Books[0]; b.Number != 1 || b.Name != "1. Mose" || b.Group != "pentateuch" || b.Study {
		t.Errorf("genesis = %+v", b)
	}

	// what the page lists around the grid: the bible's parts above it, the
	// books the one selected; its gallery and other reference works below
	var tabs []string
	for _, tab := range nav.Tabs {
		tabs = append(tabs, tab.Title+"="+tab.Path)
		if tab.Selected != (tab.Path == "nwtsty/26") {
			t.Errorf("tab %+v: selected wrong", tab)
		}
	}
	if got := strings.Join(tabs, " "); got != "EINFÜHRUNG=nwtsty/0 BÜCHER=nwtsty/26 INDEX=nwtsty/95 ANHANG A=nwtsty/98 ANHANG B=nwtsty/117 ANHANG C=nwtsty/140" {
		t.Errorf("tabs = %s", got)
	}
	if g := nav.Gallery; g == nil || g.Title != "Mediengalerie" || !strings.HasSuffix(g.URL, "/de/wol/gallery/r10/lp-x/nwtsty") {
		t.Errorf("gallery = %+v", g)
	}
	if nav.RefsHeading != "Weitere Nachschlagewerke" || len(nav.Refs) != 2 {
		t.Fatalf("refs = %q %+v", nav.RefsHeading, nav.Refs)
	}
	if r := nav.Refs[0]; r.Kind != PublicationKind || r.Path != "nwtstg" || r.Title != "Worterklärungen zur Bibel" || r.Thumbnail == "" {
		t.Errorf("glossary = %+v", r)
	}
	if r := nav.Refs[1]; r.Path != "scl" || r.Title != "Bibelverse für das Leben als Christ" {
		t.Errorf("scriptures for christian living = %+v", r)
	}
}

// The gallery of a whole bible lists its books' galleries, under the part of
// the bible they are in.
func TestBibleGallery(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/de/wol/gallery/r10/lp-x/nwtsty", serveFile(t, "testdata/gallery_nwtsty_de.html"))
	c := testClient(t, mux)
	g, err := c.Gallery(context.Background(), c.hc.Base.WOL+"/de/wol/gallery/r10/lp-x/nwtsty")
	if err != nil {
		t.Fatal(err)
	}
	if g.Title != "Mediengalerie" || len(g.Groups) != 1 {
		t.Fatalf("gallery = %q, %d groups", g.Title, len(g.Groups))
	}
	grp := g.Groups[0]
	if grp.Heading != "CHRISTLICHE GRIECHISCHE SCHRIFTEN" || len(grp.Items) != 4 {
		t.Fatalf("group = %+v", grp)
	}
	if m := grp.Items[0]; m.Title != "Matthäus" || !strings.HasSuffix(m.URL, "/nwtsty/40") || !strings.HasSuffix(m.Thumbnail, "/nwtsty/40/thumbnail") {
		t.Errorf("matthew = %+v", m)
	}
}

func TestBookNav(t *testing.T) {
	nav, err := binavClient(t).BookNav(context.Background(), cfgDE, "nwtsty", 40)
	if err != nil {
		t.Fatal(err)
	}
	if nav.Title != "Das Evangelium nach Matthäus" {
		t.Errorf("title = %q", nav.Title)
	}
	if len(nav.Chapters) != 28 || nav.Chapters[0] != 1 || nav.Chapters[27] != 28 {
		t.Errorf("chapters = %v", nav.Chapters)
	}
	// the documents below the chapters, told apart by their address
	var kinds []string
	for _, l := range nav.Links {
		kinds = append(kinds, l.Kind)
		if l.Title == "" || !strings.HasPrefix(l.URL, "http") {
			t.Errorf("link = %+v", l)
		}
	}
	if got := strings.Join(kinds, ","); got != "introduction,overview,gallery" {
		t.Errorf("link kinds = %q", got)
	}
}

func TestBookLinkKind(t *testing.T) {
	for href, want := range map[string]string{
		"/en/wol/bibledocument/r1/lp-e/nwtsty/1/introduction": BookIntroduction,
		"/en/wol/bibledocument/r1/lp-e/nwtsty/1/outline":      BookOutline,
		"/en/wol/bibledocument/r1/lp-e/nwtsty/40/overview":    BookOverview,
		"/en/wol/gallery/r1/lp-e/nwtsty/40":                   BookGallery,
		"/en/wol/b/r1/lp-e/nwtsty/40/1":                       "",
	} {
		if got := bookLinkKind(href); got != want {
			t.Errorf("%s: got %q, want %q", href, got, want)
		}
	}
}

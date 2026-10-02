package wol

import (
	"context"
	"net/http"
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
}

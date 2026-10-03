package wol

import (
	"context"
	"net/http"
	"slices"
	"testing"
)

func libraryClient(t *testing.T) *Client {
	mux := http.NewServeMux()
	mux.HandleFunc("/en/wol/library/r1/lp-e", serveFile(t, "testdata/library_en.html"))
	mux.HandleFunc("/en/wol/library/r1/lp-e/all-publications/watchtower/the-watchtower-2024/study-edition/may",
		serveFile(t, "testdata/library_w_202405.html"))
	mux.HandleFunc("/en/wol/publication/r1/lp-e/lff", serveFile(t, "testdata/publication_lff.html"))
	mux.HandleFunc("/en/wol/publication/r1/lp-e/nwt", serveFile(t, "testdata/publication_nwt.html"))
	return testClient(t, mux)
}

func TestLibraryRoot(t *testing.T) {
	page, err := libraryClient(t).Library(context.Background(), cfgEN, LibraryKind, "")
	if err != nil {
		t.Fatal(err)
	}
	cards := page.Cards()
	if len(cards) < 10 {
		t.Fatalf("cards = %d", len(cards))
	}
	if c := cards[0]; c.Kind != LibraryKind || c.Title != "Bibles" || c.Path != "all-publications/bibles" {
		t.Errorf("first card = %+v", c)
	}
	if page.Parent != nil || page.Symbol != "" {
		t.Errorf("root page has a parent or a symbol: %+v", page)
	}
}

func TestLibraryIssue(t *testing.T) {
	page, err := libraryClient(t).Library(context.Background(), cfgEN, LibraryKind,
		"/all-publications/watchtower/the-watchtower-2024/study-edition/may/")
	if err != nil {
		t.Fatal(err)
	}
	if page.Title != "May" || page.Symbol != "w" || page.Issue != "202405" {
		t.Errorf("page = %q %q %q", page.Title, page.Symbol, page.Issue)
	}
	if page.Path != "all-publications/watchtower/the-watchtower-2024/study-edition/may" {
		t.Errorf("path = %q", page.Path)
	}
	if p := page.Parent; p == nil || p.Kind != LibraryKind || p.Path != "all-publications/watchtower/the-watchtower-2024/study-edition" {
		t.Errorf("parent = %+v", p)
	}
	cards := page.Cards()
	var article *LibraryCard
	for i := range cards {
		if cards[i].DocID == 2024404 {
			article = &cards[i]
		}
	}
	if article == nil {
		t.Fatalf("no study article 18 in %+v", cards)
	}
	if article.Kind != DocumentKind || article.Title != "STUDY ARTICLE 18" ||
		article.Subtitle != "Trust in the Merciful “Judge of All the Earth”!" ||
		article.Thumbnail == "" {
		t.Errorf("article = %+v", *article)
	}
}

func TestLibraryPublication(t *testing.T) {
	page, err := libraryClient(t).Library(context.Background(), cfgEN, PublicationKind, "lff")
	if err != nil {
		t.Fatal(err)
	}
	if page.Symbol != "lff" || page.Issue != "" || page.Year != "2021" || page.Bible {
		t.Errorf("page = %+v", page)
	}
	if page.Title != "Enjoy Life Forever!—An Interactive Bible Course" {
		t.Errorf("title = %q", page.Title)
	}
	if len(page.Tabs) != 5 || !page.Tabs[0].Selected || page.Tabs[1].Path != "lff/21" || page.Tabs[1].Title != "SECTION 2" {
		t.Errorf("tabs = %+v", page.Tabs)
	}
	var titles []string
	for _, g := range page.Groups {
		titles = append(titles, g.Title)
	}
	if len(page.Groups) < 2 || page.Groups[0].Title != "" || !slices.Contains(titles, "LESSONS") {
		t.Errorf("groups = %q", titles)
	}
	if p := page.Parent; p == nil || p.Path != "all-publications/books" {
		t.Errorf("parent = %+v", p)
	}

	nwt, err := libraryClient(t).Library(context.Background(), cfgEN, PublicationKind, "nwt")
	if err != nil {
		t.Fatal(err)
	}
	if !nwt.Bible || nwt.Symbol != "nwt" {
		t.Errorf("nwt = %+v", nwt)
	}
}

func TestCardLink(t *testing.T) {
	for href, want := range map[string]LibraryCard{
		"/de/wol/library/r10/lp-x/alle-publikationen/worterkl%C3%A4rungen": {Kind: LibraryKind, Path: "alle-publikationen/worterklärungen"},
		"/en/wol/publication/r1/lp-e/lff/21":                               {Kind: PublicationKind, Path: "lff/21"},
		"/en/wol/d/r1/lp-e/2024404":                                        {Kind: DocumentKind, DocID: 2024404},
		"/en/wol/b/r1/lp-e/nwtsty/43/3":                                    {Kind: "link"},
	} {
		got := cardLink(href, "https://wol.jw.org")
		got.URL = ""
		if got != want {
			t.Errorf("cardLink(%q) = %+v, want %+v", href, got, want)
		}
	}
}

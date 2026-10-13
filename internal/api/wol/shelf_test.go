package wol

import (
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

// A document names the library page it is filed under in its navigation.
func TestParseDocumentShelf(t *testing.T) {
	html := `<ul><li class="resultsNavigationSelected documentLocation navPublications">` +
		`<a href="/de/wol/library/r10/lp-x/alle-publikationen/einsichten/j">it-1</a></li></ul>` +
		`<article id="article"><p>Text</p></article>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatal(err)
	}
	art := parseDocument(doc, "https://wol.jw.org")
	if art.ShelfKind != LibraryKind || art.ShelfPath != "alle-publikationen/einsichten/j" {
		t.Errorf("shelf = %q %q", art.ShelfKind, art.ShelfPath)
	}
}

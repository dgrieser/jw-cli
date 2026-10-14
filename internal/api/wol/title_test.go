package wol

import (
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

// pageWith is a wol page as far as its title goes: the <title>, the banner
// naming the library, and an article.
func pageWith(t *testing.T, title, site, article string) *goquery.Document {
	t.Helper()
	page := `<html><head><title>` + title + `</title></head><body>` +
		`<header><span class="ellipsized titleBig">` + site + `</span></header>` +
		`<article id="article">` + article + `</article></body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(page))
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

// A document is titled with its own heading; a page without one with its
// <title>, less the library's name the site appends to it in every language.
func TestDocumentTitle(t *testing.T) {
	for _, c := range []struct {
		title, site, article, want string
	}{
		{"Bush &mdash; Watchtower ONLINE LIBRARY", "Watchtower ONLINE LIBRARY",
			`<header><h1 id="p1"><span class="pageNum" data-no="380"></span><strong>BUSH</strong></h1></header><p>A low shrub.</p>`, "BUSH"},
		// the meetings page has no heading of its own
		{"October 12-18 &mdash; Watchtower ONLINE LIBRARY", "Watchtower ONLINE LIBRARY",
			`<div id="materialNav"><h2>Life and Ministry</h2></div>`, "October 12-18"},
		{"12.–18. Oktober &mdash; Wachtturm ONLINE-BIBLIOTHEK", "Wachtturm ONLINE-BIBLIOTHEK ",
			`<h2>Leben und Dienst</h2>`, "12.–18. Oktober"},
		{"Sin Langchyi ONLINE LAIKA DUM", "Sin Langchyi ONLINE LAIKA DUM", `<p>x</p>`, ""},
		// without the banner, the name is what follows the last dash
		{"A Day &mdash; Watchtower ONLINE LIBRARY", "", `<p>x</p>`, "A Day"},
	} {
		art := parseDocument(pageWith(t, c.title, c.site, c.article), "https://wol.jw.org")
		if art.Title != c.want {
			t.Errorf("%q: title = %q, want %q", c.title, art.Title, c.want)
		}
	}
}

// What a page carries for its scripts is no part of the document's HTML:
// the image viewer's lightbox, the hidden fields, comments. A study article's
// answer field is.
func TestDocumentDropsScriptChrome(t *testing.T) {
	article := `<h1>Caleb</h1><p>Question?</p>
<div class="gen-field"><label for="tt12">Your answer</label><textarea id="tt12"></textarea></div>
<!-- Root element of lightbox -->
<div class="pswp" role="dialog"><button class="pswp__button" title="Close (Esc)"></button></div>
<input type="hidden" id="shareBaseUrl" value="https://www.jw.org/finder?wtlocale=E&docid=1102025912"/>
<!--THE END-->`
	art := parseDocument(pageWith(t, "Caleb &mdash; Watchtower ONLINE LIBRARY", "Watchtower ONLINE LIBRARY", article),
		"https://wol.jw.org")
	for _, gone := range []string{"pswp", "<input", "<button", "<!--", "shareBaseUrl"} {
		if strings.Contains(art.HTML, gone) {
			t.Errorf("document keeps %q:\n%s", gone, art.HTML)
		}
	}
	if !strings.Contains(art.HTML, "<textarea") || !strings.Contains(art.HTML, "Question?") {
		t.Errorf("document lost its own content:\n%s", art.HTML)
	}
}

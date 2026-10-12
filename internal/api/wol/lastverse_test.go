package wol

import (
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

func TestLastVerse(t *testing.T) {
	html := `<span class="v" id="v64-1-1-1">a</span><span class="v" id="v64-1-14-1">b</span>` +
		`<span class="v" id="v64-1-14-2">c</span><span class="v" id="v64-2-1-1">next chapter</span>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatal(err)
	}
	d := &ChapterDoc{doc: doc, Book: 64, Chapter: 1}
	if got := d.LastVerse(); got != 14 {
		t.Errorf("LastVerse = %d, want 14", got)
	}
}

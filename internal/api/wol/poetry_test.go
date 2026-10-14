package wol

import (
	"os"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

// chapterFixture parses a captured chapter page the way Chapter would.
func chapterFixture(t *testing.T, path string, book, chapter int) *ChapterDoc {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	doc, err := goquery.NewDocumentFromReader(f)
	if err != nil {
		t.Fatal(err)
	}
	return &ChapterDoc{doc: doc, Book: book, Chapter: chapter}
}

// Every line of a verse of poetry is a paragraph of its own on the page; read
// as one verse, the lines are joined with a line break instead of being run
// together ("righteous.None of them").
func TestVersesKeepPoetryLines(t *testing.T) {
	d := chapterFixture(t, "testdata/chapter_pr8.html", 20, 8)
	verses, err := d.Verses(8, 9)
	if err != nil {
		t.Fatal(err)
	}
	if len(verses) != 2 {
		t.Fatalf("got %d verses, want 2", len(verses))
	}
	if !strings.Contains(verses[0].HTML, "are righteous."+lineBreak+"None of them") {
		t.Errorf("Pr 8:8 lines not broken: %s", verses[0].HTML)
	}
	if !strings.Contains(verses[1].HTML, "to the discerning"+lineBreak+"And right") {
		t.Errorf("Pr 8:9 lines not broken: %s", verses[1].HTML)
	}
	if strings.HasPrefix(strings.TrimSpace(verses[0].HTML), lineBreak) {
		t.Errorf("a verse does not open with a break: %s", verses[0].HTML)
	}
	for v := 6; v <= 10; v++ {
		if !d.StartsLine(v) {
			t.Errorf("Pr 8:%d starts a line of poetry", v)
		}
	}
}

// Prose keeps running on: verses that share a paragraph are not broken apart,
// nor is the paragraph after a paragraph of prose. A verse running from prose
// into poetry breaks where the poetry begins, and the prose after the poetry
// starts a line again.
func TestVersesProseAndPoetry(t *testing.T) {
	d := chapterFixture(t, "testdata/chapter_ge2.html", 1, 2)
	verses, err := d.Verses(21, 25)
	if err != nil {
		t.Fatal(err)
	}
	if len(verses) != 5 {
		t.Fatalf("got %d verses, want 5", len(verses))
	}
	for _, v := range []int{0, 1, 3, 4} {
		if strings.Contains(verses[v].HTML, lineBreak) {
			t.Errorf("prose verse %d broken: %s", verses[v].ID, verses[v].HTML)
		}
	}
	got := verses[2].HTML
	for _, want := range []string{
		"Then the man said:" + lineBreak + "“This is at last bone of my bones",
		"bone of my bones" + lineBreak + "And flesh of my flesh.",
		"of my flesh." + lineBreak + "This one will be called Woman,",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("Ge 2:23 misses %q: %s", want, got)
		}
	}
	for v, want := range map[int]bool{
		19: false, // in the middle of a paragraph of prose
		22: false, // the end of that paragraph still
		23: false, // a paragraph of prose after one of prose
		24: true,  // the prose after the poetry of verse 23
		25: false, // the paragraph verse 24 opened
	} {
		if got := d.StartsLine(v); got != want {
			t.Errorf("StartsLine(%d) = %v, want %v", v, got, want)
		}
	}
}

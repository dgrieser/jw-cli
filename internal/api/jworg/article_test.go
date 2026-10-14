package jworg

import (
	"os"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

// jw.org marks its bible citations as a.jsBibleLink with the verses in
// data-targetverses; they are the article's scripture references, each the
// full reference even where its link reads only "6:31-34" or "5, 6".
func TestArticleBibleLinks(t *testing.T) {
	f, err := os.Open("testdata/article_bible_links.html")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	doc, err := goquery.NewDocumentFromReader(f)
	if err != nil {
		t.Fatal(err)
	}
	art := parse(doc, "https://www.jw.org")
	if art.Title != "What Is the Kingdom of God?" {
		t.Errorf("title = %q", art.Title)
	}
	want := []string{
		"Matthew 28:18", "Matthew 4:23", "Mark 1:40, 41", "Mark 6:31-34", "Luke 7:11-17", "Revelation 5:9, 10",
		"Isaiah 35:1", "Isaiah 35:5, 6", "Matthew 6:10", "Revelation 21:1-4",
	}
	if len(art.ScriptureRefs) != len(want) {
		t.Fatalf("got %d references, want %d: %+v", len(art.ScriptureRefs), len(want), art.ScriptureRefs)
	}
	for i, w := range want {
		if got := art.ScriptureRefs[i].Text; got != w {
			t.Errorf("reference %d = %q, want %q", i, got, w)
		}
	}
	if got := art.ScriptureRefs[3].BCPath; got != "https://www.jw.org/en/library/bible/study-bible/books/mark/6/#v41006031-v41006034" {
		t.Errorf("link = %q", got)
	}
}

package wol

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// A marginal reference names several verses, and wol answers it with an item
// per verse: every one of them is part of the answer, not only the first.
func TestTooltipKeepsEveryItem(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/wol/bc/r1/lp-e/1001070147/96", serveFile(t, "testdata/citation_multi.json"))
	c := testClient(t, mux)
	tip, err := c.Tooltip(context.Background(), "/en/wol/bc/r1/lp-e/1001070147/96")
	if err != nil {
		t.Fatal(err)
	}
	items := tip.Items()
	var titles []string
	for _, it := range items {
		titles = append(titles, it.Title)
	}
	want := "Genesis 22:2|Genesis 22:16|John 1:14|Romans 5:8|Romans 8:32|1 John 4:9, 10|1 John 4:19"
	if got := strings.Join(titles, "|"); got != want {
		t.Fatalf("items = %s, want %s", got, want)
	}
	if tip.Title != "Genesis 22:2" || len(tip.More) != 6 {
		t.Errorf("the first item is the tooltip itself, the others its More: %q + %d", tip.Title, len(tip.More))
	}
	// each item is localized like the first
	last := items[len(items)-1]
	if !strings.Contains(last.ContentHTML, `id="v62-4-19-1"`) || !strings.Contains(last.ContentHTML, `href="/en/wol/`) {
		t.Errorf("the last item lost its text or its locale: %.300s", last.ContentHTML)
	}
	if !strings.HasSuffix(last.URL, "/en/wol/b/r1/lp-e/nwtsty/62/4#v=62:4:19") {
		t.Errorf("last item url = %s", last.URL)
	}
	if len(items[0].More) != 0 {
		t.Error("an item on its own carries no further items")
	}
}

// Only a link to a document names one: the numbers ending a daily text or a
// week of meetings are a date, and a citation names the document citing it.
func TestDocIDFromURL(t *testing.T) {
	for u, want := range map[string]int{
		"https://wol.jw.org/de/wol/d/r10/lp-x/1102025912":          1102025912,
		"/en/wol/d/r1/lp-e/1102026200?#h=2:0-5:0":                  1102026200,
		"/de/wol/d/r10/lp-x/202026249?q=Jer&p=par":                 202026249,
		"https://www.jw.org/finder?wtlocale=E&docid=1102014204":    1102014204,
		"https://wol.jw.org/en/wol/dt/r1/lp-e/2026/1/1":            0,
		"https://wol.jw.org/en/wol/meetings/r1/lp-e/2026/3":        0,
		"/en/wol/pc/r1/lp-e/1102026209/13/0":                       0,
		"/de/wol/fn/r10/lp-x/1001070147/8":                         0,
		"https://www.jw.org/en/library/magazines/watchtower-2024/": 0,
	} {
		if got := DocIDFromURL(u); got != want {
			t.Errorf("DocIDFromURL(%q) = %d, want %d", u, got, want)
		}
	}
}

func TestDocIDOfClasses(t *testing.T) {
	for classes, want := range map[string]int{
		"todayItem today html5 pub-es docId-1102026200 pub-es26 pub-es": 1102026200,
		"document html5 pub- docId-1001070147 pub-nwtsty bible":         1001070147,
		"docId-7":                        7,
		"bibleCitation html5 pub-nwtsty": 0,
		"xdocId-5 docId-x":               0,
	} {
		if got := DocIDOfClasses(classes); got != want {
			t.Errorf("DocIDOfClasses(%q) = %d, want %d", classes, got, want)
		}
	}
}

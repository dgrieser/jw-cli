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

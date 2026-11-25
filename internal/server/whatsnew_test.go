package server

import (
	"testing"
	"time"

	"github.com/dgrieser/jw-cli/internal/i18n"
	"github.com/dgrieser/jw-cli/internal/model"
)

// The rows of what is new go under the days their entries came out on.
func TestGroupByDay(t *testing.T) {
	items := []model.NewItem{{Published: "2026-11-24"}, {Published: "2026-11-24"}, {Published: "2026-11-23"}}
	views := []resultView{{Title: "a"}, {Title: "b"}, {Title: "c"}}
	tx := i18n.TextFor("de")
	days := groupByDay(views, items, tx)
	if len(days) != 2 || len(days[0].Items) != 2 || days[1].Items[0].Title != "c" || days[1].Items[0].Index != 1 {
		t.Fatalf("days: %+v", days)
	}
	if want := tx.Date(time.Date(2026, 11, 24, 0, 0, 0, 0, latestZone)); days[0].Heading != want {
		t.Errorf("heading %q, want %q", days[0].Heading, want)
	}
}

// The publications' row of the latest holds the publications alone and
// leads to the page of what is new, filtered to them.
func TestLatestPubShelf(t *testing.T) {
	items := []model.NewItem{
		{Kind: "news", Title: "n", URL: "https://www.jw.org/de/nachrichten/x/"},
		{Kind: "publication", Title: "p", URL: "https://www.jw.org/de/bibliothek/p/", ImageURL: "https://img/p.jpg"},
		{Kind: "video", Title: "v", LANK: "pub-x_VIDEO"},
	}
	sh := latestPubShelf(items, "Neueste Publikationen", basePage{Lang: "de"})
	if sh == nil || len(sh.Tiles) != 1 || sh.Tiles[0].Title != "p" || sh.Href != "/latest?kind=pub&lang=de" {
		t.Fatalf("shelf: %+v", sh)
	}
	if latestPubShelf(items[:1], "x", basePage{}) != nil {
		t.Error("a row without publications")
	}
}

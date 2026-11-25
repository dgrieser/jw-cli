package server

import (
	"testing"
	"time"

	"github.com/dgrieser/jw-cli/internal/i18n"
	"github.com/dgrieser/jw-cli/internal/model"
)

// What is new goes by day, newest first; on a day jw.org's entries come
// before the media, and the media go latest first.
func TestGroupByDay(t *testing.T) {
	at := func(s string) time.Time {
		v, err := time.ParseInLocation("2006-01-02 15:04", s, latestZone)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	entries := []latestEntry{
		{at: at("2026-11-23 09:00"), kind: "media", view: resultView{Title: "m1"}},
		{at: at("2026-11-24 00:00"), kind: "pub", view: resultView{Title: "p1"}},
		{at: at("2026-11-24 18:00"), kind: "media", view: resultView{Title: "m2"}},
		{at: at("2026-11-24 08:00"), kind: "media", view: resultView{Title: "m3"}},
		{at: at("2026-11-23 00:00"), kind: "news", view: resultView{Title: "n1"}},
	}
	tx := i18n.TextFor("de")
	days := groupByDay(entries, tx)
	if len(days) != 2 || days[0].Heading != tx.Date(at("2026-11-24 00:00")) {
		t.Fatalf("days: %+v", days)
	}
	var got [][]string
	for _, d := range days {
		var titles []string
		for _, v := range d.Items {
			titles = append(titles, v.Title)
		}
		got = append(got, titles)
	}
	want := [][]string{{"p1", "m2", "m3"}, {"n1", "m1"}}
	for i := range want {
		if len(got[i]) != len(want[i]) {
			t.Fatalf("got %v, want %v", got, want)
		}
		for j := range want[i] {
			if got[i][j] != want[i][j] {
				t.Fatalf("got %v, want %v", got, want)
			}
		}
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

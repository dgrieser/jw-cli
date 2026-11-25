package service

import (
	"slices"
	"testing"

	"github.com/dgrieser/jw-cli/internal/model"
)

// What is new goes newest day first; on a day jw.org's entries come first in
// their order, then the media, latest first. A video both list comes once,
// and a medium without a date not at all.
func TestLatestItems(t *testing.T) {
	news := []model.NewItem{
		{Kind: "publication", Title: "p1", Published: "2026-11-24"},
		{Kind: "news", Title: "n1", Published: "2026-11-23"},
		{Kind: "video", Title: "dup", Published: "2026-11-23", LANK: "pub-x_VIDEO"},
	}
	media := []model.MediaItem{
		{Type: "video", Title: "m2", LANK: "pub-m2_VIDEO", FirstPublished: "2026-11-24T17:00:00Z"},
		{Type: "audio", Title: "m3", LANK: "pub-m3_AUDIO", FirstPublished: "2026-11-24T07:00:00Z"},
		{Type: "audio", Title: "x", LANK: "pub-x_AUDIO", FirstPublished: "2026-11-23T08:00:00Z"},
		{Type: "video", Title: "undated", LANK: "pub-u_VIDEO"},
	}
	var got []string
	for _, it := range LatestItems(news, media) {
		got = append(got, it.Title)
	}
	if want := []string{"p1", "m2", "m3", "n1", "x"}; !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// A medium's row is known by its key, everything else by its page.
func TestNewItemsToResults(t *testing.T) {
	rs := NewItemsToResults([]model.NewItem{
		{Kind: "video", Title: "v", Published: "2026-11-24", LANK: "pub-v_VIDEO"},
		{Kind: "news", Title: "n", Published: "2026-11-23", Context: "NEWS", URL: "https://www.jw.org/en/news/x/"},
	})
	if rs[0].LANK != "pub-v_VIDEO" || rs[0].JWLink != "" || rs[0].Context != "2026-11-24" {
		t.Errorf("medium: %+v", rs[0])
	}
	if rs[1].JWLink != "https://www.jw.org/en/news/x/" || rs[1].Context != "2026-11-23 · NEWS" {
		t.Errorf("news: %+v", rs[1])
	}
}

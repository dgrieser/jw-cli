package search

import (
	"encoding/json"
	"testing"
)

// Three hits as the search answered "love ministry": a series, a category
// without a link of its own, and a video filed in a category — which is the
// video.
const categoryHits = `[
  {"type": "item", "subtype": "videoCategory", "title": "Love the Ministry", "context": "Series",
   "lank": "mc-SeriesLoveMinistry",
   "links": {"jw.org": "https://www.jw.org/open?docid=1011214&category=SeriesLoveMinistry&wtlocale=E&appLanguage=E&prefer=content"},
   "image": {"url": "https://b.jw-cdn.org/images/jwb-categories/cat-series/series_univ_lss_lg.png"}},
  {"type": "item", "subtype": "audioCategory", "title": "Songs", "lank": "mc-AudioOriginalSongs", "links": {}},
  {"type": "item", "subtype": "video", "title": "Love the Ministry—Responsibility", "lank": "pub-jwb-141_6_VIDEO",
   "links": {"jw.org": "https://www.jw.org/open?docid=1011214&item=pub-jwb-141_6_VIDEO&category=SeriesLoveMinistry&wtlocale=E"}}
]`

// A series is a category to browse, not an article to read: the CLI and the
// web UI lead to the category by its key.
func TestCategoryHits(t *testing.T) {
	var wire []wireResult
	if err := json.Unmarshal([]byte(categoryHits), &wire); err != nil {
		t.Fatal(err)
	}
	series, songs, video := wire[0].toModel(), wire[1].toModel(), wire[2].toModel()
	if series.Kind != "category" || series.CategoryKey != "SeriesLoveMinistry" || series.LANK != "" {
		t.Errorf("series = %+v", series)
	}
	if series.Title != "Love the Ministry" || series.ImageURL == "" || series.JWLink == "" {
		t.Errorf("series lost its title, picture or link: %+v", series)
	}
	if songs.Kind != "category" || songs.CategoryKey != "AudioOriginalSongs" {
		t.Errorf("songs = %+v", songs)
	}
	if video.Kind != "video" || video.LANK != "pub-jwb-141_6_VIDEO" || video.CategoryKey != "" {
		t.Errorf("video = %+v", video)
	}
}

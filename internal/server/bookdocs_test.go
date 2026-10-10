package server

import (
	"net/url"
	"testing"

	"github.com/dgrieser/jw-cli/internal/model"
)

func TestOutlineRows(t *testing.T) {
	page := &biblePage{Edition: "nwtsty", BookName: "Psalms"}
	rows := outlineRows([]model.OutlineItem{
		{Depth: 0, Title: "Two ways contrasted", Start: 19001000, End: 19001999},
		{Depth: 1, Title: "Happiness", Label: "2", Start: 19001002, End: 19001002},
		{Depth: 0, Title: "Jehovah and his anointed", Start: 19002000, End: 19002999},
		{Depth: 1, Title: "Honor the son", Label: "10-12", Start: 19002010, End: 19002012},
		{Depth: 0, Title: "Across", Label: "2:12–3:2", Start: 19002012, End: 19003002},
	}, true, page)
	wantRefs := []string{"Psalms 1", "Psalms 1:2", "Psalms 2", "Psalms 2:10-12", "Psalms 2:12-3:2"}
	wantChapters := []int{1, 0, 2, 0, 0}
	for i, r := range rows {
		u, err := url.Parse(r.Href)
		if err != nil {
			t.Fatal(err)
		}
		if got := u.Query().Get("ref"); got != wantRefs[i] {
			t.Errorf("%d: ref %q, want %q", i, got, wantRefs[i])
		}
		if r.Chapter != wantChapters[i] {
			t.Errorf("%d: chapter %d, want %d", i, r.Chapter, wantChapters[i])
		}
	}
	// an overview names its chapters itself
	for _, r := range outlineRows([]model.OutlineItem{{Title: "A", Start: 40001001, End: 40001017}}, false, page) {
		if r.Chapter != 0 {
			t.Errorf("overview row headed by chapter: %+v", r)
		}
	}
}

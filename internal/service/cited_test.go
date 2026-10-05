package service

import (
	"slices"
	"testing"
	"time"

	"github.com/dgrieser/jw-cli/internal/model"
)

func TestResultDate(t *testing.T) {
	tests := []struct {
		context string
		year    int
	}{
		{"w06 15. 11. S. 26-30 - Der Wachtturm 2006", 2006},
		{"mwb26 Juli S. 14-15 - Leben und Dienst: Arbeitsheft (2026)", 2026},
		{"es27 - Täglich in den Schriften forschen – 2027", 2027},
		{"km 12/75 S. 2 - Königreichsdienst 1975", 1975},
		{"it-1 „Gebet“ - Einsichten, Band 1", 0},
		{"si S. 248-251 - „Inspiriert“-Buch (si)", 0},
	}
	for _, tt := range tests {
		got := resultDate(model.Result{Context: tt.context})
		if tt.year == 0 {
			if !got.IsZero() {
				t.Errorf("%q: date %v, want none", tt.context, got)
			}
			continue
		}
		if got.Year() != tt.year {
			t.Errorf("%q: year %d, want %d", tt.context, got.Year(), tt.year)
		}
	}
}

func TestMergeVideos(t *testing.T) {
	day := func(s string) time.Time {
		d, err := time.Parse(time.DateOnly, s)
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	video := func(title, published string) datedVideo {
		v := datedVideo{Result: model.Result{Title: title, Kind: "video"}}
		if published != "" {
			v.published = day(published)
		}
		return v
	}
	newest := []model.Result{
		{Title: "es27", Context: "es27 - Täglich in den Schriften forschen – 2027"},
		{Title: "mwb26", Context: "mwb26 Juli S. 14-15 - Arbeitsheft (2026)"},
		{Title: "it-1", Context: "it-1 „Gebet“ - Einsichten, Band 1"},
		{Title: "w06", Context: "w06 15. 11. S. 26-30 - Der Wachtturm 2006"},
		{Title: "w51", Context: "w51 1. 7. S. 201-205 - Der Wachtturm 1951"},
	}
	videos := []datedVideo{
		video("talk26", "2026-09-25"),
		video("undated", ""),
		video("convention20", "2020-07-11"),
		video("early26", "2026-02-01"),
	}
	titles := func(items []model.Result) []string {
		var out []string
		for _, r := range items {
			out = append(out, r.Title)
		}
		return out
	}

	got := titles(mergeVideos(slices.Clone(newest), videos, "newest"))
	// a video is compared with a publication of its own year at mid-year, and
	// goes right ahead of the dated publication it is newer than: the one
	// naming no year decides nothing
	want := []string{"es27", "talk26", "mwb26", "it-1", "early26", "convention20", "w06", "w51", "undated"}
	if !slices.Equal(got, want) {
		t.Errorf("newest:\n got %v\nwant %v", got, want)
	}

	oldest := slices.Clone(newest)
	slices.Reverse(oldest)
	got = titles(mergeVideos(oldest, videos, "oldest"))
	want = []string{"w51", "w06", "it-1", "convention20", "early26", "mwb26", "talk26", "es27", "undated"}
	if !slices.Equal(got, want) {
		t.Errorf("oldest:\n got %v\nwant %v", got, want)
	}

	// ranked by occurrences there is no date order to keep
	got = titles(mergeVideos(slices.Clone(newest), videos, "occ"))
	want = []string{"es27", "mwb26", "it-1", "w06", "w51", "talk26", "undated", "convention20", "early26"}
	if !slices.Equal(got, want) {
		t.Errorf("occ:\n got %v\nwant %v", got, want)
	}
}

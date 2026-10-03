package server

import (
	"testing"

	"github.com/dgrieser/jw-cli/internal/api/wol"
)

func TestLatestFirst(t *testing.T) {
	issue := func(title, cover string) wol.LibraryCard {
		return wol.LibraryCard{Kind: wol.LibraryKind, Title: title,
			Thumbnail: "https://wol.jw.org/de/wol/publication/r10/lp-x/" + cover + "/thumbnail"}
	}
	got := latestFirst([]wol.LibraryCard{
		issue("Januar", "mwb26/2026/1"), issue("März", "mwb26/2026/3"), issue("Mai", "mwb26/2026/5"),
	})
	if got[0].Title != "Mai" || got[1].Title != "März" || got[2].Title != "Januar" {
		t.Errorf("issues = %q %q %q, want the latest first", got[0].Title, got[1].Title, got[2].Title)
	}

	// books are no issues: wol's order stays
	books := []wol.LibraryCard{issue("Ahmt nach", "ia"), issue("Aus der Bibel lernen", "lfb")}
	if got := latestFirst(books); got[0].Title != "Ahmt nach" {
		t.Errorf("books reordered: %q first", got[0].Title)
	}
}

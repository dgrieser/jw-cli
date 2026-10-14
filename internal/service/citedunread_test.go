package service

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/dgrieser/jw-cli/internal/model"
)

// A row whose document could not be read is kept as it is: its teaser cannot
// be judged fairly, and judging it anyway made a listing depend on whether wol
// answered this time — 47 rows on a cold cache, 48 on a warm one.
func TestCitedListingKeepsARowItCouldNotRead(t *testing.T) {
	german := model.Language{Symbol: "X", Locale: "de"}
	search := wolFile(t, "search_citation.html")
	s := unfoldUpstream(t, func(mux *http.ServeMux) {
		mux.HandleFunc("/de", func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`<a href="/de/wol/h/r10/lp-x">Startseite</a>`))
		})
		mux.HandleFunc("/de/wol/s/r10/lp-x", func(w http.ResponseWriter, r *http.Request) {
			w.Write(search)
		})
		// the workbook's page: wol is busy
		mux.HandleFunc("/de/wol/d/r10/lp-x/202026249", func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
		})
		// the reading schedule and the answer to a question: read, but the
		// teaser cannot be placed in them, so the teaser is judged
		for _, path := range []string{"/de/wol/d/r10/lp-x/1102026214", "/de/wol/d/r10/lp-x/2014927"} {
			mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte(`<article id="article"><p data-pid="1">Something else entirely.</p></article>`))
			})
		}
	})
	p := SearchParams{Engine: "wol", Query: "(Jeremia 31:15)", Sort: "newest", Scope: "par", Excerpts: true}
	out, err := s.CitedListing(context.Background(), german, &p, nil)
	if err != nil {
		t.Fatal(err)
	}
	var titles []string
	for _, item := range out.Items {
		titles = append(titles, item.Title)
	}
	// the workbook kept with its teaser, the reading schedule judged and left
	// out, the answer judged and kept
	want := []string{"31. August–6. September", "Fragen von Lesern"}
	if len(titles) != len(want) || titles[0] != want[0] || titles[1] != want[1] || out.Total != 2 {
		t.Fatalf("rows = %q (total %d), want %q", titles, out.Total, want)
	}
	if out.Items[0].Excerpt != "" || out.Items[0].Snippet == "" {
		t.Errorf("the unread row should keep its teaser: %+v", out.Items[0])
	}
	// and the listing says so, in the reader's language
	if len(out.Notes) != 1 || !strings.Contains(out.Notes[0], "nur die Vorschau") || !strings.HasSuffix(out.Notes[0], ": 1") {
		t.Errorf("notes = %q, want the one unread row noted", out.Notes)
	}

	// a video search that fails leaves the publications listed and says the
	// videos are missing, rather than the listing reading as complete
	p.Videos = true
	out, err = s.CitedListing(context.Background(), german, &p, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Items) != 2 || !slices.ContainsFunc(out.Notes, func(n string) bool {
		return strings.HasPrefix(n, "Die Videos, die den Vers zitieren, konnten nicht gesucht werden: ")
	}) {
		t.Errorf("rows %d, notes = %q, want the failed video search noted", len(out.Items), out.Notes)
	}
}

// keepTelling leaves an unread row alone, whatever its teaser says.
func TestKeepTellingLeavesAnUnreadRow(t *testing.T) {
	items := []model.Result{{Title: "unread", Snippet: "33 34 35"}, {Title: "read", Snippet: "33 34 35"}}
	got := keepTelling(items, []bool{true, false})
	if len(got) != 1 || got[0].Title != "unread" {
		t.Errorf("kept %+v", got)
	}
}

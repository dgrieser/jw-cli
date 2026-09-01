package wol

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestSearchWOL(t *testing.T) {
	var gotQuery string
	mux := http.NewServeMux()
	mux.HandleFunc("/en/wol/s/r1/lp-e", func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Write([]byte(`<html><body><main>
		<div class="resultsCount">42 results</div>
		<ul class="results resultContentDocument">
		  <li class="searchResult">
		    <div class="caption"><a href="/en/wol/d/r1/lp-e/2024360">Preach the Good News</a></div>
		    <div class="searchResultDocument">...this good news of the Kingdom will be preached...</div>
		  </li>
		  <li class="searchResult">
		    <div class="caption"><a href="/en/wol/d/r1/lp-e/1102014204">The Sign of the Last Days</a></div>
		    <div class="searchResultDocument">...Matthew 24:14 shows...</div>
		  </li>
		</ul>
		</main></body></html>`))
	})
	c := testClient(t, mux)

	page, err := c.Search(context.Background(), cfgEN, "(Matthew 24:14)", SearchOpts{Scope: "par", Sort: "occ", Page: 2})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"q=%28Matthew+24%3A14%29", "p=par", "r=occ", "pg=2"} {
		if !strings.Contains(gotQuery, want) {
			t.Errorf("query missing %s: %s", want, gotQuery)
		}
	}
	if page.Total != 42 || len(page.Results) != 2 {
		t.Fatalf("page = total %d, %d results", page.Total, len(page.Results))
	}
	r := page.Results[0]
	if r.Title != "Preach the Good News" || r.DocID != 2024360 || !strings.Contains(r.Snippet, "good news of the Kingdom") {
		t.Errorf("result = %+v", r)
	}
}

// The live layout keeps a document's caption, its matching passages and its
// publication line as siblings under one results list. Recorded from
// wol.jw.org/de: a citation search for (Jer 31:15) with bibles and indexes
// filtered out.
func TestSearchWOLResultMarkup(t *testing.T) {
	var gotQuery string
	mux := http.NewServeMux()
	mux.HandleFunc("/de/wol/s/r10/lp-x", func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		serveFile(t, "testdata/search_citation.html")(w, r)
	})
	c := testClient(t, mux)
	cfgDE := Config{Locale: "de", Rsconf: "r10", Lp: "lp-x"}

	page, err := c.Search(context.Background(), cfgDE, "(Jeremia 31:15)", SearchOpts{
		Sort: "newest", Categories: []string{"w", "it"},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"q=%28Jeremia+31%3A15%29", "r=newest", "fc%5B%5D=w", "fc%5B%5D=it"} {
		if !strings.Contains(gotQuery, want) {
			t.Errorf("query missing %s: %s", want, gotQuery)
		}
	}
	if page.Total != 66 || page.Limit != 40 || len(page.Results) != 3 {
		t.Fatalf("page = total %d, limit %d, %d results", page.Total, page.Limit, len(page.Results))
	}
	r := page.Results[0]
	if r.Title != "31. August–6. September" || r.DocID != 202026249 {
		t.Errorf("result = %+v", r)
	}
	if !strings.HasSuffix(r.WOLLink, "/de/wol/d/r10/lp-x/202026249?q=%28Jer+31%3A15%29&p=par") {
		t.Errorf("link = %q", r.WOLLink)
	}
	if r.Context != "mwb26 Juli S. 14-15 - Leben und Dienst: Arbeitsheft (2026)" {
		t.Errorf("publication line = %q", r.Context)
	}
	if !strings.Contains(r.Snippet, "Wie hat sich diese Prophezeiung") {
		t.Errorf("snippet = %q", r.Snippet)
	}
	// a short passage keeps its markup, so the listing can show the highlights
	if !strings.Contains(r.Snippet, "<strong>JEREMIA 31</strong>") {
		t.Errorf("snippet lost its markup: %q", r.Snippet)
	}
	if !strings.HasSuffix(r.ImageURL, "/de/wol/d/r10/lp-x/202026249/thumbnail") {
		t.Errorf("thumbnail = %q", r.ImageURL)
	}
	// a passage longer than the cap is cut down to plain text rather than to
	// half a tag
	w14 := page.Results[2]
	if !strings.Contains(w14.Snippet, "Rahel weint um ihre Söhne") ||
		!strings.HasSuffix(w14.Snippet, "…") {
		t.Errorf("long snippet = %q", w14.Snippet)
	}
	if strings.Contains(w14.Snippet, "<") {
		t.Errorf("truncated snippet kept a tag: %q", w14.Snippet)
	}
	// the refine sidebar names every category the language offers
	if !slices.Contains(page.Filters, "mwbr") || !slices.Contains(page.Filters, "bi") {
		t.Errorf("filters = %v", page.Filters)
	}
	if got := c.Categories(cfgDE); !slices.Equal(got, page.Filters) {
		t.Errorf("cached categories = %v", got)
	}
}

// A nil category list means "no filter": the site default, everything included.
func TestSearchWOLNoCategories(t *testing.T) {
	var gotQuery string
	mux := http.NewServeMux()
	mux.HandleFunc("/de/wol/s/r10/lp-x", func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		serveFile(t, "testdata/search_citation.html")(w, r)
	})
	c := testClient(t, mux)
	cfgDE := Config{Locale: "de", Rsconf: "r10", Lp: "lp-x"}
	if _, err := c.Search(context.Background(), cfgDE, "(Jeremia 31:15)", SearchOpts{}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(gotQuery, "fc") {
		t.Errorf("unfiltered search sent a category filter: %s", gotQuery)
	}
}

func TestDailyText(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/en/wol/dt/r1/lp-e/2026/7/15", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html><body>
		<div class="todayItem"><h2>Wednesday, July 15</h2>
		  <p class="themeScrp">Let your brotherly love continue.—Heb. 13:1.</p>
		  <div class="bodyTxt"><p>Comment text here.</p></div>
		</div></body></html>`))
	})
	c := testClient(t, mux)
	art, err := c.DailyText(context.Background(), cfgEN, time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(art.HTML, "brotherly love") || !strings.Contains(art.Title, "July 15, 2026") {
		t.Errorf("daily text = %+v", art)
	}
}

// A language without a daily text for the date serves an #article container
// holding only navigation chrome; that must be an error, not content.
func TestDailyTextNavOnly(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/en/wol/dt/r1/lp-e/2026/7/15", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html><body><article id="article" class="article today">
		<div id="todayNav" class="forwardBackNavControls"><nav><ul>
		  <li class="todayNav"><a href="/en/wol/dt/r1/lp-e"><span>Today</span></a></li>
		</ul></nav></div></article></body></html>`))
	})
	c := testClient(t, mux)
	_, err := c.DailyText(context.Background(), cfgEN, time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC))
	if err == nil {
		t.Fatal("want error for nav-only page, got nil")
	}
	if !strings.Contains(err.Error(), "no daily text found") {
		t.Errorf("err = %v", err)
	}
}

func TestMeetings(t *testing.T) {
	mux := http.NewServeMux()
	// 2026-07-15 is in ISO week 29 of 2026
	mux.HandleFunc("/en/wol/meetings/r1/lp-e/2026/29", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html><body><div id="article"><header><h1>July 13-19</h1></header>
		<div class="bodyTxt"><p>Treasures from God's Word</p></div></div></body></html>`))
	})
	c := testClient(t, mux)
	art, err := c.Meetings(context.Background(), cfgEN, time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if art.Title != "July 13-19" || !strings.Contains(art.HTML, "Treasures") {
		t.Errorf("meetings = %+v", art)
	}
}

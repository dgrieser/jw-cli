package wol

import (
	"context"
	"net/http"
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

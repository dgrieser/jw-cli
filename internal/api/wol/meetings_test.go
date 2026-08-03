package wol

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// meetingsPage mirrors the shape of a wol meeting-material page: one link card
// per meeting, then the same two publications repeated as library links among
// the ones used at the meetings. Only the publication symbols in the class
// attribute identify the meetings — the headings are localized.
func meetingsPage(cards ...string) string {
	return `<html><body><div id="regionMain">
		<h2>Leben und Dienst</h2>` + strings.Join(cards, "") + `
		<h2>Andere Publikationen für die Zusammenkünfte</h2>
		<a class="jwac chrome cardContainer pub-mwb pub-mwb26 docClass-106 lnk"
		   href="/de/wol/library/r10/lp-x/arbeitshefte/juli"><div class="cardTitleBlock">
		   <div class="cardLine1">library repeat</div></div></a>
		<a class="jwac chrome cardContainer pub-w pub-w26 docClass-40 lnk"
		   href="/de/wol/library/r10/lp-x/wachtturm/mai"><div class="cardTitleBlock">
		   <div class="cardLine1">library repeat</div></div></a>
		<a class="jwac chrome cardContainer lnk" href="/de/wol/publication/r10/lp-x/sjj">
		   <div class="cardTitleBlock"><div class="cardLine1">Songbook</div></div></a>
	</div></body></html>`
}

const (
	midweekCard = `<a class="jwac showRuby today chrome
		cardContainer pub-mwb docId-202026245 pub-mwb26 docClass-106 lnk"
		href="/de/wol/d/r10/lp-x/202026245"><div class="cardTitleBlock">
		<div class="cardLine1">3.-9. August</div></div></a>`
	weekendCard = `<a class="jwac showRuby publicationCitation chrome
		cardContainer pub-w docId-2026404 pub-w26 docClass-40 lnk"
		href="/de/wol/d/r10/lp-x/2026404"><div class="cardTitleBlock">
		<div class="cardLine1">Respektiere die Entscheidungen anderer</div></div></a>`
)

func meetingsMux(t *testing.T, page string) *http.ServeMux {
	t.Helper()
	mux := http.NewServeMux()
	// 2026-08-03 is in ISO week 32 of 2026
	mux.HandleFunc("/de/wol/meetings/r10/lp-x/2026/32", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, page)
	})
	return mux
}

var testCfg = Config{Locale: "de", Rsconf: "r10", Lp: "lp-x"}

var testDate = time.Date(2026, time.August, 3, 12, 0, 0, 0, time.UTC)

func TestMeetingParts(t *testing.T) {
	c := testClient(t, meetingsMux(t, meetingsPage(midweekCard, weekendCard)))
	parts, err := c.MeetingParts(context.Background(), testCfg, testDate)
	if err != nil {
		t.Fatal(err)
	}
	if parts.Week != 32 || parts.Year != 2026 {
		t.Errorf("week/year = %d/%d, want 32/2026", parts.Week, parts.Year)
	}
	// the /wol/d/ document links win; the library repeats further down are
	// skipped, and both are absolutized
	for _, tc := range []struct{ name, got, wantSuffix string }{
		{"midweek", parts.Midweek, "/de/wol/d/r10/lp-x/202026245"},
		{"weekend", parts.Weekend, "/de/wol/d/r10/lp-x/2026404"},
	} {
		if !strings.HasSuffix(tc.got, tc.wantSuffix) {
			t.Errorf("%s = %q, want a URL ending in %q", tc.name, tc.got, tc.wantSuffix)
		}
		if !strings.HasPrefix(tc.got, "http") {
			t.Errorf("%s not absolutized: %q", tc.name, tc.got)
		}
	}
}

// TestMeetingPartsOneMeeting covers a week that lists only one meeting: the
// other comes back empty rather than borrowing the library repeat.
func TestMeetingPartsOneMeeting(t *testing.T) {
	c := testClient(t, meetingsMux(t, meetingsPage(weekendCard)))
	parts, err := c.MeetingParts(context.Background(), testCfg, testDate)
	if err != nil {
		t.Fatal(err)
	}
	if parts.Midweek != "" {
		t.Errorf("midweek should be empty, got %q", parts.Midweek)
	}
	if parts.Weekend == "" {
		t.Error("weekend link missing")
	}
}

func TestMeetingPartsNoneFound(t *testing.T) {
	c := testClient(t, meetingsMux(t, meetingsPage()))
	if _, err := c.MeetingParts(context.Background(), testCfg, testDate); err == nil {
		t.Error("want an error when the page lists no meeting documents")
	}
}

// TestCitationAPI covers the transformation that makes a citation resolvable.
// A document's citation links carry a locale segment and answer with a 307 to
// the target page; the same path without it answers with the passage itself.
func TestCitationAPI(t *testing.T) {
	for _, tc := range []struct{ name, in, want string }{
		{"relative with locale", "/de/wol/pc/r10/lp-x/1204408/577/0", "/wol/pc/r10/lp-x/1204408/577/0"},
		{"bible citation", "/en/wol/bc/r1/lp-e/1102026207/11/0", "/wol/bc/r1/lp-e/1102026207/11/0"},
		{"absolute", "https://wol.jw.org/de/wol/bc/r10/lp-x/1/2", "https://wol.jw.org/wol/bc/r10/lp-x/1/2"},
		// the content wol returns already uses the locale-less form
		{"already stripped", "/wol/pc/r10/lp-x/1/2", "/wol/pc/r10/lp-x/1/2"},
		{"absolute already stripped", "https://wol.jw.org/wol/pc/r10/lp-x/1/2", "https://wol.jw.org/wol/pc/r10/lp-x/1/2"},
		// only the first segment goes, so a path that repeats the word survives
		{"leaves later segments", "/de/wol/pc/r10/lp-x/wol/1", "/wol/pc/r10/lp-x/wol/1"},
		{"empty", "", ""},
	} {
		if got := citationAPI(tc.in); got != tc.want {
			t.Errorf("%s: citationAPI(%q) = %q, want %q", tc.name, tc.in, got, tc.want)
		}
	}
}

// TestTooltipStripsLocale pins the request the client actually makes: the
// locale-prefixed path is a navigation redirect, not content.
func TestTooltipStripsLocale(t *testing.T) {
	var asked string
	mux := http.NewServeMux()
	mux.HandleFunc("/wol/pc/r10/lp-x/1204408/577/0", func(w http.ResponseWriter, r *http.Request) {
		asked = r.URL.Path
		fmt.Fprint(w, `{"items":[{"title":"Betteln","content":"<p>Das mosaische Gesetz</p>","url":"/wol/d/r10/lp-x/1200000617"}]}`)
	})
	c := testClient(t, mux)
	tip, err := c.Tooltip(context.Background(), "/de/wol/pc/r10/lp-x/1204408/577/0")
	if err != nil {
		t.Fatal(err)
	}
	if asked != "/wol/pc/r10/lp-x/1204408/577/0" {
		t.Errorf("requested %q", asked)
	}
	if tip.Title != "Betteln" || !strings.Contains(tip.ContentHTML, "mosaische") {
		t.Errorf("tooltip = %+v", tip)
	}
	if !strings.HasPrefix(tip.URL, "http") {
		t.Errorf("url not absolutized: %q", tip.URL)
	}
}

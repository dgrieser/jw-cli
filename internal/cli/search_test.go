package cli

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/dgrieser/jw-cli/internal/model"
)

func searchMux(t *testing.T) *http.ServeMux {
	mux := languagesMux(t)
	mux.HandleFunc("/tokens/jworg.jwt", func(w http.ResponseWriter, r *http.Request) {
		payload := base64.RawURLEncoding.EncodeToString(fmt.Appendf(nil, `{"exp":%d}`, time.Now().Add(time.Hour).Unix()))
		fmt.Fprint(w, "h."+payload+".s")
	})
	videos := func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			http.Error(w, "no token", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		// the live API marks up matched words and spells references with &nbsp;
		fmt.Fprint(w, `{
			"results": [{"type": "item", "subtype": "video", "title": "Daniel&nbsp;7:27 Schöpfung", "lank": "pub-xyz_1_VIDEO",
			             "snippet": "vom <strong>Königreich</strong>\nbekannt machen?",
			             "duration": "3:10", "links": {"jw.org": "https://www.jw.org/finder?lank=pub-xyz_1_VIDEO"}}],
			"insight": {"total": {"value": 1}}
		}`)
	}
	// the same results in every language of the fixture, so a test can vary -l
	for _, symbol := range []string{"E", "X", "F"} {
		mux.HandleFunc("/apis/search/results/"+symbol+"/videos", videos)
	}
	return mux
}

func TestSearchCommand(t *testing.T) {
	out, err := runCmd(t, searchMux(t), "search", "-l", "de", "-t", "videos", "Schöpfung")
	if err != nil {
		t.Fatal(err)
	}
	// the header follows -l de; tags and entities are rendered away, and the
	// snippet's newline is collapsed so it cannot break the indented listing
	for _, want := range []string{
		`1 Ergebnis für "Schöpfung"`,
		"Daniel 7:27 Schöpfung (3:10, pub-xyz_1_VIDEO)",
		"     vom Königreich bekannt machen?\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	for _, unwanted := range []string{"<strong>", "&nbsp;", "\x1b["} {
		if strings.Contains(out, unwanted) {
			t.Errorf("listing still contains %q:\n%s", unwanted, out)
		}
	}
}

// TestSearchJSONKeepsSourceMarkup pins that -o json stays the API's data: the
// highlight markup is only rendered away for human-readable listings.
func TestSearchJSONKeepsSourceMarkup(t *testing.T) {
	out, err := runCmd(t, searchMux(t), "search", "-l", "de", "-t", "videos", "-o", "json", "Schöpfung")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `\u003cstrong\u003e`) {
		t.Errorf("json should keep the source markup:\n%s", out)
	}
}

func TestSearchThenOpen(t *testing.T) {
	mux := searchMux(t)
	cacheDir := t.TempDir()
	if _, err := runCmdWithCache(t, mux, cacheDir, "search", "-l", "de", "-t", "videos", "Schöpfung"); err != nil {
		t.Fatal(err)
	}
	out, err := runCmdWithCache(t, mux, cacheDir, "open", "1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "finder?lank=pub-xyz_1_VIDEO") {
		t.Errorf("open output: %s", out)
	}
}

func TestSearchJSON(t *testing.T) {
	out, err := runCmd(t, searchMux(t), "search", "-l", "de", "-t", "videos", "-o", "json", "Schöpfung")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"lank": "pub-xyz_1_VIDEO"`) || !strings.Contains(out, `"index": 1`) {
		t.Errorf("json output:\n%s", out)
	}
	// the listing says what the engine reported for the whole query, and
	// when it was made: the stamp Save set, not the zero time of a copy
	if !strings.Contains(out, `"total": 1`) {
		t.Errorf("json output lacks the total:\n%s", out)
	}
	if strings.Contains(out, `"timestamp": "0001-01-01T00:00:00Z"`) || !strings.Contains(out, `"timestamp": "`) {
		t.Errorf("json output lacks a timestamp:\n%s", out)
	}
}

// A page size the engine would not serve is refused before anything is asked
// of it: jw.org answers more than 36 with a bare HTTP 400, and wol serves its
// pages of 40 whatever is asked.
func TestSearchLimitIsChecked(t *testing.T) {
	mux := languagesMux(t)
	mux.HandleFunc("/apis/", func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("an invalid limit reached the API: %s", r.URL)
	})
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"-n", "37"}, "1 to 36"},
		{[]string{"-n", "0"}, "1 to 36"},
		{[]string{"-e", "wol", "-n", "20"}, "pages hold 40"},
	} {
		_, err := runCmd(t, mux, append([]string{"search", "-l", "en"}, append(tc.args, "kingdom")...)...)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%v: want an error saying %q, got %v", tc.args, tc.want, err)
		}
	}
	for _, ok := range []struct {
		engine string
		limit  int
		set    bool
	}{{"jworg", 36, true}, {"jworg", 1, true}, {"wol", 10, false}, {"wol", 40, true}} {
		if err := checkLimit(ok.engine, ok.limit, ok.set); err != nil {
			t.Errorf("%+v: %v", ok, err)
		}
	}
}

// jw.org serves the first page of an all-types search grouped by type and
// sized by itself: the header names the page, not a page size it ignored.
func TestSearchGroupedPageHeader(t *testing.T) {
	mux := searchMux(t)
	mux.HandleFunc("/apis/search/results/E/all", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"results": [
			{"type": "item", "subtype": "article", "title": "A", "lank": "pa-1"},
			{"type": "group", "title": "Videos", "results": [
				{"type": "item", "subtype": "video", "title": "B", "lank": "pub-b_1_VIDEO"},
				{"type": "item", "subtype": "video", "title": "C", "lank": "pub-c_1_VIDEO"}
			]}
		], "insight": {"total": {"value": 100}}}`)
	})
	out, err := runCmd(t, mux, "search", "-l", "en", "-n", "2", "kingdom")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `100 results for "kingdom" — page 1`) || strings.Contains(out, "per page") {
		t.Errorf("header of a grouped page:\n%s", out)
	}
}

// Within one interactive session a result is listed on the page it first came
// on: jw.org's second page of an all-types search repeats rows of the first.
func TestDropRepeats(t *testing.T) {
	listed := map[string]int{}
	page1 := dropRepeats([]model.Result{{LANK: "pa-1"}, {LANK: "pa-2"}, {Title: "no key"}}, 1, listed)
	page2 := dropRepeats([]model.Result{{LANK: "pa-2"}, {LANK: "pa-3"}, {Title: "no key"}}, 2, listed)
	again := dropRepeats([]model.Result{{LANK: "pa-1"}, {LANK: "pa-2"}}, 1, listed)
	if len(page1) != 3 || len(again) != 2 {
		t.Errorf("page 1 lost rows: %+v, again %+v", page1, again)
	}
	if len(page2) != 2 || page2[0].LANK != "pa-3" {
		t.Errorf("page 2 should drop pa-2 only: %+v", page2)
	}
}

// TestSearchHeaderLanguage pins the header to the requested language, with
// English for a language that has no translation.
func TestSearchHeaderLanguage(t *testing.T) {
	for _, tc := range []struct{ lang, want string }{
		{"de", `1 Ergebnis für "Schöpfung"`},
		{"en", `1 result for "Schöpfung"`},
		{"fr", `1 result for "Schöpfung"`}, // no French catalog: fall back
	} {
		out, err := runCmd(t, searchMux(t), "search", "-l", tc.lang, "-t", "videos", "Schöpfung")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, tc.want) {
			t.Errorf("-l %s: missing %q in:\n%s", tc.lang, tc.want, out)
		}
	}
}

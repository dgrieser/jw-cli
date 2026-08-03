package cli

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// meetingsMux serves a wol library that has a meeting-material page for ISO week
// 32 of 2026, plus the two documents that page links to.
func meetingsMux(t *testing.T) *http.ServeMux {
	mux := languagesMux(t)
	mux.HandleFunc("/de", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<a href="/de/wol/h/r10/lp-x">home</a>`))
	})
	mux.HandleFunc("/de/wol/meetings/r10/lp-x/2026/32", func(w http.ResponseWriter, r *http.Request) {
		// one card per meeting, then the same publications repeated as library
		// links: only the /wol/d/ ones are the material itself
		fmt.Fprint(w, `<html><body><main><h1>3.-9. August</h1>
			<h2>Leben und Dienst</h2>
			<a class="chrome cardContainer pub-mwb pub-mwb26 docClass-106 lnk"
			   href="/de/wol/d/r10/lp-x/202026245"><div class="cardTitleBlock">
			   <div class="cardLine1">3.-9. August</div></div></a>
			<h2>Wachtturm-Studium</h2>
			<a class="chrome cardContainer pub-w pub-w26 docClass-40 lnk"
			   href="/de/wol/d/r10/lp-x/2026404"><div class="cardTitleBlock">
			   <div class="cardLine1">Respektiere die Entscheidungen anderer</div></div></a>
			<h2>Andere Publikationen</h2>
			<a class="chrome cardContainer pub-mwb docClass-106 lnk"
			   href="/de/wol/library/r10/lp-x/arbeitshefte/juli"><div class="cardTitleBlock">
			   <div class="cardLine1">library repeat</div></div></a>
			</main></body></html>`)
	})
	mux.HandleFunc("/de/wol/d/r10/lp-x/202026245", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<html><body><div id="article"><header><h1>3.-9. AUGUST</h1></header>
			<div class="bodyTxt"><p>SCHÄTZE AUS GOTTES WORT</p></div></div></body></html>`)
	})
	mux.HandleFunc("/de/wol/d/r10/lp-x/2026404", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<html><body><div id="article">
			<header><h1>Respektiere die Entscheidungen anderer</h1></header>
			<div class="bodyTxt">
			  <p>Urteilt nicht über unterschiedliche Meinungen
			    (<a class="b" data-bid="1-1" href="/de/wol/bc/r10/lp-x/2026404/0/0">Röm. 14:1</a>)</p>
			  <figure>
			    <img src="/de/wol/mp/r10/lp-x/w26/296" alt="Reiseziel"/>
			    <figcaption>Reiseziel</figcaption>
			  </figure>
			  <a class="chrome cardContainer lnk" href="/de/wol/publication/r10/lp-x/w26">
			    <div class="cardThumbnail"><img src="/de/wol/publication/r10/lp-x/w26/thumbnail"/></div>
			    <div class="cardTitleBlock"><div class="cardLine1">Der Wachtturm</div></div></a>
			</div></div></body></html>`)
	})
	return mux
}

func TestMeetingsSubcommands(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"midweek", []string{"midweek"}, "SCHÄTZE AUS GOTTES WORT"},
		{"midweek alias", []string{"mw"}, "SCHÄTZE AUS GOTTES WORT"},
		{"weekend", []string{"weekend"}, "Urteilt nicht über unterschiedliche Meinungen"},
		{"weekend alias", []string{"we"}, "Urteilt nicht über unterschiedliche Meinungen"},
	} {
		args := append([]string{"meetings"}, tc.args...)
		// a fixed date so the ISO week the mock serves is the one requested
		args = append(args, "--date", "2026-08-03", "-l", "de", "-o", "raw")
		out, err := runCmd(t, meetingsMux(t), args...)
		if err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		if !strings.Contains(out, tc.want) {
			t.Errorf("%s: missing %q in:\n%s", tc.name, tc.want, out)
		}
	}
}

// TestMeetingsOverviewStillListsBoth guards the parent command: it keeps showing
// the material page rather than one meeting's document.
func TestMeetingsOverviewStillListsBoth(t *testing.T) {
	out, err := runCmd(t, meetingsMux(t), "meetings", "--date", "2026-08-03", "-l", "de", "-o", "raw")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Leben und Dienst", "Wachtturm-Studium", "Respektiere die Entscheidungen anderer"} {
		if !strings.Contains(out, want) {
			t.Errorf("overview missing %q in:\n%s", want, out)
		}
	}
}

func TestMeetingsRejectsUnknownSubcommand(t *testing.T) {
	if _, err := runCmd(t, meetingsMux(t), "meetings", "bogus"); err == nil {
		t.Error("want an error for an unknown subcommand")
	}
}

func TestMeetingsRejectsBadDate(t *testing.T) {
	_, err := runCmd(t, meetingsMux(t), "meetings", "weekend", "--date", "03.08.2026", "-l", "de")
	if err == nil || !strings.Contains(err.Error(), "want YYYY-MM-DD") {
		t.Errorf("want a date format error, got %v", err)
	}
}

// TestMeetingsPartArticleFlags covers the article flags on a meeting part: the
// same --refs/--images the article command has, reaching the linked document.
func TestMeetingsPartArticleFlags(t *testing.T) {
	base := []string{"meetings", "weekend", "--date", "2026-08-03", "-l", "de", "-o", "raw"}
	refs, err := runCmd(t, meetingsMux(t), append(base, "--refs")...)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Bibelstellen in", "[Röm. 14:1]("} {
		if !strings.Contains(refs, want) {
			t.Errorf("--refs missing %q in:\n%s", want, refs)
		}
	}
	images, err := runCmd(t, meetingsMux(t), append(base, "--images")...)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(images, "Reiseziel") || !strings.Contains(images, "/mp/r10/lp-x/w26/296") {
		t.Errorf("--images output:\n%s", images)
	}
	// the publication cover on a link card is navigation, not an illustration
	if strings.Contains(images, "thumbnail") {
		t.Errorf("--images listed a card thumbnail:\n%s", images)
	}
}

// TestMeetingsOverviewImagesSkipsCardThumbnails pins the same rule on the
// material page, which is nothing but link cards.
func TestMeetingsOverviewImagesSkipsCardThumbnails(t *testing.T) {
	out, err := runCmd(t, meetingsMux(t), "meetings", "--images", "--date", "2026-08-03", "-l", "de", "-o", "raw")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "thumbnail") {
		t.Errorf("card thumbnails leaked into the image listing:\n%s", out)
	}
}

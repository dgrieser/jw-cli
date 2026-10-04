package server

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dgrieser/jw-cli/internal/httpx"
	"github.com/dgrieser/jw-cli/internal/render"
	"github.com/dgrieser/jw-cli/internal/service"
)

func TestClassify(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		err    error
		status int
		code   string
	}{
		{&tooExpensiveError{level: 1, requests: 5000}, 422, "too_expensive"},
		{&httpx.StatusError{StatusCode: 404}, 404, "not_found"},
		{&httpx.StatusError{StatusCode: 500}, 502, "upstream"},
		{errMissing("q"), 400, "bad_request"},
	} {
		status, code := classify(ctx, tc.err)
		if status != tc.status || code != tc.code {
			t.Errorf("classify(%v) = %d %q, want %d %q", tc.err, status, code, tc.status, tc.code)
		}
	}
}

// TestUnfoldConfigRefuses pins the server posture: nobody is at a terminal to
// confirm an expensive expansion, so the confirmation callback always declines
// with the error the API maps to 422.
func TestUnfoldConfigRefuses(t *testing.T) {
	cfg := unfoldConfig(9, false)
	if cfg.Depth != maxUnfoldDepth {
		t.Errorf("depth should be capped at %d, got %d", maxUnfoldDepth, cfg.Depth)
	}
	// the same expansion the CLI runs, citation lookups included
	if !cfg.Cited {
		t.Error("citation lookups should be on, as they are on the command line")
	}
	if unfoldConfig(0, false).Cited {
		t.Error("nothing to expand, nothing to look up")
	}
	ok, err := cfg.Confirm(2, 5000)
	if ok || err == nil {
		t.Fatalf("confirm = %v, %v; want a refusal", ok, err)
	}
	if status, code := classify(context.Background(), err); status != 422 || code != "too_expensive" {
		t.Errorf("refusal classified as %d %q", status, code)
	}
	// force is what -y is on the command line: the question is not asked, so
	// there is nothing to refuse
	if unfoldConfig(9, true).Confirm != nil {
		t.Error("force should spend the budget without asking")
	}
}

func TestBodyFormat(t *testing.T) {
	for _, tc := range []struct {
		param string
		want  render.Format
		ok    bool
	}{
		{"", render.HTML, true},
		{"html", render.HTML, true},
		{"markdown", render.Markdown, true},
		{"md", render.Markdown, true},
		{"text", render.Text, true},
		{"json", render.HTML, false},
		{"raw", render.HTML, false},
	} {
		r := httptest.NewRequest("GET", "/?format="+tc.param, nil)
		got, err := bodyFormat(r)
		if (err == nil) != tc.ok || (tc.ok && got != tc.want) {
			t.Errorf("bodyFormat(%q) = %v, %v", tc.param, got, err)
		}
	}
}

func TestBoolParam(t *testing.T) {
	for _, tc := range []struct {
		query string
		want  bool
	}{
		{"", false},
		{"?x=false", false},
		{"?x", true},
		{"?x=true", true},
		{"?x=1", true},
	} {
		r := httptest.NewRequest("GET", "/"+tc.query, nil)
		if got := boolParam(r, "x"); got != tc.want {
			t.Errorf("boolParam(%q) = %v, want %v", tc.query, got, tc.want)
		}
	}
}

// A verse's sections fold away so a reading reads as a reading: each section
// becomes a disclosure, every entry inside one a disclosure of its own, and the
// verse text and its heading stay in the open.
func TestFoldSections(t *testing.T) {
	const body = `<h2>Jeremia 34:3</h2><p>the verse</p>` +
		`<div class="expansion">` +
		`<h3>Index der Publikationen</h3><h4>w80 1. 3. 24</h4><p>a passage</p>` +
		`<h3>Querverweis Jeremia 37:17</h3><p>another verse</p>` +
		`<h3>Zitate von Jeremia 34:3</h3><h4>Zedekia</h4><p>a quotation</p>` +
		`</div>`
	out := foldSections(body)
	if n := strings.Count(out, "<details"); n != 5 {
		t.Fatalf("%d disclosures, want one per section and entry:\n%s", n, out)
	}
	for _, want := range []string{
		"<summary>Index der Publikationen</summary>",
		"<summary>Querverweis Jeremia 37:17</summary>",
		"<summary>Zitate von Jeremia 34:3</summary>",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
	// the sections side by side share one list, and an entry sits folded
	// inside the section it belongs to
	if strings.Count(out, `<div class="sections">`) != 3 {
		t.Errorf("want one list of sections and one per section with entries:\n%s", out)
	}
	if !strings.Contains(out, `<summary>Index der Publikationen</summary><div class="section-body"><div class="sections">`+
		`<details class="section"><summary>w80 1. 3. 24</summary><div class="section-body"><p>a passage</p></div></details>`) {
		t.Errorf("an entry escaped its section:\n%s", out)
	}
	// the verse and its heading are not folded
	if strings.Contains(out, "<summary>Jeremia 34:3</summary>") || !strings.Contains(out, "<p>the verse</p>") {
		t.Errorf("the verse itself was folded:\n%s", out)
	}
	// nothing to fold changes nothing
	if got := foldSections("<h2>Jeremia 34:3</h2><p>the verse</p>"); !strings.Contains(got, "<p>the verse</p>") ||
		strings.Contains(got, "<details") {
		t.Errorf("a verse without sections should be left alone: %s", got)
	}
}

// A streamed section is folded all the way down: text before the first
// heading stays open, every heading opens on its own.
func TestFoldFragment(t *testing.T) {
	out := foldFragment(`<p>intro</p><h3>a</h3><p>one</p><h4>a.1</h4><p>deep</p><h3>b</h3><p>two</p>`)
	if !strings.HasPrefix(out, `<p>intro</p><div class="sections">`) {
		t.Errorf("intro folded or list missing:\n%s", out)
	}
	if n := strings.Count(out, "<details"); n != 3 {
		t.Errorf("%d disclosures, want 3:\n%s", n, out)
	}
	if got := foldFragment("<p>plain</p>"); got != "<p>plain</p>" {
		t.Errorf("headingless fragment changed: %s", got)
	}
}

// A stream only ever follows a citation path on wol, whatever host the link
// it was given named.
func TestCitationPath(t *testing.T) {
	for raw, want := range map[string]string{
		"https://wol.jw.org/en/wol/bc/r1/lp-e/2024360/0/0": "/en/wol/bc/r1/lp-e/2024360/0/0",
		"/en/wol/pc/r1/lp-e/1102024360/1/0":                "/en/wol/pc/r1/lp-e/1102024360/1/0",
		"https://evil.example/wol/bc/1":                    "/wol/bc/1",
		"//evil.example/wol/bc/1":                          "/wol/bc/1",
	} {
		if got, ok := citationPath(raw); !ok || got != want {
			t.Errorf("citationPath(%q) = %q, %v; want %q", raw, got, ok, want)
		}
	}
	for _, raw := range []string{"/en/wol/d/r1/lp-e/2024360", "javascript:alert(1)", ""} {
		if _, ok := citationPath(raw); ok {
			t.Errorf("citationPath(%q) accepted", raw)
		}
	}
}

func TestHeadingLevel(t *testing.T) {
	for name, want := range map[string]int{"h1": 1, "h6": 6, "h7": 0, "p": 0, "div": 0, "": 0} {
		if got := headingLevel(name); got != want {
			t.Errorf("headingLevel(%q) = %d, want %d", name, got, want)
		}
	}
}

// TestUnfoldLevels pins the switcher: one link per level to the same page,
// the page's default level carrying no ?unfold=, and no force= carried along.
func TestUnfoldLevels(t *testing.T) {
	r := httptest.NewRequest("GET", "/meetings/weekend?date=2026-08-10&unfold=2&force=1&lang=de", nil)
	levels := unfoldLevels(r, 2, 0)
	if len(levels) != maxUnfoldDepth+1 {
		t.Fatalf("want %d levels, got %d", maxUnfoldDepth+1, len(levels))
	}
	for _, l := range levels {
		if strings.Contains(l.URL, "force=") {
			t.Errorf("level %d carries force: %s", l.Level, l.URL)
		}
		if !strings.HasPrefix(l.URL, "/meetings/weekend?") || !strings.Contains(l.URL, "date=2026-08-10") ||
			!strings.Contains(l.URL, "lang=de") {
			t.Errorf("level %d lost the page: %s", l.Level, l.URL)
		}
		if l.Active != (l.Level == 2) {
			t.Errorf("level %d active=%v", l.Level, l.Active)
		}
	}
	if strings.Contains(levels[0].URL, "unfold=") || !strings.Contains(levels[3].URL, "unfold=3") {
		t.Errorf("unexpected level links: %s / %s", levels[0].URL, levels[3].URL)
	}
	// the bible reads at level 1 by default, so level 0 has to be spelled out
	bible := unfoldLevels(httptest.NewRequest("GET", "/bible?ref=John+3:16", nil), 1, 1)
	if !strings.Contains(bible[0].URL, "unfold=0") || strings.Contains(bible[1].URL, "unfold=") {
		t.Errorf("bible levels: %s / %s", bible[0].URL, bible[1].URL)
	}
}

// A link to an article is only ever read from the site it belongs to, put back
// on that site's own base; anything else is refused.
func TestArticleTarget(t *testing.T) {
	s := &Server{svc: &service.Service{HTTP: httpx.New()}}
	for raw, want := range map[string]string{
		"https://wol.jw.org/de/wol/d/r10/lp-x/1102010144#h=43:0-48:0": "https://wol.jw.org/de/wol/d/r10/lp-x/1102010144#h=43:0-48:0",
		"https://evil.example/de/wol/d/r10/lp-x/1":                    "https://wol.jw.org/de/wol/d/r10/lp-x/1",
		"https://wol.jw.org/de/wol/tc/r10/lp-x/1001070640/3":          "https://wol.jw.org/de/wol/tc/r10/lp-x/1001070640/3",
		"https://www.jw.org/de/bibliothek/artikel/x/":                 "https://www.jw.org/de/bibliothek/artikel/x/",
	} {
		if got, ok := s.articleTarget(raw); !ok || got != want {
			t.Errorf("articleTarget(%q) = %q, %v; want %q", raw, got, ok, want)
		}
	}
	for _, raw := range []string{
		"https://evil.example/x", "https://wol.jw.org/de/wol/bc/r10/lp-x/1/2", "javascript:alert(1)", "",
	} {
		if got, ok := s.articleTarget(raw); ok {
			t.Errorf("articleTarget(%q) accepted as %q", raw, got)
		}
	}
}

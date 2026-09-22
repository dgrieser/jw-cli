package server

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dgrieser/jw-cli/internal/httpx"
	"github.com/dgrieser/jw-cli/internal/render"
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
// becomes a disclosure, and the verse text and its heading stay in the open.
func TestFoldSections(t *testing.T) {
	const body = `<h2>Jeremia 34:3</h2><p>the verse</p>` +
		`<div class="expansion">` +
		`<h3>Index der Publikationen</h3><h4>w80 1. 3. 24</h4><p>a passage</p>` +
		`<h3>Querverweis Jeremia 37:17</h3><p>another verse</p>` +
		`<h3>Zitate von Jeremia 34:3</h3><h4>Zedekia</h4><p>a quotation</p>` +
		`</div>`
	out := foldSections(body)
	if n := strings.Count(out, "<details"); n != 3 {
		t.Fatalf("%d disclosures, want one per section:\n%s", n, out)
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
	// what a section heads stays inside it
	if !strings.Contains(out, "<h4>w80 1. 3. 24</h4><p>a passage</p></details>") {
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

func TestHeadingLevel(t *testing.T) {
	for name, want := range map[string]int{"h1": 1, "h6": 6, "h7": 0, "p": 0, "div": 0, "": 0} {
		if got := headingLevel(name); got != want {
			t.Errorf("headingLevel(%q) = %d, want %d", name, got, want)
		}
	}
}

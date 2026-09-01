package server

import (
	"context"
	"net/http/httptest"
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
	cfg := unfoldConfig(9)
	if cfg.Depth != maxUnfoldDepth {
		t.Errorf("depth should be capped at %d, got %d", maxUnfoldDepth, cfg.Depth)
	}
	ok, err := cfg.Confirm(2, 5000)
	if ok || err == nil {
		t.Fatalf("confirm = %v, %v; want a refusal", ok, err)
	}
	if status, code := classify(context.Background(), err); status != 422 || code != "too_expensive" {
		t.Errorf("refusal classified as %d %q", status, code)
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

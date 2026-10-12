package server

import "testing"

func TestHeadingHTML(t *testing.T) {
	for _, c := range []struct {
		heading string
		chapter int
		want    string
	}{
		{"Sprüche 6", 6, `Sprüche <a class="chapter-num" href="/b">6</a>`},
		{"Sprüche 6:1-2", 6, `Sprüche <a class="chapter-num" href="/b">6</a>:1-2`},
		{"1. Mose 1:1 — NWT", 1, `1. Mose <a class="chapter-num" href="/b">1</a>:1 — NWT`},
		{"1 Kings 1", 1, `1 Kings <a class="chapter-num" href="/b">1</a>`},
		{"Psalm 23", 0, `Psalm 23`},
		{"Psalm 23", 24, `Psalm 23`},
	} {
		if got := headingHTML(c.heading, c.chapter, "/b"); got != c.want {
			t.Errorf("headingHTML(%q, %d) = %q, want %q", c.heading, c.chapter, got, c.want)
		}
	}
}

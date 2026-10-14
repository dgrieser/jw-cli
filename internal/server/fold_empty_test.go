package server

import (
	"strings"
	"testing"
)

// A heading with nothing under it stays a heading rather than a section
// that opens on nothing; one with content is folded as before.
func TestFoldEmptyHeading(t *testing.T) {
	got := foldFragment(`<header><h2><strong>Gerechtigkeit</strong></h2></header><h3>Notes</h3><p>Text</p>`, lazyFallback{})
	if strings.Contains(got, "<summary><strong>Gerechtigkeit") || !strings.Contains(got, "<h2><strong>Gerechtigkeit</strong></h2>") {
		t.Errorf("an empty heading folded:\n%s", got)
	}
	if !strings.Contains(got, `<details class="section"><summary>Notes</summary>`) {
		t.Errorf("a heading with content not folded:\n%s", got)
	}
}

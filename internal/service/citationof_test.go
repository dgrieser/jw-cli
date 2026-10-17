package service

import "testing"

// A search's place of a passage is cut to its citation; one with no title
// after it, a video's date, is kept whole.
func TestCitationOf(t *testing.T) {
	for in, want := range map[string]string{
		"es27 - Täglich in den Schriften forschen – 2027":         "es27",
		"w26 April S. 8-13 - Der Wachtturm (Studienausgabe) 2026": "w26 April S. 8-13",
		"2025-01-20": "2025-01-20",
		"":           "",
	} {
		if got := CitationOf(in); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
}

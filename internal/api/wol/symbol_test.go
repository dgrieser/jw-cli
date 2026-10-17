package wol

import "testing"

// A citation's publication symbol is the issue's own, after the series it is
// part of, and none where the classes name none.
func TestSymbolOfClasses(t *testing.T) {
	for classes, want := range map[string]string{
		"publicationCitation html5 pub- docId-1102025954 pub-wcg pub- pub-wcg docClass-13": "wcg",
		"publicationCitation html5 pub-w docId-2022400 pub-w22 pub-w pub-w22 docClass-40":  "w22",
		"document html5 docId-1001070147 bible":                                            "",
	} {
		if got := SymbolOfClasses(classes); got != want {
			t.Errorf("%q: got %q, want %q", classes, got, want)
		}
	}
}

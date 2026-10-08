package server

import (
	"testing"

	"github.com/dgrieser/jw-cli/internal/bibleref"
)

func TestVidRef(t *testing.T) {
	for _, c := range []struct {
		vid  int
		to   string
		want bibleref.Ref
		ok   bool
	}{
		{43005029, "", bibleref.Ref{Book: 43, Chapter: 5, VerseStart: 29, VerseEnd: 29}, true},
		{43005029, "43005030", bibleref.Ref{Book: 43, Chapter: 5, VerseStart: 29, VerseEnd: 30}, true},
		// a passage running into the next chapter opens where it starts
		{43005029, "43006002", bibleref.Ref{Book: 43, Chapter: 5, VerseStart: 29, VerseEnd: 29}, true},
		{43005029, "43005001", bibleref.Ref{Book: 43, Chapter: 5, VerseStart: 29, VerseEnd: 29}, true},
		{67001001, "", bibleref.Ref{}, false},
		{43000001, "", bibleref.Ref{}, false},
		{43005000, "", bibleref.Ref{}, false},
	} {
		got, ok := vidRef(c.vid, c.to)
		if ok != c.ok || got != c.want {
			t.Errorf("vidRef(%d, %q) = %+v, %v; want %+v, %v", c.vid, c.to, got, ok, c.want, c.ok)
		}
	}
}

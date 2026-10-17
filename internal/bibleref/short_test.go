package bibleref

import "testing"

// A book's short name is the abbreviation merged after its name, where it is
// shorter, and its name where none is.
func TestShort(t *testing.T) {
	tb := English()
	tb.Merge(map[int][]string{
		66: {"Offenbarung", "Offb.", "Off"},
		19: {"Ps", "Psalmen"},
	})
	for book, want := range map[int]string{66: "Offb.", 19: "Ps", 1: tb.Name(1)} {
		if got := tb.Short(book); got != want {
			t.Errorf("book %d: got %q, want %q", book, got, want)
		}
	}
}

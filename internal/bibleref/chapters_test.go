package bibleref

import (
	"reflect"
	"strings"
	"testing"
)

func TestChapters(t *testing.T) {
	total := 0
	for b := 1; b <= 66; b++ {
		if Chapters(b) < 1 {
			t.Errorf("book %d has no chapters", b)
		}
		total += Chapters(b)
	}
	if total != 1189 {
		t.Errorf("the Bible has %d chapters, want 1189", total)
	}
	for b, want := range map[int]int{1: 50, 19: 150, 29: 3, 39: 4, 43: 21, 66: 22, 0: 0, 67: 0} {
		if got := Chapters(b); got != want {
			t.Errorf("Chapters(%d) = %d, want %d", b, got, want)
		}
	}
}

// A book of one chapter is cited by its verses alone, the way the library
// writes it: "Jude 3" is the third verse of Jude, not a third chapter it does
// not have. The chapter may still be written, and the book alone, or with its
// one chapter, is all of it.
func TestParseSingleChapterBooks(t *testing.T) {
	tbl := English()
	tbl.Merge(map[int][]string{65: {"Judas", "Jud"}, 57: {"Philemon", "Phm"}})
	cases := []struct {
		in   string
		want []Ref
	}{
		{"Jude 3", []Ref{{65, 1, 3, 3}}},
		{"Jude 1:3", []Ref{{65, 1, 3, 3}}},
		{"Jude 3-5", []Ref{{65, 1, 3, 5}}},
		{"Jude 3, 5", []Ref{{65, 1, 3, 3}, {65, 1, 5, 5}}},
		{"Jude 1-4", []Ref{{65, 1, 1, 4}}},
		{"Jude", []Ref{{65, 1, 0, 0}}},
		{"Jude 1", []Ref{{65, 1, 0, 0}}},
		{"Phm 10", []Ref{{57, 1, 10, 10}}},
		{"3Jo 4", []Ref{{64, 1, 4, 4}}},
		{"3 John 4", []Ref{{64, 1, 4, 4}}},
		{"2Jo 5", []Ref{{63, 1, 5, 5}}},
		{"Ob 21", []Ref{{31, 1, 21, 21}}},
		{"Obadiah 1:21", []Ref{{31, 1, 21, 21}}},
		{"Judas 3", []Ref{{65, 1, 3, 3}}}, // a localized name
		{"Jude 3; Joh 3:16", []Ref{{65, 1, 3, 3}, {43, 3, 16, 16}}},
		// books of several chapters keep reading a bare number as a chapter
		{"Joh 3", []Ref{{43, 3, 0, 0}}},
	}
	for _, c := range cases {
		got, err := Parse(c.in, tbl)
		if err != nil {
			t.Errorf("Parse(%q): %v", c.in, err)
			continue
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("Parse(%q) = %+v, want %+v", c.in, got, c.want)
		}
	}
	// a book of several chapters still wants one
	if _, err := Parse("John", tbl); err == nil || !strings.Contains(err.Error(), "missing chapter") {
		t.Errorf("Parse(John): want a missing chapter, got %v", err)
	}
}

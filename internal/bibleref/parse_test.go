package bibleref

import (
	"reflect"
	"testing"
)

func TestParse(t *testing.T) {
	tbl := English()
	cases := []struct {
		in   string
		want []Ref
	}{
		{"Matthew 24:14", []Ref{{40, 24, 14, 14}}},
		{"mt 24:14", []Ref{{40, 24, 14, 14}}},
		{"Matt. 24:14", []Ref{{40, 24, 14, 14}}},
		{"matthew 24", []Ref{{40, 24, 0, 0}}},
		{"John 3:16-18", []Ref{{43, 3, 16, 18}}},
		{"joh3:16", []Ref{{43, 3, 16, 16}}},
		{"1 Corinthians 13:4", []Ref{{46, 13, 4, 4}}},
		{"1co 13:4,7-8", []Ref{{46, 13, 4, 4}, {46, 13, 7, 8}}},
		{"1co13:4", []Ref{{46, 13, 4, 4}}},
		{"I Corinthians 13:4", []Ref{{46, 13, 4, 4}}},
		{"Song of Solomon 2:1", []Ref{{22, 2, 1, 1}}},
		{"40 24:14", []Ref{{40, 24, 14, 14}}},
		{"Ps 83:18", []Ref{{19, 83, 18, 18}}},
		{"Joh 3:16; Ro 5:8", []Ref{{43, 3, 16, 16}, {45, 5, 8, 8}}},
		{"2 Kings 5:1", []Ref{{12, 5, 1, 1}}},
		{"Revelation 21:3-4", []Ref{{66, 21, 3, 4}}},
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
}

func TestParseErrors(t *testing.T) {
	tbl := English()
	for _, in := range []string{"", "Matthew", "Nowhere 3:16", "Matthew 0:1", "John 3:5-2", "John 3:0"} {
		if _, err := Parse(in, tbl); err == nil {
			t.Errorf("Parse(%q) should fail", in)
		}
	}
}

func TestLocalizedMerge(t *testing.T) {
	tbl := English()
	tbl.Merge(map[int][]string{
		40: {"Matthäus", "mat"},
		43: {"Johannes"},
	})
	got, err := Parse("Matthäus 24:14", tbl)
	if err != nil || got[0].Book != 40 {
		t.Errorf("localized parse: %+v, %v", got, err)
	}
	// diacritics-insensitive
	got, err = Parse("matthaus 24:14", tbl)
	if err != nil || got[0].Book != 40 {
		t.Errorf("diacritic-stripped parse: %+v, %v", got, err)
	}
	if tbl.Name(40) != "Matthäus" {
		t.Errorf("display name = %q", tbl.Name(40))
	}
	// English still works
	if _, err := Parse("Matthew 24:14", tbl); err != nil {
		t.Errorf("english after merge: %v", err)
	}
}

func TestRefString(t *testing.T) {
	for ref, want := range map[Ref]string{
		{40, 24, 14, 14}: "Matthew 24:14",
		{40, 24, 3, 14}:  "Matthew 24:3-14",
		{40, 24, 0, 0}:   "Matthew 24",
	} {
		if got := ref.String(); got != want {
			t.Errorf("%+v.String() = %q, want %q", ref, got, want)
		}
	}
}

func TestVerseID(t *testing.T) {
	if VerseID(1, 1, 1) != 1001001 {
		t.Errorf("Genesis 1:1 = %d", VerseID(1, 1, 1))
	}
	if VerseID(43, 3, 16) != 43003016 {
		t.Errorf("John 3:16 = %d", VerseID(43, 3, 16))
	}
}

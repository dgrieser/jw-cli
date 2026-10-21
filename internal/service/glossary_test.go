package service

import (
	"reflect"
	"testing"
)

// An entry's title names its word, its other forms, and what places it; only
// the words are terms.
func TestGlossaryTerms(t *testing.T) {
	for _, tc := range []struct {
		title string
		want  []string
	}{
		{"Abyss", []string{"Abyss"}},
		{"Alabaster jar", []string{"Alabaster jar"}},
		// alternatives
		{"Arabia; Arabian", []string{"Arabia", "Arabian"}},
		{"Zion; Mount Zion", []string{"Zion", "Mount Zion"}},
		{"Aussatz; Aussätziger", []string{"Aussatz", "Aussätziger"}},
		// a form of the word after a comma, not what places it
		{"Bank, Banker", []string{"Bank", "Banker"}},
		{"Burial, Burial Places", []string{"Burial", "Burial Places"}},
		{"Aloe, Aloeholz", []string{"Aloe", "Aloeholz"}},
		{"Amos, Book of", []string{"Amos"}},
		{"Good news, the", []string{"Good news"}},
		{"Arab, II", []string{"Arab"}},
		{"Hort, Fenton John Anthony", []string{"Hort"}},
		{"Appius, Marktplatz des", []string{"Appius"}},
		// brackets
		{"Abijah (King of Judah)", []string{"Abijah"}},
		{"Ancient Near (Middle) East", []string{"Ancient Near East"}},
		{"Celestial Chariot (Eze 1, 10)", []string{"Celestial Chariot"}},
		{"“Peace and Security” (1Th 5:3)", []string{"Peace and Security"}},
		{"„42 Monate“ (Off 11:2)", []string{"42 Monate"}},
		// an ending in brackets
		{"Scripture(s)", []string{"Scripture", "Scriptures"}},
		{"System(s) of things", []string{"System of things", "Systems of things"}},
		{"Ab", []string{"Ab"}},
		{"X", nil},
	} {
		if got := GlossaryTerms(tc.title); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("GlossaryTerms(%q) = %q, want %q", tc.title, got, tc.want)
		}
	}
}

// Tabs of a letter's entries are read; an introduction or an index of
// scriptures is not.
func TestLetterTab(t *testing.T) {
	for title, want := range map[string]bool{"A": true, "Ä": true, "Ch": true, "INTRODUCTION": false, "Bibelstellen": false, "": false} {
		if got := letterTab(title); got != want {
			t.Errorf("letterTab(%q) = %v, want %v", title, got, want)
		}
	}
}

// An entry titled by a word of every day alone, or with its forms, is one;
// one that the title makes another thing, or a topic of its own, is not.
func TestCommonEntry(t *testing.T) {
	common := map[string]bool{"night": true, "bank": true, "time": true, "pen": true, "thunder": true, "work": true}
	for title, want := range map[string]bool{
		"Night":            true,
		"Bank, Banker":     true,
		"Time(s)":          true,
		"Pen, II":          true,
		"Thunder, Sons of": false,
		"Work (Effort)":    false,
		"Abyss":            false,
		"Leader, Noble":    false,
	} {
		if got := commonEntry(title, common); got != want {
			t.Errorf("commonEntry(%q) = %v, want %v", title, got, want)
		}
	}
}

// An entry that says only where to look instead leads there; one that says
// more, or leads to no document, does not.
func TestSeeOnly(t *testing.T) {
	for in, want := range map[string]string{
		`<h1><strong>LAMM</strong></h1><p class="sb">Siehe <a class="it" href="/de/wol/tc/r10/lp-x/1200002671/0">SCHAF</a>.</p><p></p>`:          "/de/wol/tc/r10/lp-x/1200002671/0",
		`<p>See <a href="https://wol.jw.org/en/wol/d/r1/lp-e/1200003981">SHEEP</a>.</p>`:                                                         "https://wol.jw.org/en/wol/d/r1/lp-e/1200003981",
		`<p>Siehe <a href="/de/wol/bc/r10/lp-x/1/0/0">Ri 18:9</a>.</p>`:                                                                          "",
		`<p>Siehe <a href="/de/wol/tc/r10/lp-x/1/0">SCHAF</a> und <a href="/de/wol/tc/r10/lp-x/1/1">ZIEGE</a>.</p>`:                              "",
		`<p>Abneigung gegen Anstrengung oder Arbeit; Müßiggang, Arbeitsscheu, Trägheit. Siehe <a href="/de/wol/tc/r10/lp-x/1/0">FLEISS</a>.</p>`: "",
	} {
		href, ok := seeOnly(in)
		if !ok {
			href = ""
		}
		if href != want {
			t.Errorf("seeOnly(%s) = %q, want %q", in, href, want)
		}
	}
}

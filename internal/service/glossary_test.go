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

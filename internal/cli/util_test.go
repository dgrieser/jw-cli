package cli

import "testing"

func TestMdLinked(t *testing.T) {
	for _, tc := range []struct{ name, text, url, want string }{
		{"link", "Ps. 37:25", "https://wol.example/1", "[Ps. 37:25](https://wol.example/1)"},
		// "1. Kor." would otherwise start an ordered list inside the list item
		{"enumerator with link", "1. Kor. 8:4", "https://wol.example/2", "[1. Kor. 8:4](https://wol.example/2)"},
		{"enumerator without link", "1. Kor. 8:4", "", `1\. Kor. 8:4`},
		{"paren enumerator", "2) Something", "", `2\) Something`},
		{"plain", "Matt 24:14", "", "Matt 24:14"},
		{"brackets in text escaped", "See [note]", "https://wol.example/3", `[See \[note\]](https://wol.example/3)`},
		{"trims space", "  Ps. 1:1  ", "", "Ps. 1:1"},
	} {
		if got := mdLinked(tc.text, tc.url); got != tc.want {
			t.Errorf("%s: mdLinked(%q, %q) = %q, want %q", tc.name, tc.text, tc.url, got, tc.want)
		}
	}
}

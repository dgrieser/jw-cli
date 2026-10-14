package unfold

import "testing"

// A citation naming a page alone continues the one before it, the way an
// index writes "it-2 53, 760, 1000" as a link per page.
func TestContinue(t *testing.T) {
	for _, c := range []struct{ prev, text, want string }{
		{"ijwbq articles 120,", " 147;", "ijwbq articles 147"},
		{"it-2 53,", " 760,", "it-2 760"},
		{"it-2 760", " 1000,", "it-2 1000"},
		{"lff lessons 15,", " 27-28;", "lff lessons 27-28"},
		{"w90 11/15 12,", " 16;", "w90 11/15 16"},
		{"w24.04 2 ¶1,", " 3", "w24.04 2 ¶3"},
		{"w24.04 2 Abs. 1,", " 3", "w24.04 2 Abs. 3"},
		{"jy chapter", " 44", "jy chapter 44"},
		// a citation of its own, or one with nothing before it
		{"it-2 53,", " w24.08 2-6;", "w24.08 2-6"},
		{"", " 523", "523"},
		{"147", "148", "148"},
		{"w14 7/1 7;", "", ""},
	} {
		if got := Continue(c.prev, c.text); got != c.want {
			t.Errorf("Continue(%q, %q) = %q, want %q", c.prev, c.text, got, c.want)
		}
	}
}

// The citations of a fragment are taken in full: a page alone continues the
// publication cited before it, while verses are left as written.
func TestRefsContinueAPageAlone(t *testing.T) {
	refs := Refs(`<p>(` + link("/wol/pc/1/0", "<em>w24.04</em> 2 ¶1,") + link("/wol/pc/1/1", " 3") +
		`; ` + link("/wol/bc/1/2", "4. Mo. 14:1-10,") + link("/wol/bc/1/3", "20-38") + `)</p>`)
	var texts []string
	for _, r := range refs {
		texts = append(texts, r.Text)
	}
	want := []string{"w24.04 2 ¶1", "w24.04 2 ¶3", "4. Mo. 14:1-10,", "20-38"}
	if len(texts) != len(want) {
		t.Fatalf("refs = %q", texts)
	}
	for i := range want {
		if texts[i] != want[i] {
			t.Errorf("ref %d = %q, want %q", i, texts[i], want[i])
		}
	}
}

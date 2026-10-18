package wol

import (
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

// The research guide writes each entry's citation on the subdued line under
// its link; an entry without one is left out.
func TestGuideCitations(t *testing.T) {
	page, err := goquery.NewDocumentFromReader(strings.NewReader(`<article><div class="bodyTxt">
<p class="su"><a href="/de/wol/bc/r10/lp-x/1200029075/0/0"><strong>1:1</strong></a></p>
<div class="du-margin-children-bottom--0"><p><a href="/de/wol/pc/r10/lp-x/1200029075/0/0">Mut in finsteren Zeiten</a></p></div>
<div class="du-color--textSubdued du-fontSize--baseMinus2"><p><em>wcg Kap. 54</em></p></div>
<div class="du-margin-children-bottom--0"><p><a href="/de/wol/pc/r10/lp-x/1200029075/1/0">Die Offenbarung und du</a></p></div>
<div class="du-color--textSubdued"><p><em>w22.05 S. 2 Abs. 1-2</em></p></div>
<div><p><a href="/de/wol/pc/r10/lp-x/1200029075/2/0">Ohne Zeile</a></p></div>
<div><p>Anderes</p></div>
</div></article>`))
	if err != nil {
		t.Fatal(err)
	}
	got := guideCitations(page)
	want := map[string]string{"1200029075/0/0": "wcg Kap. 54", "1200029075/1/0": "w22.05 S. 2 Abs. 1-2"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: got %q, want %q", k, got[k], v)
		}
	}
}

// An entry's place and its guide's document are read off its /pc/ path,
// however it is spelled.
func TestGuidePaths(t *testing.T) {
	for _, p := range []string{
		"/de/wol/pc/r10/lp-x/1200029075/3/0",
		"https://wol.jw.org/de/wol/pc/r10/lp-x/1200029075/3/0",
	} {
		if k := GuideKey(p); k != "1200029075/3/0" {
			t.Errorf("GuideKey(%s) = %q", p, k)
		}
		if u, ok := guideDocURL("https://wol.jw.org", p); !ok || u != "https://wol.jw.org/de/wol/d/r10/lp-x/1200029075" {
			t.Errorf("guideDocURL(%s) = %q, %v", p, u, ok)
		}
	}
	if k := GuideKey("/en/wol/d/r1/lp-e/1102014204"); k != "" {
		t.Errorf("a document is no entry: %q", k)
	}
}

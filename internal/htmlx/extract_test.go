package htmlx

import (
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

func parseSel(t *testing.T, fragment string) *goquery.Selection {
	t.Helper()
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(fragment))
	if err != nil {
		t.Fatal(err)
	}
	return doc.Selection
}

// A wol figure: the illustration, the rights line beside it, the caption below
// it. The pixel size is stated on the tag.
const wolFigure = `<div class="bodyTxt"><figure>
<img src="/en/wol/mp/r1/lp-e/wcg/2025/297" alt="An archaeologist examining clay beehives." width="1200" height="675"/>
<p class="imgCredit">Institute of Archaeology © Tel Rehov Excavations</p>
<figcaption><p>Clay beehives in Israel</p></figcaption>
</figure></div>`

func TestImagesMetadata(t *testing.T) {
	imgs := Images(parseSel(t, wolFigure), "https://wol.jw.org")
	if len(imgs) != 1 {
		t.Fatalf("got %d images, want 1: %+v", len(imgs), imgs)
	}
	got := imgs[0]
	if got.URL != "https://wol.jw.org/en/wol/mp/r1/lp-e/wcg/2025/297" {
		t.Errorf("url = %q", got.URL)
	}
	if got.Caption != "Clay beehives in Israel" {
		t.Errorf("caption = %q", got.Caption)
	}
	if got.Alt != "An archaeologist examining clay beehives." {
		t.Errorf("alt = %q", got.Alt)
	}
	if got.Credit != "Institute of Archaeology © Tel Rehov Excavations" {
		t.Errorf("credit = %q", got.Credit)
	}
	if got.Width != 1200 || got.Height != 675 {
		t.Errorf("size = %dx%d, want 1200x675", got.Width, got.Height)
	}
}

// A jw.org figure states nothing on a tag: the source, the alt text and the
// zoom size are data attributes of a responsive span, and the credit is a
// sibling paragraph.
const jworgFigure = `<div class="bodyTxt"><figure>
<span class="jsRespImg" data-img-att-alt="A map of the journey"
  data-img-size-lg="https://cms-imgp.example/x_lg.jpg"
  data-zoom="https://cms-imgp.example/x_xl.jpg" data-zoom-width="1600" data-zoom-height="900">
  <noscript><img src="https://assets.example/x_xs.jpg" alt="A map of the journey"></noscript>
</span>
<p class="p52 imgCredit">© Example Picture Library</p>
</figure></div>`

func TestImagesResponsiveSpan(t *testing.T) {
	imgs := Images(parseSel(t, jworgFigure), "https://www.jw.org")
	if len(imgs) != 1 {
		t.Fatalf("got %d images, want 1: %+v", len(imgs), imgs)
	}
	got := imgs[0]
	if got.URL != "https://cms-imgp.example/x_xl.jpg" {
		t.Errorf("url = %q, want the zoom rendition", got.URL)
	}
	if got.Alt != "A map of the journey" {
		t.Errorf("alt = %q", got.Alt)
	}
	if got.Credit != "© Example Picture Library" {
		t.Errorf("credit = %q", got.Credit)
	}
	if got.Width != 1600 || got.Height != 900 {
		t.Errorf("size = %dx%d, want 1600x900", got.Width, got.Height)
	}
}

// Two figures in a row: neither may borrow the other's caption or credit.
func TestImagesMetadataStaysWithItsFigure(t *testing.T) {
	const two = `<div class="bodyTxt">
<figure><img src="/a.jpg" alt="a"/><p class="imgCredit">Credit A</p><figcaption>Caption A</figcaption></figure>
<figure><img src="/b.jpg" alt="b"/></figure>
</div>`
	imgs := Images(parseSel(t, two), "https://wol.jw.org")
	if len(imgs) != 2 {
		t.Fatalf("got %d images, want 2", len(imgs))
	}
	if imgs[0].Credit != "Credit A" || imgs[0].Caption != "Caption A" {
		t.Errorf("first figure: %+v", imgs[0])
	}
	if imgs[1].Credit != "" || imgs[1].Caption != "" {
		t.Errorf("second figure borrowed metadata: %+v", imgs[1])
	}
}

// A citation whose link spells only what changes from the one before it is
// listed as the full reference; one naming its book stands as it is.
func TestScriptureRefsContinue(t *testing.T) {
	const wolDoc = `<p>(<a href="/en/wol/bc/r1/lp-e/1/7/0" data-bid="8-1" class="b">Nu 14:5,</a>
<a href="/en/wol/bc/r1/lp-e/1/7/1" data-bid="8-2" class="b">20-38</a>;
<a href="/en/wol/bc/r1/lp-e/1/8/0" data-bid="9-1" class="b">1 Sam. 18:5</a>,
<a href="/en/wol/bc/r1/lp-e/1/8/1" data-bid="9-2" class="b">9-11</a>;
<a href="/en/wol/bc/r1/lp-e/1/9/0" data-bid="10-1" class="b">Ps. 83</a>,
<a href="/en/wol/bc/r1/lp-e/1/9/1" data-bid="10-2" class="b">84</a>;
<a href="/en/wol/bc/r1/lp-e/1/10/0" data-bid="11-1" class="b">Pr 8:30–9:2</a>,
<a href="/en/wol/bc/r1/lp-e/1/10/1" data-bid="11-2" class="b">6</a>)</p>`
	refs := ScriptureRefs(parseSel(t, wolDoc), "https://wol.jw.org")
	want := []string{"Nu 14:5", "Nu 14:20-38", "1 Sam. 18:5", "1 Sam. 18:9-11", "Ps. 83", "Ps. 84", "Pr 8:30–9:2", "Pr 9:6"}
	if len(refs) != len(want) {
		t.Fatalf("got %d references, want %d: %+v", len(refs), len(want), refs)
	}
	for i, w := range want {
		if refs[i].Text != w {
			t.Errorf("reference %d = %q, want %q", i, refs[i].Text, w)
		}
	}
	if refs[1].BID != "8-2" || refs[1].BCPath != "https://wol.jw.org/en/wol/bc/r1/lp-e/1/7/1" {
		t.Errorf("continued reference lost its link: %+v", refs[1])
	}
}

func TestCompleteCitation(t *testing.T) {
	for _, c := range []struct{ text, prev, want string }{
		{"20-38", "", "20-38"}, // nothing to lend it the rest
		{"John 3:16", "Nu 14:5", "John 3:16"},
		{"2 John 5", "Nu 14:5", "2 John 5"},
		{"6:31-34", "Mark 1:40, 41", "Mark 6:31-34"},
		{"5, 6", "Isaiah 35:1", "Isaiah 35:5, 6"},
		{"16", "約翰福音3：15", "約翰福音3：16"}, // the full-width colon
	} {
		if got := completeCitation(c.text, c.prev); got != c.want {
			t.Errorf("completeCitation(%q, %q) = %q, want %q", c.text, c.prev, got, c.want)
		}
	}
}

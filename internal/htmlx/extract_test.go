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

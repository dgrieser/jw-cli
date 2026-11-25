package jworg

import (
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

const whatsNewPage = `<html><body>
<div class="synopsis textOnly first last currentPage pub-ijw docId-1011267 WhatsNewPage">
  <h3><a href="/de/neu-auf-unserer-website/">Neu auf jw.org</a></h3>
</div>
<div class="synopsis borderDotted news pub-ijwnews26 docId-702026178 docClass-137 NewsArticlePage">
  <div class="syn-img sqs"><a href="/de/nachrichten/x/"><span class='jsRespImg' data-img-size-xs='https://img/xs.jpg' data-img-size-md='https://img/md.jpg'></span></a></div>
  <div class="syn-body"><p class="meta pubDate">2026-11-24</p><p class="contextTitle"> NACHRICHTEN </p>
  <h3><a href="/de/nachrichten/x/"> Ein Jahrhundert </a></h3></div>
</div>
<div class="synopsis borderDotted publications pub-gwt docId-1102026536 docClass-13 PublicationArticle">
  <div class="syn-body"><p class="meta pubDate">2026-11-23</p><p class="contextTitle">GOTTES WORT IST WAHRHEIT</p>
  <h3><a href="/de/bibliothek/buecher/gottes-wort-ist-wahrheit/zephanja/">Bibelbuch 36: Zephanja</a></h3></div>
</div>
<div class="synopsis borderDotted publications pub-w ml-X PublicationIssue">
  <div class="syn-body"><p class="meta pubDate">2026-11-17</p>
  <h3><a href="/de/bibliothek/zeitschriften/wachtturm-studienausgabe-februar-2027/">Februar 2027</a></h3></div>
</div>
<div class="synopsis borderDotted docId-1 VideoPage">
  <div class="syn-body"><p class="meta pubDate">2026-11-10</p>
  <h3><a href="/de/bibliothek/videos/#de/mediaitems/LatestVideos/pub-jwb-142_1_VIDEO">JW Broadcasting</a></h3></div>
</div>
</body></html>`

func TestParseWhatsNew(t *testing.T) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(whatsNewPage))
	if err != nil {
		t.Fatal(err)
	}
	items := parseWhatsNew(doc, "https://www.jw.org")
	if len(items) != 4 {
		t.Fatalf("got %d items, want 4 (the page's own entry is none): %+v", len(items), items)
	}
	n := items[0]
	if n.Kind != "news" || n.Title != "Ein Jahrhundert" || n.Context != "NACHRICHTEN" ||
		n.URL != "https://www.jw.org/de/nachrichten/x/" || n.Published != "2026-11-24" ||
		n.ImageURL != "https://img/md.jpg" || n.DocID != 702026178 {
		t.Errorf("news: %+v", n)
	}
	if p := items[1]; p.Kind != "publication" || p.DocID != 1102026536 {
		t.Errorf("publication: %+v", p)
	}
	if i := items[2]; i.Kind != "issue" {
		t.Errorf("issue: %+v", i)
	}
	if v := items[3]; v.Kind != "video" || v.LANK != "pub-jwb-142_1_VIDEO" {
		t.Errorf("video: %+v", v)
	}
}

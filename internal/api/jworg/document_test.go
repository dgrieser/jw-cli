package jworg

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/dgrieser/jw-cli/internal/httpx"
	"github.com/dgrieser/jw-cli/internal/model"
)

// songPage is a song's page as jw.org writes it: the stanzas a list, a
// chorus and a bridge after a stanza, a lead sheet to download above them.
const songPage = `<html><body><main><article id="article" class="jwac docClass-31 docId-502900137 pub-osg">
<div class="textSizeIncrement"><header>
<p class="contextTtl" id="p1" data-pid="1"><span class="pageNum" data-no="7"></span><strong>SONG 1</strong></p>
<h1 id="p2" data-pid="2">Rise   Again</h1></header></div>
<p id="p3" data-pid="3" class="themeScrp">(<a href="/x">Revelation 4:11</a>)</p>
<div id="docSubImgHTML"><div class="bodyTxt">
<p id="p20" data-pid="20"><strong>Download:</strong></p>
<ul><li><p id="p21" data-pid="21"><a href="https://b.jw-cdn.org/apis/pub-media/GETPUBMEDIALINKS?docid=502900137&amp;output=html&amp;fileformat=PDF&amp;track=1" class="jsDownload" data-jsonurl="https://b.jw-cdn.org/apis/pub-media/GETPUBMEDIALINKS?docid=502900137&amp;output=json&amp;fileformat=PDF&amp;track=1">Lead Sheet</a></p></li></ul>
<ol class="source"><li>
<p id="p4" data-pid="4" class="sl"><span class="txtSrcBullet">1. </span>My friend, I know it’s tough</p>
<p id="p5" data-pid="5">To say goodbye.</p>
<div class="chorus"><p id="p8" data-pid="8"><strong>(CHORUS)</strong></p>
<p id="p9" data-pid="9">You are waiting</p></div>
<p id="p10" data-pid="10">One line more.</p>
</li><li>
<p id="p11" data-pid="11"><span class="txtSrcBullet">2. </span>Now each day</p>
<div class="chorus"><p id="p12" data-pid="12"><strong>(BRIDGE)</strong></p>
<p id="p13" data-pid="13">One day <strong>soon</strong></p></div>
</li></ol></div>
<div class="closingContent"><p id="p16" data-pid="16">(See also <a>Ps. 36:9</a>.)</p></div>
</div>
<div id="docSubImg"><figure><span class="jsRespImg" data-img-size-lg="https://img.example/sub_lg.jpg" data-zoom="https://img.example/sub_xl.jpg"></span></figure></div>
</article></main></body></html>`

func parsePage(t *testing.T, page string) model.MediaDocument {
	t.Helper()
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(page))
	if err != nil {
		t.Fatal(err)
	}
	return parseDocument(doc)
}

func TestParseSong(t *testing.T) {
	d := parsePage(t, songPage)
	if d.Context != "SONG 1" || d.Title != "Rise Again" || d.Theme != "(Revelation 4:11)" || d.Closing != "(See also Ps. 36:9.)" {
		t.Errorf("frame = %q / %q / %q / %q", d.Context, d.Title, d.Theme, d.Closing)
	}
	if d.PrintedEdition != "https://img.example/sub_xl.jpg" {
		t.Errorf("printed edition = %q", d.PrintedEdition)
	}
	if len(d.Downloads) != 1 || d.Downloads[0].Label != "Lead Sheet" || !strings.Contains(d.Downloads[0].URL, "output=json") {
		t.Errorf("downloads = %+v", d.Downloads)
	}
	want := []model.TextBlock{
		{Kind: model.BlockStanza, Label: "1.", Lines: []model.TextLine{{PID: 4, Text: "My friend, I know it’s tough"}, {PID: 5, Text: "To say goodbye."}}},
		{Kind: model.BlockChorus, Label: "(CHORUS)", Lines: []model.TextLine{{PID: 9, Text: "You are waiting"}}},
		{Kind: model.BlockStanza, Lines: []model.TextLine{{PID: 10, Text: "One line more."}}},
		{Kind: model.BlockStanza, Label: "2.", Lines: []model.TextLine{{PID: 11, Text: "Now each day"}}},
		{Kind: model.BlockChorus, Label: "(BRIDGE)", Lines: []model.TextLine{{PID: 13, Text: "One day soon"}}},
	}
	if !reflect.DeepEqual(d.Blocks, want) {
		t.Errorf("blocks:\n got %+v\nwant %+v", d.Blocks, want)
	}
	if !d.Lyrics() {
		t.Error("a song's page is lyrics")
	}
}

// An older song's page numbers its stanzas inside the lines.
func TestParseNumberedSong(t *testing.T) {
	d := parsePage(t, `<article id="article" class="jwac docClass-31"><header><h1>Song</h1></header>
<div class="bodyTxt"><div class="pGroup">
<p data-pid="4">1. Jehovah God,</p><p data-pid="5">Source of all life.</p>
<p data-pid="8">2. Your lofty throne,</p></div></div></article>`)
	if len(d.Blocks) != 2 || d.Blocks[0].Label != "1." || d.Blocks[0].Lines[0].Text != "Jehovah God," ||
		len(d.Blocks[0].Lines) != 2 || d.Blocks[1].Label != "2." || !d.Lyrics() {
		t.Errorf("blocks = %+v", d.Blocks)
	}
}

// Any other page is its headings and paragraphs; numbered paragraphs of a
// page that is no song stay paragraphs.
func TestParseArticle(t *testing.T) {
	d := parsePage(t, `<article id="article" class="jwac docClass-40"><header><h1>Article</h1></header>
<div class="bodyTxt">
<p data-pid="2"><strong>Download:</strong></p><ul><li><p data-pid="3"><a class="jsDownload" href="x">PDF</a></p></li></ul>
<h2 data-pid="4">A heading</h2>
<p data-pid="5">1. A paragraph<sup>a</sup>.</p>
<figure><figcaption><p data-pid="6">A caption</p></figcaption></figure>
<p data-pid="7" class="displayNone"> </p>
</div></article>`)
	want := []model.TextBlock{
		{Kind: model.BlockHeading, Lines: []model.TextLine{{PID: 4, Text: "A heading"}}},
		{Kind: model.BlockParagraph, Lines: []model.TextLine{{PID: 5, Text: "1. A paragraph."}}},
	}
	if !reflect.DeepEqual(d.Blocks, want) || d.Lyrics() {
		t.Errorf("blocks:\n got %+v\nwant %+v", d.Blocks, want)
	}
}

func TestDocument(t *testing.T) {
	var asked string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = r.URL.String()
		w.Write([]byte(songPage))
	}))
	defer srv.Close()
	c := New(httpx.New(httpx.WithBaseURLs(httpx.BaseURLs{CDN: srv.URL, JWOrg: srv.URL, WOL: srv.URL})))
	d, err := c.Document(context.Background(), "X", 502900137)
	if err != nil {
		t.Fatal(err)
	}
	if asked != "/finder?docid=502900137&wtlocale=X" || d.DocID != 502900137 || d.URL != srv.URL+asked || len(d.Blocks) != 5 {
		t.Errorf("asked %q, got %+v", asked, d)
	}
}

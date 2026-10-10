package wol

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

func bookDocsClient(t *testing.T) *Client {
	mux := http.NewServeMux()
	mux.HandleFunc("/en/wol/bibledocument/r1/lp-e/nwtsty/40/introduction", serveFile(t, "testdata/intro_mt.html"))
	mux.HandleFunc("/en/wol/bibledocument/r1/lp-e/nwtsty/1/introduction", serveFile(t, "testdata/intro_gen.html"))
	mux.HandleFunc("/en/wol/d/r1/lp-e/1001070071", serveFile(t, "testdata/book_table.html"))
	mux.HandleFunc("/en/wol/gallery/r1/lp-e/nwtsty/40", serveFile(t, "testdata/gallery_mt.html"))
	// the video link answers with JSON, whichever track is asked for here
	mux.HandleFunc("/wol/vidlink/r1/lp-e", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("pub") != "nwtsv" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		serveFile(t, "testdata/vidlink_gen.json")(w, r)
	})
	return testClient(t, mux)
}

func TestIntroductionWithText(t *testing.T) {
	c := bookDocsClient(t)
	intro, err := c.Introduction(context.Background(), c.hc.Base.WOL+"/en/wol/bibledocument/r1/lp-e/nwtsty/40/introduction")
	if err != nil {
		t.Fatal(err)
	}
	if intro.Title != "Introduction to Matthew" {
		t.Errorf("title = %q", intro.Title)
	}
	want := []string{"Writer=Matthew", "Place Written=Israel", "Writing Completed=c. 41 C.E.", "Time Covered=2 B.C.E.–33 C.E."}
	var got []string
	for _, f := range intro.Facts {
		got = append(got, f.Label+"="+f.Value)
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("facts = %q, want %q", got, want)
	}
	if !strings.Contains(intro.NotesHTML, "Noteworthy Facts") || !strings.Contains(intro.NotesHTML, "tax collector") {
		t.Errorf("notes = %.200q", intro.NotesHTML)
	}
	if intro.Video == nil || len(intro.Video.Files) == 0 {
		t.Fatalf("video = %+v", intro.Video)
	}
	if !strings.HasSuffix(intro.Video.Poster, "/en/wol/mp/r1/lp-e/nwtsty/2026/323") {
		t.Errorf("poster = %q", intro.Video.Poster)
	}
}

func TestIntroductionVideoOnly(t *testing.T) {
	c := bookDocsClient(t)
	intro, err := c.Introduction(context.Background(), c.hc.Base.WOL+"/en/wol/bibledocument/r1/lp-e/nwtsty/1/introduction")
	if err != nil {
		t.Fatal(err)
	}
	if intro.Title != "Introduction to Genesis" || len(intro.Facts) != 0 || intro.NotesHTML != "" {
		t.Errorf("intro = %+v", intro)
	}
	v := intro.Video
	if v == nil || v.Title != "Introduction to Genesis" {
		t.Fatalf("video = %+v", v)
	}
	f := v.Files[0]
	if !strings.HasSuffix(f.URL, ".mp4") || f.Label != "240p" || f.FrameHeight != 234 || f.Duration < 390 {
		t.Errorf("file = %+v", f)
	}
	if !strings.HasSuffix(f.SubtitlesURL, "nwtsv_E_010.vtt") {
		t.Errorf("subtitles = %q", f.SubtitlesURL)
	}
}

func TestBookFacts(t *testing.T) {
	facts, err := bookDocsClient(t).BookFacts(context.Background(), cfgEN)
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 66 {
		t.Fatalf("books = %d", len(facts))
	}
	got := func(book int) string {
		var s []string
		for _, f := range facts[book] {
			s = append(s, f.Label+"="+f.Value)
		}
		return strings.Join(s, "|")
	}
	if g, want := got(1), "Writer(s)=Moses|Place written=Wilderness|Writing completed (B.C.E.)=1513|"+
		"Time covered (B.C.E.)=“In the beginning” to 1657"; g != want {
		t.Errorf("Genesis:\n got %s\nwant %s", g, want)
	}
	// the Christian Greek Scriptures are a table of their own
	if g := got(40); !strings.Contains(g, "Writing completed (C.E.)=c. 41") {
		t.Errorf("Matthew: %s", g)
	}
	// an empty cell is no fact
	if g := got(19); strings.Contains(g, "Place written") {
		t.Errorf("Psalms: %s", g)
	}
}

func TestFactLabelAndKey(t *testing.T) {
	for head, want := range map[string]string{
		"WRITER(S)":                  "Writer(s)",
		"WRITING COMPLETED (B.C.E.)": "Writing completed (B.C.E.)",
		"WANN FERTIG (V. U. Z.)":     "Wann fertig (V. U. Z.)",
		"ZEITSPANNE":                 "Zeitspanne",
	} {
		if got := factLabel(head); got != want {
			t.Errorf("%q: got %q, want %q", head, got, want)
		}
	}
	// the introduction's labels and the table's headings name the same facts
	for _, pair := range [][2]string{
		{"Writer", "Writer(s)"},
		{"Writing Completed", "Writing completed (B.C.E.)"},
		{"Wann fertig", "Wann fertig (U. Z.)"},
	} {
		if FactKey(pair[0]) != FactKey(pair[1]) {
			t.Errorf("%q and %q differ: %q, %q", pair[0], pair[1], FactKey(pair[0]), FactKey(pair[1]))
		}
	}
}

func TestGallery(t *testing.T) {
	c := bookDocsClient(t)
	g, err := c.Gallery(context.Background(), c.hc.Base.WOL+"/en/wol/gallery/r1/lp-e/nwtsty/40")
	if err != nil {
		t.Fatal(err)
	}
	if g.Title != "Media Gallery - Matthew" || len(g.Groups) != 3 {
		t.Fatalf("gallery = %q, %d groups", g.Title, len(g.Groups))
	}
	first := g.Groups[0]
	if first.Heading != "Matthew 1" || len(first.Items) != 3 {
		t.Fatalf("first group = %+v", first)
	}
	v := first.Items[0]
	if !v.Video || v.Title != "Video Introduction to the Book of Matthew" ||
		!strings.HasSuffix(v.URL, "/nwtsty/40/1001072001#chapter=1") || !strings.HasSuffix(v.Thumbnail, "/thumbnail") {
		t.Errorf("video tile = %+v", v)
	}
	if first.Items[1].Video {
		t.Errorf("picture tile marked as video: %+v", first.Items[1])
	}
}

func TestGalleryItemVideo(t *testing.T) {
	page := `<div class="gallerySelectedItem"><div class="mediaTitle wide"><h2><p>Video Introduction</p></h2></div>
<div class="mediaContent video"><div class="mediaWrapper"><div class="videoContainer"
 data-json-src="/wol/vidlink/r1/lp-e?pub=nwtsv&amp;track=400&amp;style=chromeless" data-img-src="/en/wol/mp/r1/lp-e/nwtsty/2026/523"></div></div></div></div>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(page))
	if err != nil {
		t.Fatal(err)
	}
	asset, err := parseGalleryItem(doc.Selection, "https://wol.example")
	if err != nil {
		t.Fatal(err)
	}
	if asset.VideoSource != "https://wol.example/wol/vidlink/r1/lp-e?pub=nwtsv&track=400&style=chromeless" ||
		asset.ThumbnailURL != "https://wol.example/en/wol/mp/r1/lp-e/nwtsty/2026/523" || asset.Caption != "Video Introduction" {
		t.Errorf("asset = %+v", asset)
	}
}

package wol

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func chapterClient(t *testing.T) *Client {
	mux := http.NewServeMux()
	mux.HandleFunc("/en/wol/b/r1/lp-e/nwtsty/43/3", serveFile(t, "testdata/chapter_john3.html"))
	mux.HandleFunc("/en/wol/marginalreference/r1/lp-e/nwtsty/43/3/96", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<ul><li><span class="v">For God so loved... (Ge 22:2)</span></li></ul>`))
	})
	mux.HandleFunc("/en/wol/pc/r1/lp-e/1204433/5/0", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Requested-With") != "XMLHttpRequest" {
			http.Error(w, "expected XHR", 400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"did": 1204433, "items": [{
			"content": "<p>Jehovah loved the world of redeemable mankind...</p>",
			"title": "God So Loved the World",
			"url": "/en/wol/d/r1/lp-e/2014486",
			"publicationTitle": "The Watchtower, 7/1/2014"
		}]}`))
	})
	return testClient(t, mux)
}

var cfgEN = Config{Locale: "en", Rsconf: "r1", Lp: "lp-e"}

func TestChapterVerses(t *testing.T) {
	c := chapterClient(t)
	doc, err := c.Chapter(context.Background(), cfgEN, "nwtsty", 43, 3)
	if err != nil {
		t.Fatal(err)
	}

	// single verse
	verses, err := doc.Verses(16, 16)
	if err != nil {
		t.Fatal(err)
	}
	if len(verses) != 1 || verses[0].ID != 43003016 {
		t.Fatalf("verses = %+v", verses)
	}
	if !strings.Contains(verses[0].HTML, "God loved the world so much") {
		t.Errorf("verse text missing: %s", verses[0].HTML)
	}

	// multi-segment verse is concatenated
	verses, err = doc.Verses(17, 17)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(verses[0].HTML, "did not send his Son") ||
		!strings.Contains(verses[0].HTML, "second segment") {
		t.Errorf("segments not concatenated: %s", verses[0].HTML)
	}

	// range and whole chapter
	if vs, _ := doc.Verses(16, 17); len(vs) != 2 {
		t.Errorf("range: got %d verses", len(vs))
	}
	if vs, _ := doc.Verses(0, 0); len(vs) != 2 {
		t.Errorf("whole chapter: got %d verses", len(vs))
	}
	if _, err := doc.Verses(99, 99); err == nil {
		t.Error("want error for missing verse")
	}
}

func TestStudySection(t *testing.T) {
	c := chapterClient(t)
	doc, err := c.Chapter(context.Background(), cfgEN, "nwtsty", 43, 3)
	if err != nil {
		t.Fatal(err)
	}
	sec, ok := doc.StudySection(16)
	if !ok {
		t.Fatal("study section for v16 not found")
	}
	if sec.Verse != "John 3:16" {
		t.Errorf("verse label = %q", sec.Verse)
	}
	if len(sec.Notes) != 2 || sec.Notes[0].Lemma != "loved:" {
		t.Errorf("notes = %+v", sec.Notes)
	}
	if !strings.Contains(sec.Notes[0].HTML, "a·ga·paʹo") {
		t.Errorf("note html: %s", sec.Notes[0].HTML)
	}
	if len(sec.XRefs) != 1 {
		t.Fatalf("xrefs = %+v", sec.XRefs)
	}
	x := sec.XRefs[0]
	if !strings.Contains(x.Citation, "Ge 22:2, 16") || !strings.Contains(x.SourcePath, "/marginalreference/") {
		t.Errorf("xref = %+v", x)
	}
	if len(sec.Media) != 1 {
		t.Fatalf("media = %+v", sec.Media)
	}
	m := sec.Media[0]
	if !strings.Contains(m.URL, "/thumbnail") || !strings.Contains(m.Caption, "Jesus explains birth") ||
		!strings.Contains(m.FinderLink, "lank=pub-gnj_3_VIDEO") {
		t.Errorf("media = %+v", m)
	}
	if len(sec.Research) != 2 {
		t.Fatalf("research = %+v", sec.Research)
	}
	if sec.Research[0].PCPath == "" || !strings.Contains(sec.Research[0].Title, "God So Loved") {
		t.Errorf("research[0] = %+v", sec.Research[0])
	}
	if sec.Research[1].ArticleURL == "" {
		t.Errorf("research[1] should have a direct article URL: %+v", sec.Research[1])
	}

	// verse 17 has only xrefs
	sec17, ok := doc.StudySection(17)
	if !ok || len(sec17.XRefs) != 1 || len(sec17.Notes) != 0 {
		t.Errorf("sec17 = %+v ok=%v", sec17, ok)
	}
	if _, ok := doc.StudySection(99); ok {
		t.Error("no section expected for v99")
	}
}

func TestMarginalReferenceAndTooltip(t *testing.T) {
	c := chapterClient(t)
	ctx := context.Background()
	html, err := c.MarginalReference(ctx, c.hc.Base.WOL+"/en/wol/marginalreference/r1/lp-e/nwtsty/43/3/96")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html, "For God so loved") {
		t.Errorf("marginal html: %s", html)
	}
	tip, err := c.Tooltip(ctx, c.hc.Base.WOL+"/en/wol/pc/r1/lp-e/1204433/5/0")
	if err != nil {
		t.Fatal(err)
	}
	if tip.Title != "God So Loved the World" || !strings.Contains(tip.ContentHTML, "redeemable mankind") {
		t.Errorf("tooltip = %+v", tip)
	}
	if !strings.HasPrefix(tip.URL, c.hc.Base.WOL) {
		t.Errorf("tooltip url not absolutized: %s", tip.URL)
	}
}

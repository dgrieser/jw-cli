package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dgrieser/jw-cli/internal/httpx"
	"github.com/dgrieser/jw-cli/internal/model"
)

// talk is a transcript in paragraphs: a pause after a sentence ends one.
// Paragraph n says "Paragraph n" and then words words.
func talk(paras []string) []model.Cue {
	var cues []model.Cue
	at := 0.0
	for _, p := range paras {
		for line := range strings.SplitSeq(p, "|") {
			cues = append(cues, model.Cue{Start: at, End: at + 2, Text: line})
			at += 2
		}
		at += 3 // the pause
	}
	return cues
}

func filler(n int) string {
	return strings.TrimSpace(strings.Repeat("word ", n)) + "."
}

func TestTranscriptExcerpt(t *testing.T) {
	cues := talk([]string{
		"First paragraph. " + filler(200),
		"Second paragraph. " + filler(100),
		"Third paragraph. " + filler(100),
		"Pablo, who was inactive for 30 years,|says: “I came back.”|Let us read together|James 5:19, 20.",
		"There it says: “My brothers, if anyone is misled.”|" + filler(60),
		"Sixth paragraph. " + filler(60),
		"Seventh paragraph. " + filler(60),
		"Eighth paragraph. " + filler(60),
	})
	// the search joins the captions' lines and cuts where the verse is named
	snippet := "inactive for 30 years, says:\n“I came back.” Let us read together James 5:19, 20."
	got, ok := transcriptExcerpt(snippet, cues, "James 5:19")
	if !ok {
		t.Fatal("the passage should be found")
	}
	paras := strings.Split(strings.TrimSuffix(strings.TrimPrefix(got, "<p>"), "</p>"), "</p><p>")
	// before: the second and third paragraphs make the 150 words; after: the
	// fifth, sixth and seventh do
	if len(paras) != 6 || !strings.HasPrefix(paras[0], "Second paragraph.") || !strings.HasPrefix(paras[5], "Seventh paragraph.") {
		t.Fatalf("paragraphs = %d: %.300q", len(paras), paras)
	}
	if !strings.HasPrefix(paras[2], "Pablo, who was inactive") ||
		!strings.Contains(paras[2], "Let us read together <mark>James 5:19, 20</mark>.") {
		t.Errorf("the passage's own paragraph whole, the reference marked: %q", paras[2])
	}
	if !strings.Contains(paras[2], "&#34;") && !strings.Contains(paras[2], "“I came back.”") {
		t.Errorf("text should be escaped as HTML: %q", paras[2])
	}
}

// A passage the transcript does not hold, or cut mid-word, is found by its
// ends or not at all.
func TestTranscriptExcerptLocate(t *testing.T) {
	cues := talk([]string{"One two three four five six seven eight nine ten eleven twelve."})
	for snippet, want := range map[string]bool{
		"ne two three four five": true, // cut mid-word at the start
		"two three <b>four</b>":  true, // markup and case aside
		"One two three four five XXX seven eight nine ten eleven twelve": true, // its ends match
		"something else entirely": false,
		"":                        false,
	} {
		if _, ok := transcriptExcerpt(snippet, cues, "John 3:16"); ok != want {
			t.Errorf("transcriptExcerpt(%q) found = %v, want %v", snippet, ok, want)
		}
	}
}

// A video found by the verse in its title has no passage: the excerpt is
// where its transcript first says the verse.
func TestTranscriptExcerptNoSnippet(t *testing.T) {
	cues := talk([]string{"Hello there.", "As John 3:16 says, God loved the world.", "The end."})
	got, ok := transcriptExcerpt("", cues, "Joh 3:16")
	if !ok || got != "<p>Hello there.</p><p>As <mark>John 3:16</mark> says, God loved the world.</p><p>The end.</p>" {
		t.Errorf("got %q, %v", got, ok)
	}
	if _, ok := transcriptExcerpt("", cues, "Romans 5:12"); ok {
		t.Error("a verse the transcript never says has no passage")
	}
}

func TestRefMarker(t *testing.T) {
	cases := []struct{ ref, text, want string }{
		{"Jakobus 5:19", "Lesen wir Jakobus 5:19, 20.", "Lesen wir <mark>Jakobus 5:19, 20</mark>."},
		{"1 John 4:8", "as 1. John 4:8 says", "as <mark>1. John 4:8</mark> says"},
		{"Matthew 24:14", "Matthew 24:14-16 and 24:140", "<mark>Matthew 24:14-16</mark> and 24:140"},
		{"Jak. 5:19", "chapter 15:19 and 5:19", "chapter 15:19 and <mark>5:19</mark>"},
	}
	for _, c := range cases {
		if got := refMarker(c.ref).ReplaceAllString(c.text, "$1<mark>$2</mark>"); got != c.want {
			t.Errorf("%s in %q = %q, want %q", c.ref, c.text, got, c.want)
		}
	}
	if refMarker("Psalm 23") != nil {
		t.Error("a chapter names no verse to mark")
	}
}

// A cited video's excerpt is widened from its subtitles, read through the
// mediator's item; one whose subtitles cannot be read keeps the search's.
func TestDateVideosWidensExcerpt(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/apis/mediator/v1/media-items/E/", func(w http.ResponseWriter, r *http.Request) {
		lank := strings.TrimPrefix(r.URL.Path, "/apis/mediator/v1/media-items/E/")
		vtt := "/talk.vtt"
		if lank == "pub-b_1_VIDEO" {
			vtt = "/missing.vtt"
		}
		fmt.Fprintf(w, `{"media": [{"languageAgnosticNaturalKey": %q, "firstPublished": "2026-09-25T10:00:00Z",
			"files": [{"label": "720p", "subtitles": {"url": "http://%s%s"}}]}]}`, lank, r.Host, vtt)
	})
	mux.HandleFunc("/talk.vtt", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "WEBVTT\n\n00:00:01.000 --> 00:00:03.000\nBefore it.\n\n"+
			"00:00:05.000 --> 00:00:07.000\nLet us read James 5:19.\n\n"+
			"00:00:10.000 --> 00:00:12.000\nIt says: “My brothers.”\n")
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	hc := httpx.New(httpx.WithBaseURLs(httpx.BaseURLs{CDN: srv.URL, JWOrg: srv.URL, WOL: srv.URL}))
	s := New(hc, httpx.OpenCacheAt(t.TempDir()))

	videos := []model.Result{
		{LANK: "pub-a_1_VIDEO", Snippet: "Let us read James 5:19.", Excerpt: "<p>Let us read James 5:19.</p>"},
		{LANK: "pub-b_1_VIDEO", Snippet: "Let us read James 5:19.", Excerpt: "<p>Let us read James 5:19.</p>"},
	}
	out := s.dateVideos(context.Background(), model.Language{Symbol: "E"}, videos, []string{"James 5:19", "James 5:19"})
	want := "<p>Before it.</p><p>Let us read <mark>James 5:19</mark>.</p><p>It says: “My brothers.”</p>"
	if out[0].Excerpt != want || out[0].Context != "2026-09-25" {
		t.Errorf("widened = %q (%s)", out[0].Excerpt, out[0].Context)
	}
	if out[1].Excerpt != videos[1].Excerpt {
		t.Errorf("without subtitles the search's passage stays: %q", out[1].Excerpt)
	}
	// without excerpts, nothing is widened
	out = s.dateVideos(context.Background(), model.Language{Symbol: "E"}, videos[:1], nil)
	if out[0].Excerpt != videos[0].Excerpt {
		t.Errorf("no refs, no widening: %q", out[0].Excerpt)
	}
}

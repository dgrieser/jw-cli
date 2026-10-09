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

func TestParseTrack(t *testing.T) {
	cases := map[string]track{
		"pub-sjjm_1_VIDEO":        {Pub: "sjjm", Track: 1},
		"pub-jwb_202401_1_VIDEO":  {Pub: "jwb", Issue: "202401", Track: 1},
		"pub-iam-4_18_AUDIO":      {Pub: "iam-4", Track: 18},
		"pub-jwb-142_1_VIDEO":     {Pub: "jwb-142", Track: 1},
		"docid-502014202_1_VIDEO": {DocID: 502014202, Track: 1},
	}
	for lank, want := range cases {
		if got, ok := parseTrack(lank); !ok || got != want {
			t.Errorf("parseTrack(%q) = %+v, %v; want %+v", lank, got, ok, want)
		}
	}
	for _, lank := range []string{"", "pub-sjjm_VIDEO", "sjjm_1_VIDEO", "pub-sjjm_0_AUDIO"} {
		if _, ok := parseTrack(lank); ok {
			t.Errorf("parseTrack(%q) should fail", lank)
		}
	}
}

// textUpstream is pub-media, jw.org's finder and a subtitles file: the
// songbook's song 1, timed by the choir's recording; the page a children's
// recording of it names, which has no lyrics; a video without a document.
func textUpstream(t *testing.T) (*Service, string) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/apis/pub-media/GETPUBMEDIALINKS", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("fileformat") != "MP3" || q.Get("langwritten") != "E" {
			http.Error(w, "unexpected query "+r.URL.RawQuery, http.StatusBadRequest)
			return
		}
		mp3 := map[string]string{
			"sjjc": `{"title": "1. Song", "track": 1, "docid": 1102016801, "duration": 158.98,
				"file": {"url": "https://cdn.example/sjjc_E_001.mp3", "checksum": "c-sjjc"},
				"markers": {"documentId": 1102016801, "markers": [
					{"mepsParagraphId": 4, "startTime": "00:00:16.016", "duration": "00:00:08.020"},
					{"mepsParagraphId": 99, "startTime": "00:01:24.242", "duration": "00:00:29.851"}]}}`,
			"sjjm":  `{"title": "1. Song", "track": 1, "docid": 1102016801, "duration": 139.55, "file": {"url": "u", "checksum": "c-sjjm"}, "markers": ""}`,
			"pksjj": `{"title": "1. Song", "track": 1, "docid": 501900106, "duration": 149.7, "file": {"url": "u", "checksum": "c-pksjj"}, "markers": null}`,
		}[q.Get("pub")]
		w.Header().Set("Content-Type", "application/json")
		switch {
		case mp3 != "":
			fmt.Fprintf(w, `{"pubName": "Songs %s", "pub": %q, "files": {"E": {"MP3": [%s]}}}`, q.Get("pub"), q.Get("pub"), mp3)
		case q.Get("docid") == "1102016801":
			http.Error(w, "no PDF", http.StatusNotFound)
		default:
			http.Error(w, "unknown", http.StatusNotFound)
		}
	})
	mux.HandleFunc("/finder", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("docid") {
		case "1102016801":
			fmt.Fprint(w, `<article id="article" class="docClass-31"><header><p class="contextTtl">SONG 1</p><h1>Jehovah’s Attributes</h1></header>
<div class="bodyTxt"><ol class="source"><li><p data-pid="4"><span class="txtSrcBullet">1. </span>Jehovah our God,</p><p data-pid="5">Creator of life.</p></li></ol></div></article>`)
		case "501900106":
			fmt.Fprint(w, `<article id="article" class="docClass-131"><header><h1>Song 1</h1></header><div class="bodyTxt"><p data-pid="3">Sung by children.</p></div></article>`)
		default:
			http.NotFound(w, r)
		}
	})
	mux.HandleFunc("/subs.vtt", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "WEBVTT\n\n00:00:01.000 --> 00:00:02.500\nHello\nthere.\n")
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	hc := httpx.New(httpx.WithBaseURLs(httpx.BaseURLs{CDN: srv.URL, JWOrg: srv.URL, WOL: srv.URL}))
	return New(hc, httpx.OpenCacheAt(t.TempDir())), srv.URL
}

// The choir's recording sings the songbook's song and times its lines: the
// item plays that very file, so the lines carry their times.
func TestMediaTextSyncedLyrics(t *testing.T) {
	s, _ := textUpstream(t)
	item := model.MediaItem{LANK: "pub-sjjc_1_AUDIO", Type: "audio", Files: []model.MediaFile{{Checksum: "c-sjjc"}}}
	text, err := s.MediaText(context.Background(), "E", item)
	if err != nil {
		t.Fatal(err)
	}
	d := text.Document
	if d == nil || d.DocID != 1102016801 || d.Title != "Jehovah’s Attributes" || d.Pub != "sjjc" || d.PubName != "Songs sjjc" {
		t.Fatalf("document = %+v", d)
	}
	if !d.Synced {
		t.Fatal("the lines should be timed")
	}
	lines := d.Blocks[0].Lines
	if lines[0].Start != 16.016 || lines[0].End != 24.036 || lines[1].End != 0 {
		t.Errorf("times = %+v", lines)
	}
	if len(text.Transcript) != 0 || text.SubtitlesURL != "" {
		t.Errorf("no subtitles to read: %+v", text)
	}
}

// A video longer than the recording by more than a fade is not timed by it.
func TestMediaTextUnsyncedVideo(t *testing.T) {
	s, _ := textUpstream(t)
	item := model.MediaItem{LANK: "pub-sjjc_1_VIDEO", DurationSec: 170}
	text, err := s.MediaText(context.Background(), "E", item)
	if err != nil || text.Document == nil || text.Document.Synced || text.Document.Blocks[0].Lines[0].End != 0 {
		t.Fatalf("text = %+v, %v", text.Document, err)
	}
}

// A children's recording of a song names a page without lyrics: the lyrics
// are the songbook's song of its number.
func TestMediaTextSongbookFallback(t *testing.T) {
	s, _ := textUpstream(t)
	text, err := s.MediaText(context.Background(), "E", model.MediaItem{LANK: "pub-pksjj_1_VIDEO"})
	if err != nil {
		t.Fatal(err)
	}
	d := text.Document
	if d == nil || d.DocID != 1102016801 || !d.Lyrics() || d.Pub != "pksjj" || d.PubName != "Songs pksjj" || d.Synced {
		t.Fatalf("document = %+v", d)
	}
}

// A video whose recording names no document has its subtitles' transcript.
func TestMediaTextTranscript(t *testing.T) {
	s, up := textUpstream(t)
	item := model.MediaItem{LANK: "pub-xyz_1_VIDEO", Files: []model.MediaFile{{}, {SubtitlesURL: up + "/subs.vtt"}}}
	text, err := s.MediaText(context.Background(), "E", item)
	if err != nil {
		t.Fatal(err)
	}
	if text.Document != nil || len(text.Transcript) != 1 || text.Transcript[0].Text != "Hello there." || !strings.HasSuffix(text.SubtitlesURL, "/subs.vtt") {
		t.Errorf("text = %+v", text)
	}
}

// Nothing to read is no error; a failure with nothing read is.
func TestMediaTextNothing(t *testing.T) {
	s, up := textUpstream(t)
	text, err := s.MediaText(context.Background(), "E", model.MediaItem{LANK: "docid-502014202_1_VIDEO"})
	if err != nil || !text.Empty() {
		t.Errorf("text = %+v, %v", text, err)
	}
	item := model.MediaItem{LANK: "pub-xyz_1_VIDEO", Files: []model.MediaFile{{SubtitlesURL: up + "/missing.vtt"}}}
	if _, err := s.MediaText(context.Background(), "E", item); err == nil {
		t.Error("a subtitles file that cannot be read, with nothing else, is an error")
	}
}

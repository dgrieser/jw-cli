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
		// a track is asked for in every format, which is what lists AIVTT
		if (q.Get("pub") != "" && q.Has("fileformat")) || q.Get("langwritten") != "E" {
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

// aiUpstream lists machine-made subtitles for track 1 of "xyz": the file
// whose checksum pub-media names, behind a link signed per answer. A link
// signed before the last answer has run out.
type aiUpstream struct {
	srv      *httptest.Server
	checksum string // what pub-media lists
	body     string // the file of that checksum
	answers  int    // pub-media answers given: the signature of the last
	fetched  int    // files read
	subs     bool   // the mediator links subtitles too
}

func newAIUpstream(t *testing.T) (*Service, *aiUpstream) {
	t.Helper()
	up := &aiUpstream{checksum: "empty", body: "WEBVTT\n"}
	mux := http.NewServeMux()
	mux.HandleFunc("/apis/pub-media/GETPUBMEDIALINKS", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Has("fileformat") {
			// as pub-media does: a filtered answer never lists AIVTT
			fmt.Fprint(w, `{"pub": "xyz", "files": {"E": {"MP4": [{"track": 1, "file": {"url": "v.mp4"}}]}}}`)
			return
		}
		up.answers++
		fmt.Fprintf(w, `{"pubName": "Videos", "pub": "xyz", "files": {"E": {
			"MP4": [{"track": 1, "file": {"url": "v.mp4"}}],
			"AIVTT": [{"track": 1, "filesize": %d, "mimetype": "text/vtt",
				"file": {"url": "%s/a/1/o/xyz_E_01.aivtt?Expires=1&Signature=s%d", "checksum": %q}}]}}}`,
			len(up.body), up.srv.URL, up.answers, up.checksum)
	})
	mux.HandleFunc("/a/1/o/xyz_E_01.aivtt", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("Signature") != fmt.Sprintf("s%d", up.answers) {
			http.Error(w, "expired", http.StatusForbidden)
			return
		}
		up.fetched++
		fmt.Fprint(w, up.body)
	})
	mux.HandleFunc("/subs.vtt", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "WEBVTT\n\n00:00:01.000 --> 00:00:02.000\nReal subtitles.\n")
	})
	up.srv = httptest.NewServer(mux)
	t.Cleanup(up.srv.Close)
	hc := httpx.New(httpx.WithBaseURLs(httpx.BaseURLs{CDN: up.srv.URL, JWOrg: up.srv.URL, WOL: up.srv.URL}))
	return New(hc, httpx.OpenCacheAt(t.TempDir())), up
}

func (up *aiUpstream) item() model.MediaItem {
	item := model.MediaItem{LANK: "pub-xyz_1_VIDEO", Files: []model.MediaFile{{URL: "v.mp4"}}}
	if up.subs {
		item.Files[0].SubtitlesURL = up.srv.URL + "/subs.vtt"
	}
	return item
}

// Listed before they are written, the machine-made subtitles are an empty
// file: kept as such, and not read again until pub-media lists a changed
// file — which is read, and is the item's transcript.
func TestMediaTextAIVTT(t *testing.T) {
	s, up := newAIUpstream(t)
	ctx := context.Background()
	for range 2 {
		text, err := s.MediaText(ctx, "E", up.item())
		if err != nil || !text.Empty() {
			t.Fatalf("an empty file is no text: %+v, %v", text, err)
		}
	}
	if up.fetched != 1 {
		t.Errorf("the empty file was read %d times, want once: it is kept", up.fetched)
	}

	// pub-media lists the written file: read past the cache, as a reload does
	up.checksum, up.body = "written", "WEBVTT\n\n00:00:03.000 --> 00:00:04.000\nSpoken words.\n"
	text, err := s.MediaText(httpx.WithRefresh(ctx), "E", up.item())
	if err != nil {
		t.Fatal(err)
	}
	if !text.AITranscript || len(text.Transcript) != 1 || text.Transcript[0].Text != "Spoken words." || text.SubtitlesURL != "" {
		t.Errorf("text = %+v", text)
	}
	if up.fetched != 2 {
		t.Errorf("files read = %d, want 2", up.fetched)
	}
}

// pub-media's answer is kept for longer than its links are signed: a link
// refused for its signature is asked of pub-media anew.
func TestMediaTextAIVTTExpired(t *testing.T) {
	s, up := newAIUpstream(t)
	up.body = "WEBVTT\n\n00:00:03.000 --> 00:00:04.000\nSpoken words.\n"
	ctx := context.Background()
	// pub-media's answer is kept: its link signed with s1
	if _, err := s.trackFiles(ctx, "E", track{Pub: "xyz", Track: 1}); err != nil {
		t.Fatal(err)
	}
	up.answers++ // s1 has run out
	text, err := s.MediaText(ctx, "E", up.item())
	if err != nil {
		t.Fatal(err)
	}
	if len(text.Transcript) != 1 || !text.AITranscript || up.answers != 3 || up.fetched != 1 {
		t.Errorf("text = %+v; pub-media answers %d, files read %d", text, up.answers, up.fetched)
	}
}

// Subtitles of its own are an item's transcript, not the machine-made ones.
func TestMediaTextSubtitlesBeforeAIVTT(t *testing.T) {
	s, up := newAIUpstream(t)
	up.subs = true
	up.checksum, up.body = "written", "WEBVTT\n\n00:00:03.000 --> 00:00:04.000\nSpoken words.\n"
	text, err := s.MediaText(context.Background(), "E", up.item())
	if err != nil {
		t.Fatal(err)
	}
	if text.AITranscript || len(text.Transcript) != 1 || text.Transcript[0].Text != "Real subtitles." {
		t.Errorf("text = %+v", text)
	}
}

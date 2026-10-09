package cli

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func mediaMux(t *testing.T) *http.ServeMux {
	mux := languagesMux(t)
	mux.HandleFunc("/apis/mediator/v1/categories/E", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"categories": [
			{"key": "VideoOnDemand", "name": "Videos", "type": "container", "subcategories": [], "media": []},
			{"key": "Audio", "name": "Audio", "type": "container", "subcategories": [], "media": []}
		]}`)
	})
	mux.HandleFunc("/apis/mediator/v1/categories/E/LatestVideos", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"category": {
			"key": "LatestVideos", "name": "Latest Videos", "type": "ondemand",
			"subcategories": [],
			"media": [{
				"languageAgnosticNaturalKey": "pub-abc_1_VIDEO", "type": "video",
				"title": "A New Video", "durationFormattedMinSec": "5:00",
				"images": {"lss": {"lg": "https://cdn.example/a.jpg"}},
				"files": []
			}]
		}}`)
	})
	mux.HandleFunc("/apis/mediator/v1/media-items/E/pub-abc_1_VIDEO", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"media": [{
			"languageAgnosticNaturalKey": "pub-abc_1_VIDEO", "type": "video",
			"title": "A New Video", "description": "About something.",
			"durationFormattedMinSec": "5:00", "availableLanguages": ["E","X"],
			"files": [
				{"progressiveDownloadURL": "http://%[1]s/files/v_r240P.mp4", "label": "240p", "frameHeight": 240, "mimetype": "video/mp4", "filesize": 5},
				{"progressiveDownloadURL": "http://%[1]s/files/v_r720P.mp4", "label": "720p", "frameHeight": 720, "mimetype": "video/mp4", "filesize": 5}
			]
		}]}`, r.Host)
	})
	mux.HandleFunc("/files/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(r.URL.Path[len("/files/"):]))
	})
	return mux
}

// Selecting a specific rendition row from `jw media info` must download that
// exact file; an explicit --quality flag switches to quality selection.
func TestDownloadMediaInfoRendition(t *testing.T) {
	mux := mediaMux(t)
	cacheDir := t.TempDir()
	if _, err := runCmdWithCache(t, mux, cacheDir, "media", "info", "pub-abc_1_VIDEO", "-l", "en"); err != nil {
		t.Fatal(err)
	}

	dlDir := t.TempDir()
	out, err := runCmdWithCache(t, mux, cacheDir, "download", "1", "-l", "en", "-d", dlDir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "v_r240P.mp4") {
		t.Fatalf("row 1 (240p) should download the 240p file:\n%s", out)
	}

	out, err = runCmdWithCache(t, mux, cacheDir, "download", "1", "-l", "en", "-d", t.TempDir(), "-q", "720p")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "v_r720P.mp4") {
		t.Fatalf("explicit -q 720p should override the row's rendition:\n%s", out)
	}
}

func TestMediaBrowseRoot(t *testing.T) {
	out, err := runCmd(t, mediaMux(t), "media", "browse", "-l", "en")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Videos") || !strings.Contains(out, "Audio") {
		t.Fatalf("unexpected output:\n%s", out)
	}
}

func TestMediaBrowseCategory(t *testing.T) {
	out, err := runCmd(t, mediaMux(t), "media", "browse", "LatestVideos", "-l", "en")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "A New Video (5:00)") {
		t.Fatalf("unexpected output:\n%s", out)
	}
}

func TestMediaInfo(t *testing.T) {
	out, err := runCmd(t, mediaMux(t), "media", "info", "pub-abc_1_VIDEO", "-l", "en")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"# A New Video", "720p", "240p", "jw download"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

// textMux adds a song and a subtitled video: the song's lyrics come from its
// page, timed by the recording; the video's words from its subtitles.
func textMux(t *testing.T) *http.ServeMux {
	mux := mediaMux(t)
	mux.HandleFunc("/apis/mediator/v1/media-items/E/pub-sjjm_1_VIDEO", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"media": [{
			"languageAgnosticNaturalKey": "pub-sjjm_1_VIDEO", "type": "video", "title": "1. Jehovah's Attributes", "duration": 140.78,
			"files": [{"progressiveDownloadURL": "http://%[1]s/files/s.mp4", "label": "720p", "frameHeight": 720, "mimetype": "video/mp4", "filesize": 5,
				"subtitles": {"url": "http://%[1]s/s.vtt"}}]
		}]}`, r.Host)
	})
	mux.HandleFunc("/s.vtt", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "WEBVTT\n\n00:00:01.000 --> 00:00:03.000\nFirst words.\n\n00:00:05.000 --> 00:00:06.000\nThen more.\n")
	})
	mux.HandleFunc("/apis/pub-media/GETPUBMEDIALINKS", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("pub") != "sjjm" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"pubName": "Sing Out Joyfully—Meetings", "pub": "sjjm", "files": {"E": {"MP3": [{
			"title": "1.", "track": 1, "docid": 1102016801, "duration": 139.55, "file": {"url": "u", "checksum": "c"},
			"markers": {"documentId": 1102016801, "markers": [{"mepsParagraphId": 4, "startTime": "00:00:11.671", "duration": "00:00:10.033"}]}
		}]}}}`)
	})
	mux.HandleFunc("/finder", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<article id="article" class="docClass-31"><header><p class="contextTtl">SONG 1</p><h1>Jehovah’s Attributes</h1></header>
<div class="bodyTxt"><ol class="source"><li><p data-pid="4"><span class="txtSrcBullet">1. </span>Jehovah our God,</p><p data-pid="5">Creator of life.</p></li></ol>
<div class="chorus"></div></div><div class="closingContent"><p>(See also Ps. 36:9.)</p></div></article>`)
	})
	return mux
}

func TestMediaText(t *testing.T) {
	out, err := runCmd(t, textMux(t), "media", "text", "pub-sjjm_1_VIDEO", "-l", "en", "--timestamps", "--transcript")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"# Jehovah’s Attributes", "*SONG 1*",
		"**1.** `0:11` Jehovah our God,  \nCreator of life.",
		"(See also Ps. 36:9.)",
		"- Publication: Sing Out Joyfully—Meetings",
		"## Transcript", "`0:01` First words.\n\n`0:05` Then more.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}

	// without --transcript the lyrics stand alone
	out, err = runCmd(t, textMux(t), "media", "text", "pub-sjjm_1_VIDEO", "-l", "en", "--no-urls")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "Transcript") || strings.Contains(out, "`0:11`") || strings.Contains(out, "http") {
		t.Errorf("unexpected output:\n%s", out)
	}

	// a video with nothing but subtitles is its transcript
	out, err = runCmd(t, textMux(t), "media", "text", "pub-abc_1_VIDEO", "-l", "en")
	if err == nil || !strings.Contains(err.Error(), "No lyrics, text or subtitles found for pub-abc_1_VIDEO") {
		t.Errorf("expected nothing found, got %v:\n%s", err, out)
	}
}

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
	if !strings.Contains(out, "[category] Videos") || !strings.Contains(out, "Audio") {
		t.Fatalf("unexpected output:\n%s", out)
	}
}

func TestMediaBrowseCategory(t *testing.T) {
	out, err := runCmd(t, mediaMux(t), "media", "browse", "LatestVideos", "-l", "en")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "[video] A New Video (5:00)") {
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

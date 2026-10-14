package server_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// An item without lyrics, a document or subtitles has no text: the API says
// so as jw media text does, rather than answering with an empty one.
func TestAPIMediaTextEmpty(t *testing.T) {
	mux := mediaMux(t)
	mux.HandleFunc("/apis/mediator/v1/media-items/E/pub-quiet_1_VIDEO", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"media": [{
			"languageAgnosticNaturalKey": "pub-quiet_1_VIDEO", "type": "video", "title": "Without Words",
			"files": [{"progressiveDownloadURL": "https://cdn.example/q_r720P.mp4", "label": "720p", "mimetype": "video/mp4", "filesize": 9}]
		}]}`)
	})
	srv := newTestServer(t, mux)
	resp, body := get(t, srv, "/api/v1/media/items/pub-quiet_1_VIDEO/text?lang=en")
	if resp.StatusCode != 404 || !strings.Contains(body, `"code": "not_found"`) ||
		!strings.Contains(body, "No lyrics, text or subtitles found for pub-quiet_1_VIDEO.") {
		t.Errorf("status %d body %s", resp.StatusCode, body)
	}
}

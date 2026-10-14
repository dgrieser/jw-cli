package server_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// A category's subcategories lead its listing, then its own media, as jw
// media browse lists them.
func TestUIMediaCategorySubcategoriesFirst(t *testing.T) {
	mux := mediaMux(t)
	mux.HandleFunc("/apis/mediator/v1/categories/E/Album", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"category": {
			"key": "Album", "name": "An Album", "type": "ondemand",
			"subcategories": [{"key": "AlbumAD", "name": "With Audio Descriptions", "type": "ondemand", "media": [
				{"languageAgnosticNaturalKey": "pub-ad_1_AUDIO", "type": "audio", "title": "Song One (AD)", "files": []}
			]}],
			"media": [{"languageAgnosticNaturalKey": "pub-al_1_AUDIO", "type": "audio", "title": "Song One", "files": []}]
		}}`)
	})
	srv := newTestServer(t, mux)
	resp, body := get(t, srv, "/media/category/Album?lang=en")
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	sub, song := strings.Index(body, `href="/media/category/AlbumAD?lang=en"`), strings.Index(body, ">Song One<")
	if sub < 0 || song < 0 || sub > song {
		t.Errorf("want the subcategory before the media:\n%s", body)
	}
}

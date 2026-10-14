package server_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// The root of the media categories pages with limit and offset as a category
// does.
func TestAPIMediaRootPages(t *testing.T) {
	srv := newTestServer(t, mediaMux(t))
	_, body := get(t, srv, "/api/v1/media/categories?lang=en&limit=1&offset=1")
	if strings.Contains(body, "VideoOnDemand") || !strings.Contains(body, `"key": "Audio"`) {
		t.Errorf("second category alone: %s", body)
	}
	_, body = get(t, srv, "/api/v1/media/categories?lang=en&offset=5")
	if strings.TrimSpace(body) != "[]" {
		t.Errorf("past the end: %s", body)
	}
	if resp, _ := get(t, srv, "/api/v1/media/categories?lang=en&limit=x"); resp.StatusCode != 400 {
		t.Errorf("bad limit: status %d", resp.StatusCode)
	}
}

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

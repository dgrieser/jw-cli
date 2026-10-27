package server_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
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

// The latest videos are jw.org's pick and, after it, what every category
// published in the last 30 days, each once, newest first — the videos with
// audio descriptions left out, as they are the library's videos again.
func TestUIMediaLatestFromEveryCategory(t *testing.T) {
	day := func(n int) string { return time.Now().AddDate(0, 0, -n).UTC().Format(time.RFC3339) }
	item := func(lank, title string, days int) string {
		return fmt.Sprintf(`{"languageAgnosticNaturalKey": %q, "type": "video", "title": %q, "firstPublished": %q, "files": []}`, lank, title, day(days))
	}
	mux := languagesMux(t)
	cat := func(key, body string) {
		mux.HandleFunc("/apis/mediator/v1/categories/E"+key, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, body)
		})
	}
	cat("", `{"categories": [{"key": "VideoOnDemand", "name": "Videos", "type": "container"}, {"key": "Audio", "name": "Audio", "type": "container"}]}`)
	cat("/LatestVideos", `{"category": {"key": "LatestVideos", "name": "Latest Videos", "type": "ondemand", "media": [`+
		item("pub-pick_1_VIDEO", "Picked Video", 3)+`]}}`)
	cat("/VideoOnDemand", `{"category": {"key": "VideoOnDemand", "name": "Videos", "type": "container", "subcategories": [
		{"key": "VODStudio", "name": "Studio", "type": "container"},
		{"key": "VODAudioDescriptions", "name": "Described", "type": "container"}
	]}}`)
	cat("/VODStudio", `{"category": {"key": "VODStudio", "name": "Studio", "type": "container", "subcategories": [
		{"key": "StudioTalks", "name": "Talks", "type": "ondemand", "media": [`+
		item("pub-talk_1_VIDEO", "Newest Talk", 1)+`,`+item("pub-old_1_VIDEO", "Old Talk", 45)+`,`+item("pub-pick_1_VIDEO", "Picked Video", 3)+`]},
		{"key": "StudioFeatured", "name": "Featured", "type": "ondemand", "media": [`+item("pub-talk_1_VIDEO", "Newest Talk", 1)+`]}
	]}}`)
	cat("/VODAudioDescriptions", `{"category": {"key": "VODAudioDescriptions", "type": "container", "subcategories": [
		{"key": "VODStudioAD", "type": "ondemand", "media": [`+item("pub-talk_1_AD_VIDEO", "Described Talk", 1)+`]}
	]}}`)
	cat("/Audio", `{"category": {"key": "Audio", "name": "Audio", "type": "container", "subcategories": [
		{"key": "AudioOriginalSongs", "name": "Songs", "type": "ondemand", "media": [
			{"languageAgnosticNaturalKey": "pub-song_1_AUDIO", "type": "audio", "title": "New Song", "firstPublished": "`+day(5)+`", "files": []}
		]}
	]}}`)
	srv := newTestServer(t, mux)
	resp, body := get(t, srv, "/media?lang=en")
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	talk, pick, song := strings.Index(body, ">Newest Talk<"), strings.Index(body, ">Picked Video<"), strings.Index(body, ">New Song<")
	if talk < 0 || pick < 0 || song < 0 || !(talk < pick && pick < song) {
		t.Errorf("want the talk, the pick and the song, newest first (%d %d %d):\n%s", talk, pick, song, body)
	}
	for _, title := range []string{">Newest Talk<", ">Picked Video<"} {
		if n := strings.Count(body, title); n != 1 {
			t.Errorf("%s %d times", title, n)
		}
	}
	if strings.Contains(body, "Old Talk") || strings.Contains(body, "Described Talk") {
		t.Errorf("an old or described video among the latest:\n%s", body)
	}
}

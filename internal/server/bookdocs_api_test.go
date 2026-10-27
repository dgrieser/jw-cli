package server_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// bookDocsMux serves Matthew's book page and the documents it lists, as the
// CLI's tests do. The book page is the German one, whose links lead to
// German addresses.
func bookDocsMux(t *testing.T) *http.ServeMux {
	mux := wolMux(t)
	mux.HandleFunc("/en/wol/binav/r1/lp-e/nwtsty/40", wolFixture(t, "binav_de_40.html"))
	mux.HandleFunc("/de/wol/bibledocument/r10/lp-x/nwtsty/40/introduction", wolFixture(t, "intro_mt.html"))
	mux.HandleFunc("/de/wol/gallery/r10/lp-x/nwtsty/40", wolFixture(t, "gallery_mt.html"))
	mux.HandleFunc("/en/wol/bibledocument/r1/lp-e/nwtsty/40/overview", wolFixture(t, "overview_mt.html"))
	mux.HandleFunc("/en/wol/d/r1/lp-e/1001070071", wolFixture(t, "book_table.html"))
	return mux
}

// The book documents the CLI prints with -o json come from the API too, the
// book named or numbered.
func TestAPIBookDocuments(t *testing.T) {
	srv := newTestServer(t, bookDocsMux(t))
	for _, tc := range []struct{ path, want string }{
		{"/api/v1/bible/intro?book=40&lang=en", `"title": "Introduction to Matthew"`},
		{"/api/v1/bible/intro?book=Matthew&lang=en", `"title": "Introduction to Matthew"`},
		{"/api/v1/bible/outline?book=Mt&lang=en", `"title": "A. Genealogy of Jesus Christ"`},
		{"/api/v1/bible/gallery?book=40&lang=en", `"title": "Winter in Bethlehem"`},
	} {
		resp, body := get(t, srv, tc.path)
		if resp.StatusCode != 200 || !strings.Contains(body, tc.want) {
			t.Errorf("%s: status %d, want %s in %.600s", tc.path, resp.StatusCode, tc.want, body)
			continue
		}
		var v map[string]any
		if err := json.Unmarshal([]byte(body), &v); err != nil || v["book"] == nil && v["groups"] == nil {
			t.Errorf("%s: not the document model: %.300s", tc.path, body)
		}
	}
	for _, tc := range []struct {
		path   string
		status int
	}{
		{"/api/v1/bible/intro?lang=en", 400},
		{"/api/v1/bible/intro?book=Hezekiah&lang=en", 400},
		{"/api/v1/bible/intro?book=40&bible=../x&lang=en", 400},
	} {
		if resp, body := get(t, srv, tc.path); resp.StatusCode != tc.status {
			t.Errorf("%s: status %d, want %d: %s", tc.path, resp.StatusCode, tc.status, body)
		}
	}
}

// contentMux serves Joel's chapter grid and the article of highlights on Joel
// and Amos; the other publications have nothing on Joel here.
func contentMux(t *testing.T) *http.ServeMux {
	mux := wolMux(t)
	mux.HandleFunc("/en/wol/binav/r1/lp-e/nwtsty/29", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<h1 class="navBook">Joel</h1><ul class="chapters">
			<li class="chapter"><a href="/en/wol/b/r1/lp-e/nwtsty/29/1">1</a></li>
			<li class="chapter"><a href="/en/wol/b/r1/lp-e/nwtsty/29/2">2</a></li>
			<li class="chapter"><a href="/en/wol/b/r1/lp-e/nwtsty/29/3">3</a></li></ul>`))
	})
	mux.HandleFunc("/en/wol/d/r1/lp-e/2007722", wolFixture(t, "study_w_joel.html"))
	return mux
}

// A chapter's heading unfolds what the publications that go through every
// book say about the chapter, next to its outline: the parts about it, each
// linked to its paragraphs, and nothing about the other book an article is on.
func TestUnfoldChapterContent(t *testing.T) {
	srv := newTestServer(t, contentMux(t))
	_, body := get(t, srv, "/unfold/chapter?book=29&chapter=2&lang=en")
	for _, want := range []string{
		`Content of Joel 2`,
		`Highlights (“Jehovah’s Word Is Alive”)`,
		`/en/wol/d/r1/lp-e/2007722#h=11:0-11:0`,
		`How has the prophecy about the invasion by insects been fulfilled?`,
		`Lessons for Us:`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %s in %.3000s", want, body)
		}
	}
	for _, unwanted := range []string{"cows of Bashan", "essential for salvation"} {
		if strings.Contains(body, unwanted) {
			t.Errorf("%q is not about Joel 2: %.3000s", unwanted, body)
		}
	}
}

// The same comes from the API, for a chapter or the whole book.
func TestAPIBibleContent(t *testing.T) {
	srv := newTestServer(t, contentMux(t))
	resp, body := get(t, srv, "/api/v1/bible/content?book=Joel&chapter=1&lang=en")
	var studies []map[string]any
	if resp.StatusCode != 200 || json.Unmarshal([]byte(body), &studies) != nil || len(studies) != 1 || studies[0]["pub"] != "w" {
		t.Fatalf("status %d: %.600s", resp.StatusCode, body)
	}
	if strings.Contains(body, "How has the prophecy about the invasion") {
		t.Errorf("a question on chapter 2 in chapter 1: %.600s", body)
	}
	if _, body := get(t, srv, "/api/v1/bible/content?book=29&lang=en"); !strings.Contains(body, "How has the prophecy about the invasion") {
		t.Errorf("the whole book lacks chapter 2: %.600s", body)
	}
	for _, path := range []string{
		"/api/v1/bible/content?lang=en",
		"/api/v1/bible/content?book=29&chapter=4&lang=en",
		"/api/v1/bible/content?book=29&chapter=x&lang=en",
	} {
		if resp, body := get(t, srv, path); resp.StatusCode != 400 {
			t.Errorf("%s: status %d: %s", path, resp.StatusCode, body)
		}
	}
}

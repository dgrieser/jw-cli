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

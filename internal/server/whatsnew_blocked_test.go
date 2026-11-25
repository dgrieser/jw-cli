package server_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// jw.org's bot protection answers some networks with a page of its own and
// a 200. What is new then says jw.org's list is missing rather than showing
// the media as if that were all, and the browser does not keep the page;
// the API says so beside what it could list.
func TestLatestSaysWhenJWOrgTurnsAway(t *testing.T) {
	mux := languagesMux(t)
	mux.HandleFunc("/finder", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<html><head><title>Access Denied</title></head><body><p>Please wait…</p></body></html>`))
	})
	srv := newTestServer(t, mux)

	resp, body := get(t, srv, "/latest?lang=en")
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if !strings.Contains(body, "list of what is new could not be read") {
		t.Errorf("no notice that jw.org's list is missing:\n%s", body)
	}
	if resp.Header.Get("X-JW-Keep") != "no" {
		t.Errorf("a page missing jw.org's list is kept by the browser")
	}
	// the media alone are not jw.org's list: no notice
	if _, body := get(t, srv, "/latest?lang=en&kind=media"); strings.Contains(body, "could not be read") {
		t.Errorf("the media tab says jw.org's list is missing")
	}

	resp, body = get(t, srv, "/api/v1/latest?lang=en&kind=pub")
	var out struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal([]byte(body), &out)
	if resp.StatusCode == 200 && out.Error == "" {
		t.Errorf("API: status 200 with no error for an unread list: %s", body)
	}
}

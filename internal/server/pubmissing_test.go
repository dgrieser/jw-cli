package server_test

import (
	"net/http"
	"strings"
	"testing"
)

// wol answers a publication it does not have with 400 and its "Sorry, the
// document does not exist" banner: that is a page not found. A 503 is wol
// being unavailable, and stays an upstream failure.
func TestPubPublicationMissing(t *testing.T) {
	mux := libraryMux(t)
	mux.HandleFunc("/en/wol/publication/r1/lp-e/nope", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`<html><body><div class="banner bannerError"><div class="bannerContent">Sorry, the document does not exist in English Publications.</div></div></body></html>`))
	})
	mux.HandleFunc("/en/wol/publication/r1/lp-e/down", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	})
	srv := newTestServer(t, mux)
	if resp, _ := get(t, srv, "/pub/publication/nope?lang=en"); resp.StatusCode != http.StatusNotFound {
		t.Errorf("page: status %d", resp.StatusCode)
	}
	resp, body := get(t, srv, "/api/v1/pub/publication/nope?lang=en")
	if resp.StatusCode != http.StatusNotFound || !strings.Contains(body, `"code": "not_found"`) {
		t.Errorf("api: status %d: %s", resp.StatusCode, body)
	}
	if resp, _ := get(t, srv, "/api/v1/pub/publication/down?lang=en"); resp.StatusCode != http.StatusBadGateway {
		t.Errorf("unavailable: status %d", resp.StatusCode)
	}
}

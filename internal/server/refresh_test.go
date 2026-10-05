package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dgrieser/jw-cli/internal/httpx"
)

// ?refresh=1 reads past the cache, and is gone before the handler sees it.
func TestRefreshesOnce(t *testing.T) {
	var refreshing bool
	var query string
	h := refreshes(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		refreshing, query = httpx.Refreshing(r.Context()), r.URL.RawQuery
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/bible?ref=Joh+3&refresh=1", nil))
	if !refreshing || query != "ref=Joh+3" {
		t.Fatalf("refresh=1: refreshing %v, query %q", refreshing, query)
	}
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/bible?ref=Joh+3", nil))
	if refreshing || query != "ref=Joh+3" {
		t.Fatalf("plain: refreshing %v, query %q", refreshing, query)
	}
}

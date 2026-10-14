package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dgrieser/jw-cli/internal/httpx"
	"github.com/dgrieser/jw-cli/internal/service"
)

// An API request that is not done in time is answered 504 with the error
// envelope, rather than running for as long as upstream keeps it waiting
// (here a lookup shared with other readers, which upstream holds for 2s).
func TestAPIDeadline(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	}))
	t.Cleanup(up.Close)
	hc := httpx.New(httpx.WithBaseURLs(httpx.BaseURLs{CDN: up.URL, JWOrg: up.URL, WOL: up.URL}),
		httpx.WithRetryDelays(time.Millisecond, time.Millisecond))
	s := New(Config{Svc: service.New(hc, httpx.OpenCacheAt(t.TempDir()))})
	s.apiTimeout = 100 * time.Millisecond
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)

	start := time.Now()
	resp, err := http.Get(srv.URL + "/api/v1/languages")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if took := time.Since(start); took > time.Second {
		t.Errorf("answered after %s", took)
	}
	var e apiError
	if resp.StatusCode != http.StatusGatewayTimeout || json.Unmarshal(body, &e) != nil || e.Error.Code != "timeout" {
		t.Errorf("status %d: %s", resp.StatusCode, body)
	}
}

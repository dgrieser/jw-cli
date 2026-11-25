package server

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/dgrieser/jw-cli/internal/accounts"
)

// Posting the feed of what is new replaces what every reader is shown: an
// admin's key may, a reader's may not, nobody's may not.
func TestPostFeedTakesAnAdmin(t *testing.T) {
	e := newAuthEnv(t)
	if err := e.users.Create("admin@example.org", "secret-pass", accounts.RoleAdmin, false, "en"); err != nil {
		t.Fatal(err)
	}
	if err := e.users.Create("bob@example.org", "bob-pass1", accounts.RoleUser, false, "en"); err != nil {
		t.Fatal(err)
	}
	adminKey, _, _ := e.users.CreateKey("admin@example.org", "cron", time.Time{})
	bobKey, _, _ := e.users.CreateKey("bob@example.org", "script", time.Time{})
	post := func(key string) int {
		t.Helper()
		// no feed: an admin gets past the check to the body's refusal
		req, _ := http.NewRequest(http.MethodPost, e.srv.URL+"/api/v1/latest/feed", strings.NewReader(`<html></html>`))
		req.Header.Set("Content-Type", "application/rss+xml")
		if key != "" {
			req.Header.Set("Authorization", "Bearer "+key)
		}
		resp, err := e.srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if code := post(""); code != http.StatusUnauthorized {
		t.Errorf("no key: %d", code)
	}
	if code := post(bobKey); code != http.StatusForbidden {
		t.Errorf("a reader's key: %d", code)
	}
	if code := post(adminKey); code != http.StatusBadRequest {
		t.Errorf("an admin's key: %d, want the body refused", code)
	}
}

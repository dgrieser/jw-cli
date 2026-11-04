package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dgrieser/jw-cli/internal/accounts"
	"github.com/dgrieser/jw-cli/internal/remote"
	"github.com/dgrieser/jw-cli/internal/service"
)

// cliServer is a jw serve whose command runner echoes what it was sent.
func cliServer(t *testing.T, users *accounts.Store, settings *accounts.SettingsFile) *httptest.Server {
	t.Helper()
	s := New(Config{Svc: &service.Service{}, Accounts: users, Settings: settings,
		RunCLI: func(ctx context.Context, req remote.Request) remote.Response {
			return remote.Response{Events: []remote.Event{{Kind: remote.KindWrite, Text: strings.Join(req.Args, " ")}}}
		}})
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	return srv
}

func TestAPICLIRunsTheCommand(t *testing.T) {
	srv := cliServer(t, nil, nil)
	c, err := remote.NewClient(srv.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	resp, err := c.Run(context.Background(), remote.Request{Args: []string{"article", "--", "1"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Events) != 1 || resp.Events[0].Text != "article -- 1" {
		t.Errorf("events = %+v", resp.Events)
	}
}

// The endpoint takes JSON only, so a form another site posts cannot run a
// command in the browser of someone logged in.
func TestAPICLIWantsJSON(t *testing.T) {
	srv := cliServer(t, nil, nil)
	resp, err := http.Post(srv.URL+remote.Path, "text/plain", strings.NewReader(`{"args":["article","1"]}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Errorf("status = %d, want 415", resp.StatusCode)
	}
	for _, body := range []string{`{`, `{"args":[]}`} {
		resp, err := http.Post(srv.URL+remote.Path, "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		var e apiError
		_ = json.NewDecoder(resp.Body).Decode(&e)
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest || e.Error.Code != "bad_request" {
			t.Errorf("%s: status = %d, %+v", body, resp.StatusCode, e)
		}
	}
}

func TestAPICLIBehindLogin(t *testing.T) {
	users, settings := testAccounts(t)
	if err := users.Create("me@example.org", "secret-pass", accounts.RoleAdmin, false, ""); err != nil {
		t.Fatal(err)
	}
	srv := cliServer(t, users, settings)
	req := remote.Request{Args: []string{"dailytext"}}
	c, _ := remote.NewClient(srv.URL, "")
	if _, err := c.Run(context.Background(), req); err == nil || !strings.Contains(err.Error(), "JW_SERVER_KEY") {
		t.Errorf("without a key: %v", err)
	}
	c, _ = remote.NewClient(srv.URL, accounts.KeyPrefix+"wrong")
	if _, err := c.Run(context.Background(), req); err == nil || !strings.Contains(err.Error(), "JW_SERVER_KEY") {
		t.Errorf("wrong key: %v", err)
	}
	if _, err := remote.NewClient(strings.Replace(srv.URL, "://", "://me%40example.org:secret-pass@", 1), ""); err == nil {
		t.Errorf("credentials in the address accepted")
	}
	key, _, err := users.CreateKey("me@example.org", "cli", time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	c, _ = remote.NewClient(srv.URL, key)
	if _, err := c.Run(context.Background(), req); err != nil {
		t.Errorf("with the key: %v", err)
	}
}

func TestAPICLIWithoutRunner(t *testing.T) {
	srv := httptest.NewServer(New(Config{Svc: &service.Service{}}).Handler())
	t.Cleanup(srv.Close)
	c, _ := remote.NewClient(srv.URL, "")
	if _, err := c.Run(context.Background(), remote.Request{Args: []string{"dailytext"}}); err == nil ||
		!strings.Contains(err.Error(), "does not run commands") {
		t.Errorf("err = %v", err)
	}
}

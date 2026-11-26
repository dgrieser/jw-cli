package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/dgrieser/jw-cli/internal/accounts"
	"github.com/dgrieser/jw-cli/internal/relay"
	"github.com/dgrieser/jw-cli/internal/service"
)

// newProxyEnv is a server with accounts and proxies.
func newProxyEnv(t *testing.T) (*authEnv, *relay.Pool) {
	t.Helper()
	users, settings := testAccounts(t)
	pool, err := relay.OpenPool(filepath.Join(t.TempDir(), "proxies.json"), "v-test", t.Logf)
	if err != nil {
		t.Fatal(err)
	}
	box := &mailbox{}
	s := New(Config{Svc: &service.Service{}, Accounts: users, Settings: settings, SetupToken: "setup-secret", Proxies: pool,
		Mailer: func(accounts.SMTP) accounts.Mailer { return box }})
	box.wait = s.mailing.Wait
	srv := httptest.NewTLSServer(s.Handler())
	t.Cleanup(srv.Close)
	e := &authEnv{srv: srv, users: users, settings: settings, box: box, s: s}
	e.withAdmin(t, accounts.RegisterClosed)
	return e, pool
}

// The proxies' protocol stands outside the sign-in: a proxy registers with
// its enrollment alone. Its credential and an API key each open their own
// routes, never the other's.
func TestProxyProtocolOutsideTheGate(t *testing.T) {
	e, pool := newProxyEnv(t)
	token, _, err := pool.Enroll("pi", "admin@example.org", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := registerVia(e, token)
	if err != nil {
		t.Fatalf("register without signing in: %v", err)
	}
	call := func(path, auth string) int {
		req, _ := http.NewRequest(http.MethodGet, e.srv.URL+path, nil)
		req.Header.Set("Authorization", auth)
		resp, err := e.srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if code := call(relay.PathPrefix+"self", relay.AuthScheme+" "+cfg.Credential); code != http.StatusOK {
		t.Errorf("credential on /proxy/v1/self: %d", code)
	}
	if code := call("/api/v1/languages", relay.AuthScheme+" "+cfg.Credential); code != http.StatusUnauthorized {
		t.Errorf("credential on the API: %d", code)
	}
	if code := call("/api/v1/languages", "Bearer "+cfg.Credential); code != http.StatusUnauthorized {
		t.Errorf("credential as an API key: %d", code)
	}
	key, _, _ := e.users.CreateKey("admin@example.org", "script", time.Time{})
	if code := call(relay.PathPrefix+"self", "Bearer "+key); code != http.StatusUnauthorized {
		t.Errorf("an admin's API key on /proxy/v1/self: %d", code)
	}
}

// registerVia registers a proxy through the test server, trusting its
// certificate.
func registerVia(e *authEnv, token string) (relay.Config, error) {
	old := http.DefaultTransport
	http.DefaultTransport = e.srv.Client().Transport
	defer func() { http.DefaultTransport = old }()
	return relay.Register(context.Background(), e.srv.URL, token, "v-proxy")
}

// The admin makes room for a proxy and gets the command that sets it up;
// the token is shown once. A reader may not.
func TestProxiesPage(t *testing.T) {
	e, pool := newProxyEnv(t)
	admin := e.login(t, "admin@example.org", "secret-pass")
	resp, body := e.get(t, admin, "/admin/proxies")
	if resp.StatusCode != 200 || !strings.Contains(body, "No proxies yet.") || !strings.Contains(body, `href="/admin/proxies"`) {
		t.Fatalf("page: %d\n%s", resp.StatusCode, body)
	}
	resp, body = e.post(t, admin, "/admin/proxies", url.Values{"op": {"enroll"}, "name": {"home pi"}})
	cmd := regexp.MustCompile(`jw proxy setup (https://[^ "]+) (jwpe_[A-Za-z0-9_-]+)`).FindStringSubmatch(body)
	if resp.StatusCode != 200 || cmd == nil || resp.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("enroll: %d %q\n%s", resp.StatusCode, resp.Header.Get("Cache-Control"), body)
	}
	if _, body = e.get(t, admin, "/admin/proxies"); strings.Contains(body, cmd[2]) || !strings.Contains(body, "Waiting to be set up") {
		t.Errorf("the token is shown again, or the enrollment not listed")
	}

	cfg, err := registerVia(e, cmd[2])
	if err != nil {
		t.Fatal(err)
	}
	if _, body = e.get(t, admin, "/admin/proxies"); !strings.Contains(body, "home pi") || !strings.Contains(body, "never connected") {
		t.Errorf("registered proxy not listed:\n%s", body)
	}
	if resp, _ := e.post(t, admin, "/admin/proxies", url.Values{"op": {"disable"}, "id": {cfg.ID}, "name": {"home pi"}}); resp.StatusCode != http.StatusSeeOther {
		t.Errorf("disable: %d", resp.StatusCode)
	}
	if st := pool.Proxies(time.Now()); len(st) != 1 || st[0].Enabled {
		t.Errorf("not disabled: %+v", st)
	}
	if resp, _ := e.post(t, admin, "/admin/proxies", url.Values{"op": {"delete"}, "id": {cfg.ID}}); resp.StatusCode != http.StatusSeeOther || len(pool.Proxies(time.Now())) != 0 {
		t.Errorf("delete: %d", resp.StatusCode)
	}

	_ = e.users.Create("bob@example.org", "bob-pass1", accounts.RoleUser, false, "en")
	bob := e.login(t, "bob@example.org", "bob-pass1")
	if resp, _ := e.post(t, bob, "/admin/proxies", url.Values{"op": {"enroll"}, "name": {"x"}}); resp.StatusCode != http.StatusForbidden {
		t.Errorf("a reader enrolls: %d", resp.StatusCode)
	}
	if len(pool.Enrollments(time.Now())) != 0 {
		t.Error("a reader's enrollment was kept")
	}
}

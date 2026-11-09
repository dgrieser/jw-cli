package server

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dgrieser/jw-cli/internal/accounts"
	"github.com/dgrieser/jw-cli/internal/remote"
	"github.com/dgrieser/jw-cli/internal/service"
)

// testAccounts opens accounts and settings in a temporary folder.
func testAccounts(t *testing.T) (*accounts.Store, *accounts.SettingsFile) {
	t.Helper()
	dir := t.TempDir()
	users, err := accounts.Open(filepath.Join(dir, "accounts.json"))
	if err != nil {
		t.Fatal(err)
	}
	settings, err := accounts.LoadSettings(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	return users, settings
}

// mailbox keeps the mails a server sends.
type mailbox struct {
	mu    sync.Mutex
	mails []accounts.Mail
	fail  error  // when set, sending fails
	wait  func() // waits for the mails still being sent
}

func (m *mailbox) Send(_ context.Context, mail accounts.Mail) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return m.fail
	}
	m.mails = append(m.mails, mail)
	return nil
}

var linkRe = regexp.MustCompile(`https?://\S+`)

// last returns the last mail to an address, and the link in it.
func (m *mailbox) last(t *testing.T, to string) (accounts.Mail, string) {
	t.Helper()
	m.sent()
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, mail := range slices.Backward(m.mails) {
		if mail.To == to {
			return mail, linkRe.FindString(mail.Body)
		}
	}
	t.Fatalf("no mail to %s", to)
	return accounts.Mail{}, ""
}

func (m *mailbox) sent() {
	if m.wait != nil {
		m.wait()
	}
}

func (m *mailbox) count() int {
	m.sent()
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.mails)
}

type authEnv struct {
	srv      *httptest.Server
	users    *accounts.Store
	settings *accounts.SettingsFile
	box      *mailbox
	s        *Server
}

func newAuthEnv(t *testing.T) *authEnv {
	t.Helper()
	users, settings := testAccounts(t)
	box := &mailbox{}
	s := New(Config{Svc: &service.Service{}, Accounts: users, Settings: settings, SetupToken: "setup-secret",
		Mailer: func(accounts.SMTP) accounts.Mailer { return box }})
	box.wait = s.mailing.Wait
	// TLS, as the public URL of the test settings is https and the session
	// cookie is Secure: Go before 1.26 does not send a Secure cookie to a
	// plain http server on the loopback address
	srv := httptest.NewTLSServer(s.Handler())
	t.Cleanup(srv.Close)
	return &authEnv{srv: srv, users: users, settings: settings, box: box, s: s}
}

// client is a browser: it keeps cookies and does not follow redirects.
func (e *authEnv) client(t *testing.T) *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{Jar: jar, Transport: e.srv.Client().Transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func (e *authEnv) get(t *testing.T, c *http.Client, path string) (*http.Response, string) {
	t.Helper()
	resp, err := c.Get(e.srv.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return resp, string(b)
}

func (e *authEnv) post(t *testing.T, c *http.Client, path string, form url.Values) (*http.Response, string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, e.srv.URL+path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", e.srv.URL)
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return resp, string(b)
}

// localPath turns a link of a mail into a path on the test server.
func localPath(t *testing.T, link string) string {
	t.Helper()
	u, err := url.Parse(link)
	if err != nil {
		t.Fatal(err)
	}
	return u.RequestURI()
}

func mailSettings(e *authEnv, registration string) accounts.Settings {
	st := accounts.Defaults()
	st.Registration = registration
	st.PublicURL = "https://jw.example.org"
	st.SMTP = accounts.SMTP{Host: "smtp.example.org", Port: 587, From: "jw@example.org"}
	return st
}

func TestSetupMakesTheAdmin(t *testing.T) {
	e := newAuthEnv(t)
	c := e.client(t)

	// before there is an admin, everything leads to the setup
	if resp, _ := e.get(t, c, "/"); resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "/setup" {
		t.Fatalf("/ before setup: %d %s", resp.StatusCode, resp.Header.Get("Location"))
	}
	if resp, _ := e.get(t, c, "/api/v1/languages"); resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("api before setup: %d", resp.StatusCode)
	}
	// without the link's secret, the setup is closed
	if resp, body := e.get(t, c, "/setup"); resp.StatusCode != http.StatusUnauthorized || strings.Contains(body, `name="smtp_host"`) {
		t.Errorf("setup without token: %d", resp.StatusCode)
	}
	if resp, _ := e.post(t, c, "/setup", url.Values{"email": {"a@example.org"}, "password": {"secret-pass"}, "password2": {"secret-pass"}}); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("setup post without token: %d", resp.StatusCode)
	}
	if e.users.HasAdmin() {
		t.Fatal("admin made without the setup link")
	}
	// the link sets a cookie and drops the secret from the address
	if resp, _ := e.get(t, c, "/setup?token=setup-secret"); resp.StatusCode != http.StatusFound {
		t.Fatalf("setup link: %d", resp.StatusCode)
	}
	if resp, body := e.get(t, c, "/setup"); resp.StatusCode != http.StatusOK || !strings.Contains(body, `name="smtp_host"`) {
		t.Fatalf("setup page: %d", resp.StatusCode)
	}
	// a test mail goes out with the settings of the form
	resp, body := e.post(t, c, "/setup", url.Values{"action": {"test"}, "email": {"admin@example.org"},
		"smtp_host": {"smtp.example.org"}, "smtp_port": {"587"}, "smtp_from": {"jw@example.org"}, "public_url": {"https://jw.example.org"},
		"smtp_username": {"jw"}, "smtp_password": {"tested-pass"}})
	if resp.StatusCode != http.StatusOK || e.box.count() != 1 {
		t.Fatalf("test mail: %d, %d mails\n%s", resp.StatusCode, e.box.count(), body)
	}
	// the password tested stays in the form for the save
	if !strings.Contains(body, `value="tested-pass"`) {
		t.Errorf("tested SMTP password dropped from the form")
	}
	resp, body = e.post(t, c, "/setup", url.Values{"action": {"save"}, "email": {"Admin@Example.org"},
		"password": {"secret-pass"}, "password2": {"secret-pass"}, "registration": {"approval"},
		"smtp_host": {"smtp.example.org"}, "smtp_port": {"587"}, "smtp_from": {"jw@example.org"}, "public_url": {"https://jw.example.org/"}})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("setup save: %d\n%s", resp.StatusCode, body)
	}
	u, ok := e.users.Get("admin@example.org")
	if !ok || !u.IsAdmin() {
		t.Fatalf("admin = %+v", u)
	}
	if st := e.settings.Get(); st.Registration != accounts.RegisterApproval || st.PublicURL != "https://jw.example.org" {
		t.Errorf("settings = %+v", st)
	}
	// signed in right away; the setup is over
	if resp, _ := e.get(t, c, "/admin/users"); resp.StatusCode != http.StatusOK {
		t.Errorf("admin page after setup: %d", resp.StatusCode)
	}
	if resp, _ := e.get(t, e.client(t), "/setup?token=setup-secret"); resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "/admin/settings" {
		t.Errorf("setup after setup: %d %s", resp.StatusCode, resp.Header.Get("Location"))
	}
	if e.s.SetupToken() != "" {
		t.Error("setup token still offered")
	}
}

func (e *authEnv) withAdmin(t *testing.T, registration string) {
	t.Helper()
	if err := e.users.Create("admin@example.org", "secret-pass", accounts.RoleAdmin, false, "en"); err != nil {
		t.Fatal(err)
	}
	if err := e.settings.Save(mailSettings(e, registration)); err != nil {
		t.Fatal(err)
	}
}

func (e *authEnv) login(t *testing.T, email, pw string) *http.Client {
	t.Helper()
	c := e.client(t)
	resp, body := e.post(t, c, "/login", url.Values{"email": {email}, "password": {pw}, "next": {"/admin/users"}})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("login %s: %d\n%s", email, resp.StatusCode, body)
	}
	return c
}

func TestLoginGate(t *testing.T) {
	e := newAuthEnv(t)
	e.withAdmin(t, accounts.RegisterClosed)
	c := e.client(t)
	resp, _ := e.get(t, c, "/search?q=x")
	if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "/login?next=%2Fsearch%3Fq%3Dx" {
		t.Errorf("page without login: %d %s", resp.StatusCode, resp.Header.Get("Location"))
	}
	if resp, _ := e.get(t, c, "/api/v1/languages"); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("api without login: %d", resp.StatusCode)
	}
	if resp, _ := e.get(t, c, "/ui/open"); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("the pages' endpoints without login: %d", resp.StatusCode)
	}
	if resp, body := e.post(t, c, "/login", url.Values{"email": {"admin@example.org"}, "password": {"wrong-pass"}}); resp.StatusCode != http.StatusUnauthorized ||
		!strings.Contains(body, "Wrong email address or password") {
		t.Errorf("wrong password: %d", resp.StatusCode)
	}
	// a form from another site changes nothing
	req, _ := http.NewRequest(http.MethodPost, e.srv.URL+"/login", strings.NewReader("email=admin%40example.org&password=secret-pass"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "https://evil.example")
	if resp, err := c.Do(req); err != nil || resp.StatusCode != http.StatusForbidden {
		t.Errorf("cross-origin login: %v %v", resp, err)
	}
	c = e.login(t, "ADMIN@example.org", "secret-pass")
	resp, body := e.get(t, c, "/account")
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, "admin@example.org") {
		t.Errorf("account page: %d", resp.StatusCode)
	}
	// a page read renews the cookie, as using it renews the session
	if sc := resp.Header.Get("Set-Cookie"); !strings.HasPrefix(sc, sessionCookie+"=") || !strings.Contains(sc, "Max-Age=") {
		t.Errorf("cookie not renewed: %q", sc)
	}
	// registration is closed
	if resp, body := e.post(t, e.client(t), "/register", url.Values{"email": {"x@example.org"}}); resp.StatusCode != http.StatusForbidden || e.box.count() != 0 {
		t.Errorf("closed registration: %d, %d mails\n%s", resp.StatusCode, e.box.count(), body)
	}
	resp, _ = e.post(t, c, "/logout", nil)
	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("logout: %d", resp.StatusCode)
	}
	if resp, _ := e.get(t, c, "/account"); resp.StatusCode != http.StatusFound {
		t.Errorf("account after logout: %d", resp.StatusCode)
	}
}

func TestRegistrationWithApproval(t *testing.T) {
	e := newAuthEnv(t)
	e.withAdmin(t, accounts.RegisterApproval)
	anne := e.client(t)
	resp, body := e.post(t, anne, "/register", url.Values{"email": {"Anne@example.org"}})
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, "Check your inbox") {
		t.Fatalf("register: %d\n%s", resp.StatusCode, body)
	}
	if _, ok := e.users.Get("anne@example.org"); ok {
		t.Fatal("account made before the address was confirmed")
	}
	mail, link := e.box.last(t, "anne@example.org")
	if !strings.HasPrefix(link, "https://jw.example.org/account/password?token=") {
		t.Fatalf("link %q in\n%s", link, mail.Body)
	}
	if resp, body := e.get(t, anne, localPath(t, link)); resp.StatusCode != http.StatusOK || !strings.Contains(body, "anne@example.org") {
		t.Fatalf("link page: %d", resp.StatusCode)
	}
	p := localPath(t, link)
	tok := p[strings.Index(p, "token=")+6:]
	tok, _ = url.QueryUnescape(tok)
	if resp, body := e.post(t, anne, "/account/password", url.Values{"token": {tok}, "password": {"anne-pass1"}, "password2": {"other-pass"}}); !strings.Contains(body, "differ") {
		t.Errorf("differing passwords: %d", resp.StatusCode)
	}
	resp, body = e.post(t, anne, "/account/password", url.Values{"token": {tok}, "password": {"anne-pass1"}, "password2": {"anne-pass1"}})
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, "waiting for an admin") {
		t.Fatalf("confirm: %d\n%s", resp.StatusCode, body)
	}
	if u, ok := e.users.Get("anne@example.org"); !ok || !u.Pending {
		t.Fatalf("anne = %+v", u)
	}
	// the admin hears of it
	if m, l := e.box.last(t, "admin@example.org"); !strings.Contains(m.Body, "anne@example.org") || l != "https://jw.example.org/admin/users" {
		t.Errorf("admin mail:\n%s", m.Body)
	}
	// the link is used up
	if resp, _ := e.post(t, anne, "/account/password", url.Values{"token": {tok}, "password": {"anne-pass1"}, "password2": {"anne-pass1"}}); resp.StatusCode != http.StatusNotFound {
		t.Errorf("link used twice: %d", resp.StatusCode)
	}
	if resp, body := e.post(t, anne, "/login", url.Values{"email": {"anne@example.org"}, "password": {"anne-pass1"}}); resp.StatusCode != http.StatusUnauthorized || !strings.Contains(body, "approval") {
		t.Errorf("pending login: %d", resp.StatusCode)
	}
	// a user is no admin
	admin := e.login(t, "admin@example.org", "secret-pass")
	resp, _ = e.post(t, admin, "/admin/users", url.Values{"op": {"approve"}, "email": {"anne@example.org"}})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("approve: %d", resp.StatusCode)
	}
	if m, l := e.box.last(t, "anne@example.org"); !strings.Contains(m.Subject, "approved") || l != "https://jw.example.org/login" {
		t.Errorf("approval mail: %+v", m)
	}
	anne = e.login(t, "anne@example.org", "anne-pass1")
	if resp, _ := e.get(t, anne, "/admin/users"); resp.StatusCode != http.StatusForbidden {
		t.Errorf("admin page for a user: %d", resp.StatusCode)
	}
	// registering an address with an account tells its owner, not the page
	before := e.box.count()
	resp, body = e.post(t, e.client(t), "/register", url.Values{"email": {"anne@example.org"}})
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, "Check your inbox") || e.box.count() != before+1 {
		t.Errorf("register again: %d", resp.StatusCode)
	}
	if m, _ := e.box.last(t, "anne@example.org"); m.Subject != "Your account" {
		t.Errorf("mail = %+v", m)
	}
}

func TestOpenRegistrationSignsIn(t *testing.T) {
	e := newAuthEnv(t)
	e.withAdmin(t, accounts.RegisterOpen)
	c := e.client(t)
	e.post(t, c, "/register", url.Values{"email": {"bob@example.org"}})
	_, link := e.box.last(t, "bob@example.org")
	u, _ := url.Parse(link)
	resp, _ := e.post(t, c, "/account/password", url.Values{"token": {u.Query().Get("token")}, "password": {"bob-pass12"}, "password2": {"bob-pass12"}})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("confirm: %d", resp.StatusCode)
	}
	if resp, _ := e.get(t, c, "/account"); resp.StatusCode != http.StatusOK {
		t.Errorf("signed in after confirming: %d", resp.StatusCode)
	}
}

func TestInviteAndReset(t *testing.T) {
	e := newAuthEnv(t)
	e.withAdmin(t, accounts.RegisterClosed)
	admin := e.login(t, "admin@example.org", "secret-pass")
	resp, body := e.post(t, admin, "/admin/users", url.Values{"op": {"create"}, "email": {"carl@example.org"}, "role": {"user"}})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("invite: %d\n%s", resp.StatusCode, body)
	}
	// an invitation that could not be sent leaves no account behind
	e.box.mu.Lock()
	e.box.fail = errors.New("mail server down")
	e.box.mu.Unlock()
	if resp, _ := e.post(t, admin, "/admin/users", url.Values{"op": {"create"}, "email": {"dora@example.org"}, "role": {"user"}}); resp.StatusCode == http.StatusSeeOther {
		t.Errorf("invite with failing mail: %d", resp.StatusCode)
	}
	if _, ok := e.users.Get("dora@example.org"); ok {
		t.Errorf("account of a failed invitation kept")
	}
	e.box.mu.Lock()
	e.box.fail = nil
	e.box.mu.Unlock()
	m, link := e.box.last(t, "carl@example.org")
	if m.Subject != "Your invitation" {
		t.Errorf("mail = %+v", m)
	}
	// invited: no password, no sign-in yet
	if resp, _ := e.post(t, e.client(t), "/login", url.Values{"email": {"carl@example.org"}, "password": {""}}); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("invited login: %d", resp.StatusCode)
	}
	u, _ := url.Parse(link)
	carl := e.client(t)
	resp, _ = e.post(t, carl, "/account/password", url.Values{"token": {u.Query().Get("token")}, "password": {"carl-pass1"}, "password2": {"carl-pass1"}})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("invitation link: %d", resp.StatusCode)
	}
	// forgotten password: the same answer whether or not the address has an account
	_, known := e.post(t, e.client(t), "/forgot", url.Values{"email": {"carl@example.org"}})
	_, unknown := e.post(t, e.client(t), "/forgot", url.Values{"email": {"nobody@example.org"}})
	if !strings.Contains(known, "a link to choose a new password is on its way") || !strings.Contains(unknown, "a link to choose a new password is on its way") {
		t.Errorf("forgot answers differ")
	}
	_, link = e.box.last(t, "carl@example.org")
	u, _ = url.Parse(link)
	e.post(t, e.client(t), "/account/password", url.Values{"token": {u.Query().Get("token")}, "password": {"carl-pass2"}, "password2": {"carl-pass2"}})
	// the reset ended the old session
	if resp, _ := e.get(t, carl, "/account"); resp.StatusCode != http.StatusFound {
		t.Errorf("old session after reset: %d", resp.StatusCode)
	}
	e.login(t, "carl@example.org", "carl-pass2")
	// the admin cannot lock themselves out
	resp, body = e.post(t, admin, "/admin/users", url.Values{"op": {"delete"}, "email": {"admin@example.org"}})
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, "Not for your own account") {
		t.Errorf("self delete: %d", resp.StatusCode)
	}
	resp, _ = e.post(t, admin, "/admin/users", url.Values{"op": {"delete"}, "email": {"carl@example.org"}})
	if _, ok := e.users.Get("carl@example.org"); ok || resp.StatusCode != http.StatusSeeOther {
		t.Errorf("delete: %d", resp.StatusCode)
	}
}

func TestAnonymousReading(t *testing.T) {
	e := newAuthEnv(t)
	e.withAdmin(t, accounts.RegisterClosed)
	st := e.settings.Get()
	st.Anonymous = true
	if err := e.settings.Save(st); err != nil {
		t.Fatal(err)
	}
	c := e.client(t)
	if resp, _ := e.get(t, c, "/sw.js"); resp.StatusCode != http.StatusOK {
		t.Errorf("sw: %d", resp.StatusCode)
	}
	// reading needs no account, the admin pages do
	if resp, _ := e.get(t, c, "/admin/users"); resp.StatusCode != http.StatusFound {
		t.Errorf("admin page anonymously: %d", resp.StatusCode)
	}
	if resp, _ := e.post(t, c, "/admin/settings", url.Values{"registration": {"open"}}); resp.StatusCode != http.StatusFound {
		t.Errorf("settings post anonymously: %d", resp.StatusCode)
	}
}

func TestSettingsPage(t *testing.T) {
	e := newAuthEnv(t)
	e.withAdmin(t, accounts.RegisterClosed)
	st := e.settings.Get()
	st.SMTP.Username, st.SMTP.Password = "mailer", "mail-secret"
	if err := e.settings.Save(st); err != nil {
		t.Fatal(err)
	}
	admin := e.login(t, "admin@example.org", "secret-pass")
	resp, body := e.get(t, admin, "/admin/settings")
	if resp.StatusCode != http.StatusOK || strings.Contains(body, "mail-secret") {
		t.Fatalf("settings page: %d, password shown: %v", resp.StatusCode, strings.Contains(body, "mail-secret"))
	}
	// Enter in a field saves: the form's first submit button is a save
	_, html, _ := strings.Cut(body, `method="post" action="/admin/settings"`)
	html, _, _ = strings.Cut(html, "</form>")
	if i := strings.Index(html, `type="submit"`); i < 0 || !strings.HasPrefix(html[i:], `type="submit" name="action" value="save"`) {
		t.Errorf("the settings form's default button does not save")
	}
	form := url.Values{"action": {"save"}, "registration": {"open"}, "public_url": {"https://jw.example.org"},
		"smtp_host": {"smtp.example.org"}, "smtp_port": {"465"}, "smtp_security": {"tls"}, "smtp_username": {"mailer"},
		"smtp_from": {"JW <jw@example.org>"}}
	if resp, body := e.post(t, admin, "/admin/settings", form); resp.StatusCode != http.StatusOK || !strings.Contains(body, "Settings saved") {
		t.Fatalf("save: %d\n%s", resp.StatusCode, body)
	}
	got := e.settings.Get()
	if got.Registration != accounts.RegisterOpen || got.SMTP.Password != "mail-secret" || got.SMTP.Port != 465 || got.SMTP.Security != "tls" {
		t.Errorf("settings = %+v", got)
	}
	// registration without a mail server is refused
	form.Set("smtp_host", "")
	form.Set("smtp_from", "")
	if _, body := e.post(t, admin, "/admin/settings", form); !strings.Contains(body, "Registration needs a mail server") {
		t.Errorf("no refusal")
	}
}

// The settings of the menu lead to each other through tabs: an admin's four,
// another user's two, the page shown marked.
func TestSettingsTabs(t *testing.T) {
	e := newAuthEnv(t)
	e.withAdmin(t, accounts.RegisterClosed)
	if err := e.users.Create("bob@example.org", "bob-pass1", accounts.RoleUser, false, "en"); err != nil {
		t.Fatal(err)
	}
	all := []string{"/account", "/account/keys", "/admin/users", "/admin/settings"}
	tabs := func(body string) (links []string, current string) {
		_, nav, _ := strings.Cut(body, `<nav class="tabs"`)
		nav, _, _ = strings.Cut(nav, "</nav>")
		for _, m := range regexp.MustCompile(`<a href="([^"]+)"( class="active" aria-current="page")?`).FindAllStringSubmatch(nav, -1) {
			links = append(links, m[1])
			if m[2] != "" {
				current = m[1]
			}
		}
		return links, current
	}
	admin := e.login(t, "admin@example.org", "secret-pass")
	for _, path := range all {
		_, body := e.get(t, admin, path)
		if links, current := tabs(body); !slices.Equal(links, all) || current != path {
			t.Errorf("admin's tabs on %s: %v, current %q", path, links, current)
		}
		if strings.Contains(body, `class="auth-links"`) {
			t.Errorf("%s still links to other settings below", path)
		}
	}
	bob := e.login(t, "bob@example.org", "bob-pass1")
	for _, path := range all[:2] {
		_, body := e.get(t, bob, path)
		if links, current := tabs(body); !slices.Equal(links, all[:2]) || current != path {
			t.Errorf("user's tabs on %s: %v, current %q", path, links, current)
		}
	}
}

// The page's script sends the test mail in the background and gets its
// outcome alone, not the page; nothing is saved by it.
func TestSettingsTestMailInBackground(t *testing.T) {
	e := newAuthEnv(t)
	e.withAdmin(t, accounts.RegisterClosed)
	admin := e.login(t, "admin@example.org", "secret-pass")
	before := e.settings.Get()
	test := func(to string) (*http.Response, map[string]string) {
		t.Helper()
		form := url.Values{"action": {"test"}, "test_to": {to}, "registration": {"closed"}, "public_url": {"https://jw.example.org"},
			"smtp_host": {"other.example.org"}, "smtp_port": {"2525"}, "smtp_security": {"starttls"}, "smtp_from": {"jw@example.org"}}
		req, _ := http.NewRequest(http.MethodPost, e.srv.URL+"/admin/settings", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Origin", e.srv.URL)
		req.Header.Set("Accept", "application/json")
		resp, err := admin.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var got map[string]string
		if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
			t.Fatalf("test mail reply: %d, %s: %v", resp.StatusCode, resp.Header.Get("Content-Type"), err)
		}
		return resp, got
	}
	resp, got := test("me@example.org")
	if resp.StatusCode != http.StatusOK || got["notice"] == "" || got["error"] != "" || e.box.count() != 1 {
		t.Errorf("test mail: %d %v, %d mails", resp.StatusCode, got, e.box.count())
	}
	if _, got := test("not-an-address"); got["error"] == "" || got["notice"] != "" || e.box.count() != 1 {
		t.Errorf("test mail to a bad address: %v, %d mails", got, e.box.count())
	}
	if after := e.settings.Get(); after.SMTP != before.SMTP {
		t.Errorf("test mail saved the settings: %+v", after.SMTP)
	}
}

func TestClientIPBehindProxy(t *testing.T) {
	proxies, err := ParseProxies("10.0.0.0/8, 192.168.1.5")
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{proxies: proxies}
	loopback := &Server{}
	for _, c := range []struct {
		s      *Server
		remote string
		xff    string
		want   string
	}{
		{s, "203.0.113.9:1234", "1.2.3.4", "203.0.113.9"},                // not a proxy: header ignored
		{s, "10.1.2.3:1234", "1.2.3.4", "1.2.3.4"},                       // proxy
		{s, "10.1.2.3:1234", "6.6.6.6, 1.2.3.4, 192.168.1.5", "1.2.3.4"}, // chain: last untrusted
		{s, "10.1.2.3:1234", "", "10.1.2.3"},
		{s, "127.0.0.1:1234", "1.2.3.4", "127.0.0.1"},      // loopback not in the list
		{loopback, "127.0.0.1:1234", "1.2.3.4", "1.2.3.4"}, // default: loopback
		{loopback, "10.1.2.3:1234", "1.2.3.4", "10.1.2.3"},
	} {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = c.remote
		if c.xff != "" {
			r.Header.Set("X-Forwarded-For", c.xff)
		}
		if got := c.s.clientIP(r); got != c.want {
			t.Errorf("%s %q: %s, want %s", c.remote, c.xff, got, c.want)
		}
	}
	if _, err := ParseProxies("nonsense"); err == nil {
		t.Error("bad proxy accepted")
	}
}

func TestSameOriginComparesScheme(t *testing.T) {
	s := &Server{}
	for _, c := range []struct {
		remote, origin, proto string
		tls, want             bool
	}{
		{"203.0.113.9:1", "http://jw.example.org", "", false, true},
		{"203.0.113.9:1", "https://jw.example.org", "", false, false},
		{"203.0.113.9:1", "http://jw.example.org", "", true, false},
		{"203.0.113.9:1", "https://jw.example.org", "", true, true},
		{"127.0.0.1:1", "https://jw.example.org", "https", false, true}, // trusted proxy
		{"127.0.0.1:1", "http://jw.example.org", "https", false, false},
		{"203.0.113.9:1", "https://jw.example.org", "https", false, false}, // header of anybody
		{"203.0.113.9:1", "http://evil.example", "", false, false},
	} {
		r := httptest.NewRequest(http.MethodPost, "http://jw.example.org/login", nil)
		r.RemoteAddr = c.remote
		r.Header.Set("Origin", c.origin)
		if c.proto != "" {
			r.Header.Set("X-Forwarded-Proto", c.proto)
		}
		if c.tls {
			r.TLS = &tls.ConnectionState{}
		}
		if got := s.sameOrigin(r); got != c.want {
			t.Errorf("%+v: %v", c, got)
		}
	}
}

var keyRe = regexp.MustCompile(accounts.KeyPrefix + `[A-Za-z0-9_-]{43}`)

// callAPI posts to the command endpoint with a key (or none): 401 without a
// usable key, 404 past the gate (the test server runs no commands).
func (e *authEnv) callAPI(t *testing.T, header, key string) int {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, e.srv.URL+remote.Path, strings.NewReader(`{"args":["dailytext"]}`))
	req.Header.Set("Content-Type", "application/json")
	switch header {
	case "bearer":
		req.Header.Set("Authorization", "Bearer "+key)
	case "x-api-key":
		req.Header.Set("X-API-Key", key)
	case "basic":
		req.SetBasicAuth("admin@example.org", "secret-pass")
	}
	resp, err := e.srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized && !strings.HasPrefix(resp.Header.Get("WWW-Authenticate"), "Bearer") {
		t.Errorf("401 without a Bearer challenge")
	}
	return resp.StatusCode
}

func TestAPIKeys(t *testing.T) {
	e := newAuthEnv(t)
	e.withAdmin(t, accounts.RegisterClosed)
	st := e.settings.Get()
	st.Anonymous = true // the pages for everybody: the API still not
	if err := e.settings.Save(st); err != nil {
		t.Fatal(err)
	}
	if resp, _ := e.get(t, e.client(t), "/api/v1/languages"); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("API anonymously: %d", resp.StatusCode)
	}
	// what the pages' scripts ask for follows the pages
	if resp, _ := e.get(t, e.client(t), "/ui/open"); resp.StatusCode == http.StatusUnauthorized {
		t.Errorf("the pages' endpoints refused anonymous readers")
	}
	if code := e.callAPI(t, "", ""); code != http.StatusUnauthorized {
		t.Errorf("no key: %d", code)
	}
	if code := e.callAPI(t, "basic", ""); code != http.StatusUnauthorized {
		t.Errorf("basic authentication: %d", code)
	}

	admin := e.login(t, "admin@example.org", "secret-pass")
	resp, body := e.post(t, admin, "/account/keys", url.Values{"op": {"create"}, "name": {"laptop"}, "expires": {"never"}})
	key := keyRe.FindString(body)
	if resp.StatusCode != http.StatusOK || key == "" {
		t.Fatalf("create: %d\n%s", resp.StatusCode, body)
	}
	if _, body := e.get(t, admin, "/account/keys"); strings.Contains(body, key) || !strings.Contains(body, "laptop") {
		t.Errorf("the secret shows again, or the key is not listed")
	}
	for _, h := range []string{"bearer", "x-api-key"} {
		if code := e.callAPI(t, h, key); code != http.StatusNotFound {
			t.Errorf("%s: %d", h, code)
		}
	}
	if code := e.callAPI(t, "bearer", key+"x"); code != http.StatusUnauthorized {
		t.Errorf("wrong key: %d", code)
	}

	// a key reads, but does not open the account's pages
	for _, path := range []string{"/account/keys", "/account", "/admin/users", "/admin/settings"} {
		req, _ := http.NewRequest(http.MethodGet, e.srv.URL+path, nil)
		req.Header.Set("Authorization", "Bearer "+key)
		resp, err := e.srv.Client().Transport.RoundTrip(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("%s with a key: %d", path, resp.StatusCode)
		}
	}

	id := e.users.Keys("admin@example.org")[0].ID
	// a date in the past is refused, a later one taken
	_, body = e.post(t, admin, "/account/keys", url.Values{"op": {"update"}, "id": {id}, "name": {"laptop"}, "expires": {"date"}, "expires_date": {"2020-01-01"}})
	if !strings.Contains(body, "The expiry must lie in the future") {
		t.Errorf("past date accepted")
	}
	_, body = e.post(t, admin, "/account/keys", url.Values{"op": {"update"}, "id": {id}, "name": {"old laptop"}, "expires": {"30d"}})
	if !strings.Contains(body, "Key old laptop changed") || e.users.Keys("admin@example.org")[0].Expires.IsZero() {
		t.Errorf("update:\n%s", body)
	}

	// rotation: a new secret, the old one refused at once
	_, body = e.post(t, admin, "/account/keys", url.Values{"op": {"rotate"}, "id": {id}, "grace": {""}, "expires": {"keep"}})
	fresh := keyRe.FindString(body)
	if fresh == "" || fresh == key {
		t.Fatalf("rotate:\n%s", body)
	}
	if code := e.callAPI(t, "bearer", key); code != http.StatusUnauthorized {
		t.Errorf("old secret after rotation: %d", code)
	}
	if code := e.callAPI(t, "bearer", fresh); code != http.StatusNotFound {
		t.Errorf("new secret: %d", code)
	}

	_, body = e.post(t, admin, "/account/keys", url.Values{"op": {"delete"}, "id": {id}})
	if !strings.Contains(body, "Key old laptop deleted") {
		t.Errorf("delete:\n%s", body)
	}
	if code := e.callAPI(t, "bearer", fresh); code != http.StatusUnauthorized {
		t.Errorf("deleted key: %d", code)
	}

	// an admin deletes somebody's keys
	_ = e.users.Create("bob@example.org", "bob-pass1", accounts.RoleUser, false, "en")
	bobKey, _, _ := e.users.CreateKey("bob@example.org", "script", time.Time{})
	if resp, _ := e.post(t, admin, "/admin/users", url.Values{"op": {"keys"}, "email": {"bob@example.org"}}); resp.StatusCode != http.StatusSeeOther {
		t.Errorf("delete keys of bob: %d", resp.StatusCode)
	}
	if code := e.callAPI(t, "bearer", bobKey); code != http.StatusUnauthorized {
		t.Errorf("bob's key after the admin deleted it: %d", code)
	}
}

// The pages' own lookups, like the API, never change the reader's language.
func TestUILookupsKeepTheLanguage(t *testing.T) {
	for _, p := range []string{"/ui/bible/nav", "/ui/languages", "/api/v1/languages"} {
		if remembersOn(p) {
			t.Errorf("%s remembers the language", p)
		}
	}
	if !remembersOn("/bible") {
		t.Errorf("a page does not remember the language")
	}
}

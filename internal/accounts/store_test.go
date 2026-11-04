package accounts

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func openTest(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "accounts.json"))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestNormalizeEmail(t *testing.T) {
	for in, want := range map[string]string{
		" Anne@Example.org ": "anne@example.org",
		"a.b+c@d.example":    "a.b+c@d.example",
	} {
		if got, err := NormalizeEmail(in); err != nil || got != want {
			t.Errorf("NormalizeEmail(%q) = %q, %v", in, got, err)
		}
	}
	for _, in := range []string{"", "anne", "anne@", "@example.org", "Anne <anne@example.org>", "anne@localhost", "a b@example.org"} {
		if _, err := NormalizeEmail(in); !errors.Is(err, ErrBadEmail) {
			t.Errorf("NormalizeEmail(%q) accepted", in)
		}
	}
}

func TestAccountsPersistAndProtectTheLastAdmin(t *testing.T) {
	s := openTest(t)
	if err := s.Create("Admin@example.org", "secret-pass", RoleAdmin, false, ""); err != nil {
		t.Fatal(err)
	}
	if err := s.Create("admin@EXAMPLE.org", "secret-pass", RoleUser, false, ""); !errors.Is(err, ErrExists) {
		t.Errorf("duplicate: %v", err)
	}
	if err := s.Create("short@example.org", "short", RoleUser, false, ""); !errors.Is(err, ErrBadPass) {
		t.Errorf("short password: %v", err)
	}
	if err := s.Delete("admin@example.org"); !errors.Is(err, ErrLastAdmin) {
		t.Errorf("delete last admin: %v", err)
	}
	user := RoleUser
	if err := s.Apply("admin@example.org", Update{Role: &user}); !errors.Is(err, ErrLastAdmin) {
		t.Errorf("demote last admin: %v", err)
	}
	re, err := Open(s.Path())
	if err != nil {
		t.Fatal(err)
	}
	if !re.HasAdmin() {
		t.Error("admin lost on reload")
	}
}

func TestLoginAndSessions(t *testing.T) {
	s := openTest(t)
	_ = s.Create("anne@example.org", "secret-pass", RoleUser, true, "")
	if _, _, err := s.Login("anne@example.org", "secret-pass", "ip"); !errors.Is(err, ErrPending) {
		t.Errorf("pending: %v", err)
	}
	_ = s.Apply("anne@example.org", Update{Approve: true})
	tok, u, err := s.Login("ANNE@example.org", "secret-pass", "ip")
	if err != nil || u.Email != "anne@example.org" {
		t.Fatalf("login: %v", err)
	}
	if s.Session(tok) == nil {
		t.Fatal("no session")
	}
	off := true
	_ = s.Apply("anne@example.org", Update{Disabled: &off})
	if s.Session(tok) != nil {
		t.Error("session of a disabled account")
	}
}

func TestRateLimit(t *testing.T) {
	s := openTest(t)
	_ = s.Create("anne@example.org", "secret-pass", RoleUser, false, "")
	for range limitMax {
		_, _ = s.Check("anne@example.org", "wrong-pass", "ip")
	}
	if _, err := s.Check("anne@example.org", "secret-pass", "ip"); !errors.Is(err, ErrTooMany) {
		t.Errorf("after %d failures: %v", limitMax, err)
	}
	// another client is not locked out
	if _, err := s.Check("anne@example.org", "secret-pass", "other"); err != nil {
		t.Errorf("other client: %v", err)
	}
}

func TestBasicCacheFollowsPassword(t *testing.T) {
	s := openTest(t)
	_ = s.Create("anne@example.org", "secret-pass", RoleUser, false, "")
	if _, err := s.CheckBasic("anne@example.org", "secret-pass", "ip"); err != nil {
		t.Fatal(err)
	}
	pw := "other-pass"
	_ = s.Apply("anne@example.org", Update{Password: &pw})
	if _, err := s.CheckBasic("anne@example.org", "secret-pass", "ip"); !errors.Is(err, ErrBadLogin) {
		t.Errorf("old password after change: %v", err)
	}
}

func TestTokens(t *testing.T) {
	s := openTest(t)
	first, _ := s.NewToken(TokenRegister, "anne@example.org", "de", time.Hour)
	second, err := s.NewToken(TokenRegister, "anne@example.org", "de", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	// the older link works until the new one was delivered
	if _, err := s.PeekToken(first, TokenRegister); err != nil {
		t.Errorf("older link before delivery: %v", err)
	}
	failed, _ := s.NewToken(TokenRegister, "anne@example.org", "de", time.Hour)
	s.Revoke(failed)
	if _, err := s.PeekToken(failed, TokenRegister); !errors.Is(err, ErrBadToken) {
		t.Error("a revoked link works")
	}
	s.Supersede(second)
	if _, err := s.PeekToken(first, TokenRegister); !errors.Is(err, ErrBadToken) {
		t.Error("an older link still works")
	}
	if _, err := s.PeekToken(second, TokenReset); !errors.Is(err, ErrBadToken) {
		t.Error("a link of another kind works")
	}
	u, err := s.UseRegistration(second, "secret-pass", false)
	if err != nil || u.Email != "anne@example.org" || u.Lang != "de" || !u.Active() {
		t.Fatalf("registration: %+v %v", u, err)
	}
	if _, err := s.UseRegistration(second, "secret-pass", false); !errors.Is(err, ErrBadToken) {
		t.Errorf("link used twice: %v", err)
	}
	expired, _ := s.NewToken(TokenReset, "anne@example.org", "", -time.Second)
	if _, err := s.UsePasswordLink(expired, "other-pass"); !errors.Is(err, ErrBadToken) {
		t.Errorf("expired link: %v", err)
	}
}

func TestSettingsValidate(t *testing.T) {
	st := Defaults()
	st.Registration = RegisterOpen
	if err := st.Validate(); !errors.Is(err, ErrRegisterNoMail) {
		t.Errorf("open without mail: %v", err)
	}
	st.SMTP = SMTP{Host: "smtp.example.org", Port: 587, From: "JW <jw@example.org>"}
	st.PublicURL = "https://jw.example.org/path"
	if err := st.Validate(); !errors.Is(err, ErrBadPublicURL) {
		t.Errorf("url with path: %v", err)
	}
	for _, bad := range []string{"https://jw.example.org#x", "https://jw.example.org?", "https://jw.example.org/#"} {
		st.PublicURL = bad
		if err := st.Validate(); !errors.Is(err, ErrBadPublicURL) {
			t.Errorf("%s: %v", bad, err)
		}
	}
	st.PublicURL = "https://jw.example.org"
	if err := st.Validate(); err != nil {
		t.Errorf("valid: %v", err)
	}
}

// The server and jw serve user share the file: a change one makes survives
// the other's next save, however soon it comes.
func TestEditsOfAnotherProcessSurvive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "accounts.json")
	server, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := server.Create("admin@example.org", "admin-pass", RoleAdmin, false, "en"); err != nil {
		t.Fatal(err)
	}
	server.HasAdmin() // the server has just looked at the file
	cli, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	pw := "new-password"
	if err := cli.Apply("admin@example.org", Update{Password: &pw}); err != nil {
		t.Fatal(err)
	}
	// a save of the server within its refresh interval
	tok, err := server.StartSession("admin@example.org")
	if err != nil {
		t.Fatal(err)
	}
	again, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := again.Check("admin@example.org", pw, "cli"); err != nil {
		t.Errorf("password of the CLI lost: %v", err)
	}
	if again.Session(tok) == nil {
		t.Errorf("session of the server lost")
	}
}

// A change that cannot be saved does not take effect.
func TestFailedSaveChangesNothing(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "accounts.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Create("admin@example.org", "admin-pass", RoleAdmin, false, "en"); err != nil {
		t.Fatal(err)
	}
	saveFile = func(string, []byte) error { return errors.New("disk full") }
	defer func() { saveFile = writeFile }()
	if err := s.Create("bob@example.org", "bob-pass1", RoleUser, false, "en"); err == nil {
		t.Fatal("create in a read-only folder")
	}
	if _, ok := s.Get("bob@example.org"); ok {
		t.Error("an account that was not saved exists")
	}
	if _, ok := s.Get("admin@example.org"); !ok {
		t.Error("the saved account is gone")
	}
}

// A new password ends the password links sent before.
func TestPasswordChangeEndsLinks(t *testing.T) {
	s := openTest(t)
	if err := s.Create("anne@example.org", "anne-pass1", RoleUser, false, "en"); err != nil {
		t.Fatal(err)
	}
	link, _ := s.NewToken(TokenReset, "anne@example.org", "en", time.Hour)
	pw := "anne-pass2"
	if err := s.Apply("anne@example.org", Update{Password: &pw}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PeekToken(link, TokenReset); !errors.Is(err, ErrBadToken) {
		t.Error("link works after passwd")
	}
	link, _ = s.NewToken(TokenReset, "anne@example.org", "en", time.Hour)
	if err := s.ChangePassword("anne@example.org", pw, "anne-pass3", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PeekToken(link, TokenReset); !errors.Is(err, ErrBadToken) {
		t.Error("link works after a change of the password")
	}
}

// A link made while an older one was still being sent survives that one's
// delivery.
func TestSupersedeKeepsNewerLinks(t *testing.T) {
	s := openTest(t)
	first, _ := s.NewToken(TokenReset, "anne@example.org", "", time.Hour)
	time.Sleep(time.Millisecond)
	second, _ := s.NewToken(TokenReset, "anne@example.org", "", time.Hour)
	s.Supersede(first)
	if _, err := s.PeekToken(second, TokenReset); err != nil {
		t.Errorf("newer link ended: %v", err)
	}
	s.Supersede(second)
	if _, err := s.PeekToken(first, TokenReset); !errors.Is(err, ErrBadToken) {
		t.Error("older link still works")
	}
}

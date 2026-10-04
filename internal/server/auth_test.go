package server_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/dgrieser/jw-cli/internal/server"
)

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestCredentialsHtpasswd(t *testing.T) {
	b, err := bcrypt.GenerateFromPassword([]byte("bpass"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	// htpasswd -B writes the $2y$ prefix
	bc := "$2y$" + strings.TrimPrefix(string(b), "$2a$")
	dir := t.TempDir()
	p := writeFile(t, dir, ".htpasswd", strings.Join([]string{
		"# comment",
		"bob:" + bc,
		// openssl passwd -apr1 -salt abcdefgh secret
		"anne:$apr1$abcdefgh$h9FWgUz3n9YxylKLlR5SQ/",
		// openssl passwd -apr1 -salt x/y.z pässwört-langer-als-sechzehn
		"long:$apr1$x/y.z$iipgWuqM/avvQK3ivW7cY.",
		// htpasswd -s
		"sam:{SHA}5en6G6MezRroT3XKqkdPOmY/BfQ=",
		"",
	}, "\n"))
	var c server.Credentials
	if err := c.LoadFile(p); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		user, pass string
		want       bool
	}{
		{"bob", "bpass", true},
		{"bob", "wrong", false},
		{"anne", "secret", true},
		{"anne", "Secret", false},
		{"long", "pässwört-langer-als-sechzehn", true},
		{"sam", "secret", true},
		{"sam", "", false},
		{"nobody", "secret", false},
	} {
		if got := c.Check(tc.user, tc.pass); got != tc.want {
			t.Errorf("Check(%q, %q) = %v, want %v", tc.user, tc.pass, got, tc.want)
		}
	}
}

func TestCredentialsHtaccess(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "users", "anne:$apr1$abcdefgh$h9FWgUz3n9YxylKLlR5SQ/\n")
	p := writeFile(t, dir, ".htaccess", `AuthType Basic
AuthName "Members only"
AuthUserFile users
Require valid-user
`)
	var c server.Credentials
	if err := c.LoadFile(p); err != nil {
		t.Fatal(err)
	}
	if c.Realm != "Members only" {
		t.Errorf("realm = %q", c.Realm)
	}
	if !c.Check("anne", "secret") {
		t.Error("anne from AuthUserFile not let in")
	}

	// a missing AuthUserFile is an error, not an open door
	p = writeFile(t, dir, "broken", "AuthUserFile /does/not/exist\n")
	if err := new(server.Credentials).LoadFile(p); err == nil {
		t.Error("missing AuthUserFile: want error")
	}
}

func TestCredentialsUnsupportedHash(t *testing.T) {
	p := writeFile(t, t.TempDir(), ".htpasswd", "old:rqXexS6ZhobKA\n")
	if err := new(server.Credentials).LoadFile(p); err == nil {
		t.Error("crypt(3) hash: want error")
	}
}

func TestRequireAuth(t *testing.T) {
	var c server.Credentials
	c.Add("me", "pw")
	h := c.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	}))
	for _, tc := range []struct {
		name       string
		user, pass string
		set        bool
		want       int
	}{
		{"none", "", "", false, http.StatusUnauthorized},
		{"wrong", "me", "nope", true, http.StatusUnauthorized},
		{"right", "me", "pw", true, http.StatusOK},
		{"right again (cached)", "me", "pw", true, http.StatusOK},
	} {
		r := httptest.NewRequest("GET", "/", nil)
		if tc.set {
			r.SetBasicAuth(tc.user, tc.pass)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Errorf("%s: status %d, want %d", tc.name, w.Code, tc.want)
		}
		if w.Code == http.StatusUnauthorized && !strings.HasPrefix(w.Header().Get("WWW-Authenticate"), `Basic realm="jw"`) {
			t.Errorf("%s: challenge %q", tc.name, w.Header().Get("WWW-Authenticate"))
		}
	}

	var empty *server.Credentials
	w := httptest.NewRecorder()
	empty.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})).ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != http.StatusOK {
		t.Errorf("no credentials: status %d, want 200", w.Code)
	}
}

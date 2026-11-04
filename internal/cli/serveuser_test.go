package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/dgrieser/jw-cli/internal/accounts"
	"github.com/dgrieser/jw-cli/internal/app"
)

// serveUser runs jw serve user … against a data folder.
func serveUser(t *testing.T, dir, stdin string, args ...string) (string, string, error) {
	t.Helper()
	a := app.New(app.Flags{})
	var out, errw bytes.Buffer
	a.Stdout, a.Stderr = &out, &errw
	root := NewRootCmd(a)
	root.SetArgs(append([]string{"serve", "--data-dir", dir, "user"}, args...))
	root.SetIn(strings.NewReader(stdin))
	root.SetOut(&out)
	root.SetErr(&errw)
	err := root.Execute()
	return out.String(), errw.String(), err
}

func TestServeUserKeys(t *testing.T) {
	dir := t.TempDir()
	if _, e, err := serveUser(t, dir, "anne-pass1\n", "add", "anne@example.org"); err != nil {
		t.Fatalf("add: %v %s", err, e)
	}
	secret, info, err := serveUser(t, dir, "", "key", "add", "anne@example.org", "backup", "--expires", "30d")
	secret = strings.TrimSpace(secret)
	if err != nil || !strings.HasPrefix(secret, accounts.KeyPrefix) || strings.Contains(secret, "\n") || !strings.Contains(info, "not shown again") {
		t.Fatalf("key add: %q %q %v", secret, info, err)
	}
	users, _ := accounts.Open(dir + "/accounts.json")
	if u, _, err := users.CheckKey(secret, "test"); err != nil || u.Email != "anne@example.org" {
		t.Fatalf("the key does not work: %v", err)
	}
	if out, _, err := serveUser(t, dir, "", "key", "list", "anne@example.org"); err != nil || !strings.Contains(out, "backup") || strings.Contains(out, secret) {
		t.Errorf("key list: %q %v", out, err)
	}
	if _, _, err := serveUser(t, dir, "", "key", "update", "anne@example.org", "backup", "--name", "nightly", "--expires", "never"); err != nil {
		t.Errorf("key update: %v", err)
	}
	fresh, info, err := serveUser(t, dir, "", "key", "rotate", "anne@example.org", "nightly", "--grace", "1h")
	fresh = strings.TrimSpace(fresh)
	if err != nil || fresh == secret || !strings.Contains(info, "old secret works until") {
		t.Fatalf("key rotate: %q %q %v", fresh, info, err)
	}
	users, _ = accounts.Open(dir + "/accounts.json")
	for _, s := range []string{secret, fresh} {
		if _, _, err := users.CheckKey(s, "test"); err != nil {
			t.Errorf("secret during the grace: %v", err)
		}
	}
	if out, _, err := serveUser(t, dir, "", "key", "delete", "anne@example.org", "nightly"); err != nil || !strings.Contains(out, "Deleted key nightly") {
		t.Errorf("key delete: %q %v", out, err)
	}
	if _, _, err := serveUser(t, dir, "", "key", "delete", "anne@example.org", "nightly"); err == nil {
		t.Errorf("deleting a missing key succeeds")
	}
	// a key named like another key's ID: the ID wins
	_, _, _ = serveUser(t, dir, "", "key", "add", "anne@example.org", "first")
	users, _ = accounts.Open(dir + "/accounts.json")
	firstID := users.Keys("anne@example.org")[0].ID
	_, _, _ = serveUser(t, dir, "", "key", "add", "anne@example.org", firstID)
	if _, _, err := serveUser(t, dir, "", "key", "delete", "anne@example.org", firstID); err != nil {
		t.Fatal(err)
	}
	users, _ = accounts.Open(dir + "/accounts.json")
	if ks := users.Keys("anne@example.org"); len(ks) != 1 || ks[0].Name != firstID {
		t.Errorf("deleted by name instead of ID: %+v", ks)
	}
	if _, _, err := serveUser(t, dir, "", "key", "add", "anne@example.org", "x", "--expires", "2020-01-01"); err == nil {
		t.Errorf("a past expiry is taken")
	}
}

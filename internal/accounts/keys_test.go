package accounts

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestKeys(t *testing.T) {
	s := openTest(t)
	if err := s.Create("anne@example.org", "anne-pass1", RoleUser, false, "en"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.CreateKey("nobody@example.org", "ci", time.Time{}); !errors.Is(err, ErrNotFound) {
		t.Errorf("key for nobody: %v", err)
	}
	secret, k, err := s.CreateKey("anne@example.org", " laptop ", time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(secret, KeyPrefix) || k.Name != "laptop" || !k.Expires.IsZero() || k.ID == "" {
		t.Errorf("key = %q %+v", secret, k)
	}
	if strings.Contains(k.Hint, secret[len(KeyPrefix):len(KeyPrefix)+12]) {
		t.Errorf("hint shows too much: %s", k.Hint)
	}
	if _, _, err := s.CreateKey("anne@example.org", "LAPTOP", time.Time{}); !errors.Is(err, ErrKeyTaken) {
		t.Errorf("same name twice: %v", err)
	}
	if _, _, err := s.CreateKey("anne@example.org", "old", time.Now().Add(-time.Hour)); !errors.Is(err, ErrKeyExpiry) {
		t.Errorf("expiry in the past: %v", err)
	}
	u, got, err := s.CheckKey(secret, "ip")
	if err != nil || u.Email != "anne@example.org" || got.ID != k.ID {
		t.Fatalf("check: %v %+v %v", u, got, err)
	}
	if _, _, err := s.CheckKey(KeyPrefix+"wrong", "ip"); !errors.Is(err, ErrBadKey) {
		t.Errorf("wrong key: %v", err)
	}
	if _, _, err := s.CheckKey("Bearer nonsense", "ip"); !errors.Is(err, ErrBadKey) {
		t.Errorf("not a key: %v", err)
	}

	// renamed, and expiring
	name := "work laptop"
	soon := time.Now().Add(time.Hour)
	if k, err = s.UpdateKey("anne@example.org", k.ID, KeyUpdate{Name: &name, Expires: &soon}); err != nil || k.Name != name || k.Expires.IsZero() {
		t.Fatalf("update: %+v %v", k, err)
	}
	if _, err := s.UpdateKey("bob@example.org", k.ID, KeyUpdate{Name: &name}); !errors.Is(err, ErrKeyNotFound) {
		t.Errorf("update of somebody else's key: %v", err)
	}

	// rotation without grace: the old secret stops at once
	fresh, k2, err := s.RotateKey("anne@example.org", k.ID, 0, nil)
	if err != nil || fresh == secret || k2.ID != k.ID || k2.Rotated.IsZero() || k2.Expires.IsZero() {
		t.Fatalf("rotate: %+v %v", k2, err)
	}
	if _, _, err := s.CheckKey(secret, "ip"); !errors.Is(err, ErrBadKey) {
		t.Errorf("old secret after rotation: %v", err)
	}
	// with grace: both work for a while
	newer, k3, err := s.RotateKey("anne@example.org", k.ID, time.Hour, &time.Time{})
	if err != nil || !k3.Expires.IsZero() || !k3.OldValid() {
		t.Fatalf("rotate with grace: %+v %v", k3, err)
	}
	for _, sec := range []string{fresh, newer} {
		if _, _, err := s.CheckKey(sec, "ip"); err != nil {
			t.Errorf("secret during grace: %v", err)
		}
	}

	// a disabled account's keys stop working
	off := true
	_ = s.Apply("anne@example.org", Update{Disabled: &off})
	if _, _, err := s.CheckKey(newer, "ip"); !errors.Is(err, ErrDisabled) {
		t.Errorf("disabled account: %v", err)
	}
	on := false
	_ = s.Apply("anne@example.org", Update{Disabled: &on})

	if err := s.DeleteKey("anne@example.org", k.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.CheckKey(newer, "ip"); !errors.Is(err, ErrBadKey) {
		t.Errorf("deleted key: %v", err)
	}
	if len(s.Keys("anne@example.org")) != 0 {
		t.Errorf("keys left: %v", s.Keys("anne@example.org"))
	}
}

func TestKeysExpireAndPersist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "accounts.json")
	s, _ := Open(path)
	_ = s.Create("anne@example.org", "anne-pass1", RoleUser, false, "en")
	secret, k, err := s.CreateKey("anne@example.org", "ci", time.Now().Add(50*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	again, _ := Open(path)
	if _, _, err := again.CheckKey(secret, "ip"); err != nil {
		t.Fatalf("key after reopening: %v", err)
	}
	time.Sleep(60 * time.Millisecond)
	if _, _, err := again.CheckKey(secret, "ip"); !errors.Is(err, ErrKeyExpired) {
		t.Errorf("expired key: %v", err)
	}
	// an expired key comes back with a rotation and a new expiry
	if _, _, err := again.RotateKey("anne@example.org", k.ID, 0, nil); !errors.Is(err, ErrKeyExpiry) {
		t.Errorf("rotation keeping a past expiry: %v", err)
	}
	fresh, _, err := again.RotateKey("anne@example.org", k.ID, time.Hour, &time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := again.CheckKey(fresh, "ip"); err != nil {
		t.Errorf("rotated key: %v", err)
	}
	if _, _, err := again.CheckKey(secret, "ip"); err == nil {
		t.Errorf("the expired secret got a grace period")
	}
	// deleting the account deletes its keys
	if err := again.Delete("anne@example.org"); err != nil {
		t.Fatal(err)
	}
	_ = again.Create("anne@example.org", "anne-pass1", RoleUser, false, "en")
	if len(again.Keys("anne@example.org")) != 0 {
		t.Errorf("keys outlived their account")
	}
}

func TestKeyLimits(t *testing.T) {
	s := openTest(t)
	_ = s.Create("anne@example.org", "anne-pass1", RoleUser, false, "en")
	for i := range MaxKeys {
		if _, _, err := s.CreateKey("anne@example.org", "k"+string(rune('a'+i%26))+string(rune('a'+i/26)), time.Time{}); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := s.CreateKey("anne@example.org", "one more", time.Time{}); !errors.Is(err, ErrManyKeys) {
		t.Errorf("past the limit: %v", err)
	}
	if _, err := CheckKeyName(strings.Repeat("x", 65)); !errors.Is(err, ErrKeyName) {
		t.Errorf("long name: %v", err)
	}
	if _, err := CheckKeyName("a\nb"); !errors.Is(err, ErrKeyName) {
		t.Errorf("control character: %v", err)
	}
	if n, err := s.DeleteKeys("anne@example.org"); err != nil || n != MaxKeys {
		t.Errorf("delete all: %d %v", n, err)
	}
	// failed keys count like failed sign-ins
	for range 40 {
		_, _, _ = s.CheckKey(KeyPrefix+"guess", "attacker")
	}
	if _, _, err := s.CheckKey(KeyPrefix+"guess", "attacker"); !errors.Is(err, ErrTooMany) {
		t.Errorf("no limit on guesses: %v", err)
	}
}

func TestParseExpiry(t *testing.T) {
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	for in, want := range map[string]time.Time{
		"":           {},
		"never":      {},
		"30d":        now.AddDate(0, 0, 30),
		"2w":         now.AddDate(0, 0, 14),
		"1y":         now.AddDate(1, 0, 0),
		"36h":        now.Add(36 * time.Hour),
		"2027-01-31": time.Date(2027, 2, 1, 0, 0, 0, 0, time.UTC),
	} {
		got, err := ParseExpiry(in, now, time.UTC)
		if err != nil || !got.Equal(want) {
			t.Errorf("%q: %v %v, want %v", in, got, err, want)
		}
	}
	for _, bad := range []string{"2020-01-01", "-3d", "soon", "0d"} {
		if _, err := ParseExpiry(bad, now, time.UTC); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

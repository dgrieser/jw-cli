package accounts

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// API keys let programs use the JSON API of jw serve, jw --server among them:
// a user makes as many as they like, each with a name, and expiring or not.
// The file keeps only the hash of a key's secret, which is shown once, when
// the key is made or rotated.

// KeyPrefix starts every API key, so a key is recognised (and found by
// secret scanners) wherever it ends up.
const KeyPrefix = "jw_"

// MaxKeys bounds the keys of one account.
const MaxKeys = 50

// MaxKeyName is the longest key name, in characters.
const MaxKeyName = 64

// keyUseStep is how often a key's LastUsed moves: not on every request.
const keyUseStep = 5 * time.Minute

// Errors of the keys.
var (
	ErrBadKey      = errors.New("invalid API key")
	ErrKeyExpired  = errors.New("the API key has expired")
	ErrKeyNotFound = errors.New("no such API key")
	ErrKeyName     = fmt.Errorf("a key needs a name of at most %d characters", MaxKeyName)
	ErrKeyTaken    = errors.New("there is a key of this name already")
	ErrKeyExpiry   = errors.New("the expiry must lie in the future")
	ErrManyKeys    = fmt.Errorf("an account has at most %d API keys", MaxKeys)
)

// APIKey is one key, by the hash of its secret.
type APIKey struct {
	ID       string    `json:"id"` // names the key in the pages and on the command line
	Email    string    `json:"email"`
	Name     string    `json:"name"`
	Hash     string    `json:"hash"`             // sha256 of the secret
	Hint     string    `json:"hint"`             // the secret's start and end, to tell keys apart
	Created  time.Time `json:"created"`          //
	Expires  time.Time `json:"expires,omitzero"` // zero: never
	LastUsed time.Time `json:"lastUsed,omitzero"`
	Rotated  time.Time `json:"rotated,omitzero"`
	// OldHash is the secret before the last rotation, which keeps working
	// until OldUntil: the time to replace it where it is used.
	OldHash  string    `json:"oldHash,omitempty"`
	OldUntil time.Time `json:"oldUntil,omitzero"`
}

// KeyInfo is a key without its hashes, for the pages.
type KeyInfo struct {
	ID       string
	Email    string
	Name     string
	Hint     string
	Created  time.Time
	Expires  time.Time // zero: never
	LastUsed time.Time
	Rotated  time.Time
	OldUntil time.Time // the secret before the rotation works until then
}

// Expired reports whether the key no longer works.
func (k KeyInfo) Expired() bool { return !k.Expires.IsZero() && !time.Now().Before(k.Expires) }

// OldValid reports whether the secret before the last rotation still works.
func (k KeyInfo) OldValid() bool { return !k.OldUntil.IsZero() && time.Now().Before(k.OldUntil) }

func (k *APIKey) info() KeyInfo {
	return KeyInfo{ID: k.ID, Email: k.Email, Name: k.Name, Hint: k.Hint, Created: k.Created, Expires: k.Expires,
		LastUsed: k.LastUsed, Rotated: k.Rotated, OldUntil: k.OldUntil}
}

func newKeySecret() (secret, hint string) {
	r := newSecret()
	return KeyPrefix + r, KeyPrefix + r[:4] + "…" + r[len(r)-4:]
}

func newKeyID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// CheckKeyName tidies a key's name and checks it.
func CheckKeyName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > MaxKeyName || strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return "", ErrKeyName
	}
	return name, nil
}

// ParseExpiry reads when a key expires: "never" (or empty), a number of
// days, weeks or years from now ("30d", "12w", "1y"), a duration ("36h"), or
// a date (2027-01-31, the end of that day in loc).
func ParseExpiry(s string, now time.Time, loc *time.Location) (time.Time, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" || s == "never" || s == "0" {
		return time.Time{}, nil
	}
	if d, err := time.ParseInLocation("2006-01-02", s, loc); err == nil {
		end := d.AddDate(0, 0, 1)
		if !end.After(now) {
			return time.Time{}, ErrKeyExpiry
		}
		return end.UTC(), nil
	}
	if n := len(s); n > 1 {
		if v, err := strconv.Atoi(s[:n-1]); err == nil && v > 0 && v <= 100*366 {
			switch s[n-1] {
			case 'd':
				return now.AddDate(0, 0, v).UTC(), nil
			case 'w':
				return now.AddDate(0, 0, 7*v).UTC(), nil
			case 'y':
				if v <= 100 {
					return now.AddDate(v, 0, 0).UTC(), nil
				}
			}
		}
	}
	if d, err := time.ParseDuration(s); err == nil && d > 0 {
		return now.Add(d).UTC(), nil
	}
	return time.Time{}, fmt.Errorf("unknown expiry %q: want never, 30d, 12w, 1y, 36h or a date like 2027-01-31", s)
}

// keyOf finds a key of email by ID; the caller holds mu.
func (s *Store) keyOf(email, id string) *APIKey {
	for _, k := range s.keys {
		if k.ID == id && key(k.Email) == key(email) {
			return k
		}
	}
	return nil
}

// Keys lists the keys of an account, oldest first.
func (s *Store) Keys(email string) []KeyInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refresh()
	out := []KeyInfo{}
	for _, k := range s.keys {
		if key(k.Email) == key(email) {
			out = append(out, k.info())
		}
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Created.Before(out[b].Created) })
	return out
}

// CreateKey makes a key for email, expiring at expires (zero: never). It
// returns the secret, which is not kept and cannot be shown again.
func (s *Store) CreateKey(email, name string, expires time.Time) (string, KeyInfo, error) {
	name, err := CheckKeyName(name)
	if err != nil {
		return "", KeyInfo{}, err
	}
	now := time.Now().UTC()
	if !expires.IsZero() && !expires.After(now) {
		return "", KeyInfo{}, ErrKeyExpiry
	}
	secret, hint := newKeySecret()
	var out KeyInfo
	s.mu.Lock()
	defer s.mu.Unlock()
	err = s.edit(func() (bool, error) {
		u := s.users[key(email)]
		if u == nil {
			return false, ErrNotFound
		}
		n := 0
		for _, k := range s.keys {
			if key(k.Email) != key(email) {
				continue
			}
			n++
			if strings.EqualFold(k.Name, name) {
				return false, ErrKeyTaken
			}
		}
		if n >= MaxKeys {
			return false, ErrManyKeys
		}
		k := &APIKey{ID: newKeyID(), Email: u.Email, Name: name, Hash: hashToken(secret), Hint: hint,
			Created: now, Expires: expires.UTC()}
		s.keys[k.Hash] = k
		out = k.info()
		return true, nil
	})
	if err != nil {
		return "", KeyInfo{}, err
	}
	return secret, out, nil
}

// KeyUpdate changes a key. Nil fields stay as they are; a zero Expires is
// never.
type KeyUpdate struct {
	Name    *string
	Expires *time.Time
}

// UpdateKey renames a key or changes when it expires.
func (s *Store) UpdateKey(email, id string, up KeyUpdate) (KeyInfo, error) {
	var name string
	if up.Name != nil {
		n, err := CheckKeyName(*up.Name)
		if err != nil {
			return KeyInfo{}, err
		}
		name = n
	}
	if up.Expires != nil && !up.Expires.IsZero() && !up.Expires.After(time.Now()) {
		return KeyInfo{}, ErrKeyExpiry
	}
	var out KeyInfo
	s.mu.Lock()
	defer s.mu.Unlock()
	err := s.edit(func() (bool, error) {
		k := s.keyOf(email, id)
		if k == nil {
			return false, ErrKeyNotFound
		}
		if up.Name != nil {
			for _, o := range s.keys {
				if o != k && key(o.Email) == key(email) && strings.EqualFold(o.Name, name) {
					return false, ErrKeyTaken
				}
			}
			k.Name = name
		}
		if up.Expires != nil {
			k.Expires = up.Expires.UTC()
		}
		out = k.info()
		return true, nil
	})
	return out, err
}

// RotateKey gives a key a new secret, which it returns. The old secret keeps
// working for grace (0: not at all), to replace it where it is used.
// expires, when set, is the key's new expiry (zero: never); a key past its
// expiry needs one.
func (s *Store) RotateKey(email, id string, grace time.Duration, expires *time.Time) (string, KeyInfo, error) {
	now := time.Now().UTC()
	if expires != nil && !expires.IsZero() && !expires.After(now) {
		return "", KeyInfo{}, ErrKeyExpiry
	}
	secret, hint := newKeySecret()
	var out KeyInfo
	s.mu.Lock()
	defer s.mu.Unlock()
	err := s.edit(func() (bool, error) {
		k := s.keyOf(email, id)
		if k == nil {
			return false, ErrKeyNotFound
		}
		exp := k.Expires
		if expires != nil {
			exp = expires.UTC()
		}
		if !exp.IsZero() && !exp.After(now) {
			return false, ErrKeyExpiry
		}
		delete(s.keys, k.Hash)
		k.OldHash, k.OldUntil = "", time.Time{}
		if grace > 0 && !k.info().Expired() {
			k.OldHash, k.OldUntil = k.Hash, now.Add(grace)
		}
		k.Hash, k.Hint, k.Rotated, k.Expires = hashToken(secret), hint, now, exp
		s.keys[k.Hash] = k
		out = k.info()
		return true, nil
	})
	if err != nil {
		return "", KeyInfo{}, err
	}
	return secret, out, nil
}

// DeleteKey removes a key: it stops working at once.
func (s *Store) DeleteKey(email, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.edit(func() (bool, error) {
		k := s.keyOf(email, id)
		if k == nil {
			return false, ErrKeyNotFound
		}
		delete(s.keys, k.Hash)
		return true, nil
	})
}

// DeleteKeys removes all keys of an account and returns how many there were.
func (s *Store) DeleteKeys(email string) (int, error) {
	n := 0
	s.mu.Lock()
	defer s.mu.Unlock()
	err := s.edit(func() (bool, error) {
		n = s.dropKeys(email)
		return n > 0, nil
	})
	return n, err
}

// dropKeys removes the keys of email; the caller holds mu.
func (s *Store) dropKeys(email string) int {
	n := 0
	for h, k := range s.keys {
		if key(k.Email) == key(email) {
			delete(s.keys, h)
			n++
		}
	}
	return n
}

// CheckKey returns the account an API key belongs to. Failed attempts count
// against client (an IP) as failed sign-ins do.
func (s *Store) CheckKey(secret, client string) (*User, KeyInfo, error) {
	lk := "key:" + client
	if wait := s.limiter.blocked(lk); wait > 0 {
		return nil, KeyInfo{}, &LimitError{Wait: wait}
	}
	if !strings.HasPrefix(secret, KeyPrefix) || len(secret) > 200 {
		s.limiter.fail(lk)
		return nil, KeyInfo{}, ErrBadKey
	}
	h := hashToken(secret)
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refresh()
	k := s.keys[h]
	if k == nil {
		// the secret before a rotation, while it may still be used
		for _, o := range s.keys {
			if o.OldHash == h && now.Before(o.OldUntil) {
				k = o
				break
			}
		}
	}
	if k == nil {
		s.limiter.fail(lk)
		return nil, KeyInfo{}, ErrBadKey
	}
	if k.info().Expired() {
		return nil, KeyInfo{}, ErrKeyExpired
	}
	u := s.users[key(k.Email)]
	switch {
	case u == nil:
		return nil, KeyInfo{}, ErrBadKey
	case u.Disabled:
		return nil, KeyInfo{}, ErrDisabled
	case u.Pending:
		return nil, KeyInfo{}, ErrPending
	case !u.Active():
		return nil, KeyInfo{}, ErrBadKey
	}
	if now.Sub(k.LastUsed) > keyUseStep {
		k.LastUsed = now.UTC()
		if s.dirtyAt.IsZero() {
			s.dirtyAt = now
		}
	}
	if !s.dirtyAt.IsZero() && now.Sub(s.dirtyAt) > 10*time.Minute {
		s.flush()
	}
	cp := *u
	return &cp, k.info(), nil
}

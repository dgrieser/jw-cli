// Package accounts keeps the accounts of jw serve: users, their sessions and
// the one-time links sent by email (to confirm a registration, to set a
// password after an invitation, to reset a forgotten one), in accounts.json
// in the server's data folder, and the server's own settings next to it
// (settings.go).
//
// The email address is the user name. Admins manage the settings and the
// accounts; users read. Whether people may register, and whether an admin
// has to approve them first, is a setting.
package accounts

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

// Roles.
const (
	RoleAdmin = "admin"
	RoleUser  = "user"
)

// SessionTTL is how long a login lasts without use.
const SessionTTL = 90 * 24 * time.Hour

// MinPassword is the shortest accepted password.
const MinPassword = 8

// Errors the server words for the reader (server/accounts.go).
var (
	ErrBadLogin  = errors.New("wrong email address or password")
	ErrPending   = errors.New("the account is waiting for an admin's approval")
	ErrDisabled  = errors.New("the account is disabled")
	ErrExists    = errors.New("an account with this email address exists already")
	ErrNotFound  = errors.New("no such account")
	ErrLastAdmin = errors.New("the last admin cannot be removed, disabled or demoted")
	ErrBadEmail  = errors.New("not a valid email address")
	ErrBadPass   = fmt.Errorf("the password needs at least %d characters", MinPassword)
	ErrNoPass    = errors.New("the account has no password yet: use the link from the invitation")
	ErrBadToken  = errors.New("the link is invalid or has expired")
	ErrTooMany   = errors.New("too many attempts, try again later")
)

// LimitError is ErrTooMany with the time to wait.
type LimitError struct{ Wait time.Duration }

func (e *LimitError) Error() string { return ErrTooMany.Error() }

// Is makes errors.Is(err, ErrTooMany) hold.
func (e *LimitError) Is(target error) bool { return target == ErrTooMany }

// User is one account.
type User struct {
	Email     string    `json:"email"`
	Hash      string    `json:"hash,omitempty"` // empty: invited, no password yet
	Role      string    `json:"role"`
	Pending   bool      `json:"pending,omitempty"`  // registered, waiting for an admin
	Disabled  bool      `json:"disabled,omitempty"` // locked by an admin
	Lang      string    `json:"lang,omitempty"`     // the language mails to the user are written in
	Created   time.Time `json:"created"`
	LastLogin time.Time `json:"lastLogin,omitzero"`
}

// Active reports whether the account may sign in.
func (u *User) Active() bool { return u != nil && !u.Pending && !u.Disabled && u.Hash != "" }

// IsAdmin reports an active admin.
func (u *User) IsAdmin() bool { return u.Active() && u.Role == RoleAdmin }

// Info is a user without the password hash, for the pages.
type Info struct {
	Email     string
	Role      string
	Pending   bool
	Disabled  bool
	Invited   bool // no password yet
	Created   time.Time
	LastLogin time.Time
	Sessions  int
}

// Token kinds: what a link sent by email does.
const (
	TokenRegister = "register" // confirms the address of a registration; the account is made then
	TokenReset    = "reset"    // sets a new password for an existing account
	TokenInvite   = "invite"   // sets the first password of an account an admin made
)

// Token is a link sent by email, by the hash of its secret.
type Token struct {
	Hash    string    `json:"hash"`
	Kind    string    `json:"kind"`
	Email   string    `json:"email"`
	Lang    string    `json:"lang,omitempty"`
	Created time.Time `json:"created,omitzero"`
	Expires time.Time `json:"expires"`
}

type session struct {
	Hash    string    `json:"hash"` // sha256 of the cookie value
	Email   string    `json:"email"`
	Created time.Time `json:"created"`
	Seen    time.Time `json:"seen"`
}

type file struct {
	Version  int        `json:"version"`
	Users    []*User    `json:"users"`
	Sessions []*session `json:"sessions"`
	Tokens   []*Token   `json:"tokens,omitempty"`
}

// Store holds accounts, sessions and tokens. It is safe for concurrent use
// and picks up changes another process (jw serve user …) makes to the file.
type Store struct {
	path string

	mu       sync.Mutex
	users    map[string]*User // email -> user
	sessions map[string]*session
	tokens   map[string]*Token
	modTime  time.Time
	size     int64
	checked  time.Time
	dirtyAt  time.Time // a session's Seen changed and is not saved yet

	limiter limiter
	// basic remembers credentials of HTTP basic authentication that passed,
	// so the password hash is not recomputed on every request a client
	// makes: key -> the hash the user had then.
	basic sync.Map
}

// Open loads (or starts) the store at path.
func Open(path string) (*Store, error) {
	s := &Store{path: path}
	s.limiter.fails = map[string][]time.Time{}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

// Path is the file.
func (s *Store) Path() string { return s.path }

// NormalizeEmail checks an email address and returns it lower-cased, the
// form accounts are kept by.
func NormalizeEmail(email string) (string, error) {
	email = strings.TrimSpace(email)
	if email == "" || len(email) > 254 || !utf8.ValidString(email) {
		return "", ErrBadEmail
	}
	a, err := mail.ParseAddress(email)
	if err != nil || a.Address != email || a.Name != "" {
		return "", ErrBadEmail
	}
	at := strings.LastIndexByte(email, '@')
	if at < 1 || !strings.Contains(email[at:], ".") || strings.ContainsAny(email, " \t\r\n<>\"") {
		return "", ErrBadEmail
	}
	return strings.ToLower(email), nil
}

func key(email string) string { return strings.ToLower(strings.TrimSpace(email)) }

// load reads the file; the caller holds mu.
func (s *Store) load() error {
	s.users, s.sessions, s.tokens = map[string]*User{}, map[string]*session{}, map[string]*Token{}
	st, err := os.Stat(s.path)
	if errors.Is(err, os.ErrNotExist) {
		s.modTime, s.size = time.Time{}, 0
		return nil
	}
	if err != nil {
		return err
	}
	b, err := os.ReadFile(s.path)
	if err != nil {
		return err
	}
	var f file
	if err := json.Unmarshal(b, &f); err != nil {
		return fmt.Errorf("%s: %w", s.path, err)
	}
	now := time.Now()
	for _, u := range f.Users {
		if u != nil && u.Email != "" {
			s.users[key(u.Email)] = u
		}
	}
	for _, se := range f.Sessions {
		if se != nil && s.users[key(se.Email)] != nil && now.Sub(se.Seen) < SessionTTL {
			s.sessions[se.Hash] = se
		}
	}
	for _, t := range f.Tokens {
		if t != nil && now.Before(t.Expires) {
			s.tokens[t.Hash] = t
		}
	}
	s.modTime, s.size = st.ModTime(), st.Size()
	return nil
}

// refresh reloads the file when another process changed it; the caller
// holds mu.
func (s *Store) refresh() {
	if time.Since(s.checked) < 2*time.Second {
		return
	}
	s.checked = time.Now()
	// on an error keep what we have
	_ = s.sync()
}

// sync reloads the file when it differs from what was read last, keeping the
// session times not saved yet; the caller holds mu.
func (s *Store) sync() error {
	st, err := os.Stat(s.path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		if s.modTime.IsZero() {
			return nil
		}
	case err != nil:
		return err
	case st.ModTime().Equal(s.modTime) && st.Size() == s.size:
		return nil
	}
	seen := map[string]time.Time{}
	for h, se := range s.sessions {
		seen[h] = se.Seen
	}
	if err := s.load(); err != nil {
		return err
	}
	for h, se := range s.sessions {
		if t, ok := seen[h]; ok && t.After(se.Seen) {
			se.Seen = t
		}
	}
	return nil
}

// edit changes the store: under the lock file, so the server and jw serve
// user do not overwrite each other's changes, it rereads the file, runs fn
// and saves when fn reports a change. The caller holds mu.
func (s *Store) edit(fn func() (changed bool, err error)) error {
	unlock, err := lockFile(s.path + ".lock")
	if err != nil {
		return err
	}
	defer unlock()
	if err := s.sync(); err != nil {
		return err
	}
	s.checked = time.Now()
	changed, err := fn()
	if changed {
		if serr := s.save(); serr != nil {
			// what fn changed is not in the file: back to what is (sync
			// just read it)
			_ = s.load()
			return serr
		}
	}
	return err
}

// lockFile takes a lock shared between processes: a file that exists while
// it is held. A lock older than staleLock was left by a process that died.
func lockFile(path string) (func(), error) {
	const staleLock = 30 * time.Second
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_ = f.Close()
			return func() { _ = os.Remove(path) }, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		if st, serr := os.Stat(path); serr == nil && time.Since(st.ModTime()) > staleLock {
			_ = os.Remove(path)
			continue
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("%s is locked: remove it if no jw is running", path)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// save writes the file (0600: it holds password hashes); the caller holds mu
// and, through edit, the lock file.
func (s *Store) save() error {
	f := file{Version: 1}
	for _, u := range s.users {
		f.Users = append(f.Users, u)
	}
	sort.Slice(f.Users, func(a, b int) bool { return f.Users[a].Email < f.Users[b].Email })
	for _, se := range s.sessions {
		f.Sessions = append(f.Sessions, se)
	}
	sort.Slice(f.Sessions, func(a, b int) bool { return f.Sessions[a].Created.Before(f.Sessions[b].Created) })
	now := time.Now()
	for h, t := range s.tokens {
		if now.After(t.Expires) {
			delete(s.tokens, h)
			continue
		}
		f.Tokens = append(f.Tokens, t)
	}
	sort.Slice(f.Tokens, func(a, b int) bool { return f.Tokens[a].Expires.Before(f.Tokens[b].Expires) })
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	if err := saveFile(s.path, b); err != nil {
		return err
	}
	if st, err := os.Stat(s.path); err == nil {
		s.modTime, s.size = st.ModTime(), st.Size()
	}
	s.dirtyAt = time.Time{}
	return nil
}

// saveFile writes the accounts file; tests make it fail.
var saveFile = writeFile

// writeFile replaces path atomically, readable by its owner only.
func writeFile(path string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	_, err = tmp.Write(b)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(tmp.Name(), 0o600)
	}
	if err == nil {
		err = os.Rename(tmp.Name(), path)
	}
	if err != nil {
		_ = os.Remove(tmp.Name())
	}
	return err
}

// --- accounts -------------------------------------------------------------------

// CheckPassword validates a new password.
func CheckPassword(pw string) error {
	if n := utf8.RuneCountInString(pw); n < MinPassword || n > 256 {
		return ErrBadPass
	}
	return nil
}

// HasAdmin reports whether an active admin exists.
func (s *Store) HasAdmin() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refresh()
	return s.activeAdmins() > 0
}

// Pending returns the number of accounts waiting for approval.
func (s *Store) Pending() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refresh()
	n := 0
	for _, u := range s.users {
		if u.Pending {
			n++
		}
	}
	return n
}

// Admins returns the active admins, for mails about new registrations.
func (s *Store) Admins() []User {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refresh()
	var out []User
	for _, u := range s.users {
		if u.IsAdmin() {
			out = append(out, *u)
		}
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Email < out[b].Email })
	return out
}

// List returns all accounts: waiting ones first, then admins, then by address.
func (s *Store) List() []Info {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refresh()
	count := map[string]int{}
	for _, se := range s.sessions {
		count[key(se.Email)]++
	}
	out := make([]Info, 0, len(s.users))
	for k, u := range s.users {
		out = append(out, Info{Email: u.Email, Role: u.Role, Pending: u.Pending, Disabled: u.Disabled, Invited: u.Hash == "",
			Created: u.Created, LastLogin: u.LastLogin, Sessions: count[k]})
	}
	sort.Slice(out, func(a, b int) bool {
		if out[a].Pending != out[b].Pending {
			return out[a].Pending
		}
		if (out[a].Role == RoleAdmin) != (out[b].Role == RoleAdmin) {
			return out[a].Role == RoleAdmin
		}
		return out[a].Email < out[b].Email
	})
	return out
}

// Get returns a copy of one account.
func (s *Store) Get(email string) (User, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refresh()
	u := s.users[key(email)]
	if u == nil {
		return User{}, false
	}
	return *u, true
}

// Create adds an account. An empty password makes an invited account, which
// signs in once its password is set through an invitation link; pending
// accounts need an admin's approval.
func (s *Store) Create(email, password, role string, pending bool, lang string) error {
	email, err := NormalizeEmail(email)
	if err != nil {
		return err
	}
	var hash string
	if password != "" {
		if err := CheckPassword(password); err != nil {
			return err
		}
		if hash, err = HashPassword(password); err != nil {
			return err
		}
	}
	if role != RoleAdmin {
		role = RoleUser
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.edit(func() (bool, error) {
		if s.users[email] != nil {
			return false, ErrExists
		}
		s.users[email] = &User{Email: email, Hash: hash, Role: role, Pending: pending, Lang: lang, Created: time.Now().UTC()}
		return true, nil
	})
}

// Update changes an account. Nil fields stay as they are.
type Update struct {
	Role     *string
	Approve  bool
	Disabled *bool
	Password *string
}

// Apply applies an Update. The last active admin cannot lose that status.
func (s *Store) Apply(email string, up Update) error {
	var hash string
	if up.Password != nil {
		if err := CheckPassword(*up.Password); err != nil {
			return err
		}
		h, err := HashPassword(*up.Password)
		if err != nil {
			return err
		}
		hash = h
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.edit(func() (bool, error) { return s.apply(email, up, hash) })
}

func (s *Store) apply(email string, up Update, hash string) (bool, error) {
	u := s.users[key(email)]
	if u == nil {
		return false, ErrNotFound
	}
	next := *u
	if up.Role != nil {
		next.Role = RoleUser
		if *up.Role == RoleAdmin {
			next.Role = RoleAdmin
		}
	}
	if up.Approve {
		next.Pending = false
	}
	if up.Disabled != nil {
		next.Disabled = *up.Disabled
	}
	if hash != "" {
		next.Hash = hash
	}
	if u.IsAdmin() && !next.IsAdmin() && s.activeAdmins() <= 1 {
		return false, ErrLastAdmin
	}
	*u = next
	if hash != "" || u.Disabled {
		s.dropSessions(u.Email, "")
	}
	if hash != "" {
		// a link sent before must not replace the new password
		s.dropTokens(u.Email, TokenReset)
		s.dropTokens(u.Email, TokenInvite)
	}
	return true, nil
}

// Delete removes an account, its sessions and its links.
func (s *Store) Delete(email string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.edit(func() (bool, error) {
		u := s.users[key(email)]
		if u == nil {
			return false, ErrNotFound
		}
		if u.IsAdmin() && s.activeAdmins() <= 1 {
			return false, ErrLastAdmin
		}
		delete(s.users, key(email))
		s.dropSessions(u.Email, "")
		s.dropTokens(u.Email, "")
		return true, nil
	})
}

func (s *Store) activeAdmins() int {
	n := 0
	for _, u := range s.users {
		if u.IsAdmin() {
			n++
		}
	}
	return n
}

// ChangePassword lets a user replace their own password; the account's other
// sessions end, keep (the cookie value of the current one) stays.
func (s *Store) ChangePassword(email, old, pw, keep string) error {
	if err := CheckPassword(pw); err != nil {
		return err
	}
	s.mu.Lock()
	s.refresh()
	u := s.users[key(email)]
	var cur string
	if u != nil {
		cur = u.Hash
	}
	s.mu.Unlock()
	if u == nil {
		return ErrNotFound
	}
	if !VerifyPassword(cur, old) {
		return ErrBadLogin
	}
	hash, err := HashPassword(pw)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.edit(func() (bool, error) {
		u := s.users[key(email)]
		if u == nil {
			return false, ErrNotFound
		}
		if u.Hash != cur {
			// changed meanwhile (jw serve user passwd): old no longer checked
			return false, ErrBadLogin
		}
		u.Hash = hash
		s.dropSessions(u.Email, hashToken(keep))
		s.dropTokens(u.Email, TokenReset)
		s.dropTokens(u.Email, TokenInvite)
		return true, nil
	})
}

// ClaimAdmin makes an existing account an active admin once its password is
// confirmed. The setup wizard uses it while there is no admin yet, for
// somebody who has an account already.
func (s *Store) ClaimAdmin(email, password string) error {
	s.mu.Lock()
	s.refresh()
	u := s.users[key(email)]
	hash := dummyHash
	if u != nil && u.Hash != "" {
		hash = u.Hash
	}
	s.mu.Unlock()
	if ok := VerifyPassword(hash, password); !ok || u == nil {
		return ErrBadLogin
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.edit(func() (bool, error) {
		u := s.users[key(email)]
		if u == nil || u.Hash != hash {
			return false, ErrBadLogin
		}
		u.Role, u.Pending, u.Disabled = RoleAdmin, false, false
		return true, nil
	})
}

// --- links sent by email ----------------------------------------------------

// maxTokens bounds the links waiting at once, so a flood of registrations
// cannot grow the file without end.
const maxTokens = 500

// NewToken makes a link of kind for email, valid for ttl, and returns its
// secret. Older links of the address stay valid until Supersede.
func (s *Store) NewToken(kind, email, lang string, ttl time.Duration) (string, error) {
	email, err := NormalizeEmail(email)
	if err != nil {
		return "", err
	}
	tok := newSecret()
	s.mu.Lock()
	defer s.mu.Unlock()
	err = s.edit(func() (bool, error) {
		now := time.Now()
		for h, t := range s.tokens {
			if now.After(t.Expires) {
				delete(s.tokens, h)
			}
		}
		if len(s.tokens) >= maxTokens {
			return false, ErrTooMany
		}
		s.tokens[hashToken(tok)] = &Token{Hash: hashToken(tok), Kind: kind, Email: email, Lang: lang,
			Created: now.UTC(), Expires: now.Add(ttl).UTC()}
		return true, nil
	})
	if err != nil {
		return "", err
	}
	return tok, nil
}

// Supersede ends the other links of the kind and address of secret, once the
// new link was delivered.
func (s *Store) Supersede(secret string) {
	h := hashToken(secret)
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = s.edit(func() (bool, error) {
		t := s.tokens[h]
		if t == nil {
			return false, nil
		}
		n := len(s.tokens)
		for o, ot := range s.tokens {
			// only links made before: one made meanwhile may still be on its way
			if o != h && ot.Email == t.Email && ot.Kind == t.Kind && ot.Created.Before(t.Created) {
				delete(s.tokens, o)
			}
		}
		return len(s.tokens) != n, nil
	})
}

// Revoke ends the link of secret (one that could not be delivered).
func (s *Store) Revoke(secret string) {
	h := hashToken(secret)
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = s.edit(func() (bool, error) {
		_, ok := s.tokens[h]
		delete(s.tokens, h)
		return ok, nil
	})
}

// PeekToken returns the link a secret stands for, while it is valid, without
// using it up. kinds are the kinds accepted.
func (s *Store) PeekToken(secret string, kinds ...string) (Token, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refresh()
	return s.token(secret, kinds)
}

func (s *Store) token(secret string, kinds []string) (Token, error) {
	if secret == "" {
		return Token{}, ErrBadToken
	}
	t := s.tokens[hashToken(secret)]
	if t == nil || time.Now().After(t.Expires) {
		return Token{}, ErrBadToken
	}
	if !slices.Contains(kinds, t.Kind) {
		return Token{}, ErrBadToken
	}
	return *t, nil
}

// UseRegistration creates the account a registration link confirms, with
// password; pending accounts wait for an admin. The link is used up.
func (s *Store) UseRegistration(secret, password string, pending bool) (User, error) {
	if err := CheckPassword(password); err != nil {
		return User{}, err
	}
	hash, err := HashPassword(password)
	if err != nil {
		return User{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var u User
	err = s.edit(func() (bool, error) {
		t, err := s.token(secret, []string{TokenRegister})
		if err != nil {
			return false, err
		}
		s.dropTokens(t.Email, TokenRegister)
		if s.users[t.Email] != nil {
			return true, ErrExists
		}
		u = User{Email: t.Email, Hash: hash, Role: RoleUser, Pending: pending, Lang: t.Lang, Created: time.Now().UTC()}
		cp := u
		s.users[t.Email] = &cp
		return true, nil
	})
	return u, err
}

// UsePasswordLink sets the password of the account a reset or invitation
// link names; the account's sessions end and the link is used up.
func (s *Store) UsePasswordLink(secret, password string) (User, error) {
	if err := CheckPassword(password); err != nil {
		return User{}, err
	}
	hash, err := HashPassword(password)
	if err != nil {
		return User{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var out User
	err = s.edit(func() (bool, error) {
		t, err := s.token(secret, []string{TokenReset, TokenInvite})
		if err != nil {
			return false, err
		}
		s.dropTokens(t.Email, TokenReset)
		s.dropTokens(t.Email, TokenInvite)
		u := s.users[t.Email]
		if u == nil {
			return true, ErrBadToken
		}
		u.Hash = hash
		s.dropSessions(u.Email, "")
		out = *u
		return true, nil
	})
	return out, err
}

// dropTokens removes the links of an address, of one kind or ("") all; the
// caller holds mu.
func (s *Store) dropTokens(email, kind string) {
	for h, t := range s.tokens {
		if t.Email == key(email) && (kind == "" || t.Kind == kind) {
			delete(s.tokens, h)
		}
	}
}

// --- sessions -------------------------------------------------------------------

// Login checks email and password and starts a session. It returns the
// cookie value. client identifies the caller for the rate limit (an IP).
func (s *Store) Login(email, password, client string) (string, *User, error) {
	u, err := s.Check(email, password, client)
	if err != nil {
		return "", nil, err
	}
	tok, err := s.StartSession(u.Email)
	if err != nil {
		return "", nil, err
	}
	return tok, u, nil
}

// Check verifies email and password, with the rate limit, and reports why an
// account may not sign in.
func (s *Store) Check(email, password, client string) (*User, error) {
	keys := []string{"ip:" + client, "ipu:" + client + "|" + key(email)}
	if wait := s.limiter.blocked(keys...); wait > 0 {
		return nil, &LimitError{Wait: wait}
	}
	s.mu.Lock()
	s.refresh()
	u := s.users[key(email)]
	hash := dummyHash
	if u != nil && u.Hash != "" {
		hash = u.Hash
	}
	s.mu.Unlock()
	ok := VerifyPassword(hash, password) // also for unknown addresses: same timing
	if !ok || u == nil || hash == dummyHash {
		s.limiter.fail(keys...)
		return nil, ErrBadLogin
	}
	// the account as it is now: an admin may have changed it meanwhile
	s.mu.Lock()
	var cp User
	if cur := s.users[key(email)]; cur != nil {
		cp = *cur
	}
	s.mu.Unlock()
	if cp.Hash != hash {
		s.limiter.fail(keys...)
		return nil, ErrBadLogin
	}
	s.limiter.reset(keys[1])
	if cp.Pending {
		return nil, ErrPending
	}
	if cp.Disabled {
		return nil, ErrDisabled
	}
	return &cp, nil
}

// CheckBasic verifies the credentials of HTTP basic authentication (jw
// --server and other API clients). Credentials that passed are remembered for
// as long as the account's password stays the same, so a client's every
// request does not pay for the password hash.
func (s *Store) CheckBasic(email, password, client string) (*User, error) {
	sum := sha256.Sum256([]byte(key(email) + "\x00" + password))
	k := hex.EncodeToString(sum[:])
	if h, ok := s.basic.Load(k); ok {
		s.mu.Lock()
		s.refresh()
		u := s.users[key(email)]
		var cp User
		good := u.Active() && u.Hash == h.(string)
		if good {
			cp = *u
		}
		s.mu.Unlock()
		if good {
			return &cp, nil
		}
		s.basic.Delete(k)
	}
	u, err := s.Check(email, password, client)
	if err != nil {
		return nil, err
	}
	s.basic.Store(k, u.Hash)
	return u, nil
}

// StartSession signs a user in without a password (right after the setup
// wizard made the admin, or a link set the password).
func (s *Store) StartSession(email string) (string, error) {
	tok := newSecret()
	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	err := s.edit(func() (bool, error) {
		u := s.users[key(email)]
		if !u.Active() {
			return false, ErrNotFound
		}
		u.LastLogin = now
		s.sessions[hashToken(tok)] = &session{Hash: hashToken(tok), Email: u.Email, Created: now, Seen: now}
		return true, nil
	})
	if err != nil {
		return "", err
	}
	return tok, nil
}

// Session returns the account of a cookie value, or nil.
func (s *Store) Session(tok string) *User {
	if tok == "" {
		return nil
	}
	h := hashToken(tok)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refresh()
	se := s.sessions[h]
	if se == nil {
		return nil
	}
	now := time.Now()
	if now.Sub(se.Seen) > SessionTTL {
		_ = s.edit(func() (bool, error) {
			_, ok := s.sessions[h]
			delete(s.sessions, h)
			return ok, nil
		})
		return nil
	}
	u := s.users[key(se.Email)]
	if !u.Active() {
		return nil
	}
	if now.Sub(se.Seen) > time.Hour {
		se.Seen = now.UTC()
		if s.dirtyAt.IsZero() {
			s.dirtyAt = now
		}
	}
	if !s.dirtyAt.IsZero() && now.Sub(s.dirtyAt) > 10*time.Minute {
		s.flush()
	}
	cp := *u
	return &cp
}

// Logout ends one session.
func (s *Store) Logout(tok string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = s.edit(func() (bool, error) {
		_, ok := s.sessions[hashToken(tok)]
		delete(s.sessions, hashToken(tok))
		return ok, nil
	})
}

// Flush writes pending session times.
func (s *Store) Flush() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.flush()
}

// flush saves the session times not saved yet; the caller holds mu.
func (s *Store) flush() {
	if s.dirtyAt.IsZero() {
		return
	}
	_ = s.edit(func() (bool, error) { return true, nil })
}

// dropSessions ends all sessions of a user except keep; the caller holds mu.
func (s *Store) dropSessions(email, keep string) {
	for h, se := range s.sessions {
		if key(se.Email) == key(email) && h != keep {
			delete(s.sessions, h)
		}
	}
}

func newSecret() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func hashToken(tok string) string {
	h := sha256.Sum256([]byte(tok))
	return hex.EncodeToString(h[:])
}

// NewSecret returns a random value for a link or a setup token.
func NewSecret() string { return newSecret() }

// --- passwords ------------------------------------------------------------------

// argon2id parameters (OWASP minimum: 19 MiB, 2 passes). They are stored in
// the hash.
const (
	argonMemory  = 19 * 1024
	argonTime    = 2
	argonThreads = 1
	argonKeyLen  = 32
)

// HashPassword returns an argon2id PHC string.
func HashPassword(pw string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(pw), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

// VerifyPassword checks pw against a PHC hash in constant time.
func VerifyPassword(hash, pw string) bool {
	parts := strings.Split(hash, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	var m, t uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil || m == 0 || t == 0 || p == 0 || m > 1<<20 || t > 16 {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(want) == 0 {
		return false
	}
	got := argon2.IDKey([]byte(pw), salt, t, m, p, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

var dummyHash = func() string {
	h, _ := HashPassword("jw-timing")
	return h
}()

// --- rate limit -----------------------------------------------------------------

// limiter blocks a client (IP), or a client for one address, after 8 failed
// attempts within 15 minutes. Addresses alone are not blocked: that would let
// anybody lock the admin out.
type limiter struct {
	mu    sync.Mutex
	fails map[string][]time.Time
}

const (
	limitWindow = 15 * time.Minute
	limitMax    = 8 // per client and address; a client gets 4x as many overall
)

func (l *limiter) recent(k string, now time.Time) []time.Time {
	var out []time.Time
	for _, t := range l.fails[k] {
		if now.Sub(t) < limitWindow {
			out = append(out, t)
		}
	}
	if len(out) == 0 {
		delete(l.fails, k)
	} else {
		l.fails[k] = out
	}
	return out
}

func (l *limiter) blocked(keys ...string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	var wait time.Duration
	for _, k := range keys {
		n := limitMax
		if strings.HasPrefix(k, "ip:") {
			n = 4 * limitMax
		}
		if r := l.recent(k, now); len(r) >= n {
			wait = max(wait, limitWindow-now.Sub(r[len(r)-n]))
		}
	}
	return wait
}

func (l *limiter) fail(keys ...string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	for _, k := range keys {
		l.fails[k] = append(l.recent(k, now), now)
	}
}

func (l *limiter) reset(keys ...string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, k := range keys {
		delete(l.fails, k)
	}
}

// Throttle counts one attempt of something sent by email (a registration, a
// reset) for a client and reports whether it is over the limit: 5 within
// 15 minutes, so the server cannot be used to flood a mailbox.
func (s *Store) Throttle(what, client string) error {
	k := "mail:" + what + ":" + client
	s.limiter.mu.Lock()
	defer s.limiter.mu.Unlock()
	now := time.Now()
	r := s.limiter.recent(k, now)
	if len(r) >= 5 {
		return &LimitError{Wait: limitWindow - now.Sub(r[len(r)-5])}
	}
	s.limiter.fails[k] = append(r, now)
	return nil
}

package server

import (
	"bufio"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/crypto/bcrypt"
)

// Credentials is the set of users HTTP basic authentication lets in: entries
// from an htpasswd/.htaccess file, a user named on the command line, or both.
// The zero value lets nobody in; a nil or empty one turns authentication off.
type Credentials struct {
	// Realm is shown by the browser's login prompt; an .htaccess AuthName
	// sets it.
	Realm string
	users map[string]string // name -> htpasswd hash, or plainPrefix+password
	// verified remembers headers that already passed, so a bcrypt hash is
	// not recomputed on every request a page makes. Only good credentials
	// land here, so it grows with users, not with attempts.
	verified sync.Map
}

const plainPrefix = "\x00plain:"

// Empty reports whether no user has been added.
func (c *Credentials) Empty() bool { return c == nil || len(c.users) == 0 }

// Add lets in user with the plain password.
func (c *Credentials) Add(user, password string) {
	c.set(user, plainPrefix+password)
}

func (c *Credentials) set(user, hash string) {
	if c.users == nil {
		c.users = map[string]string{}
	}
	c.users[user] = hash
}

// LoadFile reads users from path: an htpasswd file (user:hash lines), or an
// .htaccess file whose AuthUserFile names one (resolved against the
// .htaccess file's directory when relative) and whose AuthName becomes the
// realm. User lines may also stand in the .htaccess file itself. Supported
// hashes are bcrypt ($2y$), Apache MD5 ($apr1$) and SHA-1 ({SHA}) — what
// htpasswd writes; an entry in any other format is an error rather than a
// user who silently can never log in.
func (c *Credentials) LoadFile(path string) error {
	return c.loadFile(path, 0)
}

func (c *Credentials) loadFile(path string, depth int) error {
	if depth > 4 {
		return fmt.Errorf("%s: AuthUserFile nested too deep", path)
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		user, hash, isEntry := strings.Cut(line, ":")
		if isEntry && !strings.ContainsAny(user, " \t") {
			if user == "" {
				return fmt.Errorf("%s:%d: entry without a user name", path, n)
			}
			if !supportedHash(hash) {
				return fmt.Errorf("%s:%d: user %q: unsupported password hash (use bcrypt, e.g. htpasswd -B)", path, n, user)
			}
			c.set(user, hash)
			continue
		}
		// an .htaccess directive; the ones that do not concern who gets in
		// (AuthType, Require, ...) are skipped
		directive, arg, _ := strings.Cut(line, " ")
		arg = unquote(strings.TrimSpace(arg))
		switch strings.ToLower(directive) {
		case "authname":
			c.Realm = arg
		case "authuserfile":
			if arg == "" {
				return fmt.Errorf("%s:%d: AuthUserFile without a path", path, n)
			}
			if !filepath.IsAbs(arg) {
				arg = filepath.Join(filepath.Dir(path), arg)
			}
			if err := c.loadFile(arg, depth+1); err != nil {
				return err
			}
		}
	}
	return sc.Err()
}

func unquote(s string) string {
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') && s[len(s)-1] == s[0] {
		return s[1 : len(s)-1]
	}
	return s
}

func supportedHash(hash string) bool {
	switch {
	case strings.HasPrefix(hash, "$2a$"), strings.HasPrefix(hash, "$2b$"), strings.HasPrefix(hash, "$2y$"):
		_, err := bcrypt.Cost([]byte(hash))
		return err == nil
	case strings.HasPrefix(hash, "$apr1$"):
		return strings.Count(hash, "$") == 3
	case strings.HasPrefix(hash, "{SHA}"):
		b, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(hash, "{SHA}"))
		return err == nil && len(b) == sha1.Size
	}
	return false
}

// Check reports whether user may log in with password.
func (c *Credentials) Check(user, password string) bool {
	if c.Empty() {
		return false
	}
	hash, ok := c.users[user]
	if !ok {
		return false
	}
	switch {
	case strings.HasPrefix(hash, plainPrefix):
		want := sha256.Sum256([]byte(strings.TrimPrefix(hash, plainPrefix)))
		got := sha256.Sum256([]byte(password))
		return subtle.ConstantTimeCompare(want[:], got[:]) == 1
	case strings.HasPrefix(hash, "$2"):
		return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
	case strings.HasPrefix(hash, "$apr1$"):
		salt := strings.SplitN(strings.TrimPrefix(hash, "$apr1$"), "$", 2)[0]
		return subtle.ConstantTimeCompare([]byte(apr1(password, salt)), []byte(hash)) == 1
	case strings.HasPrefix(hash, "{SHA}"):
		sum := sha1.Sum([]byte(password))
		got := "{SHA}" + base64.StdEncoding.EncodeToString(sum[:])
		return subtle.ConstantTimeCompare([]byte(got), []byte(hash)) == 1
	}
	return false
}

// RequireAuth wraps next in HTTP basic authentication against c; an empty c
// lets every request through.
func (c *Credentials) RequireAuth(next http.Handler) http.Handler {
	if c.Empty() {
		return next
	}
	realm := c.Realm
	if realm == "" {
		realm = "jw"
	}
	challenge := `Basic realm=` + strconv.Quote(realm) + `, charset="UTF-8"`
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c.allowed(r) {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("WWW-Authenticate", challenge)
		http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
	})
}

func (c *Credentials) allowed(r *http.Request) bool {
	user, password, ok := r.BasicAuth()
	if !ok {
		return false
	}
	key := sha256.Sum256([]byte(r.Header.Get("Authorization")))
	if _, hit := c.verified.Load(key); hit {
		return true
	}
	if !c.Check(user, password) {
		return false
	}
	c.verified.Store(key, struct{}{})
	return true
}

// apr1 is Apache's MD5-based crypt ($apr1$salt$hash), what htpasswd -m
// writes and older htpasswd versions write by default.
func apr1(password, salt string) string {
	const magic = "$apr1$"
	if len(salt) > 8 {
		salt = salt[:8]
	}
	pw := []byte(password)

	alt := md5.Sum([]byte(password + salt + password))
	ctx := md5.New()
	ctx.Write([]byte(password + magic + salt))
	for i := len(pw); i > 0; i -= 16 {
		ctx.Write(alt[:min(i, 16)])
	}
	for i := len(pw); i > 0; i >>= 1 {
		if i&1 == 1 {
			ctx.Write([]byte{0})
		} else {
			ctx.Write(pw[:1])
		}
	}
	sum := ctx.Sum(nil)

	for i := 0; i < 1000; i++ {
		h := md5.New()
		if i&1 == 1 {
			h.Write(pw)
		} else {
			h.Write(sum)
		}
		if i%3 != 0 {
			h.Write([]byte(salt))
		}
		if i%7 != 0 {
			h.Write(pw)
		}
		if i&1 == 1 {
			h.Write(sum)
		} else {
			h.Write(pw)
		}
		sum = h.Sum(nil)
	}

	const itoa64 = "./0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	var out strings.Builder
	to64 := func(v uint32, n int) {
		for ; n > 0; n-- {
			out.WriteByte(itoa64[v&0x3f])
			v >>= 6
		}
	}
	to64(uint32(sum[0])<<16|uint32(sum[6])<<8|uint32(sum[12]), 4)
	to64(uint32(sum[1])<<16|uint32(sum[7])<<8|uint32(sum[13]), 4)
	to64(uint32(sum[2])<<16|uint32(sum[8])<<8|uint32(sum[14]), 4)
	to64(uint32(sum[3])<<16|uint32(sum[9])<<8|uint32(sum[15]), 4)
	to64(uint32(sum[4])<<16|uint32(sum[10])<<8|uint32(sum[5]), 4)
	to64(uint32(sum[11]), 2)
	return magic + salt + "$" + out.String()
}

package httpx

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"
)

// Cache is a small JSON-blob disk cache under the user cache dir, used for
// slow-changing data: language lists, wol library config, bible book tables.
type Cache struct {
	dir string
	// pruned makes sure one process sweeps out old response bodies only once
	pruned sync.Once
}

// OpenCache returns a cache rooted at <UserCacheDir>/jw. A missing cache dir
// degrades to a no-op cache rather than an error.
func OpenCache() *Cache {
	base, err := os.UserCacheDir()
	if err != nil {
		return &Cache{}
	}
	dir := filepath.Join(base, "jw")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return &Cache{}
	}
	return &Cache{dir: dir}
}

// OpenCacheAt returns a cache rooted at dir (for tests).
func OpenCacheAt(dir string) *Cache {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return &Cache{}
	}
	return &Cache{dir: dir}
}

var unsafeKey = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func (c *Cache) path(key string) string {
	return filepath.Join(c.dir, unsafeKey.ReplaceAllString(key, "_")+".json")
}

// Get loads key into out if the entry exists and is younger than maxAge.
func (c *Cache) Get(key string, maxAge time.Duration, out any) bool {
	if c == nil || c.dir == "" {
		return false
	}
	p := c.path(key)
	st, err := os.Stat(p)
	if err != nil || time.Since(st.ModTime()) > maxAge {
		return false
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return false
	}
	return json.Unmarshal(b, out) == nil
}

// Put stores v under key; failures are silently ignored (cache is best-effort).
func (c *Cache) Put(key string, v any) {
	if c == nil || c.dir == "" {
		return
	}
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	_ = os.WriteFile(c.path(key), b, 0o644)
}

// Dir exposes the cache directory ("" when the cache is inactive).
func (c *Cache) Dir() string {
	if c == nil {
		return ""
	}
	return c.dir
}

// responseDir holds the raw response bodies the HTTP client keeps, one file
// per request, named by a hash of it: URLs are too long and too varied to be
// file names.
const responseDir = "http"

// responseMaxAge is when a response body is old enough to be swept out, well
// past any TTL it is read with, so the directory does not grow for ever.
const responseMaxAge = 30 * 24 * time.Hour

func (c *Cache) bytesPath(key string) string {
	sum := sha256.Sum256([]byte(key))
	return filepath.Join(c.dir, responseDir, hex.EncodeToString(sum[:]))
}

// GetBytes loads the raw body stored under key if it is younger than maxAge.
func (c *Cache) GetBytes(key string, maxAge time.Duration) ([]byte, bool) {
	if c == nil || c.dir == "" {
		return nil, false
	}
	p := c.bytesPath(key)
	st, err := os.Stat(p)
	if err != nil || time.Since(st.ModTime()) > maxAge {
		return nil, false
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, false
	}
	return b, true
}

// PutBytes stores a raw body under key. The file is written aside and renamed
// into place, so a reader running alongside — jw serve answers many requests
// at once, a second command may share the directory — never sees half of it.
// Failures are ignored, as for Put.
func (c *Cache) PutBytes(key string, b []byte) {
	if c == nil || c.dir == "" {
		return
	}
	dir := filepath.Join(c.dir, responseDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	c.pruned.Do(func() { go prune(dir, responseMaxAge) })
	f, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return
	}
	_, werr := f.Write(b)
	cerr := f.Close()
	if werr != nil || cerr != nil || os.Rename(f.Name(), c.bytesPath(key)) != nil {
		_ = os.Remove(f.Name())
	}
}

// prune removes the files of dir older than maxAge.
func prune(dir string, maxAge time.Duration) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if info, err := e.Info(); err == nil && time.Since(info.ModTime()) > maxAge {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}

package httpx

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

// Cache is a small JSON-blob disk cache under the user cache dir, used for
// slow-changing data: language lists, wol library config, bible book tables.
type Cache struct {
	dir string
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

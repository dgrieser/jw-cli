package httpx

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/singleflight"
)

// Cache is the disk cache under the user cache dir. It holds two kinds of
// entry, both kept indefinitely and bounded only by size:
//
//   - response bodies (http/): what jw.org, wol.jw.org and the CDN answered,
//     with the validators needed to ask cheaply, by a HEAD request, whether the
//     answer still holds once it is older than the freshness window;
//   - memos (memo/): results derived from response bodies — a parsed book
//     grid, the language list — each recording the bodies it was built from,
//     so it is rebuilt exactly when one of them changed and never otherwise.
//
// The least recently used entries are dropped once the cache grows past its
// size limit.
type Cache struct {
	dir   string
	fresh time.Duration
	max   int64

	// client revalidates stale bodies on behalf of memos; set by the client
	// that uses this cache for its responses
	client atomic.Pointer[Client]

	memoFlight singleflight.Group

	// size accounting: size is the bytes on disk as of the last sweep plus what
	// this process wrote since; scanned is false until a sweep has run
	mu       sync.Mutex
	size     int64
	measured time.Time
	scanned  bool
	written  int64
	sweeping bool
	sweeps   sync.WaitGroup
}

// CacheOptions tunes a cache. Zero values take the defaults.
type CacheOptions struct {
	// Fresh is how long an entry is used as is before it is checked against
	// upstream again. Negative checks on every use.
	Fresh time.Duration
	// MaxBytes bounds the cache; the least recently used entries are dropped
	// beyond it. Negative turns the cache off.
	MaxBytes int64
}

const (
	// DefaultFresh is how long what upstream answered is used before it is
	// checked again: the library's pages change rarely, and a day is what a
	// reader asks the same thing again within.
	DefaultFresh = 24 * time.Hour
	// DefaultMaxBytes bounds the cache at 4 GiB: room for every page an
	// unfolded study walks through, which is what lets a page come back
	// without asking jw.org again.
	DefaultMaxBytes = 4 << 30
)

// OpenCache returns a cache rooted at <UserCacheDir>/jw with default options.
// A missing cache dir degrades to a no-op cache rather than an error.
func OpenCache() *Cache { return Open("", CacheOptions{}) }

// OpenCacheAt returns a cache rooted at dir with default options (for tests).
func OpenCacheAt(dir string) *Cache { return Open(dir, CacheOptions{}) }

// Open returns a cache rooted at dir, <UserCacheDir>/jw when dir is empty.
func Open(dir string, o CacheOptions) *Cache {
	c := &Cache{fresh: o.Fresh, max: o.MaxBytes}
	switch {
	case c.fresh == 0:
		c.fresh = DefaultFresh
	case c.fresh < 0:
		c.fresh = 0
	}
	if c.max == 0 {
		c.max = DefaultMaxBytes
	}
	if c.max < 0 {
		return c
	}
	if dir == "" {
		base, err := os.UserCacheDir()
		if err != nil {
			return c
		}
		dir = filepath.Join(base, "jw")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return c
	}
	migrate(dir)
	for _, sub := range []string{responseDir, memoDir} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			return c
		}
	}
	c.dir = dir
	// a cache without a sweep marker is new (or just migrated): there is
	// nothing to sweep, and what this process writes is all there is
	marker := filepath.Join(dir, sweepMarker)
	if _, err := os.Stat(marker); errors.Is(err, fs.ErrNotExist) {
		if os.WriteFile(marker, []byte("0"), 0o644) == nil {
			c.scanned, c.measured = true, time.Now()
		}
	}
	return c
}

// Dir exposes the cache directory ("" when the cache is inactive).
func (c *Cache) Dir() string {
	if c == nil {
		return ""
	}
	return c.dir
}

func (c *Cache) active() bool { return c != nil && c.dir != "" }

// Fresh is how long an entry is used before it is checked again.
func (c *Cache) Fresh() time.Duration {
	if c == nil {
		return DefaultFresh
	}
	return c.fresh
}

// Close waits for a sweep under way, so a command that exits right after
// writing does not leave the cache over its limit.
func (c *Cache) Close() {
	if c != nil {
		c.sweeps.Wait()
	}
}

const (
	responseDir = "http"
	memoDir     = "memo"
)

// migrate removes what older versions kept at the top of the cache dir: blobs
// that memos and response bodies replace, never to be read again.
func migrate(dir string) {
	if _, err := os.Stat(filepath.Join(dir, memoDir)); err == nil {
		return
	}
	legacy := []string{"woldoc1-", "gallery1-", "wolfc1-", "library2-", "binav-", "books2-", "bibles-", "wolcfg2-", "languages"}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".json") {
			continue
		}
		for _, p := range legacy {
			if strings.HasPrefix(name, p) {
				_ = os.Remove(filepath.Join(dir, name))
				break
			}
		}
	}
	// response bodies of the old layout carry no validators
	old, _ := os.ReadDir(filepath.Join(dir, responseDir))
	for _, e := range old {
		_ = os.Remove(filepath.Join(dir, responseDir, e.Name()))
	}
}

// --- response bodies ---------------------------------------------------------

// entry describes a kept response body: where it came from, the request
// headers it was asked with (a revalidation must ask the same question), and
// what upstream said about it.
type entry struct {
	URL     string            `json:"u"`
	Header  map[string]string `json:"h,omitempty"`
	ETag    string            `json:"e,omitempty"` // of the identity encoding, as a HEAD reports it
	LastMod string            `json:"m,omitempty"`
	Size    int64             `json:"s"`
	Hash    string            `json:"x"`
	Checked int64             `json:"c"` // unix seconds the body was last known current
	// NoHead is set for a URL whose server answers HEAD with an error but GET
	// with the body (the CDN's mediator API): a HEAD would be wasted on it
	NoHead bool `json:"n,omitempty"`
	// the edition the body is from, when the site says (see Editions);
	// EdSeen tells a body that says none from one not yet read for it
	Scope  string `json:"es,omitempty"`
	Ed     int    `json:"ed,omitempty"`
	EdSeen bool   `json:"ek,omitempty"`
}

// entryMagic opens every body file: the header line, then the body.
const entryMagic = "jwc1 "

func entryID(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

func (c *Cache) entryPath(id string) string {
	return filepath.Join(c.dir, responseDir, id)
}

func bodyHash(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:16])
}

func (e *entry) fresh(window time.Duration) bool {
	return time.Since(time.Unix(e.Checked, 0)) < window
}

// loadEntry reads a body and its header. With headerOnly the body is not read,
// which is all a memo needs to see whether its sources moved.
func (c *Cache) loadEntry(id string, headerOnly bool) (entry, []byte, bool) {
	var e entry
	if !c.active() {
		return e, nil, false
	}
	f, err := os.Open(c.entryPath(id))
	if err != nil {
		return e, nil, false
	}
	defer f.Close()
	r := bufio.NewReader(f)
	line, err := r.ReadSlice('\n')
	if err != nil || !bytes.HasPrefix(line, []byte(entryMagic)) {
		return e, nil, false
	}
	if json.Unmarshal(line[len(entryMagic):], &e) != nil || e.Hash == "" {
		return e, nil, false
	}
	if headerOnly {
		return e, nil, true
	}
	body, err := io.ReadAll(r)
	if err != nil || int64(len(body)) != e.Size {
		return e, nil, false
	}
	c.touch(c.entryPath(id))
	return e, body, true
}

// saveEntry writes a body aside and renames it into place, so a reader running
// alongside — jw serve answers many requests at once, a second command may
// share the directory — never sees half of it. Failures are ignored: the
// cache is best effort.
func (c *Cache) saveEntry(id string, e entry, body []byte) {
	if !c.active() {
		return
	}
	e.Size, e.Hash = int64(len(body)), bodyHash(body)
	head, err := json.Marshal(e)
	if err != nil {
		return
	}
	var buf bytes.Buffer
	buf.Grow(len(entryMagic) + len(head) + 1 + len(body))
	buf.WriteString(entryMagic)
	buf.Write(head)
	buf.WriteByte('\n')
	buf.Write(body)
	c.writeFile(c.entryPath(id), buf.Bytes())
}

func (c *Cache) removeEntry(id string) {
	if c.active() {
		_ = os.Remove(c.entryPath(id))
	}
}

// --- memos and plain values -----------------------------------------------

// memoRecord is a derived value and the bodies it was derived from, by entry
// id and body hash.
type memoRecord struct {
	Deps  map[string]string `json:"deps,omitempty"`
	At    int64             `json:"at"`
	Value json.RawMessage   `json:"v"`
}

func (c *Cache) memoPath(key string) string {
	return filepath.Join(c.dir, memoDir, entryID(key))
}

func (c *Cache) loadMemo(key string) (memoRecord, bool) {
	var m memoRecord
	if !c.active() {
		return m, false
	}
	p := c.memoPath(key)
	b, err := os.ReadFile(p)
	if err != nil || json.Unmarshal(b, &m) != nil || len(m.Value) == 0 {
		return m, false
	}
	c.touch(p)
	return m, true
}

func (c *Cache) saveMemo(key string, m memoRecord) {
	if !c.active() {
		return
	}
	b, err := json.Marshal(m)
	if err != nil {
		return
	}
	c.writeFile(c.memoPath(key), b)
}

func (c *Cache) removeMemo(key string) {
	if c.active() {
		_ = os.Remove(c.memoPath(key))
	}
}

// Get loads the value last stored under key, however old: for what is not
// read from upstream but learnt along the way, like the search categories a
// language offers. Use Memo for what is derived from upstream reads.
func (c *Cache) Get(key string, out any) bool {
	m, ok := c.loadMemo(key)
	return ok && json.Unmarshal(m.Value, out) == nil
}

// Put stores v under key until it is replaced or evicted.
func (c *Cache) Put(key string, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	c.saveMemo(key, memoRecord{At: time.Now().Unix(), Value: b})
}

// --- files and size --------------------------------------------------------

// touchEvery throttles the last-use stamps eviction goes by: an entry read
// a hundred times an hour needs one stamp, not a hundred.
const touchEvery = time.Hour

func (c *Cache) touch(p string) {
	st, err := os.Stat(p)
	if err == nil && time.Since(st.ModTime()) > touchEvery {
		now := time.Now()
		_ = os.Chtimes(p, now, now)
	}
}

func (c *Cache) writeFile(p string, b []byte) {
	dir := filepath.Dir(p)
	f, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return
	}
	_, werr := f.Write(b)
	cerr := f.Close()
	if werr != nil || cerr != nil || os.Rename(f.Name(), p) != nil {
		_ = os.Remove(f.Name())
		return
	}
	c.wrote(int64(len(b)))
}

// sweepMarker records when a sweep last measured the cache and the size it
// found, so commands run back to back do not each walk the whole directory.
const (
	sweepMarker = ".swept"
	sweepEvery  = time.Hour
)

// wrote accounts for n bytes written and starts a sweep when the cache may be
// past its limit, or when the size it goes by is over an hour old — other
// processes write to the same directory.
func (c *Cache) wrote(n int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.written += n
	if c.sweeping {
		return
	}
	if !c.scanned {
		// the size the last sweep of any process found, if it is recent
		if b, err := os.ReadFile(filepath.Join(c.dir, sweepMarker)); err == nil {
			st, _ := os.Stat(filepath.Join(c.dir, sweepMarker))
			if size, err := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64); err == nil && st != nil {
				c.size, c.measured, c.scanned = size, st.ModTime(), true
			}
		}
	}
	if !c.scanned || c.size+c.written > c.max || time.Since(c.measured) > sweepEvery {
		c.startSweep()
	}
}

// startSweep runs a sweep in the background; c.mu must be held. What is
// written while it runs is counted from its start.
func (c *Cache) startSweep() {
	c.sweeping = true
	c.written = 0
	c.sweeps.Add(1)
	go c.sweep()
}

type cacheFile struct {
	path string
	size int64
	used time.Time
}

// sweep measures the cache and, when it is over the limit, drops the least
// recently used entries until it is back under nine tenths of it — the slack
// keeps the next sweep from coming right after.
func (c *Cache) sweep() {
	defer c.sweeps.Done()
	start := time.Now()
	var files []cacheFile
	var total int64
	for _, sub := range []string{responseDir, memoDir} {
		_ = filepath.WalkDir(filepath.Join(c.dir, sub), func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return nil
			}
			// a temp file older than a minute was left by a crashed writer
			if strings.HasPrefix(d.Name(), ".tmp-") {
				if time.Since(info.ModTime()) > time.Minute {
					_ = os.Remove(p)
				}
				return nil
			}
			if info.ModTime().After(start) {
				return nil // written since the sweep began: counted as written
			}
			files = append(files, cacheFile{p, info.Size(), info.ModTime()})
			total += info.Size()
			return nil
		})
	}
	if total > c.max {
		slices.SortFunc(files, func(a, b cacheFile) int { return a.used.Compare(b.used) })
		target := c.max / 10 * 9
		for _, f := range files {
			if total <= target {
				break
			}
			if err := os.Remove(f.path); err == nil || errors.Is(err, fs.ErrNotExist) {
				total -= f.size
			}
		}
	}
	_ = os.WriteFile(filepath.Join(c.dir, sweepMarker), []byte(strconv.FormatInt(total, 10)), 0o644)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.size, c.measured, c.scanned, c.sweeping = total, start, true, false
	// what was written meanwhile may have taken the cache over again
	if c.size+c.written > c.max {
		c.startSweep()
	}
}

// ParseSize reads a byte count such as "1GB", "512M", "750MiB" or "1048576".
// Units are powers of 1024; "0" and "off" turn the cache off.
func ParseSize(s string) (int64, error) {
	t := strings.ToUpper(strings.TrimSpace(s))
	if t == "OFF" || t == "0" {
		return -1, nil
	}
	t = strings.TrimSuffix(strings.TrimSuffix(t, "B"), "I")
	mult := int64(1)
	if n := len(t); n > 0 {
		switch t[n-1] {
		case 'K':
			mult = 1 << 10
		case 'M':
			mult = 1 << 20
		case 'G':
			mult = 1 << 30
		case 'T':
			mult = 1 << 40
		}
		if mult > 1 {
			t = t[:n-1]
		}
	}
	v, perr := strconv.ParseFloat(strings.TrimSpace(t), 64)
	if perr != nil || v < 0 {
		return 0, fmt.Errorf("invalid size %q (want e.g. 1GB, 500MB, or 0 to turn the cache off)", s)
	}
	n := int64(v * float64(mult))
	if n == 0 {
		return -1, nil
	}
	return n, nil
}

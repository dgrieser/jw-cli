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

	// hot keeps what was read from the files lately in memory
	hot *hotCache

	// size accounting: size is the bytes on disk as of the last sweep, by
	// this process or another, with what other processes recorded since in
	// the sweep marker; written is what this process added since, unrecorded
	// the part of that the marker does not hold yet; scanned is false until
	// size is known
	mu         sync.Mutex
	size       int64
	measured   time.Time
	scanned    bool
	written    int64
	unrecorded int64
	sweeping   bool
	sweeps     sync.WaitGroup
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
	c.hot = newHot(hotMax)
	// a cache without a sweep marker is new (or just migrated): there is
	// nothing to sweep, and what this process writes is all there is
	if _, err := os.Stat(c.markerPath()); errors.Is(err, fs.ErrNotExist) {
		now := time.Now()
		if c.writeMarker(0, now) {
			c.scanned, c.measured = true, now
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
// writing does not leave the cache over its limit, and records what this
// process wrote for the next one to go by.
func (c *Cache) Close() {
	if !c.active() {
		return
	}
	c.sweeps.Wait()
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.sweeping {
		c.record()
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
	// Retry is when upstream may be asked about the body again, in unix
	// seconds, after it could not be lately (see backOff)
	Retry int64 `json:"r,omitempty"`
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

// nonceAttr opens the one part of a page that every render fills anew:
// wol.jw.org gives each response a CSP nonce of its own, on its script and
// style tags, so two renders of an unchanged page differ in it and in nothing
// else.
var nonceAttr = []byte(`nonce="`)

// maxNonce bounds what is taken for a nonce's value, so a stray quote cannot
// leave a stretch of real content out of the hash.
const maxNonce = 128

// bodyHash identifies a body by what it says: the values of nonce attributes
// are left out, or a page rendered again would count as changed — and every
// result derived from it would be rebuilt — however little it moved. A body
// without one hashes as it always did.
func bodyHash(b []byte) string {
	h := sha256.New()
	for off := 0; ; {
		i := bytes.Index(b[off:], nonceAttr)
		if i < 0 {
			break
		}
		start := off + i
		end := start + len(nonceAttr)
		off = end
		// an attribute, not the word in a text
		if start == 0 || !isSpace(b[start-1]) {
			continue
		}
		j := bytes.IndexByte(b[end:], '"')
		if j < 0 || j > maxNonce {
			continue
		}
		h.Write(b[:end])
		b, off = b[end+j:], 0
	}
	h.Write(b)
	return hex.EncodeToString(h.Sum(nil)[:16])
}

func isSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' }

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
	p := c.entryPath(id)
	if headerOnly {
		if st, err := os.Stat(p); err == nil {
			if v, ok := c.hot.get(p, st); ok {
				return v.(entry), nil, true
			}
		}
	}
	f, err := os.Open(p)
	if err != nil {
		return e, nil, false
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return e, nil, false
	}
	r := bufio.NewReader(f)
	line, err := r.ReadSlice('\n')
	if err != nil || !bytes.HasPrefix(line, []byte(entryMagic)) {
		return e, nil, false
	}
	if json.Unmarshal(line[len(entryMagic):], &e) != nil || e.Hash == "" {
		return e, nil, false
	}
	if int64(len(line))+e.Size != st.Size() {
		return e, nil, false // cut short
	}
	c.hot.put(p, st, e, int64(len(line))+hotItemCost)
	if headerOnly {
		return e, nil, true
	}
	body := make([]byte, e.Size)
	if _, err := io.ReadFull(r, body); err != nil {
		return e, nil, false
	}
	c.touch(p, st)
	return e, body, true
}

// hotItemCost is what keeping an item in memory costs beyond its data.
const hotItemCost = 256

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
		c.removeFile(c.entryPath(id))
	}
}

// --- memos and plain values -----------------------------------------------

// memoRecord is a derived value and the bodies it was derived from, by entry
// id and body hash.
type memoRecord struct {
	Deps  map[string]string `json:"deps,omitempty"`
	At    int64             `json:"at"`
	Value json.RawMessage   `json:"v"`
	// Retry is when a rebuild that failed may be tried again, in unix
	// seconds; until then the value stands as it is
	Retry int64 `json:"r,omitempty"`
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
	st, err := os.Stat(p)
	if err != nil {
		return m, false
	}
	if v, ok := c.hot.get(p, st); ok {
		m = v.(memoRecord)
	} else {
		b, err := os.ReadFile(p)
		if err != nil || json.Unmarshal(b, &m) != nil || len(m.Value) == 0 {
			return m, false
		}
		// the file read may be newer than st, and is then read again next
		// time rather than taken for what st describes
		if int64(len(b)) == st.Size() {
			c.hot.put(p, st, m, int64(len(b))+hotItemCost)
		}
	}
	c.touch(p, st)
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
		c.removeFile(c.memoPath(key))
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

func (c *Cache) touch(p string, st os.FileInfo) {
	if time.Since(st.ModTime()) > touchEvery {
		now := time.Now()
		if os.Chtimes(p, now, now) == nil {
			if nst, err := os.Stat(p); err == nil {
				c.hot.restamp(p, nst.ModTime())
			}
		}
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
	// what the cache grows by: a file written over replaces the old one
	n := int64(len(b))
	if st, err := os.Stat(p); err == nil {
		n -= st.Size()
	}
	c.hot.drop(p)
	if werr != nil || cerr != nil || os.Rename(f.Name(), p) != nil {
		_ = os.Remove(f.Name())
		return
	}
	c.wrote(n)
}

func (c *Cache) removeFile(p string) {
	c.hot.drop(p)
	if st, err := os.Stat(p); err == nil && os.Remove(p) == nil {
		c.wrote(-st.Size())
	}
}

// The sweep marker records what the last sweep measured — the size of the
// cache, and when — and after it, a line each, what processes added since,
// so commands run back to back neither walk the whole directory each nor
// lose sight of what the others wrote. Its first line reads "jws2 <size>
// <unix seconds>", every further one "+<bytes>" (or "-"). Adding a line
// does not move the time a sweep is due by, as the time is in the content.
const (
	sweepMarker = ".swept"
	markerMagic = "jws2"
	sweepEvery  = time.Hour
)

func (c *Cache) markerPath() string { return filepath.Join(c.dir, sweepMarker) }

// readMarker returns the size the marker gives, and when it was measured.
// The marker of older versions, a bare size whose time was that of the file,
// is not read: the cache is measured anew.
func (c *Cache) readMarker() (int64, time.Time, bool) {
	b, err := os.ReadFile(c.markerPath())
	if err != nil {
		return 0, time.Time{}, false
	}
	lines := strings.Split(string(b), "\n")
	f := strings.Fields(lines[0])
	if len(f) != 3 || f[0] != markerMagic {
		return 0, time.Time{}, false
	}
	size, err := strconv.ParseInt(f[1], 10, 64)
	if err != nil {
		return 0, time.Time{}, false
	}
	at, err := strconv.ParseInt(f[2], 10, 64)
	if err != nil {
		return 0, time.Time{}, false
	}
	for _, l := range lines[1:] {
		if n, err := strconv.ParseInt(strings.TrimSpace(l), 10, 64); err == nil {
			size += n
		}
	}
	return max(size, 0), time.Unix(at, 0), true
}

// writeMarker records a measurement, in place of whatever the marker held.
func (c *Cache) writeMarker(size int64, at time.Time) bool {
	f, err := os.CreateTemp(c.dir, ".tmp-*")
	if err != nil {
		return false
	}
	_, werr := fmt.Fprintf(f, "%s %d %d\n", markerMagic, size, at.Unix())
	cerr := f.Close()
	if werr != nil || cerr != nil || os.Rename(f.Name(), c.markerPath()) != nil {
		_ = os.Remove(f.Name())
		return false
	}
	return true
}

// record adds what this process wrote and the marker does not hold yet to
// it; c.mu must be held. An append of a line is whole even with another
// process appending alongside.
func (c *Cache) record() {
	if c.unrecorded == 0 {
		return
	}
	f, err := os.OpenFile(c.markerPath(), os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		return
	}
	_, werr := fmt.Fprintf(f, "%+d\n", c.unrecorded)
	if f.Close() == nil && werr == nil {
		c.unrecorded = 0
	}
}

// wrote accounts for n bytes added (or, negative, removed) and starts a
// sweep when the cache may be past its limit, or when the size it goes by
// was measured over an hour ago. Other processes learn of it from the
// marker: this process records there what it wrote when it is done, and
// whenever that comes to a hundredth of the limit.
func (c *Cache) wrote(n int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.written += n
	c.unrecorded += n
	if c.sweeping {
		return
	}
	if !c.scanned {
		// the size the last sweep of any process found, with what the
		// others recorded since
		if size, at, ok := c.readMarker(); ok {
			c.size, c.measured, c.scanned = size, at, true
		}
	}
	if !c.scanned || c.size+c.written > c.max || time.Since(c.measured) > sweepEvery {
		c.startSweep()
		return
	}
	if abs(c.unrecorded) >= max(c.max/100, 1) {
		c.record()
	}
}

func abs(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}

// startSweep runs a sweep in the background; c.mu must be held. What is on
// disk now is the sweep's to measure; what is written while it runs is
// counted from its start.
func (c *Cache) startSweep() {
	c.sweeping = true
	c.written, c.unrecorded = 0, 0
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
	before, beforeAt, known := c.readMarker()
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
	// files written since the sweep began were left out of the walk: what
	// other processes recorded meanwhile stands for theirs
	if after, afterAt, ok := c.readMarker(); ok && known && afterAt.Equal(beforeAt) && after > before {
		total += after - before
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.size, c.measured, c.scanned, c.sweeping = total, start, true, false
	// and the marker holds what this process wrote meanwhile as well
	if c.writeMarker(c.size+c.written, start) {
		c.unrecorded = 0
	}
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

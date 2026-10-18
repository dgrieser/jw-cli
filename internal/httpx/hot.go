package httpx

import (
	"container/list"
	"os"
	"sync"
	"time"
)

// hot keeps in memory what was read from the cache's files lately: the
// header of a body, a memo as it was decoded. A page of the library is built
// from hundreds of memos and the bodies they were derived from, and reading
// and decoding each anew from disk is most of what a page costs once it is
// cached. A file is taken from memory only while it is the file it was read
// from — same size, same modification time — so what another process sharing
// the directory wrote in the meantime is read anew.
type hotCache struct {
	mu   sync.Mutex
	max  int64
	size int64
	ll   *list.List
	m    map[string]*list.Element
}

type hotItem struct {
	path string
	mod  time.Time
	size int64
	val  any
	cost int64
}

// hotMax bounds what is kept in memory, by the bytes of the files it was
// read from: the memos and headers every page of a few languages draws on.
const hotMax = 256 << 20

func newHot(max int64) *hotCache {
	return &hotCache{max: max, ll: list.New(), m: map[string]*list.Element{}}
}

// get returns what was kept for the file at path, if it is still the file
// st describes.
func (h *hotCache) get(path string, st os.FileInfo) (any, bool) {
	if h == nil {
		return nil, false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	el, ok := h.m[path]
	if !ok {
		return nil, false
	}
	it := el.Value.(*hotItem)
	if it.size != st.Size() || !it.mod.Equal(st.ModTime()) {
		h.remove(el)
		return nil, false
	}
	h.ll.MoveToFront(el)
	return it.val, true
}

// put keeps val for the file at path as st describes it.
func (h *hotCache) put(path string, st os.FileInfo, val any, cost int64) {
	if h == nil || cost > h.max/8 {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if el, ok := h.m[path]; ok {
		h.remove(el)
	}
	it := &hotItem{path: path, mod: st.ModTime(), size: st.Size(), val: val, cost: cost}
	h.m[path] = h.ll.PushFront(it)
	h.size += cost
	for h.size > h.max {
		h.remove(h.ll.Back())
	}
}

// restamp notes that the file at path was touched: what was kept for it
// still holds.
func (h *hotCache) restamp(path string, mod time.Time) {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if el, ok := h.m[path]; ok {
		el.Value.(*hotItem).mod = mod
	}
}

// drop forgets the file at path, written over or removed.
func (h *hotCache) drop(path string) {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if el, ok := h.m[path]; ok {
		h.remove(el)
	}
}

func (h *hotCache) remove(el *list.Element) {
	it := el.Value.(*hotItem)
	h.ll.Remove(el)
	delete(h.m, it.path)
	h.size -= it.cost
}

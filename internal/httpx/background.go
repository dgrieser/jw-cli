package httpx

import (
	"context"
	"encoding/json"
	"sync"
	"time"
)

// A reader waiting on upstream is what makes a page slow, and most of that
// waiting is for nothing: once what was kept is older than the freshness
// window, every read of it asks upstream whether it changed — hundreds of
// HEAD requests for the publications page, a day after it was last read —
// and the answer is nearly always that it did not. With background
// revalidation (WithBackgroundRevalidation) a stale body or memo is used at
// once, and asked about upstream behind the reader's back; what changed is
// read by the next reader. Work done in the background — that, and what a
// caller hands Client.Go, such as reading ahead what a reader is likely to
// ask for next — gives way to readers: before each request upstream it waits
// while a reader's request is under way, for a while, so a click is never
// queued behind a page nobody asked for yet.

// maxStale bounds how old a kept body or memo may be and still be used at
// once while it is revalidated in the background; older, the reader waits
// for upstream as without background revalidation.
const maxStale = 30 * 24 * time.Hour

// bgWorkers is how many background tasks run at once.
const bgWorkers = 4

// bgQueue bounds the tasks waiting to run; past it a task is dropped, and
// asked for again by the next read that needs it.
const bgQueue = 4096

// yieldFor bounds how long a background request waits for readers to be
// done, so the background still gets on while somebody reads at length.
var yieldFor = 2 * time.Second

// settle is how long the background waits after the last reader's request
// before it goes on: a page loaded asks for what it shows next right after.
var settle = 150 * time.Millisecond

type background struct {
	mu      sync.Mutex
	pending map[string]bool
	queue   chan bgTask
	started bool
	// running counts the tasks scheduled and not yet done
	running sync.WaitGroup
	// busy counts the readers' requests under way; quiet is closed when it
	// drops to zero
	busy  int
	quiet chan struct{}
	last  time.Time
}

type bgTask struct {
	key string
	run func(context.Context)
}

func newBackground() *background {
	return &background{pending: map[string]bool{}, queue: make(chan bgTask, bgQueue), quiet: make(chan struct{})}
}

type backgroundKey struct{}

// Background marks ctx as work nobody waits on: what it reads gives way to
// the readers' requests, and a stale body or memo it reads is brought up to
// date before it is used rather than after.
func Background(ctx context.Context) context.Context {
	return context.WithValue(ctx, backgroundKey{}, true)
}

// InBackground reports whether ctx is work nobody waits on.
func InBackground(ctx context.Context) bool {
	bg, _ := ctx.Value(backgroundKey{}).(bool)
	return bg
}

// WithBackgroundRevalidation uses a stale body or memo at once and brings it
// up to date in the background, for a long-running server; see background.go.
func WithBackgroundRevalidation() Option {
	return func(c *Client) { c.bg = newBackground() }
}

// later reports whether a stale body or memo read under ctx is revalidated
// in the background rather than before it is used.
func (c *Client) later(ctx context.Context) bool {
	return c != nil && c.bg != nil && !InBackground(ctx)
}

// Backgrounds reports whether the client revalidates in the background.
func (c *Client) Backgrounds() bool { return c != nil && c.bg != nil }

// Go runs fn in the background, once for key until it is done: a second call
// with the same key meanwhile is dropped. fn gets a context marked with
// Background. Without background revalidation Go does nothing and reports
// false.
func (c *Client) Go(key string, fn func(ctx context.Context)) bool {
	if c == nil || c.bg == nil {
		return false
	}
	return c.bg.schedule(key, fn)
}

func (b *background) schedule(key string, fn func(context.Context)) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.pending[key] {
		return false
	}
	select {
	case b.queue <- bgTask{key, fn}:
	default:
		return false
	}
	b.pending[key] = true
	b.running.Add(1)
	if !b.started {
		b.started = true
		for range bgWorkers {
			go b.work()
		}
	}
	return true
}

func (b *background) work() {
	for t := range b.queue {
		func() {
			defer func() {
				// a task that fails takes nothing else down with it
				_ = recover()
				b.mu.Lock()
				delete(b.pending, t.key)
				b.mu.Unlock()
				b.running.Done()
			}()
			t.run(Background(context.Background()))
		}()
	}
}

// Busy notes a reader's request under way, until the returned func is
// called: background work gives way to it meanwhile. A server calls it for
// each request it answers; the client does for each read it makes for one.
func (c *Client) Busy() func() {
	if c == nil || c.bg == nil {
		return func() {}
	}
	return c.bg.enter()
}

func (b *background) enter() func() {
	b.mu.Lock()
	b.busy++
	b.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			b.mu.Lock()
			defer b.mu.Unlock()
			b.busy--
			b.last = time.Now()
			if b.busy == 0 {
				close(b.quiet)
				b.quiet = make(chan struct{})
			}
		})
	}
}

// yield waits, for at most yieldFor, until no reader's request has been
// under way for settle.
func (b *background) yield(ctx context.Context) {
	until := time.Now().Add(yieldFor)
	for {
		b.mu.Lock()
		busy, quiet, since := b.busy, b.quiet, time.Since(b.last)
		b.mu.Unlock()
		left := time.Until(until)
		if busy == 0 && since >= settle || left <= 0 {
			return
		}
		wait := left
		if busy == 0 {
			wait = min(wait, settle-since)
		}
		t := time.NewTimer(wait)
		select {
		case <-quiet:
		case <-t.C:
		case <-ctx.Done():
		}
		t.Stop()
		if ctx.Err() != nil {
			return
		}
	}
}

// revalidateLater brings the body kept under id up to date in the
// background. A changed body is kept unless it no longer decodes as what was
// kept did: the reader it was read for, who would have decided, is not
// there.
func (c *Client) revalidateLater(id string, e entry) {
	c.bg.schedule("GET "+id, func(ctx context.Context) {
		_, kept, ok := c.responses.loadEntry(id, false)
		if !ok {
			return
		}
		// apart from the readers' flight: a reader is not kept waiting on a
		// background request that gives way to it
		v, err, _ := c.flight.Do(id+"\x00bg", func() (any, error) {
			return c.lookup(ctx, id, e.URL, e.header())
		})
		if err != nil {
			return
		}
		f := v.(*fetched)
		if f.pending && decodesAlike(kept, f.body) {
			f.once.Do(func() { c.responses.saveEntry(f.id, f.e, f.body) })
		}
	})
}

// refreshMemoLater brings the memo kept under key up to date in the
// background, rebuilding it with build when one of its sources changed.
func (c *Cache) refreshMemoLater(cl *Client, key string, build func(context.Context) (any, error)) {
	cl.bg.schedule("memo "+key, func(ctx context.Context) {
		_, _, _ = c.memoFlight.Do(key+"\x00bg", func() (any, error) {
			return c.memo(ctx, key, false, build)
		})
	})
}

// decodesAlike reports whether a new body can stand in for the one kept: a
// body kept as JSON is replaced only by JSON.
func decodesAlike(kept, body []byte) bool {
	return !json.Valid(kept) || json.Valid(body)
}

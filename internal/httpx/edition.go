package httpx

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// A site may answer the same address from servers that are not all up to
// date: wol.jw.org serves a page of last year's library from some of its
// servers and of this year's from others, and the CDN in front of them keeps
// whichever it got for hours. When the site says in a body which edition it is
// from, the client never takes an older edition for a newer one: a body older
// than the newest edition seen is asked for again, past the CDN, and a body is
// never replaced by one older than itself.

// Editions reads editions out of a site's bodies.
type Editions interface {
	// Edition is the edition rawURL's body is from: scope names what
	// editions are compared within (one library), n orders them, higher
	// newer. ok is false for a body that does not say.
	Edition(rawURL string, body []byte) (scope string, n int, ok bool)
	// Bust is rawURL asked so that no cache between here and the site's
	// servers answers it, try telling attempts apart; "" for an address
	// that cannot be asked so.
	Bust(rawURL string, try int) string
}

// WithEditions has the client tell editions apart with e.
func WithEditions(e Editions) Option { return func(c *Client) { c.editions = e } }

// editionTries is how often a body older than the newest edition is asked
// for again before the client makes do with what it has.
const editionTries = 4

// A scope whose newest edition seen is older than another scope's is looked
// into, on probePages of its pages every probeEvery: the libraries of all
// languages move to a new year together, so one still at last year's may
// only not have been seen at this year's yet — or be one that has not moved.
const (
	probeEvery = 24 * time.Hour
	probePages = 3
)

// probe is how far a scope was looked into lately.
type probe struct {
	since time.Time
	pages int
}

// editionsKey is where the newest edition of each scope is kept, so a restart
// does not take an old edition for the newest.
const editionsKey = "editions1"

// newestEditions is the newest edition seen of each scope.
type newestEditions struct {
	mu     sync.Mutex
	loaded bool
	n      map[string]int
	// probed is how far each scope was looked into for a newer edition
	probed map[string]probe
}

func (c *Client) newestEdition(scope string) int {
	c.newest.mu.Lock()
	defer c.newest.mu.Unlock()
	c.loadEditions()
	return c.newest.n[scope]
}

// noteEdition records an edition as seen.
func (c *Client) noteEdition(scope string, n int) {
	c.newest.mu.Lock()
	defer c.newest.mu.Unlock()
	c.loadEditions()
	if n <= c.newest.n[scope] {
		return
	}
	c.newest.n[scope] = n
	if c.responses != nil {
		c.responses.Put(editionsKey, c.newest.n)
	}
}

// loadEditions reads the newest editions kept, once; the caller holds mu.
func (c *Client) loadEditions() {
	if c.newest.loaded {
		return
	}
	c.newest.loaded = true
	c.newest.n = map[string]int{}
	if c.responses != nil {
		c.responses.Get(editionsKey, &c.newest.n)
		if c.newest.n == nil {
			c.newest.n = map[string]int{}
		}
	}
}

// newestAny is the newest edition seen of any scope.
func (c *Client) newestAny() int {
	c.newest.mu.Lock()
	defer c.newest.mu.Unlock()
	c.loadEditions()
	m := 0
	for _, n := range c.newest.n {
		m = max(m, n)
	}
	return m
}

// suspect reports whether an edition n of scope may not be the newest: it is
// older than the newest seen of scope, or than the newest of any scope while
// scope is due to be looked into.
func (c *Client) suspect(scope string, n int) bool {
	c.newest.mu.Lock()
	defer c.newest.mu.Unlock()
	c.loadEditions()
	if n < c.newest.n[scope] {
		return true
	}
	for _, m := range c.newest.n {
		if n < m {
			p := c.newest.probed[scope]
			return time.Since(p.since) > probeEvery || p.pages < probePages
		}
	}
	return false
}

// probed notes that scope was looked into for a newer edition.
func (c *Client) probed(scope string) {
	c.newest.mu.Lock()
	defer c.newest.mu.Unlock()
	if c.newest.probed == nil {
		c.newest.probed = map[string]probe{}
	}
	p := c.newest.probed[scope]
	if time.Since(p.since) > probeEvery {
		p = probe{since: time.Now()}
	}
	p.pages++
	c.newest.probed[scope] = p
}

// stamp notes in e which edition body is from.
func (c *Client) stamp(e *entry, body []byte) {
	if c.editions == nil {
		return
	}
	e.EdSeen = true
	e.Scope, e.Ed = "", 0
	if scope, n, ok := c.editions.Edition(e.URL, body); ok {
		e.Scope, e.Ed = scope, n
		c.noteEdition(scope, n)
	}
}

// behind reports whether e may be from an older edition than the newest.
func (c *Client) behind(e entry) bool {
	return c.editions != nil && e.Scope != "" && c.suspect(e.Scope, e.Ed)
}

// older reports whether a is from an older edition than b of the same scope.
func older(a, b entry) bool {
	return a.Scope != "" && a.Scope == b.Scope && a.Ed < b.Ed
}

// bustKey marks the context of a request asked past the caches, with its
// try, so a redirect is followed past them too.
type bustKey struct{}

// downloadPast GETs rawURL past the caches on its way, as far as the site
// lets it be asked so, and as is otherwise.
func (c *Client) downloadPast(ctx context.Context, rawURL string, hdr http.Header, try int) ([]byte, *http.Response, error) {
	if c.editions == nil {
		return c.download(ctx, rawURL, hdr)
	}
	u := c.editions.Bust(rawURL, try)
	if u == "" {
		return c.download(ctx, rawURL, hdr)
	}
	return c.download(context.WithValue(ctx, bustKey{}, try), u, hdr)
}

// bustRedirect asks the target of a redirect past the caches when the
// request it answers was: the page a redirect leads to is in the CDN's
// store as much as any.
func (c *Client) bustRedirect(req *http.Request) {
	try, ok := req.Context().Value(bustKey{}).(int)
	if !ok || c.editions == nil {
		return
	}
	if u := c.editions.Bust(req.URL.String(), try); u != "" {
		if nu, err := url.Parse(u); err == nil {
			req.URL = nu
		}
	}
}

// latest is the newest edition of rawURL's body it can get: body, as it came,
// unless it is older than the newest edition seen, in which case it is asked
// for again past the caches, a few times, for a newer one.
func (c *Client) latest(ctx context.Context, rawURL string, hdr http.Header, body []byte, resp *http.Response) ([]byte, *http.Response) {
	if c.editions == nil {
		return body, resp
	}
	scope, n, ok := c.editions.Edition(rawURL, body)
	if !ok {
		return body, resp
	}
	if !c.suspect(scope, n) {
		return body, resp
	}
	defer c.probed(scope)
	for try := 1; try <= editionTries && n < max(c.newestEdition(scope), c.newestAny()); try++ {
		if c.editions.Bust(rawURL, try) == "" {
			break
		}
		b, r, err := c.downloadPast(ctx, rawURL, hdr, try)
		if err != nil {
			break
		}
		if s, m, ok := c.editions.Edition(rawURL, b); ok && s == scope && m > n {
			body, resp, n = b, r, m
		}
	}
	if n < c.newestEdition(scope) {
		c.verbose("GET %s (an older edition: %s %d)", rawURL, scope, n)
	}
	return body, resp
}

// renew asks again for a kept body that is from an older edition than the
// newest seen. A newer one replaces it; failing that, the kept body is used
// and not asked about again for a while.
func (c *Client) renew(ctx context.Context, id, rawURL string, hdr http.Header, e entry, body []byte) (*fetched, error) {
	nbody, resp, err := c.downloadPast(ctx, rawURL, hdr, 0)
	if err == nil {
		nbody, resp = c.latest(ctx, rawURL, hdr, nbody, resp)
		ne := entry{URL: e.URL, Header: e.Header, NoHead: e.NoHead}
		ne.setValidators(resp, false)
		c.stamp(&ne, nbody)
		if ne.Scope == e.Scope && ne.Ed > e.Ed {
			c.verbose("GET %s (a newer edition)", rawURL)
			return c.pendingBody(id, ne, nbody), nil
		}
	}
	c.backOff(id)
	return &fetched{body: body, id: id, e: e}, nil
}

// checkRedirect follows up to 10 redirects, as net/http does, a request
// asked past the caches past them still.
func (c *Client) checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return errors.New("stopped after 10 redirects")
	}
	c.bustRedirect(req)
	return nil
}

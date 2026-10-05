package httpx

import (
	"context"
	"io"
	"net/http"
	"slices"
	"sync"
	"time"
)

// fetched is a body on its way to a caller: from the cache, or new from
// upstream and kept only once the caller has seen it is good (a JSON body
// that decodes, an HTML page that parses).
type fetched struct {
	body []byte
	id   string
	e    entry
	// pending is set for a new body not yet kept; once makes sure the callers
	// sharing it keep it once
	pending bool
	once    sync.Once
}

// read fetches a body, from the cache when it holds a current one. Concurrent
// reads of the same request share one lookup, and with it one request
// upstream.
func (c *Client) read(ctx context.Context, rawURL string, hdr http.Header) (*fetched, error) {
	key := c.responseKey(rawURL, hdr)
	if key == "" {
		body, _, err := c.download(ctx, rawURL, hdr)
		if err != nil {
			return nil, err
		}
		return &fetched{body: body}, nil
	}
	id := entryID(key)
	// a read past the cache shares no lookup with the reads that use it
	flightKey, lookup := id, c.lookup
	if refreshFirst(ctx, "GET "+id) {
		flightKey, lookup = id+"\x00refresh", c.lookupAnew
	}
	ch := c.flight.DoChan(flightKey, func() (any, error) {
		return lookup(context.WithoutCancel(ctx), id, rawURL, hdr)
	})
	var f *fetched
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case r := <-ch:
		if r.Err != nil {
			return nil, r.Err
		}
		f = r.Val.(*fetched)
	}
	if !f.pending {
		record(ctx, id, f.e.Hash)
	}
	return f, nil
}

// keep stores a new body once its caller has accepted it.
func (c *Client) keep(ctx context.Context, f *fetched) {
	if f.id == "" || !f.pending {
		return
	}
	f.once.Do(func() { c.responses.saveEntry(f.id, f.e, f.body) })
	record(ctx, f.id, f.e.Hash)
}

// lookup answers a read from the cache, revalidating a stale body first, or
// from upstream when there is none.
func (c *Client) lookup(ctx context.Context, id, rawURL string, hdr http.Header) (*fetched, error) {
	// a memo's revalidation may have read the new body a moment ago
	if f := c.unstash(id); f != nil {
		return f, nil
	}
	e, body, ok := c.responses.loadEntry(id, false)
	if ok && e.fresh(c.responses.fresh) {
		c.verbose("GET %s (cached)", rawURL)
		return &fetched{body: body, id: id, e: e}, nil
	}
	if ok {
		return c.refresh(ctx, id, e, body)
	}
	body, resp, err := c.download(ctx, rawURL, hdr)
	if err != nil {
		return nil, err
	}
	e = entry{URL: rawURL, Header: keyHeaders(hdr)}
	e.setValidators(resp, false)
	return c.pendingBody(id, e, body), nil
}

// lookupAnew answers a read from upstream whatever the cache holds. When
// upstream cannot answer, a body kept before is better than none.
func (c *Client) lookupAnew(ctx context.Context, id, rawURL string, hdr http.Header) (*fetched, error) {
	body, resp, err := c.download(ctx, rawURL, hdr)
	if err != nil {
		if IsGone(err) {
			c.responses.removeEntry(id)
			return nil, err
		}
		if e, kept, ok := c.responses.loadEntry(id, false); ok {
			c.verbose("GET %s (cached, upstream unavailable: %v)", rawURL, err)
			return &fetched{body: kept, id: id, e: e}, nil
		}
		return nil, err
	}
	e := entry{URL: rawURL, Header: keyHeaders(hdr)}
	e.setValidators(resp, false)
	return c.pendingBody(id, e, body), nil
}

func (c *Client) pendingBody(id string, e entry, body []byte) *fetched {
	e.Size, e.Hash, e.Checked = int64(len(body)), bodyHash(body), time.Now().Unix()
	return &fetched{body: body, id: id, e: e, pending: true}
}

// refresh revalidates a stale body: a HEAD request first, whose validators
// usually settle it, and a GET only when they say the body changed or cannot
// tell. A body upstream cannot be asked about right now is used as it is.
func (c *Client) refresh(ctx context.Context, id string, e entry, body []byte) (*fetched, error) {
	stale := &fetched{body: body, id: id, e: e}
	if c.backingOff(id) {
		c.verbose("GET %s (cached, upstream unavailable)", e.URL)
		return stale, nil
	}
	hdr := e.header()
	var head *http.Response
	noHead := e.NoHead
	if !noHead {
		var herr error
		head, herr = c.Head(ctx, e.URL, hdr)
		if herr != nil || unavailable(head.StatusCode) {
			c.backOff(id)
			c.verbose("GET %s (cached, upstream unavailable)", e.URL)
			return stale, nil
		}
		if head.StatusCode/100 == 2 {
			if compare(e, head) == unchanged {
				e.setValidators(head, true)
				return c.confirm(id, e, body), nil
			}
			// a page rendered for every request says nothing a HEAD could
			// compare, and never will
			noHead = undecidable(head)
		} else {
			// a HEAD the server does not answer says nothing; if the GET
			// below succeeds, it never will
			head, noHead = nil, true
		}
	}
	nbody, resp, err := c.download(ctx, e.URL, hdr)
	if err != nil {
		if IsGone(err) {
			c.responses.removeEntry(id)
			return nil, err
		}
		c.backOff(id)
		c.verbose("GET %s (cached, upstream unavailable: %v)", e.URL, err)
		return stale, nil
	}
	ne := entry{URL: e.URL, Header: e.Header, NoHead: noHead}
	ne.setValidators(resp, false)
	if head != nil {
		ne.setValidators(head, true)
	}
	if bodyHash(nbody) == e.Hash {
		return c.confirm(id, ne, body), nil
	}
	c.verbose("GET %s (changed)", e.URL)
	return c.pendingBody(id, ne, nbody), nil
}

// confirm records that a kept body is still current and starts a new
// freshness window for it.
func (c *Client) confirm(id string, e entry, body []byte) *fetched {
	e.Checked = time.Now().Unix()
	c.responses.saveEntry(id, e, body)
	e.Size, e.Hash = int64(len(body)), bodyHash(body)
	return &fetched{body: body, id: id, e: e}
}

// revalidate brings a stale body up to date for a memo and returns the hash it
// has now. A changed body is not kept yet — its decoding caller decides
// that — but held for the rebuild about to read it.
func (c *Client) revalidate(ctx context.Context, id string, e entry) (string, bool) {
	v, err, _ := c.flight.Do(id, func() (any, error) {
		return c.lookup(ctx, id, e.URL, e.header())
	})
	if err != nil {
		return "", false
	}
	f := v.(*fetched)
	if f.pending {
		c.stash(f)
	}
	return f.e.Hash, true
}

const stashFor = time.Minute

type stashed struct {
	f  *fetched
	at time.Time
}

func (c *Client) stash(f *fetched) {
	c.stashMu.Lock()
	defer c.stashMu.Unlock()
	if c.stashed == nil {
		c.stashed = map[string]stashed{}
	}
	for id, s := range c.stashed {
		if time.Since(s.at) > stashFor {
			delete(c.stashed, id)
		}
	}
	c.stashed[f.id] = stashed{f, time.Now()}
}

func (c *Client) unstash(id string) *fetched {
	c.stashMu.Lock()
	defer c.stashMu.Unlock()
	s, ok := c.stashed[id]
	if !ok {
		return nil
	}
	delete(c.stashed, id)
	if time.Since(s.at) > stashFor {
		return nil
	}
	return s.f
}

// backOffFor is how long a body upstream could not be asked about is used
// without asking again, so an outage costs a request per body now and then
// rather than one per read.
const backOffFor = 10 * time.Minute

func (c *Client) backOff(id string) { c.failed.Store(id, time.Now()) }

func (c *Client) backingOff(id string) bool {
	v, ok := c.failed.Load(id)
	if !ok {
		return false
	}
	if time.Since(v.(time.Time)) > backOffFor {
		c.failed.Delete(id)
		return false
	}
	return true
}

func unavailable(status int) bool {
	return status >= 500 || status == http.StatusTooManyRequests
}

// Head issues a HEAD request, following redirects, and returns the response
// whatever its status.
func (c *Client) Head(ctx context.Context, rawURL string, hdr http.Header) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, rawURL, nil)
	if err != nil {
		return nil, err
	}
	for k, vs := range hdr {
		req.Header[k] = slices.Clone(vs)
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	resp.Body.Close()
	return resp, nil
}

// download GETs a body whole.
func (c *Client) download(ctx context.Context, rawURL string, hdr http.Header) ([]byte, *http.Response, error) {
	resp, err := c.Get(ctx, rawURL, hdr)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, err
	}
	return body, resp, nil
}

type verdict int

const (
	unknown verdict = iota
	unchanged
	changed
)

// compare tells from a HEAD response whether the body e describes is still
// what upstream serves. The sites differ in what they say: wol.jw.org sends
// an ETag per page (and a Last-Modified that is the same for the whole site),
// the CDN's APIs only a length, www.jw.org a Last-Modified that is when its
// edge last fetched the page. Validators are compared strongest first; when
// none of them settles it the answer is unknown, and a GET decides.
func compare(e entry, head *http.Response) verdict {
	etag, lm := head.Header.Get("ETag"), head.Header.Get("Last-Modified")
	// a length of the identity encoding is comparable to the body kept
	size := head.ContentLength
	sized := size >= 0 && head.Header.Get("Content-Encoding") == ""
	switch {
	case etag != "" && e.ETag != "":
		if etag == e.ETag {
			return unchanged
		}
		return changed
	case sized && size != e.Size:
		return changed
	case lm != "" && lm == e.LastMod:
		return unchanged
	case sized && etag == "" && lm == "":
		return unchanged
	}
	return unknown
}

// undecidable reports whether a HEAD response carries nothing to compare: no
// ETag, no length, and a Last-Modified that is just the time of the request.
func undecidable(head *http.Response) bool {
	if head.Header.Get("ETag") != "" || (head.ContentLength >= 0 && head.Header.Get("Content-Encoding") == "") {
		return false
	}
	return !meaningfulLastMod(head)
}

// meaningfulLastMod reports whether a Last-Modified names when the content
// changed rather than when the response was made.
func meaningfulLastMod(resp *http.Response) bool {
	lm, err := http.ParseTime(resp.Header.Get("Last-Modified"))
	if err != nil {
		return false
	}
	date, err := http.ParseTime(resp.Header.Get("Date"))
	return err != nil || date.Sub(lm) > 2*time.Second
}

// setValidators takes what a response says about its body. A HEAD reports the
// identity encoding; a GET's ETag belongs to whatever encoding the transport
// negotiated, so it is taken only when that was the identity too.
func (e *entry) setValidators(resp *http.Response, fromHead bool) {
	if resp == nil {
		return
	}
	if etag := resp.Header.Get("ETag"); etag != "" && (fromHead || !resp.Uncompressed) {
		e.ETag = etag
	}
	if meaningfulLastMod(resp) {
		e.LastMod = resp.Header.Get("Last-Modified")
	}
}

// keyHeaders are the request headers that change what the server answers,
// the ones a body is kept under and revalidated with.
var keyHeaderNames = []string{"Accept", "X-Requested-With", "Accept-Language"}

func keyHeaders(hdr http.Header) map[string]string {
	var out map[string]string
	for _, k := range keyHeaderNames {
		if v := hdr.Get(k); v != "" {
			if out == nil {
				out = map[string]string{}
			}
			out[k] = v
		}
	}
	return out
}

func (e *entry) header() http.Header {
	h := http.Header{}
	for k, v := range e.Header {
		h.Set(k, v)
	}
	return h
}

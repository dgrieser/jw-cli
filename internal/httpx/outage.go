package httpx

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// When upstream cannot answer, what is kept is used — and the sooner the
// better: a server that accepts a connection and then says nothing would
// otherwise hold every read up for the full timeout of a download, once per
// page and once per command. So a request something kept can stand in for
// (see withKept) gives the server answerWait to start answering, and a host
// that failed one is not asked again for such requests for backOffFor — by
// this process or the next, as the cache keeps which hosts failed. A read
// with nothing to fall back on is not held back by any of this, and is asked
// again a few times when the server says it is busy.

// answerWait is how long a server is given to start answering a HEAD, or a
// request something kept can stand in for.
var answerWait = 10 * time.Second

// errNoAnswer is the cause of a request given up on after answerWait.
var errNoAnswer = errors.New("no answer in time")

// errHostDown answers a request something kept can stand in for while its
// host is backed off.
var errHostDown = errors.New("failed lately, not asked again yet")

// retryDelays are the pauses before asking again a server that answered it
// is busy or that its gateway failed (429, 502, 503, 504): one such answer
// is often all there is, and would otherwise end a read that has nothing
// kept to fall back on. A variable so tests need not wait them out.
var retryDelays = []time.Duration{time.Second, 3 * time.Second}

// maxRetryAfter is the longest Retry-After waited out; a server that asks
// for more is not asked again within the read.
const maxRetryAfter = 5 * time.Second

// send issues a GET or HEAD of rawURL with hdr — both ask without changing
// anything, so asking again is safe — and notes how the host did.
func (c *Client) send(ctx context.Context, method, rawURL string, hdr http.Header) (*http.Response, error) {
	host := hostOf(rawURL)
	kept := hasKept(ctx)
	if kept && c.hostDown(host) {
		c.verbose("%s %s (not asked: upstream unavailable)", method, rawURL)
		return nil, fmt.Errorf("%s %s: %s %w", method, rawURL, host, errHostDown)
	}
	for try := 0; ; try++ {
		if c.bg != nil && InBackground(ctx) {
			c.bg.yield(ctx)
		}
		if err := pace(ctx); err != nil {
			return nil, err
		}
		resp, err := c.attempt(ctx, method, rawURL, hdr, kept || method == http.MethodHead)
		switch {
		case err != nil:
			// a caller that gave up says nothing about the host
			if ctx.Err() == nil && host != "" {
				c.hostFailed(host)
			}
			return nil, err
		case !unavailable(resp.StatusCode) || resp.StatusCode == http.StatusInternalServerError:
			// a 500 is more often about what was asked than about the host
			c.hostAnswered(host)
			return resp, nil
		}
		// busy, or behind a failing gateway: asked again in a moment,
		// unless something kept can stand in for the answer right away
		wait, again := retryWait(resp, try, c.retryDelays)
		if kept || !again {
			c.hostFailed(host)
			return resp, nil
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
		resp.Body.Close()
		c.verbose("%s %s (HTTP %d, asking again in %s)", method, rawURL, resp.StatusCode, wait)
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return nil, ctx.Err()
		case <-t.C:
		}
	}
}

// retryWait is how long to wait before asking again after resp, the answer
// to the try-th attempt, and whether to ask again at all. delays, when set,
// stand in for retryDelays.
func retryWait(resp *http.Response, try int, delays []time.Duration) (time.Duration, bool) {
	if delays == nil {
		delays = retryDelays
	}
	switch resp.StatusCode {
	case http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
	default:
		return 0, false
	}
	if try >= len(delays) {
		return 0, false
	}
	wait := delays[try]
	if ra := strings.TrimSpace(resp.Header.Get("Retry-After")); ra != "" {
		if secs, err := strconv.Atoi(ra); err == nil {
			wait = time.Duration(max(secs, 0)) * time.Second
		} else if at, err := http.ParseTime(ra); err == nil {
			wait = max(time.Until(at), 0)
		}
		if wait > maxRetryAfter {
			return 0, false
		}
	}
	return wait, true
}

// attempt sends one request. With brief, the server has answerWait to start
// answering; the body may take as long as the client's timeout allows.
func (c *Client) attempt(ctx context.Context, method, rawURL string, hdr http.Header, brief bool) (*http.Response, error) {
	rctx, cancel := ctx, context.CancelCauseFunc(nil)
	var timer *time.Timer
	if brief {
		rctx, cancel = context.WithCancelCause(ctx)
		timer = time.AfterFunc(answerWait, func() { cancel(errNoAnswer) })
	}
	req, err := http.NewRequestWithContext(rctx, method, rawURL, nil)
	if err != nil {
		if cancel != nil {
			cancel(nil)
		}
		return nil, err
	}
	for k, vs := range hdr {
		req.Header[k] = slices.Clone(vs)
	}
	resp, err := c.Do(req)
	if cancel == nil {
		return resp, err
	}
	// an answer that came as the time ran out cannot be read either
	if !timer.Stop() && err == nil {
		resp.Body.Close()
		err = errNoAnswer
	}
	if err != nil {
		cancel(nil)
		if errors.Is(context.Cause(rctx), errNoAnswer) {
			err = fmt.Errorf("%s %s: %w within %s", method, rawURL, errNoAnswer, answerWait)
		}
		return nil, err
	}
	resp.Body = cancelOnClose{resp.Body, cancel}
	return resp, nil
}

// cancelOnClose releases a request's context once its body is read.
type cancelOnClose struct {
	io.ReadCloser
	cancel context.CancelCauseFunc
}

func (b cancelOnClose) Close() error {
	err := b.ReadCloser.Close()
	b.cancel(nil)
	return err
}

// hostsKey is where the hosts that failed lately are kept, each with the
// time until which it is not asked again, so the next command does not wait
// on them either.
const hostsKey = "down1"

// downHosts are the hosts that failed lately.
type downHosts struct {
	mu     sync.Mutex
	loaded bool
	until  map[string]int64 // unix seconds
}

// loadHosts reads the hosts kept as failed, once; the caller holds mu.
func (c *Client) loadHosts() {
	if c.down.loaded {
		return
	}
	c.down.loaded = true
	c.down.until = c.keptHosts()
}

func (c *Client) keptHosts() map[string]int64 {
	m := map[string]int64{}
	if c.responses != nil {
		c.responses.Get(hostsKey, &m)
	}
	now := time.Now().Unix()
	for h, until := range m {
		if until <= now {
			delete(m, h)
		}
	}
	return m
}

// hostDown reports whether host failed within the last backOffFor.
func (c *Client) hostDown(host string) bool {
	c.down.mu.Lock()
	defer c.down.mu.Unlock()
	c.loadHosts()
	return c.down.until[host] > time.Now().Unix()
}

// hostFailed backs host off.
func (c *Client) hostFailed(host string) {
	c.markHost(host, time.Now().Add(backOffFor).Unix())
}

// hostAnswered lifts a back-off of host: it answers again.
func (c *Client) hostAnswered(host string) {
	c.down.mu.Lock()
	_, down := c.down.until[host]
	loaded := c.down.loaded
	c.down.mu.Unlock()
	if down || !loaded {
		c.markHost(host, 0)
	}
}

// markHost notes until when host is backed off, 0 for not at all, and keeps
// it, along with what other processes noted meanwhile.
func (c *Client) markHost(host string, until int64) {
	c.down.mu.Lock()
	defer c.down.mu.Unlock()
	c.loadHosts()
	_, was := c.down.until[host]
	if until == 0 && !was {
		return
	}
	m := c.keptHosts()
	now := time.Now().Unix()
	for h, u := range c.down.until {
		if u > now && u > m[h] {
			m[h] = u
		}
	}
	if until == 0 {
		delete(m, host)
	} else {
		m[host] = until
	}
	c.down.until = m
	if c.responses != nil {
		c.responses.Put(hostsKey, m)
	}
}

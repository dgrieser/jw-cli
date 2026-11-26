package httpx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

// A server that jw.org turns away — a bot protection's challenge, a
// refusal, a network that fails — can have proxies registered with it
// (package relay, jw proxy) fetch for it instead. The server always asks
// itself first; only what it could not get goes to a proxy. A host that
// turned it away is then asked through the proxies first for relayFor, so a
// blocked server does not wait on a refusal for every page, and asked
// directly again after that.

// Relay fetches what the client could not, through a proxy.
type Relay interface {
	// Fetch makes the request through a proxy; ErrNoRelay when none can
	// take it.
	Fetch(ctx context.Context, method, rawURL string, hdr http.Header) (*http.Response, error)
	// Allows reports whether a request may go through a proxy at all.
	Allows(method, rawURL string) bool
	// Available reports whether a proxy is there now that can be asked for
	// rawURL: online, and not one that failed at its host lately. Which
	// proxy failed where is the relay's to know.
	Available(rawURL string) bool
	// Rejected tells the relay that an answer it gave to a request of rawURL
	// was no answer — a bot protection's page the proxy took for one — so it
	// counts against the proxy that fetched it, at the host asked.
	Rejected(rawURL string, resp *http.Response)
}

// ProxyIDHeader carries, on an answer a proxy fetched, the proxy's id: what
// a rejection of its answer is counted against.
const ProxyIDHeader = "X-JW-Proxy-ID"

// A read whose content is checked (GetHTMLChecked, GetJSON) carries a
// record of the proxy that delivered it, if one did: content it rejects is
// counted against that proxy before another is asked.
type viaKey struct{}

type viaRecord struct {
	mu    sync.Mutex
	proxy string
}

func withVia(ctx context.Context) (context.Context, *viaRecord) {
	rec := &viaRecord{}
	return context.WithValue(ctx, viaKey{}, rec), rec
}

// noteVia records the proxy an answer came from on the read it is for.
func noteVia(ctx context.Context, resp *http.Response) {
	rec, _ := ctx.Value(viaKey{}).(*viaRecord)
	if rec == nil || resp == nil {
		return
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	rec.proxy = resp.Header.Get(ProxyIDHeader)
}

// rejectVia counts content a reader rejected against the proxy that
// delivered it, if one did.
func (c *Client) rejectVia(rawURL string, rec *viaRecord) {
	rec.mu.Lock()
	id := rec.proxy
	rec.mu.Unlock()
	r := c.relayer()
	if id == "" || r == nil {
		return
	}
	c.verbose("GET %s: not the page; counted against the proxy that gave it", rawURL)
	hdr := http.Header{}
	hdr.Set(ProxyIDHeader, id) // in its canonical form, as Get looks for it
	r.Rejected(rawURL, &http.Response{Header: hdr})
}

// ErrNoRelay is a request no proxy could take: none is registered, enabled
// and connected, or none answered in time.
var ErrNoRelay = errors.New("no proxy could fetch it")

// ErrProxyFailed is a request a proxy took and failed at — its fetch failed,
// it did not answer, it was revoked meanwhile — as opposed to one no proxy
// took: another proxy may get it. It is an ErrNoRelay too.
var ErrProxyFailed = fmt.Errorf("%w: the proxy that took it failed", ErrNoRelay)

// relayFor is how long a host that turned the client away is asked through
// the proxies first.
var relayFor = 10 * time.Minute

// relayState is what the client knows of its proxies: which hosts it asks
// through them first, until when, and why.
type relayState struct {
	mu    sync.Mutex
	relay Relay
	hosts map[string]RelayedHost
}

// RelayedHost is a host asked through the proxies first.
type RelayedHost struct {
	Host   string
	Until  time.Time
	Reason string
}

// SetRelay has the client ask r for what it cannot get itself; nil asks
// nobody.
func (c *Client) SetRelay(r Relay) {
	c.relayed.mu.Lock()
	defer c.relayed.mu.Unlock()
	c.relayed.relay = r
}

func (c *Client) relayer() Relay {
	c.relayed.mu.Lock()
	defer c.relayed.mu.Unlock()
	return c.relayed.relay
}

// RelayedHosts are the hosts asked through the proxies first now.
func (c *Client) RelayedHosts() []RelayedHost {
	c.relayed.mu.Lock()
	defer c.relayed.mu.Unlock()
	now := time.Now()
	var out []RelayedHost
	for _, h := range c.relayed.hosts {
		if now.Before(h.Until) {
			out = append(out, h)
		}
	}
	return out
}

func (c *Client) relayFirst(host string) bool {
	c.relayed.mu.Lock()
	defer c.relayed.mu.Unlock()
	h, ok := c.relayed.hosts[host]
	return ok && time.Now().Before(h.Until)
}

// Three things are kept apart: whether the host failed the client lately
// (hostDown, and a body's own backoff), whether it turned the client away
// (relayFirst: ask the proxies first), and whether a proxy that can serve
// the host is there now (relayUp, which the relay answers: it knows which
// proxy failed where). A body kept from a host that failed is used as it is
// only while no such proxy is there: one that comes online serves it at
// once, and while every proxy fails at the host the backoff holds.

// unreachable reports whether a body kept from rawURL's host is used as it
// is: the host failed lately and no proxy is there to ask instead.
func (c *Client) unreachable(rawURL string) bool {
	return c.hostDown(hostOf(rawURL)) && !c.relayUp(rawURL)
}

// relayUp reports a proxy there now that can be asked for rawURL.
func (c *Client) relayUp(rawURL string) bool {
	r := c.relayer()
	return r != nil && r.Allows(http.MethodGet, rawURL) && r.Available(rawURL)
}

func (c *Client) markRelayed(host, reason string) {
	c.relayed.mu.Lock()
	defer c.relayed.mu.Unlock()
	if c.relayed.hosts == nil {
		c.relayed.hosts = map[string]RelayedHost{}
	}
	if old, ok := c.relayed.hosts[host]; !ok || time.Now().After(old.Until) {
		c.verbose("%s turned this server away (%s): asking the proxies first for %s", host, reason, relayFor)
	}
	c.relayed.hosts[host] = RelayedHost{Host: host, Until: time.Now().Add(relayFor), Reason: reason}
}

// relayNow marks a request that goes to the proxies without asking
// directly first: what was asked directly a moment ago and turned out not
// to be the page asked for.
type relayNowKey struct{}

func withRelayNow(ctx context.Context) context.Context {
	return context.WithValue(ctx, relayNowKey{}, true)
}

func relayNow(ctx context.Context) bool {
	v, _ := ctx.Value(relayNowKey{}).(bool)
	return v
}

// send asks upstream, and the proxies when upstream turned the client away
// (see sendDirect for how it asks upstream).
func (c *Client) send(ctx context.Context, method, rawURL string, hdr http.Header) (*http.Response, error) {
	r := c.relayer()
	if r == nil || !r.Allows(method, rawURL) {
		return c.sendDirect(ctx, method, rawURL, hdr)
	}
	host := hostOf(rawURL)
	// a host that failed lately is not asked again directly while a proxy
	// can be: it would answer at once that it is down
	up := c.relayUp(rawURL)
	if relayNow(ctx) || (up && (c.relayFirst(host) || c.hostDown(host))) {
		resp, err := c.viaRelay(ctx, r, method, rawURL, hdr)
		if err == nil {
			noteVia(ctx, resp)
			if !relayNow(ctx) && !c.relayFirst(host) {
				c.markRelayed(host, "failed lately") // down for the client, served by a proxy
			}
			return resp, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		// what upstream answered a moment ago was not the page: it is not
		// asked again, and what comes back came from a proxy or not at all
		if relayNow(ctx) {
			return nil, err
		}
		// no proxy could: upstream after all
	}
	resp, err := c.sendDirect(ctx, method, rawURL, hdr)
	if ctx.Err() != nil {
		return resp, err
	}
	resp, reason := blocked(resp, err)
	if reason == "" || !c.relayUp(rawURL) {
		return resp, err
	}
	relayed, rerr := c.viaRelay(ctx, r, method, rawURL, hdr)
	if rerr != nil {
		if !errors.Is(rerr, ErrNoRelay) {
			c.verbose("%s %s: through a proxy: %v", method, rawURL, rerr)
		}
		return resp, err
	}
	c.markRelayed(host, reason)
	if resp != nil {
		resp.Body.Close()
	}
	noteVia(ctx, relayed)
	return relayed, nil
}

// viaRelay asks a proxy, and takes its answer only when it is not a
// refusal or a challenge itself. A proxy turned away counts against it, and
// the request goes once more to another that can serve the host, if any.
func (c *Client) viaRelay(ctx context.Context, r Relay, method, rawURL string, hdr http.Header) (*http.Response, error) {
	for try := 0; ; try++ {
		c.verbose("%s %s (through a proxy)", method, rawURL)
		resp, err := r.Fetch(ctx, method, rawURL, hdr)
		again := try == 0 && ctx.Err() == nil
		if err != nil {
			// the proxy that took it failed: once more, if another can
			// serve the host; none took it, or the caller gave up: no
			if again && errors.Is(err, ErrProxyFailed) && r.Available(rawURL) {
				continue
			}
			return nil, err
		}
		resp, reason := blocked(resp, nil)
		if reason == "" {
			return resp, nil
		}
		resp.Body.Close()
		r.Rejected(rawURL, resp)
		if !again || !r.Available(rawURL) {
			return nil, errors.New("the proxy was turned away too: " + reason)
		}
	}
}

// challengeMarkers are what the pages of a bot protection carry in place
// of the page asked for: AWS WAF's and Akamai's challenges and denials.
var challengeMarkers = []string{"awswaf.com", "/challenge.js", "/_sec/cp_challenge/", "bm-verify", "Access Denied</title>", "you're not a robot"}

// challengePeek is how much of an answer is read to tell a challenge: they
// are a few kilobytes, the pages asked for far more.
const challengePeek = 16 << 10

// blocked reports why an answer is upstream turning the client away rather
// than answering, "" for an answer to take as it is. A refusal (403), being
// told to slow down (429), a gateway that fails, a challenge (202), a
// connection that fails — and a 200 that is a challenge page, which is told
// by its first bytes; the answer goes on with them unread.
func blocked(resp *http.Response, err error) (*http.Response, string) {
	if err != nil {
		// a host backed off has something kept standing in; the caller's
		// own context is looked at before this, so a timeout here is the
		// request's own: upstream not answering
		if errors.Is(err, errHostDown) {
			return resp, ""
		}
		return resp, "unreachable: " + err.Error()
	}
	if resp == nil {
		return resp, ""
	}
	switch resp.StatusCode {
	case http.StatusMethodNotAllowed:
		// to a HEAD: the site does not answer HEAD, and a GET will do
		// (headUnsupported); to anything else, a refusal
		if resp.Request != nil && resp.Request.Method == http.MethodHead {
			return resp, ""
		}
		return resp, "HTTP " + http.StatusText(resp.StatusCode)
	case http.StatusAccepted, http.StatusForbidden, http.StatusTooManyRequests,
		http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return resp, "HTTP " + http.StatusText(resp.StatusCode)
	case http.StatusOK:
	default:
		return resp, ""
	}
	if !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/html") || resp.ContentLength > challengePeek*4 {
		return resp, ""
	}
	head, _ := io.ReadAll(io.LimitReader(resp.Body, challengePeek))
	resp.Body = readCloser{io.MultiReader(bytes.NewReader(head), resp.Body), resp.Body}
	if len(head) == challengePeek {
		return resp, "" // a page that size is no challenge
	}
	for _, m := range challengeMarkers {
		if bytes.Contains(head, []byte(m)) {
			return resp, "a bot protection's challenge"
		}
	}
	if jsWall(head) {
		return resp, "a page that asks for JavaScript"
	}
	return resp, ""
}

var (
	tagsRe    = regexp.MustCompile(`(?is)<(script|style|noscript)\b.*?</(script|style|noscript)>|<[^>]*>`)
	noscripts = regexp.MustCompile(`(?is)<noscript\b.*?</noscript>`)
)

// jsWall reports a small page that is nothing but a demand to run
// JavaScript: no text of its own beyond a <noscript> asking for it — the
// shape of a bot protection's challenge whose name is not known here. A page
// of jw.org's sites says far more.
func jsWall(page []byte) bool {
	ns := noscripts.Find(page)
	if ns == nil || !bytes.Contains(bytes.ToLower(ns), []byte("javascript")) {
		return false
	}
	text := strings.Join(strings.Fields(string(tagsRe.ReplaceAll(page, []byte(" ")))), " ")
	return len(text) < 200
}

// readCloser reads from one reader and closes another: a body with what was
// read of it ahead put back in front.
type readCloser struct {
	io.Reader
	io.Closer
}

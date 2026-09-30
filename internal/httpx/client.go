// Package httpx provides the shared HTTP plumbing for all jw.org / wol.jw.org
// API clients: injectable base URLs (for tests), a cookie jar (wol/Akamai),
// a realistic User-Agent, and polite per-host rate limiting.
package httpx

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/time/rate"
)

// BaseURLs holds the origin for each backend service. Tests inject
// httptest.Server URLs here.
type BaseURLs struct {
	CDN   string // b.jw-cdn.org (mediator, pub-media, search, tokens)
	JWOrg string // www.jw.org (bible JSON, articles)
	WOL   string // wol.jw.org
}

func DefaultBaseURLs() BaseURLs {
	return BaseURLs{
		CDN:   "https://b.jw-cdn.org",
		JWOrg: "https://www.jw.org",
		WOL:   "https://wol.jw.org",
	}
}

// A realistic browser UA: wol.jw.org sits behind Akamai bot management and
// rejects obvious non-browser clients.
const defaultUserAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"

type Client struct {
	hc        *http.Client
	Base      BaseURLs
	UserAgent string
	limiters  map[string]*rate.Limiter // keyed by host
	verbose   func(format string, args ...any)
	// responses keeps the bodies of successful reads on disk for responseTTL,
	// so a page read once is not read again the same day — by the next
	// command, or by jw serve after a restart. Nil keeps nothing.
	responses   *Cache
	responseTTL time.Duration
}

type Option func(*Client)

func WithBaseURLs(b BaseURLs) Option { return func(c *Client) { c.Base = b } }

func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.hc = h } }

func WithUserAgent(ua string) Option { return func(c *Client) { c.UserAgent = ua } }

func WithVerbose(f func(format string, args ...any)) Option {
	return func(c *Client) { c.verbose = f }
}

// WithResponseCache keeps the body of every successful GetJSON, GetHTML and
// GetText in cache for ttl. Get and Do stay uncached: they hand the caller a
// live response, which is what a download streams. A zero ttl or a nil cache
// turns it off.
func WithResponseCache(cache *Cache, ttl time.Duration) Option {
	return func(c *Client) {
		if cache != nil && ttl > 0 {
			c.responses, c.responseTTL = cache, ttl
		}
	}
}

// ResponseTTL is how long response bodies are kept, the default for the CLI
// and jw serve alike: the library's pages change rarely, and a day is what a
// reader asks the same thing again within.
const ResponseTTL = 24 * time.Hour

func New(opts ...Option) *Client {
	jar, _ := cookiejar.New(nil)
	c := &Client{
		hc:        &http.Client{Timeout: 60 * time.Second, Jar: jar},
		Base:      DefaultBaseURLs(),
		UserAgent: defaultUserAgent,
		verbose:   func(string, ...any) {},
	}
	for _, o := range opts {
		o(c)
	}
	if c.hc.Jar == nil {
		c.hc.Jar = jar
	}
	c.limiters = map[string]*rate.Limiter{}
	for _, raw := range []string{c.Base.WOL, c.Base.JWOrg} {
		if h := hostOf(raw); h != "" {
			c.limiters[h] = rate.NewLimiter(requestsPerSecond, requestsPerSecond)
		}
	}
	return c
}

// requestsPerSecond paces requests to wol.jw.org and www.jw.org. Neither site
// publishes a limit; this is what the client holds itself to, and doubles as
// the burst so a pause is spent rather than saved up.
const requestsPerSecond = 50

func hostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Host
}

// Do applies User-Agent and rate limiting, then executes the request.
func (c *Client) Do(req *http.Request) (*http.Response, error) {
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", c.UserAgent)
	}
	if req.Header.Get("Accept-Language") == "" {
		req.Header.Set("Accept-Language", "en")
	}
	if lim, ok := c.limiters[req.URL.Host]; ok {
		if err := lim.Wait(req.Context()); err != nil {
			return nil, err
		}
	}
	c.verbose("GET %s", req.URL)
	return c.hc.Do(req)
}

// Get issues a GET and returns the response if the status is 2xx.
// The caller must close the body.
func (c *Client) Get(ctx context.Context, rawURL string, hdr http.Header) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	for k, vs := range hdr {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		defer resp.Body.Close()
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, &StatusError{URL: rawURL, StatusCode: resp.StatusCode, Body: strings.TrimSpace(string(snippet))}
	}
	return resp, nil
}

// GetJSON fetches rawURL and decodes the JSON body into out.
func (c *Client) GetJSON(ctx context.Context, rawURL string, hdr http.Header, out any) error {
	if hdr == nil {
		hdr = http.Header{}
	}
	if hdr.Get("Accept") == "" {
		hdr.Set("Accept", "application/json")
	}
	body, key, err := c.read(ctx, rawURL, hdr)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decode %s: %w", rawURL, err)
	}
	// only a body that decoded is worth keeping
	c.keep(key, body)
	return nil
}

// read fetches a body, from the response cache when it holds one. key is what
// to keep the body under once the caller has seen it is good, empty when it is
// not to be kept.
func (c *Client) read(ctx context.Context, rawURL string, hdr http.Header) ([]byte, string, error) {
	key := c.responseKey(rawURL, hdr)
	if key != "" {
		if body, ok := c.responses.GetBytes(key, c.responseTTL); ok {
			c.verbose("GET %s (cached)", rawURL)
			return body, "", nil
		}
	}
	resp, err := c.Get(ctx, rawURL, hdr)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}
	return body, key, nil
}

func (c *Client) keep(key string, body []byte) {
	if key != "" {
		c.responses.PutBytes(key, body)
	}
}

// responseKey is what a request's body is kept under: the URL and the headers
// that change what the server answers with. A request carrying credentials is
// not kept, and neither is the token that credentials are made from: both
// expire on their own schedule, not the cache's.
func (c *Client) responseKey(rawURL string, hdr http.Header) string {
	if c.responses == nil || hdr.Get("Authorization") != "" || strings.Contains(rawURL, "/tokens/") {
		return ""
	}
	return "GET " + rawURL + "\n" + hdr.Get("Accept") + "\n" + hdr.Get("X-Requested-With") + "\n" + hdr.Get("Accept-Language")
}

// XHRHeader mimics the site's AJAX requests; wol's bc/pc/dt endpoints return
// JSON instead of HTML when these headers are present.
func XHRHeader() http.Header {
	h := http.Header{}
	h.Set("X-Requested-With", "XMLHttpRequest")
	h.Set("Accept", "application/json, text/javascript, */*; q=0.01")
	return h
}

// GetHTML fetches rawURL and parses the body as an HTML document.
func (c *Client) GetHTML(ctx context.Context, rawURL string) (*goquery.Document, error) {
	body, key, err := c.read(ctx, rawURL, http.Header{})
	if err != nil {
		return nil, err
	}
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("parse HTML %s: %w", rawURL, err)
	}
	c.keep(key, body)
	return doc, nil
}

// GetText fetches rawURL and returns the body as a string.
func (c *Client) GetText(ctx context.Context, rawURL string, hdr http.Header) (string, error) {
	if hdr == nil {
		hdr = http.Header{}
	}
	body, key, err := c.read(ctx, rawURL, hdr)
	if err != nil {
		return "", err
	}
	c.keep(key, body)
	return string(body), nil
}

type StatusError struct {
	URL        string
	StatusCode int
	Body       string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("GET %s: HTTP %d", e.URL, e.StatusCode)
}

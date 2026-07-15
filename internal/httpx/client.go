// Package httpx provides the shared HTTP plumbing for all jw.org / wol.jw.org
// API clients: injectable base URLs (for tests), a cookie jar (wol/Akamai),
// a realistic User-Agent, and polite per-host rate limiting.
package httpx

import (
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
}

type Option func(*Client)

func WithBaseURLs(b BaseURLs) Option { return func(c *Client) { c.Base = b } }

func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.hc = h } }

func WithUserAgent(ua string) Option { return func(c *Client) { c.UserAgent = ua } }

func WithVerbose(f func(format string, args ...any)) Option {
	return func(c *Client) { c.verbose = f }
}

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
	if h := hostOf(c.Base.WOL); h != "" {
		c.limiters[h] = rate.NewLimiter(2, 2) // be polite to wol.jw.org
	}
	return c
}

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
	resp, err := c.Get(ctx, rawURL, hdr)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode %s: %w", rawURL, err)
	}
	return nil
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
	resp, err := c.Get(ctx, rawURL, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("parse HTML %s: %w", rawURL, err)
	}
	return doc, nil
}

// GetText fetches rawURL and returns the body as a string.
func (c *Client) GetText(ctx context.Context, rawURL string, hdr http.Header) (string, error) {
	resp, err := c.Get(ctx, rawURL, hdr)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

type StatusError struct {
	URL        string
	StatusCode int
	Body       string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("GET %s: HTTP %d", e.URL, e.StatusCode)
}

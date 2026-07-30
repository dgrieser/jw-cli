// Package wol is a client for wol.jw.org (Watchtower Online Library):
// documents, bible chapters with study material, citations, search, and
// daily text. wol serves server-rendered HTML plus JSON "panel" endpoints
// that require XHR headers.
package wol

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"time"

	"github.com/dgrieser/jw-cli/internal/httpx"
)

type Client struct {
	hc    *httpx.Client
	cache *httpx.Cache
}

func New(hc *httpx.Client, cache *httpx.Cache) *Client {
	return &Client{hc: hc, cache: cache}
}

// Config is the per-language URL configuration: wol paths look like
// /{locale}/wol/{cmd}/{rsconf}/{lp}/... (English: /en/wol/d/r1/lp-e/...).
type Config struct {
	Locale string `json:"locale"` // "en", "de", ...
	Rsconf string `json:"rsconf"` // "r1", "r10", ...
	Lp     string `json:"lp"`     // "lp-e", "lp-x", ...
}

var cfgPathPattern = regexp.MustCompile(`^/([^/]+)/wol/[a-z]+/(r\d+)/(lp-[a-z0-9-]+)`)

// cfgPatternFor matches links belonging to the requested locale only. Used as
// a fallback when the homepage does not redirect.
func cfgPatternFor(locale string) *regexp.Regexp {
	return regexp.MustCompile(`/` + regexp.QuoteMeta(locale) + `/wol/[a-z]+/(r\d+)/(lp-[a-z0-9-]+)`)
}

// ConfigFor discovers the rsconf/lp pair for a locale. /{locale} redirects to
// that language's home document (/de -> /de/wol/h/r10/lp-x), so the final URL
// is authoritative. Scraping the body is not: the page opens with a
// <link rel="alternate"> block for every other language, and those hreflang
// locales are not unique (/en/wol/h/r969/lp-mnn is listed as hreflang="en"),
// so even a locale-anchored body match can pick up a foreign pair.
// Cached for a week.
func (c *Client) ConfigFor(ctx context.Context, locale string) (Config, error) {
	// v2: v1 entries may hold a foreign rsconf/lp pair.
	key := "wolcfg2-" + locale
	var cfg Config
	if c.cache.Get(key, 7*24*time.Hour, &cfg) && cfg.Rsconf != "" {
		return cfg, nil
	}
	home := c.hc.Base.WOL + "/" + locale
	resp, err := c.hc.Get(ctx, home, nil)
	if err != nil {
		return Config{}, fmt.Errorf("discover wol config for %q: %w", locale, err)
	}
	defer resp.Body.Close()

	if m := cfgPathPattern.FindStringSubmatch(finalPath(resp)); m != nil {
		cfg = Config{Locale: m[1], Rsconf: m[2], Lp: m[3]}
		c.cache.Put(key, cfg)
		return cfg, nil
	}
	// no redirect (or an unexpected target): fall back to a body scan
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return Config{}, fmt.Errorf("discover wol config for %q: %w", locale, err)
	}
	m := cfgPatternFor(locale).FindSubmatch(body)
	if m == nil {
		return Config{}, fmt.Errorf("could not find wol library config for %q on %s (page layout changed?)", locale, home)
	}
	cfg = Config{Locale: locale, Rsconf: string(m[1]), Lp: string(m[2])}
	c.cache.Put(key, cfg)
	return cfg, nil
}

// finalPath is the request path after redirects.
func finalPath(resp *http.Response) string {
	if resp.Request == nil || resp.Request.URL == nil {
		return ""
	}
	return resp.Request.URL.Path
}

// url builds /{locale}/wol/{cmd}/{rsconf}/{lp}{rest}.
func (c *Client) url(cfg Config, command, rest string) string {
	return fmt.Sprintf("%s/%s/wol/%s/%s/%s%s", c.hc.Base.WOL, cfg.Locale, command, cfg.Rsconf, cfg.Lp, rest)
}

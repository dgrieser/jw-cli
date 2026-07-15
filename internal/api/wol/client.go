// Package wol is a client for wol.jw.org (Watchtower Online Library):
// documents, bible chapters with study material, citations, search, and
// daily text. wol serves server-rendered HTML plus JSON "panel" endpoints
// that require XHR headers.
package wol

import (
	"context"
	"fmt"
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

var cfgPattern = regexp.MustCompile(`/wol/[a-z]+/(r\d+)/(lp-[a-z0-9-]+)`)

// ConfigFor discovers the rsconf/lp pair for a locale by loading the wol
// homepage for that locale and reading any content link. Cached for a week.
func (c *Client) ConfigFor(ctx context.Context, locale string) (Config, error) {
	key := "wolcfg-" + locale
	var cfg Config
	if c.cache.Get(key, 7*24*time.Hour, &cfg) && cfg.Rsconf != "" {
		return cfg, nil
	}
	page, err := c.hc.GetText(ctx, c.hc.Base.WOL+"/"+locale, nil)
	if err != nil {
		return Config{}, fmt.Errorf("discover wol config for %q: %w", locale, err)
	}
	m := cfgPattern.FindStringSubmatch(page)
	if m == nil {
		return Config{}, fmt.Errorf("could not find wol library config on %s/%s (page layout changed?)", c.hc.Base.WOL, locale)
	}
	cfg = Config{Locale: locale, Rsconf: m[1], Lp: m[2]}
	c.cache.Put(key, cfg)
	return cfg, nil
}

// url builds /{locale}/wol/{cmd}/{rsconf}/{lp}{rest}.
func (c *Client) url(cfg Config, command, rest string) string {
	return fmt.Sprintf("%s/%s/wol/%s/%s/%s%s", c.hc.Base.WOL, cfg.Locale, command, cfg.Rsconf, cfg.Lp, rest)
}

package wol

import (
	"context"
	"net/url"
	"slices"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"github.com/dgrieser/jw-cli/internal/httpx"
)

// GuideCitations is what the research guide writes under each entry it lists:
// the entry names an article by its headline ("Mut in finsteren Zeiten"),
// and the line under it says where it is ("wcg Kap. 54"). The study pane
// gives the headline only, so the guide's own document — the one an entry's
// /pc/ path points into, the guide to one book of the bible — is read for
// the rest, once per book: keyed by an entry's place in it (GuideKey).
func (c *Client) GuideCitations(ctx context.Context, pcPath string) (map[string]string, error) {
	doc, ok := guideDocURL(c.hc.Base.WOL, pcPath)
	if !ok {
		return nil, nil
	}
	return httpx.Memo(ctx, c.cache, "guide-citations1-"+doc, func(ctx context.Context) (map[string]string, error) {
		page, err := c.hc.GetHTML(ctx, doc)
		if err != nil {
			return nil, err
		}
		return guideCitations(page), nil
	})
}

// guideCitations reads an entry's citation off the line set under its link,
// the guide's subdued one.
func guideCitations(page *goquery.Document) map[string]string {
	out := map[string]string{}
	page.Find(`a[href*="/pc/"]`).Each(func(_ int, a *goquery.Selection) {
		key := GuideKey(a.AttrOr("href", ""))
		if key == "" {
			return
		}
		next := a.Closest("div").Next()
		if !strings.Contains(next.AttrOr("class", ""), "textSubdued") {
			return
		}
		if cite := cleanSpace(next.Text()); cite != "" {
			out[key] = cite
		}
	})
	return out
}

// GuideKey is where a /pc/ path points in its document — the document, the
// paragraph and the item ("1200029075/0/0") — the same however the path is
// spelled: relative or not, with a locale or without.
func GuideKey(pcPath string) string {
	u, err := url.Parse(pcPath)
	if err != nil {
		return ""
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	i := slices.Index(parts, "pc")
	if i < 0 || len(parts) < i+6 {
		return ""
	}
	return strings.Join(parts[i+3:i+6], "/")
}

// guideDocURL is the document a /pc/ path points into:
// /de/wol/pc/r10/lp-x/1200029075/0/0 is a passage of /de/wol/d/r10/lp-x/1200029075.
func guideDocURL(base, pcPath string) (string, bool) {
	u, err := url.Parse(pcPath)
	if err != nil {
		return "", false
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	i := slices.Index(parts, "pc")
	if i < 0 || len(parts) < i+4 {
		return "", false
	}
	doc := append(append(append([]string{}, parts[:i]...), "d"), parts[i+1:i+4]...)
	return absURL(base, "/"+strings.Join(doc, "/")), true
}

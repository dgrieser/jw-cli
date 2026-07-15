// Package htmlx has shared HTML extraction helpers used by the wol and jworg
// document parsers.
package htmlx

import (
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"github.com/dgrieser/jw-cli/internal/model"
)

// Images collects figure/inline images with captions from sel. Relative
// sources are absolutized against base.
func Images(sel *goquery.Selection, base string) []model.MediaAsset {
	var out []model.MediaAsset
	seen := map[string]bool{}
	add := func(src, alt, caption string) {
		src = absolutize(src, base)
		if src == "" || seen[src] {
			return
		}
		seen[src] = true
		out = append(out, model.MediaAsset{URL: src, Alt: alt, Caption: caption})
	}
	// responsive spans first: they carry the largest renditions
	sel.Find("span.jsRespImg").Each(func(_ int, s *goquery.Selection) {
		src := ""
		for _, attr := range []string{"data-zoom", "data-img-size-xl", "data-img-size-lg", "data-img-size-md", "data-img-size-sm", "data-img-size-xs"} {
			if v, ok := s.Attr(attr); ok && v != "" {
				src = v
				break
			}
		}
		alt, _ := s.Attr("data-img-att-alt")
		add(src, alt, captionFor(s))
	})
	sel.Find("img").Each(func(_ int, s *goquery.Selection) {
		if s.Closest("noscript").Length() > 0 && s.Closest("span.jsRespImg").Length() > 0 {
			return // fallback of a span we already handled
		}
		src := ""
		for _, attr := range []string{"data-zoom", "data-img-size-xl", "data-img-size-lg", "src"} {
			if v, ok := s.Attr(attr); ok && v != "" {
				src = v
				break
			}
		}
		alt, _ := s.Attr("alt")
		add(src, alt, captionFor(s))
	})
	return out
}

func captionFor(s *goquery.Selection) string {
	if fig := s.Closest("figure"); fig.Length() > 0 {
		if cap := fig.Find("figcaption"); cap.Length() > 0 {
			return clean(cap.Text())
		}
	}
	for _, parentSel := range []string{".pictureContainer", ".imgGrid", "figure", "div"} {
		p := s.Closest(parentSel)
		if p.Length() == 0 {
			continue
		}
		if cap := p.Find(".caption, figcaption").First(); cap.Length() > 0 {
			return clean(cap.Text())
		}
		break
	}
	return ""
}

// ScriptureRefs collects bible citation anchors (class "b" with data-bid) as
// found in wol documents and jw.org articles.
func ScriptureRefs(sel *goquery.Selection, base string) []model.ScriptureAnchor {
	var out []model.ScriptureAnchor
	sel.Find("a.b, a[data-bid]").Each(func(_ int, s *goquery.Selection) {
		text := clean(s.Text())
		if text == "" {
			return
		}
		href, _ := s.Attr("href")
		bid, _ := s.Attr("data-bid")
		out = append(out, model.ScriptureAnchor{
			Text:   text,
			BCPath: absolutize(href, base),
			BID:    bid,
		})
	})
	return out
}

func absolutize(raw, base string) string {
	if raw == "" || base == "" {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if u.IsAbs() {
		return raw
	}
	b, err := url.Parse(base)
	if err != nil {
		return raw
	}
	return b.ResolveReference(u).String()
}

func clean(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

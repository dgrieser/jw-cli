package render

import (
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// sanitize normalizes a site HTML fragment before conversion:
//   - drops script/style/noscript and hidden UI chrome
//   - materializes responsive images (span.jsRespImg data attributes) into
//     plain <img> tags with the largest available source
//   - prefers data-img-size-* attributes on <img> tags
//   - absolutizes href/src attributes against baseURL
func sanitize(fragment string, baseURL string) (string, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(fragment))
	if err != nil {
		return "", err
	}
	body := doc.Find("body")

	// responsive image spans -> <img>
	body.Find("span.jsRespImg").Each(func(_ int, s *goquery.Selection) {
		src := ""
		for _, attr := range []string{"data-zoom", "data-img-size-xl", "data-img-size-lg", "data-img-size-md", "data-img-size-sm", "data-img-size-xs"} {
			if v, ok := s.Attr(attr); ok && v != "" {
				src = v
				break
			}
		}
		if src == "" {
			if img := s.Find("noscript img"); img.Length() > 0 {
				src, _ = img.Attr("src")
			}
		}
		if src == "" {
			s.Remove()
			return
		}
		alt, _ := s.Attr("data-img-att-alt")
		s.ReplaceWithHtml(`<img src="` + src + `" alt="` + alt + `"/>`)
	})

	body.Find("script, style, noscript").Remove()

	// prefer larger image renditions on plain <img>
	body.Find("img").Each(func(_ int, s *goquery.Selection) {
		for _, attr := range []string{"data-zoom", "data-img-size-xl", "data-img-size-lg"} {
			if v, ok := s.Attr(attr); ok && v != "" {
				s.SetAttr("src", v)
				break
			}
		}
	})

	if baseURL != "" {
		if base, err := url.Parse(baseURL); err == nil {
			absolutize(body, "a", "href", base)
			absolutize(body, "img", "src", base)
		}
	}

	html, err := body.Html()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(html), nil
}

func absolutize(body *goquery.Selection, tag, attr string, base *url.URL) {
	body.Find(tag).Each(func(_ int, s *goquery.Selection) {
		v, ok := s.Attr(attr)
		if !ok || v == "" || strings.HasPrefix(v, "#") ||
			strings.HasPrefix(v, "data:") || strings.HasPrefix(v, "mailto:") {
			return
		}
		u, err := url.Parse(v)
		if err != nil {
			return
		}
		s.SetAttr(attr, base.ResolveReference(u).String())
	})
}

package render

import (
	"html"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"
	xhtml "golang.org/x/net/html"
)

// sanitize normalizes a site HTML fragment before conversion:
//   - drops script/style/noscript and hidden UI chrome
//   - materializes responsive images (span.jsRespImg data attributes) into
//     plain <img> tags with the largest available source
//   - prefers data-img-size-* attributes on <img> tags
//   - flattens wol's multi-line publication link cards into one line each
//   - drops soft hyphens, which only a browser knows how to hide
//   - absolutizes href/src attributes against o.BaseURL, or strips link targets
//     and image sources entirely under o.NoURLs
func sanitize(fragment string, o Options) (string, error) {
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
	flattenLinkCards(body)
	for _, n := range body.Nodes {
		stripSoftHyphens(n)
	}

	// prefer larger image renditions on plain <img>
	body.Find("img").Each(func(_ int, s *goquery.Selection) {
		for _, attr := range []string{"data-zoom", "data-img-size-xl", "data-img-size-lg"} {
			if v, ok := s.Attr(attr); ok && v != "" {
				s.SetAttr("src", v)
				break
			}
		}
	})

	if o.NoURLs {
		// nothing left to absolutize once the targets are gone
		stripURLs(body)
	} else if o.BaseURL != "" {
		if base, err := url.Parse(o.BaseURL); err == nil {
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

// cardDecoration is the parts of a wol link card that carry no text: the
// publication cover thumbnail, the section icon and the chevron. They have no
// alt text, so they convert to an empty image or nothing at all.
const cardDecoration = ".cardThumbnail, .cardChevron, .sectionIcon, .cardTitleDetail"

// cardSeparator joins the lines of a flattened card.
const cardSeparator = " — "

// flattenLinkCards rewrites wol's publication links. wol builds them as cards —
// a cover thumbnail, then one <div> per line of text, all nested inside the <a>
// — which converts to an empty image followed by hard line breaks inside the
// link text, so a single link comes out spread over five ragged lines. The
// cards are the content of `jw meetings`, so they are flattened to one line
// each instead of being dropped.
func flattenLinkCards(body *goquery.Selection) {
	body.Find("a.cardContainer").Each(func(_ int, card *goquery.Selection) {
		card.Find(cardDecoration).Remove()
		var lines []string
		// one <div> per line, whatever wol names its classes
		card.Find(".cardTitleBlock").First().Children().Each(func(_ int, line *goquery.Selection) {
			if t := collapseSpace(line.Text()); t != "" {
				lines = append(lines, t)
			}
		})
		if len(lines) == 0 {
			if t := collapseSpace(card.Text()); t != "" {
				lines = []string{t}
			}
		}
		card.SetHtml(html.EscapeString(strings.Join(lines, cardSeparator)))
	})
}

// softHyphen is U+00AD, a conditional hyphen: it marks where a word may be
// broken and is shown only if the break happens there.
const softHyphen = "\u00ad"

// stripSoftHyphens removes them from every text node. wol peppers long German
// compounds with soft hyphens for the browser's line breaking; nothing outside a
// browser hides them, so a terminal shows a stray gap mid-word
// ("Tausendjahr herrschaft"). The no-break spaces around numbers and dates are
// left alone — those hold text together on purpose.
func stripSoftHyphens(n *xhtml.Node) {
	if n.Type == xhtml.TextNode {
		n.Data = strings.ReplaceAll(n.Data, softHyphen, "")
		return
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		stripSoftHyphens(c)
	}
}

// stripURLs takes every target out of the fragment for --no-urls, without
// taking the words with it: a link is replaced by its own content, so the link
// text stays part of the sentence, and an image by the same "[image: alt]"
// placeholder the text renderer writes, so a picture still leaves a trace. An
// image with no alt text says nothing once its source is gone and is dropped.
// Images are handled first, so one wrapped in a link is already a placeholder
// by the time the link is unwrapped.
func stripURLs(body *goquery.Selection) {
	body.Find("img").Each(func(_ int, s *goquery.Selection) {
		alt := collapseSpace(s.AttrOr("alt", ""))
		if alt == "" {
			s.Remove()
			return
		}
		s.ReplaceWithHtml(html.EscapeString(imagePlaceholder(alt)))
	})
	body.Find("a").Each(func(_ int, s *goquery.Selection) {
		inner, err := s.Html()
		if err != nil {
			inner = html.EscapeString(s.Text())
		}
		s.ReplaceWithHtml(inner)
	})
}

// imagePlaceholder is how an image is spelled where its source cannot be shown:
// in plain text, and under --no-urls in every format.
func imagePlaceholder(alt string) string { return "[image: " + alt + "]" }

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

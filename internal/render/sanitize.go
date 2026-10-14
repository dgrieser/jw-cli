package render

import (
	"html"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"
	xhtml "golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// sanitize normalizes a site HTML fragment before conversion:
//   - drops script/style/noscript and hidden UI chrome: the image viewer's
//     lightbox and the page's form controls (pageChrome), and HTML comments
//   - writes an ordered list whose items carry their own number as plain
//     blocks, so the number is printed once
//   - materializes responsive images (span.jsRespImg data attributes) into
//     plain <img> tags with the largest available source
//   - prefers data-img-size-* attributes on <img> tags
//   - flattens wol's multi-line publication link cards into one line each
//   - drops soft hyphens, which only a browser knows how to hide
//   - absolutizes href/src attributes against o.BaseURL, or strips link targets,
//     image sources and every other attribute holding a URL under o.NoURLs
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
	body.Find(pageChrome).Remove()
	flattenLinkCards(body)
	unlistSelfNumbered(body)
	for _, n := range body.Nodes {
		dropComments(n)
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
		for _, n := range body.Nodes {
			stripURLAttrs(n)
		}
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

// pageChrome is what a document page carries inside its article that is no
// reading matter: the lightbox wol builds its image viewer from (PhotoSwipe's
// .pswp, two dozen elements with its buttons), and the page's form controls —
// some twenty-five hidden fields its scripts read their settings from, among
// them the pub-media endpoint and the share link, which no rendering wants and
// --no-urls must not leak. A study article's answer fields (textarea) stay.
const pageChrome = ".pswp, input, button"

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

// selOwnNumber is the number an item of a list carries in its text: wol's
// study questions are an <ol> whose every item opens with "<span
// class="txtSrcBullet"><strong>1.</strong></span>".
const selOwnNumber = ".txtSrcBullet"

// unlistSelfNumbered writes an ordered list whose every item carries its own
// number as plain blocks. Kept a list, each item would be numbered twice —
// "1. **1.** Why can we say ..." — and the number the source prints is the one
// to keep: it need not start at one, nor be a digit.
func unlistSelfNumbered(body *goquery.Selection) {
	body.Find("ol").Each(func(_ int, ol *goquery.Selection) {
		items := ol.ChildrenFiltered("li")
		if items.Length() == 0 || items.FilterFunction(func(_ int, li *goquery.Selection) bool {
			return li.Find(selOwnNumber).Length() == 0
		}).Length() > 0 {
			return
		}
		for _, n := range append(ol.Nodes, items.Nodes...) {
			n.Data, n.DataAtom = "div", atom.Div
		}
	})
}

// dropComments removes the HTML comments, which no format prints on purpose:
// a jw.org article ends in "<!--THE END-->", which markdown kept as it is.
func dropComments(n *xhtml.Node) {
	for c := n.FirstChild; c != nil; {
		next := c.NextSibling
		if c.Type == xhtml.CommentNode {
			n.RemoveChild(c)
		} else {
			dropComments(c)
		}
		c = next
	}
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

// stripURLAttrs drops whatever attribute still holds a URL once the links and
// images are gone — a lazy-loaded source in a data attribute, say — so the
// HTML output under --no-urls names no address at all.
func stripURLAttrs(n *xhtml.Node) {
	if n.Type == xhtml.ElementNode {
		kept := n.Attr[:0]
		for _, a := range n.Attr {
			if !looksLikeURL(a.Val) {
				kept = append(kept, a)
			}
		}
		n.Attr = kept
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		stripURLAttrs(c)
	}
}

// looksLikeURL reports whether an attribute value is an address: absolute,
// protocol-relative, or a site path.
func looksLikeURL(v string) bool {
	v = strings.TrimSpace(v)
	return strings.Contains(v, "://") || strings.HasPrefix(v, "/") ||
		strings.HasPrefix(v, "mailto:")
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

// printEditionsOnce writes a run of wol's "Printed Edition" blocks — one per
// spread of the printed page, each a picture and the same title — as that
// title once, followed by the pictures. Text drops pictures, so each block
// came out as a bare "Printed Edition" line, as many as there are spreads.
// HTML keeps the blocks as they are: jw serve folds the same run into one
// disclosure of its own.
func printEditionsOnce(fragment string) string {
	if !strings.Contains(fragment, "alternatePresentation") {
		return fragment
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(fragment))
	if err != nil {
		return fragment
	}
	doc.Find("div.alternatePresentation").Each(func(_ int, div *goquery.Selection) {
		// a run of them is written from its first
		if prev := div.Prev(); prev.Length() > 0 && prev.HasClass("alternatePresentation") {
			return
		}
		run := div
		for next := div.Next(); next.Length() > 0 && next.HasClass("alternatePresentation"); next = next.Next() {
			run = run.AddSelection(next)
		}
		title := collapseSpace(div.Find(".title").First().Text())
		var b strings.Builder
		if title != "" {
			b.WriteString("<p>" + html.EscapeString(title) + "</p>")
		}
		run.Find("img").Each(func(_ int, img *goquery.Selection) {
			if h, err := goquery.OuterHtml(img); err == nil {
				b.WriteString("<p>" + h + "</p>")
			}
		})
		div.BeforeHtml(b.String())
		run.Remove()
	})
	out, err := doc.Find("body").Html()
	if err != nil {
		return fragment
	}
	return out
}

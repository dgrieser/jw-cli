// Package htmlx has shared HTML extraction helpers used by the wol and jworg
// document parsers.
package htmlx

import (
	"net/url"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"github.com/dgrieser/jw-cli/internal/model"
)

// cardThumbnail holds the publication cover on a wol link card. It is
// navigation decoration, not an illustration: no caption, no alt text, and the
// same cover repeats for every link to that publication. The body renderer drops
// it too (see render.cardDecoration).
const cardThumbnail = ".cardThumbnail"

// Images collects figure/inline images with their metadata from sel: caption,
// alt text, the rights line the site prints beside the picture, and the pixel
// size when the markup states it. Relative sources are absolutized against
// base. Link-card thumbnails are skipped.
//
// The metadata is only here to be had: the sites serve their images with
// EXIF/IPTC stripped, so nothing about a picture is readable from the file.
func Images(sel *goquery.Selection, base string) []model.MediaAsset {
	var out []model.MediaAsset
	seen := map[string]bool{}
	add := func(s *goquery.Selection, src, alt string) {
		src = absolutize(src, base)
		if src == "" || seen[src] {
			return
		}
		seen[src] = true
		w, h := dimensionsOf(s)
		out = append(out, model.MediaAsset{
			URL:     src,
			Alt:     clean(alt),
			Caption: captionFor(s),
			Credit:  creditFor(s),
			Width:   w,
			Height:  h,
		})
	}
	// responsive spans first: they carry the largest renditions
	sel.Find("span.jsRespImg").Each(func(_ int, s *goquery.Selection) {
		if s.Closest(cardThumbnail).Length() > 0 {
			return
		}
		src := ""
		for _, attr := range []string{"data-zoom", "data-img-size-xl", "data-img-size-lg", "data-img-size-md", "data-img-size-sm", "data-img-size-xs"} {
			if v, ok := s.Attr(attr); ok && v != "" {
				src = v
				break
			}
		}
		alt, _ := s.Attr("data-img-att-alt")
		add(s, src, alt)
	})
	sel.Find("img").Each(func(_ int, s *goquery.Selection) {
		if s.Closest("noscript").Length() > 0 && s.Closest("span.jsRespImg").Length() > 0 {
			return // fallback of a span we already handled
		}
		if s.Closest(cardThumbnail).Length() > 0 {
			return
		}
		src := ""
		for _, attr := range []string{"data-zoom", "data-img-size-xl", "data-img-size-lg", "src"} {
			if v, ok := s.Attr(attr); ok && v != "" {
				src = v
				break
			}
		}
		alt, _ := s.Attr("alt")
		add(s, src, alt)
	})
	return out
}

func captionFor(s *goquery.Selection) string {
	if fig := s.Closest("figure"); fig.Length() > 0 {
		if cap := fig.Find("figcaption"); cap.Length() > 0 {
			return clean(cap.Text())
		}
	}
	return nearestText(s, ".caption, figcaption")
}

// imgCredit is the rights line wol and jw.org print beside an illustration
// ("Institute of Archaeology/Hebrew University © ..."). It sits next to the
// image inside the same figure, never inside the caption.
const imgCredit = ".imgCredit"

// creditFor reads that line for the image at s.
func creditFor(s *goquery.Selection) string {
	if fig := s.Closest("figure"); fig.Length() > 0 {
		if c := fig.Find(imgCredit).First(); c.Length() > 0 {
			return clean(c.Text())
		}
	}
	return nearestText(s, imgCredit)
}

// nearestText finds what the image's own container says: the first match of
// want inside the closest wrapper of s. The wrappers are tried from the most
// specific outwards and the search stops at the first one that exists, so a
// caption two pictures over is not picked up.
func nearestText(s *goquery.Selection, want string) string {
	for _, parentSel := range []string{".pictureContainer", ".imgGrid", "figure", "div"} {
		p := s.Closest(parentSel)
		if p.Length() == 0 {
			continue
		}
		if hit := p.Find(want).First(); hit.Length() > 0 {
			return clean(hit.Text())
		}
		break
	}
	return ""
}

// dimensionsOf reads the pixel size of an image: wol states it on the <img>
// tag, and a zoomable responsive span states the size of its zoom rendition.
func dimensionsOf(s *goquery.Selection) (width, height int) {
	for _, pair := range [][2]string{{"width", "height"}, {"data-zoom-width", "data-zoom-height"}} {
		w := intAttr(s, pair[0])
		h := intAttr(s, pair[1])
		if w > 0 || h > 0 {
			return w, h
		}
	}
	return 0, 0
}

func intAttr(s *goquery.Selection, attr string) int {
	v, ok := s.Attr(attr)
	if !ok {
		return 0
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n < 0 {
		return 0
	}
	return n
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

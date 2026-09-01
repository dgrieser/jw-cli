package wol

import (
	"context"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html"
)

// The blocks a search hit can sit in. A hit in a table cell is reported as the
// whole table: one cell on its own says nothing.
const selDocBlock = "p, li, td, th, figcaption, blockquote, h1, h2, h3, h4, h5, h6"

// chromeInBlock is what a block carries that is not text: the answer boxes of a
// study article, and anything scripted.
const chromeInBlock = ".gen-field, textarea, script, style"

// textProbe is how much of a fragment is matched against the document when the
// fragment names no paragraph id and holds no link. wol elides the middle and
// the end of a fragment, never its start. textProbeMin is the point below
// which a fragment says too little to place: matching "30-31" would land
// somewhere, and somewhere is worse than the teaser.
const (
	textProbe    = 60
	textProbeMin = 20
)

// Excerpts returns the document blocks that the search fragments of one result
// were cut from, as HTML and in document order. fragment is the result's
// snippet HTML, pageURL the document it links to. Best effort: a fragment that
// cannot be placed is skipped, and a result none of whose fragments can be
// placed comes back empty rather than as an error.
func (c *Client) Excerpts(ctx context.Context, pageURL, fragment string) ([]string, error) {
	if strings.TrimSpace(fragment) == "" {
		return nil, nil
	}
	doc, err := c.documentPage(ctx, pageURL)
	if err != nil {
		return nil, err
	}
	frag, err := goquery.NewDocumentFromReader(strings.NewReader(fragment))
	if err != nil {
		return nil, err
	}
	found := map[*html.Node]bool{}
	pieces := frag.Find("body").Children()
	if pieces.Length() == 0 {
		// a fragment of bare text still has its opening words to match on
		pieces = frag.Find("body")
	}
	pieces.Each(func(_ int, piece *goquery.Selection) {
		if n := locate(doc, piece); n != nil {
			found[n] = true
		}
	})
	if len(found) == 0 {
		return nil, nil
	}
	return blocksInOrder(doc, found), nil
}

// locate places one fragment piece in the document: by the paragraph id it
// carries, else by a link the document repeats, else by its opening words.
func locate(doc *goquery.Document, piece *goquery.Selection) *html.Node {
	for _, pid := range attrsOf(piece, "data-pid") {
		if s := doc.Find(`[data-pid="` + pid + `"]`).First(); s.Length() > 0 {
			return blockOf(s)
		}
	}
	for _, href := range attrsOf(piece, "href") {
		if s := doc.Find(`a[href="` + href + `"]`).First(); s.Length() > 0 {
			return blockOf(s)
		}
	}
	probe := probeOf(piece.Text())
	if probe == "" {
		return nil
	}
	var hit *goquery.Selection
	doc.Find(selDocBlock).EachWithBreak(func(_ int, s *goquery.Selection) bool {
		if strings.Contains(normalize(s.Text()), probe) {
			hit = s
			return false
		}
		return true
	})
	if hit == nil {
		return nil
	}
	return blockOf(hit)
}

// attrsOf collects an attribute off the piece itself and everything under it,
// outermost first, skipping the values that cannot be looked up.
func attrsOf(piece *goquery.Selection, attr string) []string {
	var out []string
	add := func(s *goquery.Selection) {
		if v, ok := s.Attr(attr); ok {
			if v = strings.TrimSpace(v); v != "" && !strings.Contains(v, `"`) {
				out = append(out, v)
			}
		}
	}
	add(piece)
	piece.Find("[" + attr + "]").Each(func(_ int, s *goquery.Selection) { add(s) })
	return out
}

// probeOf is the opening run of a fragment's text, normalized for matching. A
// fragment that is only wol's "..." separator has nothing to match on.
func probeOf(text string) string {
	// a piece that was cut carries the mark of it; the document does not
	t := strings.TrimRight(normalize(text), " .…")
	r := []rune(t)
	if len(r) < textProbeMin {
		return ""
	}
	if len(r) > textProbe {
		t = string(r[:textProbe])
	}
	return t
}

// softHyphen is the break hint wol sets inside long words; it is invisible and
// must not stand between a fragment and the passage it came from.
const softHyphen = "\u00ad"

// normalize makes two renderings of the same sentence comparable: the document
// breaks words with soft hyphens and spreads them over inline elements, so only
// the collapsed text without those hyphens matches.
func normalize(s string) string {
	return cleanSpace(strings.ReplaceAll(s, softHyphen, ""))
}

// blockOf is the block a hit belongs to: the nearest paragraph, list item,
// heading or caption around it, and the whole table when the hit is in a cell.
func blockOf(s *goquery.Selection) *html.Node {
	block := s
	if !s.Is(selDocBlock) {
		block = s.Closest(selDocBlock)
	}
	if block.Length() == 0 {
		return nil
	}
	if block.Is("td, th") {
		if t := block.Closest("table"); t.Length() > 0 {
			return t.Nodes[0]
		}
	}
	return block.Nodes[0]
}

// blocksInOrder walks the document once and returns the marked blocks in the
// order they are read, leaving out a block another one already contains.
func blocksInOrder(doc *goquery.Document, found map[*html.Node]bool) []string {
	var out []string
	var taken []*goquery.Selection
	doc.Find(selDocBlock + ", table").Each(func(_ int, s *goquery.Selection) {
		if !found[s.Nodes[0]] {
			return
		}
		for _, prev := range taken {
			if prev.Find("*").AddSelection(prev).IsSelection(s) {
				return // already inside a block that was taken
			}
		}
		taken = append(taken, s)
		clone := s.Clone()
		clone.Find(chromeInBlock).Remove()
		if h, err := goquery.OuterHtml(clone); err == nil {
			out = append(out, h)
		}
	})
	return out
}

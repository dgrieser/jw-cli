package wol

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"github.com/dgrieser/jw-cli/internal/httpx"
	"github.com/dgrieser/jw-cli/internal/model"
)

const (
	selOutline          = "ul.outline" // the outline of contents, nested lists of headings
	classOutlineChapter = "chapterNo"  // a list item that only names the chapter its children are in
)

// outlineDocs are the documents a book's headings are kept in, in the order
// they are tried: most books have an outline of contents, the Gospels and Acts
// an overview written across chapters instead.
var outlineDocs = []string{"outline", "overview"}

// Outline reads the outline of contents of a bible book: what each stretch of
// verses is about, as the study bible lists it ahead of the book. An edition or
// a book that has none answers with no headings and no error. Kept until the
// page changes upstream.
func (c *Client) Outline(ctx context.Context, cfg Config, edition string, book int) ([]model.OutlineItem, error) {
	if edition == "" {
		edition = "nwtsty"
	}
	u := c.url(cfg, "bibledocument", fmt.Sprintf("/%s/%d", edition, book))
	return httpx.Memo(ctx, c.cache, "outline1-"+u, func(ctx context.Context) ([]model.OutlineItem, error) {
		return c.outline(ctx, u, book)
	})
}

func (c *Client) outline(ctx context.Context, u string, book int) ([]model.OutlineItem, error) {
	var firstErr error
	read := false
	for _, kind := range outlineDocs {
		doc, err := c.hc.GetHTML(ctx, u+"/"+kind)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		read = true
		// an edition without the document is answered with the library's
		// home page, which simply has no outline on it
		if items := parseOutline(doc, book); len(items) > 0 {
			return items, nil
		}
	}
	if read {
		return nil, nil
	}
	return nil, firstErr
}

// outlineSpan is the verses one heading covers, as BBCCCVVV ids.
type outlineSpan struct{ start, end int }

func chapterSpan(book, chapter int) outlineSpan {
	base := book*1_000_000 + chapter*1_000
	return outlineSpan{base, base + 999}
}

// parseOutline lists the headings of an outline page in reading order. A
// chapter number is not a heading of its own but the chapter the verse numbers
// below it count in; a heading that names no verses covers what the heading
// above it covers — a psalm's theme over the lines picked out of it.
func parseOutline(doc *goquery.Document, book int) []model.OutlineItem {
	var out []model.OutlineItem
	var walk func(ul *goquery.Selection, depth int, parent outlineSpan)
	walk = func(ul *goquery.Selection, depth int, parent outlineSpan) {
		ul.ChildrenFiltered("li").Each(func(_ int, li *goquery.Selection) {
			p := li.ChildrenFiltered("p").First()
			sub := li.ChildrenFiltered("ul")
			if li.HasClass(classOutlineChapter) {
				ch, err := strconv.Atoi(cleanSpace(p.Text()))
				if err != nil || ch <= 0 {
					return
				}
				walk(sub, depth, chapterSpan(book, ch))
				return
			}
			label := cleanSpace(p.Find("a").Last().Text())
			title := outlineTitle(cleanSpace(p.Text()), label)
			if title == "" {
				return
			}
			span, ok := parseOutlineSpan(label, book, parent.start/1_000%1_000)
			if !ok {
				span, label = parent, ""
			}
			out = append(out, model.OutlineItem{
				Depth: depth, Title: title, Label: label, Start: span.start, End: span.end,
			})
			walk(sub, depth+1, span)
		})
	}
	// a book of one chapter lists its headings without a chapter number
	walk(doc.Find(selOutline).First(), 0, chapterSpan(book, 1))
	return out
}

// outlineTitle is a heading without the verses written after it in
// parentheses: "Six days of preparing the earth (3-31)".
func outlineTitle(text, label string) string {
	if label == "" || !strings.HasSuffix(text, ")") {
		return text
	}
	i := strings.LastIndex(text, "(")
	if i < 0 || !strings.Contains(text[i:], label) {
		return text
	}
	return strings.TrimSpace(text[:i])
}

// outlineVerse is one verse an outline names: "31", or "3:17" across chapters.
var outlineVerse = regexp.MustCompile(`(\d+)(?:\s*:\s*(\d+))?`)

// parseOutlineSpan reads the verses a heading names — "1, 2", "3-31",
// "37-46a", "1:18–3:17" — counting a bare verse number in chapter. Only the
// first and the last verse matter: a heading covers what lies between them.
func parseOutlineSpan(label string, book, chapter int) (outlineSpan, bool) {
	ms := outlineVerse.FindAllStringSubmatch(label, -1)
	if len(ms) == 0 {
		return outlineSpan{}, false
	}
	at := func(m []string, chapter int) (int, int) {
		n, _ := strconv.Atoi(m[1])
		if m[2] == "" {
			return chapter, n
		}
		v, _ := strconv.Atoi(m[2])
		return n, v
	}
	sc, sv := at(ms[0], chapter)
	ec, ev := at(ms[len(ms)-1], sc)
	span := outlineSpan{
		start: book*1_000_000 + sc*1_000 + sv,
		end:   book*1_000_000 + ec*1_000 + ev,
	}
	if span.end < span.start {
		span.end = span.start
	}
	return span, true
}

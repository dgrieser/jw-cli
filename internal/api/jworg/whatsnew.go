package jworg

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"github.com/dgrieser/jw-cli/internal/model"
)

// whatsNewDocID is jw.org's page of what is new on the site ("Neu auf
// jw.org"), which the finder leads to in every language.
const whatsNewDocID = 1011267

// ErrNoContent is a page of jw.org without what it is the page of: jw.org's
// bot management answers some networks — a server's — that way, with a
// page of its own and a 200.
var ErrNoContent = errors.New("jw.org answered without the page's content (its bot protection may be turning this network away)")

// WhatsNewURL is the page of what is new on jw.org in a language.
func (c *Client) WhatsNewURL(symbol string) string {
	return c.DocumentURL(symbol, whatsNewDocID)
}

// WhatsNew reads jw.org's page of what is new on the site: the articles,
// publications, news and videos it published lately, newest first.
func (c *Client) WhatsNew(ctx context.Context, symbol string) ([]model.NewItem, error) {
	u := c.WhatsNewURL(symbol)
	var items []model.NewItem
	_, err := c.hc.GetHTMLChecked(ctx, u, func(doc *goquery.Document) error {
		// the page lists itself first, dateless: a page of jw.org without
		// one entry is not that page
		if doc.Find(".synopsis").Length() == 0 {
			return fmt.Errorf("what's new %s: %w", u, ErrNoContent)
		}
		items = parseWhatsNew(doc, c.hc.Base.JWOrg)
		return nil
	})
	return items, err
}

// parseWhatsNew reads the entries of the page: each a .synopsis with a date,
// a context line (the publication or "NEWS"), a title linking to the page,
// and a square picture. The page's own entry, which has no date, is not one.
func parseWhatsNew(doc *goquery.Document, base string) []model.NewItem {
	var items []model.NewItem
	doc.Find(".synopsis").Each(func(_ int, s *goquery.Selection) {
		a := s.Find("h3 a[href]").First()
		if a.Length() == 0 {
			a = s.Find(".syn-body a[href]").First()
		}
		href, _ := a.Attr("href")
		date, err := time.Parse(time.DateOnly, clean(s.Find(".pubDate").First().Text()))
		if href == "" || err != nil {
			return
		}
		it := model.NewItem{
			Title:     clean(a.Text()),
			URL:       absolute(base, href),
			Published: date.Format(time.DateOnly),
			Context:   clean(s.Find(".contextTitle").First().Text()),
		}
		img := s.Find(".jsRespImg").First()
		for _, size := range []string{"data-img-size-md", "data-img-size-lg", "data-img-size-sm", "data-img-size-xs"} {
			if v, ok := img.Attr(size); ok && v != "" {
				it.ImageURL = v
				break
			}
		}
		if it.ImageURL == "" {
			it.ImageURL, _ = s.Find(".syn-img img[src]").First().Attr("src")
		}
		cls, _ := s.Attr("class")
		it.Kind, it.DocID, it.Symbol, it.Issue = classify(cls)
		if lank := mediaKey(href); lank != "" {
			it.Kind, it.LANK = "video", lank
		}
		items = append(items, it)
	})
	return items
}

// classify reads what an entry is off its class list: "news" and the
// announcements (NewsArticlePage) are news, an issue of a periodical an
// issue, anything under the library a publication; the rest are articles of
// the site. The docId-… token names the document, pub-… and iss-… the
// publication and the issue it is from.
func classify(cls string) (kind string, docid int, symbol, issue string) {
	kind = "article"
	var news, pubs, isIssue bool
	for tok := range strings.FieldsSeq(cls) {
		switch {
		case tok == "news" || tok == "NewsArticlePage":
			news = true
		case tok == "publications":
			pubs = true
		case tok == "PublicationIssue":
			isIssue = true
		case strings.HasPrefix(tok, "docId-"):
			docid, _ = strconv.Atoi(strings.TrimPrefix(tok, "docId-"))
		case strings.HasPrefix(tok, "pub-"):
			symbol = strings.TrimPrefix(tok, "pub-")
		case strings.HasPrefix(tok, "iss-"):
			issue = strings.TrimPrefix(tok, "iss-")
		}
	}
	switch {
	case isIssue:
		kind = "issue"
	case news:
		kind = "news"
	case pubs:
		kind = "publication"
	}
	return kind, docid, symbol, issue
}

// mediaKey is the media item a link of the video library opens:
// /de/bibliothek/videos/#de/mediaitems/LatestVideos/pub-jwb-142_1_VIDEO.
func mediaKey(href string) string {
	_, frag, ok := strings.Cut(href, "#")
	if !ok {
		return ""
	}
	parts := strings.Split(frag, "/")
	for i, p := range parts {
		if p == "mediaitems" && i+2 < len(parts) {
			return parts[i+2]
		}
	}
	return ""
}

// absolute resolves a link of a jw.org page against the site.
func absolute(base, href string) string {
	u, err := url.Parse(href)
	if err != nil || u.IsAbs() {
		return href
	}
	b, err := url.Parse(base)
	if err != nil {
		return href
	}
	return b.ResolveReference(u).String()
}

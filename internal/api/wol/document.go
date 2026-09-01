package wol

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"github.com/dgrieser/jw-cli/internal/htmlx"
	"github.com/dgrieser/jw-cli/internal/model"
)

// Document fetches article docid from the library.
func (c *Client) Document(ctx context.Context, cfg Config, docid int) (model.Article, error) {
	return c.DocumentByURL(ctx, c.url(cfg, "d", fmt.Sprintf("/%d", docid)))
}

// DocumentByURL fetches and parses any wol document page (including finder
// redirect URLs from search results).
func (c *Client) DocumentByURL(ctx context.Context, pageURL string) (model.Article, error) {
	doc, err := c.documentPage(ctx, pageURL)
	if err != nil {
		return model.Article{}, err
	}
	art := parseDocument(doc, c.hc.Base.WOL)
	art.URL = pageURL
	if art.DocID == 0 {
		art.DocID = docIDFromURL(pageURL)
	}
	if art.HTML == "" {
		return art, fmt.Errorf("no article content found at %s", pageURL)
	}
	return art, nil
}

const (
	docPageKey    = "woldoc1-"
	docPageMaxAge = 7 * 24 * time.Hour
)

// documentPage fetches a document page, from the disk cache when it is there.
// A published document does not change, so a week is safe, and it makes the
// second reading of the same publication — a listing that quotes it, then
// jw show on one of its rows — free.
func (c *Client) documentPage(ctx context.Context, pageURL string) (*goquery.Document, error) {
	key := docPageKey + docPageCacheKey(pageURL)
	var body string
	if !c.cache.Get(key, docPageMaxAge, &body) || body == "" {
		var err error
		if body, err = c.hc.GetText(ctx, pageURL, nil); err != nil {
			return nil, err
		}
		c.cache.Put(key, body)
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("parse HTML %s: %w", pageURL, err)
	}
	return doc, nil
}

// docPageCacheKey drops the query and the fragment: a search links the same
// document with its own ?q=, and every one of them is the same page.
func docPageCacheKey(pageURL string) string {
	u, err := url.Parse(pageURL)
	if err != nil {
		return pageURL
	}
	u.RawQuery, u.Fragment = "", ""
	return u.Host + u.Path
}

// contentSelectors are tried in order to find the document body; kept
// together so live layout drift is cheap to fix.
var contentSelectors = []string{"#article", "article", ".bodyTxt", "main"}

func parseDocument(doc *goquery.Document, base string) model.Article {
	var art model.Article

	var content *goquery.Selection
	for _, sel := range contentSelectors {
		if s := doc.Find(sel).First(); s.Length() > 0 {
			content = s
			break
		}
	}
	if content == nil {
		return art
	}

	title := doc.Find("h1").First()
	art.Title = cleanSpace(title.Text())
	if art.Title == "" {
		art.Title = cleanSpace(doc.Find("title").Text())
	}

	// drop non-content chrome inside the article container
	pruned := content.Clone()
	pruned.Find("#regionMain nav, .resultsNavigationSelected, #docSubMedia, .groupTOC, #contentColumnControl, " + todayChrome).Remove()
	html, err := goquery.OuterHtml(pruned)
	if err == nil {
		art.HTML = html
	}
	art.Images = htmlx.Images(content, base)
	art.ScriptureRefs = htmlx.ScriptureRefs(content, base)
	return art
}

var docIDPattern = regexp.MustCompile(`(?:/d/[^/]+/[^/]+/|docid=)(\d+)`)

func docIDFromURL(u string) int {
	if m := docIDPattern.FindStringSubmatch(u); m != nil {
		id, _ := strconv.Atoi(m[1])
		return id
	}
	// /{loc}/wol/d/r1/lp-e/1102025912 -> last path segment
	parts := strings.Split(strings.TrimRight(u, "/"), "/")
	if len(parts) > 0 {
		if id, err := strconv.Atoi(parts[len(parts)-1]); err == nil {
			return id
		}
	}
	return 0
}

func cleanSpace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
